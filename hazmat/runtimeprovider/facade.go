// Package runtimeprovider defines the reusable boundary between Hazmat's
// side-effect-free runtime contracts and effectful runtime implementations.
package runtimeprovider

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"hazmat/containment"
	"hazmat/sessionbackend"
)

// Kind names a concrete runtime provider lane, not just a backend family.
type Kind string

const (
	KindMacOSCurrentUser  Kind = "macos-current-user"
	KindMacOSAgentUser    Kind = "macos-agent-user"
	KindDockerSandbox     Kind = "docker-sandbox"
	KindAppleContainer    Kind = "apple-container"
	KindLinuxCurrentUser  Kind = "linux-current-user"
	KindLinuxAgentUser    Kind = "linux-agent-user"
	KindRemoteEnvelope    Kind = "remote-envelope"
	KindUnsupportedNative Kind = "unsupported-native"

	// KindDarwinNative is a compatibility alias for the supported macOS
	// agent-user native lane. New provider-facing code should use
	// KindMacOSAgentUser so backend and identity lane stay distinct.
	KindDarwinNative Kind = KindMacOSAgentUser
)

// Status is the provider support state surfaced to planners and callers.
type Status string

const (
	StatusSupported     Status = "supported"
	StatusExperimental  Status = "experimental"
	StatusPlanOnly      Status = "plan-only"
	StatusSetupRequired Status = "setup-required"
	StatusUnsupported   Status = "unsupported"
)

// IdentityBoundary describes the user/runtime identity boundary for a provider.
type IdentityBoundary string

const (
	IdentityMacOSAgentUser IdentityBoundary = "macos-agent-user"
	IdentityCurrentUser    IdentityBoundary = "current-user"
	IdentityLinuxAgentUser IdentityBoundary = "linux-agent-user"
	IdentityContainerUser  IdentityBoundary = "container-user"
	IdentityRemoteWorker   IdentityBoundary = "remote-worker"
	IdentityNone           IdentityBoundary = "none"
)

type HostPlatform string

const (
	PlatformMacOS       HostPlatform = "macos"
	PlatformLinux       HostPlatform = "linux"
	PlatformContainer   HostPlatform = "container"
	PlatformRemote      HostPlatform = "remote"
	PlatformUnsupported HostPlatform = "unsupported"
)

type UserMode string

const (
	UserModeCurrent   UserMode = "current-user"
	UserModeAgent     UserMode = "agent-user"
	UserModeContainer UserMode = "container-user"
	UserModeRemote    UserMode = "remote-worker"
	UserModeNone      UserMode = "none"
)

type Descriptor struct {
	Kind             Kind
	Backend          sessionbackend.Kind
	Status           Status
	IdentityBoundary IdentityBoundary
	HostPlatform     HostPlatform
	UserMode         UserMode
}

type Lane struct {
	HostPlatform HostPlatform
	UserMode     UserMode
	Backend      sessionbackend.Kind
}

type StatusDefinition struct {
	Status     Status
	Label      string
	Executable bool
	Message    string
}

func StatusDefinitions() []StatusDefinition {
	return []StatusDefinition{
		{Status: StatusSupported, Label: "supported", Executable: true, Message: "provider may launch when admission succeeds"},
		{Status: StatusExperimental, Label: "experimental", Executable: true, Message: "provider is executable behind explicit experimental controls"},
		{Status: StatusPlanOnly, Label: "plan-only", Message: "provider can preview plans and gaps but must not launch"},
		{Status: StatusSetupRequired, Label: "setup required", Message: "provider needs persistent setup resources before admission"},
		{Status: StatusUnsupported, Label: "unsupported", Message: "provider is registered only to explain unsupported routing"},
	}
}

func DescribeStatus(status Status) (StatusDefinition, bool) {
	for _, definition := range StatusDefinitions() {
		if definition.Status == status {
			return definition, true
		}
	}
	return StatusDefinition{}, false
}

func (d Descriptor) Validate() error {
	if d.Kind == "" {
		return fmt.Errorf("runtime provider kind is required")
	}
	if d.Backend == "" {
		return fmt.Errorf("runtime provider backend is required")
	}
	switch d.Status {
	case StatusSupported, StatusExperimental, StatusPlanOnly, StatusSetupRequired, StatusUnsupported:
	case "":
		return fmt.Errorf("runtime provider status is required")
	default:
		return fmt.Errorf("runtime provider status %q is unsupported", d.Status)
	}
	switch d.IdentityBoundary {
	case IdentityMacOSAgentUser, IdentityCurrentUser, IdentityLinuxAgentUser, IdentityContainerUser, IdentityRemoteWorker, IdentityNone:
	case "":
		return fmt.Errorf("runtime provider identity boundary is required")
	default:
		return fmt.Errorf("runtime provider identity boundary %q is unsupported", d.IdentityBoundary)
	}
	switch d.HostPlatform {
	case PlatformMacOS, PlatformLinux, PlatformContainer, PlatformRemote, PlatformUnsupported:
	case "":
		return fmt.Errorf("runtime provider host platform is required")
	default:
		return fmt.Errorf("runtime provider host platform %q is unsupported", d.HostPlatform)
	}
	switch d.UserMode {
	case UserModeCurrent, UserModeAgent, UserModeContainer, UserModeRemote, UserModeNone:
	case "":
		return fmt.Errorf("runtime provider user mode is required")
	default:
		return fmt.Errorf("runtime provider user mode %q is unsupported", d.UserMode)
	}
	return nil
}

func (d Descriptor) StatusRecord() ProviderStatusRecord {
	definition, ok := DescribeStatus(d.Status)
	if !ok {
		definition = StatusDefinition{Status: d.Status, Label: string(d.Status)}
	}
	return ProviderStatusRecord{
		Provider:         d.Kind,
		Backend:          d.Backend,
		Status:           d.Status,
		StatusLabel:      definition.Label,
		Executable:       definition.Executable,
		IdentityBoundary: d.IdentityBoundary,
		HostPlatform:     d.HostPlatform,
		UserMode:         d.UserMode,
		Message:          definition.Message,
	}
}

func (d Descriptor) Lane() Lane {
	return Lane{
		HostPlatform: d.HostPlatform,
		UserMode:     d.UserMode,
		Backend:      d.Backend,
	}
}

func KnownDescriptors() []Descriptor {
	return []Descriptor{
		{Kind: KindMacOSCurrentUser, Backend: sessionbackend.KindDarwinNative, Status: StatusExperimental, IdentityBoundary: IdentityCurrentUser, HostPlatform: PlatformMacOS, UserMode: UserModeCurrent},
		{Kind: KindMacOSAgentUser, Backend: sessionbackend.KindDarwinNative, Status: StatusSupported, IdentityBoundary: IdentityMacOSAgentUser, HostPlatform: PlatformMacOS, UserMode: UserModeAgent},
		{Kind: KindDockerSandbox, Backend: sessionbackend.KindDockerSandbox, Status: StatusSupported, IdentityBoundary: IdentityContainerUser, HostPlatform: PlatformContainer, UserMode: UserModeContainer},
		{Kind: KindAppleContainer, Backend: sessionbackend.KindAppleContainer, Status: StatusExperimental, IdentityBoundary: IdentityContainerUser, HostPlatform: PlatformContainer, UserMode: UserModeContainer},
		{Kind: KindLinuxCurrentUser, Backend: sessionbackend.KindLinuxNative, Status: StatusPlanOnly, IdentityBoundary: IdentityCurrentUser, HostPlatform: PlatformLinux, UserMode: UserModeCurrent},
		{Kind: KindLinuxAgentUser, Backend: sessionbackend.KindLinuxNative, Status: StatusSetupRequired, IdentityBoundary: IdentityLinuxAgentUser, HostPlatform: PlatformLinux, UserMode: UserModeAgent},
		{Kind: KindRemoteEnvelope, Backend: sessionbackend.KindRemoteEnvelope, Status: StatusPlanOnly, IdentityBoundary: IdentityRemoteWorker, HostPlatform: PlatformRemote, UserMode: UserModeRemote},
		{Kind: KindUnsupportedNative, Backend: sessionbackend.KindUnsupportedNative, Status: StatusUnsupported, IdentityBoundary: IdentityNone, HostPlatform: PlatformUnsupported, UserMode: UserModeNone},
	}
}

func DescriptorForKind(kind Kind) (Descriptor, bool) {
	for _, descriptor := range KnownDescriptors() {
		if descriptor.Kind == kind {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}

func DescriptorForLane(lane Lane) (Descriptor, bool) {
	for _, descriptor := range KnownDescriptors() {
		if descriptor.Lane() == lane {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}

type ProviderStatusRecord struct {
	Provider         Kind                `json:"provider"`
	Backend          sessionbackend.Kind `json:"backend"`
	Status           Status              `json:"status"`
	StatusLabel      string              `json:"status_label"`
	Executable       bool                `json:"executable"`
	IdentityBoundary IdentityBoundary    `json:"identity_boundary"`
	HostPlatform     HostPlatform        `json:"host_platform"`
	UserMode         UserMode            `json:"user_mode"`
	Message          string              `json:"message,omitempty"`
}

type GapRecord struct {
	ID       string `json:"id"`
	Provider Kind   `json:"provider,omitempty"`
	Status   Status `json:"status,omitempty"`
	Message  string `json:"message"`
	State    string `json:"state,omitempty"`
}

func NewGapRecord(provider Kind, status Status, id, message, state string) (GapRecord, error) {
	id = strings.TrimSpace(id)
	message = strings.TrimSpace(message)
	if id == "" {
		return GapRecord{}, fmt.Errorf("runtime provider gap id is required")
	}
	if message == "" {
		return GapRecord{}, fmt.Errorf("runtime provider gap message is required")
	}
	return GapRecord{
		ID:       id,
		Provider: provider,
		Status:   status,
		Message:  message,
		State:    strings.TrimSpace(state),
	}, nil
}

func MustGapRecord(provider Kind, status Status, id, message, state string) GapRecord {
	record, err := NewGapRecord(provider, status, id, message, state)
	if err != nil {
		panic(err)
	}
	return record
}

func RenderGap(record GapRecord) string {
	line := fmt.Sprintf("%s: %s", record.ID, record.Message)
	if record.State != "" {
		line += fmt.Sprintf(" (%s)", record.State)
	}
	return line
}

func RenderGaps(records []GapRecord) []string {
	if len(records) == 0 {
		return nil
	}
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, RenderGap(record))
	}
	return out
}

type HelperStrategy string

const (
	HelperNone           HelperStrategy = "none"
	HelperRoot           HelperStrategy = "root-helper"
	HelperRootlessUserNS HelperStrategy = "rootless-userns"
)

type ContainmentLevel string

const (
	ContainmentContractSandbox ContainmentLevel = "contract-sandbox"
	ContainmentSameUIDProcess  ContainmentLevel = "same-uid-process"
)

type NetworkAuthority string

const (
	NetworkDefaultEnforced NetworkAuthority = "default-enforced"
	NetworkNoneEnforced    NetworkAuthority = "none-enforced"
	NetworkAdvisory        NetworkAuthority = "advisory"
)

type CredentialAuthority string

const (
	CredentialNone           CredentialAuthority = "none"
	CredentialBroker         CredentialAuthority = "broker"
	CredentialEnvPassthrough CredentialAuthority = "env-passthrough"
)

type DockerAuthority string

const (
	DockerNone          DockerAuthority = "none"
	DockerPrivateDaemon DockerAuthority = "private-daemon"
	DockerHostSocket    DockerAuthority = "host-socket"
)

type Requirements struct {
	IdentityBoundary IdentityBoundary
	HelperStrategy   HelperStrategy
	Containment      ContainmentLevel
	Network          NetworkAuthority
	Credentials      CredentialAuthority
	Docker           DockerAuthority
}

type Capabilities struct {
	IdentityBoundary IdentityBoundary
	HelperStrategy   HelperStrategy
	Containment      ContainmentLevel
	Network          NetworkAuthority
	Credentials      CredentialAuthority
	Docker           DockerAuthority
}

type GapCode string

const (
	GapIdentityBoundaryDowngrade GapCode = "identity-boundary-downgrade"
	GapHelperStrategyDowngrade   GapCode = "helper-strategy-downgrade"
	GapContainmentDowngrade      GapCode = "containment-downgrade"
	GapNetworkDowngrade          GapCode = "network-downgrade"
	GapCredentialDowngrade       GapCode = "credential-downgrade"
	GapDockerAuthorityDowngrade  GapCode = "docker-authority-downgrade"
)

type CapabilityGap struct {
	Code      GapCode
	Field     string
	Required  string
	Available string
	Message   string
}

type DowngradeError struct {
	Gaps []CapabilityGap
}

func (e DowngradeError) Error() string {
	if len(e.Gaps) == 0 {
		return "runtime provider capability downgrade"
	}
	codes := make([]string, 0, len(e.Gaps))
	for _, gap := range e.Gaps {
		codes = append(codes, string(gap.Code))
	}
	sort.Strings(codes)
	return "runtime provider capability downgrade: " + strings.Join(codes, ", ")
}

func RequireCapabilities(required Requirements, available Capabilities) error {
	gaps := CapabilityGaps(required, available)
	if len(gaps) == 0 {
		return nil
	}
	return DowngradeError{Gaps: gaps}
}

func CapabilityGaps(required Requirements, available Capabilities) []CapabilityGap {
	var gaps []CapabilityGap
	if required.IdentityBoundary != "" && required.IdentityBoundary != available.IdentityBoundary {
		gaps = append(gaps, capabilityGap(GapIdentityBoundaryDowngrade, "identity_boundary", string(required.IdentityBoundary), string(available.IdentityBoundary)))
	}
	if required.HelperStrategy != "" && required.HelperStrategy != available.HelperStrategy {
		gaps = append(gaps, capabilityGap(GapHelperStrategyDowngrade, "helper_strategy", string(required.HelperStrategy), string(available.HelperStrategy)))
	}
	if required.Containment != "" && required.Containment != available.Containment {
		gaps = append(gaps, capabilityGap(GapContainmentDowngrade, "containment", string(required.Containment), string(available.Containment)))
	}
	if required.Network != "" && required.Network != available.Network {
		gaps = append(gaps, capabilityGap(GapNetworkDowngrade, "network", string(required.Network), string(available.Network)))
	}
	if required.Credentials != "" && required.Credentials != available.Credentials {
		gaps = append(gaps, capabilityGap(GapCredentialDowngrade, "credentials", string(required.Credentials), string(available.Credentials)))
	}
	if required.Docker != "" && required.Docker != available.Docker {
		gaps = append(gaps, capabilityGap(GapDockerAuthorityDowngrade, "docker", string(required.Docker), string(available.Docker)))
	}
	return gaps
}

func capabilityGap(code GapCode, field, required, available string) CapabilityGap {
	if available == "" {
		available = "unspecified"
	}
	return CapabilityGap{
		Code:      code,
		Field:     field,
		Required:  required,
		Available: available,
		Message:   fmt.Sprintf("%s requires %s but provider offers %s", field, required, available),
	}
}

type PrepareRequest struct {
	target    string
	sessionID string
	contract  containment.Contract
}

func NewPrepareRequest(target, sessionID string, contract containment.Contract) (PrepareRequest, error) {
	if strings.TrimSpace(target) == "" {
		return PrepareRequest{}, fmt.Errorf("runtime prepare target is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return PrepareRequest{}, fmt.Errorf("runtime prepare session id is required")
	}
	if err := contract.Validate(); err != nil {
		return PrepareRequest{}, fmt.Errorf("runtime prepare contract: %w", err)
	}
	return PrepareRequest{target: strings.TrimSpace(target), sessionID: strings.TrimSpace(sessionID), contract: contract}, nil
}

func (r PrepareRequest) Target() string {
	return r.target
}

func (r PrepareRequest) SessionID() string {
	return r.sessionID
}

func (r PrepareRequest) Contract() containment.Contract {
	return r.contract
}

type AdmissionObligation string

const (
	ObligationNoDowngrade                  AdmissionObligation = "no-downgrade"
	ObligationVerifyIdentityBoundary       AdmissionObligation = "verify-identity-boundary"
	ObligationEnforceContainment           AdmissionObligation = "enforce-containment"
	ObligationEnforceNetwork               AdmissionObligation = "enforce-network"
	ObligationEmitMetadataAfterContainment AdmissionObligation = "emit-metadata-after-containment"
	ObligationPreserveRawStreams           AdmissionObligation = "preserve-raw-streams"
)

type CleanupObligation string

const (
	CleanupGeneratedPolicy      CleanupObligation = "remove-generated-policy"
	CleanupGeneratedCredentials CleanupObligation = "remove-generated-credentials"
	CleanupRuntimeContainer     CleanupObligation = "remove-runtime-container"
	CleanupMetadataSidecar      CleanupObligation = "remove-metadata-sidecar"
	CleanupHelperBroker         CleanupObligation = "stop-helper-broker"
)

type CleanupPlan struct {
	obligations []CleanupObligation
}

func NewCleanupPlan(obligations ...CleanupObligation) (CleanupPlan, error) {
	copied := append([]CleanupObligation(nil), obligations...)
	for _, obligation := range copied {
		if obligation == "" {
			return CleanupPlan{}, fmt.Errorf("cleanup obligation is required")
		}
	}
	return CleanupPlan{obligations: copied}, nil
}

func (p CleanupPlan) Obligations() []CleanupObligation {
	return append([]CleanupObligation(nil), p.obligations...)
}

type Admission struct {
	descriptor  Descriptor
	launch      sessionbackend.PreparedLaunch
	obligations []AdmissionObligation
	cleanup     CleanupPlan
	admittedAt  time.Time
}

func NewAdmission(descriptor Descriptor, launch sessionbackend.PreparedLaunch, obligations []AdmissionObligation, cleanup CleanupPlan, admittedAt time.Time) (Admission, error) {
	if err := descriptor.Validate(); err != nil {
		return Admission{}, err
	}
	if launch.ArtifactKind() == "" {
		return Admission{}, fmt.Errorf("admission requires a prepared launch")
	}
	if len(obligations) == 0 {
		return Admission{}, fmt.Errorf("admission obligations are required")
	}
	copied := append([]AdmissionObligation(nil), obligations...)
	for _, obligation := range copied {
		if obligation == "" {
			return Admission{}, fmt.Errorf("admission obligation is required")
		}
	}
	if admittedAt.IsZero() {
		admittedAt = time.Now().UTC()
	}
	return Admission{
		descriptor:  descriptor,
		launch:      launch,
		obligations: copied,
		cleanup:     cleanup,
		admittedAt:  admittedAt.UTC(),
	}, nil
}

func (a Admission) Descriptor() Descriptor {
	return a.descriptor
}

func (a Admission) Launch() sessionbackend.PreparedLaunch {
	return a.launch
}

func (a Admission) Obligations() []AdmissionObligation {
	return append([]AdmissionObligation(nil), a.obligations...)
}

func (a Admission) Cleanup() CleanupPlan {
	return a.cleanup
}

func (a Admission) AdmittedAt() time.Time {
	return a.admittedAt
}

type LaunchHandle struct {
	provider Kind
	token    string
}

func NewLaunchHandle(provider Kind, token string) (LaunchHandle, error) {
	if provider == "" {
		return LaunchHandle{}, fmt.Errorf("launch handle provider is required")
	}
	if strings.TrimSpace(token) == "" {
		return LaunchHandle{}, fmt.Errorf("launch handle token is required")
	}
	return LaunchHandle{provider: provider, token: strings.TrimSpace(token)}, nil
}

func (h LaunchHandle) Provider() Kind {
	return h.provider
}

func (h LaunchHandle) Token() string {
	return h.token
}

type ResultPhase string

const (
	ResultPlanned   ResultPhase = "planned"
	ResultLaunched  ResultPhase = "launched"
	ResultContained ResultPhase = "contained"
	ResultExited    ResultPhase = "exited"
	ResultFailed    ResultPhase = "failed"
	ResultCancelled ResultPhase = "cancelled"
)

type ResultClassification string

const (
	ResultPublic          ResultClassification = "public"
	ResultOperatorPrivate ResultClassification = "operator-private"
	ResultSecretAdjacent  ResultClassification = "secret-adjacent"
)

type Result struct {
	Phase          ResultPhase
	Classification ResultClassification
	ExitCode       int
	Message        string
	Metadata       map[string]string
}

func NewResult(phase ResultPhase, classification ResultClassification, exitCode int, message string, metadata map[string]string) (Result, error) {
	switch phase {
	case ResultPlanned, ResultLaunched, ResultContained, ResultExited, ResultFailed, ResultCancelled:
	case "":
		return Result{}, fmt.Errorf("runtime result phase is required")
	default:
		return Result{}, fmt.Errorf("runtime result phase %q is unsupported", phase)
	}
	switch classification {
	case ResultPublic, ResultOperatorPrivate, ResultSecretAdjacent:
	case "":
		return Result{}, fmt.Errorf("runtime result classification is required")
	default:
		return Result{}, fmt.Errorf("runtime result classification %q is unsupported", classification)
	}
	return Result{
		Phase:          phase,
		Classification: classification,
		ExitCode:       exitCode,
		Message:        message,
		Metadata:       copyStringMap(metadata),
	}, nil
}

type CleanupResult struct {
	Completed []CleanupObligation
	Failed    map[CleanupObligation]string
}

func NewCleanupResult(completed []CleanupObligation, failed map[CleanupObligation]string) CleanupResult {
	return CleanupResult{
		Completed: append([]CleanupObligation(nil), completed...),
		Failed:    copyCleanupFailures(failed),
	}
}

type Provider interface {
	Descriptor() Descriptor
	Prepare(context.Context, PrepareRequest) (sessionbackend.PreparedLaunch, error)
	Admit(context.Context, sessionbackend.PreparedLaunch) (Admission, error)
	Launch(context.Context, Admission) (LaunchHandle, error)
	Monitor(context.Context, LaunchHandle) (Result, error)
	Cleanup(context.Context, CleanupPlan) (CleanupResult, error)
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func copyCleanupFailures(values map[CleanupObligation]string) map[CleanupObligation]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[CleanupObligation]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
