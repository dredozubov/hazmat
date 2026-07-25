// Package planescapeprovider is Hazmat's strict client-side model of the
// Planescape shared-provider v1 contract. It owns framing-independent DTO
// validation and lifecycle bindings only; containment policy and evidence
// authority remain in the Planescape kernel and provider.
package planescapeprovider

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ProtocolVersionV1 = "v1"
	MaxRecordBytes    = 64 * 1024

	maxIdentifierBytes = 256
	maxNonceBytes      = 256
	maxCapabilities    = 64
	maxResources       = 64
)

// Profile is the closed provider profile set published by
// planescape.provider.v1.
type Profile string

const (
	ProfilePortable   Profile = "portable.v1"
	ProfileStockLinux Profile = "stock_linux.v1"
)

func (p Profile) valid() bool {
	switch p {
	case ProfilePortable, ProfileStockLinux:
		return true
	default:
		return false
	}
}

// Satisfies reports whether an advertised profile can satisfy a required
// profile without negotiation or downgrade. stock_linux.v1 explicitly extends
// portable.v1; no other relation is implicit.
func (p Profile) Satisfies(required Profile) bool {
	if !p.valid() || !required.valid() {
		return false
	}
	return p == required || (p == ProfileStockLinux && required == ProfilePortable)
}

// Capability is a closed logical capability, never a host capability or path.
type Capability string

const (
	CapabilityArtifactRead   Capability = "artifact_read"
	CapabilityApplyPatch     Capability = "apply_patch"
	CapabilityWorkspaceRead  Capability = "workspace_read"
	CapabilityWorkspaceWrite Capability = "workspace_write"
	CapabilityToolExecute    Capability = "tool_execute"
)

func (c Capability) valid() bool {
	switch c {
	case CapabilityArtifactRead, CapabilityApplyPatch, CapabilityWorkspaceRead, CapabilityWorkspaceWrite, CapabilityToolExecute:
		return true
	default:
		return false
	}
}

// ResourceDimension is a closed logical resource dimension.
type ResourceDimension string

const (
	ResourceCPUTime          ResourceDimension = "cpu_time"
	ResourceMemoryBytes      ResourceDimension = "memory_bytes"
	ResourceOpenFiles        ResourceDimension = "open_files"
	ResourceProcessCount     ResourceDimension = "process_count"
	ResourceWorkspaceBytes   ResourceDimension = "workspace_bytes"
	ResourceWorkspaceEntries ResourceDimension = "workspace_entries"
)

func (r ResourceDimension) valid() bool {
	switch r {
	case ResourceCPUTime, ResourceMemoryBytes, ResourceOpenFiles, ResourceProcessCount, ResourceWorkspaceBytes, ResourceWorkspaceEntries:
		return true
	default:
		return false
	}
}

// OperationKind is a closed provider operation kind.
type OperationKind string

const (
	OperationAgentStart OperationKind = "agent_start"
	OperationTool       OperationKind = "tool"
	OperationWorkspace  OperationKind = "workspace"
	OperationPause      OperationKind = "pause"
	OperationCancel     OperationKind = "cancel"
	OperationFreeze     OperationKind = "freeze"
	OperationCloseout   OperationKind = "closeout"
)

func (k OperationKind) valid() bool {
	switch k {
	case OperationAgentStart, OperationTool, OperationWorkspace, OperationPause, OperationCancel, OperationFreeze, OperationCloseout:
		return true
	default:
		return false
	}
}

// ResultKind is a closed operation-result disposition.
type ResultKind string

const (
	ResultAccepted    ResultKind = "accepted"
	ResultCompleted   ResultKind = "completed"
	ResultCancelled   ResultKind = "cancelled"
	ResultFailed      ResultKind = "failed"
	ResultUnavailable ResultKind = "unavailable"
)

func (k ResultKind) valid() bool {
	switch k {
	case ResultAccepted, ResultCompleted, ResultCancelled, ResultFailed, ResultUnavailable:
		return true
	default:
		return false
	}
}

// TerminalOutcome is a closed terminal provider outcome.
type TerminalOutcome string

const (
	OutcomeSucceeded   TerminalOutcome = "succeeded"
	OutcomeFailed      TerminalOutcome = "failed"
	OutcomeNeedsReview TerminalOutcome = "needs_review"
	OutcomeCancelled   TerminalOutcome = "cancelled"
)

func (o TerminalOutcome) valid() bool {
	switch o {
	case OutcomeSucceeded, OutcomeFailed, OutcomeNeedsReview, OutcomeCancelled:
		return true
	default:
		return false
	}
}

// ProviderErrorCode is the provider's closed error taxonomy.
type ProviderErrorCode string

const (
	ProviderErrorInvalid         ProviderErrorCode = "invalid"
	ProviderErrorUnsupported     ProviderErrorCode = "unsupported"
	ProviderErrorUnavailable     ProviderErrorCode = "unavailable"
	ProviderErrorConflict        ProviderErrorCode = "conflict"
	ProviderErrorStaleEpoch      ProviderErrorCode = "stale_epoch"
	ProviderErrorProfileMismatch ProviderErrorCode = "profile_mismatch"
	ProviderErrorReplayConflict  ProviderErrorCode = "replay_conflict"
)

func (c ProviderErrorCode) valid() bool {
	switch c {
	case ProviderErrorInvalid, ProviderErrorUnsupported, ProviderErrorUnavailable, ProviderErrorConflict, ProviderErrorStaleEpoch, ProviderErrorProfileMismatch, ProviderErrorReplayConflict:
		return true
	default:
		return false
	}
}

// ErrorClass is Hazmat's stable, fail-closed public failure classification.
type ErrorClass string

const (
	ErrorInvalid     ErrorClass = "invalid"
	ErrorUnsupported ErrorClass = "unsupported"
	ErrorUnavailable ErrorClass = "unavailable"
	ErrorConflict    ErrorClass = "conflict"
)

// Error carries no raw provider diagnostic bytes. Use Class to select stable
// product behavior rather than parsing Error strings.
type Error struct {
	class ErrorClass
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return "planescape provider: " + string(e.class)
}

func (e *Error) Class() ErrorClass {
	if e == nil {
		return ""
	}
	return e.class
}

func clientError(class ErrorClass) error {
	return &Error{class: class}
}

// Identifier is a bounded, canonical protocol identifier. Record constructors
// assign it to a specific role, preventing callers from putting an unvalidated
// raw string into a session, operation, or provider binding.
type Identifier struct {
	value string
}

func NewIdentifier(value string) (Identifier, error) {
	if value == "" || value != strings.TrimSpace(value) {
		return Identifier{}, fmt.Errorf("planescapeprovider: identifier is required")
	}
	if !utf8.ValidString(value) || len(value) > maxIdentifierBytes {
		return Identifier{}, fmt.Errorf("planescapeprovider: identifier is invalid")
	}
	if strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return Identifier{}, fmt.Errorf("planescapeprovider: identifier contains control characters")
	}
	return Identifier{value: value}, nil
}

func (v Identifier) String() string { return v.value }

func (v Identifier) valid() bool {
	_, err := NewIdentifier(v.value)
	return err == nil
}

// Nonce is a bounded replay binding.
type Nonce struct {
	value string
}

func NewNonce(value string) (Nonce, error) {
	if value == "" || value != strings.TrimSpace(value) {
		return Nonce{}, fmt.Errorf("planescapeprovider: nonce is required")
	}
	if !utf8.ValidString(value) || len(value) > maxNonceBytes {
		return Nonce{}, fmt.Errorf("planescapeprovider: nonce is invalid")
	}
	if strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return Nonce{}, fmt.Errorf("planescapeprovider: nonce contains control characters")
	}
	return Nonce{value: value}, nil
}

func (v Nonce) String() string { return v.value }

func (v Nonce) valid() bool {
	_, err := NewNonce(v.value)
	return err == nil
}

// Fingerprint is a lowercase sha256 fingerprint. It is a commitment supplied
// by the kernel or provider, never a Go-side policy or evidence derivation.
type Fingerprint struct {
	value string
}

func ParseFingerprint(value string) (Fingerprint, error) {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") || value != strings.ToLower(value) {
		return Fingerprint{}, fmt.Errorf("planescapeprovider: fingerprint must be lowercase sha256")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:")); err != nil {
		return Fingerprint{}, fmt.Errorf("planescapeprovider: fingerprint must be lowercase sha256")
	}
	return Fingerprint{value: value}, nil
}

func (v Fingerprint) String() string { return v.value }

func (v Fingerprint) valid() bool {
	_, err := ParseFingerprint(v.value)
	return err == nil
}

// ProviderEpoch and OperationSequence reject zero because both establish
// authority and replay order.
type ProviderEpoch uint64

func NewProviderEpoch(value uint64) (ProviderEpoch, error) {
	if value == 0 {
		return 0, fmt.Errorf("planescapeprovider: provider epoch must be positive")
	}
	return ProviderEpoch(value), nil
}

func (v ProviderEpoch) Uint64() uint64 { return uint64(v) }

// BackendIdentityBinding pins one provider lifecycle to the exact protected
// containment backend that authenticated the endpoint. IdentitySHA256 commits
// the backend executable, environment, profile, attestor, and broker epoch.
type BackendIdentityBinding struct {
	identitySHA256 Fingerprint
	epoch          ProviderEpoch
}

type BackendIdentityBindingInput struct {
	IdentitySHA256 string
	ProviderEpoch  uint64
}

func NewBackendIdentityBinding(
	input BackendIdentityBindingInput,
) (BackendIdentityBinding, error) {
	identitySHA256, err := ParseFingerprint(input.IdentitySHA256)
	if err != nil {
		return BackendIdentityBinding{}, err
	}
	epoch, err := NewProviderEpoch(input.ProviderEpoch)
	if err != nil {
		return BackendIdentityBinding{}, err
	}
	return BackendIdentityBinding{
		identitySHA256: identitySHA256,
		epoch:          epoch,
	}, nil
}

func (v BackendIdentityBinding) IdentitySHA256() Fingerprint  { return v.identitySHA256 }
func (v BackendIdentityBinding) ProviderEpoch() ProviderEpoch { return v.epoch }

func (v BackendIdentityBinding) valid() bool {
	return v.identitySHA256.valid() && v.epoch != 0
}

type OperationSequence uint64

func NewOperationSequence(value uint64) (OperationSequence, error) {
	if value == 0 {
		return 0, fmt.Errorf("planescapeprovider: operation sequence must be positive")
	}
	return OperationSequence(value), nil
}

func (v OperationSequence) Uint64() uint64 { return uint64(v) }

func normalizeCapabilities(values []Capability) ([]Capability, error) {
	if len(values) > maxCapabilities {
		return nil, fmt.Errorf("planescapeprovider: too many capabilities")
	}
	seen := make(map[Capability]struct{}, len(values))
	out := make([]Capability, 0, len(values))
	for _, value := range values {
		if !value.valid() {
			return nil, fmt.Errorf("planescapeprovider: unsupported capability")
		}
		if _, ok := seen[value]; ok {
			return nil, fmt.Errorf("planescapeprovider: duplicate capability")
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func normalizeResources(values []ResourceDimension) ([]ResourceDimension, error) {
	if len(values) > maxResources {
		return nil, fmt.Errorf("planescapeprovider: too many resource dimensions")
	}
	seen := make(map[ResourceDimension]struct{}, len(values))
	out := make([]ResourceDimension, 0, len(values))
	for _, value := range values {
		if !value.valid() {
			return nil, fmt.Errorf("planescapeprovider: unsupported resource dimension")
		}
		if _, ok := seen[value]; ok {
			return nil, fmt.Errorf("planescapeprovider: duplicate resource dimension")
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func containsCapabilities(advertised, required []Capability) bool {
	available := make(map[Capability]struct{}, len(advertised))
	for _, value := range advertised {
		available[value] = struct{}{}
	}
	for _, value := range required {
		if _, ok := available[value]; !ok {
			return false
		}
	}
	return true
}

func containsResources(advertised, required []ResourceDimension) bool {
	available := make(map[ResourceDimension]struct{}, len(advertised))
	for _, value := range advertised {
		available[value] = struct{}{}
	}
	for _, value := range required {
		if _, ok := available[value]; !ok {
			return false
		}
	}
	return true
}

// ProviderCapabilities is the discovered, non-reserving provider declaration.
type ProviderCapabilities struct {
	providerID         Identifier
	epoch              ProviderEpoch
	profile            Profile
	capabilityHash     Fingerprint
	canonicalHash      Fingerprint
	capabilities       []Capability
	resourceDimensions []ResourceDimension
}

type ProviderCapabilitiesInput struct {
	ProviderID         string
	ProviderEpoch      uint64
	Profile            Profile
	CapabilityHash     string
	CanonicalHash      string
	Capabilities       []Capability
	ResourceDimensions []ResourceDimension
}

func NewProviderCapabilities(input ProviderCapabilitiesInput) (ProviderCapabilities, error) {
	providerID, err := NewIdentifier(input.ProviderID)
	if err != nil {
		return ProviderCapabilities{}, fmt.Errorf("planescapeprovider: provider capabilities: %w", err)
	}
	epoch, err := NewProviderEpoch(input.ProviderEpoch)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	if !input.Profile.valid() {
		return ProviderCapabilities{}, fmt.Errorf("planescapeprovider: unsupported provider profile")
	}
	capabilityHash, err := ParseFingerprint(input.CapabilityHash)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	capabilities, err := normalizeCapabilities(input.Capabilities)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	resources, err := normalizeResources(input.ResourceDimensions)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	return ProviderCapabilities{
		providerID:         providerID,
		epoch:              epoch,
		profile:            input.Profile,
		capabilityHash:     capabilityHash,
		canonicalHash:      canonicalHash,
		capabilities:       capabilities,
		resourceDimensions: resources,
	}, nil
}

func (v ProviderCapabilities) ProviderID() Identifier       { return v.providerID }
func (v ProviderCapabilities) ProviderEpoch() ProviderEpoch { return v.epoch }
func (v ProviderCapabilities) Profile() Profile             { return v.profile }
func (v ProviderCapabilities) CapabilityHash() Fingerprint  { return v.capabilityHash }
func (v ProviderCapabilities) CanonicalHash() Fingerprint   { return v.canonicalHash }
func (v ProviderCapabilities) Capabilities() []Capability {
	return append([]Capability(nil), v.capabilities...)
}
func (v ProviderCapabilities) ResourceDimensions() []ResourceDimension {
	return append([]ResourceDimension(nil), v.resourceDimensions...)
}

func (v ProviderCapabilities) valid() bool {
	if !v.providerID.valid() || v.epoch == 0 || !v.profile.valid() || !v.capabilityHash.valid() || !v.canonicalHash.valid() {
		return false
	}
	_, capabilityErr := normalizeCapabilities(v.capabilities)
	_, resourceErr := normalizeResources(v.resourceDimensions)
	return capabilityErr == nil && resourceErr == nil
}

// ExecutionRequirement is an already kernel-bound admission request. The Go
// client validates its bounded bindings but does not derive its canonical hash.
type ExecutionRequirement struct {
	requirementID              Identifier
	controllerAttemptID        Identifier
	authorityHash              Fingerprint
	requiredCapabilities       []Capability
	requiredResourceDimensions []ResourceDimension
	evidenceProfileHash        Fingerprint
	canonicalHash              Fingerprint
}

type ExecutionRequirementInput struct {
	RequirementID              string
	ControllerAttemptID        string
	AuthorityHash              string
	RequiredCapabilities       []Capability
	RequiredResourceDimensions []ResourceDimension
	EvidenceProfileHash        string
	CanonicalHash              string
}

func NewExecutionRequirement(input ExecutionRequirementInput) (ExecutionRequirement, error) {
	requirementID, err := NewIdentifier(input.RequirementID)
	if err != nil {
		return ExecutionRequirement{}, err
	}
	attemptID, err := NewIdentifier(input.ControllerAttemptID)
	if err != nil {
		return ExecutionRequirement{}, err
	}
	authorityHash, err := ParseFingerprint(input.AuthorityHash)
	if err != nil {
		return ExecutionRequirement{}, err
	}
	capabilities, err := normalizeCapabilities(input.RequiredCapabilities)
	if err != nil {
		return ExecutionRequirement{}, err
	}
	resources, err := normalizeResources(input.RequiredResourceDimensions)
	if err != nil {
		return ExecutionRequirement{}, err
	}
	evidenceProfileHash, err := ParseFingerprint(input.EvidenceProfileHash)
	if err != nil {
		return ExecutionRequirement{}, err
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return ExecutionRequirement{}, err
	}
	return ExecutionRequirement{
		requirementID:              requirementID,
		controllerAttemptID:        attemptID,
		authorityHash:              authorityHash,
		requiredCapabilities:       capabilities,
		requiredResourceDimensions: resources,
		evidenceProfileHash:        evidenceProfileHash,
		canonicalHash:              canonicalHash,
	}, nil
}

func (v ExecutionRequirement) RequirementID() Identifier       { return v.requirementID }
func (v ExecutionRequirement) ControllerAttemptID() Identifier { return v.controllerAttemptID }
func (v ExecutionRequirement) AuthorityHash() Fingerprint      { return v.authorityHash }
func (v ExecutionRequirement) RequiredCapabilities() []Capability {
	return append([]Capability(nil), v.requiredCapabilities...)
}
func (v ExecutionRequirement) RequiredResourceDimensions() []ResourceDimension {
	return append([]ResourceDimension(nil), v.requiredResourceDimensions...)
}
func (v ExecutionRequirement) EvidenceProfileHash() Fingerprint { return v.evidenceProfileHash }
func (v ExecutionRequirement) CanonicalHash() Fingerprint       { return v.canonicalHash }

func (v ExecutionRequirement) valid() bool {
	if !v.requirementID.valid() || !v.controllerAttemptID.valid() || !v.authorityHash.valid() || !v.evidenceProfileHash.valid() || !v.canonicalHash.valid() {
		return false
	}
	_, capabilityErr := normalizeCapabilities(v.requiredCapabilities)
	_, resourceErr := normalizeResources(v.requiredResourceDimensions)
	return capabilityErr == nil && resourceErr == nil
}

// AdmissionInput carries one validated Rust-produced containment plan. Hazmat
// intentionally has no constructor that can derive a plan from requirement or
// profile inputs.
type AdmissionInput struct {
	plan CompiledContainmentPlan
}

func NewAdmissionInput(plan CompiledContainmentPlan) (AdmissionInput, error) {
	if !plan.valid() {
		return AdmissionInput{}, fmt.Errorf("planescapeprovider: invalid compiled containment plan")
	}
	return AdmissionInput{plan: plan}, nil
}

func (v AdmissionInput) Plan() CompiledContainmentPlan { return v.plan }

func (v AdmissionInput) valid() bool { return v.plan.valid() }

// SessionAdmission is the provider's exact binding of a session to one
// discovered identity, epoch, requirement, and compiled plan.
type SessionAdmission struct {
	sessionID             Identifier
	providerID            Identifier
	epoch                 ProviderEpoch
	requirementHash       Fingerprint
	compiledPlanHash      Fingerprint
	sessionCapabilityHash Fingerprint
	expiresAt             time.Time
	canonicalHash         Fingerprint
}

type SessionAdmissionInput struct {
	SessionID             string
	ProviderID            string
	ProviderEpoch         uint64
	RequirementHash       string
	CompiledPlanHash      string
	SessionCapabilityHash string
	ExpiresAt             time.Time
	CanonicalHash         string
}

func NewSessionAdmission(input SessionAdmissionInput) (SessionAdmission, error) {
	sessionID, err := NewIdentifier(input.SessionID)
	if err != nil {
		return SessionAdmission{}, err
	}
	providerID, err := NewIdentifier(input.ProviderID)
	if err != nil {
		return SessionAdmission{}, err
	}
	epoch, err := NewProviderEpoch(input.ProviderEpoch)
	if err != nil {
		return SessionAdmission{}, err
	}
	requirementHash, err := ParseFingerprint(input.RequirementHash)
	if err != nil {
		return SessionAdmission{}, err
	}
	planHash, err := ParseFingerprint(input.CompiledPlanHash)
	if err != nil {
		return SessionAdmission{}, err
	}
	capabilityHash, err := ParseFingerprint(input.SessionCapabilityHash)
	if err != nil {
		return SessionAdmission{}, err
	}
	if input.ExpiresAt.IsZero() || input.ExpiresAt.UnixMilli() <= 0 {
		return SessionAdmission{}, fmt.Errorf("planescapeprovider: session expiration is required")
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return SessionAdmission{}, err
	}
	return SessionAdmission{
		sessionID:             sessionID,
		providerID:            providerID,
		epoch:                 epoch,
		requirementHash:       requirementHash,
		compiledPlanHash:      planHash,
		sessionCapabilityHash: capabilityHash,
		expiresAt:             input.ExpiresAt.UTC(),
		canonicalHash:         canonicalHash,
	}, nil
}

func (v SessionAdmission) SessionID() Identifier              { return v.sessionID }
func (v SessionAdmission) ProviderID() Identifier             { return v.providerID }
func (v SessionAdmission) ProviderEpoch() ProviderEpoch       { return v.epoch }
func (v SessionAdmission) RequirementHash() Fingerprint       { return v.requirementHash }
func (v SessionAdmission) CompiledPlanHash() Fingerprint      { return v.compiledPlanHash }
func (v SessionAdmission) SessionCapabilityHash() Fingerprint { return v.sessionCapabilityHash }
func (v SessionAdmission) ExpiresAt() time.Time               { return v.expiresAt }
func (v SessionAdmission) CanonicalHash() Fingerprint         { return v.canonicalHash }

func (v SessionAdmission) valid() bool {
	return v.sessionID.valid() && v.providerID.valid() && v.epoch != 0 && v.requirementHash.valid() && v.compiledPlanHash.valid() && v.sessionCapabilityHash.valid() && !v.expiresAt.IsZero() && v.canonicalHash.valid()
}

// OperationInput is the product's bounded, opaque operation request. Session,
// sequence, and plan bindings are assigned by Session, not supplied by callers.
type OperationInput struct {
	operationID Identifier
	kind        OperationKind
	nonce       Nonce
	payloadHash Fingerprint
}

type OperationInputValues struct {
	OperationID string
	Kind        OperationKind
	Nonce       string
	PayloadHash string
}

func NewOperationInput(input OperationInputValues) (OperationInput, error) {
	operationID, err := NewIdentifier(input.OperationID)
	if err != nil {
		return OperationInput{}, err
	}
	if !input.Kind.valid() {
		return OperationInput{}, fmt.Errorf("planescapeprovider: unsupported operation kind")
	}
	nonce, err := NewNonce(input.Nonce)
	if err != nil {
		return OperationInput{}, err
	}
	payloadHash, err := ParseFingerprint(input.PayloadHash)
	if err != nil {
		return OperationInput{}, err
	}
	return OperationInput{
		operationID: operationID,
		kind:        input.Kind,
		nonce:       nonce,
		payloadHash: payloadHash,
	}, nil
}

func (v OperationInput) OperationID() Identifier  { return v.operationID }
func (v OperationInput) Kind() OperationKind      { return v.kind }
func (v OperationInput) Nonce() Nonce             { return v.nonce }
func (v OperationInput) PayloadHash() Fingerprint { return v.payloadHash }

func (v OperationInput) valid() bool {
	return v.operationID.valid() && v.kind.valid() && v.nonce.valid() && v.payloadHash.valid()
}

// AgentOperation is the fully bound record sent to a provider.
type AgentOperation struct {
	sessionID     Identifier
	operationID   Identifier
	sequence      OperationSequence
	kind          OperationKind
	planHash      Fingerprint
	nonce         Nonce
	payloadHash   Fingerprint
	canonicalHash Fingerprint
}

func newAgentOperation(sessionID Identifier, sequence OperationSequence, planHash Fingerprint, input OperationInput) (AgentOperation, error) {
	if !sessionID.valid() || sequence == 0 || !planHash.valid() || !input.valid() {
		return AgentOperation{}, fmt.Errorf("planescapeprovider: invalid agent operation")
	}
	dto := providerV1OperationDTO{
		Schema:            providerV1SchemaOperation,
		SessionID:         sessionID.String(),
		OperationID:       input.operationID.String(),
		OperationSequence: sequence.Uint64(),
		OperationKind:     string(input.kind),
		PlanHash:          planHash.String(),
		Nonce:             input.nonce.String(),
		PayloadHash:       input.payloadHash.String(),
	}
	preimage, err := dto.canonicalPreimage()
	if err != nil {
		return AgentOperation{}, fmt.Errorf("planescapeprovider: invalid agent operation")
	}
	canonicalHash, err := ParseFingerprint(providerV1CanonicalHash(preimage))
	if err != nil {
		return AgentOperation{}, fmt.Errorf("planescapeprovider: invalid agent operation")
	}
	return AgentOperation{
		sessionID:     sessionID,
		operationID:   input.operationID,
		sequence:      sequence,
		kind:          input.kind,
		planHash:      planHash,
		nonce:         input.nonce,
		payloadHash:   input.payloadHash,
		canonicalHash: canonicalHash,
	}, nil
}

func (v AgentOperation) SessionID() Identifier       { return v.sessionID }
func (v AgentOperation) OperationID() Identifier     { return v.operationID }
func (v AgentOperation) Sequence() OperationSequence { return v.sequence }
func (v AgentOperation) Kind() OperationKind         { return v.kind }
func (v AgentOperation) PlanHash() Fingerprint       { return v.planHash }
func (v AgentOperation) Nonce() Nonce                { return v.nonce }
func (v AgentOperation) PayloadHash() Fingerprint    { return v.payloadHash }
func (v AgentOperation) CanonicalHash() Fingerprint  { return v.canonicalHash }

func (v AgentOperation) valid() bool {
	return v.sessionID.valid() && v.operationID.valid() && v.sequence != 0 && v.kind.valid() && v.planHash.valid() && v.nonce.valid() && v.payloadHash.valid() && v.canonicalHash.valid()
}

type responseKind string

const (
	responseOperationResult responseKind = "operation_result"
	responseQuiescence      responseKind = "quiescence"
	responseCloseout        responseKind = "closeout"
)

// OperationResponse is sealed so a transport cannot hand the client an
// arbitrary response variant. Constructors below validate every record.
type OperationResponse interface {
	operationResponse()
	responseKind() responseKind
	valid() bool
}

// OperationResult is the observed provider outcome for an agent operation.
type OperationResult struct {
	sessionID     Identifier
	operationID   Identifier
	sequence      OperationSequence
	resultKind    ResultKind
	artifactHash  Fingerprint
	evidenceHash  Fingerprint
	canonicalHash Fingerprint
}

type OperationResultInput struct {
	SessionID     string
	OperationID   string
	Sequence      uint64
	ResultKind    ResultKind
	ArtifactHash  string
	EvidenceHash  string
	CanonicalHash string
}

func NewOperationResult(input OperationResultInput) (OperationResult, error) {
	sessionID, err := NewIdentifier(input.SessionID)
	if err != nil {
		return OperationResult{}, err
	}
	operationID, err := NewIdentifier(input.OperationID)
	if err != nil {
		return OperationResult{}, err
	}
	sequence, err := NewOperationSequence(input.Sequence)
	if err != nil {
		return OperationResult{}, err
	}
	if !input.ResultKind.valid() {
		return OperationResult{}, fmt.Errorf("planescapeprovider: unsupported result kind")
	}
	artifactHash, err := ParseFingerprint(input.ArtifactHash)
	if err != nil {
		return OperationResult{}, err
	}
	evidenceHash, err := ParseFingerprint(input.EvidenceHash)
	if err != nil {
		return OperationResult{}, err
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{sessionID: sessionID, operationID: operationID, sequence: sequence, resultKind: input.ResultKind, artifactHash: artifactHash, evidenceHash: evidenceHash, canonicalHash: canonicalHash}, nil
}

func (v OperationResult) operationResponse()          {}
func (v OperationResult) responseKind() responseKind  { return responseOperationResult }
func (v OperationResult) SessionID() Identifier       { return v.sessionID }
func (v OperationResult) OperationID() Identifier     { return v.operationID }
func (v OperationResult) Sequence() OperationSequence { return v.sequence }
func (v OperationResult) ResultKind() ResultKind      { return v.resultKind }
func (v OperationResult) ArtifactHash() Fingerprint   { return v.artifactHash }
func (v OperationResult) EvidenceHash() Fingerprint   { return v.evidenceHash }
func (v OperationResult) CanonicalHash() Fingerprint  { return v.canonicalHash }
func (v OperationResult) valid() bool {
	return v.sessionID.valid() && v.operationID.valid() && v.sequence != 0 && v.resultKind.valid() && v.artifactHash.valid() && v.evidenceHash.valid() && v.canonicalHash.valid()
}

// Quiescence is the provider's evidence that an active session is quiescent.
type Quiescence struct {
	sessionID            Identifier
	quiescenceHash       Fingerprint
	resourceEvidenceHash Fingerprint
	canonicalHash        Fingerprint
}

type QuiescenceInput struct {
	SessionID            string
	QuiescenceHash       string
	ResourceEvidenceHash string
	CanonicalHash        string
}

func NewQuiescence(input QuiescenceInput) (Quiescence, error) {
	sessionID, err := NewIdentifier(input.SessionID)
	if err != nil {
		return Quiescence{}, err
	}
	quiescenceHash, err := ParseFingerprint(input.QuiescenceHash)
	if err != nil {
		return Quiescence{}, err
	}
	resourceHash, err := ParseFingerprint(input.ResourceEvidenceHash)
	if err != nil {
		return Quiescence{}, err
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return Quiescence{}, err
	}
	return Quiescence{sessionID: sessionID, quiescenceHash: quiescenceHash, resourceEvidenceHash: resourceHash, canonicalHash: canonicalHash}, nil
}

func (v Quiescence) operationResponse()                {}
func (v Quiescence) responseKind() responseKind        { return responseQuiescence }
func (v Quiescence) SessionID() Identifier             { return v.sessionID }
func (v Quiescence) QuiescenceHash() Fingerprint       { return v.quiescenceHash }
func (v Quiescence) ResourceEvidenceHash() Fingerprint { return v.resourceEvidenceHash }
func (v Quiescence) CanonicalHash() Fingerprint        { return v.canonicalHash }
func (v Quiescence) valid() bool {
	return v.sessionID.valid() && v.quiescenceHash.valid() && v.resourceEvidenceHash.valid() && v.canonicalHash.valid()
}

// FreezeInput intentionally omits quiescence: Session supplies only the exact
// prior quiescence hash it persisted from the provider.
type FreezeInput struct {
	freezeID      Identifier
	nonce         Nonce
	canonicalHash Fingerprint
}

type FreezeInputValues struct {
	FreezeID      string
	Nonce         string
	CanonicalHash string
}

func NewFreezeInput(input FreezeInputValues) (FreezeInput, error) {
	freezeID, err := NewIdentifier(input.FreezeID)
	if err != nil {
		return FreezeInput{}, err
	}
	nonce, err := NewNonce(input.Nonce)
	if err != nil {
		return FreezeInput{}, err
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return FreezeInput{}, err
	}
	return FreezeInput{freezeID: freezeID, nonce: nonce, canonicalHash: canonicalHash}, nil
}

func (v FreezeInput) FreezeID() Identifier       { return v.freezeID }
func (v FreezeInput) Nonce() Nonce               { return v.nonce }
func (v FreezeInput) CanonicalHash() Fingerprint { return v.canonicalHash }
func (v FreezeInput) valid() bool {
	return v.freezeID.valid() && v.nonce.valid() && v.canonicalHash.valid()
}

// Freeze is the fully bound provider freeze record.
type Freeze struct {
	sessionID      Identifier
	freezeID       Identifier
	quiescenceHash Fingerprint
	nonce          Nonce
	canonicalHash  Fingerprint
}

func newFreeze(sessionID Identifier, quiescenceHash Fingerprint, input FreezeInput) (Freeze, error) {
	if !sessionID.valid() || !quiescenceHash.valid() || !input.valid() {
		return Freeze{}, fmt.Errorf("planescapeprovider: invalid freeze")
	}
	return Freeze{sessionID: sessionID, freezeID: input.freezeID, quiescenceHash: quiescenceHash, nonce: input.nonce, canonicalHash: input.canonicalHash}, nil
}

func (v Freeze) SessionID() Identifier       { return v.sessionID }
func (v Freeze) FreezeID() Identifier        { return v.freezeID }
func (v Freeze) QuiescenceHash() Fingerprint { return v.quiescenceHash }
func (v Freeze) Nonce() Nonce                { return v.nonce }
func (v Freeze) CanonicalHash() Fingerprint  { return v.canonicalHash }
func (v Freeze) valid() bool {
	return v.sessionID.valid() && v.freezeID.valid() && v.quiescenceHash.valid() && v.nonce.valid() && v.canonicalHash.valid()
}

type FreezeAck struct {
	sessionID      Identifier
	freezeID       Identifier
	quiescenceHash Fingerprint
	frozenAt       time.Time
	canonicalHash  Fingerprint
}

type FreezeAckInput struct {
	SessionID      string
	FreezeID       string
	QuiescenceHash string
	FrozenAt       time.Time
	CanonicalHash  string
}

func NewFreezeAck(input FreezeAckInput) (FreezeAck, error) {
	sessionID, err := NewIdentifier(input.SessionID)
	if err != nil {
		return FreezeAck{}, err
	}
	freezeID, err := NewIdentifier(input.FreezeID)
	if err != nil {
		return FreezeAck{}, err
	}
	quiescenceHash, err := ParseFingerprint(input.QuiescenceHash)
	if err != nil {
		return FreezeAck{}, err
	}
	if input.FrozenAt.IsZero() || input.FrozenAt.UnixMilli() <= 0 {
		return FreezeAck{}, fmt.Errorf("planescapeprovider: frozen_at is required")
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return FreezeAck{}, err
	}
	return FreezeAck{sessionID: sessionID, freezeID: freezeID, quiescenceHash: quiescenceHash, frozenAt: input.FrozenAt.UTC(), canonicalHash: canonicalHash}, nil
}

func (v FreezeAck) SessionID() Identifier       { return v.sessionID }
func (v FreezeAck) FreezeID() Identifier        { return v.freezeID }
func (v FreezeAck) QuiescenceHash() Fingerprint { return v.quiescenceHash }
func (v FreezeAck) FrozenAt() time.Time         { return v.frozenAt }
func (v FreezeAck) CanonicalHash() Fingerprint  { return v.canonicalHash }
func (v FreezeAck) valid() bool {
	return v.sessionID.valid() && v.freezeID.valid() && v.quiescenceHash.valid() && !v.frozenAt.IsZero() && v.canonicalHash.valid()
}

// CancellationInput omits a session binding; Session supplies the one durable
// session it owns. Reason is bounded but is never reflected through Error.
type CancellationInput struct {
	cancellationID Identifier
	reason         string
	nonce          Nonce
	canonicalHash  Fingerprint
}

type CancellationInputValues struct {
	CancellationID string
	Reason         string
	Nonce          string
	CanonicalHash  string
}

func NewCancellationInput(input CancellationInputValues) (CancellationInput, error) {
	cancellationID, err := NewIdentifier(input.CancellationID)
	if err != nil {
		return CancellationInput{}, err
	}
	reason := input.Reason
	if reason == "" || reason != strings.TrimSpace(reason) || !utf8.ValidString(reason) || len(reason) > maxIdentifierBytes || strings.ContainsFunc(reason, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return CancellationInput{}, fmt.Errorf("planescapeprovider: cancellation reason is invalid")
	}
	nonce, err := NewNonce(input.Nonce)
	if err != nil {
		return CancellationInput{}, err
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return CancellationInput{}, err
	}
	return CancellationInput{cancellationID: cancellationID, reason: reason, nonce: nonce, canonicalHash: canonicalHash}, nil
}

func (v CancellationInput) CancellationID() Identifier { return v.cancellationID }
func (v CancellationInput) Reason() string             { return v.reason }
func (v CancellationInput) Nonce() Nonce               { return v.nonce }
func (v CancellationInput) CanonicalHash() Fingerprint { return v.canonicalHash }
func (v CancellationInput) valid() bool {
	return v.cancellationID.valid() && v.reason != "" && v.nonce.valid() && v.canonicalHash.valid()
}

type Cancellation struct {
	sessionID      Identifier
	cancellationID Identifier
	reason         string
	nonce          Nonce
	canonicalHash  Fingerprint
}

func newCancellation(sessionID Identifier, input CancellationInput) (Cancellation, error) {
	if !sessionID.valid() || !input.valid() {
		return Cancellation{}, fmt.Errorf("planescapeprovider: invalid cancellation")
	}
	return Cancellation{sessionID: sessionID, cancellationID: input.cancellationID, reason: input.reason, nonce: input.nonce, canonicalHash: input.canonicalHash}, nil
}

func (v Cancellation) SessionID() Identifier      { return v.sessionID }
func (v Cancellation) CancellationID() Identifier { return v.cancellationID }
func (v Cancellation) Reason() string             { return v.reason }
func (v Cancellation) Nonce() Nonce               { return v.nonce }
func (v Cancellation) CanonicalHash() Fingerprint { return v.canonicalHash }
func (v Cancellation) valid() bool {
	return v.sessionID.valid() && v.cancellationID.valid() && v.reason != "" && v.nonce.valid() && v.canonicalHash.valid()
}

type CancellationAck struct {
	sessionID           Identifier
	cancellationID      Identifier
	terminalOutcome     TerminalOutcome
	logicalEvidenceHash Fingerprint
	canonicalHash       Fingerprint
}

type CancellationAckInput struct {
	SessionID           string
	CancellationID      string
	TerminalOutcome     TerminalOutcome
	LogicalEvidenceHash string
	CanonicalHash       string
}

func NewCancellationAck(input CancellationAckInput) (CancellationAck, error) {
	sessionID, err := NewIdentifier(input.SessionID)
	if err != nil {
		return CancellationAck{}, err
	}
	cancellationID, err := NewIdentifier(input.CancellationID)
	if err != nil {
		return CancellationAck{}, err
	}
	if !input.TerminalOutcome.valid() {
		return CancellationAck{}, fmt.Errorf("planescapeprovider: unsupported terminal outcome")
	}
	logicalHash, err := ParseFingerprint(input.LogicalEvidenceHash)
	if err != nil {
		return CancellationAck{}, err
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return CancellationAck{}, err
	}
	return CancellationAck{sessionID: sessionID, cancellationID: cancellationID, terminalOutcome: input.TerminalOutcome, logicalEvidenceHash: logicalHash, canonicalHash: canonicalHash}, nil
}

func (v CancellationAck) SessionID() Identifier            { return v.sessionID }
func (v CancellationAck) CancellationID() Identifier       { return v.cancellationID }
func (v CancellationAck) TerminalOutcome() TerminalOutcome { return v.terminalOutcome }
func (v CancellationAck) LogicalEvidenceHash() Fingerprint { return v.logicalEvidenceHash }
func (v CancellationAck) CanonicalHash() Fingerprint       { return v.canonicalHash }
func (v CancellationAck) valid() bool {
	return v.sessionID.valid() && v.cancellationID.valid() && v.terminalOutcome.valid() && v.logicalEvidenceHash.valid() && v.canonicalHash.valid()
}

// Closeout is the provider's final, evidence-bound session record.
type Closeout struct {
	sessionID           Identifier
	closeoutID          Identifier
	terminalOutcome     TerminalOutcome
	quiescenceHash      Fingerprint
	logicalEvidenceHash Fingerprint
	nativeExtensionHash Fingerprint
	canonicalHash       Fingerprint
}

type CloseoutInput struct {
	SessionID           string
	CloseoutID          string
	TerminalOutcome     TerminalOutcome
	QuiescenceHash      string
	LogicalEvidenceHash string
	NativeExtensionHash string
	CanonicalHash       string
}

func NewCloseout(input CloseoutInput) (Closeout, error) {
	sessionID, err := NewIdentifier(input.SessionID)
	if err != nil {
		return Closeout{}, err
	}
	closeoutID, err := NewIdentifier(input.CloseoutID)
	if err != nil {
		return Closeout{}, err
	}
	if !input.TerminalOutcome.valid() {
		return Closeout{}, fmt.Errorf("planescapeprovider: unsupported terminal outcome")
	}
	quiescenceHash, err := ParseFingerprint(input.QuiescenceHash)
	if err != nil {
		return Closeout{}, err
	}
	logicalHash, err := ParseFingerprint(input.LogicalEvidenceHash)
	if err != nil {
		return Closeout{}, err
	}
	nativeHash, err := ParseFingerprint(input.NativeExtensionHash)
	if err != nil {
		return Closeout{}, err
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return Closeout{}, err
	}
	return Closeout{sessionID: sessionID, closeoutID: closeoutID, terminalOutcome: input.TerminalOutcome, quiescenceHash: quiescenceHash, logicalEvidenceHash: logicalHash, nativeExtensionHash: nativeHash, canonicalHash: canonicalHash}, nil
}

func (v Closeout) operationResponse()               {}
func (v Closeout) responseKind() responseKind       { return responseCloseout }
func (v Closeout) SessionID() Identifier            { return v.sessionID }
func (v Closeout) CloseoutID() Identifier           { return v.closeoutID }
func (v Closeout) TerminalOutcome() TerminalOutcome { return v.terminalOutcome }
func (v Closeout) QuiescenceHash() Fingerprint      { return v.quiescenceHash }
func (v Closeout) LogicalEvidenceHash() Fingerprint { return v.logicalEvidenceHash }
func (v Closeout) NativeExtensionHash() Fingerprint { return v.nativeExtensionHash }
func (v Closeout) CanonicalHash() Fingerprint       { return v.canonicalHash }
func (v Closeout) valid() bool {
	return v.sessionID.valid() && v.closeoutID.valid() && v.terminalOutcome.valid() && v.quiescenceHash.valid() && v.logicalEvidenceHash.valid() && v.nativeExtensionHash.valid() && v.canonicalHash.valid()
}

// Transition is the closed lifecycle predecessor included in a provider error.
type Transition string

const (
	TransitionDiscover Transition = "undiscovered_to_discovered"
	TransitionAdmit    Transition = "discovered_to_admitted"
	TransitionActivate Transition = "admitted_to_active"
	TransitionQuiesce  Transition = "active_to_quiescent"
	TransitionFreeze   Transition = "quiescent_to_frozen"
	TransitionCancel   Transition = "active_to_cancelled"
	TransitionCloseout Transition = "frozen_to_closed"
)

func (t Transition) valid() bool {
	switch t {
	case TransitionDiscover, TransitionAdmit, TransitionActivate, TransitionQuiesce, TransitionFreeze, TransitionCancel, TransitionCloseout:
		return true
	default:
		return false
	}
}

// ProviderFailure is the only provider-originated error accepted by Client.
// Its Error text is intentionally stable and has no provider-supplied detail.
type ProviderFailure struct {
	code          ProviderErrorCode
	providerID    Identifier
	epoch         ProviderEpoch
	retryFrom     Transition
	canonicalHash Fingerprint
}

type ProviderFailureInput struct {
	Code          ProviderErrorCode
	ProviderID    string
	ProviderEpoch uint64
	RetryFrom     Transition
	CanonicalHash string
}

func NewProviderFailure(input ProviderFailureInput) (*ProviderFailure, error) {
	if !input.Code.valid() {
		return nil, fmt.Errorf("planescapeprovider: unsupported provider error code")
	}
	providerID, err := NewIdentifier(input.ProviderID)
	if err != nil {
		return nil, err
	}
	epoch, err := NewProviderEpoch(input.ProviderEpoch)
	if err != nil {
		return nil, err
	}
	if !input.RetryFrom.valid() {
		return nil, fmt.Errorf("planescapeprovider: unsupported retry transition")
	}
	canonicalHash, err := ParseFingerprint(input.CanonicalHash)
	if err != nil {
		return nil, err
	}
	return &ProviderFailure{code: input.Code, providerID: providerID, epoch: epoch, retryFrom: input.RetryFrom, canonicalHash: canonicalHash}, nil
}

func (v *ProviderFailure) Error() string {
	if v == nil {
		return ""
	}
	return "planescape provider failure: " + string(v.code)
}

func (v *ProviderFailure) Code() ProviderErrorCode {
	if v == nil {
		return ""
	}
	return v.code
}

func (v *ProviderFailure) ProviderID() Identifier {
	if v == nil {
		return Identifier{}
	}
	return v.providerID
}

func (v *ProviderFailure) ProviderEpoch() ProviderEpoch {
	if v == nil {
		return 0
	}
	return v.epoch
}

func (v *ProviderFailure) RetryFrom() Transition {
	if v == nil {
		return ""
	}
	return v.retryFrom
}

func (v *ProviderFailure) CanonicalHash() Fingerprint {
	if v == nil {
		return Fingerprint{}
	}
	return v.canonicalHash
}

func (v *ProviderFailure) valid() bool {
	return v != nil && v.code.valid() && v.providerID.valid() && v.epoch != 0 && v.retryFrom.valid() && v.canonicalHash.valid()
}
