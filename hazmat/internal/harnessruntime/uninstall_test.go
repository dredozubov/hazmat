package harnessruntime

import (
	"errors"
	"strings"
	"testing"

	"hazmat/harnesses"
)

func TestBuildUninstallPlanCopiesPreservedAndDetectsMetadata(t *testing.T) {
	store := &fakeStateStore{
		snapshot: Snapshot{
			Harnesses: map[harnesses.ID]State{
				harnesses.Claude: {StateVersion: harnesses.ClaudeStateVersion},
			},
		},
	}
	preserved := []string{"auth state"}

	plan := BuildUninstallPlan(
		store,
		fakeArtifactRead(nil, nil),
		testAgentHome,
		harnesses.MustSpec(harnesses.Claude),
		nil,
		preserved,
	)
	preserved[0] = "mutated"

	if plan.StateErr != nil {
		t.Fatalf("StateErr = %v", plan.StateErr)
	}
	if !plan.MetadataPresent {
		t.Fatal("expected metadata to be present")
	}
	if plan.Preserved[0] != "auth state" {
		t.Fatalf("Preserved = %#v, want copy", plan.Preserved)
	}
	if !plan.HasWork() {
		t.Fatal("expected plan to have work from metadata")
	}
}

func TestExecuteUninstallPlanRemovesExistingArtifactsAndMetadata(t *testing.T) {
	store := &fakeStateStore{
		snapshot: Snapshot{
			Harnesses: map[harnesses.ID]State{
				harnesses.Claude: {StateVersion: harnesses.ClaudeStateVersion},
			},
		},
	}
	path := testAgentHome + "/.local/bin/claude"
	plan := UninstallPlan{
		Spec:            harnesses.MustSpec(harnesses.Claude),
		Artifacts:       []ArtifactStatus{{Artifact: FileArtifact(path, "Claude Code executable"), Exists: true}},
		MetadataPresent: true,
	}
	var removed []string

	err := ExecuteUninstallPlan(plan, UninstallOptions{
		Store:     store,
		AgentHome: testAgentHome,
		Remove: func(_ string, args ...string) error {
			removed = append(removed, args[len(args)-1])
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ExecuteUninstallPlan: %v", err)
	}

	if strings.Join(removed, ",") != path {
		t.Fatalf("removed = %#v, want %s", removed, path)
	}
	if store.snapshot.HasHarnessState() {
		t.Fatalf("metadata still present: %+v", store.snapshot.Harnesses)
	}
}

func TestExecuteUninstallPlanDryRunPreservesMetadata(t *testing.T) {
	store := &fakeStateStore{
		snapshot: Snapshot{
			Harnesses: map[harnesses.ID]State{
				harnesses.Claude: {StateVersion: harnesses.ClaudeStateVersion},
			},
		},
	}

	err := ExecuteUninstallPlan(UninstallPlan{
		Spec:            harnesses.MustSpec(harnesses.Claude),
		MetadataPresent: true,
	}, UninstallOptions{
		Store:     store,
		AgentHome: testAgentHome,
		DryRun:    true,
		Remove: func(string, ...string) error {
			t.Fatal("unexpected artifact removal")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ExecuteUninstallPlan: %v", err)
	}
	if !store.snapshot.HasHarnessState() {
		t.Fatal("dry-run removed harness metadata")
	}
}

func TestExecuteUninstallPlanRequiresForceForDrift(t *testing.T) {
	path := testAgentHome + "/.local/bin/codex"
	plan := UninstallPlan{
		Spec: harnesses.MustSpec(harnesses.Codex),
		Artifacts: []ArtifactStatus{{
			Artifact: FileArtifact(path, "Codex executable"),
			Exists:   true,
			Drift:    "symlink not allowed",
		}},
	}
	removeErr := errors.New("remove failed")

	err := ExecuteUninstallPlan(plan, UninstallOptions{
		Store:     &fakeStateStore{},
		AgentHome: testAgentHome,
		Remove: func(string, ...string) error {
			return removeErr
		},
	})
	if err == nil || !strings.Contains(err.Error(), "planned artifacts drifted") {
		t.Fatalf("ExecuteUninstallPlan error = %v, want drift refusal", err)
	}

	err = ExecuteUninstallPlan(plan, UninstallOptions{
		Store:     &fakeStateStore{},
		AgentHome: testAgentHome,
		Force:     true,
		Remove: func(string, ...string) error {
			return removeErr
		},
	})
	if !errors.Is(err, removeErr) {
		t.Fatalf("forced ExecuteUninstallPlan error = %v, want %v", err, removeErr)
	}
}
