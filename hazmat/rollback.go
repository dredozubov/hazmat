package main

import (
	"fmt"
	"os"
	"strings"

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

	runRollbackSteps(rollbackStepContext{
		ui:          ui,
		runner:      r,
		deleteUser:  deleteUser,
		deleteGroup: deleteGroup,
	})
	rollbackProjectHooks(ui)

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

func rollbackSeatbelt(ui *UI, r *Runner) {
	ui.Step("Remove seatbelt profile and wrapper")

	for _, path := range []string{seatbeltWrapperPath} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			ui.SkipDone(fmt.Sprintf("%s not present", path))
			continue
		}
		if err := r.Sudo("remove seatbelt wrapper", "rm", "-f", path); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", path, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed %s", path))
		}
	}
}

func rollbackUserExperience(ui *UI, r *Runner) {
	ui.Step("Remove command wrappers and shell integration")

	if _, err := os.Stat(agentEnvPath); os.IsNotExist(err) {
		ui.SkipDone(fmt.Sprintf("%s not present", agentEnvPath))
	} else if err := r.Sudo("remove agent environment file", "rm", "-f", agentEnvPath); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", agentEnvPath, err))
	} else {
		ui.Ok(fmt.Sprintf("Removed %s", agentEnvPath))
	}

	for _, path := range []string{
		hostWrapperPath(hostClaudeWrapperName),
		hostWrapperPath(hostExecWrapperName),
		hostWrapperPath(hostShellWrapperName),
	} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			ui.SkipDone(fmt.Sprintf("%s not present", path))
			continue
		}
		if err := os.Remove(path); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", path, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed %s", path))
		}
	}

	agentZshrc := agentHome + "/.zshrc"
	if data, err := asAgentOutput("cat", agentZshrc); err == nil &&
		strings.Contains(data, agentShellBlockStart) {
		cleaned := removeManagedBlock(data, agentShellBlockStart, agentShellBlockEnd)
		if err := r.SudoWriteFile("remove hazmat shell block from agent .zshrc", agentZshrc, cleaned); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", agentZshrc, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed hazmat shell block from %s", agentZshrc))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Hazmat shell block not present in %s", agentZshrc))
	}

	for _, profile := range supportedUserShellProfiles() {
		if data, err := os.ReadFile(profile.rcPath); err == nil &&
			strings.Contains(string(data), userPathBlockStart) {
			cleaned := removeManagedBlock(string(data), userPathBlockStart, userPathBlockEnd)
			if err := r.UserWriteFile(profile.rcPath, cleaned); err != nil {
				ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", profile.rcPath, err))
			} else {
				ui.Ok(fmt.Sprintf("Removed hazmat PATH block from %s", profile.rcPath))
			}
		} else {
			ui.SkipDone(fmt.Sprintf("Hazmat PATH block not present in %s", profile.rcPath))
		}
	}
}

func rollbackHomeDirTraverse(ui *UI, r *Runner) {
	ui.Step("Remove home directory traverse ACL")

	homeDir := os.Getenv("HOME")
	if homeHasAgentTraverseACL(homeDir) {
		inv := sudoACLInvoker{runner: r, reason: "remove home directory traverse ACL"}
		if err := removeACL(inv, homeDir, agentTraverseGrant); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove home traversal ACL: %v", err))
		} else {
			ui.Ok("Removed home traversal ACL")
		}
	} else {
		ui.SkipDone("Home traversal ACL not present")
	}
}

func rollbackUmask(ui *UI, r *Runner) {
	ui.Step("Remove umask managed block from shell rc files")

	// Agent .zshrc — only remove the block this tool added.
	agentZshrc := agentHome + "/.zshrc"
	if data, err := asAgentOutput("cat", agentZshrc); err == nil &&
		strings.Contains(data, umaskBlockStart) {
		cleaned := removeManagedBlock(data, umaskBlockStart, umaskBlockEnd)
		if err := r.SudoWriteFile("remove umask block from agent .zshrc", agentZshrc, cleaned); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", agentZshrc, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed umask block from %s", agentZshrc))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Umask block not present in %s", agentZshrc))
	}

	for _, profile := range supportedUserShellProfiles() {
		if data, err := os.ReadFile(profile.rcPath); err == nil &&
			strings.Contains(string(data), umaskBlockStart) {
			cleaned := removeManagedBlock(string(data), umaskBlockStart, umaskBlockEnd)
			if err := r.UserWriteFile(profile.rcPath, cleaned); err != nil {
				ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", profile.rcPath, err))
			} else {
				ui.Ok(fmt.Sprintf("Removed umask block from %s", profile.rcPath))
			}
		} else {
			ui.SkipDone(fmt.Sprintf("Umask block not present in %s", profile.rcPath))
		}
	}
}

func rollbackLocalRepo(ui *UI) {
	ui.Step("Remove local snapshot repository")

	if _, err := os.Stat(localRepoDir); os.IsNotExist(err) {
		ui.SkipDone("Local snapshot repository not present")
		return
	}

	// Remove config file and repo directory. Both are user-owned, no sudo.
	os.Remove(localConfigFile) //nolint:errcheck // best-effort config cleanup during rollback
	if err := os.RemoveAll(localRepoDir); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", localRepoDir, err))
	} else {
		ui.Ok(fmt.Sprintf("Removed %s", localRepoDir))
	}
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
