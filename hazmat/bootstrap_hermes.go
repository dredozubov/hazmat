package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var errHermesBinaryMissing = errors.New("Hermes binary missing")

const (
	hermesBinRel      = "/.local/bin/hermes"
	hermesStateDirRel = "/.hazmat/hermes"
	hermesMissingHelp = "Error: Hermes not installed for agent user. Run: hazmat bootstrap hermes"
)

func hermesStateDir() string {
	return agentHome + hermesStateDirRel
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
	if err := agentEnsureDir(path, 0o700); err != nil {
		return err
	}
	return nil
}

var ensureHermesStateRoot = ensureHermesStateRootAsAgent

func buildHermesStateRootMutationPlan(cfg sessionConfig) sessionMutationPlan {
	if cfg.HarnessID != HarnessHermes {
		return sessionMutationPlan{}
	}
	stateDir := hermesStateDir()
	return sessionMutationPlan{
		Mutations: []plannedSessionMutation{
			{
				Metadata: sessionMutation{
					Summary:     "Hermes state root",
					Detail:      fmt.Sprintf("may create managed HERMES_HOME at %s without importing host ~/.hermes", stateDir),
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
	cfg.HarnessEnv["HERMES_HOME"] = hermesStateDir()
}

func appendHarnessStaticSessionNotes(cfg *sessionConfig) {
	if cfg == nil || cfg.HarnessID != HarnessHermes {
		return
	}
	cfg.SessionNotes = append(cfg.SessionNotes,
		"Hermes uses managed HERMES_HOME="+hermesStateDir()+"; host ~/.hermes is not imported.",
		"Hermes gateway, dashboard/API, and persistent cron entrypoints are deferred in this foreground harness.",
	)
}
