package main

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

// ── Embedded content ──────────────────────────────────────────────────────────

// seatbeltWrapperContent is the Claude launch wrapper installed at
// seatbeltWrapperPath. Hazmat prepares it during init so the Claude harness can
// be added later without rewriting the base shell environment.
//
// It is aliased to `claude` inside agent-shell sessions. The outer
// sandbox-exec confinement applied by `hazmat shell/exec/claude` already
// covers the session, so this wrapper simply execs the claude binary directly.
const seatbeltWrapperContent = `#!/bin/bash
# claude-sandboxed — launch Claude Code inside the agent sandbox.
# Installed by hazmat init — do not edit manually.
#
# This wrapper is aliased to "claude" in the agent shell. It runs inside a
# The session is already confined by sandbox-exec (started via "hazmat shell"
# or "hazmat claude"), so no additional seatbelt policy is applied here.
set -euo pipefail

CLAUDE_BIN=/Users/agent/.local/bin/claude

if [[ ! -x "$CLAUDE_BIN" ]]; then
    printf 'error: claude binary not found: %s\n' "$CLAUDE_BIN" >&2
    exit 1
fi

exec "$CLAUDE_BIN" "$@"
`

// ── Command ───────────────────────────────────────────────────────────────────

const initBootstrapSkip = "skip"

func newInitCmd() *cobra.Command {
	var agentUIDFlag, sharedGIDFlag, bootstrapAgentFlag string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up containment and optionally bootstrap an AI coding agent",
		Long: `One command to go from fresh macOS to contained agent execution on your Mac.

Creates a dedicated agent user, workspace ACLs, pf port blocklist, DNS
blocklist, seatbelt profile, and automatic snapshots. You can optionally
bootstrap a supported AI coding agent during setup, or skip that step and
install one later.

After init completes:   cd your-project && hazmat shell

Interactive by default — prompts for confirmation before making changes.

  hazmat init                                 # Interactive (recommended)
  hazmat init --bootstrap-agent claude        # Also install Claude Code
  hazmat init --bootstrap-agent codex         # Also install Codex
  hazmat init --bootstrap-agent gemini        # Also install Gemini CLI
  hazmat init --yes                           # Non-interactive; install maintenance sudoers by default
  hazmat check                                # Verify the setup
  hazmat rollback                             # Undo everything
  hazmat config agent                         # Configure agent API keys + git identity
  hazmat init cloud                           # Configure S3 cloud backup

Use --dry-run to preview all commands without executing anything.`,
	}
	cmd.Flags().StringVar(&agentUIDFlag, "agent-uid", "",
		"Override UID for the agent user (default: 599; use when 599 is already taken)")
	cmd.Flags().StringVar(&sharedGIDFlag, "group-gid", "",
		"Override GID for the dev group (default: 599; use when 599 is already taken)")
	cmd.Flags().StringVar(&bootstrapAgentFlag, "bootstrap-agent", "",
		"Optional AI coding agent to install during init: skip, claude, codex, opencode")
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if agentUIDFlag != "" {
			agentUID = agentUIDFlag
		}
		if sharedGIDFlag != "" {
			sharedGID = sharedGIDFlag
		}
		return runInit(c, args, bootstrapAgentFlag)
	}
	return cmd
}

// newInitCheckCmd creates the `hazmat check` command.
func newInitCheckCmd() *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Verify the setup is working",
		Long: `Runs the verification suite to check that containment is correctly configured.

By default runs quick checks (no network traffic). Use --full to include
live network probes that verify firewall rules are active.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTest(!full) // runTest(quick bool): true = skip network
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "Include live network probes (sends external traffic)")
	return cmd
}

// newInitCloudCmd wraps cloud setup as `hazmat init cloud`.
func newInitCloudCmd() *cobra.Command {
	return newConfigCloudCmd()
}

// newStatusCmd shows a progress checklist and optionally runs health checks.
func newStatusCmd() *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show setup progress and health check",
		Long: `Shows which setup phases are complete and what to do next.
Use --full to run the complete health check suite (same as 'hazmat check --quick').`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runStatus(full)
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "Run full health checks (same as 'hazmat check --quick')")
	return cmd
}

func runStatus(full bool) error {
	fmt.Println()
	cBold.Println("  Hazmat — AI agent containment for macOS")
	fmt.Println()

	containmentConfigured := func() bool {
		if _, err := user.Lookup(agentUser); err != nil {
			return false
		}
		if _, err := os.Stat(sudoersFile); err != nil {
			return false
		}
		if _, err := os.Stat(pfAnchorFile); err != nil {
			return false
		}
		return true
	}
	installedHarnesses := installedManagedHarnesses()
	claudeConfigured := func() bool {
		value, _, err := lookupConfiguredAPIKey(harnessAPIKeyPrompts[0])
		return err == nil && value != ""
	}

	allDone := containmentConfigured()
	if allDone {
		cGreen.Printf("  [✓] %-24s %s\n", "Containment configured", "hazmat init")
	} else {
		cYellow.Printf("  [→] %-24s %s   ◀ next\n", "Containment configured", "hazmat init")
	}

	if len(installedHarnesses) == 0 {
		cDim.Printf("  [ ] %-24s %s\n", "Agent harness installed", "hazmat bootstrap claude|codex|opencode")
	} else {
		var names []string
		for _, harness := range installedHarnesses {
			names = append(names, harness.Spec.DisplayName)
		}
		cGreen.Printf("  [✓] %-24s %s\n", "Agent harness installed", strings.Join(names, ", "))
	}

	if isManagedHarnessInstalled(HarnessClaude) {
		if claudeConfigured() {
			cGreen.Printf("  [✓] %-24s %s\n", "Claude credentials set", "hazmat config agent")
		} else {
			cYellow.Printf("  [→] %-24s %s\n", "Claude credentials set", "hazmat config agent")
		}
	} else {
		cDim.Printf("  [ ] %-24s %s\n", "Claude credentials set", "optional; needed only for hazmat claude")
	}

	fmt.Println()
	if allDone {
		if len(installedHarnesses) > 0 {
			fmt.Println("  Quick start:")
			fmt.Printf("    cd your-project && %s\n", installedHarnesses[0].LaunchCommand)
		} else {
			fmt.Println("  Core containment is ready.")
			fmt.Println("  Install a harness when needed:")
			fmt.Println("    hazmat bootstrap claude")
			fmt.Println("    hazmat bootstrap codex")
			fmt.Println("    hazmat bootstrap opencode")
			fmt.Println("  Or run contained commands directly:")
			fmt.Println("    cd your-project && hazmat shell")
		}
	} else {
		fmt.Println("  Next step: hazmat init")
	}
	fmt.Println()

	if full {
		ui := &UI{}
		verifySetup(ui)
		if ui.Summary() {
			return fmt.Errorf("health check found failures")
		}
	}

	return nil
}

// runInit is the top-level entry point.  Named return so defer can inspect
// retErr and print the rollback hint — equivalent to shell's "trap ... ERR".
func initBootstrapChoices() []UIChoice {
	choices := []UIChoice{
		{
			Key:         initBootstrapSkip,
			Label:       "Skip harness install",
			Description: "Set up containment only. Install a coding agent later with 'hazmat bootstrap ...'.",
		},
	}
	for _, harness := range managedHarnesses() {
		choices = append(choices, UIChoice{
			Key:         string(harness.Spec.ID),
			Label:       "Install " + harness.Spec.DisplayName,
			Description: fmt.Sprintf("Bootstrap %s during init and make it ready for 'cd your-project && %s'.", harness.Spec.DisplayName, harness.LaunchCommand),
		})
	}
	return choices
}

func normalizeInitBootstrapAgent(selection string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(selection))
	switch normalized {
	case "", initBootstrapSkip:
		return initBootstrapSkip, nil
	}
	if _, ok := managedHarnessByID(HarnessID(normalized)); ok {
		return normalized, nil
	}
	return "", fmt.Errorf("invalid --bootstrap-agent %q (expected skip, claude, codex, opencode, or gemini)", selection)
}

func resolveInitBootstrapAgent(ui *UI, flagValue string) (string, error) {
	if flagValue != "" {
		return normalizeInitBootstrapAgent(flagValue)
	}
	if !ui.IsInteractive() {
		return initBootstrapSkip, nil
	}
	fmt.Println()
	cBold.Println("  Optional AI coding agent bootstrap")
	fmt.Println("  Pick a harness to install now, or skip and install one later.")
	selection, err := ui.Choose("Select a harness [1-4, Enter for default]:", initBootstrapChoices(), initBootstrapSkip)
	if err != nil {
		return "", err
	}
	return selection, nil
}

func runInitSelectedBootstrap(ui *UI, r *Runner, selection string) error {
	if selection == initBootstrapSkip {
		fmt.Println()
		cDim.Println("  Skipping optional AI coding agent bootstrap.")
		return nil
	}
	harness, ok := managedHarnessByID(HarnessID(selection))
	if !ok {
		return fmt.Errorf("unsupported harness %q", selection)
	}
	return harness.Bootstrap(ui, r)
}

func runInit(_ *cobra.Command, _ []string, bootstrapAgentFlag string) (retErr error) {
	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	r := NewRunner(ui, flagVerbose, flagDryRun)

	if err := checkPlatform(); err != nil {
		ui.Fatal(err.Error())
	}

	cu, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}
	if cu.Username == "root" {
		ui.Fatal("Run as your normal user, not root. The tool uses sudo where needed.")
	}

	ui.Banner(cu.Username)

	if flagDryRun {
		cYellow.Println("  ────────────────────────────────────────────────────")
		cYellow.Println("  DRY RUN — no changes will be made.")
		cYellow.Println("  Shows all commands regardless of current system state.")
		cYellow.Println("  Steps already complete are skipped when run for real.")
		cYellow.Println("  ────────────────────────────────────────────────────")
		fmt.Println()
	} else if !ui.YesAll && !ui.IsInteractive() {
		// Non-TTY without --yes: fail fast with an actionable message rather
		// than silently aborting after the Ask prompt returns false.
		return fmt.Errorf("stdin is not a terminal\nFor non-interactive setup run: hazmat init --yes")
	} else if !ui.Ask("Proceed with setup?") {
		fmt.Println("  Aborted.")
		return nil
	}

	bootstrapSelection, err := resolveInitBootstrapAgent(ui, bootstrapAgentFlag)
	if err != nil {
		return err
	}

	defer func() {
		if retErr != nil && !flagDryRun {
			fmt.Fprintln(os.Stderr)
			cRed.Fprintln(os.Stderr, "Setup interrupted — some steps may be incomplete.")
			fmt.Fprintln(os.Stderr, "See setup-option-a.md § Uninstall / Rollback")
		}
	}()

	if !flagDryRun {
		if err := preflightChecks(cu.Username); err != nil {
			return err
		}
	}

	// ── Migrations: upgrade from older init versions ─────────────────────
	// Must run BEFORE normal setup steps so old artifacts are cleaned up
	// before new ones are created. The TLA+ spec (MC_Migration) proves
	// this preserves AgentContained across all 44,795 reachable states.
	if !flagDryRun {
		if err := runMigrations(ui, r); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	if err := runInitSetupSteps(initStepContext{
		ui:                 ui,
		runner:             r,
		currentUser:        cu.Username,
		bootstrapSelection: bootstrapSelection,
	}); err != nil {
		return err
	}

	// ── Optional import: portable harness basics ────────────────────────────
	// Symmetric across all four supported harnesses — whichever one the user
	// just bootstrapped gets the same one-shot offer to copy their existing
	// host credentials/settings into the agent home.
	_ = offerHarnessBasicsImport(ui, r, bootstrapSelection)

	if !flagDryRun {
		// Record the version so future inits can detect and migrate.
		if err := saveState(version); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not save init state: %v", err))
		}
		verifySetup(ui)
		ui.Logo()
	}

	// If cloud backup was configured during this init, remind the user
	// to save the encryption password — it's easy to miss mid-flow.
	cfg, _ := loadConfig()
	if cfg.Backup.Cloud != nil && cfg.Backup.Cloud.RecoveryKey != "" {
		fmt.Println()
		cYellow.Println("  ┌──────────────────────────────────────────────────┐")
		cYellow.Println("  │  SAVE YOUR CLOUD BACKUP RECOVERY KEY             │")
		cYellow.Println("  └──────────────────────────────────────────────────┘")
		fmt.Println()
		fmt.Printf("    %s\n", cfg.Backup.Cloud.RecoveryKey)
		fmt.Println()
		cYellow.Println("  You need this key to restore from cloud backup.")
		cYellow.Println("  It cannot be recovered if lost.")
		fmt.Println()
	}

	cGreen.Println("━━━ Setup complete ━━━")
	fmt.Println()
	fmt.Println("  Ready to use:")
	switch bootstrapSelection {
	case string(HarnessClaude), string(HarnessCodex), string(HarnessOpenCode), string(HarnessGemini):
		fmt.Printf("    cd your-project && hazmat %s\n", bootstrapSelection)
	default:
		fmt.Println("    cd your-project && hazmat shell")
		fmt.Println("    hazmat bootstrap claude|codex|opencode|gemini")
	}
	fmt.Println()
	fmt.Println("  Check status:   hazmat status")
	switch bootstrapSelection {
	case string(HarnessClaude), string(HarnessCodex), string(HarnessOpenCode), string(HarnessGemini):
		fmt.Println("  Update creds:   hazmat config agent")
		fmt.Printf("  Import basics:  hazmat config import %s\n", bootstrapSelection)
	default:
		fmt.Println("  Install agent:  hazmat bootstrap claude|codex|opencode|gemini")
	}
	fmt.Println("  View config:    hazmat config")
	fmt.Println("  Uninstall:      hazmat rollback")
	fmt.Println()
	return nil
}

// offerHarnessBasicsImport runs the per-harness 'config import' interactive
// flow as part of init, when the user just bootstrapped one of the supported
// harnesses. Cancellation by the user is non-fatal (init already produced a
// working harness install); other errors print a yellow advisory but never
// abort init.
//
// Returns true when the harness ID matched a dispatch case (regardless of
// whether the user accepted or skipped the import). Returns false for
// "skip" or any unrecognised value — used by tests to assert dispatch
// coverage across every managed harness.
//
// Each harness has its own env / options / cancel-error types, so we dispatch
// on bootstrapSelection rather than threading a polymorphic interface.
func offerHarnessBasicsImport(ui *UI, r *Runner, bootstrapSelection string) bool {
	switch bootstrapSelection {
	case string(HarnessClaude):
		env, err := defaultClaudeImportEnv()
		if err != nil {
			return true
		}
		if err := claudeCodeHarness.ImportBasics(ui, r, env, claudeImportOptions{
			PromptBeforeImport: true,
			ConflictPolicy:     claudeConflictPrompt,
			AllowNoopMessage:   false,
		}); err != nil && !errors.Is(err, errClaudeImportCancelled) {
			cYellow.Printf("\n  Claude basics import skipped: %v\n", err)
			fmt.Println("  Run 'hazmat config import claude' later to retry.")
		}
		return true
	case string(HarnessCodex):
		env, err := defaultCodexImportEnv()
		if err != nil {
			return true
		}
		if err := codexHarness.ImportBasics(ui, r, env, codexImportOptions{
			PromptBeforeImport: true,
			ConflictPolicy:     claudeConflictPrompt,
			AllowNoopMessage:   false,
		}); err != nil && !errors.Is(err, errCodexImportCancelled) {
			cYellow.Printf("\n  Codex basics import skipped: %v\n", err)
			fmt.Println("  Run 'hazmat config import codex' later to retry.")
		}
		return true
	case string(HarnessOpenCode):
		env, err := defaultOpenCodeImportEnv()
		if err != nil {
			return true
		}
		if err := openCodeHarness.ImportBasics(ui, r, env, opencodeImportOptions{
			PromptBeforeImport: true,
			ConflictPolicy:     claudeConflictPrompt,
			AllowNoopMessage:   false,
		}); err != nil && !errors.Is(err, errOpenCodeImportCancelled) {
			cYellow.Printf("\n  OpenCode basics import skipped: %v\n", err)
			fmt.Println("  Run 'hazmat config import opencode' later to retry.")
		}
		return true
	case string(HarnessGemini):
		env, err := defaultGeminiImportEnv()
		if err != nil {
			return true
		}
		if err := geminiHarness.ImportBasics(ui, r, env, geminiImportOptions{
			PromptBeforeImport: true,
			ConflictPolicy:     claudeConflictPrompt,
			AllowNoopMessage:   false,
		}); err != nil && !errors.Is(err, errGeminiImportCancelled) {
			cYellow.Printf("\n  Gemini basics import skipped: %v\n", err)
			fmt.Println("  Run 'hazmat config import gemini' later to retry.")
		}
		return true
	}
	return false
}

// offerHarnessBasicsImportCovers reports whether offerHarnessBasicsImport has
// a dispatch case for the named selection without invoking the import flow.
// Used for static coverage assertions in tests.
func offerHarnessBasicsImportCovers(bootstrapSelection string) bool {
	switch bootstrapSelection {
	case string(HarnessClaude), string(HarnessCodex), string(HarnessOpenCode), string(HarnessGemini):
		return true
	}
	return false
}

// ── Step 1: Agent user ────────────────────────────────────────────────────────

func setupAgentUser(ui *UI, r *Runner) error {
	return nativeAccountBackendForHost().SetupAgentUser(ui, r)
}

// ── Step 2: Dev group ─────────────────────────────────────────────────────────

func setupDevGroup(ui *UI, r *Runner, currentUser string) error {
	return nativeAccountBackendForHost().SetupDevGroup(ui, r, currentUser)
}

// ── Step 3: Workspace root ────────────────────────────────────────────────────

// setupHomeDirTraverse grants the agent user directory traversal on $HOME
// so it can reach project directories anywhere under the user's home.
// This is a one-time ACL on the home directory itself — it does NOT grant
// read or list access, only execute (traverse).
func setupHomeDirTraverse(ui *UI, r *Runner) error {
	ui.Step("Allow agent to traverse home directory")

	homeDir := os.Getenv("HOME")
	if homeAllowsAgentTraverse(homeDir) {
		if homeHasAgentTraverseACL(homeDir) {
			ui.SkipDone("Home directory ACL already allows agent traversal")
		} else {
			ui.SkipDone("Home directory permissions already allow agent traversal")
		}
	} else {
		inv := sudoACLInvoker{runner: r, reason: "allow agent to traverse home directory"}
		if err := ensureACL(inv, homeDir, agentTraverseGrant); err != nil {
			return fmt.Errorf("set home traversal ACL: %w", err)
		}
		ui.Ok("Home directory ACL set — agent can reach project directories")
	}
	return nil
}

// ── Step 3b: Backup scope file ────────────────────────────────────────────────

func setupLocalRepo(ui *UI) error {
	ui.Step("Configure snapshot backup")

	// Write config.yaml with defaults if it doesn't exist yet.
	cfg, _ := loadConfig()
	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		if !flagDryRun {
			if err := saveConfig(cfg); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
		}
	}

	if _, err := os.Stat(localConfigFile); err == nil {
		ui.SkipDone(fmt.Sprintf("Snapshot repository already configured at %s", localRepoDir))
	} else if flagDryRun {
		faint.Printf("    $ kopia repository create filesystem --path %s\n", localRepoDir)
	} else {
		if err := initLocalRepo(); err != nil {
			return fmt.Errorf("initialize snapshot repo: %w", err)
		}
		ui.Ok(fmt.Sprintf("Snapshot repository created at %s", localRepoDir))
	}

	printBackupConfig(cfg)

	// Offer cloud setup if interactive, not already configured, and not --yes.
	if cfg.Backup.Cloud == nil && !flagDryRun && !flagYesAll {
		innerUI := &UI{}
		if innerUI.IsInteractive() {
			if innerUI.Ask("Set up cloud backup (S3-compatible)?") {
				if err := runConfigCloud("", "", "", false); err != nil {
					// Non-fatal: user can configure later.
					cYellow.Printf("\n    Cloud setup skipped: %v\n", err)
					fmt.Println("    Configure later: hazmat config cloud")
				}
			}
		}
	}

	return nil
}

func printBackupConfig(cfg HazmatConfig) {
	fmt.Println()
	cDim.Println("    Snapshots are taken automatically before each session.")
	fmt.Println()
	cDim.Printf("    Repository:  %s\n", cfg.Backup.Local.Path)
	cDim.Printf("    Retention:   %d latest, %d daily, %d weekly\n",
		cfg.Backup.Local.Retention.KeepLatest,
		cfg.Backup.Local.Retention.KeepDaily,
		cfg.Backup.Local.Retention.KeepWeekly)
	cDim.Printf("    Excludes:    node_modules/ .venv/ dist/ build/ target/ ...\n")
	if cfg.Backup.Cloud != nil {
		cDim.Printf("    Cloud:       %s/%s\n", cfg.Backup.Cloud.Endpoint, cfg.Backup.Cloud.Bucket)
	}
	cDim.Printf("    Config:      %s\n", configFilePath)
	fmt.Println()
}

// ── Step 4: Hardening gaps ────────────────────────────────────────────────────

func setupHardeningGaps(ui *UI, r *Runner) error {
	ui.Step("Harden known macOS isolation gaps")

	// Docker socket — owned by current user, no sudo needed.
	dockerSock := os.Getenv("HOME") + "/.docker/run/docker.sock"
	if info, err := os.Stat(dockerSock); err == nil && info.Mode()&os.ModeSocket != 0 {
		current := info.Mode().Perm()
		if current == 0o700 {
			ui.SkipDone("Docker socket already restricted (700)")
		} else {
			if err := r.Chmod(dockerSock, 0o700); err != nil {
				return fmt.Errorf("chmod docker socket: %w", err)
			}
			ui.Ok(fmt.Sprintf("Docker socket restricted to owner only (was %04o)", current))
		}
	} else {
		ui.SkipDone("Docker socket not found (Docker Desktop not running or not installed)")
	}

	// Restrictive umask for agent user — use a managed block so rollback is precise.
	agentZshrc := agentHome + "/.zshrc"
	agentZshrcData, _ := r.SudoOutput("cat", agentZshrc)
	if strings.Contains(agentZshrcData, umaskBlockStart) {
		ui.SkipDone("umask 007 already set in agent's .zshrc")
	} else {
		updated := upsertManagedBlock(agentZshrcData, umaskBlockStart, umaskBlockEnd, "umask 007")
		if err := r.SudoWriteFile("write agent umask to .zshrc", agentZshrc, updated); err != nil {
			return fmt.Errorf("set umask in agent .zshrc: %w", err)
		}
		if err := r.Sudo("set agent .zshrc ownership", "chown", agentUser+":staff", agentZshrc); err != nil {
			return fmt.Errorf("chown agent .zshrc: %w", err)
		}
		ui.Ok("Set umask 007 in agent's .zshrc")
	}

	// Restrictive umask for current user — leave the host shell untouched.
	ui.SkipDone("Host shell umask left unchanged")

	return nil
}

// ── Step 5: Seatbelt wrapper ──────────────────────────────────────────────────

func setupSeatbelt(ui *UI, r *Runner) error {
	ui.Step("Install Claude compatibility wrapper")

	// Create the config dir (used by agentEnvPath) and the bin dir.
	if err := r.Sudo("create seatbelt config directory",
		"install", "-d", "-o", agentUser, "-g", "staff", "-m", "755", seatbeltProfileDir); err != nil {
		return fmt.Errorf("ensure %s: %w", seatbeltProfileDir, err)
	}

	wrapperDir := agentHome + "/.local/bin"
	if err := r.Sudo("create agent bin directory",
		"install", "-d", "-o", agentUser, "-g", "staff", "-m", "755", wrapperDir); err != nil {
		return fmt.Errorf("ensure %s: %w", wrapperDir, err)
	}

	// The wrapper is a managed artifact; re-write on every run so setup
	// doubles as an upgrade path.
	if err := r.SudoWriteFile("install seatbelt wrapper", seatbeltWrapperPath, seatbeltWrapperContent); err != nil {
		return fmt.Errorf("write seatbelt wrapper: %w", err)
	}
	if err := r.Sudo("set seatbelt wrapper ownership", "chown", agentUser+":staff", seatbeltWrapperPath); err != nil {
		return fmt.Errorf("chown seatbelt wrapper: %w", err)
	}
	if err := r.Sudo("make seatbelt wrapper executable", "chmod", "755", seatbeltWrapperPath); err != nil {
		return fmt.Errorf("chmod seatbelt wrapper: %w", err)
	}
	ui.Ok(fmt.Sprintf("Seatbelt wrapper installed at %s", seatbeltWrapperPath))

	return nil
}

func setupUserExperience(ui *UI, r *Runner) error {
	ui.Step("Install command wrappers and toolchain env")

	for _, dir := range []string{
		defaultAgentCacheHome,
		defaultAgentDataHome,
		agentHome + "/.npm",
	} {
		if err := r.Sudo("create agent directory",
			"install", "-d", "-o", agentUser, "-g", "staff", "-m", "755", dir); err != nil {
			return fmt.Errorf("ensure %s: %w", dir, err)
		}
	}

	if err := r.SudoWriteFile("write agent toolchain env", agentEnvPath, agentEnvContent()); err != nil {
		return fmt.Errorf("write agent env file: %w", err)
	}
	if err := r.Sudo("set agent env file ownership", "chown", agentUser+":staff", agentEnvPath); err != nil {
		return fmt.Errorf("chown agent env file: %w", err)
	}
	if err := r.Sudo("set agent env file permissions", "chmod", "644", agentEnvPath); err != nil {
		return fmt.Errorf("chmod agent env file: %w", err)
	}
	ui.Ok(fmt.Sprintf("Agent toolchain env written to %s", agentEnvPath))

	agentZshrc := agentHome + "/.zshrc"
	agentZshrcData, _ := r.SudoOutput("cat", agentZshrc)
	updatedAgentZshrc := upsertManagedBlock(agentZshrcData,
		agentShellBlockStart,
		agentShellBlockEnd,
		`[[ -f "$HOME/.config/hazmat/agent-env.zsh" ]] && source "$HOME/.config/hazmat/agent-env.zsh"`,
	)
	if err := r.SudoWriteFile("write agent shell bootstrap to .zshrc", agentZshrc, updatedAgentZshrc); err != nil {
		return fmt.Errorf("update %s: %w", agentZshrc, err)
	}
	if err := r.Sudo("set agent .zshrc ownership", "chown", agentUser+":staff", agentZshrc); err != nil {
		return fmt.Errorf("chown %s: %w", agentZshrc, err)
	}
	ui.Ok(fmt.Sprintf("Agent shell bootstraps %s", agentEnvPath))

	if err := r.MkdirAll(hostWrapperDir(), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", hostWrapperDir(), err)
	}

	hazmatBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve hazmat binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(hazmatBin); err == nil {
		hazmatBin = resolved
	}

	wrappers := []struct {
		name       string
		subcommand string
	}{
		{name: hostClaudeWrapperName, subcommand: "claude"},
		{name: hostExecWrapperName, subcommand: "exec"},
		{name: hostShellWrapperName, subcommand: "shell"},
	}
	for _, wrapper := range wrappers {
		path := hostWrapperPath(wrapper.name)
		if err := r.UserWriteFile(path, hostWrapperContent(hazmatBin, wrapper.subcommand)); err != nil {
			return fmt.Errorf("write wrapper %s: %w", path, err)
		}
		if err := r.Chmod(path, 0o755); err != nil {
			return fmt.Errorf("chmod %s: %w", path, err)
		}
		ui.Ok(fmt.Sprintf("Installed host wrapper %s", path))
	}

	profile, ok := currentUserShellProfile()
	if !ok {
		ui.WarnMsg(fmt.Sprintf("Shell %q is not auto-configured — add %s to your PATH manually", filepath.Base(os.Getenv("SHELL")), hostWrapperDir()))
		return nil
	}

	userRCData, _ := os.ReadFile(profile.rcPath)
	if strings.Contains(string(userRCData), userPathBlockStart) {
		ui.SkipDone(fmt.Sprintf("%s already has a hazmat PATH block", profile.rcPath))
		return nil
	}

	updatedUserRC := upsertManagedBlock(string(userRCData),
		userPathBlockStart,
		userPathBlockEnd,
		profile.pathBlockLines...,
	)
	if err := r.MkdirAll(filepath.Dir(profile.rcPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(profile.rcPath), err)
	}
	if err := r.UserWriteFile(profile.rcPath, updatedUserRC); err != nil {
		return fmt.Errorf("update %s: %w", profile.rcPath, err)
	}
	ui.Ok(fmt.Sprintf("Added %s PATH block to %s", hostWrapperDir(), profile.rcPath))

	return nil
}

// ── Step 5b: Install sandbox-launch helper ────────────────────────────────────

// setupLaunchHelper verifies that the sandbox-launch helper is installed and
// accessible at the path Hazmat will invoke at runtime.
//
// The helper is built separately via 'make install' (user-local) or
// 'sudo make install-system' (system-wide). Setup does not build it
// automatically — the user must have installed Hazmat before 'hazmat init'.
// If the helper is absent, setup prints clear instructions and fails so the
// sudoers step is never reached with an incorrect path.
func setupLaunchHelper(ui *UI, r *Runner) error {
	return nativeServiceBackendForHost().SetupLaunchHelper(ui, r)
}

// findBrewLaunchHelper returns the path to hazmat-launch inside a Homebrew
// installation, or "" if not found.
func findBrewLaunchHelper() string {
	return nativeServiceBackendForHost().FindBrewLaunchHelper()
}

// ── Step 6: Passwordless sudo ─────────────────────────────────────────────────

func setupSudoers(ui *UI, r *Runner, currentUser string) error {
	return nativeServiceBackendForHost().SetupSudoers(ui, r, currentUser)
}

// ── Step 6: pf firewall ───────────────────────────────────────────────────────

func setupPfFirewall(ui *UI, r *Runner) error {
	return nativeServiceBackendForHost().SetupPfFirewall(ui, r)
}

// ── Step 7: DNS blocklist ─────────────────────────────────────────────────────

func setupDNSBlocklist(ui *UI, r *Runner) error {
	return nativeServiceBackendForHost().SetupDNSBlocklist(ui, r)
}

// ── Step 8: LaunchDaemon ──────────────────────────────────────────────────────

func setupLaunchDaemon(ui *UI, r *Runner) error {
	return nativeServiceBackendForHost().SetupLaunchDaemon(ui, r)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// preflightChecks validates that all prerequisites are met before making any
// system changes. This catches problems like missing hazmat-launch or UID
// conflicts before setup modifies 13 system files.
func preflightChecks(currentUser string) error {
	type check struct {
		label string
		fn    func() error
	}

	checks := []check{
		{"macOS detected", checkPlatform},
		{"hazmat-launch installed", func() error {
			helperPath := launchHelperPath()
			if info, err := os.Stat(helperPath); err == nil {
				if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
					return fmt.Errorf("%s is not an executable file", helperPath)
				}
				return nil
			}
			// Not at the final path — check if brew has it.
			if helperPath == systemLaunchHelper && findBrewLaunchHelper() != "" {
				return nil // setupLaunchHelper will install it
			}
			return fmt.Errorf("%s not found\n\n"+
				"Install Hazmat before running setup:\n\n"+
				"  make install\n\n"+
				"Or install system-wide:\n\n"+
				"  sudo make install-system", helperPath)
		}},
		{"UID " + agentUID + " available", func() error {
			if _, err := user.Lookup(agentUser); err == nil {
				return nil // agent user already exists — fine
			}
			if taken, err := uidTaken(agentUID); err != nil {
				return fmt.Errorf("cannot check UID: %w", err)
			} else if taken {
				return fmt.Errorf("UID %s is already taken — use: hazmat init --agent-uid <different-uid>", agentUID)
			}
			return nil
		}},
		{"GID " + sharedGID + " available", func() error {
			if _, err := user.LookupGroup(sharedGroup); err == nil {
				return nil // group already exists — fine
			}
			if taken, err := gidTaken(sharedGID); err != nil {
				return fmt.Errorf("cannot check GID: %w", err)
			} else if taken {
				return fmt.Errorf("GID %s is already taken — use: hazmat init --group-gid <different-gid>", sharedGID)
			}
			return nil
		}},
	}

	fmt.Println("  Pre-flight checks:")
	for _, c := range checks {
		if err := c.fn(); err != nil {
			cRed.Printf("  ✗ %s\n", c.label)
			return fmt.Errorf("pre-flight check failed: %w", err)
		}
		cGreen.Printf("  ✓ %s\n", c.label)
	}
	fmt.Println()
	return nil
}

func uidTaken(uid string) (bool, error) {
	return nativeAccountBackendForHost().UIDTaken(uid)
}

func gidTaken(gid string) (bool, error) {
	return nativeAccountBackendForHost().GIDTaken(gid)
}

func ensureAgentCanTraverseLaunchHelper(r *Runner, helperPath string) error {
	inv := sudoACLInvoker{runner: r, reason: "allow agent to traverse launch-helper directory"}
	for _, path := range pendingLaunchHelperTraverseTargets(helperPath) {
		if homeAllowsAgentTraverse(path) {
			continue
		}
		if err := ensureACL(inv, path, agentTraverseGrant); err != nil {
			return fmt.Errorf("set launch-helper traversal ACL on %s: %w", path, err)
		}
	}
	return nil
}

// homeHasAgentTraverseACL reports whether the home directory carries an
// agent-traverse ACL. A row for the agent user must both satisfy the
// grant's principal/kind/inherit contract and grant the execute permission
// (macOS renders this as "search" on directory ACLs).
func homeHasAgentTraverseACL(homeDir string) bool {
	rows, err := readACLs(homeDir)
	if err != nil {
		return false
	}
	for _, r := range rows {
		if r.Satisfies(agentTraverseGrant) && r.GrantsPerm("execute") {
			return true
		}
	}
	return false
}

func homeAllowsAgentTraverse(homeDir string) bool {
	if homeHasAgentTraverseACL(homeDir) {
		return true
	}

	info, err := os.Stat(homeDir)
	if err != nil {
		return false
	}
	if info.Mode().Perm()&0o001 != 0 {
		return true
	}
	if info.Mode().Perm()&0o010 == 0 {
		return false
	}

	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	group, err := user.LookupGroupId(fmt.Sprintf("%d", st.Gid))
	if err != nil {
		return false
	}
	member, err := groupMembershipContains(group.Name, agentUser)
	if err != nil {
		return false
	}
	return member
}

func groupMembershipContains(group, username string) (bool, error) {
	return nativeAccountBackendForHost().GroupMembershipContains(group, username)
}
