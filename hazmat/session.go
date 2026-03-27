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
	ProjectDir string
	ReadDirs   []string
}

func newShellCmd() *cobra.Command {
	var project string
	var readDirs []string
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Open a contained shell as the agent user",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := resolveSessionConfig(project, readDirs)
			if err != nil {
				return err
			}
			warnUnmanagedProject(cfg.ProjectDir)
			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && exec /bin/zsh -il`)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	return cmd
}

func newExecCmd() *cobra.Command {
	var project string
	var readDirs []string
	cmd := &cobra.Command{
		Use:   "exec [flags] <command> [args...]",
		Short: "Run a command in containment as the agent user",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := resolveSessionConfig(project, readDirs)
			if err != nil {
				return err
			}
			warnUnmanagedProject(cfg.ProjectDir)
			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && exec "$@"`, args...)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	return cmd
}

func newClaudeCmd() *cobra.Command {
	var project string
	var readDirs []string
	var allowDocker bool
	cmd := &cobra.Command{
		Use:   "claude [flags] [claude-args...]",
		Short: "Launch Claude Code in containment",
		Args:  cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			projectHint, forwarded, err := maybeConsumeProjectArg(args)
			if err != nil {
				return err
			}
			if project == "" {
				project = projectHint
			}

			cfg, err := resolveSessionConfig(project, readDirs)
			if err != nil {
				return err
			}
			if err := warnDockerProject(cfg.ProjectDir, allowDocker); err != nil {
				return err
			}
			warnUnmanagedProject(cfg.ProjectDir)
			// The install check runs inside the sandbox after privilege
			// transition, so no extra sudo call is needed on the daily path.
			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && `+
					`{ test -x "$HOME/.local/bin/claude" || `+
					`{ echo "Error: Claude Code not installed for agent user. Run: hazmat init" >&2; exit 1; }; }; `+
					`exec "$HOME/.local/bin/claude" "$@"`, forwarded...)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	cmd.Flags().BoolVar(&allowDocker, "ignore-docker", false,
		"Skip Docker artifact check (Docker won't work; use Tier 3 for Docker support)")
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

func resolveSessionConfig(project string, readPaths []string) (sessionConfig, error) {
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return sessionConfig{}, fmt.Errorf("project: %w", err)
	}

	readDirs, err := resolveReadDirs(readPaths)
	if err != nil {
		return sessionConfig{}, err
	}

	return sessionConfig{
		ProjectDir: projectDir,
		ReadDirs:   readDirs,
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

func resolveReadDirs(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(paths))
	resolved := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := resolveDir(p, false)
		if err != nil {
			return nil, fmt.Errorf("read dir %q: %w", p, err)
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
// Pass allow=true via --ignore-docker when the project has Docker files but
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
Docker artifacts detected in %s: %s

Docker is not available in this containment mode (socket locked to owner-only).

  hazmat claude --ignore-docker   Continue without Docker support
  docker hazmat run claude %s     Use Tier 3 for full Docker (see tier3-docker-sandboxes.md)
`,
		projectDir,
		strings.Join(found, ", "),
		projectDir,
	)
	msg = strings.TrimLeft(msg, "\n")
	if allow {
		fmt.Fprintln(os.Stderr, "Warning:", msg)
		return nil
	}
	return fmt.Errorf("%s", msg)
}

// warnUnmanagedProject prints a warning when projectDir is outside the
// canonical workspace root.  Projects outside that root are not covered
// by 'hazmat backup' — changes made in the session will not be backed up.
//
// The warning is advisory: the session still launches.  To silence it,
// move the project inside ~/workspace or enroll it with 'hazmat enroll'.
func warnUnmanagedProject(projectDir string) {
	// Resolve sharedWorkspace symlinks so the comparison works even when
	// ~/workspace is itself a symlink (e.g. → /Users/Shared/workspace).
	managedRoot := sharedWorkspace
	if resolved, err := filepath.EvalSymlinks(sharedWorkspace); err == nil {
		managedRoot = resolved
	}
	if isWithinDir(managedRoot, projectDir) {
		return
	}
	fmt.Fprintf(os.Stderr,
		"Warning: %s is outside the managed workspace (%s).\n"+
			"Changes made in this session are not covered by 'hazmat backup'.\n",
		projectDir, sharedWorkspace)
}


// generateSBPL produces a per-session Seatbelt (SBPL) policy with all
// filesystem boundaries embedded as literal absolute paths. This makes
// --read an actual OS-level boundary rather than an advisory env var:
// only the listed directories receive read access beyond the project.
//
// Policy structure:
//   - PROJECT_DIR gets read+write
//   - Each ReadDirs entry gets read-only (skipped if covered by ProjectDir
//     or another ReadDirs entry)
//   - Agent home subtrees, system libraries, tmp, terminal, mach, and network
//     rules are identical to the former static profile
//   - Credential directories are denied last (last-match wins in SBPL)
func generateSBPL(cfg sessionConfig) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w(";; Claude Code runtime seatbelt policy.\n")
	w(";; Generated per-session by hazmat — do not edit manually.\n\n")
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

	// Read-only directories: individual rules, skipping any path already
	// covered by the project dir or by another (broader) read dir.
	if len(cfg.ReadDirs) > 0 {
		var pending []string
		for _, dir := range cfg.ReadDirs {
			if isWithinDir(cfg.ProjectDir, dir) {
				continue // already covered by project read+write
			}
			covered := false
			for _, other := range cfg.ReadDirs {
				if other != dir && isWithinDir(other, dir) {
					covered = true
					break
				}
			}
			if covered {
				continue
			}
			pending = append(pending, dir)
		}
		if len(pending) > 0 {
			w(";; ── Read-only directories ──────────────────────────────────────────────────\n")
			for _, dir := range pending {
				w("(allow file-read* (subpath %q))\n", dir)
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
	for _, sub := range []string{
		"/.ssh",                // SSH keys
		"/.aws",                // AWS credentials
		"/.gnupg",              // GPG keys
		"/Library/Keychains",   // macOS Keychain
		"/.config/gh",          // GitHub CLI tokens
		"/.docker",             // Docker registry credentials
		"/.kube",               // Kubernetes credentials
		"/.netrc",              // HTTP/FTP basic auth
		"/.m2/settings.xml",    // Maven credentials (file, not dir)
		"/.config/gcloud",      // Google Cloud credentials
		"/.azure",              // Azure CLI credentials
		"/.oci",                // Oracle Cloud credentials
	} {
		w("(deny file-read* (subpath %q))\n", home+sub)
	}

	return b.String()
}

func runAgentSeatbeltScript(cfg sessionConfig, script string, args ...string) error {
	policy := generateSBPL(cfg)
	policyFile := fmt.Sprintf("/private/tmp/hazmat-%d.sb", os.Getpid())
	if err := os.WriteFile(policyFile, []byte(policy), 0o644); err != nil {
		return fmt.Errorf("write seatbelt policy: %w", err)
	}
	defer os.Remove(policyFile)
	// Explicit chmod overrides the process umask so hazmat-launch always
	// receives a 0644 file regardless of what umask dr has set.
	if err := os.Chmod(policyFile, 0o644); err != nil {
		return fmt.Errorf("set seatbelt policy mode: %w", err)
	}

	// The NOPASSWD sudoers rule covers exactly:
	//   sudo -u agent /usr/local/libexec/hazmat-launch <policy-file> ...
	//
	// hazmat-launch validates the policy file path and SUDO_UID ownership
	// before calling sandbox-exec -f.  It refuses -p inline policies.
	// env -i runs *inside* the sandbox so the environment is set after the
	// privilege boundary is crossed.
	full := []string{
		"-u", agentUser,
		launchHelper, policyFile,
		"/usr/bin/env", "-i",
	}
	full = append(full, agentEnvPairs(cfg)...)
	full = append(full, "/bin/zsh", "-lc", script, "zsh")
	full = append(full, args...)

	cmd := exec.Command("sudo", full...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func agentEnvPairs(cfg sessionConfig) []string {
	readDirsJSON, _ := json.Marshal(cfg.ReadDirs)
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
		"SANDBOX_PROJECT_DIR=" + cfg.ProjectDir,
		"SANDBOX_READ_DIRS_JSON=" + string(readDirsJSON),
	}
	for _, key := range []string{"TERM", "COLORTERM", "LANG", "LC_ALL"} {
		if value := os.Getenv(key); value != "" {
			pairs = append(pairs, key+"="+value)
		}
	}
	return pairs
}
