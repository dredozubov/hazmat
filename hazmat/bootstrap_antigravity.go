package hazmat

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	antigravityInstallerURL    = "https://antigravity.google/cli/install.sh"
	antigravityInstallerSHA256 = "ee1ea43ce4e9e56356c4ab6dad907ef357ae4bdfcaadb682735909fb57c9c640"

	antigravityBinRel      = "/.local/bin/agy"
	antigravityMissingHelp = "Error: Antigravity (agy) not installed for agent user. Run: hazmat harness update antigravity"
	antigravityStateDirRel = "/.gemini/antigravity-cli"
)

func findInstalledAntigravityBinary() (string, bool) {
	return findInstalledAntigravityBinaryWith(asAgentOutput)
}

func findInstalledAntigravityBinaryWith(read func(args ...string) (string, error)) (string, bool) {
	path := agentHome + antigravityBinRel
	if _, err := read("test", "-x", path); err == nil {
		return path, true
	}
	return "", false
}

func probeAntigravityHarness(read func(args ...string) (string, error)) harnessProbe {
	return probeHarnessBinary(read, findInstalledAntigravityBinaryWith, "--version")
}

func antigravityHarnessManagedCodeArtifacts() []harnessManagedArtifact {
	return []harnessManagedArtifact{
		harnessFileArtifact(agentHome+antigravityBinRel, "Antigravity (agy) executable"),
	}
}

func antigravityLaunchScript() string {
	return `cd "$SANDBOX_PROJECT_DIR" && ` +
		`{ test -x "$HOME` + antigravityBinRel + `" || ` +
		`{ echo "` + antigravityMissingHelp + `" >&2; exit 1; }; }; ` +
		`exec "$HOME` + antigravityBinRel + `" "$@"`
}

func antigravityInstallScript() string {
	// agy is a flat native binary; the official installer downloads it directly
	// (no npm/node toolchain). We pin the installer URL and verify its SHA-256
	// before executing it, mirroring how Claude Code's bootstrap pins its
	// installer.
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
installer=$(mktemp "${TMPDIR:-/tmp}/antigravity-install.XXXXXX")
cleanup() { rm -f "$installer"; }
trap cleanup EXIT
curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail %q -o "$installer"
actual=$(shasum -a 256 "$installer" | awk '{print $1}')
expected=%q
if [[ "$actual" != "$expected" ]]; then
  echo "Antigravity installer checksum mismatch: expected $expected, got $actual" >&2
  exit 1
fi
bash "$installer"
test -x "$HOME%s"
`, antigravityInstallerURL, antigravityInstallerSHA256, antigravityBinRel)
}

func newBootstrapAntigravityCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "antigravity",
		Aliases: []string{"agy"},
		Short:   "Install or update Antigravity (agy) for the agent user",
		Long: `Install or update Antigravity (agy) for the agent user.

Hazmat downloads the pinned, checksum-verified official installer and runs it as
the agent user. agy is a flat native binary installed at ~/.local/bin/agy; no
Node.js toolchain is required. agy keeps its own config and runtime state under
~/.gemini/antigravity-cli.

agy authenticates via the GEMINI_API_KEY / ANTIGRAVITY_API_KEY environment
variables (configure with 'hazmat config agent'). Its interactive OAuth flow is
Keychain-backed and remains an external, adapter-required boundary.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			harness, _ := managedHarnessByID(HarnessAntigravity)
			return runManagedHarnessUpdate(harness)
		},
	}
}

func runAntigravityBootstrap(ui *UI, r *Runner) error {
	if err := prepareAgentUserForBootstrap(ui, r); err != nil {
		return err
	}

	if err := runHarnessInstallOrUpdateStep(ui, r, harnessInstallOrUpdateStep{
		DisplayName:   "Antigravity (agy)",
		TempPattern:   "hazmat-antigravity-bootstrap-*.sh",
		InstallReason: "download, verify, and install or update Antigravity (agy) as agent user",
		BuildScript: func(bool) (string, error) {
			return antigravityInstallScript(), nil
		},
		FindExisting: findInstalledAntigravityBinaryWith,
	}); err != nil {
		return err
	}

	ui.Step("Create Antigravity state directory")
	stateDir := agentHome + antigravityStateDirRel
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
