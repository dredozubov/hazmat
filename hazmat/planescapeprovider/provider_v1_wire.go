package planescapeprovider

const (
	providerV1KindDiscovery         = "provider_discovery"
	providerV1KindCapabilities      = "provider_capabilities"
	providerV1KindRequirement       = "execution_requirement"
	providerV1KindCompiledPlan      = "compiled_containment_plan"
	providerV1KindAdmission         = "session_admission"
	providerV1KindOperation         = "agent_operation"
	providerV1KindOperationResult   = "operation_result"
	providerV1KindQuiescence        = "quiescence"
	providerV1KindFreeze            = "freeze"
	providerV1KindFreezeAck         = "freeze_ack"
	providerV1KindCloseout          = "closeout"
	providerV1KindCancellation      = "cancellation"
	providerV1KindCancellationAck   = "cancellation_ack"
	providerV1KindError             = "provider_error"
	providerV1SchemaDiscovery       = "planescape.provider.discovery.v1"
	providerV1SchemaCapabilities    = "planescape.provider.provider_capabilities.v1"
	providerV1SchemaRequirement     = "planescape.provider.execution_requirement.v1"
	providerV1SchemaCompiledPlan    = "planescape.provider.compiled_containment_plan.v1"
	providerV1SchemaAdmission       = "planescape.provider.session_admission.v1"
	providerV1SchemaOperation       = "planescape.provider.agent_operation.v1"
	providerV1SchemaOperationResult = "planescape.provider.operation_result.v1"
	providerV1SchemaQuiescence      = "planescape.provider.quiescence.v1"
	providerV1SchemaFreeze          = "planescape.provider.freeze.v1"
	providerV1SchemaFreezeAck       = "planescape.provider.freeze_ack.v1"
	providerV1SchemaCloseout        = "planescape.provider.closeout.v1"
	providerV1SchemaCancellation    = "planescape.provider.cancellation.v1"
	providerV1SchemaCancellationAck = "planescape.provider.cancellation_ack.v1"
	providerV1SchemaError           = "planescape.provider.provider_error.v1"
	providerV1DomainCapabilitySet   = "planescape.provider.capability_set.canonical.v1"
	providerV1DomainDiscovery       = "planescape.provider.discovery.canonical.v1"
	providerV1DomainCapabilities    = "planescape.provider.capabilities.canonical.v1"
	providerV1DomainRequirement     = "planescape.provider.requirement.canonical.v1"
	providerV1DomainCompiledPlan    = "planescape.provider.compiled_containment_plan.canonical.v1"
	providerV1DomainAdmission       = "planescape.provider.session_admission.canonical.v1"
	providerV1DomainOperation       = "planescape.provider.agent_operation.canonical.v1"
	providerV1DomainOperationResult = "planescape.provider.operation_result.canonical.v1"
	providerV1DomainQuiescence      = "planescape.provider.quiescence.canonical.v1"
	providerV1DomainFreeze          = "planescape.provider.freeze.canonical.v1"
	providerV1DomainFreezeAck       = "planescape.provider.freeze_ack.canonical.v1"
	providerV1DomainCloseout        = "planescape.provider.closeout.canonical.v1"
	providerV1DomainCancellation    = "planescape.provider.cancellation.canonical.v1"
	providerV1DomainCancellationAck = "planescape.provider.cancellation_ack.canonical.v1"
	providerV1DomainError           = "planescape.provider.error.canonical.v1"
)

type providerV1DiscoveryDTO struct {
	Schema          string `json:"schema"`
	ProtocolVersion string `json:"protocol_version"`
	CanonicalHash   string `json:"canonical_hash"`
}

func (v providerV1DiscoveryDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainDiscovery)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.ProtocolVersion)
	return builder.preimage()
}

type providerV1CapabilitiesDTO struct {
	Schema             string   `json:"schema"`
	ProviderID         string   `json:"provider_id"`
	ProviderEpoch      uint64   `json:"provider_epoch"`
	ProfileID          string   `json:"profile_id"`
	ProtocolVersion    string   `json:"protocol_version"`
	Capabilities       []string `json:"capabilities"`
	ResourceDimensions []string `json:"resource_dimensions"`
	CapabilityHash     string   `json:"capability_hash"`
	CanonicalHash      string   `json:"canonical_hash"`
}

func (v providerV1CapabilitiesDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainCapabilities)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.ProviderID)
	builder.u64(3, v.ProviderEpoch)
	builder.utf8(4, v.ProfileID)
	builder.utf8(5, v.ProtocolVersion)
	builder.fingerprint(6, v.CapabilityHash)
	return builder.preimage()
}

func (v providerV1CapabilitiesDTO) capabilitySetPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainCapabilitySet)
	builder.sortedList(1, v.Capabilities)
	builder.sortedList(2, v.ResourceDimensions)
	return builder.preimage()
}

type providerV1RequirementDTO struct {
	Schema                     string   `json:"schema"`
	RequirementID              string   `json:"requirement_id"`
	ControllerAttemptID        string   `json:"controller_attempt_id"`
	AuthorityHash              string   `json:"authority_hash"`
	RequiredCapabilities       []string `json:"required_capabilities"`
	RequiredResourceDimensions []string `json:"required_resource_dimensions"`
	EvidenceProfileHash        string   `json:"evidence_profile_hash"`
	CanonicalHash              string   `json:"canonical_hash"`
}

func (v providerV1RequirementDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainRequirement)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.RequirementID)
	builder.utf8(3, v.ControllerAttemptID)
	builder.fingerprint(4, v.AuthorityHash)
	builder.sortedList(5, v.RequiredCapabilities)
	builder.sortedList(6, v.RequiredResourceDimensions)
	builder.fingerprint(7, v.EvidenceProfileHash)
	return builder.preimage()
}

type providerV1CompiledContainmentPlanDTO struct {
	Schema                      string `json:"schema"`
	ProtocolVersion             string `json:"protocol_version"`
	RequirementRecordBase64URL  string `json:"requirement_record_base64url"`
	RequirementHash             string `json:"requirement_hash"`
	RequiredProfileID           string `json:"required_profile_id"`
	ProviderID                  string `json:"provider_id"`
	ProviderEpoch               uint64 `json:"provider_epoch"`
	ProviderProfileID           string `json:"provider_profile_id"`
	ProviderCapabilityHash      string `json:"provider_capability_hash"`
	ContainmentRequestBase64URL string `json:"containment_request_base64url"`
	ContainmentRequestHash      string `json:"containment_request_hash"`
	AuthorityHash               string `json:"authority_hash"`
	EvidenceProfileHash         string `json:"evidence_profile_hash"`
	CanonicalHash               string `json:"canonical_hash"`
}

func (v providerV1CompiledContainmentPlanDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainCompiledPlan)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.ProtocolVersion)
	builder.utf8(3, v.RequirementRecordBase64URL)
	builder.fingerprint(4, v.RequirementHash)
	builder.utf8(5, v.RequiredProfileID)
	builder.utf8(6, v.ProviderID)
	builder.u64(7, v.ProviderEpoch)
	builder.utf8(8, v.ProviderProfileID)
	builder.fingerprint(9, v.ProviderCapabilityHash)
	builder.utf8(10, v.ContainmentRequestBase64URL)
	builder.fingerprint(11, v.ContainmentRequestHash)
	builder.fingerprint(12, v.AuthorityHash)
	builder.fingerprint(13, v.EvidenceProfileHash)
	return builder.preimage()
}

type providerV1AdmissionDTO struct {
	Schema                string `json:"schema"`
	SessionID             string `json:"session_id"`
	ProviderID            string `json:"provider_id"`
	ProviderEpoch         uint64 `json:"provider_epoch"`
	RequirementHash       string `json:"requirement_hash"`
	CompiledPlanHash      string `json:"compiled_plan_hash"`
	SessionCapabilityHash string `json:"session_capability_hash"`
	ExpiresAtMS           uint64 `json:"expires_at_ms"`
	CanonicalHash         string `json:"canonical_hash"`
}

func (v providerV1AdmissionDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainAdmission)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.SessionID)
	builder.utf8(3, v.ProviderID)
	builder.u64(4, v.ProviderEpoch)
	builder.fingerprint(5, v.RequirementHash)
	builder.fingerprint(6, v.CompiledPlanHash)
	builder.fingerprint(7, v.SessionCapabilityHash)
	builder.u64(8, v.ExpiresAtMS)
	return builder.preimage()
}

type providerV1OperationDTO struct {
	Schema            string `json:"schema"`
	SessionID         string `json:"session_id"`
	OperationID       string `json:"operation_id"`
	OperationSequence uint64 `json:"operation_sequence"`
	OperationKind     string `json:"operation_kind"`
	PlanHash          string `json:"plan_hash"`
	Nonce             string `json:"nonce"`
	PayloadHash       string `json:"payload_hash"`
	CanonicalHash     string `json:"canonical_hash"`
}

func (v providerV1OperationDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainOperation)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.SessionID)
	builder.utf8(3, v.OperationID)
	builder.u64(4, v.OperationSequence)
	builder.utf8(5, v.OperationKind)
	builder.fingerprint(6, v.PlanHash)
	builder.utf8(7, v.Nonce)
	builder.fingerprint(8, v.PayloadHash)
	return builder.preimage()
}

type providerV1OperationResultDTO struct {
	Schema            string `json:"schema"`
	SessionID         string `json:"session_id"`
	OperationID       string `json:"operation_id"`
	OperationSequence uint64 `json:"operation_sequence"`
	ResultKind        string `json:"result_kind"`
	ArtifactHash      string `json:"artifact_hash"`
	EvidenceHash      string `json:"evidence_hash"`
	CanonicalHash     string `json:"canonical_hash"`
}

func (v providerV1OperationResultDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainOperationResult)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.SessionID)
	builder.utf8(3, v.OperationID)
	builder.u64(4, v.OperationSequence)
	builder.utf8(5, v.ResultKind)
	builder.fingerprint(6, v.ArtifactHash)
	builder.fingerprint(7, v.EvidenceHash)
	return builder.preimage()
}

type providerV1QuiescenceDTO struct {
	Schema               string `json:"schema"`
	SessionID            string `json:"session_id"`
	QuiescenceHash       string `json:"quiescence_hash"`
	ResourceEvidenceHash string `json:"resource_evidence_hash"`
	CanonicalHash        string `json:"canonical_hash"`
}

func (v providerV1QuiescenceDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainQuiescence)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.SessionID)
	builder.fingerprint(3, v.QuiescenceHash)
	builder.fingerprint(4, v.ResourceEvidenceHash)
	return builder.preimage()
}

type providerV1FreezeDTO struct {
	Schema         string `json:"schema"`
	SessionID      string `json:"session_id"`
	FreezeID       string `json:"freeze_id"`
	QuiescenceHash string `json:"quiescence_hash"`
	Nonce          string `json:"nonce"`
	CanonicalHash  string `json:"canonical_hash"`
}

func (v providerV1FreezeDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainFreeze)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.SessionID)
	builder.utf8(3, v.FreezeID)
	builder.fingerprint(4, v.QuiescenceHash)
	builder.utf8(5, v.Nonce)
	return builder.preimage()
}

type providerV1FreezeAckDTO struct {
	Schema         string `json:"schema"`
	SessionID      string `json:"session_id"`
	FreezeID       string `json:"freeze_id"`
	QuiescenceHash string `json:"quiescence_hash"`
	FrozenAtMS     uint64 `json:"frozen_at_ms"`
	CanonicalHash  string `json:"canonical_hash"`
}

func (v providerV1FreezeAckDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainFreezeAck)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.SessionID)
	builder.utf8(3, v.FreezeID)
	builder.fingerprint(4, v.QuiescenceHash)
	builder.u64(5, v.FrozenAtMS)
	return builder.preimage()
}

type providerV1CloseoutDTO struct {
	Schema              string `json:"schema"`
	SessionID           string `json:"session_id"`
	CloseoutID          string `json:"closeout_id"`
	TerminalOutcome     string `json:"terminal_outcome"`
	QuiescenceHash      string `json:"quiescence_hash"`
	LogicalEvidenceHash string `json:"logical_evidence_hash"`
	NativeExtensionHash string `json:"native_extension_hash"`
	CanonicalHash       string `json:"canonical_hash"`
}

func (v providerV1CloseoutDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainCloseout)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.SessionID)
	builder.utf8(3, v.CloseoutID)
	builder.utf8(4, v.TerminalOutcome)
	builder.fingerprint(5, v.QuiescenceHash)
	builder.fingerprint(6, v.LogicalEvidenceHash)
	builder.fingerprint(7, v.NativeExtensionHash)
	return builder.preimage()
}

type providerV1CancellationDTO struct {
	Schema         string `json:"schema"`
	SessionID      string `json:"session_id"`
	CancellationID string `json:"cancellation_id"`
	Reason         string `json:"reason"`
	Nonce          string `json:"nonce"`
	CanonicalHash  string `json:"canonical_hash"`
}

func (v providerV1CancellationDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainCancellation)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.SessionID)
	builder.utf8(3, v.CancellationID)
	builder.utf8(4, v.Reason)
	builder.utf8(5, v.Nonce)
	return builder.preimage()
}

type providerV1CancellationAckDTO struct {
	Schema              string `json:"schema"`
	SessionID           string `json:"session_id"`
	CancellationID      string `json:"cancellation_id"`
	TerminalOutcome     string `json:"terminal_outcome"`
	LogicalEvidenceHash string `json:"logical_evidence_hash"`
	CanonicalHash       string `json:"canonical_hash"`
}

func (v providerV1CancellationAckDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainCancellationAck)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.SessionID)
	builder.utf8(3, v.CancellationID)
	builder.utf8(4, v.TerminalOutcome)
	builder.fingerprint(5, v.LogicalEvidenceHash)
	return builder.preimage()
}

type providerV1ErrorDTO struct {
	Schema              string `json:"schema"`
	ErrorCode           string `json:"error_code"`
	ProviderID          string `json:"provider_id"`
	ProviderEpoch       uint64 `json:"provider_epoch"`
	RetryFromTransition string `json:"retry_from_transition"`
	CanonicalHash       string `json:"canonical_hash"`
}

func (v providerV1ErrorDTO) canonicalPreimage() ([]byte, error) {
	builder := newProviderV1CanonicalBuilder(providerV1DomainError)
	builder.utf8(1, v.Schema)
	builder.utf8(2, v.ErrorCode)
	builder.utf8(3, v.ProviderID)
	builder.u64(4, v.ProviderEpoch)
	builder.utf8(5, v.RetryFromTransition)
	return builder.preimage()
}
