package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var errHermesBinaryMissing = errors.New("Hermes binary missing")

const (
	hermesBinRel          = "/.local/bin/hermes"
	hermesStateDirRel     = "/.hazmat/hermes"
	hermesProjectsDirRel  = hermesStateDirRel + "/projects"
	hermesMissingHelp     = "Error: Hermes not installed for agent user. Run: hazmat bootstrap hermes"
	hermesFallbackProject = "unknown-project"
)

func hermesStateDir() string {
	return agentHome + hermesStateDirRel
}

func hermesProjectsDir() string {
	return agentHome + hermesProjectsDirRel
}

func hermesProjectStateDir(projectDir string) string {
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		return filepath.Join(hermesProjectsDir(), hermesFallbackProject)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(projectDir)))
	return filepath.Join(hermesProjectsDir(), hex.EncodeToString(sum[:]))
}

func findInstalledHermesBinary() (string, bool) {
	return findInstalledHermesBinaryWith(asAgentOutput)
}

func findInstalledHermesBinaryWith(read func(args ...string) (string, error)) (string, bool) {
	path := agentHome + hermesBinRel
	if _, err := read("test", "-x", path); err == nil {
		return path, true
	}
	return "", false
}

func hermesLaunchScript() string {
	return `cd "$SANDBOX_PROJECT_DIR" && ` +
		`{ test -x "$HOME` + hermesBinRel + `" || ` +
		`{ echo "` + hermesMissingHelp + `" >&2; exit 1; }; }; ` +
		`exec "$HOME` + hermesBinRel + `" "$@"`
}

func newBootstrapHermesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hermes",
		Short: "Verify a manually installed Hermes CLI for the agent user",
		Long: `Verify a manually installed Hermes CLI for the agent user.

Phase 1 does not run the upstream Hermes installer automatically. Place or link
the Hermes executable at /Users/agent/.local/bin/hermes, then run this command.
Hazmat records Hermes harness state only after 'hermes --version' succeeds.

This command does not import host ~/.hermes, host provider files, MCP config,
skills, cron state, or gateway settings.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)
			return hermesHarness.Bootstrap(ui, r)
		},
	}
}

func runHermesBootstrap(ui *UI, r *Runner) error {
	ui.Step(fmt.Sprintf("Verify agent user %q", agentUser))
	if _, err := requireAgentUser(); err != nil {
		return err
	}
	ui.Ok(fmt.Sprintf("Agent user %s exists", agentUser))

	ui.Step("Verify Hermes CLI for agent user")
	binaryPath, version, err := probeHermesBinary(r.AgentOutput, r.DryRun)
	if errors.Is(err, errHermesBinaryMissing) {
		return fmt.Errorf("%s", hermesManualInstallMessage())
	}
	if err != nil {
		return err
	}
	if binaryPath == "" {
		ui.Ok(fmt.Sprintf("Would verify Hermes CLI at %s", agentHome+hermesBinRel))
		return nil
	}
	ui.Ok(fmt.Sprintf("Hermes CLI detected at %s (%s)", binaryPath, version))
	return nil
}

func probeHermesBinary(read func(args ...string) (string, error), dryRun bool) (string, string, error) {
	binaryPath, ok := findInstalledHermesBinaryWith(read)
	if !ok {
		if dryRun {
			return "", "", nil
		}
		return "", "", errHermesBinaryMissing
	}

	version, err := read(binaryPath, "--version")
	if err != nil {
		return "", "", fmt.Errorf("verify Hermes CLI with %s --version: %w", binaryPath, err)
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = "version output empty"
	}
	return binaryPath, version, nil
}

func hermesManualInstallMessage() string {
	return fmt.Sprintf(`Hermes CLI is not installed for the agent user.

Phase 1 Hazmat does not run the upstream Hermes installer automatically. Install
Hermes for the agent account using an audited path, then place or link the
executable here:

  %s

After installing, verify and record the harness with:

  hazmat bootstrap hermes

No host ~/.hermes profile is imported by this command.`, agentHome+hermesBinRel)
}

func ensureHermesStateRootAsAgent(path string) error {
	if err := agentEnsureDir(agentHome+"/.hazmat", 0o700); err != nil {
		return err
	}
	if err := agentEnsureDir(hermesStateDir(), 0o700); err != nil {
		return err
	}
	if err := agentEnsureDir(hermesProjectsDir(), 0o700); err != nil {
		return err
	}
	if err := agentEnsureDir(path, 0o700); err != nil {
		return err
	}
	return nil
}

var (
	ensureHermesStateRoot  = ensureHermesStateRootAsAgent
	requireHermesAgentUser = requireAgentUser
	removeHermesStatePath  = removeHermesStatePathAsAgent
)

func buildHermesStateRootMutationPlan(cfg sessionConfig) sessionMutationPlan {
	if cfg.HarnessID != HarnessHermes {
		return sessionMutationPlan{}
	}
	stateDir := hermesProjectStateDir(cfg.ProjectDir)
	return sessionMutationPlan{
		Mutations: []plannedSessionMutation{
			{
				Metadata: sessionMutation{
					Summary:     "Hermes state root",
					Detail:      fmt.Sprintf("may create project-scoped managed HERMES_HOME at %s without importing host ~/.hermes", stateDir),
					Persistence: "persistent in agent home",
					ProofScope:  sessionMutationProofScopeTestsDocs,
				},
				Apply: func() (sessionMutationExecution, error) {
					if err := ensureHermesStateRoot(stateDir); err != nil {
						return sessionMutationExecution{}, err
					}
					return sessionMutationExecution{
						AppliedMessage: fmt.Sprintf("  Prepared Hermes state root at %s", stateDir),
					}, nil
				},
			},
		},
	}
}

func applyHarnessStaticSessionEnv(cfg *sessionConfig) {
	if cfg == nil || cfg.HarnessID != HarnessHermes {
		return
	}
	if cfg.HarnessEnv == nil {
		cfg.HarnessEnv = make(map[string]string, 1)
	}
	cfg.HarnessEnv["HERMES_HOME"] = hermesProjectStateDir(cfg.ProjectDir)
}

func appendHarnessStaticSessionNotes(cfg *sessionConfig) {
	if cfg == nil || cfg.HarnessID != HarnessHermes {
		return
	}
	cfg.SessionNotes = append(cfg.SessionNotes,
		"Hermes uses project-scoped managed HERMES_HOME="+hermesProjectStateDir(cfg.ProjectDir)+"; host ~/.hermes is not imported.",
		"Hermes gateway, dashboard/API, and persistent cron entrypoints are deferred in this foreground harness.",
	)
}

type hermesResetOptions struct {
	Project string
	All     bool
	Force   bool
}

type hermesResetTarget struct {
	Path  string
	Scope string
}

func executeHermesResetArgs(args []string) error {
	cmd := newHermesResetCmd()
	cmd.SetArgs(args)
	return cmd.Execute()
}

func newHermesResetCmd() *cobra.Command {
	var opts hermesResetOptions
	cmd := &cobra.Command{
		Use:   "reset [--project <dir> | --all]",
		Short: "Remove Hermes managed profile state",
		Long: `Remove Hermes managed profile state.

By default this removes only the project-scoped HERMES_HOME for the selected
project. Use --all to remove all Hazmat-managed Hermes profile state, including
legacy managed Hermes roots. This does not uninstall /Users/agent/.local/bin/hermes
or remove Hazmat's recorded harness metadata.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runHermesReset(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Project, "project", "C", "", "Project directory whose Hermes profile state should be removed")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Remove all Hazmat-managed Hermes profile state")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "Remove without prompting")
	return cmd
}

func resolveHermesResetTarget(opts hermesResetOptions) (hermesResetTarget, error) {
	if opts.All && strings.TrimSpace(opts.Project) != "" {
		return hermesResetTarget{}, fmt.Errorf("--all cannot be combined with --project")
	}
	if opts.All {
		return hermesResetTarget{
			Path:  hermesStateDir(),
			Scope: "all Hazmat-managed Hermes profile state",
		}, nil
	}

	projectDir, err := resolveDir(opts.Project, true)
	if err != nil {
		return hermesResetTarget{}, fmt.Errorf("resolve Hermes reset project: %w", err)
	}
	return hermesResetTarget{
		Path:  hermesProjectStateDir(projectDir),
		Scope: "Hermes profile state for project " + projectDir,
	}, nil
}

func isHermesManagedStatePath(path string) bool {
	clean := filepath.Clean(path)
	if clean == filepath.Clean(hermesStateDir()) {
		return true
	}
	projectsRoot := filepath.Clean(hermesProjectsDir())
	rel, err := filepath.Rel(projectsRoot, clean)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func runHermesReset(opts hermesResetOptions) error {
	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll || opts.Force}
	r := NewRunner(ui, flagVerbose, flagDryRun)
	return runHermesResetWith(ui, r, opts)
}

func runHermesResetWith(ui *UI, r *Runner, opts hermesResetOptions) error {
	target, err := resolveHermesResetTarget(opts)
	if err != nil {
		return err
	}
	if !isHermesManagedStatePath(target.Path) {
		return fmt.Errorf("refusing to remove non-Hermes-managed path %q", target.Path)
	}
	if _, err := requireHermesAgentUser(); err != nil {
		return err
	}

	fmt.Println()
	cBold.Println("  Hermes reset")
	fmt.Println()
	fmt.Printf("    Scope:  %s\n", target.Scope)
	fmt.Printf("    Target: %s\n", target.Path)
	fmt.Println("    Keeps:  /Users/agent/.local/bin/hermes and Hazmat harness metadata")
	fmt.Println()

	if !ui.Ask("Remove this Hermes managed profile state?") {
		fmt.Println()
		return nil
	}

	if flagDryRun {
		cYellow.Printf("  Dry-run: would remove %s\n", target.Path)
		fmt.Println()
		return nil
	}

	if err := removeHermesStatePath(r, target.Path); err != nil {
		return fmt.Errorf("remove Hermes managed profile state %s: %w", target.Path, err)
	}
	ui.Ok(fmt.Sprintf("Removed %s", target.Path))
	fmt.Println()
	return nil
}

func removeHermesStatePathAsAgent(r *Runner, path string) error {
	return r.AsAgent("remove Hermes managed profile state", "/bin/rm", "-rf", "--", path)
}
