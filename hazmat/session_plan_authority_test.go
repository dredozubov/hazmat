package hazmat

import (
	"path/filepath"
	"slices"
	"testing"

	"hazmat/hostfacts"
)

func TestSessionPlanAuthorityNormalizesAndCopiesPlannerInputs(t *testing.T) {
	persistentHome := filepath.Join(t.TempDir(), "agent")
	sessionHomeLaunch, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	sessionHomeRuntime, err := newSessionHomeRuntimePlan(sessionHomeLaunch, persistentHome)
	if err != nil {
		t.Fatalf("newSessionHomeRuntimePlan: %v", err)
	}
	expectedSessionHomePhases := []string{
		"cleanup-stale-session-homes",
		"generate-or-resolve-session-id",
		"assemble-session-home",
		"sync-resume-state",
		"launch-harness",
	}
	expectedBridgeRoots := append([]string(nil), sessionHomeRuntime.AgentHomePolicy.DurableBridgeRoots...)
	expectedBlocker := sessionHomeRuntime.Launch.Blockers[0]
	cfg := sessionConfig{
		Target:                  "codex",
		ProjectDir:              "/workspace/project",
		ReadDirs:                []string{"/opt/sdk"},
		WriteDirs:               []string{"/tmp/cache"},
		UserReadDirs:            []string{"/opt/sdk"},
		AutoReadDirs:            []string{"/opt/runtime"},
		IntegrationExcludes:     []string{"node_modules"},
		SuggestedIntegrations:   []string{" node "},
		ActiveIntegrations:      []string{" go "},
		IntegrationEnv:          map[string]string{" gopath ": "/go", "GOROOT": "/goroot"},
		IntegrationRegistryKeys: []string{" goproxy "},
		CredentialEnvGrants: []sessionCredentialEnvGrant{{
			EnvVar:          " anthropic_api_key ",
			CredentialID:    credentialProviderAnthropicAPIKey,
			Source:          "host secret store",
			ConsumerHarness: HarnessClaude,
		}},
		ServiceAccess:           []string{"api.openai.com"},
		NetworkMode:             sessionNetworkNone,
		EmitSessionMetadataJSON: true,
		RoutingReason:           "test route",
		SessionNotes:            []string{"note"},
		SessionHome:             &sessionHomeRuntime,
		HarnessID:               HarnessCodex,
	}

	authority := newSessionPlanAuthority("codex", cfg, sessionModeNative, true)
	cfg.ReadDirs[0] = "/mutated"
	cfg.IntegrationEnv[" gopath "] = "/mutated"
	cfg.CredentialEnvGrants[0].Source = "mutated"
	cfg.SessionHome.Launch.Phases[0] = "mutated"
	cfg.SessionHome.Launch.Blockers[0].Reason = "mutated"
	cfg.SessionHome.AgentHomePolicy.DurableBridgeRoots[0] = "/mutated"

	contract := authority.ContractInput()
	if contract.ProjectDir != "/workspace/project" || contract.NetworkMode != sessionNetworkNone {
		t.Fatalf("contract identity = %+v", contract)
	}
	if !slices.Equal(contract.ReadOnlyDirs, []string{"/opt/sdk"}) ||
		!slices.Equal(contract.AutoReadOnlyDirs, []string{"/opt/runtime"}) ||
		!slices.Equal(contract.UserReadOnlyDirs, []string{"/opt/sdk"}) ||
		!slices.Equal(contract.ReadWriteExtensions, []string{"/tmp/cache"}) {
		t.Fatalf("contract paths = %+v", contract)
	}
	if !slices.Equal(contract.SuggestedIntegrations, []string{"node"}) ||
		!slices.Equal(contract.ActiveIntegrations, []string{"go"}) {
		t.Fatalf("contract integrations = suggested %v active %v", contract.SuggestedIntegrations, contract.ActiveIntegrations)
	}
	if contract.IntegrationEnv["GOPATH"] != "/go" || contract.IntegrationEnv["GOROOT"] != "/goroot" {
		t.Fatalf("contract IntegrationEnv = %v", contract.IntegrationEnv)
	}
	if !slices.Equal(contract.RegistryEnvKeys, []string{"GOPROXY"}) {
		t.Fatalf("RegistryEnvKeys = %v", contract.RegistryEnvKeys)
	}
	if len(contract.CredentialEnvGrants) != 1 ||
		contract.CredentialEnvGrants[0].EnvVar != "ANTHROPIC_API_KEY" ||
		contract.CredentialEnvGrants[0].CredentialID != string(credentialProviderAnthropicAPIKey) ||
		contract.CredentialEnvGrants[0].ConsumerHarness != string(HarnessClaude) ||
		contract.CredentialEnvGrants[0].Source != "host secret store" {
		t.Fatalf("CredentialEnvGrants = %+v", contract.CredentialEnvGrants)
	}
	if contract.Snapshot.Enabled || !slices.Equal(contract.Snapshot.Excludes, []string{"node_modules"}) {
		t.Fatalf("Snapshot = %+v", contract.Snapshot)
	}
	if contract.SessionHome == nil ||
		contract.SessionHome.Status != "experimental-preview" ||
		contract.SessionHome.ActivationReady ||
		len(contract.SessionHome.ActivationBlockers) == 0 ||
		contract.SessionHome.ActivationBlockers[0].RelPath != expectedBlocker.RelPath ||
		contract.SessionHome.ActivationBlockers[0].Reason != string(expectedBlocker.Reason) ||
		contract.SessionHome.Mode != "session-local" ||
		contract.SessionHome.Home != sessionHomeLaunch.Layout.Home ||
		contract.SessionHome.PersistentHome != persistentHome ||
		!slices.Equal(contract.SessionHome.Phases, expectedSessionHomePhases) ||
		!slices.Equal(contract.SessionHome.DurableBridgeRoots, expectedBridgeRoots) {
		t.Fatalf("SessionHome = %+v", contract.SessionHome)
	}

	contract.IntegrationEnv["GOPATH"] = "/mutated"
	contract.SessionHome.Phases[0] = "mutated"
	contract.SessionHome.ActivationBlockers[0].Reason = "mutated"
	contract.SessionHome.DurableBridgeRoots[0] = "/mutated"
	if fresh := authority.ContractInput(); fresh.IntegrationEnv["GOPATH"] != "/go" ||
		fresh.SessionHome.Phases[0] != "cleanup-stale-session-homes" ||
		fresh.SessionHome.ActivationBlockers[0].Reason != string(expectedBlocker.Reason) ||
		fresh.SessionHome.DurableBridgeRoots[0] != expectedBridgeRoots[0] {
		t.Fatal("ContractInput returned storage aliasing authority")
	}

	backend := authority.BackendInput(hostfacts.ForGOOS("darwin"))
	if !slices.Equal(backend.IntegrationEnvKeys, []string{"GOPATH", "GOROOT"}) {
		t.Fatalf("backend IntegrationEnvKeys = %v", backend.IntegrationEnvKeys)
	}
	if !slices.Equal(backend.Integrations, []string{"go"}) {
		t.Fatalf("backend Integrations = %v", backend.Integrations)
	}

	requirements := authority.HarnessRequirements()
	if len(requirements) != 1 || requirements[0].ID != string(HarnessCodex) {
		t.Fatalf("HarnessRequirements = %+v", requirements)
	}
}
