package setup

import (
	"fmt"
	"os"
	"path/filepath"
)

type GitSafeDirectoryEnv struct {
	SystemGitConfigPath func() string
	ManagedEntries      func() []string
	SyncConfig          func(string, []string) (string, bool)
}

func SetupGitSafeDirectory(env GitSafeDirectoryEnv, ui StepStatusUI, runner ToolingRunner) error {
	ui.Step("Configure git safe.directory for agent user")

	gitconfig := env.systemGitConfigPath()
	if gitconfig == "" {
		ui.WarnMsg("Could not determine system gitconfig path — skipping")
		return nil
	}

	wanted := env.managedEntries()
	if len(wanted) == 0 {
		ui.SkipDone("No session.read_dirs configured — nothing to add")
		return nil
	}

	content, err := os.ReadFile(gitconfig)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", gitconfig, err)
	}

	updated, changed := env.syncConfig(string(content), wanted)
	if !changed {
		ui.SkipDone(fmt.Sprintf("safe.directory already configured for %d workspace root(s)", len(wanted)))
		return nil
	}

	if err := runner.Sudo("create system gitconfig directory", "mkdir", "-p", filepath.Dir(gitconfig)); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(gitconfig), err)
	}
	if err := runner.SudoWriteFile("write hazmat-managed git safe.directory entries", gitconfig, updated); err != nil {
		return fmt.Errorf("write system gitconfig: %w", err)
	}
	if err := runner.Sudo("set system gitconfig permissions", "chmod", "644", gitconfig); err != nil {
		return fmt.Errorf("chmod %s: %w", gitconfig, err)
	}

	for _, entry := range wanted {
		ui.Ok(fmt.Sprintf("safe.directory = %s", entry))
	}
	ui.Ok(fmt.Sprintf("Written to %s", gitconfig))
	return nil
}

func RollbackGitSafeDirectory(env GitSafeDirectoryEnv, ui StepStatusUI, runner ToolingRunner) {
	ui.Step("Remove hazmat-managed git safe.directory entries from system gitconfig")

	gitconfig := env.systemGitConfigPath()
	if gitconfig == "" {
		ui.SkipDone("Could not determine system gitconfig path")
		return
	}

	content, err := os.ReadFile(gitconfig)
	if err != nil {
		ui.SkipDone("System gitconfig not readable")
		return
	}

	updated, changed := env.syncConfig(string(content), nil)
	if !changed {
		ui.SkipDone("No hazmat-managed safe.directory entries in system gitconfig")
		return
	}

	if err := runner.SudoWriteFile("remove hazmat-managed git safe.directory entries", gitconfig, updated); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", gitconfig, err))
		return
	}
	if err := runner.Sudo("set system gitconfig permissions", "chmod", "644", gitconfig); err != nil {
		ui.WarnMsg(fmt.Sprintf("Could not chmod %s: %v", gitconfig, err))
		return
	}
	ui.Ok(fmt.Sprintf("Removed hazmat-managed safe.directory entries from %s", gitconfig))
}

func (env GitSafeDirectoryEnv) systemGitConfigPath() string {
	if env.SystemGitConfigPath == nil {
		return ""
	}
	return env.SystemGitConfigPath()
}

func (env GitSafeDirectoryEnv) managedEntries() []string {
	if env.ManagedEntries == nil {
		return nil
	}
	return env.ManagedEntries()
}

func (env GitSafeDirectoryEnv) syncConfig(content string, wanted []string) (string, bool) {
	if env.SyncConfig == nil {
		return content, false
	}
	return env.SyncConfig(content, wanted)
}
