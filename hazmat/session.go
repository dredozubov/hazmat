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
	PlannedHostMutations    []sessionMutation
	SuggestedIntegrations   []string          // auto-detected built-in integrations not currently active
	IntegrationEnv          map[string]string // from integration env_passthrough (resolved values)
	IntegrationRegistryKeys []string          // active registry-redirect env keys (for UX)
	IntegrationExcludes     []string          // snapshot excludes added by active integrations
	IntegrationSources      []string          // provenance for runtime-resolved integration inputs
	IntegrationDetails      []string          // detailed runtime resolution notes for explain/show flows
	IntegrationWarnings     []string          // warnings surfaced by active integrations
	ActiveIntegrations      []string          // integration names, for status bar
	GitSSH                  *sessionGitSSHConfig
	ServiceAccess           []string  // explicit external-service access granted to session
	RoutingReason           string    // plain-language explanation for the chosen mode
	SessionNotes            []string  // plain-language notes about session behavior
	HarnessID               HarnessID // which agent harness this session is for ("" = generic shell/exec)
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
	dockerRequestDefault       dockerRequestSource = "default"
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
	Config           sessionConfig
	Mode             sessionMode
	HostMutationPlan sessionMutationPlan
}

type sessionPreparationProgress struct {
	w       io.Writer
	now     func() time.Time
	start   time.Time
	started bool
}

func newSessionPreparationProgress(w io.Writer) *sessionPreparationProgress {
	return &sessionPreparationProgress{
		w:     w,
		now:   time.Now,
		start: time.Now(),
	}
}

func (p *sessionPreparationProgress) Step(label string) {
	if p == nil || p.w == nil {
		return
	}
	if !p.started {
		fmt.Fprintln(p.w, "hazmat: preparing session startup")
		p.started = true
	}
	fmt.Fprintf(p.w, "  %s...\n", label)
}

func (p *sessionPreparationProgress) Done() {
	if p == nil || p.w == nil || !p.started {
		return
	}
	fmt.Fprintf(p.w, "hazmat: session startup preparation complete (%.1fs)\n", p.now().Sub(p.start).Seconds())
}

func (m sessionMode) label() string {
	switch m {
	case sessionModeDockerSandbox:
		return "Docker Sandbox"
	default:
		return "Native containment"
	}
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
			prepared, err := prepareLaunchSession("shell", harnessSessionOpts{
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
	cmd.Flags().StringVar(&dockerModeValue, "docker", string(dockerModeNone),
		"Docker routing: none (default), sandbox, or auto")
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
			prepared, err := prepareLaunchSession("exec", harnessSessionOpts{
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
	cmd.Flags().StringVar(&dockerModeValue, "docker", string(dockerModeNone),
		"Docker routing: none (default), sandbox, or auto")
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
  --skip-harness-assets-sync  Skip managed harness prompt-asset sync for this launch
  --no-backup            Skip pre-session snapshot
  --docker <mode>        Docker routing: none (default), sandbox, or auto
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
  hazmat claude --docker=sandbox -C /proj  Use Docker Sandbox mode
  hazmat claude --docker=auto -C /proj     Auto-detect private-daemon Docker mode
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

			prepared, err := prepareLaunchSession("claude", opts, true)
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
					`{ echo "Error: Claude Code not installed for agent user. Run: hazmat bootstrap claude" >&2; exit 1; }; }; `+
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
  --skip-harness-assets-sync  Skip managed harness prompt-asset sync for this launch
  --no-backup            Skip pre-session snapshot
  --docker <mode>        Docker routing: none (default), sandbox, or auto
  --ignore-docker        Alias for --docker=none (deprecated)

All other flags and arguments are forwarded to OpenCode.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root.
Docker Sandbox sessions are currently available through hazmat claude. Use
--docker=auto only when you want Docker markers to block explicitly.

Examples:
  hazmat opencode
  hazmat opencode -p "explain this"
  hazmat opencode -C /proj -p "hi"
  hazmat opencode --docker=auto -C /proj
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

			prepared, err := prepareLaunchSession("opencode", opts, false)
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
  --skip-harness-assets-sync  Skip managed harness prompt-asset sync for this launch
  --no-backup            Skip pre-session snapshot
  --docker <mode>        Docker routing: none (default), sandbox, or auto
  --ignore-docker        Alias for --docker=none (deprecated)

All other flags and arguments are forwarded to Codex.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root.
Docker Sandbox sessions are currently available through hazmat claude. Use
--docker=auto only when you want Docker markers to block explicitly.

Examples:
  hazmat codex
  hazmat codex "explain this repo"
  hazmat codex --docker=auto -C /proj
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

			prepared, err := prepareLaunchSession("codex", opts, false)
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

func newGeminiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gemini [hazmat-flags] [gemini-flags] [gemini-args...]",
		Short: "Launch Gemini CLI in containment",
		Long: `Launch Gemini CLI in a sandboxed environment.

Hazmat flags (parsed first, may appear anywhere before --):
  -C, --project <dir>    Writable project directory (defaults to cwd)
  -R, --read <dir>       Read-only directory (repeatable)
  -W, --write <dir>      Read-write directory (repeatable)
  --integration <name>   Activate a session integration (repeatable)
  --skip-harness-assets-sync  Skip managed harness prompt-asset sync for this launch
  --no-backup            Skip pre-session snapshot

All other flags and arguments are forwarded to Gemini.
Directory arguments are forwarded unchanged; use -C/--project to change
the writable project root.

Examples:
  hazmat gemini
  hazmat gemini -p "explain this repo"
  hazmat gemini -C /proj
  hazmat gemini --no-backup`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, forwarded, err := parseHarnessArgs(args)
			if err != nil {
				if err == errHarnessHelp {
					return cmd.Help()
				}
				return err
			}

			prepared, err := prepareLaunchSession("gemini", opts, false)
			if err != nil {
				return err
			}
			if err := beginPreparedSession(prepared, "gemini", opts.noBackup, true); err != nil {
				return err
			}
			return runAgentSeatbeltScript(prepared.Config, geminiLaunchScript(), forwarded...)
		},
	}
	return cmd
}

// harnessSessionOpts holds hazmat-specific flags extracted from a harness
// command line before forwarding the rest to the harness CLI.
type harnessSessionOpts struct {
	project               string
	readDirs              []string
	writeDirs             []string
	integrations          []string
	resolvedIntegrations  []IntegrationSpec
	integrationsResolved  bool
	skipHarnessAssetsSync bool
	noBackup              bool
	useSandbox            bool
	allowDocker           bool
	dockerMode            string
	dockerModeExplicit    bool
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
// Hazmat flags (--project, --read, --write, --integration,
// --skip-harness-assets-sync, --no-backup,
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
		case arg == "--skip-harness-assets-sync":
			opts.skipHarnessAssetsSync = true
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
func applyIntegrations(cfg *sessionConfig, integrationFlags []string) (sessionMutationPlan, error) {
	integrations, err := resolveActiveIntegrationsForSession(integrationFlags, cfg.ProjectDir)
	if err != nil {
		return sessionMutationPlan{}, err
	}
	return applyResolvedIntegrations(cfg, integrations)
}

var resolveActiveIntegrationsForSession = resolveActiveIntegrations

func applyResolvedIntegrations(cfg *sessionConfig, integrations []IntegrationSpec) (sessionMutationPlan, error) {
	// Detect integrations that remain unresolved after active ones are merged.
	activeNames := make(map[string]struct{}, len(integrations))
	for _, spec := range integrations {
		activeNames[spec.Meta.Name] = struct{}{}
	}
	cfg.SuggestedIntegrations = suggestedIntegrationsForProject(cfg.ProjectDir, activeNames)

	if len(integrations) == 0 {
		return sessionMutationPlan{}, nil
	}

	names := make([]string, 0, len(integrations))
	for _, spec := range integrations {
		names = append(names, spec.Meta.Name)
	}
	cfg.ActiveIntegrations = names

	resolved, mutationPlan, err := resolveRuntimeIntegrations(cfg.ProjectDir, integrations)
	if err != nil {
		return sessionMutationPlan{}, err
	}

	// Merge all integrations.
	merged, err := mergeResolvedIntegrations(resolved)
	if err != nil {
		return sessionMutationPlan{}, err
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

	return mutationPlan, nil
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
	return resolvePreparedSessionWithProgress(commandName, opts, supportsSandbox, nil)
}

func resolvePreparedSessionWithProgress(commandName string, opts harnessSessionOpts, supportsSandbox bool, progress *sessionPreparationProgress) (preparedSession, error) {
	if err := requireInit(); err != nil {
		return preparedSession{}, err
	}

	progress.Step("resolving project access")
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
	if id, ok := harnessIDForCommand(commandName); ok {
		cfg.HarnessID = id
	}
	cfg.UserReadDirs, err = resolveReadDirs(userReadPaths)
	if err != nil {
		return preparedSession{}, err
	}
	cfg.AutoReadDirs, err = resolveReadDirs(autoReadPaths)
	if err != nil {
		return preparedSession{}, err
	}
	progress.Step("applying session integrations")
	var integrationMutationPlan sessionMutationPlan
	if opts.integrationsResolved {
		integrationMutationPlan, err = applyResolvedIntegrations(&cfg, opts.resolvedIntegrations)
		if err != nil {
			return preparedSession{}, err
		}
	} else {
		integrationMutationPlan, err = applyIntegrations(&cfg, opts.integrations)
		if err != nil {
			return preparedSession{}, err
		}
	}
	progress.Step("checking repo-local hooks")
	maybePromptProjectHooks(cfg.ProjectDir)

	progress.Step("checking Docker routing")
	request, err := resolveDockerRoutingRequest(cfg.ProjectDir, opts)
	if err != nil {
		return preparedSession{}, err
	}
	detection := detectDockerProject(cfg.ProjectDir)

	mode, err := resolvePreparedSessionMode(commandName, cfg.ProjectDir, request, detection, supportsSandbox)
	if err != nil {
		return preparedSession{}, err
	}

	progress.Step("checking Git SSH access")
	cfg.GitSSH, err = resolveManagedGitSSH(cfg)
	if err != nil {
		return preparedSession{}, err
	}
	if cfg.GitSSH != nil && mode != sessionModeNative {
		return preparedSession{}, fmt.Errorf("managed Git SSH is not supported for Docker Sandbox sessions yet\nuse %s for a native code session, or clear the project capability with: hazmat config ssh clear -C %s",
			dockerSessionExample(commandName, cfg.ProjectDir, dockerModeNone),
			cfg.ProjectDir,
		)
	}

	cfg.RoutingReason, cfg.SessionNotes = sessionRoutingExplanation(commandName, cfg.ProjectDir, request, detection, mode)
	if cfg.GitSSH != nil {
		cfg.ServiceAccess = append(cfg.ServiceAccess, "git+ssh")
		cfg.SessionNotes = append(cfg.SessionNotes, cfg.GitSSH.SessionNote)
	}
	progress.Step("planning harness asset sync")
	harnessAssetMutationPlan, err := buildHarnessAssetSessionMutationPlan(commandName, mode, opts)
	if err != nil {
		return preparedSession{}, err
	}
	prepared := preparedSession{
		Config:           cfg,
		Mode:             mode,
		HostMutationPlan: mergeSessionMutationPlans(integrationMutationPlan, harnessAssetMutationPlan),
	}
	if mode == sessionModeNative {
		progress.Step("planning host repairs")
		prepared.HostMutationPlan = mergeSessionMutationPlans(prepared.HostMutationPlan, buildNativeSessionMutationPlan(cfg))
	}
	prepared.Config.PlannedHostMutations = prepared.HostMutationPlan.Describe()
	return prepared, nil
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
	printSuggestedIntegrations(prepared.Config.SuggestedIntegrations)
	printSessionMutationDetails(prepared.Config.PlannedHostMutations)
	if prepared.Mode != sessionModeNative {
		if err := executeSessionMutationPlan(prepared.HostMutationPlan); err != nil {
			return err
		}
		preSessionSnapshot(prepared.Config, commandName, skipSnapshot)
		return nil
	}

	if preflightBeforeSnapshot {
		if err := executeSessionMutationPlan(prepared.HostMutationPlan); err != nil {
			return err
		}
	}

	preSessionSnapshot(prepared.Config, commandName, skipSnapshot)

	if !preflightBeforeSnapshot {
		if err := executeSessionMutationPlan(prepared.HostMutationPlan); err != nil {
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
	if len(cfg.PlannedHostMutations) > 0 {
		fmt.Fprintf(&b, "  Host changes:          %s\n", sessionMutationList(cfg.PlannedHostMutations))
	}
	fmt.Fprintf(&b, "  Auto read-only:       %s\n", sessionContractList(cfg.AutoReadDirs))
	fmt.Fprintf(&b, "  Read-only extensions: %s\n", sessionContractList(cfg.UserReadDirs))
	fmt.Fprintf(&b, "  Read-write extensions: %s\n", sessionContractList(cfg.WriteDirs))
	fmt.Fprintf(&b, "  Service access:       %s\n", sessionContractList(cfg.ServiceAccess))
	if cfg.GitSSH != nil && strings.TrimSpace(cfg.GitSSH.DisplayName) != "" {
		fmt.Fprintf(&b, "  Git SSH key:          %s\n", cfg.GitSSH.DisplayName)
	}
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

func printSessionMutationDetails(mutations []sessionMutation) {
	fmt.Fprint(os.Stderr, renderSessionMutationDetails(mutations))
}

func renderSuggestedIntegrations(suggestions []string) string {
	if len(suggestions) == 0 {
		return ""
	}
	return fmt.Sprintf("hazmat: suggested integrations: %s (activate with --integration <name> or approve them during an interactive launch)\n",
		strings.Join(suggestions, ", "))
}

func printSuggestedIntegrations(suggestions []string) {
	fmt.Fprint(os.Stderr, renderSuggestedIntegrations(suggestions))
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

	return dockerRoutingRequest{Mode: dockerModeNone, Source: dockerRequestDefault}, nil
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
	case "gemini":
		return fmt.Sprintf("hazmat gemini %s -C %s", flag, projectDir)
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
		switch request.Mode {
		case dockerModeSandbox:
			switch request.Source {
			case dockerRequestDefault, dockerRequestLegacyIgnore:
				return "using Docker Sandbox for this session", nil
			case dockerRequestFlag:
				return "using Docker Sandbox because --docker=sandbox was requested", nil
			case dockerRequestLegacySandbox:
				return "using Docker Sandbox because --sandbox was requested", nil
			case dockerRequestProjectConfig:
				return "using Docker Sandbox because this project is configured with docker: sandbox", nil
			}
		case dockerModeAuto:
			if len(detection.HardMarkers) > 0 {
				markers := strings.Join(detection.HardMarkers, ", ")
				switch request.Source {
				case dockerRequestDefault, dockerRequestLegacyIgnore, dockerRequestLegacySandbox:
					return fmt.Sprintf("using Docker Sandbox because automatic Docker routing detected a private-daemon fit (%s)", markers), nil
				case dockerRequestFlag:
					return fmt.Sprintf("using Docker Sandbox because --docker=auto detected a private-daemon Docker fit (%s)", markers), nil
				case dockerRequestProjectConfig:
					return fmt.Sprintf("using Docker Sandbox because this project is configured with docker: auto and appears compatible with a private Docker daemon (%s)", markers), nil
				}
			}
		case dockerModeNone:
			return "using Docker Sandbox for this session", nil
		}
		return "using Docker Sandbox for this session", nil
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
		case dockerRequestDefault:
			reason = "using native containment by default (Docker routing: none)"
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
		if len(detection.SoftMarkers) > 0 {
			notes = append(notes, fmt.Sprintf("Container metadata detected: %s. Docker mode is not enabled by default.", strings.Join(detection.SoftMarkers, ", ")))
		}
		if len(detection.HardMarkers) > 0 || len(detection.SoftMarkers) > 0 {
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
		if request.Source == dockerRequestProjectConfig {
			return "using native containment because this project is configured with docker: auto and no Docker requirement was detected", nil
		}
		if request.Source == dockerRequestFlag {
			return "using native containment because --docker=auto found no Docker requirement", nil
		}
	}
	return "using native containment for this session", nil
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
			fmt.Fprintf(os.Stderr, "hazmat: hint: omit --docker=auto, use --docker=none, or run hazmat config docker none -C %s to persist code-only mode\n", projectDir)
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

// warnDockerProject checks whether explicit Docker auto/sandbox routing would
// require a Docker-capable command. The default mode is native code-only
// containment, so Docker markers are notes rather than blockers by default.
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

// generateSBPL keeps the current public test/helper entrypoint while routing
// session configs through the backend-neutral native policy contract first.
func generateSBPL(cfg sessionConfig) string {
	return compileDarwinSBPL(newNativeSessionPolicy(cfg))
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

	runtime, err := prepareSessionRuntime(cfg)
	if err != nil {
		return err
	}
	defer runtime.Cleanup()

	policy, err := prepareNativeLaunchPolicy(cfg)
	if err != nil {
		return err
	}
	defer policy.Cleanup()

	full := nativeLaunchSudoArgs(cfg, policy, runtime.EnvPairs, script, args...)

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
		cmd = exec.Command(hostScriptPath, scriptArgs...)
		cmd.Dir = "/"
		watchStop = make(chan struct{})
		watchDone = make(chan struct{})
		go func() {
			defer close(watchDone)
			watchTranscriptForAltScreen(transcriptPath, startBar, watchStop)
		}()
	} else {
		cmd = newSudoCommand(full...)
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

	err = cmd.Run()

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
