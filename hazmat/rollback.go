package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newRollbackCmd() *cobra.Command {
	var deleteUser, deleteGroup bool
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Undo host mutations made by hazmat setup",
		Long: `Reverses the system-level changes applied by hazmat setup:

  - pf anchor file and /etc/pf.conf additions
  - LaunchDaemon for pf persistence
  - DNS blocklist from /etc/hosts
  - Sudoers entry (/etc/sudoers.d/agent)
  - Seatbelt profile and wrapper
  - Agent shell env + host wrapper commands
  - Workspace access helpers (/Users/agent/workspace, home-directory ACL)
  - umask 077 lines from .zshrc files
  - Backup scope file (.backup-excludes)

User and group deletion require explicit flags because they are destructive:
  --delete-user   Delete the agent user account and home directory
  --delete-group  Delete the dev group

The workspace root (` + sharedWorkspace + `) is NOT removed automatically.
Back it up first if needed: hazmat backup /Volumes/BACKUP/workspace

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
	ui := &UI{DryRun: flagDryRun}
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
	} else if !ui.Ask("Rollback all hazmat setup changes?") {
		fmt.Println("  Aborted.")
		return nil
	}

	rollbackLaunchDaemon(ui, r)
	rollbackPfFirewall(ui, r)
	rollbackDNSBlocklist(ui, r)
	rollbackSudoers(ui, r)
	rollbackSeatbelt(ui, r)
	rollbackUserExperience(ui, r)
	rollbackSymlinks(ui, r)
	rollbackUmask(ui, r)
	rollbackBackupScope(ui, r)

	if deleteUser {
		rollbackAgentUser(ui, r)
	} else {
		ui.WarnMsg(fmt.Sprintf("Agent user '%s' not removed. Use --delete-user to delete the account and %s.", agentUser, agentHome))
	}

	if deleteGroup {
		rollbackDevGroup(ui, r)
	} else {
		ui.WarnMsg(fmt.Sprintf("Group '%s' not removed. Use --delete-group to delete it.", sharedGroup))
	}

	fmt.Println()
	cGreen.Println("  Rollback complete.")
	cYellow.Printf("  Note: workspace root at %s was not touched. Remove it manually if no longer needed.\n", sharedWorkspace)
	return nil
}

// ── Rollback steps ────────────────────────────────────────────────────────────

func rollbackLaunchDaemon(ui *UI, r *Runner) {
	ui.Step("Remove LaunchDaemon")

	if _, err := os.Stat(pfDaemonPlist); os.IsNotExist(err) {
		ui.SkipDone("LaunchDaemon plist not present")
		return
	}

	// bootout may fail if the daemon was never loaded; ignore the error.
	r.Sudo("launchctl", "bootout", "system", pfDaemonPlist) //nolint:errcheck
	ui.Ok("LaunchDaemon unloaded (or was not loaded)")

	if err := r.Sudo("rm", "-f", pfDaemonPlist); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", pfDaemonPlist, err))
	} else {
		ui.Ok(fmt.Sprintf("Removed %s", pfDaemonPlist))
	}
}

func rollbackPfFirewall(ui *UI, r *Runner) {
	ui.Step("Remove pf anchor")

	if _, err := os.Stat(pfAnchorFile); os.IsNotExist(err) {
		ui.SkipDone("pf anchor file not present")
	} else {
		if err := r.Sudo("rm", "-f", pfAnchorFile); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", pfAnchorFile, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed pf anchor file %s", pfAnchorFile))
		}
	}

	pfConf := "/etc/pf.conf"
	data, err := os.ReadFile(pfConf)
	if err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not read %s: %v", pfConf, err))
		return
	}

	if !strings.Contains(string(data), `anchor "agent"`) {
		ui.SkipDone("/etc/pf.conf does not reference the agent anchor")
		return
	}

	// Prefer restoring from the timestamped backup made during setup.
	backup := latestPfConfBackup()
	if backup != "" {
		if err := r.Sudo("cp", "-f", backup, pfConf); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not restore %s from backup %s: %v — stripping lines in place", pfConf, backup, err))
			stripPfAnchorLines(ui, r, pfConf, data)
		} else {
			ui.Ok(fmt.Sprintf("Restored %s from backup %s", pfConf, backup))
		}
	} else {
		ui.WarnMsg("No timestamped backup of /etc/pf.conf found — stripping anchor lines in place")
		stripPfAnchorLines(ui, r, pfConf, data)
	}

	if err := r.PfctlLoad(); err != nil {
		ui.WarnMsg(fmt.Sprintf("pfctl reload failed: %v", err))
	} else {
		ui.Ok("pf rules reloaded")
	}
}

// latestPfConfBackup returns the most-recent /etc/pf.conf.backup.YYYYMMDDHHMMSS, or "".
func latestPfConfBackup() string {
	entries, err := filepath.Glob("/etc/pf.conf.backup.*")
	if err != nil || len(entries) == 0 {
		return ""
	}
	// filepath.Glob returns lexicographically sorted entries; the last has the
	// highest timestamp (YYYYMMDDHHMMSS sorts correctly as a string).
	return entries[len(entries)-1]
}

// stripPfAnchorLines rewrites pfConf with the agent anchor stanza removed.
func stripPfAnchorLines(ui *UI, r *Runner, pfConf string, data []byte) {
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `anchor "agent"`) ||
			strings.Contains(line, `load anchor "agent"`) ||
			strings.TrimSpace(line) == "# Claude Code sandbox user blocklist" {
			continue
		}
		kept = append(kept, line)
	}
	cleaned := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"
	if err := r.SudoWriteFile(pfConf, cleaned); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", pfConf, err))
	} else {
		ui.Ok("Removed agent anchor lines from /etc/pf.conf")
	}
}

func rollbackDNSBlocklist(ui *UI, r *Runner) {
	ui.Step("Remove DNS blocklist")

	data, err := os.ReadFile("/etc/hosts")
	if err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not read /etc/hosts: %v", err))
		return
	}

	if !strings.Contains(string(data), hostsMarker) {
		ui.SkipDone("DNS blocklist not present in /etc/hosts")
		return
	}

	// Remove lines from hostsMarker through hostsEndMarker, inclusive.
	// The block was appended with a leading \n — trim trailing blank lines after removal.
	const endMarker = "# === End AI Agent Blocklist ==="
	var kept []string
	inside := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == hostsMarker {
			inside = true
			continue
		}
		if inside {
			if strings.TrimSpace(line) == endMarker {
				inside = false
			}
			continue
		}
		kept = append(kept, line)
	}

	cleaned := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"
	if err := r.SudoWriteFile("/etc/hosts", cleaned); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not update /etc/hosts: %v", err))
		return
	}
	ui.Ok("Removed DNS blocklist from /etc/hosts")

	// Flush DNS cache — fire-and-forget.
	r.Sudo("dscacheutil", "-flushcache")       //nolint:errcheck
	r.Sudo("killall", "-HUP", "mDNSResponder") //nolint:errcheck
	ui.Ok("DNS cache flushed")
}

func rollbackSudoers(ui *UI, r *Runner) {
	ui.Step("Remove sudoers entry")

	if _, err := os.Stat(sudoersFile); os.IsNotExist(err) {
		ui.SkipDone("Sudoers file not present")
		return
	}

	if err := r.Sudo("rm", "-f", sudoersFile); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", sudoersFile, err))
	} else {
		ui.Ok(fmt.Sprintf("Removed %s", sudoersFile))
	}
}

func rollbackSeatbelt(ui *UI, r *Runner) {
	ui.Step("Remove seatbelt profile and wrapper")

	for _, path := range []string{seatbeltWrapperPath} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			ui.SkipDone(fmt.Sprintf("%s not present", path))
			continue
		}
		if err := r.Sudo("rm", "-f", path); err != nil {
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
	} else if err := r.Sudo("rm", "-f", agentEnvPath); err != nil {
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
		if err := r.SudoWriteFile(agentZshrc, cleaned); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", agentZshrc, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed hazmat shell block from %s", agentZshrc))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Hazmat shell block not present in %s", agentZshrc))
	}

	userZshrc := userZshrcPath()
	if data, err := os.ReadFile(userZshrc); err == nil &&
		strings.Contains(string(data), userPathBlockStart) {
		cleaned := removeManagedBlock(string(data), userPathBlockStart, userPathBlockEnd)
		if err := r.UserWriteFile(userZshrc, cleaned); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", userZshrc, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed hazmat PATH block from %s", userZshrc))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Hazmat PATH block not present in %s", userZshrc))
	}
}

func rollbackSymlinks(ui *UI, r *Runner) {
	ui.Step("Remove workspace access helpers")

	legacyLink := os.Getenv("HOME") + "/workspace-shared"
	if info, err := os.Lstat(legacyLink); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(legacyLink); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove legacy symlink %s: %v", legacyLink, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed legacy symlink %s", legacyLink))
		}
	}

	// /Users/agent/workspace is owned by the agent user — use sudo.
	agentLink := agentHome + "/workspace"
	if asAgentQuiet("test", "-L", agentLink) == nil {
		if err := r.Sudo("rm", "-f", agentLink); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove symlink %s: %v", agentLink, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed symlink %s", agentLink))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("%s is not a symlink or does not exist", agentLink))
	}

	if homeHasAgentTraverseACL(os.Getenv("HOME")) {
		if err := r.Sudo("chmod", "-a", homeTraverseACLEntry(), os.Getenv("HOME")); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove home traversal ACL from %s: %v", os.Getenv("HOME"), err))
		} else {
			ui.Ok(fmt.Sprintf("Removed home traversal ACL from %s", os.Getenv("HOME")))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Home traversal ACL not present on %s", os.Getenv("HOME")))
	}
}

func rollbackUmask(ui *UI, r *Runner) {
	ui.Step("Remove umask managed block from .zshrc files")

	// Agent .zshrc — only remove the block this tool added.
	agentZshrc := agentHome + "/.zshrc"
	if data, err := asAgentOutput("cat", agentZshrc); err == nil &&
		strings.Contains(data, umaskBlockStart) {
		cleaned := removeManagedBlock(data, umaskBlockStart, umaskBlockEnd)
		if err := r.SudoWriteFile(agentZshrc, cleaned); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", agentZshrc, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed umask block from %s", agentZshrc))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Umask block not present in %s", agentZshrc))
	}

	// Current user .zshrc — only remove the block this tool added.
	userZshrc := os.Getenv("HOME") + "/.zshrc"
	if data, err := os.ReadFile(userZshrc); err == nil &&
		strings.Contains(string(data), umaskBlockStart) {
		cleaned := removeManagedBlock(string(data), umaskBlockStart, umaskBlockEnd)
		if err := r.UserWriteFile(userZshrc, cleaned); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", userZshrc, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed umask block from %s", userZshrc))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Umask block not present in %s", userZshrc))
	}
}

func rollbackBackupScope(ui *UI, r *Runner) {
	ui.Step("Remove backup scope file")

	if _, err := os.Stat(backupExcludesFile); os.IsNotExist(err) {
		ui.SkipDone("Backup scope file not present")
		return
	}

	// The scope file lives inside the workspace root — user-owned, no sudo.
	if err := os.Remove(backupExcludesFile); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", backupExcludesFile, err))
	} else {
		ui.Ok(fmt.Sprintf("Removed %s", backupExcludesFile))
	}
}

func rollbackAgentUser(ui *UI, r *Runner) {
	ui.Step(fmt.Sprintf("Delete '%s' user and home directory", agentUser))

	if _, err := user.Lookup(agentUser); err != nil {
		ui.SkipDone(fmt.Sprintf("User '%s' does not exist", agentUser))
	} else {
		if err := r.Sudo("dscl", ".", "-delete", "/Users/"+agentUser); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not delete user record for '%s': %v", agentUser, err))
		} else {
			ui.Ok(fmt.Sprintf("Deleted user record for '%s'", agentUser))
		}
	}

	if _, err := os.Stat(agentHome); err == nil {
		if err := r.Sudo("rm", "-rf", agentHome); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove home directory %s: %v", agentHome, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed home directory %s", agentHome))
		}
	} else {
		ui.SkipDone(fmt.Sprintf("Home directory %s does not exist", agentHome))
	}
}

func rollbackDevGroup(ui *UI, r *Runner) {
	ui.Step(fmt.Sprintf("Delete '%s' group", sharedGroup))

	if _, err := user.LookupGroup(sharedGroup); err != nil {
		ui.SkipDone(fmt.Sprintf("Group '%s' does not exist", sharedGroup))
		return
	}

	if err := r.Sudo("dscl", ".", "-delete", "/Groups/"+sharedGroup); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not delete group '%s': %v", sharedGroup, err))
	} else {
		ui.Ok(fmt.Sprintf("Deleted group '%s'", sharedGroup))
	}
}
