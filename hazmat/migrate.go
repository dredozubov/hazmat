package main

import (
	"fmt"
	"os"
	"os/exec"
)

// migration defines a version transition with forward (up) and reverse (down)
// functions. The TLA+ spec (MC_Migration) proves that:
// - Migrations are applied in sequence (no skipping)
// - AgentContained holds during and after each migration
// - Rollback can reach a clean state from any intermediate state
// - Failed migrations are recoverable by re-running init
type migration struct {
	From string
	To   string
	Up   func(ui *UI, r *Runner) error // forward: old → new
	Down func(ui *UI, r *Runner) error // reverse: new → old (for rollback)
}

// migrations maps "from→to" keys to migration functions.
// Must stay in sync with:
//   - TLA+ spec: MC_Migration.tla HasMigration(), Expected(), NextVersion()
//   - Version list: state.go knownVersions
var migrations = map[string]migration{
	"0.1.0→0.2.0": {
		From: "0.1.0",
		To:   "0.2.0",
		Up:   migrateUp_0_1_0_to_0_2_0,
		Down: migrateDown_0_2_0_to_0_1_0,
	},
	"0.2.0→0.3.0": {
		From: "0.2.0",
		To:   "0.3.0",
		Up:   migrateUp_0_2_0_to_0_3_0,
		Down: migrateDown_0_3_0_to_0_2_0,
	},
}

// ═══════════════════════════════════════════════════════════════════════════════
// v0.1.0 → v0.2.0: Remove workspace concept, add home dir traverse
//
// TLA+ Expected(V1) \ Expected(V2) = {workspace, workspaceACL, agentSymlink}
// TLA+ Expected(V2) \ Expected(V1) = {homeDirTraverse}
// ═══════════════════════════════════════════════════════════════════════════════

func migrateUp_0_1_0_to_0_2_0(ui *UI, r *Runner) error {
	ui.Step("Migration: v0.1.0 → v0.2.0 (remove workspace concept)")

	// Remove agent workspace symlink.
	agentLink := agentHome + "/workspace"
	if out, _ := exec.Command("sudo", "test", "-L", agentLink).CombinedOutput(); len(out) == 0 {
		if err := r.Sudo("remove agent workspace symlink", "rm", "-f", agentLink); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", agentLink, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed %s", agentLink))
		}
	}

	// Remove workspace ACL (dev group inheritable ACL on ~/workspace).
	// This is best-effort — the ACL may not exist or the dir may not exist.
	home := os.Getenv("HOME")
	workspaceDir := home + "/workspace"
	if _, err := os.Stat(workspaceDir); err == nil {
		// Remove the dev group ACL. Errors are non-fatal — the ACL may
		// already be gone or the user may have removed ~/workspace.
		aclEntry := devGroupACLEntry()
		if err := exec.Command("chmod", "-a", aclEntry, workspaceDir).Run(); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove workspace ACL (non-fatal): %v", err))
		} else {
			ui.Ok("Removed dev group ACL from ~/workspace")
		}
	}

	// Restore ~/workspace to normal permissions (755 dr:staff).
	// Only if it's currently 770 dr:dev (as set by old init).
	if info, err := os.Stat(workspaceDir); err == nil {
		if info.Mode().Perm() == 0o770 {
			exec.Command("sudo", "chmod", "755", workspaceDir).Run()
			exec.Command("sudo", "chown", os.Getenv("USER")+":staff", workspaceDir).Run()
			ui.Ok("Restored ~/workspace to 755 dr:staff")
		}
	}

	// Remove legacy workspace-shared symlink if it exists.
	legacyLink := home + "/workspace-shared"
	if info, err := os.Lstat(legacyLink); err == nil && info.Mode()&os.ModeSymlink != 0 {
		os.Remove(legacyLink)
		ui.Ok("Removed legacy ~/workspace-shared symlink")
	}

	// Remove old .backup-excludes file.
	backupExcludes := home + "/workspace/.backup-excludes"
	if _, err := os.Stat(backupExcludes); err == nil {
		os.Remove(backupExcludes)
		ui.Ok("Removed .backup-excludes")
	}

	// homeDirTraverse is added by setupHomeDirTraverse in the normal init
	// flow — no need to duplicate it here.

	return nil
}

func migrateDown_0_2_0_to_0_1_0(ui *UI, r *Runner) error {
	ui.Step("Reverse migration: v0.2.0 → v0.1.0")

	// Remove homeDirTraverse ACL (added by v0.2.0).
	homeDir := os.Getenv("HOME")
	if homeHasAgentTraverseACL(homeDir) {
		if err := r.Sudo("remove home dir traverse ACL",
			"chmod", "-a", homeTraverseACLEntry(), homeDir); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove home traverse ACL: %v", err))
		} else {
			ui.Ok("Removed home directory traverse ACL")
		}
	}

	// v0.1.0 artifacts (workspace, symlink) are NOT restored — the user
	// is rolling back to a clean state, not to v0.1.0. The down function
	// only needs to remove what v0.2.0 added.
	return nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// v0.2.0 → v0.3.0: Add supply chain hardening (npmrc, pip.conf)
//
// TLA+ Expected(V3) \ Expected(V2) = {npmrc}
// TLA+ Expected(V2) \ Expected(V3) = {} (nothing removed)
// ═══════════════════════════════════════════════════════════════════════════════

func migrateUp_0_2_0_to_0_3_0(ui *UI, r *Runner) error {
	ui.Step("Migration: v0.2.0 → v0.3.0 (supply chain hardening)")

	// npmrc and pip.conf are added by runBootstrap's supply chain step
	// in the normal init flow. No migration action needed — the idempotent
	// init step will create them if missing.
	ui.Ok("Supply chain hardening will be applied by init")
	return nil
}

func migrateDown_0_3_0_to_0_2_0(ui *UI, r *Runner) error {
	ui.Step("Reverse migration: v0.3.0 → v0.2.0")

	// Remove npmrc.
	npmrc := agentHome + "/.npmrc"
	if _, err := os.Stat(npmrc); err == nil {
		if err := r.Sudo("remove agent .npmrc", "rm", "-f", npmrc); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", npmrc, err))
		} else {
			ui.Ok("Removed " + npmrc)
		}
	}

	// Remove pip.conf.
	pipConf := agentHome + "/.config/pip/pip.conf"
	if _, err := os.Stat(pipConf); err == nil {
		if err := r.Sudo("remove agent pip.conf", "rm", "-f", pipConf); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", pipConf, err))
		} else {
			ui.Ok("Removed " + pipConf)
		}
	}

	return nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// Migration dispatch
// ═══════════════════════════════════════════════════════════════════════════════

// runMigrations applies all pending forward migrations. Called by runInit
// before the normal idempotent setup steps.
func runMigrations(ui *UI, r *Runner) error {
	state, err := loadState()
	if err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not read state file: %v (treating as fresh install)", err))
		return nil
	}

	if state.InitVersion == "" {
		// No state file — either fresh install or pre-migration install.
		// Detect pre-migration installs by checking for v0.1.0 artifacts.
		if detectV010Artifacts() {
			state.InitVersion = "0.1.0"
			cDim.Println("  Detected v0.1.0 artifacts — will migrate.")
		} else {
			return nil // truly fresh install, no migration needed
		}
	}

	chain := pendingMigrations(state.InitVersion, version)
	if len(chain) == 0 {
		return nil
	}

	fmt.Println()
	cBold.Printf("  Migrating from v%s to v%s (%d step(s))\n", state.InitVersion, version, len(chain))
	fmt.Println()

	for _, m := range chain {
		if err := m.Up(ui, r); err != nil {
			return fmt.Errorf("migration %s→%s failed: %w", m.From, m.To, err)
		}
		// Record progress after each step so partial migration can resume.
		if err := saveState(m.To); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not save migration state: %v", err))
		}
	}

	return nil
}

// detectV010Artifacts checks for artifacts that only existed in v0.1.0
// (workspace symlink in agent home, workspace ACL).
func detectV010Artifacts() bool {
	// Check for agent workspace symlink (only in v0.1.0).
	agentLink := agentHome + "/workspace"
	if info, err := os.Lstat(agentLink); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	// Check for workspace with dev group ownership (set by v0.1.0 init).
	home := os.Getenv("HOME")
	workspaceDir := home + "/workspace"
	if pathHasDevACL(workspaceDir, false) {
		return true
	}
	return false
}

// runDownMigrations applies reverse migrations during rollback.
// Removes artifacts from newer versions that the rollback code doesn't
// know about natively.
func runDownMigrations(ui *UI, r *Runner) {
	state, _ := loadState()
	if state.InitVersion == "" {
		return
	}

	// Apply down migrations in reverse order from current version.
	ver := state.InitVersion
	for {
		// Find the migration that brought us TO this version.
		found := false
		for _, m := range migrations {
			if m.To == ver {
				if err := m.Down(ui, r); err != nil {
					ui.WarnMsg(fmt.Sprintf("Reverse migration %s→%s: %v", m.To, m.From, err))
				}
				ver = m.From
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	// Remove state file.
	os.Remove(stateFilePath)
}
