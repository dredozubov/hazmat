package sessionbackend

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"hazmat/hostfacts"
	"hazmat/sessionmeta"
)

func TestPreparedLaunchHasNoExportedAuthorityFields(t *testing.T) {
	typ := reflect.TypeOf(PreparedLaunch{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.IsExported() {
			t.Fatalf("PreparedLaunch field %s is exported", field.Name)
		}
	}
}

func TestNewPreparedLaunchAcceptsSingleMatchingArtifact(t *testing.T) {
	plan := BuildPlan(Input{
		Target:     "codex",
		Mode:       sessionmeta.ModeNative,
		ProjectDir: "/workspace/project",
		HostFacts:  hostfacts.ForGOOS("darwin"),
	})
	artifact := DarwinSeatbelt{PolicyPath: "/private/tmp/hazmat.sb"}

	prepared, err := NewPreparedLaunch(plan, NewDarwinSeatbeltArtifact(artifact), nil)
	if err != nil {
		t.Fatalf("NewPreparedLaunch: %v", err)
	}
	darwinSeatbelt, ok := prepared.DarwinSeatbelt()
	if !ok || prepared.ArtifactKind() != PreparedArtifactDarwinSeatbelt || darwinSeatbelt.PolicyPath != artifact.PolicyPath {
		t.Fatalf("PreparedLaunch = %+v", prepared)
	}

	artifact.PolicyPath = "/mutated"
	plan.ProjectDir = "/mutated"
	darwinSeatbelt, _ = prepared.DarwinSeatbelt()
	if darwinSeatbelt.PolicyPath != "/private/tmp/hazmat.sb" || prepared.Plan().ProjectDir != "/workspace/project" {
		t.Fatalf("PreparedLaunch aliases caller input: %+v", prepared)
	}
}

func TestNewPreparedLaunchRequiresArtifact(t *testing.T) {
	plan := BuildPlan(Input{Mode: sessionmeta.ModeNative, HostFacts: hostfacts.ForGOOS("darwin")})

	_, err := NewPreparedLaunch(plan, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "artifact is required") {
		t.Fatalf("missing artifact error = %v", err)
	}
}

func TestPreparedArtifactIsSealed(t *testing.T) {
	typ := reflect.TypeOf((*PreparedArtifact)(nil)).Elem()
	method, ok := typ.MethodByName("preparedArtifact")
	if !ok {
		t.Fatalf("PreparedArtifact is missing sealing method")
	}
	if method.PkgPath == "" {
		t.Fatalf("PreparedArtifact sealing method is exported")
	}
}

func TestNewPreparedLaunchRejectsBackendMismatch(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:      sessionmeta.ModeDockerSandbox,
		HostFacts: hostfacts.ForGOOS("darwin"),
	})

	_, err := NewPreparedLaunch(plan, NewDarwinSeatbeltArtifact(DarwinSeatbelt{PolicyPath: "/tmp/policy.sb"}), nil)
	if err == nil || !strings.Contains(err.Error(), `does not match backend "docker-sandbox"`) {
		t.Fatalf("backend mismatch error = %v", err)
	}
}

func TestNewPreparedLaunchRequiresAcceptedCapabilityGaps(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:      sessionmeta.ModeNative,
		HostFacts: hostfacts.ForGOOS("linux"),
	})

	_, err := NewPreparedLaunch(plan, NewLinuxLaunchArtifact(LinuxLaunchSpec{FormatVersion: 1, Backend: string(KindLinuxNative), Phase: "plan-only"}), nil)
	if err == nil || !strings.Contains(err.Error(), `capability gap "native-launch" must be accepted`) {
		t.Fatalf("missing accepted gap error = %v", err)
	}

	prepared, err := NewPreparedLaunch(plan, NewLinuxLaunchArtifact(LinuxLaunchSpec{FormatVersion: 1, Backend: string(KindLinuxNative), Phase: "plan-only"}), []AcceptedGap{{
		Feature:       GapNativeLaunch,
		Justification: "plan-only Linux launch artifact",
	}})
	if err != nil {
		t.Fatalf("NewPreparedLaunch with accepted gap: %v", err)
	}
	if got := prepared.AcceptedGaps(); len(got) != 1 || got[0].Feature != GapNativeLaunch {
		t.Fatalf("AcceptedGaps = %+v", got)
	}
}

func TestNewPreparedLaunchRejectsExtraAcceptedCapabilityGap(t *testing.T) {
	plan := BuildPlan(Input{
		Mode:      sessionmeta.ModeDockerSandbox,
		HostFacts: hostfacts.ForGOOS("darwin"),
	})

	_, err := NewPreparedLaunch(plan, NewDockerSandboxArtifact(DockerSandboxSpec{Name: "hazmat", Agent: "claude", ProjectDir: "/workspace/project", PolicyProfile: "baseline"}), []AcceptedGap{{Feature: GapNativeLaunch}})
	if err == nil || !strings.Contains(err.Error(), "accepted capability gaps require matching plan gaps") {
		t.Fatalf("extra accepted gap error = %v", err)
	}
}

func TestNewPreparedLaunchAllowsRemoteEnvelopePlaceholder(t *testing.T) {
	plan := Plan{
		Mode:    sessionmeta.ModeNative,
		Backend: KindRemoteEnvelope,
	}

	prepared, err := NewPreparedLaunch(plan, NewRemoteEnvelopeArtifact(RemoteEnvelope{SchemaVersion: 1, Digest: "sha256:test"}), nil)
	if err != nil {
		t.Fatalf("NewPreparedLaunch remote envelope: %v", err)
	}
	remoteEnvelope, ok := prepared.RemoteEnvelope()
	if !ok || prepared.ArtifactKind() != PreparedArtifactRemoteEnvelope || remoteEnvelope.SchemaVersion != 1 {
		t.Fatalf("PreparedLaunch = %+v", prepared)
	}
}

func TestPreparedLaunchRequiresExplicitDTOForJSON(t *testing.T) {
	plan := BuildPlan(Input{
		Target:       "codex",
		Mode:         sessionmeta.ModeNative,
		ProjectDir:   "/workspace/project",
		ReadOnlyDirs: []string{"/workspace/reference"},
		HostFacts:    hostfacts.ForGOOS("darwin"),
	})
	prepared, err := NewPreparedLaunch(plan, NewDarwinSeatbeltArtifact(DarwinSeatbelt{
		PolicyPath: "/private/tmp/hazmat.sb",
		Policy:     `(allow file-read* (subpath "/workspace/project"))`,
	}), nil)
	if err != nil {
		t.Fatalf("NewPreparedLaunch: %v", err)
	}

	if _, err := json.Marshal(prepared); err == nil || !strings.Contains(err.Error(), "explicit DTO disclosure scope") {
		t.Fatalf("MarshalJSON error = %v", err)
	}

	redacted, err := json.Marshal(prepared.DTO(PreparedLaunchDTOScope{}))
	if err != nil {
		t.Fatalf("marshal redacted DTO: %v", err)
	}
	for _, secret := range []string{"/workspace/project", "/workspace/reference", "/private/tmp/hazmat.sb", "(allow file-read*"} {
		if strings.Contains(string(redacted), secret) {
			t.Fatalf("redacted DTO leaked %q: %s", secret, string(redacted))
		}
	}

	full, err := json.Marshal(prepared.DTO(PreparedLaunchDTOScope{IncludeResolvedHostPaths: true, IncludePolicyText: true}))
	if err != nil {
		t.Fatalf("marshal full DTO: %v", err)
	}
	for _, want := range []string{"/workspace/project", "/workspace/reference", "/private/tmp/hazmat.sb", "(allow file-read*"} {
		if !strings.Contains(string(full), want) {
			t.Fatalf("full DTO missing %q: %s", want, string(full))
		}
	}
}

func TestPreparedLaunchDockerDTORedactsResolvedHostPaths(t *testing.T) {
	plan := BuildPlan(Input{
		Target:        "claude",
		Mode:          sessionmeta.ModeDockerSandbox,
		ProjectDir:    "/workspace/project",
		ReadOnlyDirs:  []string{"/workspace/reference"},
		ReadWriteDirs: []string{"/workspace/cache"},
		HostFacts:     hostfacts.ForGOOS("darwin"),
	})
	artifact := DockerSandboxSpec{
		Name:           "hazmat-claude-project-abcdef",
		Agent:          "claude",
		ProjectDir:     "/workspace/project",
		PolicyProfile:  "baseline",
		MountReadDirs:  []string{"/workspace/reference"},
		MountWriteDirs: []string{"/workspace/cache"},
	}
	prepared, err := NewPreparedLaunch(plan, NewDockerSandboxArtifact(artifact), nil)
	if err != nil {
		t.Fatalf("NewPreparedLaunch: %v", err)
	}
	artifact.MountReadDirs[0] = "/mutated"

	redacted := prepared.DTO(PreparedLaunchDTOScope{})
	if redacted.Plan.ProjectDir != "" || len(redacted.Plan.ReadOnlyDirs) != 0 || len(redacted.Plan.ReadWriteDirs) != 0 {
		t.Fatalf("redacted plan leaked paths: %+v", redacted.Plan)
	}
	if redacted.DockerSandbox.ProjectDir != "" || len(redacted.DockerSandbox.MountReadDirs) != 0 || len(redacted.DockerSandbox.MountWriteDirs) != 0 {
		t.Fatalf("redacted docker DTO leaked paths: %+v", redacted.DockerSandbox)
	}

	full := prepared.DTO(PreparedLaunchDTOScope{IncludeResolvedHostPaths: true})
	if full.Plan.ProjectDir != "/workspace/project" || full.DockerSandbox.ProjectDir != "/workspace/project" {
		t.Fatalf("full DTO missing project paths: %+v", full)
	}
	if len(full.DockerSandbox.MountReadDirs) != 1 || full.DockerSandbox.MountReadDirs[0] != "/workspace/reference" {
		t.Fatalf("full DTO MountReadDirs = %+v", full.DockerSandbox.MountReadDirs)
	}
}
