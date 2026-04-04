package main

import (
	"fmt"
	"os"

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
	return findInstalledOpenCodeBinaryWith(sudoOutput)
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

func ensureOpenCodePathShim(ui *UI, r *Runner) error {
	ui.Step("Ensure OpenCode is on agent PATH")

	installedPath, ok := findInstalledOpenCodeBinaryWith(r.SudoOutput)
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
	if err := r.Sudo("create agent OpenCode PATH directory", "install", "-d", "-o", agentUser, "-g", "staff", "-m", "0700", shimDir); err != nil {
		return fmt.Errorf("ensure %s: %w", shimDir, err)
	}
	if _, err := r.SudoOutput("test", "-L", shimPath); err == nil {
		if err := r.Sudo("refresh OpenCode PATH shim", "ln", "-sfn", installedPath, shimPath); err != nil {
			return fmt.Errorf("refresh OpenCode PATH shim: %w", err)
		}
		ui.Ok(fmt.Sprintf("Linked %s -> %s", shimPath, installedPath))
		return nil
	}
	if _, err := r.SudoOutput("test", "-e", shimPath); err == nil {
		ui.SkipDone(shimPath + " already present (not overwritten)")
		return nil
	}
	if err := r.Sudo("link OpenCode into agent PATH", "ln", "-s", installedPath, shimPath); err != nil {
		return fmt.Errorf("link OpenCode PATH shim: %w", err)
	}
	ui.Ok(fmt.Sprintf("Linked %s -> %s", shimPath, installedPath))
	return nil
}

func newBootstrapOpenCodeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "opencode",
		Short: "Install OpenCode for the agent user and write a minimal config",
		Long: `Install OpenCode for the agent user and write a minimal global config.

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

	ui.Step("Install OpenCode for agent user")
	if opencodeBin, ok := findInstalledOpenCodeBinaryWith(r.SudoOutput); ok {
		ui.SkipDone(fmt.Sprintf("OpenCode already installed at %s", opencodeBin))
	} else {
		installScript := fmt.Sprintf(`#!/bin/bash
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

		scriptFile, err := os.CreateTemp("/tmp", "hazmat-opencode-bootstrap-*.sh")
		if err != nil {
			return fmt.Errorf("create OpenCode bootstrap script: %w", err)
		}
		defer os.Remove(scriptFile.Name())
		if _, err := scriptFile.WriteString(installScript); err != nil {
			scriptFile.Close() //nolint:errcheck // error-path close; write error is more important
			return fmt.Errorf("write OpenCode bootstrap script: %w", err)
		}
		scriptFile.Close() //nolint:errcheck // close-to-flush; chmod below catches problems
		if err := os.Chmod(scriptFile.Name(), 0o755); err != nil {
			return fmt.Errorf("chmod OpenCode bootstrap script: %w", err)
		}

		if err := r.SudoVisible("download and install OpenCode as agent user",
			"-u", agentUser, "-H", "bash", scriptFile.Name()); err != nil {
			return fmt.Errorf("install OpenCode: %w", err)
		}
		ui.Ok("OpenCode installed")
	}

	if err := ensureOpenCodePathShim(ui, r); err != nil {
		return err
	}

	ui.Step("Write agent OpenCode config")
	configDir := agentHome + "/.config/opencode"
	configPath := configDir + "/opencode.json"
	if err := r.Sudo("create agent OpenCode config directory", "install", "-d", "-o", agentUser, "-g", "staff", "-m", "0700", configDir); err != nil {
		return fmt.Errorf("ensure %s: %w", configDir, err)
	}
	if _, err := sudoOutput("test", "-f", configPath); err == nil {
		ui.SkipDone(configPath + " already present (not overwritten)")
	} else {
		if err := r.SudoWriteFile("write agent OpenCode config", configPath, agentOpenCodeConfigJSON); err != nil {
			return fmt.Errorf("write OpenCode config: %w", err)
		}
		if err := r.Sudo("set OpenCode config ownership", "chown", agentUser+":staff", configPath); err != nil {
			return fmt.Errorf("chown OpenCode config: %w", err)
		}
		if err := r.Sudo("set OpenCode config permissions", "chmod", "0600", configPath); err != nil {
			return fmt.Errorf("chmod OpenCode config: %w", err)
		}
		ui.Ok(fmt.Sprintf("Wrote %s (0600)", configPath))
	}

	ui.Step("Create OpenCode data directory")
	dataDir := agentHome + "/.local/share/opencode"
	if err := r.Sudo("create agent OpenCode data directory", "install", "-d", "-o", agentUser, "-g", "staff", "-m", "0700", dataDir); err != nil {
		return fmt.Errorf("ensure %s: %w", dataDir, err)
	}
	ui.Ok(fmt.Sprintf("Prepared %s", dataDir))

	return nil
}
