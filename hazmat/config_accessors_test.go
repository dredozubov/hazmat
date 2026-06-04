package hazmat

import "testing"

func TestHazmatConfigAccessorsAreZeroValueSafe(t *testing.T) {
	var cfg HazmatConfig

	if got := cfg.PinnedIntegrations(); got != nil {
		t.Fatalf("zero PinnedIntegrations = %v, want nil", got)
	}
	if got := cfg.RejectedIntegrations(); got != nil {
		t.Fatalf("zero RejectedIntegrations = %v, want nil", got)
	}
	if got := cfg.SessionReadDirs(); got != nil {
		t.Fatalf("zero SessionReadDirs = %v, want nil", got)
	}
	if got := cfg.SandboxBackend(); got != nil {
		t.Fatalf("zero SandboxBackend = %+v, want nil", got)
	}
	if got := cfg.ManagedSandboxes(); got != nil {
		t.Fatalf("zero ManagedSandboxes = %v, want nil", got)
	}
}

func TestHazmatConfigAccessorsReturnDefensiveCopies(t *testing.T) {
	readDirs := []string{"/workspace/shared"}
	cfg := HazmatConfig{
		Session: SessionConfig{ReadDirs: &readDirs},
		Integrations: IntegrationsConfig{
			Pinned: []IntegrationPin{{
				ProjectDir:   "/workspace/project",
				Integrations: []string{"go"},
			}},
			Rejected: []IntegrationRejection{{
				ProjectDir:   "/workspace/project",
				Integrations: []string{"node"},
			}},
		},
		Sandbox: SandboxConfig{
			Backend: &SandboxBackendConfig{
				Type:          sandboxBackendDockerSandboxes,
				PolicyProfile: sandboxPolicyProfileBaseline,
			},
			Managed: []ManagedSandboxConfig{{
				Name:          "hazmat-codex-project",
				BackendType:   sandboxBackendDockerSandboxes,
				Agent:         "codex",
				ProjectDir:    "/workspace/project",
				PolicyProfile: sandboxPolicyProfileBaseline,
			}},
		},
	}

	pins := cfg.PinnedIntegrations()
	pins[0].ProjectDir = "/mutated"
	pins[0].Integrations[0] = "mutated"
	if fresh := cfg.PinnedIntegrations(); fresh[0].ProjectDir != "/workspace/project" || fresh[0].Integrations[0] != "go" {
		t.Fatalf("PinnedIntegrations aliases config: %+v", fresh)
	}

	rejections := cfg.RejectedIntegrations()
	rejections[0].ProjectDir = "/mutated"
	rejections[0].Integrations[0] = "mutated"
	if fresh := cfg.RejectedIntegrations(); fresh[0].ProjectDir != "/workspace/project" || fresh[0].Integrations[0] != "node" {
		t.Fatalf("RejectedIntegrations aliases config: %+v", fresh)
	}

	dirs := cfg.SessionReadDirs()
	dirs[0] = "/mutated"
	if fresh := cfg.SessionReadDirs(); fresh[0] != "/workspace/shared" {
		t.Fatalf("SessionReadDirs aliases config: %v", fresh)
	}

	backend := cfg.SandboxBackend()
	backend.Type = "mutated"
	backend.PolicyProfile = "mutated"
	if fresh := cfg.SandboxBackend(); fresh.Type != sandboxBackendDockerSandboxes || fresh.PolicyProfile != sandboxPolicyProfileBaseline {
		t.Fatalf("SandboxBackend aliases config: %+v", fresh)
	}

	managed := cfg.ManagedSandboxes()
	managed[0].Name = "mutated"
	managed[0].ProjectDir = "/mutated"
	if fresh := cfg.ManagedSandboxes(); fresh[0].Name != "hazmat-codex-project" || fresh[0].ProjectDir != "/workspace/project" {
		t.Fatalf("ManagedSandboxes aliases config: %+v", fresh)
	}
}
