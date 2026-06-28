package sessionbackend

import (
	"encoding/json"
	"fmt"

	"hazmat/sessionmeta"
)

const (
	PreparedArtifactDarwinSeatbelt ArtifactKind = "darwin-seatbelt"
	PreparedArtifactLinuxLaunch    ArtifactKind = "linux-launch-spec"
	PreparedArtifactDockerSandbox  ArtifactKind = "docker-sandbox-spec"
	PreparedArtifactRemoteEnvelope ArtifactKind = "remote-envelope"
	PreparedArtifactAppleContainer ArtifactKind = "apple-container-launch-spec"

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

// AppleContainerLaunchSpec is a prepared Apple Container launch artifact
// summary. The runtime remains plan-only until the Apple Container backend
// implementation lands (tla/MC_AppleContainerLaunch governs that boundary).
type AppleContainerLaunchSpec struct {
	FormatVersion int    `json:"format_version"`
	Backend       string `json:"backend"`
	Phase         string `json:"phase"`
	ContainerName string `json:"container_name,omitempty"`
	Image         string `json:"image,omitempty"`
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

// PreparedArtifact is the closed set of prepared backend artifact variants.
// Callers construct values through the New*Artifact functions in this package.
type PreparedArtifact interface {
	preparedArtifact()
	artifactKind() ArtifactKind
	validate() error
	applyToPreparedLaunch(*PreparedLaunch)
}

type darwinSeatbeltArtifact struct {
	artifact DarwinSeatbelt
}

type linuxLaunchArtifact struct {
	artifact LinuxLaunchSpec
}

type dockerSandboxArtifact struct {
	artifact DockerSandboxSpec
}

type remoteEnvelopeArtifact struct {
	artifact RemoteEnvelope
}

type appleContainerArtifact struct {
	artifact AppleContainerLaunchSpec
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
	appleContainer *AppleContainerLaunchSpec
	acceptedGaps   []AcceptedGap
}

type PreparedLaunchDTOScope struct {
	IncludePolicyText        bool
	IncludeResolvedHostPaths bool
}

type PreparedLaunchDTO struct {
	Plan           Plan                      `json:"plan"`
	ArtifactKind   ArtifactKind              `json:"artifact_kind"`
	DarwinSeatbelt *DarwinSeatbeltDTO        `json:"darwin_seatbelt,omitempty"`
	LinuxLaunch    *LinuxLaunchSpec          `json:"linux_launch,omitempty"`
	DockerSandbox  *DockerSandboxDTO         `json:"docker_sandbox,omitempty"`
	RemoteEnvelope *RemoteEnvelope           `json:"remote_envelope,omitempty"`
	AppleContainer *AppleContainerLaunchSpec `json:"apple_container,omitempty"`
	AcceptedGaps   []AcceptedGap             `json:"accepted_gaps,omitempty"`
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

// NewDarwinSeatbeltArtifact returns a prepared macOS Seatbelt artifact variant.
func NewDarwinSeatbeltArtifact(artifact DarwinSeatbelt) PreparedArtifact {
	return darwinSeatbeltArtifact{artifact: artifact}
}

// NewLinuxLaunchArtifact returns a prepared Linux native launch artifact variant.
func NewLinuxLaunchArtifact(artifact LinuxLaunchSpec) PreparedArtifact {
	return linuxLaunchArtifact{artifact: artifact}
}

// NewDockerSandboxArtifact returns a prepared Docker Sandbox artifact variant.
func NewDockerSandboxArtifact(artifact DockerSandboxSpec) PreparedArtifact {
	artifact.MountReadDirs = copyStrings(artifact.MountReadDirs)
	artifact.MountWriteDirs = copyStrings(artifact.MountWriteDirs)
	return dockerSandboxArtifact{artifact: artifact}
}

// NewRemoteEnvelopeArtifact returns a prepared remote envelope artifact variant.
func NewRemoteEnvelopeArtifact(artifact RemoteEnvelope) PreparedArtifact {
	return remoteEnvelopeArtifact{artifact: artifact}
}

// NewAppleContainerArtifact returns a prepared Apple Container launch artifact variant.
func NewAppleContainerArtifact(artifact AppleContainerLaunchSpec) PreparedArtifact {
	return appleContainerArtifact{artifact: artifact}
}

func (darwinSeatbeltArtifact) preparedArtifact() {}

func (a darwinSeatbeltArtifact) artifactKind() ArtifactKind {
	return PreparedArtifactDarwinSeatbelt
}

func (a darwinSeatbeltArtifact) validate() error {
	if a.artifact.PolicyPath == "" && a.artifact.Policy == "" {
		return fmt.Errorf("darwin seatbelt artifact requires policy path or policy text")
	}
	return nil
}

func (a darwinSeatbeltArtifact) applyToPreparedLaunch(p *PreparedLaunch) {
	p.darwinSeatbelt = copyDarwinSeatbelt(&a.artifact)
}

func (linuxLaunchArtifact) preparedArtifact() {}

func (a linuxLaunchArtifact) artifactKind() ArtifactKind {
	return PreparedArtifactLinuxLaunch
}

func (a linuxLaunchArtifact) validate() error {
	if a.artifact.FormatVersion <= 0 {
		return fmt.Errorf("linux launch artifact format_version is required")
	}
	if a.artifact.Backend != string(KindLinuxNative) {
		return fmt.Errorf("linux launch artifact backend %q does not match %q", a.artifact.Backend, KindLinuxNative)
	}
	if a.artifact.Phase == "" {
		return fmt.Errorf("linux launch artifact phase is required")
	}
	return nil
}

func (a linuxLaunchArtifact) applyToPreparedLaunch(p *PreparedLaunch) {
	p.linuxLaunch = copyLinuxLaunchSpec(&a.artifact)
}

func (dockerSandboxArtifact) preparedArtifact() {}

func (a dockerSandboxArtifact) artifactKind() ArtifactKind {
	return PreparedArtifactDockerSandbox
}

func (a dockerSandboxArtifact) validate() error {
	if a.artifact.Name == "" {
		return fmt.Errorf("docker sandbox artifact name is required")
	}
	if a.artifact.Agent == "" {
		return fmt.Errorf("docker sandbox artifact agent is required")
	}
	if a.artifact.ProjectDir == "" {
		return fmt.Errorf("docker sandbox artifact project_dir is required")
	}
	if a.artifact.PolicyProfile == "" {
		return fmt.Errorf("docker sandbox artifact policy_profile is required")
	}
	return nil
}

func (a dockerSandboxArtifact) applyToPreparedLaunch(p *PreparedLaunch) {
	artifact := copyDockerSandboxSpec(&a.artifact)
	if artifact != nil {
		artifact.MountReadDirs = copyStrings(a.artifact.MountReadDirs)
		artifact.MountWriteDirs = copyStrings(a.artifact.MountWriteDirs)
	}
	p.dockerSandbox = artifact
}

func (remoteEnvelopeArtifact) preparedArtifact() {}

func (a remoteEnvelopeArtifact) artifactKind() ArtifactKind {
	return PreparedArtifactRemoteEnvelope
}

func (a remoteEnvelopeArtifact) validate() error {
	if a.artifact.SchemaVersion <= 0 {
		return fmt.Errorf("remote envelope artifact schema_version is required")
	}
	if a.artifact.Digest == "" {
		return fmt.Errorf("remote envelope artifact digest is required")
	}
	return nil
}

func (a remoteEnvelopeArtifact) applyToPreparedLaunch(p *PreparedLaunch) {
	p.remoteEnvelope = copyRemoteEnvelope(&a.artifact)
}

func (appleContainerArtifact) preparedArtifact() {}

func (a appleContainerArtifact) artifactKind() ArtifactKind {
	return PreparedArtifactAppleContainer
}

func (a appleContainerArtifact) validate() error {
	if a.artifact.FormatVersion <= 0 {
		return fmt.Errorf("apple container artifact format_version is required")
	}
	if a.artifact.Backend != string(KindAppleContainer) {
		return fmt.Errorf("apple container artifact backend %q does not match %q", a.artifact.Backend, KindAppleContainer)
	}
	if a.artifact.Phase == "" {
		return fmt.Errorf("apple container artifact phase is required")
	}
	if a.artifact.ContainerName == "" {
		return fmt.Errorf("apple container artifact container_name is required")
	}
	if a.artifact.Image == "" {
		return fmt.Errorf("apple container artifact image is required")
	}
	return nil
}

func (a appleContainerArtifact) applyToPreparedLaunch(p *PreparedLaunch) {
	p.appleContainer = copyAppleContainerLaunchSpec(&a.artifact)
}

// NewPreparedLaunch validates and constructs a prepared launch artifact.
func NewPreparedLaunch(plan Plan, artifact PreparedArtifact, acceptedGaps []AcceptedGap) (PreparedLaunch, error) {
	if artifact == nil {
		return PreparedLaunch{}, fmt.Errorf("prepared launch artifact is required")
	}
	if err := validatePreparedPlan(plan); err != nil {
		return PreparedLaunch{}, err
	}
	kind := artifact.artifactKind()
	if err := validateArtifactBackend(kind, plan.Backend); err != nil {
		return PreparedLaunch{}, err
	}
	if err := artifact.validate(); err != nil {
		return PreparedLaunch{}, err
	}
	accepted, err := validateAcceptedGaps(plan.CapabilityGaps, acceptedGaps)
	if err != nil {
		return PreparedLaunch{}, err
	}

	prepared := PreparedLaunch{
		plan:         copyPlan(plan),
		artifactKind: kind,
		acceptedGaps: accepted,
	}
	artifact.applyToPreparedLaunch(&prepared)
	return prepared, nil
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

func (p PreparedLaunch) AppleContainer() (*AppleContainerLaunchSpec, bool) {
	return copyAppleContainerLaunchSpec(p.appleContainer), p.artifactKind == PreparedArtifactAppleContainer && p.appleContainer != nil
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
		AppleContainer: copyAppleContainerLaunchSpec(p.appleContainer),
		AcceptedGaps:   copyAcceptedGaps(p.acceptedGaps),
	}
}

func (p PreparedLaunch) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("sessionbackend.PreparedLaunch requires explicit DTO disclosure scope")
}

var _ json.Marshaler = PreparedLaunch{}

func validateArtifactBackend(kind ArtifactKind, backend Kind) error {
	want, ok := map[ArtifactKind]Kind{
		PreparedArtifactDarwinSeatbelt: KindDarwinNative,
		PreparedArtifactLinuxLaunch:    KindLinuxNative,
		PreparedArtifactDockerSandbox:  KindDockerSandbox,
		PreparedArtifactRemoteEnvelope: KindRemoteEnvelope,
		PreparedArtifactAppleContainer: KindAppleContainer,
	}[kind]
	if !ok {
		return fmt.Errorf("unsupported prepared artifact kind %q", kind)
	}
	if backend != want {
		return fmt.Errorf("artifact %q does not match backend %q", kind, backend)
	}
	return nil
}

func validatePreparedPlan(plan Plan) error {
	if plan.Target == "" {
		return fmt.Errorf("prepared launch plan target is required")
	}
	if plan.ProjectDir == "" {
		return fmt.Errorf("prepared launch plan project_dir is required")
	}
	switch plan.Mode {
	case sessionmeta.ModeNative, sessionmeta.ModeDockerSandbox, sessionmeta.ModeAppleContainer:
	case "":
		return fmt.Errorf("prepared launch plan mode is required")
	default:
		return fmt.Errorf("prepared launch plan mode %q is unsupported", plan.Mode)
	}
	switch plan.Backend {
	case KindDarwinNative, KindLinuxNative, KindUnsupportedNative, KindRemoteEnvelope:
		if plan.Mode != sessionmeta.ModeNative {
			return fmt.Errorf("prepared launch backend %q requires mode %q", plan.Backend, sessionmeta.ModeNative)
		}
	case KindDockerSandbox:
		if plan.Mode != sessionmeta.ModeDockerSandbox {
			return fmt.Errorf("prepared launch backend %q requires mode %q", plan.Backend, sessionmeta.ModeDockerSandbox)
		}
	case KindAppleContainer:
		if plan.Mode != sessionmeta.ModeAppleContainer {
			return fmt.Errorf("prepared launch backend %q requires mode %q", plan.Backend, sessionmeta.ModeAppleContainer)
		}
	case "":
		return fmt.Errorf("prepared launch plan backend is required")
	default:
		return fmt.Errorf("prepared launch plan backend %q is unsupported", plan.Backend)
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

func copyAppleContainerLaunchSpec(value *AppleContainerLaunchSpec) *AppleContainerLaunchSpec {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
