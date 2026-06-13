package sessioncontract

import (
	"reflect"
	"testing"

	"hazmat/sessionmeta"
)

func TestRequestNormalizedAndLaunchMetadataInput(t *testing.T) {
	request := Request{
		Target:              "codex",
		ProjectDir:          "/workspace/project",
		ReadOnlyDirs:        []string{"/opt/sdk"},
		ReadWriteExtensions: []string{"/tmp/cache"},
		Integrations:        []string{"go"},
	}

	normalized := request.Normalized()
	if normalized.NetworkMode != sessionmeta.NetworkDefault {
		t.Fatalf("NetworkMode = %q, want default", normalized.NetworkMode)
	}
	normalized.ReadOnlyDirs[0] = "/mutated"
	if request.ReadOnlyDirs[0] != "/opt/sdk" {
		t.Fatalf("Normalized mutated original ReadOnlyDirs: %v", request.ReadOnlyDirs)
	}

	input := request.LaunchMetadataInput(sessionmeta.ModeNative)
	if input.Target != "codex" || input.ProjectDir != "/workspace/project" || input.NetworkMode != sessionmeta.NetworkDefault {
		t.Fatalf("LaunchMetadataInput = %+v", input)
	}
}

func TestBuildPlanCopiesAndSortsStableFields(t *testing.T) {
	input := PlanInput{
		Target:                "shell",
		Mode:                  sessionmeta.ModeNative,
		ProjectDir:            "/workspace/project",
		RoutingReason:         "using native containment",
		SuggestedIntegrations: []string{"node"},
		RepoSetupSummary:      "remembered (1 read-only path)",
		RepoSetupApplied: []RepoSetupEffect{
			{Class: "safe", Kind: "read_only", Value: "/opt/sdk", Sources: []string{"go"}},
		},
		ActiveIntegrations:  []string{"go"},
		IntegrationSources:  []string{"go (go env GOROOT)"},
		IntegrationDetails:  []string{"go: resolved GOROOT"},
		IntegrationWarnings: []string{"warning"},
		IntegrationEnv: map[string]string{
			"Z_VAR": "z",
			"A_VAR": "a",
		},
		RegistryEnvKeys: []string{"NPM_CONFIG_REGISTRY"},
		CredentialEnvGrants: []CredentialEnvGrant{
			{EnvVar: "OPENAI_API_KEY", CredentialID: "provider.openai-api-key", Source: "host secret store", ConsumerHarness: "codex", Redacted: true},
		},
		PlannedHostMutations: []HostMutation{
			{Summary: "project ACL repair", Detail: "repair detail", Persistence: "persistent in project", ProofScope: "tests/docs"},
		},
		ReadOnlyDirs:        []string{"/opt/sdk"},
		AutoReadOnlyDirs:    []string{"/opt/auto"},
		UserReadOnlyDirs:    []string{"/opt/user"},
		ReadWriteExtensions: []string{"/tmp/cache"},
		NetworkMode:         sessionmeta.NetworkNone,
		ServiceAccess:       []string{"git+ssh"},
		GitSSHKey:           "id_ed25519",
		Snapshot:            Snapshot{Enabled: true, Excludes: []string{".venv/"}},
		SessionHome: &SessionHome{
			Enabled:         true,
			Status:          "experimental-preview",
			ActivationReady: false,
			ActivationBlockers: []SessionHomeBlocker{{
				RelPath:       ".zshrc",
				Reason:        "seed-materialization",
				Class:         "shell-config",
				RuntimePolicy: "seed-only",
			}},
			Mode:               "session-local",
			Home:               "/private/tmp/hazmat-home/session-123/home",
			PersistentHome:     "/Users/agent",
			CleanupRoot:        "/private/tmp/hazmat-home",
			CleanupMaxAge:      "24h0m0s",
			Phases:             []string{"cleanup-stale-session-homes", "assemble-session-home"},
			ResumeRequested:    true,
			DurableBridgeRoots: []string{"/Users/agent/.claude/projects"},
		},
		SessionNotes: []string{"network none"},
	}

	plan := BuildPlan(input)
	if plan.FormatVersion != PlanFormatVersion {
		t.Fatalf("FormatVersion = %d", plan.FormatVersion)
	}
	if plan.Mode != string(sessionmeta.ModeNative) || plan.ModeLabel != sessionmeta.ModeNative.Label() {
		t.Fatalf("mode fields = %q/%q", plan.Mode, plan.ModeLabel)
	}
	if got, want := plan.IntegrationEnvKeys, []string{"A_VAR", "Z_VAR"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("IntegrationEnvKeys = %v, want %v", got, want)
	}
	if !plan.NetworkPolicy.DenyAllEgress || plan.NetworkPolicy.Enforcement != "native-seatbelt" {
		t.Fatalf("NetworkPolicy = %+v, want native deny-all", plan.NetworkPolicy)
	}
	if got := plan.CredentialEnvGrants[0]; got.EnvVar != "OPENAI_API_KEY" || got.ConsumerHarness != "codex" || !got.Redacted {
		t.Fatalf("CredentialEnvGrants[0] = %+v", got)
	}
	input.RepoSetupApplied[0].Sources[0] = "mutated"
	input.Snapshot.Excludes[0] = "mutated"
	input.SessionHome.Phases[0] = "mutated"
	input.SessionHome.ActivationBlockers[0].Reason = "mutated"
	input.SessionHome.ActivationBlockers[0].Class = "mutated"
	input.SessionHome.ActivationBlockers[0].RuntimePolicy = "mutated"
	input.SessionHome.DurableBridgeRoots[0] = "/mutated"
	if plan.RepoSetupApplied[0].Sources[0] != "go" ||
		plan.Snapshot.Excludes[0] != ".venv/" ||
		plan.SessionHome.Phases[0] != "cleanup-stale-session-homes" ||
		plan.SessionHome.ActivationBlockers[0].Reason != "seed-materialization" ||
		plan.SessionHome.ActivationBlockers[0].Class != "shell-config" ||
		plan.SessionHome.ActivationBlockers[0].RuntimePolicy != "seed-only" ||
		plan.SessionHome.DurableBridgeRoots[0] != "/Users/agent/.claude/projects" {
		t.Fatalf("BuildPlan did not defensively copy nested slices: %+v", plan)
	}
}

func TestBuildPlanDockerNetworkMetadata(t *testing.T) {
	plan := BuildPlan(PlanInput{
		Mode:        sessionmeta.ModeDockerSandbox,
		ProjectDir:  "/workspace/project",
		NetworkMode: sessionmeta.NetworkNone,
	})
	if plan.NetworkPolicy.Effective != "sandbox-profile" || !plan.NetworkPolicy.CleanupRequired {
		t.Fatalf("NetworkPolicy = %+v, want Docker Sandbox profile metadata", plan.NetworkPolicy)
	}
	if plan.NetworkPolicy.DenyAllEgress {
		t.Fatalf("Docker network policy should delegate to sandbox profile: %+v", plan.NetworkPolicy)
	}
}
