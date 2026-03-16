package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newShellCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Open an interactive sandboxed shell as the agent user",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			projectDir, err := resolveProjectDir(project)
			if err != nil {
				return err
			}
			return runAgentSeatbeltScript(projectDir,
				`cd "$SANDBOX_PROJECT_DIR" && exec /bin/zsh -il`)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory inside the shared workspace (defaults to current directory)")
	return cmd
}

func newExecCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "exec [flags] <command> [args...]",
		Short: "Run a tool inside the sandbox as the agent user",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			projectDir, err := resolveProjectDir(project)
			if err != nil {
				return err
			}
			return runAgentSeatbeltScript(projectDir,
				`cd "$SANDBOX_PROJECT_DIR" && exec "$@"`, args...)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory inside the shared workspace (defaults to current directory)")
	return cmd
}

func newClaudeCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "claude [flags] [claude-args...]",
		Short: "Launch Claude Code inside the sandbox as the agent user",
		Args:  cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			projectHint, forwarded, err := maybeConsumeProjectArg(args)
			if err != nil {
				return err
			}
			if project == "" {
				project = projectHint
			}

			projectDir, err := resolveProjectDir(project)
			if err != nil {
				return err
			}
			if err := ensureAgentClaudeInstalled(); err != nil {
				return err
			}
			return runAgentSeatbeltScript(projectDir,
				`cd "$SANDBOX_PROJECT_DIR" && exec "$HOME/.local/bin/claude" "$@"`, forwarded...)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Project directory inside the shared workspace (defaults to current directory)")
	return cmd
}

func maybeConsumeProjectArg(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, nil
	}

	candidate := args[0]
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", nil, fmt.Errorf("resolve %q: %w", candidate, err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", args, nil
	}
	return abs, args[1:], nil
}

func resolveProjectDir(project string) (string, error) {
	target := project
	if target == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determine current directory: %w", err)
		}
		target = wd
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", target, err)
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return "", fmt.Errorf("resolve symlinks for %q: %w", target, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", abs)
	}

	workspace := sharedWorkspace
	if resolved, err := filepath.EvalSymlinks(sharedWorkspace); err == nil {
		workspace = resolved
	}
	if !isWithinDir(workspace, abs) {
		return "", fmt.Errorf("%s is outside %s\nMove the repo into ~/workspace-shared or pass --project with a directory inside %s",
			abs, sharedWorkspace, sharedWorkspace)
	}
	return abs, nil
}

func isWithinDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func ensureAgentClaudeInstalled() error {
	if asAgentQuiet("test", "-x", agentHome+"/.local/bin/claude") == nil {
		return nil
	}
	return fmt.Errorf("Claude Code is not installed for %s\nInstall it with: sudo -u %s -i, then run: curl -fsSL https://claude.ai/install.sh | bash",
		agentUser, agentUser)
}

func runAgentSeatbeltScript(projectDir, script string, args ...string) error {
	if _, err := os.Stat(seatbeltProfilePath); err != nil {
		return fmt.Errorf("seatbelt profile missing at %s\nRun 'sandbox setup' first", seatbeltProfilePath)
	}

	full := []string{"-u", agentUser, "/usr/bin/env", "-i"}
	full = append(full, agentEnvPairs(projectDir)...)
	full = append(full,
		"/usr/bin/sandbox-exec",
		"-D", "HOME="+agentHome,
		"-D", "PROJECT_DIR="+projectDir,
		"-D", "TMPDIR="+defaultAgentTmpDir,
		"-f", seatbeltProfilePath,
		"/bin/zsh", "-lc", script, "zsh",
	)
	full = append(full, args...)

	cmd := exec.Command("sudo", full...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func agentEnvPairs(projectDir string) []string {
	pairs := []string{
		"HOME=" + agentHome,
		"USER=" + agentUser,
		"LOGNAME=" + agentUser,
		"SHELL=/bin/zsh",
		"PATH=" + defaultAgentPath,
		"TMPDIR=" + defaultAgentTmpDir,
		"XDG_CACHE_HOME=" + defaultAgentCacheHome,
		"XDG_CONFIG_HOME=" + defaultAgentConfigHome,
		"XDG_DATA_HOME=" + defaultAgentDataHome,
		"HOMEBREW_NO_AUTO_UPDATE=1",
		"SANDBOX_ACTIVE=1",
		"SANDBOX_PROJECT_DIR=" + projectDir,
	}
	for _, key := range []string{"TERM", "COLORTERM", "LANG", "LC_ALL"} {
		if value := os.Getenv(key); value != "" {
			pairs = append(pairs, key+"="+value)
		}
	}
	return pairs
}
