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
			IntegrationWarnings: []string{"Go integration warning"},
			CredentialEnvGrants: []sessioncontract.CredentialEnvGrant{
				{EnvVar: "OPENAI_API_KEY", CredentialID: "provider.openai-api-key", Source: "host secret store", ConsumerHarness: "codex", Redacted: true},
			},
			PlannedHostMutations: []sessioncontract.HostMutation{
				{Summary: "project ACL repair", Detail: "repair /workspace/project", Persistence: "persistent in project", ProofScope: "tla:MC_SessionPermissionRepairs"},
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
		HarnessRequirements: []HarnessRequirement{
			{ID: "codex", Reason: "target harness", Notes: []string{"sync managed assets"}},
		},
		Warnings: []Warning{
			{Source: "frontend", Message: "explicit warning"},
		},
	}

	plan := Build(input)
	if plan.FormatVersion != PlanFormatVersion {
		t.Fatalf("FormatVersion = %d, want %d", plan.FormatVersion, PlanFormatVersion)
	}
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
	if got := plan.HostMutations; len(got) != 1 || got[0].Summary != "project ACL repair" {
		t.Fatalf("HostMutations = %+v", got)
	}
	if got := plan.CredentialEnvGrants; len(got) != 1 || got[0].EnvVar != "OPENAI_API_KEY" || !got[0].Redacted {
		t.Fatalf("CredentialEnvGrants = %+v", got)
	}
	if got := plan.HarnessRequirements; len(got) != 1 || got[0].ID != "codex" || !slices.Equal(got[0].Notes, []string{"sync managed assets"}) {
		t.Fatalf("HarnessRequirements = %+v", got)
	}
	if got := plan.Warnings; len(got) != 2 || got[0].Source != "integration" || got[1].Source != "frontend" {
		t.Fatalf("Warnings = %+v", got)
	}

	input.Contract.ReadOnlyDirs[0] = "/mutated"
	input.Backend.ReadOnlyDirs[0] = "/mutated"
	input.HarnessRequirements[0].Notes[0] = "mutated"
	if !slices.Equal(plan.Contract.ReadOnlyDirs, []string{"/opt/sdk"}) {
		t.Fatalf("Contract ReadOnlyDirs aliases input: %v", plan.Contract.ReadOnlyDirs)
	}
	if !slices.Equal(plan.Backend.ReadOnlyDirs, []string{"/opt/sdk"}) {
		t.Fatalf("Backend ReadOnlyDirs aliases input: %v", plan.Backend.ReadOnlyDirs)
	}
	if !slices.Equal(plan.HarnessRequirements[0].Notes, []string{"sync managed assets"}) {
		t.Fatalf("HarnessRequirements aliases input: %+v", plan.HarnessRequirements)
	}
}
