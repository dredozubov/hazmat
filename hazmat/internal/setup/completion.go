package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CompletionEnv struct {
	ShellName             string
	SystemCompletionDir   string
	CompletionFile        string
	LegacyCompletionDir   string
	CompletionBlockStart  string
	CompletionBlockEnd    string
	ShellProfiles         []ShellProfile
	GenerateZshCompletion func() (string, error)
}

func SetupZshCompletions(env CompletionEnv, ui StepStatusUI, runner ToolingRunner) error {
	ui.Step("Install zsh completions")

	if env.ShellName != "zsh" {
		ui.SkipDone(fmt.Sprintf("Shell is %s, not zsh — skipping completions", env.ShellName))
		return nil
	}

	out, err := env.generateZshCompletion()
	if err != nil {
		return err
	}

	if err := runner.Sudo("create zsh completions directory",
		"mkdir", "-p", env.SystemCompletionDir); err != nil {
		return fmt.Errorf("mkdir %s: %w", env.SystemCompletionDir, err)
	}

	dest := env.CompletionFile
	if err := runner.SudoWriteFile("install zsh completions", dest, out); err != nil {
		return fmt.Errorf("write completion file: %w", err)
	}
	if err := runner.Sudo("set completion file permissions", "chmod", "644", dest); err != nil {
		return fmt.Errorf("chmod %s: %w", dest, err)
	}
	ui.Ok(fmt.Sprintf("Wrote %s", dest))

	removeLegacyCompletion(env, ui, runner, false)
	return nil
}

func RollbackZshCompletions(env CompletionEnv, ui StepStatusUI, runner ToolingRunner) {
	ui.Step("Remove zsh completions")

	dest := env.CompletionFile
	if _, err := os.Stat(dest); err == nil {
		if err := runner.Sudo("remove zsh completions", "rm", "-f", dest); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", dest, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed %s", dest))
		}
	} else {
		ui.SkipDone("Completion file not present")
	}

	removeLegacyCompletion(env, ui, runner, true)
}

func removeLegacyCompletion(env CompletionEnv, ui StepStatusUI, runner ToolingRunner, warnOnProfileWrite bool) {
	legacyFile := filepath.Join(env.LegacyCompletionDir, "_hazmat")
	if _, err := os.Stat(legacyFile); err == nil {
		os.Remove(legacyFile) //nolint:errcheck // best-effort legacy cleanup
	}

	for _, profile := range env.ShellProfiles {
		if data, err := os.ReadFile(profile.RCPath); err == nil &&
			strings.Contains(string(data), env.CompletionBlockStart) {
			cleaned := RemoveManagedBlock(string(data), env.CompletionBlockStart, env.CompletionBlockEnd)
			if err := runner.UserWriteFile(profile.RCPath, cleaned); err != nil {
				if warnOnProfileWrite {
					ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", profile.RCPath, err))
				}
			} else if warnOnProfileWrite {
				ui.Ok(fmt.Sprintf("Removed hazmat completions block from %s", profile.RCPath))
			} else {
				ui.Ok(fmt.Sprintf("Removed legacy completions block from %s", profile.RCPath))
			}
		}
	}
}

func (env CompletionEnv) generateZshCompletion() (string, error) {
	if env.GenerateZshCompletion == nil {
		return "", fmt.Errorf("zsh completion generation callback is not configured")
	}
	return env.GenerateZshCompletion()
}
