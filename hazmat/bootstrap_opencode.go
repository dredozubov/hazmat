package main

import (
	"fmt"
	"os"
	"os/user"

	"github.com/spf13/cobra"
)

const (
	opencodeInstallerURL = "https://opencode.ai/install"
)

const agentOpenCodeConfigJSON = `{
  "$schema": "https://opencode.ai/config.json",
  "autoupdate": false
}
`

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
	if _, err := user.Lookup(agentUser); err != nil {
		return fmt.Errorf("agent user %q not found — run 'hazmat init' first", agentUser)
	}
	ui.Ok(fmt.Sprintf("Agent user %s exists", agentUser))

	ui.Step("Install OpenCode for agent user")
	opencodeBin := agentHome + "/.local/bin/opencode"
	if _, err := sudoOutput("test", "-x", opencodeBin); err == nil {
		ui.SkipDone("OpenCode already installed")
	} else {
		installScript := fmt.Sprintf(`#!/bin/bash
set -euo pipefail
installer=$(mktemp "${TMPDIR:-/tmp}/opencode-install.XXXXXX")
cleanup() { rm -f "$installer"; }
trap cleanup EXIT
curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail %q -o "$installer"
env OPENCODE_INSTALL_DIR="$HOME/.local/bin" bash "$installer" --no-modify-path
test -x "$HOME/.local/bin/opencode"
`, opencodeInstallerURL)

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
