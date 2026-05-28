package sessionbackend

import (
	"slices"
	"testing"

	"hazmat/sessionmeta"
)

func TestBuildPlanForDarwinNativeCopiesInputs(t *testing.T) {
	input := Input{
		Target:             "codex",
		Mode:               sessionmeta.ModeNative,
		ProjectDir:         "/workspace/project",
		ReadOnlyDirs:       []string{"/opt/sdk"},
		ReadWriteDirs:      []string{"/tmp/cache"},
		NetworkMode:        sessionmeta.NetworkNone,
		Integrations:       []string{"go"},
		IntegrationEnvKeys: []string{"GOROOT", "GOPATH"},
		GOOS:               "darwin",
	}

	plan := BuildPlan(input)
	if plan.Backend != KindDarwinNative || plan.Mode != sessionmeta.ModeNative {
		t.Fatalf("backend/mode = %q/%q", plan.Backend, plan.Mode)
	}
	if plan.NetworkMode != sessionmeta.NetworkNone {
		t.Fatalf("NetworkMode = %q", plan.NetworkMode)
	}
	if !slices.Equal(plan.ReadOnlyDirs, []string{"/opt/sdk"}) ||
		!slices.Equal(plan.ReadWriteDirs, []string{"/tmp/cache"}) ||
		!slices.Equal(plan.Integrations, []string{"go"}) {
		t.Fatalf("plan paths/integrations = %+v", plan)
	}
	if len(plan.CapabilityGaps) != 0 {
		t.Fatalf("CapabilityGaps = %v", plan.CapabilityGaps)
	}
	if len(plan.LifecycleArtifacts) != 1 || plan.LifecycleArtifacts[0].Kind != ArtifactSeatbeltPolicy {
		t.Fatalf("LifecycleArtifacts = %v", plan.LifecycleArtifacts)
	}

	input.ReadOnlyDirs[0] = "/mutated"
	if plan.ReadOnlyDirs[0] != "/opt/sdk" {
		t.Fatal("BuildPlan returned storage aliasing input")
	}
}

func TestBuildPlanReportsLinuxNativeGap(t *testing.T) {
	plan := BuildPlan(Input{
		Mode: sessionmeta.ModeNative,
		GOOS: "linux",
	})
	if plan.Backend != KindLinuxNative {
		t.Fatalf("Backend = %q", plan.Backend)
	}
	if len(plan.CapabilityGaps) != 1 || plan.CapabilityGaps[0].Feature != GapNativeLaunch {
		t.Fatalf("CapabilityGaps = %v", plan.CapabilityGaps)
	}
}

func TestBuildPlanReportsDockerIntegrationEnvGap(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:               sessionmeta.ModeDockerSandbox,
		IntegrationEnvKeys: []string{"NPM_CONFIG_REGISTRY", "GOPROXY"},
		GOOS:               "darwin",
	})
	if plan.Backend != KindDockerSandbox {
		t.Fatalf("Backend = %q", plan.Backend)
	}
	if !slices.Equal(plan.IntegrationEnvKeys, []string{"GOPROXY", "NPM_CONFIG_REGISTRY"}) {
		t.Fatalf("IntegrationEnvKeys = %v", plan.IntegrationEnvKeys)
	}
	if len(plan.CapabilityGaps) != 1 || plan.CapabilityGaps[0].Feature != GapIntegrationEnv {
		t.Fatalf("CapabilityGaps = %v", plan.CapabilityGaps)
	}
	if len(plan.LifecycleArtifacts) != 1 || plan.LifecycleArtifacts[0].Kind != ArtifactDockerSandbox {
		t.Fatalf("LifecycleArtifacts = %v", plan.LifecycleArtifacts)
	}
}
