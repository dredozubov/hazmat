//go:build darwin

package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hazmat/internal/setup"
)

func TestDarwinSetupVerificationRejectsWrongHostWrapperSubcommand(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	wrapperDir := filepath.Join(tmp, hostWrapperDirRel)
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hazmatBin := filepath.Join(tmp, "hazmat")
	if err := os.WriteFile(hazmatBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, wrapper := range managedHostWrapperSpecs() {
		subcommand := wrapper.Subcommand
		if wrapper.Name == hostShellWrapperName {
			subcommand = "exec"
		}
		path := filepath.Join(wrapperDir, wrapper.Name)
		if err := os.WriteFile(path, []byte(setup.HostWrapperContent(hazmatBin, subcommand)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ui := &UI{}
	darwinSetupVerificationBackend{}.verifyHostWrappers(ui)
	report := ui.diagnosticReport()
	assertReportHasFinding(t, report, findingSetupHostWrappers)
	if !diagnosticReportAdviceMentions(report, "hazmat doctor --fix") {
		t.Fatalf("setup verification report does not point at doctor repair: %s", diagnosticReportJSON(t, report))
	}
	var found bool
	for _, finding := range report.Findings {
		if strings.Contains(finding.Message, `does not dispatch managed subcommand "shell"`) &&
			strings.Contains(strings.Join(finding.Details, "\n"), hostShellWrapperName) {
			found = true
		}
	}
	if !found {
		t.Fatalf("setup verification report missing wrong-subcommand detail: %s", diagnosticReportJSON(t, report))
	}
}

func TestDarwinSetupVerificationReportsAgentEnvDriftThroughAgentRead(t *testing.T) {
	savedRead := agentReadFile
	agentReadFile = func(path string) ([]byte, error) {
		if path != agentEnvPath {
			t.Fatalf("agentReadFile path = %q, want %q", path, agentEnvPath)
		}
		stale := strings.Replace(setup.AgentEnvContent(defaultAgentPath), `export HOMEBREW_NO_AUTO_UPDATE="${HOMEBREW_NO_AUTO_UPDATE:-1}"`+"\n", "", 1)
		return []byte(stale), nil
	}
	t.Cleanup(func() { agentReadFile = savedRead })

	ui := &UI{}
	darwinSetupVerificationBackend{}.verifyAgentEnv(ui)
	report := ui.diagnosticReport()
	assertReportHasFinding(t, report, findingSetupAgentEnv)
	if !diagnosticReportAdviceMentions(report, "hazmat doctor --fix") {
		t.Fatalf("setup verification report does not point at doctor repair: %s", diagnosticReportJSON(t, report))
	}
	var found bool
	for _, finding := range report.Findings {
		if strings.Contains(finding.Message, "agent env content drifted") {
			found = true
		}
	}
	if !found {
		t.Fatalf("setup verification report missing agent env drift detail: %s", diagnosticReportJSON(t, report))
	}
}
