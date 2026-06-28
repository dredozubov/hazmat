package hazmat

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRollbackCmd() *cobra.Command {
	var deleteUser, deleteGroup bool
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Undo host mutations made by hazmat init",
		Long: `Reverses the system-level changes applied by hazmat init:

  - pf anchor file and /etc/pf.conf additions
  - LaunchDaemon for pf persistence
  - DNS blocklist from /etc/hosts
  - Sudoers entries (/etc/sudoers.d/agent, /etc/sudoers.d/agent-maintenance)
  - Seatbelt profile and wrapper
  - Agent shell env + host wrapper commands
  - Workspace access helpers (/Users/agent/workspace, home-directory ACL)
  - Workspace ACLs applied to existing project directories
  - umask lines from managed shell rc files
  - Backup scope file (.backup-excludes)

User and group deletion require explicit flags because they are destructive:
  --delete-user   Delete the agent user account and home directory
  --delete-group  Delete the dev group

Your project files are NOT modified or removed. Hazmat-managed repo-local Git
hook state is cleaned up as part of rollback, including host approvals,
approved snapshots, per-repo wrappers, and managed .git dispatchers.
Rollback prints receipts that classify removed, preserved, and manual follow-up
items. Host-owned credential stores and session-time permission repairs are
preserved unless a receipt explicitly names them for removal.

Use --dry-run to preview all commands without executing.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runRollback(deleteUser, deleteGroup)
		},
	}
	cmd.Flags().BoolVar(&deleteUser, "delete-user", false,
		"Delete the agent user account and home directory (DESTRUCTIVE)")
	cmd.Flags().BoolVar(&deleteGroup, "delete-group", false,
		"Delete the dev group (DESTRUCTIVE)")
	return cmd
}

func runRollback(deleteUser, deleteGroup bool) error {
	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	r := NewRunner(ui, flagVerbose, flagDryRun)

	if linuxDiagnosticRuntimeOS() == "linux" {
		return runLinuxRollback(ui, r, deleteUser, deleteGroup)
	}

	if err := checkPlatform(); err != nil {
		ui.Fatal(err.Error())
	}

	fmt.Println()
	cBold.Println("  ┌────────────────────────────────────────────────┐")
	cBold.Println("  │  Option A Rollback — undo all setup changes    │")
	cBold.Println("  └────────────────────────────────────────────────┘")
	fmt.Println()

	if flagDryRun {
		cYellow.Println("  ────────────────────────────────────────────────────")
		cYellow.Println("  DRY RUN — no changes will be made.")
		cYellow.Println("  ────────────────────────────────────────────────────")
		fmt.Println()
	} else if !ui.Ask("Rollback all hazmat init changes?") {
		fmt.Println("  Aborted.")
		return nil
	}

	// Run reverse migrations first — removes artifacts from newer versions
	// that the core rollback functions don't know about. The TLA+ spec
	// (MC_Migration) proves AgentContained holds during this process.
	runDownMigrations(ui, r)

	if err := runRollbackSteps(rollbackStepContext{
		ui:          ui,
		runner:      r,
		deleteUser:  deleteUser,
		deleteGroup: deleteGroup,
	}); err != nil {
		return err
	}
	rollbackProjectHooks(ui)
	if entries, err := inspectCredentialInventory(""); err == nil {
		printRollbackReceipts(rollbackReceipts(deleteUser, deleteGroup, entries))
	} else {
		ui.WarnMsg(fmt.Sprintf("Could not inspect credential inventory for rollback receipts: %v", err))
		printRollbackReceipts(rollbackReceipts(deleteUser, deleteGroup, nil))
	}

	fmt.Println()
	cGreen.Println("  Rollback complete.")
	fmt.Println("  Your project files were not touched.")
	return nil
}

// ── Rollback steps ────────────────────────────────────────────────────────────

func rollbackLaunchDaemon(ui *UI, r *Runner) {
	nativeServiceBackendForHost().RollbackLaunchDaemon(ui, r)
}

func rollbackPfFirewall(ui *UI, r *Runner) {
	nativeServiceBackendForHost().RollbackPfFirewall(ui, r)
}

func rollbackDNSBlocklist(ui *UI, r *Runner) {
	nativeServiceBackendForHost().RollbackDNSBlocklist(ui, r)
}

func rollbackSudoers(ui *UI, r *Runner) {
	nativeServiceBackendForHost().RollbackSudoers(ui, r)
}

func rollbackProjectHooks(ui *UI) {
	ui.Step("Remove repo-local git hook approvals and dispatchers")

	approvals := loadProjectHookApprovals()
	if len(approvals.Approvals) == 0 {
		if _, err := os.Stat(projectHookSnapshotsRootDir); os.IsNotExist(err) {
			ui.SkipDone("Repo-local git hook state not present")
			return
		}
	}

	var cleanedProjects int
	for _, approval := range approvals.Approvals {
		if err := uninstallProjectHookRuntime(approval.ProjectDir); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not fully remove repo hook state for %s: %v", approval.ProjectDir, err))
		} else {
			cleanedProjects++
		}
	}

	if err := os.Remove(projectHookApprovalsFilePath); err != nil && !os.IsNotExist(err) {
		ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", projectHookApprovalsFilePath, err))
	}
	if err := os.RemoveAll(projectHookSnapshotsRootDir); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", projectHookSnapshotsRootDir, err))
	}

	if cleanedProjects == 0 && len(approvals.Approvals) == 0 {
		ui.Ok("Removed repo-local git hook snapshot storage")
		return
	}
	ui.Ok(fmt.Sprintf("Removed repo-local git hook state for %d approved repos", cleanedProjects))
}

func rollbackAgentUser(ui *UI, r *Runner) {
	nativeAccountBackendForHost().RollbackAgentUser(ui, r)
}

func rollbackDevGroup(ui *UI, r *Runner) {
	nativeAccountBackendForHost().RollbackDevGroup(ui, r)
}
