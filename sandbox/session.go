package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type sessionConfig struct {
	WorkspaceRoot string
	ProjectDir    string
	ReferenceDirs []string
}

func newShellCmd() *cobra.Command {
	var project string
	var workspace string
	var references []string
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Open an interactive sandboxed shell as the agent user",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := resolveSessionConfig(project, workspace, references)
			if err != nil {
				return err
			}
			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && exec /bin/zsh -il`)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory, may be outside ~/workspace)")
	cmd.Flags().StringVarP(&workspace, "workspace", "W", "",
		"Read-only workspace root to expose to the agent (optional)")
	cmd.Flags().StringArrayVarP(&references, "reference", "R", nil,
		"Read-only reference directory (repeat flag for multiple paths)")
	return cmd
}

func newExecCmd() *cobra.Command {
	var project string
	var workspace string
	var references []string
	cmd := &cobra.Command{
		Use:   "exec [flags] <command> [args...]",
		Short: "Run a tool inside the sandbox as the agent user",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := resolveSessionConfig(project, workspace, references)
			if err != nil {
				return err
			}
			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && exec "$@"`, args...)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory, may be outside ~/workspace)")
	cmd.Flags().StringVarP(&workspace, "workspace", "W", "",
		"Read-only workspace root to expose to the agent (optional)")
	cmd.Flags().StringArrayVarP(&references, "reference", "R", nil,
		"Read-only reference directory (repeat flag for multiple paths)")
	return cmd
}

func newClaudeCmd() *cobra.Command {
	var project string
	var workspace string
	var references []string
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

			cfg, err := resolveSessionConfig(project, workspace, references)
			if err != nil {
				return err
			}
			if err := ensureAgentClaudeInstalled(); err != nil {
				return err
			}
			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && exec "$HOME/.local/bin/claude" "$@"`, forwarded...)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory, may be outside ~/workspace)")
	cmd.Flags().StringVarP(&workspace, "workspace", "W", "",
		"Read-only workspace root to expose to the agent (optional)")
	cmd.Flags().StringArrayVarP(&references, "reference", "R", nil,
		"Read-only reference directory (repeat flag for multiple paths)")
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

func resolveSessionConfig(project, workspace string, references []string) (sessionConfig, error) {
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return sessionConfig{}, fmt.Errorf("project: %w", err)
	}

	var workspaceRoot string
	if workspace != "" {
		workspaceRoot, err = resolveDir(workspace, false)
		if err != nil {
			return sessionConfig{}, fmt.Errorf("workspace: %w", err)
		}
	}

	referenceDirs, err := resolveReferenceDirs(references)
	if err != nil {
		return sessionConfig{}, err
	}

	return sessionConfig{
		WorkspaceRoot: workspaceRoot,
		ProjectDir:    projectDir,
		ReferenceDirs: referenceDirs,
	}, nil
}

// resolveDir resolves target to an absolute, symlink-free directory path.
// If target is empty and defaultToCwd is true, the current working directory
// is used. The resolved path must exist and be a directory.
func resolveDir(target string, defaultToCwd bool) (string, error) {
	if target == "" && defaultToCwd {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determine current directory: %w", err)
		}
		target = wd
	}
	if target == "" {
		return "", fmt.Errorf("path is required")
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

	return abs, nil
}

func resolveReferenceDirs(references []string) ([]string, error) {
	if len(references) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(references))
	resolved := make([]string, 0, len(references))
	for _, ref := range references {
		abs, err := resolveDir(ref, false)
		if err != nil {
			return nil, fmt.Errorf("reference %q: %w", ref, err)
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		resolved = append(resolved, abs)
	}
	return resolved, nil
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

func runAgentSeatbeltScript(cfg sessionConfig, script string, args ...string) error {
	if _, err := os.Stat(seatbeltProfilePath); err != nil {
		return fmt.Errorf("seatbelt profile missing at %s\nRun 'sandbox setup' first", seatbeltProfilePath)
	}

	// When no explicit workspace root is given, collapse WORKSPACE_ROOT to
	// PROJECT_DIR so the static SBPL profile's param remains valid without
	// inadvertently exposing directories outside the project.
	workspaceRoot := cfg.WorkspaceRoot
	if workspaceRoot == "" {
		workspaceRoot = cfg.ProjectDir
	}

	full := []string{"-u", agentUser, "/usr/bin/env", "-i"}
	full = append(full, agentEnvPairs(cfg)...)
	full = append(full,
		"/usr/bin/sandbox-exec",
		"-D", "HOME="+agentHome,
		"-D", "WORKSPACE_ROOT="+workspaceRoot,
		"-D", "PROJECT_DIR="+cfg.ProjectDir,
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

func agentEnvPairs(cfg sessionConfig) []string {
	referencesJSON, _ := json.Marshal(cfg.ReferenceDirs)
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
		"SANDBOX_WORKSPACE_ROOT=" + cfg.WorkspaceRoot,
		"SANDBOX_PROJECT_DIR=" + cfg.ProjectDir,
		"SANDBOX_REFERENCE_DIRS_JSON=" + string(referencesJSON),
	}
	for _, key := range []string{"TERM", "COLORTERM", "LANG", "LC_ALL"} {
		if value := os.Getenv(key); value != "" {
			pairs = append(pairs, key+"="+value)
		}
	}
	return pairs
}
