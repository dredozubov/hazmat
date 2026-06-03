package hazmat

import (
	"hazmat/internal/harnessruntime"
)

type harnessInstallOrUpdateStep = harnessruntime.InstallOrUpdateStep

// runHarnessInstallOrUpdateStep intentionally has no "skip when installed"
// mode. Existing binaries are useful evidence for the status line, but
// bootstrap must still execute the harness installer so agent-owned harnesses
// do not silently drift behind host/upstream versions.
func runHarnessInstallOrUpdateStep(ui *UI, r *Runner, step harnessInstallOrUpdateStep) error {
	return harnessruntime.RunInstallOrUpdateStep(step, harnessruntime.InstallOrUpdateOptions{
		DryRun:     r.DryRun,
		Read:       r.AgentOutput,
		Step:       ui.Step,
		OK:         ui.Ok,
		RunVisible: r.AsAgentVisible,
		TempDir:    "/tmp",
	})
}
