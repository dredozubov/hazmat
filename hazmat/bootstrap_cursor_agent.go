package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var errCursorAgentBinaryMissing = errors.New("Cursor Agent binary missing")

const (
	cursorAgentBinRel      = "/.local/bin/cursor-agent"
	cursorAgentMissingHelp = "Error: Cursor Agent not installed for agent user. Run: hazmat bootstrap cursor-agent"
)

func findInstalledCursorAgentBinary() (string, bool) {
	return findInstalledCursorAgentBinaryWith(asAgentOutput)
}

func findInstalledCursorAgentBinaryWith(read func(args ...string) (string, error)) (string, bool) {
	path := agentHome + cursorAgentBinRel
	if _, err := read("test", "-x", path); err == nil {
		return path, true
	}
	return "", false
}

func probeCursorAgentHarness(read func(args ...string) (string, error)) harnessProbe {
	return probeHarnessBinary(read, findInstalledCursorAgentBinaryWith, "--version")
}

func cursorAgentHarnessManagedCodeArtifacts() []harnessManagedArtifact {
	return []harnessManagedArtifact{
		harnessFileArtifact(agentHome+cursorAgentBinRel, "Cursor Agent executable"),
	}
}

func cursorAgentLaunchScript() string {
	return `cd "$SANDBOX_PROJECT_DIR" && ` +
		`{ test -x "$HOME` + cursorAgentBinRel + `" || ` +
		`{ echo "` + cursorAgentMissingHelp + `" >&2; exit 1; }; }; ` +
		`exec "$HOME` + cursorAgentBinRel + `" "$@"`
}

func newBootstrapCursorAgentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cursor-agent",
		Short: "Verify a manually installed Cursor Agent CLI for the agent user",
		Long: `Verify a manually installed Cursor Agent CLI for the agent user.

Phase 1 does not run the upstream Cursor installer automatically. Place or link
the Cursor Agent executable at /Users/agent/.local/bin/cursor-agent, then run
this command. Hazmat records Cursor Agent harness state only after
'cursor-agent --version' succeeds.

This command does not import host Cursor IDE state, host ~/.cursor profile
state, or host Cursor auth settings.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)
			return cursorAgentHarness.Bootstrap(ui, r)
		},
	}
}

func runCursorAgentBootstrap(ui *UI, r *Runner) error {
	if err := verifyAgentUserForBootstrap(ui, r); err != nil {
		return err
	}

	ui.Step("Verify Cursor Agent CLI for agent user")
	binaryPath, version, err := probeCursorAgentBinary(r.AgentOutput, r.DryRun)
	if errors.Is(err, errCursorAgentBinaryMissing) {
		return fmt.Errorf("%s", cursorAgentManualInstallMessage())
	}
	if err != nil {
		return err
	}
	if binaryPath == "" {
		ui.Ok(fmt.Sprintf("Would verify Cursor Agent CLI at %s", agentHome+cursorAgentBinRel))
		return nil
	}
	ui.Ok(fmt.Sprintf("Cursor Agent CLI detected at %s (%s)", binaryPath, version))
	return nil
}

func probeCursorAgentBinary(read func(args ...string) (string, error), dryRun bool) (string, string, error) {
	binaryPath, ok := findInstalledCursorAgentBinaryWith(read)
	if !ok {
		if dryRun {
			return "", "", nil
		}
		return "", "", errCursorAgentBinaryMissing
	}

	version, err := read(binaryPath, "--version")
	if err != nil {
		return "", "", fmt.Errorf("verify Cursor Agent CLI with %s --version: %w", binaryPath, err)
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = "version output empty"
	}
	return binaryPath, version, nil
}

func cursorAgentManualInstallMessage() string {
	return fmt.Sprintf(`Cursor Agent CLI is not installed for the agent user.

Phase 1 Hazmat does not run the upstream Cursor installer automatically. Install
Cursor Agent for the agent account using an audited path, then place or link the
executable here:

  %s

After installing, verify and record the harness with:

  hazmat bootstrap cursor-agent

No host Cursor IDE state, host ~/.cursor profile state, or host auth settings
are imported by this command.`, agentHome+cursorAgentBinRel)
}
