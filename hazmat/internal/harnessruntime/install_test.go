package harnessruntime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInstallOrUpdateStepRunsInstallerEvenWhenExistingBinaryFound(t *testing.T) {
	tempDir := t.TempDir()
	var messages []string
	var runReason string
	var runArgs []string

	err := RunInstallOrUpdateStep(InstallOrUpdateStep{
		DisplayName:   "Codex",
		TempPattern:   "hazmat-codex-*.sh",
		InstallReason: "install Codex",
		BuildScript: func(dryRun bool) (string, error) {
			if dryRun {
				t.Fatal("BuildScript got dryRun=true")
			}
			return "echo install", nil
		},
		FindExisting: func(CommandReader) (string, bool) {
			return testAgentHome + "/.local/bin/codex", true
		},
	}, InstallOrUpdateOptions{
		TempDir: tempDir,
		Step: func(message string) {
			messages = append(messages, "step:"+message)
		},
		OK: func(message string) {
			messages = append(messages, "ok:"+message)
		},
		RunVisible: func(reason string, args ...string) error {
			runReason = reason
			runArgs = append([]string(nil), args...)
			raw, err := os.ReadFile(args[len(args)-1])
			if err != nil {
				t.Fatalf("read temp script: %v", err)
			}
			if string(raw) != "echo install" {
				t.Fatalf("script = %q, want install script", string(raw))
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("RunInstallOrUpdateStep: %v", err)
	}

	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "Found existing Codex at "+testAgentHome+"/.local/bin/codex; refreshing to latest") {
		t.Fatalf("messages = %#v", messages)
	}
	if !strings.Contains(joined, "Codex installed or updated") {
		t.Fatalf("messages = %#v", messages)
	}
	if runReason != "install Codex" {
		t.Fatalf("run reason = %q", runReason)
	}
	if len(runArgs) != 2 || runArgs[0] != "/bin/bash" || filepath.Dir(runArgs[1]) != tempDir {
		t.Fatalf("run args = %#v", runArgs)
	}
	if entries, err := os.ReadDir(tempDir); err != nil {
		t.Fatalf("read temp dir: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("temp script was not removed: %#v", entries)
	}
}

func TestRunInstallOrUpdateStepReportsDryRunMissingMessage(t *testing.T) {
	var messages []string

	err := RunInstallOrUpdateStep(InstallOrUpdateStep{
		DisplayName:      "Antigravity (agy)",
		TempPattern:      "hazmat-antigravity-*.sh",
		InstallReason:    "install Antigravity",
		MissingDryRunMsg: "Would install Antigravity manually",
		BuildScript: func(dryRun bool) (string, error) {
			if !dryRun {
				t.Fatal("BuildScript got dryRun=false")
			}
			return "echo dry-run", nil
		},
		FindExisting: func(CommandReader) (string, bool) {
			return "", false
		},
	}, InstallOrUpdateOptions{
		DryRun:  true,
		TempDir: t.TempDir(),
		OK: func(message string) {
			messages = append(messages, message)
		},
		RunVisible: func(string, ...string) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("RunInstallOrUpdateStep: %v", err)
	}
	if !containsString(messages, "Would install Antigravity manually") {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestRunInstallOrUpdateStepReturnsBuildAndRunnerErrors(t *testing.T) {
	buildErr := errors.New("build failed")
	err := RunInstallOrUpdateStep(InstallOrUpdateStep{
		DisplayName: "Claude",
		BuildScript: func(bool) (string, error) {
			return "", buildErr
		},
	}, InstallOrUpdateOptions{TempDir: t.TempDir()})
	if !errors.Is(err, buildErr) {
		t.Fatalf("build error = %v, want %v", err, buildErr)
	}

	runErr := errors.New("runner failed")
	err = RunInstallOrUpdateStep(InstallOrUpdateStep{
		DisplayName:   "Claude",
		TempPattern:   "hazmat-claude-*.sh",
		InstallReason: "install Claude",
		BuildScript: func(bool) (string, error) {
			return "echo install", nil
		},
	}, InstallOrUpdateOptions{
		TempDir: t.TempDir(),
		RunVisible: func(string, ...string) error {
			return runErr
		},
	})
	if !errors.Is(err, runErr) {
		t.Fatalf("runner error = %v, want %v", err, runErr)
	}
	if !strings.Contains(err.Error(), "install Claude") {
		t.Fatalf("runner error lacks install context: %v", err)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
