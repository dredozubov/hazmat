package hazmat

import (
	"fmt"
	"os"
	"path/filepath"

	"hazmat/internal/setup"
)

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

func setupToolingEnv() setup.ToolingEnv {
	return setup.ToolingEnv{
		AgentUser:             agentUser,
		AgentHome:             agentHome,
		SeatbeltProfileDir:    seatbeltProfileDir,
		SeatbeltWrapperPath:   seatbeltWrapperPath,
		SeatbeltWrapper:       seatbeltWrapperContent,
		AgentEnvPath:          agentEnvPath,
		DefaultAgentPath:      defaultAgentPath,
		DefaultAgentCacheHome: defaultAgentCacheHome,
		DefaultAgentDataHome:  defaultAgentDataHome,
		HostWrapperDir:        hostWrapperDir(),
		HostClaudeWrapperName: hostClaudeWrapperName,
		HostExecWrapperName:   hostExecWrapperName,
		HostShellWrapperName:  hostShellWrapperName,
		AgentShellBlockStart:  agentShellBlockStart,
		AgentShellBlockEnd:    agentShellBlockEnd,
		UserPathBlockStart:    userPathBlockStart,
		UserPathBlockEnd:      userPathBlockEnd,
		UmaskBlockStart:       umaskBlockStart,
		UmaskBlockEnd:         umaskBlockEnd,
		ShellName:             filepath.Base(os.Getenv("SHELL")),
		ShellProfiles:         setupShellProfiles(),
	}
}

func setupHardeningEnv() setup.HardeningEnv {
	return setup.HardeningEnv{
		AgentUser:       agentUser,
		AgentHome:       agentHome,
		HostHome:        os.Getenv("HOME"),
		UmaskBlockStart: umaskBlockStart,
		UmaskBlockEnd:   umaskBlockEnd,
	}
}

func setupLocalRepoEnv() setup.LocalRepoEnv {
	return setup.LocalRepoEnv{
		ConfigFilePath:  configFilePath,
		LocalConfigFile: localConfigFile,
		LocalRepoDir:    localRepoDir,
		DryRun:          flagDryRun,
		YesAll:          flagYesAll,
		LoadConfig: func() setup.LocalRepoConfig {
			cfg, _ := loadConfig()
			return localRepoConfigView(cfg)
		},
		SaveConfig: func() error {
			cfg, _ := loadConfig()
			return saveConfig(cfg)
		},
		InitLocalRepo: initLocalRepo,
		PrintConfig:   printBackupConfig,
		PreviewCreateRepo: func(path string) {
			faint.Printf("    $ kopia repository create filesystem --path %s\n", path)
		},
		OfferCloudSetup: offerCloudBackupSetup,
	}
}

func setupShellProfiles() []setup.ShellProfile {
	profiles := supportedUserShellProfiles()
	out := make([]setup.ShellProfile, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, setup.ShellProfile{
			Name:           profile.name,
			RCPath:         profile.rcPath,
			PathBlockLines: append([]string(nil), profile.pathBlockLines...),
		})
	}
	return out
}

func setupLocalRepo(ui *UI) error {
	return setup.SetupLocalRepo(setupLocalRepoEnv(), ui)
}

func setupHardeningGaps(ui *UI, r *Runner) error {
	return setup.SetupHardeningGaps(setupHardeningEnv(), ui, r)
}

func setupHomeDirTraverse(ui *UI, r *Runner) error {
	inv := sudoACLInvoker{runner: r, reason: "allow agent to traverse home directory"}
	return setup.SetupHomeDirTraverse(setup.HomeTraverseEnv{
		HomeDir:             os.Getenv("HOME"),
		AllowsAgentTraverse: homeAllowsAgentTraverse,
		HasAgentTraverseACL: homeHasAgentTraverseACL,
		EnsureAgentTraverseACL: func(path string) error {
			return ensureACL(inv, path, agentTraverseGrant)
		},
	}, ui)
}

func setupSeatbelt(ui *UI, r *Runner) error {
	return setup.SetupSeatbelt(setupToolingEnv(), ui, r)
}

func setupUserExperience(ui *UI, r *Runner) error {
	return setup.SetupUserExperience(setupToolingEnv(), ui, r)
}

func rollbackSeatbelt(ui *UI, r *Runner) {
	setup.RollbackSeatbelt(setupToolingEnv(), ui, r)
}

func rollbackUserExperience(ui *UI, r *Runner) {
	setup.RollbackUserExperience(setupToolingEnv(), ui, r)
}

func rollbackHomeDirTraverse(ui *UI, r *Runner) {
	inv := sudoACLInvoker{runner: r, reason: "remove home directory traverse ACL"}
	setup.RollbackHomeDirTraverse(setup.HomeTraverseEnv{
		HomeDir:             os.Getenv("HOME"),
		HasAgentTraverseACL: homeHasAgentTraverseACL,
		RemoveAgentTraverseACL: func(path string) error {
			return removeACL(inv, path, agentTraverseGrant)
		},
	}, ui)
}

func rollbackLocalRepo(ui *UI) {
	setup.RollbackLocalRepo(setupLocalRepoEnv(), ui)
}

func rollbackUmask(ui *UI, r *Runner) {
	setup.RollbackUmask(setupToolingEnv(), ui, r)
}

func localRepoConfigView(cfg HazmatConfig) setup.LocalRepoConfig {
	view := setup.LocalRepoConfig{
		RepositoryPath: cfg.Backup.Local.Path,
		KeepLatest:     cfg.Backup.Local.Retention.KeepLatest,
		KeepDaily:      cfg.Backup.Local.Retention.KeepDaily,
		KeepWeekly:     cfg.Backup.Local.Retention.KeepWeekly,
	}
	if cfg.Backup.Cloud != nil {
		view.CloudConfigured = true
		view.CloudEndpoint = cfg.Backup.Cloud.Endpoint
		view.CloudBucket = cfg.Backup.Cloud.Bucket
	}
	return view
}

func printBackupConfig(cfg setup.LocalRepoConfig) {
	fmt.Println()
	cDim.Println("    Snapshots are taken automatically before each session.")
	fmt.Println()
	cDim.Printf("    Repository:  %s\n", cfg.RepositoryPath)
	cDim.Printf("    Retention:   %d latest, %d daily, %d weekly\n",
		cfg.KeepLatest,
		cfg.KeepDaily,
		cfg.KeepWeekly)
	cDim.Printf("    Excludes:    node_modules/ .venv/ dist/ build/ target/ ...\n")
	if cfg.CloudConfigured {
		cDim.Printf("    Cloud:       %s/%s\n", cfg.CloudEndpoint, cfg.CloudBucket)
	}
	cDim.Printf("    Config:      %s\n", configFilePath)
	fmt.Println()
}

func offerCloudBackupSetup() {
	innerUI := &UI{}
	if !innerUI.IsInteractive() {
		return
	}
	if !innerUI.Ask("Set up cloud backup (S3-compatible)?") {
		return
	}
	if err := runConfigCloud("", "", "", false); err != nil {
		cYellow.Printf("\n    Cloud setup skipped: %v\n", err)
		fmt.Println("    Configure later: hazmat config cloud")
	}
}
