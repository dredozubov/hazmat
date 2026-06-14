package sessionplanner

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"hazmat/hostfacts"
	"hazmat/sessionbackend"
	"hazmat/sessioncontract"
	"hazmat/sessionmeta"
)

var updateGoldenBaselines = flag.Bool("update-golden", false, "update golden baseline files")

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

func TestGoldenSessionPlannerPlanBaselines(t *testing.T) {
	cases := map[string]Input{
		"planner/native.json": goldenPlannerInput(sessionmeta.ModeNative, "staying in native containment because docker: none is configured", []string{"Docker files detected but disabled by config"}, true),
		"planner/docker.json": goldenPlannerInput(sessionmeta.ModeDockerSandbox, "using Docker Sandbox because --docker=sandbox was requested", []string{"Docker Sandbox uses a private daemon; integration env passthrough is not delivered in this backend yet."}, false),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			assertGoldenJSON(t, name, Build(input))
		})
	}
}

func goldenPlannerInput(mode sessionmeta.Mode, routingReason string, sessionNotes []string, requireCodex bool) Input {
	contract := sessioncontract.PlanInput{
		Target:                "shell",
		Mode:                  mode,
		ProjectDir:            "/Users/dr/workspace/project",
		RoutingReason:         routingReason,
		SuggestedIntegrations: []string{"node"},
		RepoSetupSummary:      "remembered (1 read-only path); additional approval required (1 write path)",
		RepoSetupApplied: []sessioncontract.RepoSetupEffect{{
			Class:   "safe",
			Kind:    "read_only",
			Value:   "/opt/homebrew/Cellar/go/1.2.3/libexec",
			Sources: []string{"Suggested by project files (go)"},
		}},
		RepoSetupPending: []sessioncontract.RepoSetupEffect{{
			Class:   "explicit",
			Kind:    "write",
			Value:   "/Users/dr/workspace/project/.cache",
			Sources: []string{"Learned from previous session denial"},
		}},
		ActiveIntegrations:  []string{"go"},
		IntegrationSources:  []string{"go (go.mod)"},
		IntegrationDetails:  []string{"go: resolved GOROOT through Homebrew"},
		IntegrationWarnings: []string{"Go integration warning"},
		IntegrationEnv: map[string]string{
			"GOROOT":  "/opt/homebrew/Cellar/go/1.2.3/libexec",
			"GOPROXY": "https://proxy.golang.org,direct",
		},
		RegistryEnvKeys: []string{"GOPROXY"},
		CredentialEnvGrants: []sessioncontract.CredentialEnvGrant{{
			EnvVar:          "OPENAI_API_KEY",
			CredentialID:    "provider.openai.api-key",
			Source:          "host secret store",
			ConsumerHarness: "codex",
			Redacted:        true,
		}},
		PlannedHostMutations: []sessioncontract.HostMutation{{
			Summary:     "project ACL repair",
			Detail:      "may add bounded collaborative ACLs on /Users/dr/workspace/project",
			Persistence: "persistent in project",
			ProofScope:  "TLA+ model + tests/docs",
		}},
		ReadOnlyDirs:        []string{"/Users/dr/workspace/reference", "/opt/homebrew/Cellar/go/1.2.3/libexec"},
		AutoReadOnlyDirs:    []string{"/opt/homebrew/Cellar/go/1.2.3/libexec"},
		UserReadOnlyDirs:    []string{"/Users/dr/workspace/reference"},
		ReadWriteExtensions: []string{"/Users/dr/workspace/project/.cache"},
		NetworkMode:         sessionmeta.NetworkDefault,
		ServiceAccess:       []string{"docker"},
		GitSSHKey:           "id_ed25519",
		Snapshot:            sessioncontract.Snapshot{Enabled: true, Excludes: []string{".gocache/"}},
		SessionNotes:        sessionNotes,
	}
	backend := sessionbackend.Input{
		Target:             "shell",
		Mode:               mode,
		ProjectDir:         contract.ProjectDir,
		ReadOnlyDirs:       contract.ReadOnlyDirs,
		ReadWriteDirs:      contract.ReadWriteExtensions,
		NetworkMode:        contract.NetworkMode,
		Integrations:       contract.ActiveIntegrations,
		IntegrationEnvKeys: []string{"GOROOT", "GOPROXY"},
		GitSSHConfigured:   true,
		HostFacts:          hostfacts.ForGOOS("darwin"),
	}
	input := Input{Contract: contract, Backend: backend}
	if requireCodex {
		input.HarnessRequirements = []HarnessRequirement{{ID: "codex", Reason: "session target harness"}}
	}
	return input
}

func assertGoldenJSON(t *testing.T, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	assertGolden(t, name, prettyJSON(t, data)+"\n")
}

func prettyJSON(t *testing.T, data []byte) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("unmarshal JSON: %v\n%s", err, string(data))
	}
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal indent JSON: %v", err)
	}
	return string(out)
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", filepath.FromSlash(name))
	if *updateGoldenBaselines {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nRun `go test ./sessionplanner -update-golden` from hazmat/ to refresh baselines.", path, err)
	}
	if got != string(want) {
		t.Fatalf("%s changed; run `go test ./sessionplanner -update-golden` only after reviewing the diff.\n--- want\n%s\n--- got\n%s", name, string(want), got)
	}
}
