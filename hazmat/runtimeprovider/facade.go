// Package runtimeprovider defines the reusable boundary between Hazmat's
// side-effect-free runtime contracts and effectful runtime implementations.
package runtimeprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hazmat/containment"
	"hazmat/sessionbackend"
)

// Kind names a concrete runtime provider lane, not just a backend family.
type Kind string

const (
	KindDarwinNative      Kind = "darwin-native"
	KindDockerSandbox     Kind = "docker-sandbox"
	KindAppleContainer    Kind = "apple-container"
	KindLinuxCurrentUser  Kind = "linux-current-user"
	KindLinuxAgentUser    Kind = "linux-agent-user"
	KindRemoteEnvelope    Kind = "remote-envelope"
	KindUnsupportedNative Kind = "unsupported-native"
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

type Descriptor struct {
	Kind             Kind
	Backend          sessionbackend.Kind
	Status           Status
	IdentityBoundary IdentityBoundary
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
	return nil
}

func KnownDescriptors() []Descriptor {
	return []Descriptor{
		{Kind: KindDarwinNative, Backend: sessionbackend.KindDarwinNative, Status: StatusSupported, IdentityBoundary: IdentityMacOSAgentUser},
		{Kind: KindDockerSandbox, Backend: sessionbackend.KindDockerSandbox, Status: StatusSupported, IdentityBoundary: IdentityContainerUser},
		{Kind: KindAppleContainer, Backend: sessionbackend.KindAppleContainer, Status: StatusExperimental, IdentityBoundary: IdentityContainerUser},
		{Kind: KindLinuxCurrentUser, Backend: sessionbackend.KindLinuxNative, Status: StatusExperimental, IdentityBoundary: IdentityCurrentUser},
		{Kind: KindLinuxAgentUser, Backend: sessionbackend.KindLinuxNative, Status: StatusSetupRequired, IdentityBoundary: IdentityLinuxAgentUser},
		{Kind: KindRemoteEnvelope, Backend: sessionbackend.KindRemoteEnvelope, Status: StatusPlanOnly, IdentityBoundary: IdentityRemoteWorker},
		{Kind: KindUnsupportedNative, Backend: sessionbackend.KindUnsupportedNative, Status: StatusUnsupported, IdentityBoundary: IdentityNone},
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
