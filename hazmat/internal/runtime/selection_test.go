package runtime

import (
	"testing"

	"hazmat/internal/runtime/darwin"
	"hazmat/internal/runtime/docker"
	"hazmat/internal/runtime/linux"
	"hazmat/sessionbackend"
)

func TestSelectRoutesConcreteRuntimes(t *testing.T) {
	tests := []struct {
		name          string
		backend       sessionbackend.Kind
		packagePath   string
		native        bool
		dockerSandbox bool
		planOnly      bool
	}{
		{
			name:        "darwin native",
			backend:     sessionbackend.KindDarwinNative,
			packagePath: darwin.PackagePath,
			native:      true,
		},
		{
			name:          "docker sandbox",
			backend:       sessionbackend.KindDockerSandbox,
			packagePath:   docker.PackagePath,
			dockerSandbox: true,
		},
		{
			name:        "linux native plan only",
			backend:     sessionbackend.KindLinuxNative,
			packagePath: linux.PackagePath,
			native:      true,
			planOnly:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selection, err := Select(sessionbackend.Plan{Backend: tt.backend})
			if err != nil {
				t.Fatalf("Select(): %v", err)
			}
			if selection.Backend != tt.backend ||
				selection.PackagePath != tt.packagePath ||
				selection.Native != tt.native ||
				selection.DockerSandbox != tt.dockerSandbox ||
				selection.PlanOnly != tt.planOnly {
				t.Fatalf("selection = %+v", selection)
			}
		})
	}
}

func TestSelectKeepsRemoteAndUnsupportedPlanOnly(t *testing.T) {
	for _, backend := range []sessionbackend.Kind{
		sessionbackend.KindRemoteEnvelope,
		sessionbackend.KindUnsupportedNative,
		sessionbackend.KindAppleContainer,
	} {
		selection, err := Select(sessionbackend.Plan{Backend: backend})
		if err != nil {
			t.Fatalf("Select(%q): %v", backend, err)
		}
		if !selection.PlanOnly {
			t.Fatalf("Select(%q).PlanOnly = false, want true", backend)
		}
	}
}

func TestSelectRejectsUnknownBackend(t *testing.T) {
	if _, err := Select(sessionbackend.Plan{Backend: "new-runtime"}); err == nil {
		t.Fatal("Select() succeeded for unknown backend")
	}
}
