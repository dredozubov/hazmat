package planescapeprovider

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"time"
)

const (
	providerV1MaxEmbeddedRequirementBytes = 16 * 1024
	providerV1MaxEmbeddedContainmentBytes = 24 * 1024

	providerV1ContainmentLeaseRequestSchema = "execution.containment-lease-request.v1"
	providerV1ContainmentScopeDomain        = "planescape.execution.containment-scope.v1\x00"
	providerV1NamespaceContractDomain       = "planescape.execution.namespace-contract.v1\x00"
	providerV1ContainmentContractDomain     = "planescape.execution.containment-contract.v1\x00"
	providerV1ContainmentRequestDomain      = "planescape.execution.containment-lease-request.v1\x00"
)

// CompiledContainmentPlan is a validated Rust-produced provider-v1 carrier.
// Hazmat can retain and replay it, but intentionally exposes no compiler or
// semantic-input constructor.
type CompiledContainmentPlan struct {
	requirement           ExecutionRequirement
	requirementRecordJSON []byte
	requiredProfile       Profile
	providerID            Identifier
	providerEpoch         ProviderEpoch
	providerProfile       Profile
	providerCapability    Fingerprint
	containmentRequest    providerV1ContainmentLeaseRequest
	canonicalHash         Fingerprint
}

func (v CompiledContainmentPlan) Requirement() ExecutionRequirement {
	return v.requirement
}

func (v CompiledContainmentPlan) RequirementRecordJSON() []byte {
	return append([]byte(nil), v.requirementRecordJSON...)
}

func (v CompiledContainmentPlan) RequiredProfile() Profile {
	return v.requiredProfile
}

func (v CompiledContainmentPlan) ProviderID() Identifier {
	return v.providerID
}

func (v CompiledContainmentPlan) ProviderEpoch() ProviderEpoch {
	return v.providerEpoch
}

func (v CompiledContainmentPlan) ProviderProfile() Profile {
	return v.providerProfile
}

func (v CompiledContainmentPlan) ProviderCapabilityHash() Fingerprint {
	return v.providerCapability
}

func (v CompiledContainmentPlan) ContainmentRequestJSON() []byte {
	return v.containmentRequest.canonicalJSON()
}

func (v CompiledContainmentPlan) ContainmentRequestHash() Fingerprint {
	return v.containmentRequest.requestHash
}

func (v CompiledContainmentPlan) DeadlineAt() (time.Time, bool) {
	if v.containmentRequest.deadlineAtMS == 0 ||
		v.containmentRequest.deadlineAtMS > math.MaxInt64 {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(v.containmentRequest.deadlineAtMS)).UTC(), true
}

func (v CompiledContainmentPlan) AuthorityHash() Fingerprint {
	return v.requirement.AuthorityHash()
}

func (v CompiledContainmentPlan) EvidenceProfileHash() Fingerprint {
	return v.requirement.EvidenceProfileHash()
}

func (v CompiledContainmentPlan) CanonicalHash() Fingerprint {
	return v.canonicalHash
}

// ValidateProvider requires the exact provider selected by the Rust compiler.
func (v CompiledContainmentPlan) ValidateProvider(provider ProviderCapabilities) error {
	if !v.valid() || !provider.valid() ||
		provider.ProviderID() != v.providerID ||
		provider.ProviderEpoch() != v.providerEpoch ||
		provider.Profile() != v.providerProfile ||
		provider.CapabilityHash() != v.providerCapability ||
		!provider.Profile().Satisfies(v.requiredProfile) ||
		!containsCapabilities(
			provider.Capabilities(),
			v.requirement.RequiredCapabilities(),
		) ||
		!containsResources(
			provider.ResourceDimensions(),
			v.requirement.RequiredResourceDimensions(),
		) {
		return errProviderV1Frame
	}
	return nil
}

// ValidateSessionAdmission requires every provider, requirement, plan,
// selected-capability, and deadline binding to be repeated exactly.
func (v CompiledContainmentPlan) ValidateSessionAdmission(
	admission SessionAdmission,
) error {
	deadline, hasDeadline := v.DeadlineAt()
	if !v.valid() || !admission.valid() ||
		!hasDeadline ||
		admission.ProviderID() != v.providerID ||
		admission.ProviderEpoch() != v.providerEpoch ||
		admission.RequirementHash() != v.requirement.CanonicalHash() ||
		admission.CompiledPlanHash() != v.canonicalHash ||
		admission.SessionCapabilityHash() != v.providerCapability ||
		!admission.ExpiresAt().Equal(deadline) {
		return errProviderV1Frame
	}
	return nil
}

// ValidateAgentOperation requires the operation to use the session admitted
// for this exact compiled plan.
func (v CompiledContainmentPlan) ValidateAgentOperation(
	admission SessionAdmission,
	operation AgentOperation,
) error {
	if v.ValidateSessionAdmission(admission) != nil ||
		!operation.valid() ||
		operation.SessionID() != admission.SessionID() ||
		operation.PlanHash() != v.canonicalHash {
		return errProviderV1Frame
	}
	return nil
}

func (v CompiledContainmentPlan) valid() bool {
	return v.requirement.valid() &&
		len(v.requirementRecordJSON) > 0 &&
		len(v.requirementRecordJSON) <= providerV1MaxEmbeddedRequirementBytes &&
		v.requiredProfile.valid() &&
		v.providerID.valid() &&
		v.providerEpoch != 0 &&
		v.providerProfile.valid() &&
		v.providerProfile.Satisfies(v.requiredProfile) &&
		v.providerCapability.valid() &&
		v.containmentRequest.valid() &&
		v.canonicalHash.valid()
}

func (v CompiledContainmentPlan) dto() providerV1CompiledContainmentPlanDTO {
	return providerV1CompiledContainmentPlanDTO{
		Schema:                      providerV1SchemaCompiledPlan,
		ProtocolVersion:             ProtocolVersionV1,
		RequirementRecordBase64URL:  base64.RawURLEncoding.EncodeToString(v.requirementRecordJSON),
		RequirementHash:             v.requirement.CanonicalHash().String(),
		RequiredProfileID:           string(v.requiredProfile),
		ProviderID:                  v.providerID.String(),
		ProviderEpoch:               v.providerEpoch.Uint64(),
		ProviderProfileID:           string(v.providerProfile),
		ProviderCapabilityHash:      v.providerCapability.String(),
		ContainmentRequestBase64URL: base64.RawURLEncoding.EncodeToString(v.containmentRequest.record),
		ContainmentRequestHash:      v.containmentRequest.requestHash.String(),
		AuthorityHash:               v.requirement.AuthorityHash().String(),
		EvidenceProfileHash:         v.requirement.EvidenceProfileHash().String(),
		CanonicalHash:               v.canonicalHash.String(),
	}
}

func decodeProviderV1CompiledContainmentPlan(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1CompiledContainmentPlanDTO
	if err := object.decode(
		providerV1SchemaCompiledPlan,
		&dto,
		"schema",
		"protocol_version",
		"requirement_record_base64url",
		"requirement_hash",
		"required_profile_id",
		"provider_id",
		"provider_epoch",
		"provider_profile_id",
		"provider_capability_hash",
		"containment_request_base64url",
		"containment_request_hash",
		"authority_hash",
		"evidence_profile_hash",
		"canonical_hash",
	); err != nil || dto.ProtocolVersion != ProtocolVersionV1 {
		return decodedProviderV1Record{}, errProviderV1Frame
	}

	requirementJSON, err := decodeProviderV1CanonicalBase64URL(
		dto.RequirementRecordBase64URL,
		providerV1MaxEmbeddedRequirementBytes,
	)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	requirementRecord, err := decodeProviderV1Record(requirementJSON)
	if err != nil || requirementRecord.kind != providerV1KindRequirement {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	requirement, ok := requirementRecord.value.(ExecutionRequirement)
	if !ok {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	canonicalRequirement, err := (ProviderV1FrameCodec{}).EncodeExecutionRequirement(requirement)
	if err != nil || !bytes.Equal(canonicalRequirement, requirementJSON) ||
		dto.RequirementHash != requirement.CanonicalHash().String() ||
		dto.AuthorityHash != requirement.AuthorityHash().String() ||
		dto.EvidenceProfileHash != requirement.EvidenceProfileHash().String() {
		return decodedProviderV1Record{}, errProviderV1Frame
	}

	containmentJSON, err := decodeProviderV1CanonicalBase64URL(
		dto.ContainmentRequestBase64URL,
		providerV1MaxEmbeddedContainmentBytes,
	)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	containment, err := parseProviderV1ContainmentLeaseRequest(containmentJSON)
	if err != nil || dto.ContainmentRequestHash != containment.requestHash.String() {
		return decodedProviderV1Record{}, errProviderV1Frame
	}

	requiredProfile := Profile(dto.RequiredProfileID)
	providerProfile := Profile(dto.ProviderProfileID)
	if !requiredProfile.valid() || !providerProfile.Satisfies(requiredProfile) {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	providerID, err := NewIdentifier(dto.ProviderID)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	providerEpoch, err := NewProviderEpoch(dto.ProviderEpoch)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	providerCapability, err := ParseFingerprint(dto.ProviderCapabilityHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	canonicalHash, err := ParseFingerprint(dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}

	value := CompiledContainmentPlan{
		requirement:           requirement,
		requirementRecordJSON: append([]byte(nil), requirementJSON...),
		requiredProfile:       requiredProfile,
		providerID:            providerID,
		providerEpoch:         providerEpoch,
		providerProfile:       providerProfile,
		providerCapability:    providerCapability,
		containmentRequest:    containment,
		canonicalHash:         canonicalHash,
	}
	if !value.valid() {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindCompiledPlan,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1CanonicalBase64URL(
	encoded string,
	maximumBytes int,
) ([]byte, error) {
	maximumEncodedBytes := (maximumBytes*4 + 2) / 3
	if encoded == "" || maximumBytes <= 0 || len(encoded) > maximumEncodedBytes {
		return nil, errProviderV1Frame
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) == 0 || len(decoded) > maximumBytes ||
		base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return nil, errProviderV1Frame
	}
	return decoded, nil
}

type providerV1ContainmentLeaseRequest struct {
	record       []byte
	requestHash  Fingerprint
	deadlineAtMS uint64
}

func (v providerV1ContainmentLeaseRequest) canonicalJSON() []byte {
	return append([]byte(nil), v.record...)
}

func (v providerV1ContainmentLeaseRequest) valid() bool {
	return len(v.record) > 0 &&
		len(v.record) <= providerV1MaxEmbeddedContainmentBytes &&
		v.requestHash.valid() &&
		v.deadlineAtMS > 0
}

type providerV1ContainmentLeaseRequestDTO struct {
	Schema        string                           `json:"schema"`
	Scope         providerV1ContainmentScopeDTO    `json:"scope"`
	Contract      providerV1ContainmentContractDTO `json:"contract"`
	RequestSHA256 string                           `json:"request_sha256"`
}

type providerV1ContainmentScopeDTO struct {
	ProjectID            string `json:"project_id"`
	WorkItemID           string `json:"work_item_id"`
	RunID                string `json:"run_id"`
	AttemptID            string `json:"attempt_id"`
	WorkspaceID          string `json:"workspace_id"`
	WorkspaceClaimSHA256 string `json:"workspace_claim_sha256"`
	RequestNonce         string `json:"request_nonce"`
	SourceArtifactSHA256 string `json:"source_artifact_sha256"`
	PolicySHA256         string `json:"policy_sha256"`
	ToolProfileSHA256    string `json:"tool_profile_sha256"`
	DeadlineAtMS         uint64 `json:"deadline_at_ms"`
	ScopeSHA256          string `json:"scope_sha256"`
}

type providerV1ContainmentContractDTO struct {
	Namespace      providerV1NamespaceContractDTO `json:"namespace"`
	Resources      providerV1ResourceLimitsDTO    `json:"resources"`
	ContractSHA256 string                         `json:"contract_sha256"`
}

type providerV1NamespaceContractDTO struct {
	SourceDelivery     string `json:"source_delivery"`
	HostWritableMounts string `json:"host_writable_mounts"`
	WorkspaceMutation  string `json:"workspace_mutation"`
	Evidence           string `json:"evidence"`
	Transcript         string `json:"transcript"`
	Cleanup            string `json:"cleanup"`
	ContractSHA256     string `json:"contract_sha256"`
}

type providerV1ResourceLimitsDTO struct {
	MemoryBytes             uint64                    `json:"memory_bytes"`
	SwapBytes               uint64                    `json:"swap_bytes"`
	Tasks                   uint64                    `json:"tasks"`
	CPUBandwidth            providerV1CPUBandwidthDTO `json:"cpu_bandwidth"`
	AggregateOpenFiles      uint64                    `json:"aggregate_open_files"`
	WorkspaceAllocatedBytes uint64                    `json:"workspace_allocated_bytes"`
	WorkspaceInodes         uint64                    `json:"workspace_inodes"`
	LogicalFileBytes        uint64                    `json:"logical_file_bytes"`
}

type providerV1CPUBandwidthDTO struct {
	QuotaUS  uint64 `json:"quota_us"`
	PeriodUS uint64 `json:"period_us"`
}

func parseProviderV1ContainmentLeaseRequest(
	record []byte,
) (providerV1ContainmentLeaseRequest, error) {
	if len(record) == 0 || len(record) > providerV1MaxEmbeddedContainmentBytes {
		return providerV1ContainmentLeaseRequest{}, errProviderV1Frame
	}
	var dto providerV1ContainmentLeaseRequestDTO
	decoder := json.NewDecoder(bytes.NewReader(record))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dto); err != nil || requireProviderV1JSONEOF(decoder) != nil {
		return providerV1ContainmentLeaseRequest{}, errProviderV1Frame
	}
	if err := validateProviderV1ContainmentLeaseRequest(dto); err != nil {
		return providerV1ContainmentLeaseRequest{}, errProviderV1Frame
	}
	canonical, err := json.Marshal(dto)
	if err != nil || !bytes.Equal(canonical, record) {
		return providerV1ContainmentLeaseRequest{}, errProviderV1Frame
	}
	requestHash, err := ParseFingerprint(dto.RequestSHA256)
	if err != nil {
		return providerV1ContainmentLeaseRequest{}, errProviderV1Frame
	}
	return providerV1ContainmentLeaseRequest{
		record:       append([]byte(nil), canonical...),
		requestHash:  requestHash,
		deadlineAtMS: dto.Scope.DeadlineAtMS,
	}, nil
}

func validateProviderV1ContainmentLeaseRequest(
	dto providerV1ContainmentLeaseRequestDTO,
) error {
	if dto.Schema != providerV1ContainmentLeaseRequestSchema ||
		!validProviderV1ContainmentScope(dto.Scope) ||
		!validProviderV1NamespaceContract(dto.Contract.Namespace) ||
		!validProviderV1ResourceLimits(dto.Contract.Resources) {
		return errProviderV1Frame
	}

	namespaceHash, err := providerV1ContainmentHash(
		providerV1NamespaceContractDomain,
		[]any{
			dto.Contract.Namespace.SourceDelivery,
			dto.Contract.Namespace.HostWritableMounts,
			dto.Contract.Namespace.WorkspaceMutation,
			dto.Contract.Namespace.Evidence,
			dto.Contract.Namespace.Transcript,
			dto.Contract.Namespace.Cleanup,
		},
	)
	if err != nil || namespaceHash.String() != dto.Contract.Namespace.ContractSHA256 {
		return errProviderV1Frame
	}
	scopeHash, err := providerV1ContainmentHash(
		providerV1ContainmentScopeDomain,
		[]any{
			dto.Scope.ProjectID,
			dto.Scope.WorkItemID,
			dto.Scope.RunID,
			dto.Scope.AttemptID,
			dto.Scope.WorkspaceID,
			dto.Scope.WorkspaceClaimSHA256,
			dto.Scope.RequestNonce,
			dto.Scope.SourceArtifactSHA256,
			dto.Scope.PolicySHA256,
			dto.Scope.ToolProfileSHA256,
			dto.Scope.DeadlineAtMS,
		},
	)
	if err != nil || scopeHash.String() != dto.Scope.ScopeSHA256 {
		return errProviderV1Frame
	}
	contractHash, err := providerV1ContainmentHash(
		providerV1ContainmentContractDomain,
		[]any{dto.Contract.Namespace.ContractSHA256, dto.Contract.Resources},
	)
	if err != nil || contractHash.String() != dto.Contract.ContractSHA256 {
		return errProviderV1Frame
	}
	requestHash, err := providerV1ContainmentHash(
		providerV1ContainmentRequestDomain,
		[]any{dto.Schema, dto.Scope, dto.Contract},
	)
	if err != nil || requestHash.String() != dto.RequestSHA256 {
		return errProviderV1Frame
	}
	return nil
}

func validProviderV1ContainmentScope(scope providerV1ContainmentScopeDTO) bool {
	for _, value := range []string{
		scope.ProjectID,
		scope.WorkItemID,
		scope.RunID,
		scope.AttemptID,
		scope.WorkspaceID,
		scope.RequestNonce,
	} {
		if !validProviderV1ContainmentID(value) {
			return false
		}
	}
	for _, value := range []string{
		scope.WorkspaceClaimSHA256,
		scope.SourceArtifactSHA256,
		scope.PolicySHA256,
		scope.ToolProfileSHA256,
		scope.ScopeSHA256,
	} {
		if _, err := ParseFingerprint(value); err != nil {
			return false
		}
	}
	return scope.DeadlineAtMS > 0
}

func validProviderV1ContainmentID(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > maxIdentifierBytes {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-' || r == ':')
	}) == -1
}

func validProviderV1NamespaceContract(namespace providerV1NamespaceContractDTO) bool {
	if namespace.SourceDelivery != "content_addressed_only" ||
		namespace.HostWritableMounts != "denied" ||
		namespace.WorkspaceMutation != "broker_only" ||
		namespace.Evidence != "protected_append_only" ||
		namespace.Transcript != "exact_receipt_manifest" ||
		namespace.Cleanup != "privileged_after_retirement" {
		return false
	}
	_, err := ParseFingerprint(namespace.ContractSHA256)
	return err == nil
}

func validProviderV1ResourceLimits(resources providerV1ResourceLimitsDTO) bool {
	return resources.MemoryBytes > 0 &&
		resources.Tasks > 0 &&
		resources.CPUBandwidth.QuotaUS > 0 &&
		resources.CPUBandwidth.PeriodUS > 0 &&
		resources.AggregateOpenFiles > 0 &&
		resources.WorkspaceAllocatedBytes > 0 &&
		resources.WorkspaceInodes > 0 &&
		resources.LogicalFileBytes > 0 &&
		resources.LogicalFileBytes <= resources.WorkspaceAllocatedBytes
}

func providerV1ContainmentHash(domain string, value any) (Fingerprint, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return Fingerprint{}, errProviderV1Frame
	}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(domain))
	_, _ = hasher.Write(encoded)
	return ParseFingerprint("sha256:" + hex.EncodeToString(hasher.Sum(nil)))
}
