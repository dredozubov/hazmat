package sessionbackend

import (
	"slices"
	"testing"

	"hazmat/hostfacts"
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
		HostFacts:          hostfacts.ForGOOS("darwin"),
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
		Mode:      sessionmeta.ModeNative,
		HostFacts: hostfacts.ForGOOS("linux"),
	})
	if plan.Backend != KindLinuxNative {
		t.Fatalf("Backend = %q", plan.Backend)
	}
	if len(plan.CapabilityGaps) != 1 || plan.CapabilityGaps[0].Feature != GapNativeLaunch {
		t.Fatalf("CapabilityGaps = %v", plan.CapabilityGaps)
	}
}

func TestBuildPlanRequiresExplicitGOOSForNative(t *testing.T) {
	plan := BuildPlan(Input{Mode: sessionmeta.ModeNative})
	if plan.Backend != KindUnsupportedNative {
		t.Fatalf("Backend = %q, want %q", plan.Backend, KindUnsupportedNative)
	}
	if len(plan.CapabilityGaps) != 1 || plan.CapabilityGaps[0].Feature != GapNativeLaunch {
		t.Fatalf("CapabilityGaps = %v", plan.CapabilityGaps)
	}
}

func TestBuildPlanReportsDockerIntegrationEnvGap(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:               sessionmeta.ModeDockerSandbox,
		IntegrationEnvKeys: []string{"NPM_CONFIG_REGISTRY", "GOPROXY"},
		HostFacts:          hostfacts.ForGOOS("darwin"),
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

func TestBuildPlanReportsDockerGitSSHTransportGap(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:             sessionmeta.ModeDockerSandbox,
		GitSSHConfigured: true,
		HostFacts:        hostfacts.ForGOOS("darwin"),
	})
	if plan.Backend != KindDockerSandbox {
		t.Fatalf("Backend = %q", plan.Backend)
	}
	if !plan.GitSSHConfigured {
		t.Fatal("GitSSHConfigured = false, want true")
	}
	if len(plan.CapabilityGaps) != 1 || plan.CapabilityGaps[0].Feature != GapGitSSHTransport {
		t.Fatalf("CapabilityGaps = %v", plan.CapabilityGaps)
	}
}

func TestRemoteEnvelopeBackendReportsPlanOnlyGap(t *testing.T) {
	gaps := capabilityGaps(Input{}, KindRemoteEnvelope)
	if len(gaps) != 1 || gaps[0].Feature != GapRemoteLaunch {
		t.Fatalf("capabilityGaps(remote-envelope) = %v", gaps)
	}
}

func TestBuildPlanForAppleContainerReportsPlanOnlyGapAndCleanupArtifact(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:      sessionmeta.ModeAppleContainer,
		HostFacts: hostfacts.ForGOOS("darwin"),
	})
	if plan.Backend != KindAppleContainer {
		t.Fatalf("Backend = %q, want %q", plan.Backend, KindAppleContainer)
	}
	if len(plan.CapabilityGaps) != 1 || plan.CapabilityGaps[0].Feature != GapAppleContainerLaunch {
		t.Fatalf("CapabilityGaps = %v", plan.CapabilityGaps)
	}
	if len(plan.LifecycleArtifacts) != 1 ||
		plan.LifecycleArtifacts[0].Kind != ArtifactAppleContainer ||
		!plan.LifecycleArtifacts[0].CleanupRequired {
		t.Fatalf("LifecycleArtifacts = %v, want cleanup-required apple-container artifact", plan.LifecycleArtifacts)
	}
}

func TestBuildPlanReportsAppleContainerIntegrationEnvGap(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:               sessionmeta.ModeAppleContainer,
		IntegrationEnvKeys: []string{"GOPROXY"},
		HostFacts:          hostfacts.ForGOOS("darwin"),
	})
	var features []string
	for _, gap := range plan.CapabilityGaps {
		features = append(features, gap.Feature)
	}
	if !slices.Contains(features, GapIntegrationEnv) || !slices.Contains(features, GapAppleContainerLaunch) {
		t.Fatalf("CapabilityGaps = %v, want integration env and plan-only gaps", plan.CapabilityGaps)
	}
}

func TestAppleContainerPreparedArtifactMatchesBackend(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:      sessionmeta.ModeAppleContainer,
		HostFacts: hostfacts.ForGOOS("darwin"),
	})
	artifact := NewAppleContainerArtifact(AppleContainerLaunchSpec{
		FormatVersion: 1,
		Backend:       string(KindAppleContainer),
		Phase:         "plan-only",
		ContainerName: "hazmat-codex-project-abc",
		Image:         "ghcr.io/example/hazmat-codex:sha256-abc",
	})
	prepared, err := NewPreparedLaunch(plan, artifact, []AcceptedGap{{
		Feature:       GapAppleContainerLaunch,
		Justification: "test fixture",
	}})
	if err != nil {
		t.Fatalf("NewPreparedLaunch: %v", err)
	}
	spec, ok := prepared.AppleContainer()
	if !ok || spec == nil || spec.ContainerName != "hazmat-codex-project-abc" {
		t.Fatalf("AppleContainer() = %+v, %v", spec, ok)
	}

	dockerPlan := BuildPlan(Input{
		Mode:      sessionmeta.ModeDockerSandbox,
		HostFacts: hostfacts.ForGOOS("darwin"),
	})
	if _, err := NewPreparedLaunch(dockerPlan, artifact, nil); err == nil {
		t.Fatal("NewPreparedLaunch accepted an apple-container artifact for a docker backend")
	}
}
