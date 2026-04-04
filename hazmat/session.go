package main

import (
	"bytes"
	"encoding/json"
	"errors"
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
	ProjectDir              string
	ReadDirs                []string
	WriteDirs               []string
	UserReadDirs            []string // explicit host-configured or CLI read-only extensions
	AutoReadDirs            []string // automatically added read-only dirs from integrations/defaults
	BackupExcludes          []string
	IntegrationEnv          map[string]string // from integration env_passthrough (resolved values)
	IntegrationRegistryKeys []string          // active registry-redirect env keys (for UX)
	IntegrationExcludes     []string          // snapshot excludes added by active integrations
	IntegrationSources      []string          // provenance for runtime-resolved integration inputs
	IntegrationDetails      []string          // detailed runtime resolution notes for explain/show flows
	IntegrationWarnings     []string          // warnings surfaced by active integrations
	ActiveIntegrations      []string          // integration names, for status bar
	ServiceAccess           []string          // explicit external-service access granted to session
	RoutingReason           string            // plain-language explanation for the chosen mode
	SessionNotes            []string          // plain-language notes about session behavior
}

type sessionLaunchUI struct {
	clearScreen      bool
	showStatusBar    bool
	waitForAltScreen bool
}

type dockerMode string

const (
	dockerModeAuto    dockerMode = "auto"
	dockerModeNone    dockerMode = "none"
	dockerModeSandbox dockerMode = "sandbox"
)

type dockerRequestSource string

const (
	dockerRequestDefaultAuto   dockerRequestSource = "default-auto"
	dockerRequestProjectConfig dockerRequestSource = "project-config"
	dockerRequestFlag          dockerRequestSource = "flag"
	dockerRequestLegacyIgnore  dockerRequestSource = "legacy-ignore"
	dockerRequestLegacySandbox dockerRequestSource = "legacy-sandbox"
)

type dockerRoutingRequest struct {
	Mode   dockerMode
	Source dockerRequestSource
}

type sessionMode string

const (
	sessionModeNative        sessionMode = "native"
	sessionModeDockerSandbox sessionMode = "docker-sandbox"
)

type preparedSession struct {
	Config sessionConfig
	Mode   sessionMode
}

func (m sessionMode) label() string {
	switch m {
	case sessionModeDockerSandbox:
		return "Docker Sandbox"
	default:
		return "Native containment"
	}
}

func runSessionPreflight(cfg sessionConfig) error {
	if ensureProjectWritable(cfg.ProjectDir) {
		fmt.Fprintln(os.Stderr, "  Fixed project permissions for agent access")
	}
	exposedDirs := append(append([]string{}, cfg.ReadDirs...), cfg.WriteDirs...)
	if fixed, failures := ensureAgentCanTraverseExposedDirs(cfg.ProjectDir, exposedDirs); len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "  Warning: could not fully prepare exposed directories: %s\n", failures[0])
	} else if fixed {
		fmt.Fprintln(os.Stderr, "  Fixed exposed directory traversal for agent access")
	}
	if fixed, err := ensureGitMetadataHealthy(cfg.ProjectDir); err != nil {
		return err
	} else if fixed {
		fmt.Fprintln(os.Stderr, "  Fixed Git metadata permissions for collaborative access")
	}
	return nil
}

func newShellCmd() *cobra.Command {
	var project string
	var readDirs []string
	var writeDirs []string
	var integrationNames []string
	var noBackup bool
	var useSandbox bool
	var allowDocker bool
	var dockerModeValue string
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Open a contained shell as the agent user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			prepared, err := resolvePreparedSession("shell", harnessSessionOpts{
				project:            project,
				readDirs:           readDirs,
				writeDirs:          writeDirs,
				integrations:       integrationNames,
				noBackup:           noBackup,
				useSandbox:         useSandbox,
				allowDocker:        allowDocker,
				dockerMode:         dockerModeValue,
				dockerModeExplicit: cmd.Flags().Changed("docker"),
			}, true)
			if err != nil {
				return err
			}
			if err := beginPreparedSession(prepared, "shell", noBackup, false); err != nil {
				return err
			}
			if prepared.Mode == sessionModeDockerSandbox {
				return runSandboxShellSession(prepared.Config)
			}
			return runAgentSeatbeltScript(prepared.Config,
				`cd "$SANDBOX_PROJECT_DIR" && exec /bin/zsh -il`)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	cmd.Flags().StringArrayVarP(&writeDirs, "write", "W", nil,
		"Read-write directory to expose to the agent (repeatable)")
	cmd.Flags().StringArrayVar(&integrationNames, "integration", nil,
		"Activate a session integration (repeatable, e.g. --integration go)")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false,
		"Skip pre-session snapshot")
	cmd.Flags().StringVar(&dockerModeValue, "docker", string(dockerModeAuto),
		"Docker routing: auto, none, or sandbox")
	cmd.Flags().BoolVar(&useSandbox, "sandbox", false,
		"Run with Docker Sandbox support")
	cmd.Flags().BoolVar(&allowDocker, "ignore-docker", false,
		"Continue without Docker support even if Docker markers are present")
	cmd.SetFlagErrorFunc(legacyIntegrationFlagError)
	_ = cmd.Flags().MarkDeprecated("sandbox", "use --docker=sandbox")
	_ = cmd.Flags().MarkDeprecated("ignore-docker", "use --docker=none")
	return cmd
}

func newExecCmd() *cobra.Command {
	var project string
	var readDirs []string
	var writeDirs []string
	var integrationNames []string
	var noBackup bool
	var useSandbox bool
	var allowDocker bool
	var dockerModeValue string
	cmd := &cobra.Command{
		Use:   "exec [flags] <command> [args...]",
		Short: "Run a command in containment as the agent user",
		Long: `Run a command in containment as the agent user.

Use -- before the command when the command itself takes flags. Without the
separator, Cobra may try to parse the forwarded flags as hazmat flags.

Examples:
  hazmat exec make test
  hazmat exec -- npm test
  hazmat exec -C ~/workspace/app -- /bin/zsh -lc 'uv run pytest -q'
  hazmat exec --docker=none -C ~/workspace/app -- /bin/zsh -lc 'cd frontend && npm run build'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prepared, err := resolvePreparedSession("exec", harnessSessionOpts{
				project:            project,
				readDirs:           readDirs,
				writeDirs:          writeDirs,
				integrations:       integrationNames,
				noBackup:           noBackup,
				useSandbox:         useSandbox,
				allowDocker:        allowDocker,
				dockerMode:         dockerModeValue,
				dockerModeExplicit: cmd.Flags().Changed("docker"),
			}, true)
			if err != nil {
				return err
			}
			if err := beginPreparedSession(prepared, "exec", noBackup, false); err != nil {
				return err
			}
			if prepared.Mode == sessionModeDockerSandbox {
				return runSandboxExecSession(prepared.Config, args)
			}
			return runAgentSeatbeltScript(prepared.Config,
				`cd "$SANDBOX_PROJECT_DIR" && exec "$@"`, args...)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", "",
		"Writable project directory (defaults to current directory)")
	cmd.Flags().StringArrayVarP(&readDirs, "read", "R", nil,
		"Read-only directory to expose to the agent (repeatable)")
	cmd.Flags().StringArrayVarP(&writeDirs, "write", "W", nil,
		"Read-write directory to expose to the agent (repeatable)")
	cmd.Flags().StringArrayVar(&integrationNames, "integration", nil,
		"Activate a session integration (repeatable, e.g. --integration go)")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false,
		"Skip pre-session snapshot")
	cmd.Flags().StringVar(&dockerModeValue, "docker", string(dockerModeAuto),
		"Docker routing: auto, none, or sandbox")
	cmd.Flags().BoolVar(&useSandbox, "sandbox", false,
		"Run with Docker Sandbox support")
	cmd.Flags().BoolVar(&allowDocker, "ignore-docker", false,
		"Continue without Docker support even if Docker markers are present")
	cmd.SetFlagErrorFunc(legacyIntegrationFlagError)
	_ = cmd.Flags().MarkDeprecated("sandbox", "use --docker=sandbox")
	_ = cmd.Flags().MarkDeprecated("ignore-docker", "use --docker=none")
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
  -W, --write <dir>      Read-write directory (repeatable)
  --integration <name>   Activate a session integration (repeatable)
  --no-backup            Skip pre-session snapshot
  --docker <mode>        Docker routing: auto, none, or sandbox
  --sandbox              Alias for --docker=sandbox
  --ignore-docker        Alias for --docker=none (deprecated)

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
  hazmat claude --docker=sandbox -C /proj  Use Docker Sandboxes
  hazmat claude --docker=none -C /proj     Code-only session in native containment
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

			prepared, err := resolvePreparedSession("claude", opts, true)
			if err != nil {
				return err
			}
			if err := beginPreparedSession(prepared, "claude", opts.noBackup, false); err != nil {
				return err
			}

			if prepared.Mode == sessionModeDockerSandbox {
				return runSandboxClaudeSession(prepared.Config, forwarded)
			}

			// Sync sessions for --resume / --continue.
			// Copies the invoking user's session files into the agent's
			// config so Claude Code can discover and resume them without
			// reading the host transcript directory in place.
			wantsResume, resumeTarget, wantsContinue := detectResumeFlags(forwarded)
			if wantsResume || wantsContinue {
				if err := syncResumeSession(prepared.Config.ProjectDir, resumeTarget, wantsContinue); err != nil {
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

			return runAgentSeatbeltScriptWithUI(prepared.Config, claudeLaunchUI(forwarded),
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
  -W, --write <dir>      Read-write directory (repeatable)
  --integration <name>   Activate a session integration (repeatable)
  --no-backup            Skip pre-session snapshot
  --docker <mode>        Docker routing: auto, none, or sandbox
  --ignore-docker        Alias for --docker=none (deprecated)

All other flags and arguments are forwarded to OpenCode.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root.
Docker Sandbox sessions are currently available through hazmat claude; use
--docker=none here for code-only sessions in Docker-marked repos.

Examples:
  hazmat opencode
  hazmat opencode -p "explain this"
  hazmat opencode -C /proj -p "hi"
  hazmat opencode --docker=none -C /proj
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

			prepared, err := resolvePreparedSession("opencode", opts, false)
			if err != nil {
				return err
			}
			if err := beginPreparedSession(prepared, "opencode", opts.noBackup, true); err != nil {
				return err
			}
			return runAgentSeatbeltScript(prepared.Config, openCodeLaunchScript(), forwarded...)
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
  -W, --write <dir>      Read-write directory (repeatable)
  --integration <name>   Activate a session integration (repeatable)
  --no-backup            Skip pre-session snapshot
  --docker <mode>        Docker routing: auto, none, or sandbox
  --ignore-docker        Alias for --docker=none (deprecated)

All other flags and arguments are forwarded to Codex.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root.
Docker Sandbox sessions are currently available through hazmat claude; use
--docker=none here for code-only sessions in Docker-marked repos.

Examples:
  hazmat codex
  hazmat codex "explain this repo"
  hazmat codex --docker=none -C /proj
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

			prepared, err := resolvePreparedSession("codex", opts, false)
			if err != nil {
				return err
			}
			if err := beginPreparedSession(prepared, "codex", opts.noBackup, true); err != nil {
				return err
			}

			// Hazmat provides the primary containment boundary here, so when
			// session.skip_permissions is enabled we bypass Codex's own
			// approval prompts and sandbox layer.
			if hcfg, _ := loadConfig(); hcfg.SkipPermissions() {
				forwarded = append(codexSkipPermissionsArgs(), forwarded...)
			}

			return runAgentSeatbeltScript(prepared.Config, codexLaunchScript(), forwarded...)
		},
	}
	return cmd
}

func codexSkipPermissionsArgs() []string {
	return []string{"--dangerously-bypass-approvals-and-sandbox"}
}

// harnessSessionOpts holds hazmat-specific flags extracted from a harness
// command line before forwarding the rest to the harness CLI.
type harnessSessionOpts struct {
	project            string
	readDirs           []string
	writeDirs          []string
	integrations       []string
	noBackup           bool
	useSandbox         bool
	allowDocker        bool
	dockerMode         string
	dockerModeExplicit bool
}

type claudeOpts = harnessSessionOpts

var errHarnessHelp = fmt.Errorf("help requested")
var errClaudeHelp = errHarnessHelp

func legacyIntegrationFlagError(_ *cobra.Command, err error) error {
	if err != nil && strings.Contains(err.Error(), "--pack") {
		return fmt.Errorf("--pack was removed before v1; use --integration")
	}
	return err
}

// parseHarnessArgs separates hazmat flags from a forwarded harness CLI.
// Hazmat flags (--project, --read, --write, --integration, --no-backup,
// --docker, --sandbox, --ignore-docker)
// are extracted; everything else is returned as forwarded args.
func parseHarnessArgs(args []string) (harnessSessionOpts, []string, error) {
	var opts harnessSessionOpts
	var forwarded []string
	nextValue := func(i *int, flag, want string) (string, error) {
		if *i+1 >= len(args) {
			return "", fmt.Errorf("%s requires %s", flag, want)
		}
		*i++
		return args[*i], nil
	}

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
		case arg == "--docker":
			value, err := nextValue(&i, arg, "a mode (auto, none, sandbox)")
			if err != nil {
				return opts, nil, err
			}
			opts.dockerMode = value
			opts.dockerModeExplicit = true
		case strings.HasPrefix(arg, "--docker="):
			opts.dockerMode = arg[len("--docker="):]
			opts.dockerModeExplicit = true
		case arg == "--sandbox":
			opts.useSandbox = true
		case arg == "--ignore-docker":
			opts.allowDocker = true
		case arg == "--project" || arg == "-C":
			value, err := nextValue(&i, arg, "a directory argument")
			if err != nil {
				return opts, nil, err
			}
			opts.project = value
		case strings.HasPrefix(arg, "--project="):
			opts.project = arg[len("--project="):]
		case arg == "--read" || arg == "-R":
			value, err := nextValue(&i, arg, "a directory argument")
			if err != nil {
				return opts, nil, err
			}
			opts.readDirs = append(opts.readDirs, value)
		case strings.HasPrefix(arg, "--read="):
			opts.readDirs = append(opts.readDirs, arg[len("--read="):])
		case arg == "--write" || arg == "-W":
			value, err := nextValue(&i, arg, "a directory argument")
			if err != nil {
				return opts, nil, err
			}
			opts.writeDirs = append(opts.writeDirs, value)
		case strings.HasPrefix(arg, "--write="):
			opts.writeDirs = append(opts.writeDirs, arg[len("--write="):])
		case arg == "--integration":
			value, err := nextValue(&i, arg, "an integration name")
			if err != nil {
				return opts, nil, err
			}
			opts.integrations = append(opts.integrations, value)
		case strings.HasPrefix(arg, "--integration="):
			opts.integrations = append(opts.integrations, arg[len("--integration="):])
		case arg == "--pack" || strings.HasPrefix(arg, "--pack="):
			return opts, nil, fmt.Errorf("--pack was removed before v1; use --integration")
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

// applyIntegrations resolves, validates, and merges active integrations into
// the session config.
func applyIntegrations(cfg *sessionConfig, integrationFlags []string) error {
	integrations, err := resolveActiveIntegrations(integrationFlags, cfg.ProjectDir)
	if err != nil {
		return err
	}

	// Detect and suggest integrations if none are active.
	activeNames := make(map[string]struct{}, len(integrations))
	for _, spec := range integrations {
		activeNames[spec.Meta.Name] = struct{}{}
	}
	if suggestions := suggestIntegrations(cfg.ProjectDir, activeNames); len(suggestions) > 0 {
		fmt.Fprintf(os.Stderr, "hazmat: suggested integrations: %s (activate with --integration <name>)\n",
			strings.Join(suggestions, ", "))
	}

	if len(integrations) == 0 {
		return nil
	}

	names := make([]string, 0, len(integrations))
	for _, spec := range integrations {
		names = append(names, spec.Meta.Name)
	}
	cfg.ActiveIntegrations = names

	resolved, err := resolveRuntimeIntegrations(cfg.ProjectDir, integrations)
	if err != nil {
		return err
	}

	// Merge all integrations.
	merged, err := mergeResolvedIntegrations(resolved)
	if err != nil {
		return err
	}

	// Apply merged read dirs.
	if len(merged.ReadDirs) > 0 {
		var added []string
		cfg.ReadDirs, added = appendUniqueDirs(cfg.ReadDirs, merged.ReadDirs)
		cfg.AutoReadDirs, _ = appendUniqueDirs(cfg.AutoReadDirs, added)
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
		cfg.IntegrationExcludes = added
	}

	// Apply env passthrough.
	cfg.IntegrationEnv = merged.EnvPassthrough
	cfg.IntegrationRegistryKeys = merged.RegistryKeys
	cfg.IntegrationWarnings = append([]string(nil), merged.Warnings...)
	cfg.IntegrationSources = cfg.IntegrationSources[:0]
	cfg.IntegrationDetails = cfg.IntegrationDetails[:0]
	sourceSeen := make(map[string]struct{}, len(resolved))
	detailSeen := make(map[string]struct{})
	for _, integration := range resolved {
		if integration.Source != "" {
			if _, dup := sourceSeen[integration.Source]; !dup {
				cfg.IntegrationSources = append(cfg.IntegrationSources, integration.Source)
				sourceSeen[integration.Source] = struct{}{}
			}
		}
		for _, detail := range integration.Details {
			if _, dup := detailSeen[detail]; dup {
				continue
			}
			cfg.IntegrationDetails = append(cfg.IntegrationDetails, detail)
			detailSeen[detail] = struct{}{}
		}
	}

	return nil
}

func appendUniqueDirs(existing, additions []string) ([]string, []string) {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, dir := range existing {
		seen[dir] = struct{}{}
	}

	merged := append([]string(nil), existing...)
	var added []string
	for _, dir := range additions {
		if _, dup := seen[dir]; dup {
			continue
		}
		seen[dir] = struct{}{}
		merged = append(merged, dir)
		added = append(added, dir)
	}
	return merged, added
}

func resolveSessionConfig(project string, readPaths, writePaths []string) (sessionConfig, error) {
	projectDir, err := resolveDir(project, true)
	if err != nil {
		return sessionConfig{}, fmt.Errorf("project: %w", err)
	}
	if isCredentialDenyPath(projectDir) {
		return sessionConfig{}, fmt.Errorf("project dir %q resolves to credential deny zone", projectDir)
	}

	readDirs, err := resolveReadDirs(readPaths)
	if err != nil {
		return sessionConfig{}, err
	}
	for _, dir := range readDirs {
		if isCredentialDenyPath(dir) {
			return sessionConfig{}, fmt.Errorf("read dir %q resolves to credential deny zone", dir)
		}
	}
	writeDirs, err := resolveReadDirs(writePaths)
	if err != nil {
		return sessionConfig{}, fmt.Errorf("write dirs: %w", err)
	}
	for _, dir := range writeDirs {
		if isCredentialDenyPath(dir) {
			return sessionConfig{}, fmt.Errorf("write dir %q resolves to credential deny zone", dir)
		}
	}

	return sessionConfig{
		ProjectDir:     projectDir,
		ReadDirs:       readDirs,
		WriteDirs:      writeDirs,
		BackupExcludes: snapshotIgnoreRules(nil),
	}, nil
}

func resolvePreparedSession(commandName string, opts harnessSessionOpts, supportsSandbox bool) (preparedSession, error) {
	projectDir, err := resolveDir(opts.project, true)
	if err != nil {
		return preparedSession{}, err
	}

	userReadPaths := configuredReadDirs(opts.readDirs)
	projectReadPaths, projectWritePaths := configuredProjectAccess(projectDir)
	userReadPaths = append(append([]string{}, projectReadPaths...), userReadPaths...)
	writePaths := append(append([]string{}, projectWritePaths...), opts.writeDirs...)
	autoReadPaths := subtractResolvedDirs(implicitReadDirs(), userReadPaths)
	allReadPaths := append(append([]string{}, userReadPaths...), autoReadPaths...)

	cfg, err := resolveSessionConfig(projectDir, allReadPaths, writePaths)
	if err != nil {
		return preparedSession{}, err
	}
	cfg.UserReadDirs, err = resolveReadDirs(userReadPaths)
	if err != nil {
		return preparedSession{}, err
	}
	cfg.AutoReadDirs, err = resolveReadDirs(autoReadPaths)
	if err != nil {
		return preparedSession{}, err
	}
	if err := applyIntegrations(&cfg, opts.integrations); err != nil {
		return preparedSession{}, err
	}

	request, err := resolveDockerRoutingRequest(cfg.ProjectDir, opts)
	if err != nil {
		return preparedSession{}, err
	}
	detection := detectDockerProject(cfg.ProjectDir)

	mode, err := resolvePreparedSessionMode(commandName, cfg.ProjectDir, request, detection, supportsSandbox)
	if err != nil {
		return preparedSession{}, err
	}

	cfg.RoutingReason, cfg.SessionNotes = sessionRoutingExplanation(commandName, cfg.ProjectDir, request, detection, mode)
	return preparedSession{Config: cfg, Mode: mode}, nil
}

func resolvePreparedSessionMode(commandName, projectDir string, request dockerRoutingRequest, detection dockerProjectDetection, supportsSandbox bool) (sessionMode, error) {
	if supportsSandbox {
		useSandbox, err := resolveSessionSandboxMode(commandName, projectDir, request, detection)
		if err != nil {
			return "", err
		}
		if useSandbox {
			return sessionModeDockerSandbox, nil
		}
		return sessionModeNative, nil
	}

	if request.Mode == dockerModeSandbox {
		return "", fmt.Errorf("%s", unsupportedSandboxTargetMessage(commandName, projectDir, request))
	}
	if err := warnDockerProject(commandName, projectDir, request); err != nil {
		return "", err
	}
	return sessionModeNative, nil
}

func beginPreparedSession(prepared preparedSession, commandName string, skipSnapshot, preflightBeforeSnapshot bool) error {
	printSessionContract(prepared.Config, prepared.Mode, skipSnapshot)
	if prepared.Mode != sessionModeNative {
		preSessionSnapshot(prepared.Config, commandName, skipSnapshot)
		return nil
	}

	if preflightBeforeSnapshot {
		if err := runSessionPreflight(prepared.Config); err != nil {
			return err
		}
	}

	preSessionSnapshot(prepared.Config, commandName, skipSnapshot)

	if !preflightBeforeSnapshot {
		if err := runSessionPreflight(prepared.Config); err != nil {
			return err
		}
	}
	return nil
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

var terminalEnvPassthroughKeys = []string{
	"TERM",
	"COLORTERM",
	"LANG",
	"LC_ALL",
	"LC_CTYPE",
	"TERM_PROGRAM",
	"TERM_PROGRAM_VERSION",
}

func terminalCapabilitySupport(home string, getenv func(string) string) ([]string, []string) {
	var envPairs []string
	for _, key := range terminalEnvPassthroughKeys {
		if value := getenv(key); value != "" {
			envPairs = append(envPairs, key+"="+value)
		}
	}

	terminfo := getenv("TERMINFO")
	if terminfo != "" {
		envPairs = append(envPairs, "TERMINFO="+terminfo)
	}

	terminfoDirs := getenv("TERMINFO_DIRS")
	if home != "" && terminfo == "" {
		userTerminfoDir := filepath.Join(home, ".terminfo")
		if dirExists(userTerminfoDir) && !pathListContains(terminfoDirs, userTerminfoDir) {
			if terminfoDirs == "" {
				// Keep the system default search paths available after the custom dir.
				terminfoDirs = userTerminfoDir + string(os.PathListSeparator)
			} else {
				terminfoDirs = userTerminfoDir + string(os.PathListSeparator) + terminfoDirs
			}
		}
	}
	if terminfoDirs != "" {
		envPairs = append(envPairs, "TERMINFO_DIRS="+terminfoDirs)
	}

	var readDirs []string
	readDirs = appendResolvedDirIfExists(readDirs, terminfo)
	for _, dir := range filepath.SplitList(terminfoDirs) {
		readDirs = appendResolvedDirIfExists(readDirs, dir)
	}

	return envPairs, readDirs
}

func pathListContains(list, target string) bool {
	for _, entry := range filepath.SplitList(list) {
		if entry == target {
			return true
		}
	}
	return false
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func appendResolvedDirIfExists(dirs []string, path string) []string {
	if path == "" {
		return dirs
	}
	resolved, err := resolveDir(path, false)
	if err != nil {
		return dirs
	}
	for _, existing := range dirs {
		if existing == resolved {
			return dirs
		}
	}
	return append(dirs, resolved)
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
	if home, err := os.UserHomeDir(); err == nil {
		_, terminalReadDirs := terminalCapabilitySupport(home, os.Getenv)
		dirs = append(dirs, terminalReadDirs...)
	}
	// Future: Rust (~/.cargo/registry), Maven (~/.m2/repository), etc.
	return dirs
}

// configuredReadDirs prepends configured session.read_dirs to the user-supplied
// read dirs, skipping any that don't exist on disk and deduplicating against
// what the user already passed via -R.
func configuredReadDirs(explicit []string) []string {
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
	return append(added, explicit...)
}

// defaultReadDirs keeps the existing behavior for callers/tests that want the
// full default read-only set, including implicit toolchain directories.
func defaultReadDirs(explicit []string) []string {
	configured := configuredReadDirs(explicit)
	implicit := subtractResolvedDirs(implicitReadDirs(), configured)
	return append(configured, implicit...)
}

func configuredProjectAccess(projectDir string) ([]string, []string) {
	cfg, _ := loadConfig()
	return cfg.ProjectReadDirs(projectDir), cfg.ProjectWriteDirs(projectDir)
}

func subtractResolvedDirs(candidates, existing []string) []string {
	existingResolved := make(map[string]struct{}, len(existing))
	for _, dir := range existing {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			continue
		}
		existingResolved[resolved] = struct{}{}
	}

	var filtered []string
	seen := make(map[string]struct{}, len(candidates))
	for _, dir := range candidates {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			continue
		}
		if _, dup := existingResolved[resolved]; dup {
			continue
		}
		if _, dup := seen[resolved]; dup {
			continue
		}
		seen[resolved] = struct{}{}
		filtered = append(filtered, dir)
	}
	return filtered
}

func renderSessionContract(cfg sessionConfig, mode sessionMode, skipSnapshot bool) string {
	var b strings.Builder

	fmt.Fprintln(&b, "hazmat: session")
	fmt.Fprintf(&b, "  Mode:                 %s\n", mode.label())
	if cfg.RoutingReason != "" {
		fmt.Fprintf(&b, "  Why this mode:        %s\n", cfg.RoutingReason)
	}
	fmt.Fprintf(&b, "  Project (read-write): %s\n", cfg.ProjectDir)
	fmt.Fprintf(&b, "  Integrations:         %s\n", sessionContractList(cfg.ActiveIntegrations))
	if len(cfg.IntegrationSources) > 0 {
		fmt.Fprintf(&b, "  Integration sources: %s\n", sessionContractList(cfg.IntegrationSources))
	}
	fmt.Fprintf(&b, "  Auto read-only:       %s\n", sessionContractList(cfg.AutoReadDirs))
	fmt.Fprintf(&b, "  Read-only extensions: %s\n", sessionContractList(cfg.UserReadDirs))
	fmt.Fprintf(&b, "  Read-write extensions: %s\n", sessionContractList(cfg.WriteDirs))
	fmt.Fprintf(&b, "  Service access:       %s\n", sessionContractList(cfg.ServiceAccess))
	if skipSnapshot {
		fmt.Fprintln(&b, "  Pre-session snapshot: skipped (--no-backup)")
	} else {
		fmt.Fprintln(&b, "  Pre-session snapshot: on")
	}
	if len(cfg.IntegrationExcludes) > 0 {
		fmt.Fprintf(&b, "  Snapshot excludes:    %s\n", strings.Join(cfg.IntegrationExcludes, ", "))
	}
	if len(cfg.IntegrationRegistryKeys) > 0 {
		fmt.Fprintf(&b, "  Invoker env passthrough: registry URLs via %s\n",
			strings.Join(cfg.IntegrationRegistryKeys, ", "))
	}
	if len(cfg.SessionNotes) > 0 {
		fmt.Fprintln(&b, "  Notes:")
		for _, note := range cfg.SessionNotes {
			fmt.Fprintf(&b, "    - %s\n", note)
		}
	}
	if len(cfg.IntegrationWarnings) > 0 {
		fmt.Fprintln(&b, "  Warnings:")
		for _, warning := range cfg.IntegrationWarnings {
			fmt.Fprintf(&b, "    - %s\n", warning)
		}
	}
	b.WriteByte('\n')
	return b.String()
}

func sessionContractList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func printSessionContract(cfg sessionConfig, mode sessionMode, skipSnapshot bool) {
	fmt.Fprint(os.Stderr, renderSessionContract(cfg, mode, skipSnapshot))
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
	HardMarkers         []string
	SoftMarkers         []string
	SharedDaemonSignals []string
}

func (d dockerProjectDetection) HasHardMarkers() bool {
	return len(d.HardMarkers) > 0
}

func (d dockerProjectDetection) HasSoftMarkers() bool {
	return len(d.SoftMarkers) > 0
}

func (d dockerProjectDetection) HasSharedDaemonSignals() bool {
	return len(d.SharedDaemonSignals) > 0
}

func (d dockerProjectDetection) markers() []string {
	out := make([]string, 0, len(d.HardMarkers)+len(d.SoftMarkers))
	out = append(out, d.HardMarkers...)
	out = append(out, d.SoftMarkers...)
	return out
}

type sharedDaemonSignalMatcher struct {
	Needle string
	Label  string
}

var sharedDaemonSignalMatchers = []sharedDaemonSignalMatcher{
	{Needle: "external: true", Label: "external Docker network or volume"},
	{Needle: "traefik.enable", Label: "Traefik Docker labels"},
	{Needle: "traefik.docker.network", Label: "Traefik Docker network"},
	{Needle: "network_mode: host", Label: "host networking"},
	{Needle: "shared-postgres", Label: "shared container reference"},
	{Needle: "docker-infra", Label: "other Compose project dependency"},
}

func validDockerMode(mode dockerMode) bool {
	switch mode {
	case dockerModeAuto, dockerModeNone, dockerModeSandbox:
		return true
	default:
		return false
	}
}

func parseDockerMode(raw string) (dockerMode, error) {
	mode := dockerMode(strings.ToLower(strings.TrimSpace(raw)))
	if validDockerMode(mode) {
		return mode, nil
	}
	return "", fmt.Errorf("invalid Docker mode %q (want auto, none, or sandbox)", raw)
}

func resolveDockerRoutingRequest(projectDir string, opts harnessSessionOpts) (dockerRoutingRequest, error) {
	if opts.dockerModeExplicit {
		if opts.useSandbox || opts.allowDocker {
			return dockerRoutingRequest{}, fmt.Errorf("cannot combine --docker with deprecated --sandbox/--ignore-docker flags")
		}
		mode, err := parseDockerMode(opts.dockerMode)
		if err != nil {
			return dockerRoutingRequest{}, err
		}
		return dockerRoutingRequest{Mode: mode, Source: dockerRequestFlag}, nil
	}

	if opts.useSandbox && opts.allowDocker {
		return dockerRoutingRequest{}, fmt.Errorf("cannot combine --sandbox and --ignore-docker")
	}
	if opts.useSandbox {
		return dockerRoutingRequest{Mode: dockerModeSandbox, Source: dockerRequestLegacySandbox}, nil
	}
	if opts.allowDocker {
		return dockerRoutingRequest{Mode: dockerModeNone, Source: dockerRequestLegacyIgnore}, nil
	}

	if cfg, err := loadConfig(); err == nil {
		if mode, ok := cfg.ProjectDockerMode(projectDir); ok {
			return dockerRoutingRequest{Mode: mode, Source: dockerRequestProjectConfig}, nil
		}
	}

	return dockerRoutingRequest{Mode: dockerModeAuto, Source: dockerRequestDefaultAuto}, nil
}

func detectDockerProject(projectDir string) dockerProjectDetection {
	var detection dockerProjectDetection
	for _, name := range dockerArtifacts {
		if _, err := os.Stat(filepath.Join(projectDir, name)); err == nil {
			detection.HardMarkers = append(detection.HardMarkers, name)
		}
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".devcontainer")); err == nil {
		if devcontainerNeedsDocker(projectDir) {
			detection.HardMarkers = append(detection.HardMarkers, ".devcontainer/")
		} else {
			detection.SoftMarkers = append(detection.SoftMarkers, ".devcontainer/")
		}
	}
	if detection.HasHardMarkers() {
		detection.SharedDaemonSignals = detectSharedDaemonSignals(projectDir)
	}
	return detection
}

func detectSharedDaemonSignals(projectDir string) []string {
	candidates := sharedDaemonCandidateFiles(projectDir)
	if len(candidates) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var signals []string
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		contents := string(data)
		rel, err := filepath.Rel(projectDir, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		for _, matcher := range sharedDaemonSignalMatchers {
			if !strings.Contains(contents, matcher.Needle) {
				continue
			}
			signal := fmt.Sprintf("%s in %s", matcher.Label, rel)
			if _, dup := seen[signal]; dup {
				continue
			}
			signals = append(signals, signal)
			seen[signal] = struct{}{}
		}
	}
	return signals
}

func sharedDaemonCandidateFiles(projectDir string) []string {
	seen := make(map[string]struct{})
	var candidates []string

	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return
		}
		candidates = append(candidates, path)
		seen[path] = struct{}{}
	}

	for _, pattern := range []string{
		filepath.Join(projectDir, "compose*.yml"),
		filepath.Join(projectDir, "compose*.yaml"),
		filepath.Join(projectDir, "docker-compose*.yml"),
		filepath.Join(projectDir, "docker-compose*.yaml"),
		filepath.Join(projectDir, "*.sh"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			add(match)
		}
	}

	scriptsDir := filepath.Join(projectDir, "scripts")
	_ = filepath.WalkDir(scriptsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".sh", ".bash", ".zsh":
			add(path)
		}
		return nil
	})

	return candidates
}

// devcontainerNeedsDocker inspects devcontainer.json to determine whether
// the configuration positively requires Docker or Compose. If the config
// contains "image", "dockerFile", "dockerComposeFile", or a "build" object
// with a "dockerfile" field, Docker is required and the marker is promoted
// to hard. If the config is absent or unparseable, we return false (advisory).
func devcontainerNeedsDocker(projectDir string) bool {
	// devcontainer.json may live at .devcontainer/devcontainer.json or
	// at .devcontainer.json in the project root.
	candidates := []string{
		filepath.Join(projectDir, ".devcontainer", "devcontainer.json"),
		filepath.Join(projectDir, ".devcontainer.json"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return devcontainerJSONNeedsDocker(data)
	}
	return false
}

// devcontainerJSONNeedsDocker checks raw devcontainer.json bytes for
// fields that indicate Docker is required. Only top-level fields are
// inspected — no deep schema validation.
func devcontainerJSONNeedsDocker(data []byte) bool {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	for _, key := range []string{"image", "dockerFile", "dockerComposeFile"} {
		if _, ok := cfg[key]; ok {
			return true
		}
	}
	if raw, ok := cfg["build"]; ok {
		var build map[string]json.RawMessage
		if json.Unmarshal(raw, &build) == nil {
			if _, ok := build["dockerfile"]; ok {
				return true
			}
		}
	}
	return false
}

func dockerSessionExample(commandName, projectDir string, mode dockerMode) string {
	flag := fmt.Sprintf("--docker=%s", mode)
	switch commandName {
	case "shell":
		return fmt.Sprintf("hazmat shell %s -C %s", flag, projectDir)
	case "exec":
		return fmt.Sprintf("hazmat exec %s -C %s -- <command>", flag, projectDir)
	case "claude":
		return fmt.Sprintf("hazmat claude %s -C %s", flag, projectDir)
	case "opencode":
		if mode == dockerModeSandbox {
			return fmt.Sprintf("hazmat claude %s -C %s", flag, projectDir)
		}
		return fmt.Sprintf("hazmat opencode %s -C %s", flag, projectDir)
	case "codex":
		if mode == dockerModeSandbox {
			return fmt.Sprintf("hazmat claude %s -C %s", flag, projectDir)
		}
		return fmt.Sprintf("hazmat codex %s -C %s", flag, projectDir)
	default:
		return fmt.Sprintf("hazmat claude %s -C %s", flag, projectDir)
	}
}

func summarizeList(values []string, limit int) string {
	if len(values) == 0 {
		return ""
	}
	if limit <= 0 || len(values) <= limit {
		return strings.Join(values, ", ")
	}
	return fmt.Sprintf("%s, +%d more", strings.Join(values[:limit], ", "), len(values)-limit)
}

func dockerProjectNeedsSandboxMessage(commandName, projectDir string, detection dockerProjectDetection) string {
	return strings.TrimLeft(fmt.Sprintf(`
Docker files detected: %s

Hazmat can only provide Docker access for this project by launching a private
Docker Sandbox. Native containment does not expose Docker commands.

  hazmat sandbox doctor
  %s
  hazmat config docker none -C %s
  %s

Use --docker=none only for code-only sessions. Docker commands will not work in
native containment.
`,
		strings.Join(detection.markers(), ", "),
		dockerSessionExample(commandName, projectDir, dockerModeSandbox),
		projectDir,
		dockerSessionExample(commandName, projectDir, dockerModeNone),
	), "\n")
}

func dockerSharedDaemonUnsupportedMessage(commandName, projectDir string, detection dockerProjectDetection) string {
	var signalLines strings.Builder
	for _, signal := range detection.SharedDaemonSignals {
		fmt.Fprintf(&signalLines, "  - %s\n", signal)
	}

	return strings.TrimLeft(fmt.Sprintf(`
Docker files detected: %s

This project appears to depend on the host Docker daemon:
%s
Hazmat containment does not support shared-daemon Docker access. The agent
cannot safely share a Docker daemon it does not exclusively own.

For code-only sessions:
  hazmat config docker none -C %s
  %s

If this detection is wrong and the repo is actually self-contained, force a
private-daemon session with:
  %s

Docker commands will not work with --docker=none. For agent-managed shared
Docker workflows, use Tier 4 or run Docker outside Hazmat.
`,
		strings.Join(detection.markers(), ", "),
		signalLines.String(),
		projectDir,
		dockerSessionExample(commandName, projectDir, dockerModeNone),
		dockerSessionExample(commandName, projectDir, dockerModeSandbox),
	), "\n")
}

func dockerProjectBlockedMessage(commandName, projectDir string, detection dockerProjectDetection) string {
	if detection.HasSharedDaemonSignals() {
		return dockerSharedDaemonUnsupportedMessage(commandName, projectDir, detection)
	}
	return dockerProjectNeedsSandboxMessage(commandName, projectDir, detection)
}

func sessionRoutingExplanation(commandName, projectDir string, request dockerRoutingRequest, detection dockerProjectDetection, mode sessionMode) (string, []string) {
	if mode == sessionModeDockerSandbox {
		switch request.Source {
		case dockerRequestFlag:
			return "using Docker Sandbox because --docker=sandbox was requested", nil
		case dockerRequestLegacySandbox:
			return "using Docker Sandbox because --sandbox was requested", nil
		case dockerRequestProjectConfig:
			return "using Docker Sandbox because this project is configured with docker: sandbox", nil
		default:
			if len(detection.HardMarkers) > 0 {
				return fmt.Sprintf("using Docker Sandbox because this project appears compatible with a private Docker daemon (%s)", strings.Join(detection.HardMarkers, ", ")), nil
			}
			return "using Docker Sandbox for this session", nil
		}
	}

	switch request.Mode {
	case dockerModeNone:
		var reason string
		switch request.Source {
		case dockerRequestFlag:
			reason = "staying in native containment because --docker=none was requested"
		case dockerRequestLegacyIgnore:
			reason = "staying in native containment because --ignore-docker was set"
		case dockerRequestProjectConfig:
			reason = "staying in native containment because this project is configured with docker: none"
		default:
			reason = "staying in native containment because Docker support was disabled for this session"
		}

		var notes []string
		if len(detection.HardMarkers) > 0 {
			notes = append(notes, fmt.Sprintf("Docker files detected: %s. Docker commands will not work in native containment.", strings.Join(detection.HardMarkers, ", ")))
		}
		if detection.HasSharedDaemonSignals() {
			notes = append(notes, fmt.Sprintf("Shared-daemon signals detected: %s.", summarizeList(detection.SharedDaemonSignals, 2)))
		}
		if len(detection.HardMarkers) > 0 {
			notes = append(notes, fmt.Sprintf("If this session needs Docker, use: %s", dockerSessionExample(commandName, projectDir, dockerModeSandbox)))
		}
		return reason, notes
	case dockerModeSandbox:
		// Sandbox was requested but we ended up in native mode (e.g.
		// resolved via resolvePreparedSessionMode before this point).
		// Fall through to the default reason below.
	case dockerModeAuto:
		if len(detection.HardMarkers) > 0 {
			return "using native containment because Docker Sandbox approval was declined", []string{
				"Docker commands will not work in this session.",
				fmt.Sprintf("If this session needs Docker, use: %s", dockerSessionExample(commandName, projectDir, dockerModeSandbox)),
			}
		}
		if len(detection.SoftMarkers) > 0 {
			return "staying in native containment because .devcontainer/ alone does not require Docker mode", []string{
				fmt.Sprintf("If this session needs Docker, use: %s", dockerSessionExample(commandName, projectDir, dockerModeSandbox)),
			}
		}
	}
	return "using native containment because no Docker requirement was detected", nil
}

func resolveSessionSandboxMode(commandName, projectDir string, request dockerRoutingRequest, detection dockerProjectDetection) (bool, error) {
	switch request.Mode {
	case dockerModeSandbox:
		return true, nil
	case dockerModeNone:
		return false, nil
	case dockerModeAuto:
	default:
		return false, fmt.Errorf("unsupported Docker mode %q", request.Mode)
	}

	if detection.HasSharedDaemonSignals() {
		return false, fmt.Errorf("%s", dockerProjectBlockedMessage(commandName, projectDir, detection))
	}
	if !detection.HasHardMarkers() {
		return false, nil
	}

	cfg, err := loadConfig()
	if err != nil {
		return false, err
	}

	var backend *SandboxBackendConfig
	var profile sandboxPolicyProfile

	if cfg.SandboxBackend() == nil {
		b, p, _, err := detectHealthySandboxBackend(sandboxProbeFactory())
		if err != nil {
			return false, fmt.Errorf("%s", dockerProjectBlockedMessage(commandName, projectDir, detection))
		}
		backend, profile = b, p
	} else {
		backend = cfg.SandboxBackend()
		p, err := sandboxPolicyProfileByName(backend.PolicyProfile)
		if err != nil {
			p = defaultSandboxPolicyProfile()
		}
		profile = p
	}

	if err := ensureSandboxApproval(projectDir, backend.Type, profile); err != nil {
		if errors.Is(err, errSandboxApprovalDeclined) {
			fmt.Fprintf(os.Stderr, "hazmat: falling back to native containment (Docker commands will not work in session)\n")
			fmt.Fprintf(os.Stderr, "hazmat: hint: use --docker=none to skip this prompt or hazmat config docker none -C %s to persist code-only mode\n", projectDir)
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func unsupportedSandboxTargetMessage(commandName, projectDir string, request dockerRoutingRequest) string {
	if request.Source == dockerRequestProjectConfig {
		return fmt.Sprintf("this project is configured with docker: sandbox, but hazmat %s does not support Docker Sandboxes yet\nuse %s or change the project setting with: hazmat config docker none -C %s", commandName, dockerSessionExample("claude", projectDir, dockerModeSandbox), projectDir)
	}
	return fmt.Sprintf("--docker=sandbox is not supported for hazmat %s yet\nuse %s instead", commandName, dockerSessionExample("claude", projectDir, dockerModeSandbox))
}

// warnDockerProject checks whether projectDir contains Docker artifacts that
// require an explicit Docker choice for commands that do not support Docker
// Sandboxes directly.
func warnDockerProject(commandName, projectDir string, request dockerRoutingRequest) error {
	if request.Mode == dockerModeNone {
		return nil
	}

	detection := detectDockerProject(projectDir)
	if !detection.HasHardMarkers() {
		return nil
	}
	return fmt.Errorf("%s", dockerProjectBlockedMessage(commandName, projectDir, detection))
}

// generateSBPL produces a per-session Seatbelt (SBPL) policy with all
// filesystem boundaries embedded as literal absolute paths. This makes
// --read an actual OS-level boundary rather than an advisory env var:
// only the listed directories receive read access beyond the project.
//
// Policy structure:
//   - PROJECT_DIR gets read+write
//   - Each ReadDirs entry gets read-only (skipped if covered by ProjectDir,
//     a WriteDirs entry, or another ReadDirs entry)
//   - Each WriteDirs entry gets read+write (skipped if covered by ProjectDir
//     or another WriteDirs entry)
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
	for _, p := range []string{"/usr/bin", "/bin", "/usr/local", "/opt/homebrew", "/Library/Developer/CommandLineTools", agentHome} {
		w("(allow process-exec (subpath %q))\n", p)
	}
	for _, dir := range cfg.ReadDirs {
		w("(allow process-exec (subpath %q))\n", dir)
	}
	for _, dir := range cfg.WriteDirs {
		w("(allow process-exec (subpath %q))\n", dir)
	}
	w("(allow process-exec (subpath %q))\n", cfg.ProjectDir)
	w("(allow process-fork)\n")
	w("(allow process-info* (target same-sandbox))\n")
	w("(allow signal (target same-sandbox))\n\n")

	w(";; ── System info (V8 reads CPU/memory via sysctl at startup) ────────────\n")
	w("(allow sysctl-read)\n\n")

	w(";; ── System libraries (required by Node.js / dyld) ──────────────────────\n")
	w(";; Path traversal literals for realpath() and symlink resolution.\n")
	w(";; /var → /private/var (DNS resolv.conf), /tmp → /private/tmp.\n")
	for _, p := range []string{"/", "/private", "/var", "/var/select", "/tmp", "/etc", "/usr", "/System", "/Library"} {
		w("(allow file-read* (literal %q))\n", p)
	}
	for _, p := range []string{"/usr/lib", "/usr/share", "/System/Library", "/Library/Frameworks", "/Library/Developer/CommandLineTools", "/private/etc", "/private/var/select"} {
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
	hostPaths = append(hostPaths, cfg.WriteDirs...)
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
	// covered by the project dir, by a writable extension, or by another
	// (broader) read dir.
	if len(cfg.ReadDirs) > 0 {
		var pending []string
		for _, dir := range cfg.ReadDirs {
			if isWithinDir(cfg.ProjectDir, dir) {
				continue // already covered by project read+write
			}
			coveredByWrite := false
			for _, writeDir := range cfg.WriteDirs {
				if isWithinDir(writeDir, dir) {
					coveredByWrite = true
					break
				}
			}
			if coveredByWrite {
				continue
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

	// Extra writable directories: individual rules, skipping any path already
	// covered by the project dir or by another (broader) write dir.
	if len(cfg.WriteDirs) > 0 {
		var pending []string
		for _, dir := range cfg.WriteDirs {
			if isWithinDir(cfg.ProjectDir, dir) {
				continue
			}
			covered := false
			for _, other := range cfg.WriteDirs {
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
			w(";; ── Read-write extensions ────────────────────────────────────────────────\n")
			for _, dir := range pending {
				w("(allow file-read* file-write* (subpath %q))\n", dir)
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
	w("(allow file-read* file-write* (literal \"/dev/tty\"))\n")
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

	w(";; ── Writable roots (re-assert after all read-only rules) ───────────────────\n")
	w(";; SBPL is last-match-wins. When a read-only -R directory is a parent of\n")
	w(";; a writable root (e.g. -R ~/workspace with project ~/workspace/foo),\n")
	w(";; the broad file-read* rule must not suppress explicit write access.\n")
	w(";; Re-asserting file-write* here guarantees it is the last matching allow\n")
	w(";; for any write operation targeting an explicit writable root.\n")
	w("(allow file-read* file-write* (subpath %q))\n\n", cfg.ProjectDir)
	for _, dir := range cfg.WriteDirs {
		if isWithinDir(cfg.ProjectDir, dir) {
			continue
		}
		w("(allow file-read* file-write* (subpath %q))\n", dir)
	}
	if len(cfg.WriteDirs) > 0 {
		w("\n")
	}

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
			bar := newStatusBar(cfg.ActiveIntegrations, cfg.ProjectDir)
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

	err := cmd.Run()

	// Post-session: repair .git/ permissions that may have been altered
	// by agent git operations. New files created by the agent are owned
	// by the agent user; re-applying the dev group ACL restores
	// collaborative access for the host user.
	repairGitAfterSession(cfg.ProjectDir)

	return err
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
	writeDirsJSON, _ := json.Marshal(cfg.WriteDirs)
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
		"SANDBOX_WRITE_DIRS_JSON=" + string(writeDirsJSON),
	}
	if home, err := os.UserHomeDir(); err == nil {
		terminalPairs, _ := terminalCapabilitySupport(home, os.Getenv)
		pairs = append(pairs, terminalPairs...)
	}

	// Go toolchain: share the invoking user's module cache read-only.
	// GOMODCACHE points to the invoker's cache so `go build` uses
	// pre-downloaded modules instead of re-fetching. The seatbelt enforces
	// read-only access — if a new dependency is needed, `go mod download`
	// must be run outside the sandbox first.
	if modCache := invokerGoModCache(); modCache != "" {
		pairs = append(pairs, "GOMODCACHE="+modCache)
	}

	// Integration env passthrough: passive path pointers and selectors resolved
	// from the invoker's environment. Only keys in safeEnvKeys are allowed;
	// validation happens at integration-manifest load time.
	for key, val := range cfg.IntegrationEnv {
		pairs = append(pairs, key+"="+val)
	}

	return pairs
}
