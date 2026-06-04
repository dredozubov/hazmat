package configmodel

import (
	"strings"
	"testing"
)

func TestNewSandboxBackendAuthorityNormalizesAndCopies(t *testing.T) {
	got, err := NewSandboxBackendAuthority(SandboxBackendConfig{
		Type:           " docker-sandboxes ",
		PolicyProfile:  " baseline ",
		DesktopVersion: " 4.50.0 ",
		ComposeVersion: " v2.40.0 ",
		ConfiguredAt:   " 2026-06-04T00:00:00Z ",
	})
	if err != nil {
		t.Fatalf("NewSandboxBackendAuthority: %v", err)
	}

	dto := got.DTO()
	if dto.Type != string(SandboxBackendTypeDockerSandboxes) ||
		dto.PolicyProfile != string(PolicyProfileBaseline) ||
		dto.DesktopVersion != "4.50.0" ||
		dto.ComposeVersion != "v2.40.0" ||
		dto.ConfiguredAt != "2026-06-04T00:00:00Z" {
		t.Fatalf("DTO = %+v", dto)
	}
}

func TestValidateSandboxConfigRejectsInvalidManagedSandbox(t *testing.T) {
	err := ValidateSandboxConfig(SandboxConfig{
		Managed: []ManagedSandboxConfig{{
			Name:          "hazmat-codex-project",
			BackendType:   "colima",
			Agent:         "codex",
			ProjectDir:    "/workspace/project",
			PolicyProfile: "baseline",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported sandbox backend type") {
		t.Fatalf("ValidateSandboxConfig error = %v", err)
	}
}

func TestValidateSandboxConfigRejectsDuplicateManagedSandboxNameAfterNormalization(t *testing.T) {
	err := ValidateSandboxConfig(SandboxConfig{
		Managed: []ManagedSandboxConfig{
			{
				Name:          "hazmat-codex-project",
				BackendType:   "docker-sandboxes",
				Agent:         "codex",
				ProjectDir:    "/workspace/project",
				PolicyProfile: "baseline",
			},
			{
				Name:          " hazmat-codex-project ",
				BackendType:   "docker-sandboxes",
				Agent:         "codex",
				ProjectDir:    "/workspace/project",
				PolicyProfile: "baseline",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate sandbox name") {
		t.Fatalf("ValidateSandboxConfig error = %v", err)
	}
}

func TestNormalizeSandboxConfigReturnsDefensiveDTO(t *testing.T) {
	normalized, err := NormalizeSandboxConfig(SandboxConfig{
		Managed: []ManagedSandboxConfig{{
			Name:          " hazmat-codex-project ",
			BackendType:   " docker-sandboxes ",
			Agent:         " codex ",
			ProjectDir:    " /workspace/project ",
			PolicyProfile: " baseline ",
		}},
	})
	if err != nil {
		t.Fatalf("NormalizeSandboxConfig: %v", err)
	}
	if len(normalized.Managed) != 1 ||
		normalized.Managed[0].Name != "hazmat-codex-project" ||
		normalized.Managed[0].ProjectDir != "/workspace/project" {
		t.Fatalf("normalized = %+v", normalized)
	}

	authority, err := NewSandboxAuthority(normalized)
	if err != nil {
		t.Fatalf("NewSandboxAuthority: %v", err)
	}
	dto := authority.DTO()
	dto.Managed[0].Name = "mutated"
	if fresh := authority.DTO(); fresh.Managed[0].Name != "hazmat-codex-project" {
		t.Fatal("DTO returned storage aliasing authority")
	}
}
