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
	plan := BuildPlan(Input{
		Target:     "codex",
		Mode:       sessionmeta.ModeNative,
		ProjectDir: "/workspace/project",
		HostFacts:  hostfacts.ForGOOS("darwin"),
	})

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

func TestNewPreparedLaunchRejectsMalformedPlan(t *testing.T) {
	valid := BuildPlan(Input{
		Target:     "codex",
		Mode:       sessionmeta.ModeNative,
		ProjectDir: "/workspace/project",
		HostFacts:  hostfacts.ForGOOS("darwin"),
	})
	cases := []struct {
		name string
		mut  func(Plan) Plan
		want string
	}{
		{
			name: "missing target",
			mut: func(plan Plan) Plan {
				plan.Target = ""
				return plan
			},
			want: "target is required",
		},
		{
			name: "missing project",
			mut: func(plan Plan) Plan {
				plan.ProjectDir = ""
				return plan
			},
			want: "project_dir is required",
		},
		{
			name: "missing mode",
			mut: func(plan Plan) Plan {
				plan.Mode = ""
				return plan
			},
			want: "mode is required",
		},
		{
			name: "unsupported backend",
			mut: func(plan Plan) Plan {
				plan.Backend = Kind("raw")
				return plan
			},
			want: `backend "raw" is unsupported`,
		},
		{
			name: "backend mode mismatch",
			mut: func(plan Plan) Plan {
				plan.Mode = sessionmeta.ModeDockerSandbox
				return plan
			},
			want: `backend "darwin-native" requires mode "native"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPreparedLaunch(tc.mut(valid), NewDarwinSeatbeltArtifact(DarwinSeatbelt{PolicyPath: "/tmp/policy.sb"}), nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNewPreparedLaunchRejectsMalformedArtifacts(t *testing.T) {
	cases := []struct {
		name         string
		plan         Plan
		artifact     PreparedArtifact
		acceptedGaps []AcceptedGap
		want         string
	}{
		{
			name: "darwin empty policy",
			plan: BuildPlan(Input{
				Target:     "codex",
				Mode:       sessionmeta.ModeNative,
				ProjectDir: "/workspace/project",
				HostFacts:  hostfacts.ForGOOS("darwin"),
			}),
			artifact: NewDarwinSeatbeltArtifact(DarwinSeatbelt{}),
			want:     "policy path or policy text",
		},
		{
			name: "linux missing backend",
			plan: BuildPlan(Input{
				Target:     "codex",
				Mode:       sessionmeta.ModeNative,
				ProjectDir: "/workspace/project",
				HostFacts:  hostfacts.ForGOOS("linux"),
			}),
			artifact: NewLinuxLaunchArtifact(LinuxLaunchSpec{FormatVersion: 1, Phase: "plan-only"}),
			acceptedGaps: []AcceptedGap{{
				Feature:       GapNativeLaunch,
				Justification: "plan-only Linux launch artifact",
			}},
			want: "linux launch artifact backend",
		},
		{
			name: "docker missing agent",
			plan: BuildPlan(Input{
				Target:     "codex",
				Mode:       sessionmeta.ModeDockerSandbox,
				ProjectDir: "/workspace/project",
				HostFacts:  hostfacts.ForGOOS("darwin"),
			}),
			artifact: NewDockerSandboxArtifact(DockerSandboxSpec{Name: "hazmat", ProjectDir: "/workspace/project", PolicyProfile: "baseline"}),
			want:     "agent is required",
		},
		{
			name:     "remote missing digest",
			plan:     Plan{Target: "codex", Mode: sessionmeta.ModeNative, Backend: KindRemoteEnvelope, ProjectDir: "/workspace/project"},
			artifact: NewRemoteEnvelopeArtifact(RemoteEnvelope{SchemaVersion: 1}),
			want:     "digest is required",
		},
		{
			name: "apple container missing image",
			plan: BuildPlan(Input{
				Target:     "codex",
				Mode:       sessionmeta.ModeAppleContainer,
				ProjectDir: "/workspace/project",
				HostFacts:  hostfacts.ForGOOS("darwin"),
			}),
			artifact: NewAppleContainerArtifact(AppleContainerLaunchSpec{
				FormatVersion: 1,
				Backend:       string(KindAppleContainer),
				Phase:         "plan-only",
				ContainerName: "hazmat-apple",
			}),
			acceptedGaps: []AcceptedGap{{
				Feature:       GapAppleContainerLaunch,
				Justification: "plan-only Apple Container launch artifact",
			}},
			want: "image is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPreparedLaunch(tc.plan, tc.artifact, tc.acceptedGaps)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNewPreparedLaunchRejectsBackendMismatch(t *testing.T) {
	plan := BuildPlan(Input{
		Target:     "codex",
		Mode:       sessionmeta.ModeDockerSandbox,
		ProjectDir: "/workspace/project",
		HostFacts:  hostfacts.ForGOOS("darwin"),
	})

	_, err := NewPreparedLaunch(plan, NewDarwinSeatbeltArtifact(DarwinSeatbelt{PolicyPath: "/tmp/policy.sb"}), nil)
	if err == nil || !strings.Contains(err.Error(), `does not match backend "docker-sandbox"`) {
		t.Fatalf("backend mismatch error = %v", err)
	}
}

func TestNewPreparedLaunchRequiresAcceptedCapabilityGaps(t *testing.T) {
	plan := BuildPlan(Input{
		Target:     "codex",
		Mode:       sessionmeta.ModeNative,
		ProjectDir: "/workspace/project",
		HostFacts:  hostfacts.ForGOOS("linux"),
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
		Target:     "codex",
		Mode:       sessionmeta.ModeDockerSandbox,
		ProjectDir: "/workspace/project",
		HostFacts:  hostfacts.ForGOOS("darwin"),
	})

	_, err := NewPreparedLaunch(plan, NewDockerSandboxArtifact(DockerSandboxSpec{Name: "hazmat", Agent: "claude", ProjectDir: "/workspace/project", PolicyProfile: "baseline"}), []AcceptedGap{{Feature: GapNativeLaunch}})
	if err == nil || !strings.Contains(err.Error(), "accepted capability gaps require matching plan gaps") {
		t.Fatalf("extra accepted gap error = %v", err)
	}
}

func TestNewPreparedLaunchAllowsRemoteEnvelopePlaceholder(t *testing.T) {
	plan := Plan{
		Target:     "codex",
		Mode:       sessionmeta.ModeNative,
		Backend:    KindRemoteEnvelope,
		ProjectDir: "/workspace/project",
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

func TestPreparedLaunchDTODisclosureScopeMatrix(t *testing.T) {
	plan := BuildPlan(Input{
		Target:       "codex",
		Mode:         sessionmeta.ModeNative,
		ProjectDir:   "/workspace/project",
		ReadOnlyDirs: []string{"/workspace/reference"},
		HostFacts:    hostfacts.ForGOOS("darwin"),
	})
	prepared, err := NewPreparedLaunch(plan, NewDarwinSeatbeltArtifact(DarwinSeatbelt{
		PolicyPath: "/private/tmp/hazmat.sb",
		Policy:     "sbpl-policy-text",
	}), nil)
	if err != nil {
		t.Fatalf("NewPreparedLaunch: %v", err)
	}

	cases := []struct {
		name    string
		scope   PreparedLaunchDTOScope
		present []string
		absent  []string
	}{
		{
			name: "default redacts host paths and policy text",
			present: []string{
				`"artifact_kind":"darwin-seatbelt"`,
			},
			absent: []string{
				"/workspace/project",
				"/workspace/reference",
				"/private/tmp/hazmat.sb",
				"sbpl-policy-text",
			},
		},
		{
			name:  "host path scope exposes paths only",
			scope: PreparedLaunchDTOScope{IncludeResolvedHostPaths: true},
			present: []string{
				"/workspace/project",
				"/workspace/reference",
				"/private/tmp/hazmat.sb",
			},
			absent: []string{"sbpl-policy-text"},
		},
		{
			name:  "policy scope exposes policy only",
			scope: PreparedLaunchDTOScope{IncludePolicyText: true},
			present: []string{
				"sbpl-policy-text",
			},
			absent: []string{
				"/workspace/project",
				"/workspace/reference",
				"/private/tmp/hazmat.sb",
			},
		},
		{
			name:  "full scope exposes paths and policy",
			scope: PreparedLaunchDTOScope{IncludeResolvedHostPaths: true, IncludePolicyText: true},
			present: []string{
				"/workspace/project",
				"/workspace/reference",
				"/private/tmp/hazmat.sb",
				"sbpl-policy-text",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(prepared.DTO(tc.scope))
			if err != nil {
				t.Fatalf("marshal DTO: %v", err)
			}
			text := string(data)
			for _, want := range tc.present {
				if !strings.Contains(text, want) {
					t.Fatalf("DTO missing %q: %s", want, text)
				}
			}
			for _, forbidden := range tc.absent {
				if strings.Contains(text, forbidden) {
					t.Fatalf("DTO leaked %q: %s", forbidden, text)
				}
			}
		})
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

func TestPreparedLaunchDTOEmitsExactlySelectedArtifact(t *testing.T) {
	cases := []struct {
		name         string
		plan         Plan
		artifact     PreparedArtifact
		acceptedGaps []AcceptedGap
		wantKind     ArtifactKind
	}{
		{
			name: "darwin",
			plan: BuildPlan(Input{
				Target:     "codex",
				Mode:       sessionmeta.ModeNative,
				ProjectDir: "/workspace/project",
				HostFacts:  hostfacts.ForGOOS("darwin"),
			}),
			artifact: NewDarwinSeatbeltArtifact(DarwinSeatbelt{PolicyPath: "/tmp/policy.sb"}),
			wantKind: PreparedArtifactDarwinSeatbelt,
		},
		{
			name: "linux",
			plan: BuildPlan(Input{
				Target:     "codex",
				Mode:       sessionmeta.ModeNative,
				ProjectDir: "/workspace/project",
				HostFacts:  hostfacts.ForGOOS("linux"),
			}),
			artifact: NewLinuxLaunchArtifact(LinuxLaunchSpec{FormatVersion: 1, Backend: string(KindLinuxNative), Phase: "plan-only"}),
			acceptedGaps: []AcceptedGap{{
				Feature:       GapNativeLaunch,
				Justification: "plan-only Linux launch artifact",
			}},
			wantKind: PreparedArtifactLinuxLaunch,
		},
		{
			name: "docker",
			plan: BuildPlan(Input{
				Target:     "codex",
				Mode:       sessionmeta.ModeDockerSandbox,
				ProjectDir: "/workspace/project",
				HostFacts:  hostfacts.ForGOOS("darwin"),
			}),
			artifact: NewDockerSandboxArtifact(DockerSandboxSpec{Name: "hazmat", Agent: "claude", ProjectDir: "/workspace/project", PolicyProfile: "baseline"}),
			wantKind: PreparedArtifactDockerSandbox,
		},
		{
			name:     "remote",
			plan:     Plan{Target: "codex", Mode: sessionmeta.ModeNative, Backend: KindRemoteEnvelope, ProjectDir: "/workspace/project"},
			artifact: NewRemoteEnvelopeArtifact(RemoteEnvelope{SchemaVersion: 1, Digest: "sha256:remote"}),
			wantKind: PreparedArtifactRemoteEnvelope,
		},
		{
			name: "apple-container",
			plan: BuildPlan(Input{
				Target:     "codex",
				Mode:       sessionmeta.ModeAppleContainer,
				ProjectDir: "/workspace/project",
				HostFacts:  hostfacts.ForGOOS("darwin"),
			}),
			artifact: NewAppleContainerArtifact(AppleContainerLaunchSpec{
				FormatVersion: 1,
				Backend:       string(KindAppleContainer),
				Phase:         "plan-only",
				ContainerName: "hazmat-apple",
				Image:         "example:latest",
			}),
			acceptedGaps: []AcceptedGap{{
				Feature:       GapAppleContainerLaunch,
				Justification: "plan-only Apple Container launch artifact",
			}},
			wantKind: PreparedArtifactAppleContainer,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := NewPreparedLaunch(tc.plan, tc.artifact, tc.acceptedGaps)
			if err != nil {
				t.Fatalf("NewPreparedLaunch: %v", err)
			}
			dto := prepared.DTO(PreparedLaunchDTOScope{IncludeResolvedHostPaths: true, IncludePolicyText: true})
			if dto.ArtifactKind != tc.wantKind {
				t.Fatalf("ArtifactKind = %q, want %q", dto.ArtifactKind, tc.wantKind)
			}
			if count := preparedLaunchDTOArtifactCount(dto); count != 1 {
				t.Fatalf("DTO artifact count = %d, want 1: %+v", count, dto)
			}
		})
	}
}

func preparedLaunchDTOArtifactCount(dto PreparedLaunchDTO) int {
	count := 0
	for _, present := range []bool{
		dto.DarwinSeatbelt != nil,
		dto.LinuxLaunch != nil,
		dto.DockerSandbox != nil,
		dto.RemoteEnvelope != nil,
		dto.AppleContainer != nil,
	} {
		if present {
			count++
		}
	}
	return count
}
