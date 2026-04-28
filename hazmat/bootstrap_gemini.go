package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	geminiBinRel      = "/.local/bin/gemini"
	geminiNpmPackage  = "@google/gemini-cli@latest"
	geminiMissingHelp = "Error: Gemini CLI not installed for agent user. Run: hazmat bootstrap gemini"
	geminiStateDirRel = "/.gemini"
)

func findInstalledGeminiBinary() (string, bool) {
	return findInstalledGeminiBinaryWith(asAgentOutput)
}

func findInstalledGeminiBinaryWith(read func(args ...string) (string, error)) (string, bool) {
	path := agentHome + geminiBinRel
	if _, err := read("test", "-x", path); err == nil {
		return path, true
	}
	return "", false
}

func geminiLaunchScript() string {
	return `cd "$SANDBOX_PROJECT_DIR" && ` +
		`{ test -x "$HOME` + geminiBinRel + `" || ` +
		`{ echo "` + geminiMissingHelp + `" >&2; exit 1; }; }; ` +
		`exec "$HOME` + geminiBinRel + `" "$@"`
}

func geminiInstallScript() string {
	// npm install -g writes the package + bin shim into the configured
	// prefix. We force prefix=$HOME/.local so the install lands in the
	// agent's home (isolated from any host-side nvm/node dirs that
	// happen to be on the agent's PATH).
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
if ! command -v node >/dev/null 2>&1; then
  echo "Node.js not found on agent PATH — install Homebrew node first: brew install node" >&2
  exit 1
fi
mkdir -p "$HOME/.local/bin" "$HOME/.local/lib/node_modules"
export NPM_CONFIG_PREFIX="$HOME/.local"
npm install -g --silent %q
test -x "$HOME%s"
`, geminiNpmPackage, geminiBinRel)
}

func newBootstrapGeminiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gemini",
		Short: "Install or update Gemini CLI for the agent user",
		Long: `Install or update Gemini CLI for the agent user.

Hazmat installs the official @google/gemini-cli npm package into the agent
user's ~/.local prefix. Node.js must be available on the agent's PATH
(typically via Homebrew at /opt/homebrew/bin/node). Gemini keeps its own
auth and runtime state under ~/.gemini.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)
			return geminiHarness.Bootstrap(ui, r)
		},
	}
}

func runGeminiBootstrap(ui *UI, r *Runner) error {
	ui.Step(fmt.Sprintf("Verify agent user %q", agentUser))
	if _, err := requireAgentUser(); err != nil {
		return err
	}
	ui.Ok(fmt.Sprintf("Agent user %s exists", agentUser))

	if err := runHarnessInstallOrUpdateStep(ui, r, harnessInstallOrUpdateStep{
		DisplayName:   "Gemini CLI",
		TempPattern:   "hazmat-gemini-bootstrap-*.sh",
		InstallReason: "install or update Gemini CLI as agent user via npm",
		BuildScript: func(bool) (string, error) {
			return geminiInstallScript(), nil
		},
		FindExisting: findInstalledGeminiBinaryWith,
	}); err != nil {
		return err
	}

	ui.Step("Create Gemini state directory")
	stateDir := agentHome + geminiStateDirRel
	if err := agentEnsureSharedDir(stateDir, 0o2770); err != nil {
		return fmt.Errorf("ensure %s: %w", stateDir, err)
	}
	ui.Ok(fmt.Sprintf("Prepared %s", stateDir))

	return nil
}
