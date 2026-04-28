package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	opencodeInstallerURL  = "https://opencode.ai/install"
	openCodeCurrentBinRel = "/.opencode/bin/opencode"
	openCodeLegacyBinRel  = "/.local/bin/opencode"
	openCodeMissingHelp   = "Error: OpenCode not installed for agent user. Run: hazmat bootstrap opencode"
)

const agentOpenCodeConfigJSON = `{
  "$schema": "https://opencode.ai/config.json",
  "autoupdate": false
}
`

func openCodeBinaryCandidates() []string {
	return []string{
		agentHome + openCodeCurrentBinRel,
		agentHome + openCodeLegacyBinRel,
	}
}

func findInstalledOpenCodeBinary() (string, bool) {
	return findInstalledOpenCodeBinaryWith(asAgentOutput)
}

func findInstalledOpenCodeBinaryWith(read func(args ...string) (string, error)) (string, bool) {
	for _, path := range openCodeBinaryCandidates() {
		if _, err := read("test", "-x", path); err == nil {
			return path, true
		}
	}
	return "", false
}

func openCodeLaunchScript() string {
	return `cd "$SANDBOX_PROJECT_DIR" && ` +
		`opencode_bin=""; ` +
		`for candidate in "$HOME` + openCodeCurrentBinRel + `" "$HOME` + openCodeLegacyBinRel + `"; do ` +
		`if [ -x "$candidate" ]; then opencode_bin="$candidate"; break; fi; ` +
		`done; ` +
		`if [ -z "$opencode_bin" ]; then echo "` + openCodeMissingHelp + `" >&2; exit 1; fi; ` +
		`exec "$opencode_bin" "$@"`
}

func openCodeInstallScript() string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
installer=$(mktemp "${TMPDIR:-/tmp}/opencode-install.XXXXXX")
cleanup() { rm -f "$installer"; }
trap cleanup EXIT
curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail %q -o "$installer"
bash "$installer" --no-modify-path
if [ -x "$HOME%s" ] && [ ! -e "$HOME%s" ] && [ ! -L "$HOME%s" ]; then
  install -d -m 0700 "$HOME/.local/bin"
  ln -s "$HOME%s" "$HOME%s"
fi
test -x "$HOME%s" || test -x "$HOME%s"
`, opencodeInstallerURL,
		openCodeCurrentBinRel,
		openCodeLegacyBinRel,
		openCodeLegacyBinRel,
		openCodeCurrentBinRel,
		openCodeLegacyBinRel,
		openCodeCurrentBinRel,
		openCodeLegacyBinRel)
}

func ensureOpenCodePathShim(ui *UI, r *Runner) error {
	ui.Step("Ensure OpenCode is on agent PATH")

	installedPath, ok := findInstalledOpenCodeBinaryWith(r.AgentOutput)
	if !ok {
		if !r.DryRun {
			return fmt.Errorf("OpenCode binary not found after install")
		}
		installedPath = agentHome + openCodeCurrentBinRel
	}

	shimPath := agentHome + openCodeLegacyBinRel
	if installedPath == shimPath {
		ui.SkipDone(shimPath + " already present")
		return nil
	}

	shimDir := agentHome + "/.local/bin"
	if err := agentEnsureSharedDir(shimDir, 0o2770); err != nil {
		return fmt.Errorf("ensure %s: %w", shimDir, err)
	}
	if _, err := r.AgentOutput("test", "-L", shimPath); err == nil {
		if err := r.AsAgent("refresh OpenCode PATH shim", "ln", "-sfn", installedPath, shimPath); err != nil {
			return fmt.Errorf("refresh OpenCode PATH shim: %w", err)
		}
		ui.Ok(fmt.Sprintf("Linked %s -> %s", shimPath, installedPath))
		return nil
	}
	if _, err := r.AgentOutput("test", "-e", shimPath); err == nil {
		ui.SkipDone(shimPath + " already present (not overwritten)")
		return nil
	}
	if err := r.AsAgent("link OpenCode into agent PATH", "ln", "-s", installedPath, shimPath); err != nil {
		return fmt.Errorf("link OpenCode PATH shim: %w", err)
	}
	ui.Ok(fmt.Sprintf("Linked %s -> %s", shimPath, installedPath))
	return nil
}

func newBootstrapOpenCodeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "opencode",
		Short: "Install or update OpenCode for the agent user and write a minimal config",
		Long: `Install or update OpenCode for the agent user and write a minimal global config.

Hazmat writes only a small agent-owned opencode.json with autoupdate disabled.
Runtime behavior, provider settings, commands, agents, skills, and auth can be
managed separately via OpenCode itself or 'hazmat config import opencode'.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
			r := NewRunner(ui, flagVerbose, flagDryRun)
			return openCodeHarness.Bootstrap(ui, r)
		},
	}
}

func runOpenCodeBootstrap(ui *UI, r *Runner) error {
	ui.Step(fmt.Sprintf("Verify agent user %q", agentUser))
	if _, err := requireAgentUser(); err != nil {
		return err
	}
	ui.Ok(fmt.Sprintf("Agent user %s exists", agentUser))

	if err := runHarnessInstallOrUpdateStep(ui, r, harnessInstallOrUpdateStep{
		DisplayName:   "OpenCode",
		TempPattern:   "hazmat-opencode-bootstrap-*.sh",
		InstallReason: "download and install or update OpenCode as agent user",
		BuildScript: func(bool) (string, error) {
			return openCodeInstallScript(), nil
		},
		FindExisting: findInstalledOpenCodeBinaryWith,
	}); err != nil {
		return err
	}

	if err := ensureOpenCodePathShim(ui, r); err != nil {
		return err
	}

	ui.Step("Write agent OpenCode config")
	configDir := agentHome + "/.config/opencode"
	configPath := configDir + "/opencode.json"
	if err := agentEnsureSharedDir(configDir, 0o2770); err != nil {
		return fmt.Errorf("ensure %s: %w", configDir, err)
	}
	if _, err := r.AgentOutput("test", "-f", configPath); err == nil {
		ui.SkipDone(configPath + " already present (not overwritten)")
	} else {
		if err := agentWriteSharedFile(configPath, []byte(agentOpenCodeConfigJSON), 0o660); err != nil {
			return fmt.Errorf("write OpenCode config: %w", err)
		}
		ui.Ok(fmt.Sprintf("Wrote %s (0660)", configPath))
	}

	ui.Step("Create OpenCode data directory")
	dataDir := agentHome + "/.local/share/opencode"
	if err := agentEnsureSharedDir(dataDir, 0o2770); err != nil {
		return fmt.Errorf("ensure %s: %w", dataDir, err)
	}
	ui.Ok(fmt.Sprintf("Prepared %s", dataDir))

	return nil
}
