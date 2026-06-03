package sessionplanner

import (
	"slices"
	"testing"

	"hazmat/hostfacts"
	"hazmat/sessionbackend"
	"hazmat/sessioncontract"
	"hazmat/sessionmeta"
)

func TestBuildComposesContractAndBackendPlans(t *testing.T) {
	input := Input{
		Contract: sessioncontract.PlanInput{
			Target:              "codex",
			Mode:                sessionmeta.ModeNative,
			ProjectDir:          "/workspace/project",
			ReadOnlyDirs:        []string{"/opt/sdk"},
			ReadWriteExtensions: []string{"/tmp/cache"},
			NetworkMode:         sessionmeta.NetworkNone,
			ActiveIntegrations:  []string{"go"},
			IntegrationEnv: map[string]string{
				"GOROOT": "/opt/go",
			},
			Snapshot: sessioncontract.Snapshot{Enabled: true, Excludes: []string{".venv/"}},
		},
		Backend: sessionbackend.Input{
			Target:             "codex",
			Mode:               sessionmeta.ModeNative,
			ProjectDir:         "/workspace/project",
			ReadOnlyDirs:       []string{"/opt/sdk"},
			ReadWriteDirs:      []string{"/tmp/cache"},
			NetworkMode:        sessionmeta.NetworkNone,
			Integrations:       []string{"go"},
			IntegrationEnvKeys: []string{"GOROOT"},
			HostFacts:          hostfacts.ForGOOS("darwin"),
		},
	}

	plan := Build(input)
	if plan.Contract.Target != "codex" || plan.Contract.ProjectDir != "/workspace/project" {
		t.Fatalf("Contract identity = %+v", plan.Contract)
	}
	if !plan.Contract.NetworkPolicy.DenyAllEgress {
		t.Fatalf("Contract NetworkPolicy = %+v, want deny-all", plan.Contract.NetworkPolicy)
	}
	if plan.Backend.Backend != sessionbackend.KindDarwinNative {
		t.Fatalf("Backend = %q", plan.Backend.Backend)
	}
	if !slices.Equal(plan.Backend.IntegrationEnvKeys, []string{"GOROOT"}) {
		t.Fatalf("IntegrationEnvKeys = %v", plan.Backend.IntegrationEnvKeys)
	}

	input.Contract.ReadOnlyDirs[0] = "/mutated"
	input.Backend.ReadOnlyDirs[0] = "/mutated"
	if !slices.Equal(plan.Contract.ReadOnlyDirs, []string{"/opt/sdk"}) {
		t.Fatalf("Contract ReadOnlyDirs aliases input: %v", plan.Contract.ReadOnlyDirs)
	}
	if !slices.Equal(plan.Backend.ReadOnlyDirs, []string{"/opt/sdk"}) {
		t.Fatalf("Backend ReadOnlyDirs aliases input: %v", plan.Backend.ReadOnlyDirs)
	}
}
