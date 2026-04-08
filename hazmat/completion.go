package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// zshSystemCompletionDir is already in zsh's default fpath on macOS,
// so no .zshrc modifications are needed. Matches where Homebrew and
// other system tools install completions.
const zshSystemCompletionDir = "/usr/local/share/zsh/site-functions"

func zshCompletionFile() string {
	return filepath.Join(zshSystemCompletionDir, "_hazmat")
}

// legacyZshCompletionDir is the old user-local location. Kept only for
// rollback cleanup of installs that used the previous approach.
func legacyZshCompletionDir() string {
	return filepath.Join(os.Getenv("HOME"), ".local/share/zsh/site-functions")
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
// /usr/local/share/zsh/site-functions/_hazmat, which is already in zsh's
// default fpath on macOS. No .zshrc modifications needed.
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

	if err := r.Sudo("create zsh completions directory",
		"mkdir", "-p", zshSystemCompletionDir); err != nil {
		return fmt.Errorf("mkdir %s: %w", zshSystemCompletionDir, err)
	}

	dest := zshCompletionFile()
	if err := r.SudoWriteFile("install zsh completions", dest, string(out)); err != nil {
		return fmt.Errorf("write completion file: %w", err)
	}
	ui.Ok(fmt.Sprintf("Wrote %s", dest))

	// Clean up legacy user-local completion file and fpath block if present.
	legacyFile := filepath.Join(legacyZshCompletionDir(), "_hazmat")
	if _, err := os.Stat(legacyFile); err == nil {
		os.Remove(legacyFile) //nolint:errcheck // best-effort legacy cleanup
	}
	for _, profile := range supportedUserShellProfiles() {
		if data, err := os.ReadFile(profile.rcPath); err == nil &&
			strings.Contains(string(data), completionBlockStart) {
			cleaned := removeManagedBlock(string(data), completionBlockStart, completionBlockEnd)
			if err := r.UserWriteFile(profile.rcPath, cleaned); err == nil {
				ui.Ok(fmt.Sprintf("Removed legacy completions block from %s", profile.rcPath))
			}
		}
	}

	return nil
}

// rollbackZshCompletions removes the completion file from the system
// directory and cleans up any legacy user-local files or .zshrc blocks.
func rollbackZshCompletions(ui *UI, r *Runner) {
	ui.Step("Remove zsh completions")

	dest := zshCompletionFile()
	if _, err := os.Stat(dest); err == nil {
		if err := r.Sudo("remove zsh completions", "rm", "-f", dest); err != nil {
			ui.WarnMsg(fmt.Sprintf("Could not remove %s: %v", dest, err))
		} else {
			ui.Ok(fmt.Sprintf("Removed %s", dest))
		}
	} else {
		ui.SkipDone("Completion file not present")
	}

	// Clean up legacy user-local file.
	legacyFile := filepath.Join(legacyZshCompletionDir(), "_hazmat")
	if _, err := os.Stat(legacyFile); err == nil {
		os.Remove(legacyFile) //nolint:errcheck // best-effort legacy cleanup
	}

	// Clean up legacy fpath managed blocks from shell profiles.
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
