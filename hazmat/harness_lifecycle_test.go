package main

import (
	"errors"
	"strings"
	"testing"
)

func TestHarnessCommandExposesLifecycleSubcommands(t *testing.T) {
	cmd := newHarnessCmd()
	for _, name := range []string{"status", "update", "uninstall"} {
		found, _, err := cmd.Find([]string{name})
		if err != nil {
			t.Fatalf("Find(%s): %v", name, err)
		}
		if found == nil || found.Name() != name {
			t.Fatalf("expected harness %s subcommand, got %#v", name, found)
		}
	}
}

func TestInspectManagedHarnessStatusIncludesProbeStateImportAndCredentials(t *testing.T) {
	restore := stubHarnessCredentialSummary(t, "2 configured; hazmat config agent")
	defer restore()

	harness, ok := managedHarnessByID(HarnessCodex)
	if !ok {
		t.Fatal("missing Codex harness")
	}
	read := fakeHarnessRead(
		map[string]harnessManagedArtifactKind{
			agentHome + codexBinRel: harnessArtifactFile,
		},
		map[string]string{
			agentHome + codexBinRel: "codex-cli 1.2.3\n",
		},
	)

	status := inspectManagedHarnessStatus(harness, HazmatState{
		Harnesses: map[HarnessID]HarnessState{
			HarnessCodex: {
				StateVersion:    codexHarnessStateVersion,
				LastImportRunAt: "2026-06-01T12:00:00Z",
			},
		},
	}, nil, read)

	if !status.Probe.Installed {
		t.Fatal("expected installed probe")
	}
	if status.Probe.BinaryPath != agentHome+codexBinRel {
		t.Fatalf("BinaryPath = %q", status.Probe.BinaryPath)
	}
	if status.Probe.Version != "codex-cli 1.2.3" {
		t.Fatalf("Version = %q", status.Probe.Version)
	}
	if status.StateSummary != "recorded v"+codexHarnessStateVersion {
		t.Fatalf("StateSummary = %q", status.StateSummary)
	}
	if status.ImportSummary != "2026-06-01T12:00:00Z" {
		t.Fatalf("ImportSummary = %q", status.ImportSummary)
	}
	if status.CredentialSummary != "2 configured; hazmat config agent" {
		t.Fatalf("CredentialSummary = %q", status.CredentialSummary)
	}
	if status.NextAction != "hazmat harness update codex" {
		t.Fatalf("NextAction = %q", status.NextAction)
	}
}

func TestInspectHarnessArtifactDetectsDrift(t *testing.T) {
	path := agentHome + "/.local/lib/node_modules/@google/gemini-cli"
	status := inspectHarnessArtifact(
		fakeHarnessRead(map[string]harnessManagedArtifactKind{
			path: harnessArtifactFile,
		}, nil),
		harnessDirArtifact(path, "Gemini CLI npm package"),
	)
	if !status.Exists {
		t.Fatal("expected artifact to exist")
	}
	if !strings.Contains(status.Drift, "expected directory") {
		t.Fatalf("Drift = %q", status.Drift)
	}
}

func TestBuildHarnessUninstallPlanKeepsHermesManualBinaryOutOfManagedCode(t *testing.T) {
	restoreState := isolateStateFile(t)
	defer restoreState()

	if err := hermesHarness.RecordInstalled(); err != nil {
		t.Fatalf("RecordInstalled: %v", err)
	}
	harness, ok := managedHarnessByID(HarnessHermes)
	if !ok {
		t.Fatal("missing Hermes harness")
	}

	plan := buildHarnessUninstallPlan(harness, fakeHarnessRead(nil, nil))
	if plan.StateErr != nil {
		t.Fatalf("StateErr: %v", plan.StateErr)
	}
	if !plan.MetadataPresent {
		t.Fatal("expected Hermes metadata to be present")
	}
	if len(plan.Artifacts) != 0 {
		t.Fatalf("Hermes managed artifacts = %d, want none", len(plan.Artifacts))
	}
	if !containsHarnessLifecycleString(plan.Preserved, agentHome+hermesBinRel+" manual Hermes executable") {
		t.Fatalf("preserved artifacts did not mention manual Hermes binary: %#v", plan.Preserved)
	}
}

func TestRunManagedHarnessUpdateCallsBootstrap(t *testing.T) {
	calls := 0
	harness := ManagedHarness{
		Spec: HarnessSpec{
			ID:          HarnessID("test"),
			DisplayName: "Test",
		},
		Bootstrap: func(*UI, *Runner) error {
			calls++
			return nil
		},
	}
	if err := runManagedHarnessUpdate(harness); err != nil {
		t.Fatalf("runManagedHarnessUpdate: %v", err)
	}
	if calls != 1 {
		t.Fatalf("Bootstrap calls = %d, want 1", calls)
	}
}

func stubHarnessCredentialSummary(t *testing.T, value string) func() {
	t.Helper()
	old := summarizeHarnessCredentialsForStatus
	summarizeHarnessCredentialsForStatus = func(HarnessID) string {
		return value
	}
	return func() {
		summarizeHarnessCredentialsForStatus = old
	}
}

func fakeHarnessRead(paths map[string]harnessManagedArtifactKind, versions map[string]string) func(args ...string) (string, error) {
	if paths == nil {
		paths = map[string]harnessManagedArtifactKind{}
	}
	if versions == nil {
		versions = map[string]string{}
	}
	return func(args ...string) (string, error) {
		if len(args) >= 3 && (args[0] == "test" || args[0] == "/usr/bin/test") {
			kind, ok := paths[args[2]]
			if !ok {
				return "", errors.New("missing")
			}
			switch args[1] {
			case "-e", "-x":
				return "", nil
			case "-f":
				if kind == harnessArtifactFile {
					return "", nil
				}
			case "-d":
				if kind == harnessArtifactDir {
					return "", nil
				}
			case "-L":
				return "", errors.New("not symlink")
			}
			return "", errors.New("wrong kind")
		}
		if len(args) > 0 {
			if version, ok := versions[args[0]]; ok {
				return version, nil
			}
		}
		return "", errors.New("unexpected command")
	}
}

func containsHarnessLifecycleString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
