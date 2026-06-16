package hazmat

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var errPiBinaryMissing = errors.New("Pi binary missing")

const (
	piBinRel      = "/.local/bin/pi"
	piStateDirRel = "/.pi/agent"
	piMissingHelp = "Error: Pi not installed for agent user. Run: hazmat harness update pi"
)

func findInstalledPiBinary() (string, bool) {
	return findInstalledPiBinaryWith(asAgentOutput)
}

func findInstalledPiBinaryWith(read func(args ...string) (string, error)) (string, bool) {
	path := agentHome + piBinRel
	if _, err := read("test", "-x", path); err == nil {
		return path, true
	}
	return "", false
}

func probePiHarness(read func(args ...string) (string, error)) harnessProbe {
	binaryPath, ok := findInstalledPiBinaryWith(read)
	if !ok {
		return harnessProbe{MissingReason: "manual Pi binary not found for agent user"}
	}
	probe := harnessProbe{
		Installed:  true,
		BinaryPath: binaryPath,
	}
	version, err := read(binaryPath, "--version")
	if err != nil {
		probe.VersionErr = fmt.Sprintf("verify Pi CLI with %s --version: %v", binaryPath, err)
		return probe
	}
	probe.Version = firstStatusLine(version)
	return probe
}

func piHarnessManagedCodeArtifacts() []harnessManagedArtifact {
	return nil
}

func piLaunchScript() string {
	return `cd "$SANDBOX_PROJECT_DIR" && ` +
		`{ test -x "$HOME` + piBinRel + `" || ` +
		`{ echo "` + piMissingHelp + `" >&2; exit 1; }; }; ` +
		`exec "$HOME` + piBinRel + `" "$@"`
}

func newBootstrapPiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pi",
		Short: "Verify a manually installed Pi CLI for the agent user",
		Long: `Verify a manually installed Pi CLI for the agent user.

Phase 1 does not run the upstream Pi installer automatically. Place or link
the Pi executable at /Users/agent/.local/bin/pi, then run this command. Hazmat
records Pi harness state only after 'pi --version' succeeds.

This command does not import host ~/.pi/agent settings, trust decisions,
sessions, skills, extensions, or auth.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			harness, _ := managedHarnessByID(HarnessPi)
			return runManagedHarnessUpdate(harness)
		},
	}
}

func runPiBootstrap(ui *UI, r *Runner) error {
	if err := prepareAgentUserForBootstrap(ui, r); err != nil {
		return err
	}

	ui.Step("Verify Pi CLI for agent user")
	binaryPath, version, err := probePiBinary(r.AgentOutput, r.DryRun)
	if errors.Is(err, errPiBinaryMissing) {
		return fmt.Errorf("%s", piManualInstallMessage())
	}
	if err != nil {
		return err
	}
	if binaryPath == "" {
		ui.Ok(fmt.Sprintf("Would verify Pi CLI at %s", agentHome+piBinRel))
	} else {
		ui.Ok(fmt.Sprintf("Pi CLI detected at %s (%s)", binaryPath, version))
	}

	ui.Step("Prepare Pi state directory")
	if r.DryRun {
		ui.Ok(fmt.Sprintf("Would prepare %s", agentHome+piStateDirRel))
		return nil
	}
	if err := ensurePiStateDir(); err != nil {
		return err
	}
	ui.Ok(fmt.Sprintf("Prepared %s", agentHome+piStateDirRel))
	return nil
}

func probePiBinary(read func(args ...string) (string, error), dryRun bool) (string, string, error) {
	binaryPath, ok := findInstalledPiBinaryWith(read)
	if !ok {
		if dryRun {
			return "", "", nil
		}
		return "", "", errPiBinaryMissing
	}

	version, err := read(binaryPath, "--version")
	if err != nil {
		return "", "", fmt.Errorf("verify Pi CLI with %s --version: %w", binaryPath, err)
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = "version output empty"
	}
	return binaryPath, version, nil
}

func ensurePiStateDir() error {
	if err := agentEnsureDir(agentHome+"/.pi", 0o700); err != nil {
		return err
	}
	if err := agentEnsureDir(agentHome+piStateDirRel, 0o700); err != nil {
		return err
	}
	return nil
}

func piManualInstallMessage() string {
	return fmt.Sprintf(`Pi CLI is not installed for the agent user.

Phase 1 Hazmat does not run the upstream Pi installer automatically. Install
Pi for the agent account using an audited path, then place or link the
executable here:

  %s

	After installing, verify and record the harness with:

	  hazmat harness update pi

	No host ~/.pi/agent profile is imported by this command.`, agentHome+piBinRel)
}
