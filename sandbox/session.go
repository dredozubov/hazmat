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
	var allowDocker bool
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
			if err := warnDockerProject(cfg.ProjectDir, allowDocker); err != nil {
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
	cmd.Flags().BoolVar(&allowDocker, "allow-docker", false,
		"Allow running in a project that contains Docker artifacts (Docker commands will still fail inside the sandbox; use Tier 3 for actual Docker support)")
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

// dockerArtifacts lists filenames that indicate a project requires Docker.
// Their presence means the host Docker daemon would be needed, which is a
// full sandbox escape — the agent user cannot safely share it.
var dockerArtifacts = []string{
	"Dockerfile",
	"Containerfile",
	"compose.yaml",
	"compose.yml",
	"docker-compose.yml",
	"docker-compose.yaml",
}

// warnDockerProject checks whether projectDir contains Docker artifacts and
// either returns an error (allow=false) or prints a warning and continues
// (allow=true) with Tier 3 guidance. The host Docker socket is locked to
// owner-only (0700) by sandbox setup, so Docker commands will fail inside
// the sandbox regardless. This surfaces the issue early with a clear path.
//
// Pass allow=true via --allow-docker when the project has Docker files but
// this session only needs code-editing — Docker commands will still fail,
// but the session is not blocked.
//
// Note: the Docker socket is blocked by filesystem ACL (0700 on dr's socket),
// not by the seatbelt policy. The seatbelt allows broad network-outbound;
// the protection is the socket file permission, enforced by sandbox setup.
func warnDockerProject(projectDir string, allow bool) error {
	var found []string
	for _, name := range dockerArtifacts {
		if _, err := os.Stat(filepath.Join(projectDir, name)); err == nil {
			found = append(found, name)
		}
	}
	// Also check .devcontainer/
	if _, err := os.Stat(filepath.Join(projectDir, ".devcontainer")); err == nil {
		found = append(found, ".devcontainer/")
	}
	if len(found) == 0 {
		return nil
	}

	msg := fmt.Sprintf(`
Docker artifacts detected in %s:
  %s

This sandbox mode (Tier 2: dedicated agent user) does not support Docker.
The host Docker socket is locked to owner-only (0700) and is inaccessible
to the agent user. Granting access would be a full sandbox escape — the
agent could bind-mount the workspace into any container.

Use Tier 3 instead:

  docker sandbox run claude %s

Or for docker-compose projects:

  docker sandbox run claude %s   # docker compose works inside the sandbox

See tier3-docker-sandboxes.md for setup and network policy configuration.
Network policy defaults to allow — switch to deny-mode before running:

  docker sandbox network proxy <name> --allow-host "api.anthropic.com"
  docker sandbox network proxy <name> --allow-host "github.com"
  docker sandbox network proxy <name> --deny-host "*"

To run Tier 2 anyway for code-only work (Docker commands will still fail):

  sandbox claude --allow-docker ...
`,
		projectDir,
		strings.Join(found, "\n  "),
		projectDir,
		projectDir,
	)
	msg = strings.TrimLeft(msg, "\n")
	if allow {
		fmt.Fprintln(os.Stderr, "Warning:", msg)
		return nil
	}
	return fmt.Errorf("%s", msg)
}

func ensureAgentClaudeInstalled() error {
	if asAgentQuiet("test", "-x", agentHome+"/.local/bin/claude") == nil {
		return nil
	}
	return fmt.Errorf("Claude Code is not installed for %s\nInstall it with: sudo -u %s -i, then run: curl -fsSL https://claude.ai/install.sh | bash",
		agentUser, agentUser)
}

// generateSBPL produces a per-session Seatbelt (SBPL) policy with all
// filesystem boundaries embedded as literal absolute paths. This makes
// --reference an actual OS-level boundary rather than an advisory env var:
// only the listed directories receive read access, not the entire workspace.
//
// Policy structure:
//   - PROJECT_DIR gets read+write
//   - Each ReferenceDirs entry gets read-only (skipped if covered by WorkspaceRoot
//     or if it is the same as ProjectDir)
//   - WorkspaceRoot (if non-empty and different from ProjectDir) gets broad read-only
//   - Agent home subtrees, system libraries, tmp, terminal, mach, and network
//     rules are identical to the former static profile
//   - Credential directories are denied last (last-match wins in SBPL)
func generateSBPL(cfg sessionConfig) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w(";; Claude Code runtime seatbelt policy.\n")
	w(";; Generated per-session by sandbox — do not edit manually.\n\n")
	w("(version 1)\n(deny default)\n\n")

	w(";; ── Process execution ──────────────────────────────────────────────────────\n")
	for _, p := range []string{"/usr/bin", "/bin", "/usr/local", "/opt/homebrew", agentHome} {
		w("(allow process-exec (subpath %q))\n", p)
	}
	w("(allow process-fork)\n")
	w("(allow process-info* (target same-sandbox))\n")
	w("(allow signal (target same-sandbox))\n\n")

	w(";; ── System libraries (required by Node.js) ────────────────────────────────\n")
	for _, p := range []string{"/usr/lib", "/usr/share", "/System/Library", "/Library/Frameworks", "/private/etc"} {
		w("(allow file-read* (subpath %q))\n", p)
	}
	for _, p := range []string{"/dev/urandom", "/dev/null", "/dev/zero"} {
		w("(allow file-read* (literal %q))\n", p)
	}
	w("(allow file-write* (literal \"/dev/null\"))\n")
	for _, p := range []string{"/usr/local", "/opt/homebrew"} {
		w("(allow file-read* (subpath %q))\n", p)
	}
	w("\n")

	// Workspace root: broad read-only, only when explicitly requested and
	// distinct from the project dir (avoid a redundant rule).
	if cfg.WorkspaceRoot != "" && cfg.WorkspaceRoot != cfg.ProjectDir {
		w(";; ── Workspace root — read-only ────────────────────────────────────────────\n")
		w("(allow file-read* (subpath %q))\n\n", cfg.WorkspaceRoot)
	}

	// Reference dirs: individual read-only rules, skipping any path already
	// covered by the workspace root or the project dir.
	if len(cfg.ReferenceDirs) > 0 {
		var pending []string
		for _, ref := range cfg.ReferenceDirs {
			if cfg.WorkspaceRoot != "" && isWithinDir(cfg.WorkspaceRoot, ref) {
				continue // already covered by workspace root read rule
			}
			if isWithinDir(cfg.ProjectDir, ref) {
				continue // already covered by project read+write rule below
			}
			pending = append(pending, ref)
		}
		if len(pending) > 0 {
			w(";; ── Reference directories — read-only ─────────────────────────────────────\n")
			for _, ref := range pending {
				w("(allow file-read* (subpath %q))\n", ref)
			}
			w("\n")
		}
	}

	w(";; ── Active project — full read/write ──────────────────────────────────────\n")
	w("(allow file-read* (subpath %q))\n", cfg.ProjectDir)
	w("(allow file-write* (subpath %q))\n\n", cfg.ProjectDir)

	home := agentHome
	w(";; ── Claude config (auth tokens, settings, model cache) ────────────────────\n")
	w("(allow file-read* (subpath %q))\n", home+"/.claude")
	w("(allow file-write* (subpath %q))\n\n", home+"/.claude")

	w(";; ── Claude installation (binary + node_modules) ───────────────────────────\n")
	w("(allow file-read* (subpath %q))\n", home+"/.local")
	w("(allow file-write* (subpath %q))\n\n", home+"/.local")

	w(";; ── Git config (needed for commit operations) ──────────────────────────────\n")
	w("(allow file-read* (literal %q))\n", home+"/.gitconfig")
	w("(allow file-read* (subpath %q))\n\n", home+"/.config/git")

	w(";; ── Shell rc files (read-only; needed at login) ────────────────────────────\n")
	for _, rc := range []string{"/.zshrc", "/.zprofile", "/.bashrc", "/.bash_profile"} {
		w("(allow file-read* (literal %q))\n", home+rc)
	}
	w("\n")

	w(";; ── npm / node cache ────────────────────────────────────────────────────────\n")
	w("(allow file-read* file-write* (subpath %q))\n\n", home+"/.npm")

	w(";; ── XDG / toolchain state under agent home ─────────────────────────────────\n")
	for _, sub := range []string{"/.cache", "/.config", "/.local/share", "/Library/Caches"} {
		w("(allow file-read* file-write* (subpath %q))\n", home+sub)
	}
	w("\n")

	w(";; ── Temp and cache directories ──────────────────────────────────────────────\n")
	for _, p := range []string{"/private/tmp", "/private/var/folders"} {
		w("(allow file-read* file-write* (subpath %q))\n", p)
	}
	w("\n")

	w(";; ── Terminal support (Node.js requires these) ──────────────────────────────\n")
	w("(allow pseudo-tty)\n")
	w("(allow file-ioctl)\n")
	w("(allow file-read* file-write* (literal \"/dev/ptmx\"))\n")
	w("(allow file-read* file-write* (regex #\"/dev/ttys[0-9]+\"))\n\n")

	w(";; ── Mach services ───────────────────────────────────────────────────────────\n")
	for _, svc := range []string{
		"com.apple.system.logger",
		"com.apple.CoreServices.coreservicesd",
		"com.apple.system.notification_center",
		"com.apple.mDNSResponder",
	} {
		w("(allow mach-lookup (global-name %q))\n", svc)
	}
	w("(allow mach-host*)\n\n")

	w(";; ── Network: outbound for Anthropic API calls ──────────────────────────────\n")
	w("(allow network-outbound)\n")
	w("(allow network-inbound (local tcp \"*:*\"))\n\n")

	w(";; ── DENY sensitive credential directories ──────────────────────────────────\n")
	w(";; These appear last so they override the broad allows above (last match wins).\n")
	for _, sub := range []string{"/.ssh", "/.aws", "/.gnupg", "/Library/Keychains", "/.config/gh"} {
		w("(deny file-read* (subpath %q))\n", home+sub)
	}

	return b.String()
}

func runAgentSeatbeltScript(cfg sessionConfig, script string, args ...string) error {
	policy := generateSBPL(cfg)
	policyFile := fmt.Sprintf("/private/tmp/sandbox-%d.sb", os.Getpid())
	if err := os.WriteFile(policyFile, []byte(policy), 0o644); err != nil {
		return fmt.Errorf("write seatbelt policy: %w", err)
	}
	defer os.Remove(policyFile)

	full := []string{"-u", agentUser, "/usr/bin/env", "-i"}
	full = append(full, agentEnvPairs(cfg)...)
	full = append(full,
		"/usr/bin/sandbox-exec",
		"-f", policyFile,
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
