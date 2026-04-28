package main

import (
	"fmt"
	"os"
)

type harnessInstallOrUpdateStep struct {
	DisplayName       string
	TempPattern       string
	InstallReason     string
	BuildScript       func(dryRun bool) (string, error)
	FindExisting      func(read func(args ...string) (string, error)) (string, bool)
	ExistingMessage   func(path string) string
	MissingDryRunMsg  string
	CompletionMessage string
}

func (s harnessInstallOrUpdateStep) existingMessage(path string) string {
	if s.ExistingMessage != nil {
		return s.ExistingMessage(path)
	}
	return fmt.Sprintf("Found existing %s at %s; refreshing to latest", s.DisplayName, path)
}

func (s harnessInstallOrUpdateStep) missingDryRunMessage() string {
	if s.MissingDryRunMsg != "" {
		return s.MissingDryRunMsg
	}
	return fmt.Sprintf("Would install latest %s for agent user", s.DisplayName)
}

func (s harnessInstallOrUpdateStep) completionMessage() string {
	if s.CompletionMessage != "" {
		return s.CompletionMessage
	}
	return fmt.Sprintf("%s installed or updated", s.DisplayName)
}

// runHarnessInstallOrUpdateStep intentionally has no "skip when installed"
// mode. Existing binaries are useful evidence for the status line, but
// bootstrap must still execute the harness installer so agent-owned harnesses
// do not silently drift behind host/upstream versions.
func runHarnessInstallOrUpdateStep(ui *UI, r *Runner, step harnessInstallOrUpdateStep) error {
	ui.Step(fmt.Sprintf("Install or update %s for agent user", step.DisplayName))
	if step.FindExisting != nil {
		if binaryPath, ok := step.FindExisting(r.AgentOutput); ok {
			ui.Ok(step.existingMessage(binaryPath))
		} else if r.DryRun {
			ui.Ok(step.missingDryRunMessage())
		}
	} else if r.DryRun {
		ui.Ok(step.missingDryRunMessage())
	}

	installScript, err := step.BuildScript(r.DryRun)
	if err != nil {
		return err
	}

	scriptFile, err := os.CreateTemp("/tmp", step.TempPattern)
	if err != nil {
		return fmt.Errorf("create %s bootstrap script: %w", step.DisplayName, err)
	}
	defer os.Remove(scriptFile.Name())
	if _, err := scriptFile.WriteString(installScript); err != nil {
		scriptFile.Close() //nolint:errcheck // error-path close; write error is more important
		return fmt.Errorf("write %s bootstrap script: %w", step.DisplayName, err)
	}
	scriptFile.Close() //nolint:errcheck // close-to-flush; chmod below catches problems
	if err := os.Chmod(scriptFile.Name(), 0o755); err != nil {
		return fmt.Errorf("chmod %s bootstrap script: %w", step.DisplayName, err)
	}

	if err := r.AsAgentVisible(step.InstallReason, "/bin/bash", scriptFile.Name()); err != nil {
		return fmt.Errorf("install %s: %w", step.DisplayName, err)
	}
	ui.Ok(step.completionMessage())
	return nil
}
