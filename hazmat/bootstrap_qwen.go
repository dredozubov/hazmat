package hazmat

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	qwenBinRel      = "/.local/bin/qwen"
	qwenNpmPackage  = "@qwen-code/qwen-code@latest"
	qwenMissingHelp = "Error: Qwen Code not installed for agent user. Run: hazmat bootstrap qwen"
	qwenStateDirRel = "/.qwen"
)

func findInstalledQwenBinary() (string, bool) {
	return findInstalledQwenBinaryWith(asAgentOutput)
}

func findInstalledQwenBinaryWith(read func(args ...string) (string, error)) (string, bool) {
	path := agentHome + qwenBinRel
	if _, err := read("test", "-x", path); err == nil {
		return path, true
	}
	return "", false
}

func probeQwenHarness(read func(args ...string) (string, error)) harnessProbe {
	return probeHarnessBinary(read, findInstalledQwenBinaryWith, "--version")
}

func qwenHarnessManagedCodeArtifacts() []harnessManagedArtifact {
	return []harnessManagedArtifact{
		harnessFileArtifact(agentHome+qwenBinRel, "Qwen Code executable"),
		harnessNpmPackageDirArtifact(agentHome+"/.local/lib/node_modules/@qwen-code/qwen-code", "@qwen-code/qwen-code", "Qwen Code npm package"),
	}
}

func qwenLaunchScript() string {
	return `cd "$SANDBOX_PROJECT_DIR" && ` +
		`{ test -x "$HOME` + qwenBinRel + `" || ` +
		`{ echo "` + qwenMissingHelp + `" >&2; exit 1; }; }; ` +
		`exec "$HOME` + qwenBinRel + `" "$@"`
}

func qwenInstallScript() string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
if ! command -v node >/dev/null 2>&1; then
  echo "Node.js not found on agent PATH — install Node.js 20+ first: brew install node" >&2
  exit 1
fi
if ! node -e 'const major=Number(process.versions.node.split(".")[0]); process.exit(major >= 20 ? 0 : 1)' >/dev/null 2>&1; then
  echo "Qwen Code requires Node.js 20 or newer on the agent PATH" >&2
  exit 1
fi
mkdir -p "$HOME/.local/bin" "$HOME/.local/lib/node_modules"
export NPM_CONFIG_PREFIX="$HOME/.local"
npm install -g --silent %q
test -x "$HOME%s"
`, qwenNpmPackage, qwenBinRel)
}

func newBootstrapQwenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "qwen",
		Short: "Install or update Qwen Code for the agent user",
		Long: `Install or update Qwen Code for the agent user.

Hazmat installs the official @qwen-code/qwen-code npm package into the agent
user's ~/.local prefix. Node.js 20 or newer must be available on the agent's
PATH (Homebrew node at /opt/homebrew/bin/node works). Qwen Code keeps its own
auth, settings, extension, and runtime state under ~/.qwen.

This command does not import host ~/.qwen state.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			harness, _ := managedHarnessByID(HarnessQwen)
			return runManagedHarnessUpdate(harness)
		},
	}
}

func runQwenBootstrap(ui *UI, r *Runner) error {
	if err := verifyAgentUserForBootstrap(ui, r); err != nil {
		return err
	}

	if err := runHarnessInstallOrUpdateStep(ui, r, harnessInstallOrUpdateStep{
		DisplayName:   "Qwen Code",
		TempPattern:   "hazmat-qwen-bootstrap-*.sh",
		InstallReason: "install or update Qwen Code as agent user via npm",
		BuildScript: func(bool) (string, error) {
			return qwenInstallScript(), nil
		},
		FindExisting: findInstalledQwenBinaryWith,
	}); err != nil {
		return err
	}

	ui.Step("Create Qwen state directory")
	stateDir := agentHome + qwenStateDirRel
	if r.DryRun {
		ui.Ok(fmt.Sprintf("Would prepare %s", stateDir))
	} else {
		if err := agentEnsureSharedDir(stateDir, 0o2770); err != nil {
			return fmt.Errorf("ensure %s: %w", stateDir, err)
		}
		ui.Ok(fmt.Sprintf("Prepared %s", stateDir))
	}

	return nil
}
