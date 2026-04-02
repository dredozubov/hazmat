package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type sessionConfig struct {
	ProjectDir       string
	ReadDirs         []string
	BackupExcludes   []string
	PackEnv          map[string]string // from pack env_passthrough (resolved values)
	PackRegistryKeys   []string          // active registry-redirect env keys (for UX)
	PackCredentialKeys []string          // active credential-token env keys (for UX)
	ActivePacks        []string          // pack names, for status bar
}

type sessionLaunchUI struct {
	clearScreen      bool
	showStatusBar    bool
	waitForAltScreen bool
}

func runProjectPreflight(projectDir string) error {
	if ensureProjectWritable(projectDir) {
		fmt.Fprintln(os.Stderr, "  Fixed project permissions for agent access")
	}
	if fixed, err := ensureGitMetadataHealthy(projectDir); err != nil {
		return err
	} else if fixed {
		fmt.Fprintln(os.Stderr, "  Fixed Git metadata permissions for collaborative access")
	}
	return nil
}

func newShellCmd() *cobra.Command {
	var project string
	var readDirs []string
	var packNames []string
	var noBackup bool
	var useSandbox bool
	var allowDocker bool
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Open a contained shell as the agent user",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := resolveSessionConfig(project, defaultReadDirs(readDirs))
			if err != nil {
				return err
			}
			if err := applyPacks(&cfg, packNames); err != nil {
				return err
			}
			useSandbox, err = resolveSessionSandboxMode("shell", cfg.ProjectDir, useSandbox, allowDocker)
			if err != nil {
				return err
			}
			preSessionSnapshot(cfg, "shell", noBackup)
			if useSandbox {
				return runSandboxShellSession(cfg)
			}
			if err := runProjectPreflight(cfg.ProjectDir); err != nil {
				return err
			}
			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && exec /bin/zsh -il`)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	cmd.Flags().StringArrayVar(&packNames, "pack", nil,
		"Activate a stack pack (repeatable, e.g. --pack go --pack node)")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false,
		"Skip pre-session snapshot")
	cmd.Flags().BoolVar(&useSandbox, "sandbox", false,
		"Run with Docker Sandbox support")
	cmd.Flags().BoolVar(&allowDocker, "ignore-docker", false,
		"Continue without Docker support even if Docker markers are present")
	return cmd
}

func newExecCmd() *cobra.Command {
	var project string
	var readDirs []string
	var packNames []string
	var noBackup bool
	var useSandbox bool
	var allowDocker bool
	cmd := &cobra.Command{
		Use:   "exec [flags] <command> [args...]",
		Short: "Run a command in containment as the agent user",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := resolveSessionConfig(project, defaultReadDirs(readDirs))
			if err != nil {
				return err
			}
			if err := applyPacks(&cfg, packNames); err != nil {
				return err
			}
			useSandbox, err = resolveSessionSandboxMode("exec", cfg.ProjectDir, useSandbox, allowDocker)
			if err != nil {
				return err
			}
			preSessionSnapshot(cfg, "exec", noBackup)
			if useSandbox {
				return runSandboxExecSession(cfg, args)
			}
			if err := runProjectPreflight(cfg.ProjectDir); err != nil {
				return err
			}
			return runAgentSeatbeltScript(cfg,
				`cd "$SANDBOX_PROJECT_DIR" && exec "$@"`, args...)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	cmd.Flags().StringArrayVar(&packNames, "pack", nil,
		"Activate a stack pack (repeatable, e.g. --pack go --pack node)")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false,
		"Skip pre-session snapshot")
	cmd.Flags().BoolVar(&useSandbox, "sandbox", false,
		"Run with Docker Sandbox support")
	cmd.Flags().BoolVar(&allowDocker, "ignore-docker", false,
		"Continue without Docker support even if Docker markers are present")
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
  --pack <name>          Activate a stack pack (repeatable)
  --no-backup            Skip pre-session snapshot
  --sandbox              Use Docker Sandbox support
  --ignore-docker        Skip Docker artifact check

All other flags and arguments are forwarded to Claude Code.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root.
When --resume or --continue is detected, sessions from your user account
are copied into the agent user's local Claude session directory.

Examples:
  hazmat claude                        Launch interactively
  hazmat claude -p "explain this"      Print mode
  hazmat claude --model sonnet         Use specific model
  hazmat claude -C /proj -p "hi"       Set project + Claude print mode
  hazmat claude --sandbox -C /proj     Use Docker Sandboxes
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
				if err == errHarnessHelp {
					return cmd.Help()
				}
				return err
			}

			cfg, err := resolveSessionConfig(opts.project, defaultReadDirs(opts.readDirs))
			if err != nil {
				return err
			}
			if err := applyPacks(&cfg, opts.packs); err != nil {
				return err
			}
			useSandbox, err := resolveSessionSandboxMode("claude", cfg.ProjectDir, opts.useSandbox, opts.allowDocker)
			if err != nil {
				return err
			}

			preSessionSnapshot(cfg, "claude", opts.noBackup)

			if useSandbox {
				return runSandboxClaudeSession(cfg, forwarded)
			}

			// Pre-flight: ensure the agent user can write to the project.
			// Catches projects created before hazmat init or with restrictive umask.
			if err := runProjectPreflight(cfg.ProjectDir); err != nil {
				return err
			}

			// Sync sessions for --resume / --continue.
			// Copies the invoking user's session files into the agent's
			// config so Claude Code can discover and resume them without
			// reading the host transcript directory in place.
			wantsResume, resumeTarget, wantsContinue := detectResumeFlags(forwarded)
			if wantsResume || wantsContinue {
				if err := syncResumeSession(cfg.ProjectDir, resumeTarget, wantsContinue); err != nil {
					fmt.Fprintf(os.Stderr, "  Warning: session sync failed: %v\n", err)
					fmt.Fprintln(os.Stderr, "  Resume may not find sessions from your user account.")
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

			return runAgentSeatbeltScriptWithUI(cfg, claudeLaunchUI(forwarded),
				`cd "$SANDBOX_PROJECT_DIR" && `+
					`{ test -x "$HOME/.local/bin/claude" || `+
					`{ echo "Error: Claude Code not installed for agent user. Run: hazmat init" >&2; exit 1; }; }; `+
					`exec "$HOME/.local/bin/claude" `+skipFlag+`"$@"`, forwarded...)
		},
	}
	return cmd
}

func newOpenCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "opencode [hazmat-flags] [opencode-flags] [opencode-args...]",
		Short: "Launch OpenCode in containment",
		Long: `Launch OpenCode in a sandboxed environment.

Hazmat flags (parsed first, may appear anywhere before --):
  -C, --project <dir>    Writable project directory (defaults to cwd)
  -R, --read <dir>       Read-only directory (repeatable)
  --pack <name>          Activate a stack pack (repeatable)
  --no-backup            Skip pre-session snapshot
  --ignore-docker        Skip Docker artifact check

All other flags and arguments are forwarded to OpenCode.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root.

Examples:
  hazmat opencode
  hazmat opencode -p "explain this"
  hazmat opencode -C /proj -p "hi"
  hazmat opencode --no-backup -p "hi"`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, forwarded, err := parseHarnessArgs(args)
			if err != nil {
				if err == errHarnessHelp {
					return cmd.Help()
				}
				return err
			}

			cfg, err := resolveSessionConfig(opts.project, defaultReadDirs(opts.readDirs))
			if err != nil {
				return err
			}
			if opts.useSandbox {
				return fmt.Errorf("--sandbox is not supported for hazmat opencode yet")
			}
			if err := applyPacks(&cfg, opts.packs); err != nil {
				return err
			}
			if err := warnDockerProject(cfg.ProjectDir, opts.allowDocker); err != nil {
				return err
			}
			if err := runProjectPreflight(cfg.ProjectDir); err != nil {
				return err
			}

			preSessionSnapshot(cfg, "opencode", opts.noBackup)

			return runAgentSeatbeltScript(cfg, openCodeLaunchScript(), forwarded...)
		},
	}
	return cmd
}

func newCodexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "codex [hazmat-flags] [codex-flags] [codex-args...]",
		Short: "Launch Codex in containment",
		Long: `Launch Codex in a sandboxed environment.

Hazmat flags (parsed first, may appear anywhere before --):
  -C, --project <dir>    Writable project directory (defaults to cwd)
  -R, --read <dir>       Read-only directory (repeatable)
  --pack <name>          Activate a stack pack (repeatable)
  --no-backup            Skip pre-session snapshot
  --ignore-docker        Skip Docker artifact check

All other flags and arguments are forwarded to Codex.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root.

Examples:
  hazmat codex
  hazmat codex "explain this repo"
  hazmat codex -C /proj --full-auto
  hazmat codex --no-backup`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, forwarded, err := parseHarnessArgs(args)
			if err != nil {
				if err == errHarnessHelp {
					return cmd.Help()
				}
				return err
			}

			cfg, err := resolveSessionConfig(opts.project, defaultReadDirs(opts.readDirs))
			if err != nil {
				return err
			}
			if opts.useSandbox {
				return fmt.Errorf("--sandbox is not supported for hazmat codex yet")
			}
			if err := applyPacks(&cfg, opts.packs); err != nil {
				return err
			}
			if err := warnDockerProject(cfg.ProjectDir, opts.allowDocker); err != nil {
				return err
			}
			if err := runProjectPreflight(cfg.ProjectDir); err != nil {
				return err
			}

			preSessionSnapshot(cfg, "codex", opts.noBackup)

			return runAgentSeatbeltScript(cfg, codexLaunchScript(), forwarded...)
		},
	}
	return cmd
}

// harnessSessionOpts holds hazmat-specific flags extracted from a harness
// command line before forwarding the rest to the harness CLI.
type harnessSessionOpts struct {
	project     string
	readDirs    []string
	packs       []string
	noBackup    bool
	useSandbox  bool
	allowDocker bool
}

type claudeOpts = harnessSessionOpts

var errHarnessHelp = fmt.Errorf("help requested")
var errClaudeHelp = errHarnessHelp

// parseHarnessArgs separates hazmat flags from a forwarded harness CLI.
// Hazmat flags (--project, --read, --pack, --no-backup, --sandbox,
// --ignore-docker)
// are extracted; everything else is returned as forwarded args.
func parseHarnessArgs(args []string) (harnessSessionOpts, []string, error) {
	var opts harnessSessionOpts
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
			return opts, nil, errHarnessHelp
		case arg == "--no-backup":
			opts.noBackup = true
		case arg == "--sandbox":
			opts.useSandbox = true
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
		case arg == "--pack":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("%s requires a pack name", arg)
			}
			i++
			opts.packs = append(opts.packs, args[i])
		case strings.HasPrefix(arg, "--pack="):
			opts.packs = append(opts.packs, arg[len("--pack="):])
		default:
			forwarded = append(forwarded, arg)
		}
	}
	return opts, forwarded, nil
}

func parseClaudeArgs(args []string) (claudeOpts, []string, error) {
	return parseHarnessArgs(args)
}

func claudeLaunchUI(forwarded []string) sessionLaunchUI {
	wantsResume, resumeTarget, wantsContinue := detectResumeFlags(forwarded)
	if wantsResume && resumeTarget == "" && !wantsContinue {
		return sessionLaunchUI{
			clearScreen:      true,
			showStatusBar:    false,
			waitForAltScreen: true,
		}
	}
	return sessionLaunchUI{showStatusBar: true}
}

var altScreenEnterSequences = [][]byte{
	[]byte("\x1b[?1049h"),
	[]byte("\x1b[?1047h"),
	[]byte("\x1b[?47h"),
}

func transcriptHasAltScreenEnter(buf []byte) bool {
	for _, seq := range altScreenEnterSequences {
		if len(seq) > 0 && bytes.Contains(buf, seq) {
			return true
		}
	}
	return false
}

func watchTranscriptForAltScreen(path string, activate func(), stop <-chan struct{}) {
	const (
		pollInterval = 100 * time.Millisecond
		tailBytes    = 64
	)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var offset int64
	var tail []byte

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			f, err := os.Open(path)
			if err != nil {
				continue
			}

			info, err := f.Stat()
			if err != nil {
				_ = f.Close()
				continue
			}
			if info.Size() < offset {
				offset = 0
				tail = tail[:0]
			}
			if info.Size() == offset {
				_ = f.Close()
				continue
			}

			chunk := make([]byte, info.Size()-offset)
			n, err := f.ReadAt(chunk, offset)
			_ = f.Close()
			if err != nil && err != io.EOF {
				continue
			}

			chunk = chunk[:n]
			offset += int64(n)

			window := append(append([]byte{}, tail...), chunk...)
			if transcriptHasAltScreenEnter(window) {
				activate()
				return
			}

			if len(window) > tailBytes {
				tail = append([]byte{}, window[len(window)-tailBytes:]...)
			} else {
				tail = append([]byte{}, window...)
			}
		}
	}
}

// applyPacks resolves, validates, and merges active packs into the session
// config. It also prints pack suggestions and warnings to stderr.
func applyPacks(cfg *sessionConfig, packFlags []string) error {
	packs, err := resolveActivePacks(packFlags, cfg.ProjectDir)
	if err != nil {
		return err
	}

	// Detect and suggest packs if none are active.
	activeNames := make(map[string]struct{}, len(packs))
	for _, p := range packs {
		activeNames[p.PackMeta.Name] = struct{}{}
	}
	if suggestions := suggestPacks(cfg.ProjectDir, activeNames); len(suggestions) > 0 {
		fmt.Fprintf(os.Stderr, "hazmat: suggested packs: %s (activate with --pack <name>)\n",
			strings.Join(suggestions, ", "))
	}

	if len(packs) == 0 {
		return nil
	}

	// Print active packs.
	names := make([]string, 0, len(packs))
	for _, p := range packs {
		names = append(names, p.PackMeta.Name)
	}
	cfg.ActivePacks = names
	fmt.Fprintf(os.Stderr, "hazmat: active packs: %s\n", strings.Join(names, ", "))

	// Merge all packs.
	merged, err := mergePacks(packs)
	if err != nil {
		return err
	}

	// Apply merged read dirs.
	if len(merged.ReadDirs) > 0 {
		cfg.ReadDirs = append(cfg.ReadDirs, merged.ReadDirs...)
		fmt.Fprintf(os.Stderr, "hazmat: pack read dirs: %s\n",
			strings.Join(merged.ReadDirs, ", "))
	}

	if len(merged.Excludes) > 0 {
		seen := make(map[string]struct{}, len(cfg.BackupExcludes))
		for _, pat := range cfg.BackupExcludes {
			seen[pat] = struct{}{}
		}
		var added []string
		for _, pat := range merged.Excludes {
			if _, dup := seen[pat]; dup {
				continue
			}
			cfg.BackupExcludes = append(cfg.BackupExcludes, pat)
			added = append(added, pat)
			seen[pat] = struct{}{}
		}
		if len(added) > 0 {
			fmt.Fprintf(os.Stderr, "hazmat: pack snapshot excludes: %s\n",
				strings.Join(added, ", "))
		}
	}

	// Apply env passthrough.
	cfg.PackEnv = merged.EnvPassthrough
	cfg.PackRegistryKeys = merged.RegistryKeys
	cfg.PackCredentialKeys = merged.CredentialKeys

	// Surface registry-redirect keys as a residual risk notice.
	if len(merged.RegistryKeys) > 0 {
		fmt.Fprintf(os.Stderr, "hazmat: note: pack passes registry URLs from invoker env: %s\n",
			strings.Join(merged.RegistryKeys, ", "))
	}

	// Surface credential-token keys with a stronger warning.
	if len(merged.CredentialKeys) > 0 {
		fmt.Fprintf(os.Stderr, "hazmat: warning: pack passes credential tokens from invoker env: %s\n",
			strings.Join(merged.CredentialKeys, ", "))
		fmt.Fprintf(os.Stderr, "hazmat: the agent can act as you on services these tokens authenticate to\n")
	}

	// Print warnings.
	for _, w := range merged.Warnings {
		fmt.Fprintf(os.Stderr, "hazmat: pack warning: %s\n", w)
	}

	return nil
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
		ProjectDir:     projectDir,
		ReadDirs:       readDirs,
		BackupExcludes: snapshotIgnoreRules(nil),
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

type dockerProjectDetection struct {
	HardMarkers []string
	SoftMarkers []string
}

func (d dockerProjectDetection) HasHardMarkers() bool {
	return len(d.HardMarkers) > 0
}

func (d dockerProjectDetection) HasSoftMarkers() bool {
	return len(d.SoftMarkers) > 0
}

func (d dockerProjectDetection) markers() []string {
	out := make([]string, 0, len(d.HardMarkers)+len(d.SoftMarkers))
	out = append(out, d.HardMarkers...)
	out = append(out, d.SoftMarkers...)
	return out
}

func detectDockerProject(projectDir string) dockerProjectDetection {
	var detection dockerProjectDetection
	for _, name := range dockerArtifacts {
		if _, err := os.Stat(filepath.Join(projectDir, name)); err == nil {
			detection.HardMarkers = append(detection.HardMarkers, name)
		}
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".devcontainer")); err == nil {
		detection.SoftMarkers = append(detection.SoftMarkers, ".devcontainer/")
	}
	return detection
}

func dockerTier3Example(commandName, projectDir string) string {
	switch commandName {
	case "shell":
		return fmt.Sprintf("hazmat shell --sandbox -C %s", projectDir)
	case "exec":
		return fmt.Sprintf("hazmat exec --sandbox -C %s -- <command>", projectDir)
	default:
		return fmt.Sprintf("hazmat claude --sandbox -C %s", projectDir)
	}
}

func dockerProjectBlockedMessage(commandName, projectDir string, detection dockerProjectDetection) string {
	return strings.TrimLeft(fmt.Sprintf(`
Docker artifacts detected in %s: %s

Docker-capable sessions require healthy Docker Sandbox support.

  hazmat sandbox doctor             Verify Docker Sandbox support
  %s   Use isolated Docker support
  hazmat sandbox setup              Record Docker Sandbox support in advance (optional)
  hazmat %s --ignore-docker         Continue without Docker support
`,
		projectDir,
		strings.Join(detection.markers(), ", "),
		dockerTier3Example(commandName, projectDir),
		commandName,
	), "\n")
}

func dockerProjectAdvisoryMessage(commandName, projectDir string, detection dockerProjectDetection) string {
	return strings.TrimLeft(fmt.Sprintf(`
Container workflow metadata detected in %s: %s

Hazmat is continuing in the current containment mode because .devcontainer/
alone does not force Docker Sandbox routing.

  %s   Use Docker Sandboxes explicitly if this session needs Docker
`,
		projectDir,
		strings.Join(detection.markers(), ", "),
		dockerTier3Example(commandName, projectDir),
	), "\n")
}

func resolveSessionSandboxMode(commandName, projectDir string, requestedSandbox, allowDocker bool) (bool, error) {
	if requestedSandbox {
		return true, nil
	}

	detection := detectDockerProject(projectDir)
	if detection.HasHardMarkers() {
		if allowDocker {
			fmt.Fprintln(os.Stderr, "Warning:", dockerProjectBlockedMessage(commandName, projectDir, detection))
			return false, nil
		}

		cfg, err := loadConfig()
		if err != nil {
			return false, err
		}

		if cfg.SandboxBackend() == nil {
			if _, _, _, err := detectHealthySandboxBackend(sandboxProbeFactory()); err != nil {
				return false, fmt.Errorf("%s", dockerProjectBlockedMessage(commandName, projectDir, detection))
			}
			fmt.Fprintf(os.Stderr, "hazmat: Docker artifacts detected: %s\n", strings.Join(detection.HardMarkers, ", "))
			fmt.Fprintf(os.Stderr, "hazmat: auto-routing %s into Docker Sandboxes\n", commandName)
			return true, nil
		}

		fmt.Fprintf(os.Stderr, "hazmat: Docker artifacts detected: %s\n", strings.Join(detection.HardMarkers, ", "))
		fmt.Fprintf(os.Stderr, "hazmat: auto-routing %s into Docker Sandboxes\n", commandName)
		return true, nil
	}

	if detection.HasSoftMarkers() {
		fmt.Fprintln(os.Stderr, "Warning:", dockerProjectAdvisoryMessage(commandName, projectDir, detection))
	}

	return false, nil
}

// warnDockerProject checks whether projectDir contains Docker artifacts and
// either returns an error (allow=false) or prints a warning and continues
// (allow=true) with Docker Sandbox guidance. The host Docker socket is locked to
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
	detection := detectDockerProject(projectDir)
	if !detection.HasHardMarkers() {
		if detection.HasSoftMarkers() {
			fmt.Fprintln(os.Stderr, "Warning:", dockerProjectAdvisoryMessage("claude", projectDir, detection))
		}
		return nil
	}

	msg := dockerProjectBlockedMessage("claude", projectDir, detection)
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

	// Ancestor metadata: tools like git need to stat() each ancestor directory
	// when resolving canonical paths (strbuf_realpath). Without this, git can't
	// verify safe.directory matches and readlink(1) returns truncated paths.
	// file-read-metadata allows stat/lstat/getattr but NOT open/read/readdir,
	// so no directory contents or file data are exposed.
	ancestors := make(map[string]struct{})
	hostPaths := append([]string{cfg.ProjectDir}, cfg.ReadDirs...)
	for _, dir := range hostPaths {
		for p := filepath.Dir(dir); p != "/" && p != "."; p = filepath.Dir(p) {
			ancestors[p] = struct{}{}
		}
	}
	if len(ancestors) > 0 {
		w(";; ── Ancestor metadata (stat only, no content) ────────────────────────────\n")
		w(";; Required for path canonicalization by git, readlink, etc.\n")
		for p := range ancestors {
			w("(allow file-read-metadata (literal %q))\n", p)
		}
		w("\n")
	}

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
		"com.apple.trustd",                                // TLS certificate verification (Go, curl, Python, etc.)
		"com.apple.system.opendirectoryd.api",             // user/group directory lookups
		"com.apple.system.opendirectoryd.libinfo",         // getpwuid/getgrnam via libinfo (needed by git, id, etc.)
		"com.apple.system.DirectoryService.libinfo_v1",    // getpwuid/getgrnam legacy path
		"com.apple.system.DirectoryService.membership_v1", // group membership checks
		"com.apple.pboard",                                // pasteboard (clipboard read/write — paste into Claude Code and copy out)
	} {
		w("(allow mach-lookup (global-name %q))\n", svc)
	}
	w("(allow mach-host*)\n\n")

	w(";; ── Pasteboard shared memory (clipboard copy out of session) ───────────────\n")
	w(";; mach-lookup for com.apple.pboard covers the IPC handshake; the actual\n")
	w(";; clipboard data is transferred via POSIX shared memory segments named\n")
	w(";; com.apple.pasteboard.<N>.  Without these rules pbcopy silently fails.\n")
	w("(allow ipc-posix-shm-read-data    (ipc-posix-name-regex #\"^com\\.apple\\.pasteboard\\.\"))\n")
	w("(allow ipc-posix-shm-write-data   (ipc-posix-name-regex #\"^com\\.apple\\.pasteboard\\.\"))\n")
	w("(allow ipc-posix-shm-write-create (ipc-posix-name-regex #\"^com\\.apple\\.pasteboard\\.\"))\n\n")

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

func runAgentSeatbeltScript(cfg sessionConfig, script string, args ...string) error {
	return runAgentSeatbeltScriptWithUI(cfg, sessionLaunchUI{showStatusBar: true}, script, args...)
}

func applyStatusBarConfig(ui sessionLaunchUI, cfg HazmatConfig) sessionLaunchUI {
	if !cfg.StatusBar() {
		ui.showStatusBar = false
		ui.waitForAltScreen = false
	}
	return ui
}

func runAgentSeatbeltScriptWithUI(cfg sessionConfig, ui sessionLaunchUI, script string, args ...string) error {
	if hcfg, err := loadConfig(); err == nil {
		ui = applyStatusBarConfig(ui, hcfg)
	}

	pid := os.Getpid()

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

	var (
		barOnce     sync.Once
		barTeardown = func() {}
	)
	startBar := func() {
		barOnce.Do(func() {
			bar := newStatusBar(cfg.ActivePacks, cfg.ProjectDir)
			barTeardown = bar.Start()
		})
	}

	if ui.showStatusBar {
		startBar()
	}

	var (
		cmd            *exec.Cmd
		transcriptPath string
		watchStop      chan struct{}
		watchDone      chan struct{}
	)
	if ui.waitForAltScreen && term.IsTerminal(int(os.Stderr.Fd())) {
		f, err := os.CreateTemp("", "hazmat-resume-*.typescript")
		if err == nil {
			transcriptPath = f.Name()
			_ = f.Close()
			if err := os.Chmod(transcriptPath, 0o600); err != nil {
				_ = os.Remove(transcriptPath)
				transcriptPath = ""
			}
		}
	}
	if transcriptPath != "" {
		scriptArgs := append([]string{"-q", transcriptPath, "sudo"}, full...)
		cmd = exec.Command("script", scriptArgs...)
		watchStop = make(chan struct{})
		watchDone = make(chan struct{})
		go func() {
			defer close(watchDone)
			watchTranscriptForAltScreen(transcriptPath, startBar, watchStop)
		}()
	} else {
		cmd = exec.Command("sudo", full...)
	}

	defer func() {
		if watchStop != nil {
			close(watchStop)
			<-watchDone
		}
		if transcriptPath != "" {
			_ = os.Remove(transcriptPath)
		}
		barTeardown()
	}()

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if ui.clearScreen && term.IsTerminal(int(os.Stderr.Fd())) {
		fmt.Fprint(os.Stderr, "\033[2J\033[H")
	}

	return cmd.Run()
}

// preSessionSnapshot takes an automatic snapshot before a session starts.
// Warns on failure but never blocks the session.
func preSessionSnapshot(cfg sessionConfig, command string, skip bool) {
	if skip {
		return
	}
	start := time.Now()
	fmt.Fprintf(os.Stderr, "  Snapshot: %s ... ", cfg.ProjectDir)
	if err := snapshotProject(cfg.ProjectDir, command, cfg.BackupExcludes...); err != nil {
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

	// Pack env passthrough: passive path pointers and selectors resolved
	// from the invoker's environment. Only keys in safeEnvKeys are allowed;
	// validation happens at pack load time.
	for key, val := range cfg.PackEnv {
		pairs = append(pairs, key+"="+val)
	}

	return pairs
}
