package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const zshCompletionDirRel = ".local/share/zsh/site-functions"

func zshCompletionDir() string {
	return filepath.Join(os.Getenv("HOME"), zshCompletionDirRel)
}

func zshCompletionFile() string {
	return filepath.Join(zshCompletionDir(), "_hazmat")
}

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "completion [bash|zsh|fish]",
		Short:  "Generate shell completion script",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			switch args[0] {
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, false)
			default:
				return fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish)", args[0])
			}
		},
	}
	return cmd
}

// setupZshCompletions generates a zsh completion script and installs it to
// ~/.local/share/zsh/site-functions/_hazmat. A managed fpath block is added
// to ~/.zshrc so the completions directory is on the search path.
func setupZshCompletions(ui *UI, r *Runner) error {
	ui.Step("Install zsh completions")

	shell := filepath.Base(os.Getenv("SHELL"))
	if shell != "zsh" {
		ui.SkipDone(fmt.Sprintf("Shell is %s, not zsh — skipping completions", shell))
		return nil
	}

	hazmatBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve hazmat binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(hazmatBin); err == nil {
		hazmatBin = resolved
	}

	out, err := exec.Command(hazmatBin, "completion", "zsh").Output()
	if err != nil {
		return fmt.Errorf("generate zsh completions: %w", err)
	}

	dir := zshCompletionDir()
	if err := r.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	dest := zshCompletionFile()
	if err := r.UserWriteFile(dest, string(out)); err != nil {
		return fmt.Errorf("write completion file: %w", err)
	}
	ui.Ok(fmt.Sprintf("Wrote %s", dest))

	// Add fpath to .zshrc so zsh can find the completion file.
	profile, ok := currentUserShellProfile()
	if !ok {
		return nil
	}
	userRCData, _ := os.ReadFile(profile.rcPath)
	rc := string(userRCData)
	if strings.Contains(rc, completionBlockStart) {
		ui.SkipDone(fmt.Sprintf("%s already has a hazmat completions block", profile.rcPath))
		return nil
	}

	updatedRC := upsertManagedBlock(rc,
		completionBlockStart,
		completionBlockEnd,
		fmt.Sprintf(`fpath=(%s $fpath)`, zshCompletionDir()),
	)
	if err := r.UserWriteFile(profile.rcPath, updatedRC); err != nil {
		return fmt.Errorf("update %s: %w", profile.rcPath, err)
	}
	ui.Ok(fmt.Sprintf("Added completions fpath to %s", profile.rcPath))

	return nil
}

// rollbackZshCompletions removes the completion file and the fpath managed
// block from all supported shell profiles.
func rollbackZshCompletions(ui *UI, r *Runner) {
	ui.Step("Remove zsh completions")

	dest := zshCompletionFile()
	if _, err := os.Stat(dest); err == nil {
		if err := os.Remove(dest); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", dest, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed %s", dest))
		}
	} else {
		ui.SkipDone("Completion file not present")
	}

	for _, profile := range supportedUserShellProfiles() {
		if data, err := os.ReadFile(profile.rcPath); err == nil &&
			strings.Contains(string(data), completionBlockStart) {
			cleaned := removeManagedBlock(string(data), completionBlockStart, completionBlockEnd)
			if err := r.UserWriteFile(profile.rcPath, cleaned); err != nil {
				ui.WarnMsg(fmt.Sprintf("Could not update %s: %v", profile.rcPath, err))
			} else {
				ui.Ok(fmt.Sprintf("Removed hazmat completions block from %s", profile.rcPath))
			}
		}
	}
}
