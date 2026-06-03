package harnessruntime

import (
	"fmt"
	"os"
)

type InstallOrUpdateStep struct {
	DisplayName       string
	TempPattern       string
	InstallReason     string
	BuildScript       func(dryRun bool) (string, error)
	FindExisting      func(read CommandReader) (string, bool)
	ExistingMessage   func(path string) string
	MissingDryRunMsg  string
	CompletionMessage string
}

type InstallOrUpdateOptions struct {
	DryRun     bool
	Read       CommandReader
	Step       func(string)
	OK         func(string)
	RunVisible func(reason string, args ...string) error
	TempDir    string
}

func (s InstallOrUpdateStep) existingMessage(path string) string {
	if s.ExistingMessage != nil {
		return s.ExistingMessage(path)
	}
	return fmt.Sprintf("Found existing %s at %s; refreshing to latest", s.DisplayName, path)
}

func (s InstallOrUpdateStep) missingDryRunMessage() string {
	if s.MissingDryRunMsg != "" {
		return s.MissingDryRunMsg
	}
	return fmt.Sprintf("Would install latest %s for agent user", s.DisplayName)
}

func (s InstallOrUpdateStep) completionMessage() string {
	if s.CompletionMessage != "" {
		return s.CompletionMessage
	}
	return fmt.Sprintf("%s installed or updated", s.DisplayName)
}

// RunInstallOrUpdateStep intentionally has no "skip when installed" mode.
// Existing binaries are useful evidence for the status line, but bootstrap
// must still execute the harness installer so agent-owned harnesses do not
// silently drift behind host/upstream versions.
func RunInstallOrUpdateStep(step InstallOrUpdateStep, opts InstallOrUpdateOptions) error {
	if opts.Step != nil {
		opts.Step(fmt.Sprintf("Install or update %s for agent user", step.DisplayName))
	}
	if step.FindExisting != nil {
		if binaryPath, ok := step.FindExisting(opts.Read); ok {
			if opts.OK != nil {
				opts.OK(step.existingMessage(binaryPath))
			}
		} else if opts.DryRun && opts.OK != nil {
			opts.OK(step.missingDryRunMessage())
		}
	} else if opts.DryRun && opts.OK != nil {
		opts.OK(step.missingDryRunMessage())
	}

	installScript, err := step.BuildScript(opts.DryRun)
	if err != nil {
		return err
	}

	tempDir := opts.TempDir
	if tempDir == "" {
		tempDir = "/tmp"
	}
	scriptFile, err := os.CreateTemp(tempDir, step.TempPattern)
	if err != nil {
		return fmt.Errorf("create %s bootstrap script: %w", step.DisplayName, err)
	}
	defer os.Remove(scriptFile.Name()) //nolint:errcheck // best-effort temp cleanup
	if _, err := scriptFile.WriteString(installScript); err != nil {
		scriptFile.Close() //nolint:errcheck // error-path close; write error is more important
		return fmt.Errorf("write %s bootstrap script: %w", step.DisplayName, err)
	}
	scriptFile.Close() //nolint:errcheck // close-to-flush; chmod below catches problems
	if err := os.Chmod(scriptFile.Name(), 0o755); err != nil {
		return fmt.Errorf("chmod %s bootstrap script: %w", step.DisplayName, err)
	}

	if opts.RunVisible == nil {
		return fmt.Errorf("install %s: install runner is not configured", step.DisplayName)
	}
	if err := opts.RunVisible(step.InstallReason, "/bin/bash", scriptFile.Name()); err != nil {
		return fmt.Errorf("install %s: %w", step.DisplayName, err)
	}
	if opts.OK != nil {
		opts.OK(step.completionMessage())
	}
	return nil
}
