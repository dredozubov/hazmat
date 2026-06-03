package sessionbackend

import (
	"encoding/json"
	"fmt"
)

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
	plan           Plan
	artifactKind   ArtifactKind
	darwinSeatbelt *DarwinSeatbelt
	linuxLaunch    *LinuxLaunchSpec
	dockerSandbox  *DockerSandboxSpec
	remoteEnvelope *RemoteEnvelope
	acceptedGaps   []AcceptedGap
}

type PreparedLaunchDTOScope struct {
	IncludePolicyText        bool
	IncludeResolvedHostPaths bool
}

type PreparedLaunchDTO struct {
	Plan           Plan               `json:"plan"`
	ArtifactKind   ArtifactKind       `json:"artifact_kind"`
	DarwinSeatbelt *DarwinSeatbeltDTO `json:"darwin_seatbelt,omitempty"`
	LinuxLaunch    *LinuxLaunchSpec   `json:"linux_launch,omitempty"`
	DockerSandbox  *DockerSandboxDTO  `json:"docker_sandbox,omitempty"`
	RemoteEnvelope *RemoteEnvelope    `json:"remote_envelope,omitempty"`
	AcceptedGaps   []AcceptedGap      `json:"accepted_gaps,omitempty"`
}

type DarwinSeatbeltDTO struct {
	PolicyPath string `json:"policy_path,omitempty"`
	Policy     string `json:"policy,omitempty"`
}

type DockerSandboxDTO struct {
	Name           string   `json:"name"`
	Agent          string   `json:"agent"`
	ProjectDir     string   `json:"project_dir,omitempty"`
	PolicyProfile  string   `json:"policy_profile"`
	MountReadDirs  []string `json:"mount_read_dirs,omitempty"`
	MountWriteDirs []string `json:"mount_write_dirs,omitempty"`
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
		plan:           copyPlan(plan),
		artifactKind:   kind,
		darwinSeatbelt: copyDarwinSeatbelt(variant.DarwinSeatbelt),
		linuxLaunch:    copyLinuxLaunchSpec(variant.LinuxLaunch),
		dockerSandbox:  copyDockerSandboxSpec(variant.DockerSandbox),
		remoteEnvelope: copyRemoteEnvelope(variant.RemoteEnvelope),
		acceptedGaps:   accepted,
	}, nil
}

func (p PreparedLaunch) Plan() Plan {
	return copyPlan(p.plan)
}

func (p PreparedLaunch) ArtifactKind() ArtifactKind {
	return p.artifactKind
}

func (p PreparedLaunch) DarwinSeatbelt() (*DarwinSeatbelt, bool) {
	return copyDarwinSeatbelt(p.darwinSeatbelt), p.artifactKind == PreparedArtifactDarwinSeatbelt && p.darwinSeatbelt != nil
}

func (p PreparedLaunch) LinuxLaunch() (*LinuxLaunchSpec, bool) {
	return copyLinuxLaunchSpec(p.linuxLaunch), p.artifactKind == PreparedArtifactLinuxLaunch && p.linuxLaunch != nil
}

func (p PreparedLaunch) DockerSandbox() (*DockerSandboxSpec, bool) {
	return copyDockerSandboxSpec(p.dockerSandbox), p.artifactKind == PreparedArtifactDockerSandbox && p.dockerSandbox != nil
}

func (p PreparedLaunch) RemoteEnvelope() (*RemoteEnvelope, bool) {
	return copyRemoteEnvelope(p.remoteEnvelope), p.artifactKind == PreparedArtifactRemoteEnvelope && p.remoteEnvelope != nil
}

func (p PreparedLaunch) AcceptedGaps() []AcceptedGap {
	return copyAcceptedGaps(p.acceptedGaps)
}

// DTO returns a serialization shape with explicit disclosure controls. The
// zero scope redacts resolved host paths and policy text.
func (p PreparedLaunch) DTO(scope PreparedLaunchDTOScope) PreparedLaunchDTO {
	return PreparedLaunchDTO{
		Plan:           planDTO(p.plan, scope),
		ArtifactKind:   p.artifactKind,
		DarwinSeatbelt: darwinSeatbeltDTO(p.darwinSeatbelt, scope),
		LinuxLaunch:    copyLinuxLaunchSpec(p.linuxLaunch),
		DockerSandbox:  dockerSandboxDTO(p.dockerSandbox, scope),
		RemoteEnvelope: copyRemoteEnvelope(p.remoteEnvelope),
		AcceptedGaps:   copyAcceptedGaps(p.acceptedGaps),
	}
}

func (p PreparedLaunch) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("sessionbackend.PreparedLaunch requires explicit DTO disclosure scope")
}

var _ json.Marshaler = PreparedLaunch{}

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

func planDTO(plan Plan, scope PreparedLaunchDTOScope) Plan {
	out := copyPlan(plan)
	if !scope.IncludeResolvedHostPaths {
		out.ProjectDir = ""
		out.ReadOnlyDirs = nil
		out.ReadWriteDirs = nil
		for i := range out.LifecycleArtifacts {
			out.LifecycleArtifacts[i].Path = ""
		}
	}
	return out
}

func darwinSeatbeltDTO(value *DarwinSeatbelt, scope PreparedLaunchDTOScope) *DarwinSeatbeltDTO {
	if value == nil {
		return nil
	}
	out := &DarwinSeatbeltDTO{}
	if scope.IncludeResolvedHostPaths {
		out.PolicyPath = value.PolicyPath
	}
	if scope.IncludePolicyText {
		out.Policy = value.Policy
	}
	return out
}

func dockerSandboxDTO(value *DockerSandboxSpec, scope PreparedLaunchDTOScope) *DockerSandboxDTO {
	if value == nil {
		return nil
	}
	out := &DockerSandboxDTO{
		Name:          value.Name,
		Agent:         value.Agent,
		PolicyProfile: value.PolicyProfile,
	}
	if scope.IncludeResolvedHostPaths {
		out.ProjectDir = value.ProjectDir
		out.MountReadDirs = copyStrings(value.MountReadDirs)
		out.MountWriteDirs = copyStrings(value.MountWriteDirs)
	}
	return out
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
