package sessionbackend

import "fmt"

const (
	PreparedArtifactDarwinSeatbelt ArtifactKind = "darwin-seatbelt"
	PreparedArtifactLinuxLaunch    ArtifactKind = "linux-launch-spec"
	PreparedArtifactDockerSandbox  ArtifactKind = "docker-sandbox-spec"
	PreparedArtifactRemoteEnvelope ArtifactKind = "remote-envelope"

	KindRemoteEnvelope Kind = "remote-envelope"
)

// ArtifactKind is the closed set of prepared backend artifact variants.
type ArtifactKind string

// DarwinSeatbelt is a prepared macOS Seatbelt artifact.
type DarwinSeatbelt struct {
	PolicyPath string `json:"policy_path,omitempty"`
	Policy     string `json:"policy,omitempty"`
}

// LinuxLaunchSpec is a prepared Linux native launch artifact. The launch helper
// remains plan-only until the Linux backend implementation lands.
type LinuxLaunchSpec struct {
	FormatVersion int    `json:"format_version"`
	Backend       string `json:"backend"`
	Phase         string `json:"phase"`
}

// DockerSandboxSpec is a prepared Docker Sandbox artifact.
type DockerSandboxSpec struct {
	Name           string   `json:"name"`
	Agent          string   `json:"agent"`
	ProjectDir     string   `json:"project_dir"`
	PolicyProfile  string   `json:"policy_profile"`
	MountReadDirs  []string `json:"mount_read_dirs,omitempty"`
	MountWriteDirs []string `json:"mount_write_dirs,omitempty"`
}

// RemoteEnvelope is an experimental placeholder for future remote execution.
// It is typeable here, but admission/integrity semantics belong to the remote
// envelope planning bead.
type RemoteEnvelope struct {
	SchemaVersion int    `json:"schema_version"`
	Digest        string `json:"digest,omitempty"`
}

// ArtifactVariant carries exactly one prepared backend artifact candidate.
type ArtifactVariant struct {
	DarwinSeatbelt *DarwinSeatbelt
	LinuxLaunch    *LinuxLaunchSpec
	DockerSandbox  *DockerSandboxSpec
	RemoteEnvelope *RemoteEnvelope
}

// AcceptedGap records a deliberate acceptance of one backend capability gap.
type AcceptedGap struct {
	Feature       string `json:"feature"`
	Reason        string `json:"reason,omitempty"`
	Justification string `json:"justification,omitempty"`
}

// PreparedLaunch is a backend plan paired with exactly one typed prepared
// artifact. It is constructible only when capability gaps are absent or all
// gaps were explicitly accepted.
type PreparedLaunch struct {
	Plan           Plan               `json:"plan"`
	ArtifactKind   ArtifactKind       `json:"artifact_kind"`
	DarwinSeatbelt *DarwinSeatbelt    `json:"darwin_seatbelt,omitempty"`
	LinuxLaunch    *LinuxLaunchSpec   `json:"linux_launch,omitempty"`
	DockerSandbox  *DockerSandboxSpec `json:"docker_sandbox,omitempty"`
	RemoteEnvelope *RemoteEnvelope    `json:"remote_envelope,omitempty"`
	AcceptedGaps   []AcceptedGap      `json:"accepted_gaps,omitempty"`
}

// NewPreparedLaunch validates and constructs a prepared launch artifact.
func NewPreparedLaunch(plan Plan, variant ArtifactVariant, acceptedGaps []AcceptedGap) (PreparedLaunch, error) {
	kind, count := variantKind(variant)
	if count != 1 {
		return PreparedLaunch{}, fmt.Errorf("prepared launch must carry exactly one artifact variant, got %d", count)
	}
	if err := validateArtifactBackend(kind, plan.Backend); err != nil {
		return PreparedLaunch{}, err
	}
	accepted, err := validateAcceptedGaps(plan.CapabilityGaps, acceptedGaps)
	if err != nil {
		return PreparedLaunch{}, err
	}

	return PreparedLaunch{
		Plan:           copyPlan(plan),
		ArtifactKind:   kind,
		DarwinSeatbelt: copyDarwinSeatbelt(variant.DarwinSeatbelt),
		LinuxLaunch:    copyLinuxLaunchSpec(variant.LinuxLaunch),
		DockerSandbox:  copyDockerSandboxSpec(variant.DockerSandbox),
		RemoteEnvelope: copyRemoteEnvelope(variant.RemoteEnvelope),
		AcceptedGaps:   accepted,
	}, nil
}

func variantKind(variant ArtifactVariant) (ArtifactKind, int) {
	var kind ArtifactKind
	count := 0
	if variant.DarwinSeatbelt != nil {
		kind = PreparedArtifactDarwinSeatbelt
		count++
	}
	if variant.LinuxLaunch != nil {
		kind = PreparedArtifactLinuxLaunch
		count++
	}
	if variant.DockerSandbox != nil {
		kind = PreparedArtifactDockerSandbox
		count++
	}
	if variant.RemoteEnvelope != nil {
		kind = PreparedArtifactRemoteEnvelope
		count++
	}
	return kind, count
}

func validateArtifactBackend(kind ArtifactKind, backend Kind) error {
	want := map[ArtifactKind]Kind{
		PreparedArtifactDarwinSeatbelt: KindDarwinNative,
		PreparedArtifactLinuxLaunch:    KindLinuxNative,
		PreparedArtifactDockerSandbox:  KindDockerSandbox,
		PreparedArtifactRemoteEnvelope: KindRemoteEnvelope,
	}[kind]
	if backend != want {
		return fmt.Errorf("artifact %q does not match backend %q", kind, backend)
	}
	return nil
}

func validateAcceptedGaps(gaps []CapabilityGap, accepted []AcceptedGap) ([]AcceptedGap, error) {
	if len(gaps) == 0 {
		if len(accepted) > 0 {
			return nil, fmt.Errorf("accepted capability gaps require matching plan gaps")
		}
		return nil, nil
	}
	needed := make(map[string]CapabilityGap, len(gaps))
	for _, gap := range gaps {
		needed[gap.Feature] = gap
	}
	seen := make(map[string]struct{}, len(accepted))
	for _, acceptedGap := range accepted {
		if acceptedGap.Feature == "" {
			return nil, fmt.Errorf("accepted capability gap feature is required")
		}
		if _, ok := needed[acceptedGap.Feature]; !ok {
			return nil, fmt.Errorf("accepted capability gap %q is not present in plan", acceptedGap.Feature)
		}
		seen[acceptedGap.Feature] = struct{}{}
	}
	for _, gap := range gaps {
		if _, ok := seen[gap.Feature]; !ok {
			return nil, fmt.Errorf("capability gap %q must be accepted before preparing launch", gap.Feature)
		}
	}
	return copyAcceptedGaps(accepted), nil
}

func copyPlan(plan Plan) Plan {
	plan.ReadOnlyDirs = copyStrings(plan.ReadOnlyDirs)
	plan.ReadWriteDirs = copyStrings(plan.ReadWriteDirs)
	plan.Integrations = copyStrings(plan.Integrations)
	plan.IntegrationEnvKeys = copyStrings(plan.IntegrationEnvKeys)
	plan.CapabilityGaps = copyCapabilityGaps(plan.CapabilityGaps)
	plan.LifecycleArtifacts = copyLifecycleArtifacts(plan.LifecycleArtifacts)
	return plan
}

func copyCapabilityGaps(values []CapabilityGap) []CapabilityGap {
	if len(values) == 0 {
		return nil
	}
	out := make([]CapabilityGap, len(values))
	copy(out, values)
	return out
}

func copyLifecycleArtifacts(values []LifecycleArtifact) []LifecycleArtifact {
	if len(values) == 0 {
		return nil
	}
	out := make([]LifecycleArtifact, len(values))
	copy(out, values)
	return out
}

func copyAcceptedGaps(values []AcceptedGap) []AcceptedGap {
	if len(values) == 0 {
		return nil
	}
	out := make([]AcceptedGap, len(values))
	copy(out, values)
	return out
}

func copyDarwinSeatbelt(value *DarwinSeatbelt) *DarwinSeatbelt {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func copyLinuxLaunchSpec(value *LinuxLaunchSpec) *LinuxLaunchSpec {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func copyDockerSandboxSpec(value *DockerSandboxSpec) *DockerSandboxSpec {
	if value == nil {
		return nil
	}
	out := *value
	out.MountReadDirs = copyStrings(value.MountReadDirs)
	out.MountWriteDirs = copyStrings(value.MountWriteDirs)
	return &out
}

func copyRemoteEnvelope(value *RemoteEnvelope) *RemoteEnvelope {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
