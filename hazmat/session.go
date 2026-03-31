package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type sessionConfig struct {
	ProjectDir string
	ReadDirs   []string
	ResumeDir  string // invoker's session dir — needs seatbelt read+write for symlink targets
}

func newShellCmd() *cobra.Command {
	var project string
	var readDirs []string
	var noBackup bool
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Open a contained shell as the agent user",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := resolveSessionConfig(project, defaultReadDirs(readDirs))
			if err != nil {
				return err
			}
			
			if ensureProjectWritable(cfg.ProjectDir) {
				fmt.Fprintln(os.Stderr, "  Fixed project permissions for agent access")
			}
			preSessionSnapshot(cfg.ProjectDir, "shell", noBackup)
			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && exec /bin/zsh -il`)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false,
		"Skip pre-session snapshot")
	return cmd
}

func newExecCmd() *cobra.Command {
	var project string
	var readDirs []string
	var noBackup bool
	cmd := &cobra.Command{
		Use:   "exec [flags] <command> [args...]",
		Short: "Run a command in containment as the agent user",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := resolveSessionConfig(project, defaultReadDirs(readDirs))
			if err != nil {
				return err
			}
			
			if ensureProjectWritable(cfg.ProjectDir) {
				fmt.Fprintln(os.Stderr, "  Fixed project permissions for agent access")
			}
			preSessionSnapshot(cfg.ProjectDir, "exec", noBackup)
			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && exec "$@"`, args...)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false,
		"Skip pre-session snapshot")
	return cmd
}

func newClaudeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude [hazmat-flags] [claude-flags] [claude-args...]",
		Short: "Launch Claude Code in containment",
		Long: `Launch Claude Code in a sandboxed environment.

Hazmat flags (parsed first, may appear anywhere before --):
  -C, --project <dir>    Writable project directory (defaults to cwd)
  -R, --read <dir>       Read-only directory (repeatable)
  --no-backup            Skip pre-session snapshot
  --ignore-docker        Skip Docker artifact check

All other flags and arguments are forwarded to Claude Code.
When --resume or --continue is detected, sessions from your user account
are automatically synced to the agent user via symlinks.

Examples:
  hazmat claude                        Launch interactively
  hazmat claude -p "explain this"      Print mode
  hazmat claude --model sonnet         Use specific model
  hazmat claude -C /proj -p "hi"       Set project + Claude print mode
  hazmat claude --no-backup -p "hi"    Skip snapshot + Claude print mode
  hazmat claude --resume               Resume a conversation in containment
  hazmat claude --continue             Continue most recent conversation`,
		// Cobra's flag parser rejects unknown flags, which prevents
		// forwarding Claude's own flags (--print, --model, etc.).
		// We disable Cobra's parsing and extract hazmat flags manually.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, forwarded, err := parseClaudeArgs(args)
			if err != nil {
				if err == errClaudeHelp {
					return cmd.Help()
				}
				return err
			}

			if opts.project == "" {
				var projectHint string
				projectHint, forwarded, err = maybeConsumeProjectArg(forwarded)
				if err != nil {
					return err
				}
				opts.project = projectHint
			}

			cfg, err := resolveSessionConfig(opts.project, defaultReadDirs(opts.readDirs))
			if err != nil {
				return err
			}
			if err := warnDockerProject(cfg.ProjectDir, opts.allowDocker); err != nil {
				return err
			}

			// Pre-flight: ensure the agent user can write to the project.
			// Catches projects created before hazmat init or with restrictive umask.
			
			if ensureProjectWritable(cfg.ProjectDir) {
				fmt.Fprintln(os.Stderr, "  Fixed project permissions for agent access")
			}

			preSessionSnapshot(cfg.ProjectDir, "claude", opts.noBackup)

			// Sync sessions for --resume / --continue.
			// Symlinks the invoking user's session files into the agent's
			// config so Claude Code can discover and resume them.
			wantsResume, resumeTarget, wantsContinue := detectResumeFlags(forwarded)
			if wantsResume || wantsContinue {
				invokerDir, err := syncResumeSession(cfg.ProjectDir, resumeTarget)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  Warning: session sync failed: %v\n", err)
					fmt.Fprintln(os.Stderr, "  Resume may not find sessions from your user account.")
				} else if invokerDir != "" {
					cfg.ResumeDir = invokerDir
				}
			}

			// --dangerously-skip-permissions is the default inside hazmat.
			// The containment is OS-level (user isolation + seatbelt +
			// pf firewall); Claude's permission prompts are redundant.
			// Configurable via: hazmat config set session.skip_permissions false
			skipFlag := ""
			if hcfg, _ := loadConfig(); hcfg.SkipPermissions() {
				skipFlag = "--dangerously-skip-permissions "
			}

			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && `+
					`{ test -x "$HOME/.local/bin/claude" || `+
					`{ echo "Error: Claude Code not installed for agent user. Run: hazmat init" >&2; exit 1; }; }; `+
					`exec "$HOME/.local/bin/claude" `+skipFlag+`"$@"`, forwarded...)
		},
	}
	return cmd
}

// claudeOpts holds hazmat-specific flags extracted from the claude command line.
type claudeOpts struct {
	project     string
	readDirs    []string
	noBackup    bool
	allowDocker bool
}

var errClaudeHelp = fmt.Errorf("help requested")

// parseClaudeArgs separates hazmat flags from claude flags+args.
// Hazmat flags (--project, --read, --no-backup, --ignore-docker) are extracted;
// everything else is returned as forwarded args for claude.
func parseClaudeArgs(args []string) (claudeOpts, []string, error) {
	var opts claudeOpts
	var forwarded []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// -- separator: everything after is forwarded verbatim.
		if arg == "--" {
			forwarded = append(forwarded, args[i+1:]...)
			return opts, forwarded, nil
		}

		switch {
		case arg == "--help" || arg == "-h":
			return opts, nil, errClaudeHelp
		case arg == "--no-backup":
			opts.noBackup = true
		case arg == "--ignore-docker":
			opts.allowDocker = true
		case arg == "--project" || arg == "-C":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("%s requires a directory argument", arg)
			}
			i++
			opts.project = args[i]
		case strings.HasPrefix(arg, "--project="):
			opts.project = arg[len("--project="):]
		case arg == "--read" || arg == "-R":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("%s requires a directory argument", arg)
			}
			i++
			opts.readDirs = append(opts.readDirs, args[i])
		case strings.HasPrefix(arg, "--read="):
			opts.readDirs = append(opts.readDirs, arg[len("--read="):])
		default:
			forwarded = append(forwarded, arg)
		}
	}
	return opts, forwarded, nil
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

// expandTilde replaces a leading ~ with the current user's home directory.
func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

// invokerGoModCache returns the invoking user's Go module cache path
// by running `go env GOMODCACHE`. Returns "" if Go is not installed
// or the path doesn't exist.
func invokerGoModCache() string {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// implicitReadDirs returns toolchain cache directories that are always
// included as read-only without user configuration. These are the invoking
// user's package manager caches — sharing them avoids re-downloading the
// world on every sandboxed build. Only paths that actually exist on disk
// are returned.
func implicitReadDirs() []string {
	var dirs []string
	if p := invokerGoModCache(); p != "" {
		dirs = append(dirs, p)
	}
	// Future: Rust (~/.cargo/registry), Maven (~/.m2/repository), etc.
	return dirs
}

// defaultReadDirs prepends the configured session.read_dirs and implicit
// toolchain dirs to the user-supplied read dirs, skipping any that don't
// exist on disk and deduplicating against what the user already passed via -R.
func defaultReadDirs(explicit []string) []string {
	cfg, _ := loadConfig()
	configured := cfg.SessionReadDirs()

	// Resolve explicit dirs for dedup comparison.
	explicitResolved := make(map[string]struct{}, len(explicit))
	for _, d := range explicit {
		abs, err := filepath.Abs(d)
		if err == nil {
			if resolved, err := filepath.EvalSymlinks(abs); err == nil {
				explicitResolved[resolved] = struct{}{}
			}
		}
	}

	var added []string
	for _, dir := range configured {
		dir = expandTilde(dir)
		if _, err := os.Stat(dir); err != nil {
			continue // skip non-existent
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		if _, dup := explicitResolved[resolved]; dup {
			continue
		}
		added = append(added, dir)
		explicitResolved[resolved] = struct{}{}
	}

	if len(added) > 0 {
		fmt.Fprintf(os.Stderr, "hazmat: auto-adding -R %s (from config session.read_dirs)\n",
			strings.Join(added, " -R "))
	}

	// Implicit toolchain dirs — always included, no config needed.
	var implicit []string
	for _, dir := range implicitReadDirs() {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		if _, dup := explicitResolved[resolved]; dup {
			continue
		}
		implicit = append(implicit, dir)
		explicitResolved[resolved] = struct{}{}
	}
	if len(implicit) > 0 {
		fmt.Fprintf(os.Stderr, "hazmat: auto-adding -R %s (toolchain cache)\n",
			strings.Join(implicit, " -R "))
	}

	return append(append(added, implicit...), explicit...)
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

	w(";; ── System info (V8 reads CPU/memory via sysctl at startup) ────────────\n")
	w("(allow sysctl-read)\n\n")

	w(";; ── System libraries (required by Node.js / dyld) ──────────────────────\n")
	w(";; Path traversal literals for realpath() and symlink resolution.\n")
	w(";; /var → /private/var (DNS resolv.conf), /tmp → /private/tmp.\n")
	for _, p := range []string{"/", "/private", "/var", "/tmp", "/etc", "/usr", "/System", "/Library"} {
		w("(allow file-read* (literal %q))\n", p)
	}
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

	// Resume session directory: when --resume or --continue is used, symlinks
	// in the agent's session dir point to the invoking user's session files.
	// The seatbelt must allow read+write so Claude Code can follow the symlinks
	// and append new messages to the resumed conversation transcript.
	// Narrowly scoped to just the project's session directory (JSONL files only).
	if cfg.ResumeDir != "" {
		w(";; ── Resume session directory (symlink targets) ────────────────────────────\n")
		w("(allow file-read* file-write* (subpath %q))\n\n", cfg.ResumeDir)
	}

	home := agentHome
	w(";; ── Agent home — broad read/write, credential dirs denied below ───────────\n")
	w(";; A single subpath rule replaces individual subdirectory allows.\n")
	w(";; Claude Code, Node.js, git, and shell rc files all live here.\n")
	w(";; Credential directories are denied at the end (last-match-wins).\n")
	w("(allow file-read* file-write* (subpath %q))\n\n", home)

	w(";; ── Temp and cache directories ──────────────────────────────────────────────\n")
	w(";; ── DNS resolver + system state ───────────────────────────────────────────\n")
	w(";; resolv.conf is a symlink to /private/var/run/resolv.conf.\n")
	w("(allow file-read* (subpath \"/private/var/run\"))\n\n")

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
		"com.apple.trustd",                // TLS certificate verification (Go, curl, Python, etc.)
		"com.apple.system.opendirectoryd.api",            // user/group directory lookups
		"com.apple.system.DirectoryService.libinfo_v1",    // getpwuid/getgrnam (needed by git, ssh, etc.)
		"com.apple.system.DirectoryService.membership_v1", // group membership checks
	} {
		w("(allow mach-lookup (global-name %q))\n", svc)
	}
	w("(allow mach-host*)\n\n")

	w(";; ── Network: outbound for API calls ──────────────────────────────────────\n")
	w("(allow network-outbound)\n")
	w("(allow network-inbound (local tcp \"*:*\"))\n\n")

	w(";; ── Project write (re-assert after all read-only rules) ───────────────────\n")
	w(";; SBPL is last-match-wins. When a read-only -R directory is a parent of\n")
	w(";; the project directory (e.g. -R ~/workspace with project ~/workspace/foo),\n")
	w(";; the broad file-read* rule must not suppress the project's write access.\n")
	w(";; Re-asserting file-write* here guarantees it is the last matching allow\n")
	w(";; for any write operation targeting the project directory.\n")
	w("(allow file-read* file-write* (subpath %q))\n\n", cfg.ProjectDir)

	w(";; ── DENY sensitive credential directories ──────────────────────────────────\n")
	w(";; These appear last so they override the broad allows above (last match wins).\n")
	w(";; Both file-read* (exfiltration) and file-write* (planting) are denied.\n")
	for _, sub := range []string{
		"/.ssh",              // SSH keys
		"/.aws",              // AWS credentials
		"/.gnupg",            // GPG keys
		"/Library/Keychains", // macOS Keychain
		"/.config/gh",        // GitHub CLI tokens
		"/.docker",           // Docker registry credentials
		"/.kube",             // Kubernetes credentials
		"/.netrc",            // HTTP/FTP basic auth
		"/.m2/settings.xml",  // Maven credentials (file, not dir)
		"/.config/gcloud",    // Google Cloud credentials
		"/.azure",            // Azure CLI credentials
		"/.oci",              // Oracle Cloud credentials
	} {
		w("(deny file-read* file-write* (subpath %q))\n", home+sub)
	}

	return b.String()
}

// syncGitSafeDirectory writes safe.directory entries to the agent user's
// global gitconfig so git accepts repos owned by the invoking user.
// Git 2.36+ rejects repos with different ownership; modern git (2.45+)
// checks this before reading any config, so env vars don't work — it
// must be in the global gitconfig file.
// Runs as the invoking user (not agent), writing to agentHome/.gitconfig
// which is group-writable via the dev group ACL.
func syncGitSafeDirectory(cfg sessionConfig) {
	gitconfig := agentHome + "/.gitconfig"
	content, _ := os.ReadFile(gitconfig)
	sections := parseINI(string(content))

	// Collect all directories that need safe.directory entries.
	dirs := []string{cfg.ProjectDir}
	dirs = append(dirs, cfg.ReadDirs...)

	// Remove existing [safe] section entries and rebuild with current dirs.
	found := false
	for i, s := range sections {
		if s.name != "safe" {
			continue
		}
		found = true
		var kept []string
		for _, line := range s.lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "directory =") || strings.HasPrefix(trimmed, "directory=") {
				continue // remove old entries
			}
			kept = append(kept, line)
		}
		for _, d := range dirs {
			kept = append(kept, "\tdirectory = "+d)
		}
		sections[i].lines = kept
	}

	if !found {
		var lines []string
		for _, d := range dirs {
			lines = append(lines, "\tdirectory = "+d)
		}
		sections = append(sections, iniSection{name: "safe", lines: lines})
	}

	os.WriteFile(gitconfig, []byte(renderINI(sections)), 0o644)
}

func runAgentSeatbeltScript(cfg sessionConfig, script string, args ...string) error {
	pid := os.Getpid()

	// Write safe.directory to agent's gitconfig before entering sandbox.
	syncGitSafeDirectory(cfg)

	policy := generateSBPL(cfg)
	policyFile := fmt.Sprintf("/private/tmp/hazmat-%d.sb", pid)
	if err := os.WriteFile(policyFile, []byte(policy), 0o644); err != nil {
		return fmt.Errorf("write seatbelt policy: %w", err)
	}
	defer os.Remove(policyFile)
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

// preSessionSnapshot takes an automatic snapshot before a session starts.
// Warns on failure but never blocks the session.
func preSessionSnapshot(projectDir, command string, skip bool) {
	if skip {
		return
	}
	start := time.Now()
	fmt.Fprintf(os.Stderr, "  Snapshot: %s ... ", projectDir)
	if err := snapshotProject(projectDir, command); err != nil {
		fmt.Fprintf(os.Stderr, "\n  Warning: pre-session snapshot failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "  Session will proceed without a restore point.")
		return
	}
	fmt.Fprintf(os.Stderr, "done (%.1fs)\n", time.Since(start).Seconds())
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

	// Go toolchain: share the invoking user's module cache read-only.
	// GOMODCACHE points to the invoker's cache so `go build` uses
	// pre-downloaded modules instead of re-fetching. The seatbelt enforces
	// read-only access — if a new dependency is needed, `go mod download`
	// must be run outside the sandbox first.
	if modCache := invokerGoModCache(); modCache != "" {
		pairs = append(pairs, "GOMODCACHE="+modCache)
	}

	return pairs
}
