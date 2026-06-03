package sessionbackend

import (
	"strings"
	"testing"

	"hazmat/hostfacts"
	"hazmat/sessionmeta"
)

func TestNewPreparedLaunchAcceptsSingleMatchingArtifact(t *testing.T) {
	plan := BuildPlan(Input{
		Target:     "codex",
		Mode:       sessionmeta.ModeNative,
		ProjectDir: "/workspace/project",
		HostFacts:  hostfacts.ForGOOS("darwin"),
	})
	artifact := &DarwinSeatbelt{PolicyPath: "/private/tmp/hazmat.sb"}

	prepared, err := NewPreparedLaunch(plan, ArtifactVariant{DarwinSeatbelt: artifact}, nil)
	if err != nil {
		t.Fatalf("NewPreparedLaunch: %v", err)
	}
	if prepared.ArtifactKind != PreparedArtifactDarwinSeatbelt || prepared.DarwinSeatbelt.PolicyPath != artifact.PolicyPath {
		t.Fatalf("PreparedLaunch = %+v", prepared)
	}

	artifact.PolicyPath = "/mutated"
	plan.ProjectDir = "/mutated"
	if prepared.DarwinSeatbelt.PolicyPath != "/private/tmp/hazmat.sb" || prepared.Plan.ProjectDir != "/workspace/project" {
		t.Fatalf("PreparedLaunch aliases caller input: %+v", prepared)
	}
}

func TestNewPreparedLaunchRejectsMissingOrMultipleArtifacts(t *testing.T) {
	plan := BuildPlan(Input{Mode: sessionmeta.ModeNative, HostFacts: hostfacts.ForGOOS("darwin")})

	_, err := NewPreparedLaunch(plan, ArtifactVariant{}, nil)
	if err == nil || !strings.Contains(err.Error(), "exactly one artifact") {
		t.Fatalf("missing artifact error = %v", err)
	}

	_, err = NewPreparedLaunch(plan, ArtifactVariant{
		DarwinSeatbelt: &DarwinSeatbelt{PolicyPath: "/tmp/policy.sb"},
		DockerSandbox:  &DockerSandboxSpec{Name: "hazmat"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "exactly one artifact") {
		t.Fatalf("multiple artifact error = %v", err)
	}
}

func TestNewPreparedLaunchRejectsBackendMismatch(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:      sessionmeta.ModeDockerSandbox,
		HostFacts: hostfacts.ForGOOS("darwin"),
	})

	_, err := NewPreparedLaunch(plan, ArtifactVariant{
		DarwinSeatbelt: &DarwinSeatbelt{PolicyPath: "/tmp/policy.sb"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), `does not match backend "docker-sandbox"`) {
		t.Fatalf("backend mismatch error = %v", err)
	}
}

func TestNewPreparedLaunchRequiresAcceptedCapabilityGaps(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:      sessionmeta.ModeNative,
		HostFacts: hostfacts.ForGOOS("linux"),
	})

	_, err := NewPreparedLaunch(plan, ArtifactVariant{
		LinuxLaunch: &LinuxLaunchSpec{FormatVersion: 1, Backend: string(KindLinuxNative), Phase: "plan-only"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), `capability gap "native-launch" must be accepted`) {
		t.Fatalf("missing accepted gap error = %v", err)
	}

	prepared, err := NewPreparedLaunch(plan, ArtifactVariant{
		LinuxLaunch: &LinuxLaunchSpec{FormatVersion: 1, Backend: string(KindLinuxNative), Phase: "plan-only"},
	}, []AcceptedGap{{
		Feature:       GapNativeLaunch,
		Justification: "plan-only Linux launch artifact",
	}})
	if err != nil {
		t.Fatalf("NewPreparedLaunch with accepted gap: %v", err)
	}
	if len(prepared.AcceptedGaps) != 1 || prepared.AcceptedGaps[0].Feature != GapNativeLaunch {
		t.Fatalf("AcceptedGaps = %+v", prepared.AcceptedGaps)
	}
}

func TestNewPreparedLaunchRejectsExtraAcceptedCapabilityGap(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:      sessionmeta.ModeDockerSandbox,
		HostFacts: hostfacts.ForGOOS("darwin"),
	})

	_, err := NewPreparedLaunch(plan, ArtifactVariant{
		DockerSandbox: &DockerSandboxSpec{Name: "hazmat", Agent: "claude", ProjectDir: "/workspace/project", PolicyProfile: "baseline"},
	}, []AcceptedGap{{Feature: GapNativeLaunch}})
	if err == nil || !strings.Contains(err.Error(), "accepted capability gaps require matching plan gaps") {
		t.Fatalf("extra accepted gap error = %v", err)
	}
}

func TestNewPreparedLaunchAllowsRemoteEnvelopePlaceholder(t *testing.T) {
	plan := Plan{
		Mode:    sessionmeta.ModeNative,
		Backend: KindRemoteEnvelope,
	}

	prepared, err := NewPreparedLaunch(plan, ArtifactVariant{
		RemoteEnvelope: &RemoteEnvelope{SchemaVersion: 1, Digest: "sha256:test"},
	}, nil)
	if err != nil {
		t.Fatalf("NewPreparedLaunch remote envelope: %v", err)
	}
	if prepared.ArtifactKind != PreparedArtifactRemoteEnvelope || prepared.RemoteEnvelope.SchemaVersion != 1 {
		t.Fatalf("PreparedLaunch = %+v", prepared)
	}
}
