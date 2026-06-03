package hazmat

import (
	"fmt"
	"hazmat/internal/setup"
	"os"
	"os/exec"
	"path/filepath"

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

func setupZshCompletions(ui *UI, r *Runner) error {
	return setup.SetupZshCompletions(setupCompletionEnv(), ui, r)
}

func rollbackZshCompletions(ui *UI, r *Runner) {
	setup.RollbackZshCompletions(setupCompletionEnv(), ui, r)
}

func setupCompletionEnv() setup.CompletionEnv {
	return setup.CompletionEnv{
		ShellName:             filepath.Base(os.Getenv("SHELL")),
		SystemCompletionDir:   zshSystemCompletionDir,
		CompletionFile:        zshCompletionFile(),
		LegacyCompletionDir:   legacyZshCompletionDir(),
		CompletionBlockStart:  completionBlockStart,
		CompletionBlockEnd:    completionBlockEnd,
		ShellProfiles:         setupShellProfiles(),
		GenerateZshCompletion: generateZshCompletion,
	}
}

func generateZshCompletion() (string, error) {
	hazmatBin, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve hazmat binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(hazmatBin); err == nil {
		hazmatBin = resolved
	}

	out, err := exec.Command(hazmatBin, "completion", "zsh").Output()
	if err != nil {
		return "", fmt.Errorf("generate zsh completions: %w", err)
	}

	return string(out), nil
}
