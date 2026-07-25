package planescapeprovider

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"math"
)

const (
	protectedBrokerContainmentPreflightSchemaV2         = "execution.containment-preflight.v2"
	protectedBrokerResourceEvidenceSnapshotSchemaV2     = "execution.resource-evidence-snapshot.v2"
	protectedBrokerToolTranscriptManifestSchemaV2       = "execution.tool-transcript-manifest.v2"
	protectedBrokerTranscriptZeroHashDomainV2           = "planescape.execution.tool-transcript-zero.v2\x00"
	protectedBrokerPreflightSignatureDomainV2           = "planescape.execution.containment-preflight-attestation.v2\x00"
	protectedBrokerPreflightHashDomainV2                = "planescape.execution.containment-preflight.v2\x00"
	protectedBrokerResourceEvidenceSnapshotHashDomainV2 = "planescape.execution.resource-evidence-snapshot.v2\x00"
	protectedBrokerManifestSignatureDomainV2            = "planescape.execution.tool-transcript-manifest-attestation.v2\x00"
	protectedBrokerManifestHashDomainV2                 = "planescape.execution.tool-transcript-manifest.v2\x00"
	protectedBrokerTranscriptStateExplicitZeroV1        = "explicit_zero"
)

type protectedBrokerLeasePhaseV1 string

const (
	protectedBrokerLeaseAdmissionPendingV1  protectedBrokerLeasePhaseV1 = "admission_pending"
	protectedBrokerLeaseActiveV1            protectedBrokerLeasePhaseV1 = "active"
	protectedBrokerLeaseToolPendingV1       protectedBrokerLeasePhaseV1 = "tool_pending"
	protectedBrokerLeaseFencedV1            protectedBrokerLeasePhaseV1 = "fenced"
	protectedBrokerLeaseQuiescencePendingV1 protectedBrokerLeasePhaseV1 = "quiescence_pending"
	protectedBrokerLeaseQuiescentV1         protectedBrokerLeasePhaseV1 = "quiescent"
	protectedBrokerLeaseFrozenV1            protectedBrokerLeasePhaseV1 = "frozen"
	protectedBrokerLeaseClosedV1            protectedBrokerLeasePhaseV1 = "closed"
)

func (v protectedBrokerLeasePhaseV1) valid() bool {
	switch v {
	case protectedBrokerLeaseAdmissionPendingV1,
		protectedBrokerLeaseActiveV1,
		protectedBrokerLeaseToolPendingV1,
		protectedBrokerLeaseFencedV1,
		protectedBrokerLeaseQuiescencePendingV1,
		protectedBrokerLeaseQuiescentV1,
		protectedBrokerLeaseFrozenV1,
		protectedBrokerLeaseClosedV1:
		return true
	default:
		return false
	}
}

type protectedBrokerAdmissionEvidenceSuccessWireV1 struct {
	Outcome                      string                      `json:"outcome"`
	LeaseID                      string                      `json:"lease_id"`
	LeaseRevision                uint64                      `json:"lease_revision"`
	LeasePhase                   protectedBrokerLeasePhaseV1 `json:"lease_phase"`
	LeaseStateSHA256             protectedBrokerHashV1       `json:"lease_state_sha256"`
	PreflightEvidenceSHA256      protectedBrokerHashV1       `json:"preflight_evidence_sha256"`
	InitialManifestSHA256        protectedBrokerHashV1       `json:"initial_manifest_sha256"`
	PreflightJSONBase64URL       string                      `json:"preflight_json_b64"`
	InitialManifestJSONBase64URL string                      `json:"initial_manifest_json_b64"`
}

type protectedBrokerAdmissionEvidenceFailureWireV1 struct {
	Outcome string                        `json:"outcome"`
	Class   protectedBrokerFailureClassV1 `json:"class"`
}

type protectedBrokerAdmissionResponsePayloadWireV1 struct {
	historical bool
	success    *protectedBrokerAdmissionEvidenceSuccessWireV1
	failure    *protectedBrokerFailureClassV1
}

func (v protectedBrokerAdmissionResponsePayloadWireV1) MarshalJSON() ([]byte, error) {
	if v.success != nil && v.failure == nil {
		wire := *v.success
		if v.historical {
			wire.Outcome = protectedBrokerProviderAdmissionHistoricalV1
		} else {
			wire.Outcome = protectedBrokerProviderAdmissionCurrentV1
		}
		return json.Marshal(wire)
	}
	if v.success == nil && v.failure != nil && v.failure.valid() {
		return json.Marshal(protectedBrokerAdmissionEvidenceFailureWireV1{
			Outcome: protectedBrokerProviderAdmissionFailureV1,
			Class:   *v.failure,
		})
	}
	return nil, errors.New("invalid protected broker admission evidence payload")
}

func (v *protectedBrokerAdmissionResponsePayloadWireV1) UnmarshalJSON(
	encoded []byte,
) error {
	if v == nil {
		return errors.New("invalid protected broker admission evidence payload")
	}
	var discriminator struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(encoded, &discriminator); err != nil {
		return errors.New("invalid protected broker admission evidence payload")
	}
	switch discriminator.Outcome {
	case protectedBrokerProviderAdmissionCurrentV1,
		protectedBrokerProviderAdmissionHistoricalV1:
		var wire protectedBrokerAdmissionEvidenceSuccessWireV1
		if err := decodeProtectedBrokerStrictJSONObjectV1(encoded, &wire); err != nil ||
			!wire.LeasePhase.valid() ||
			!wire.LeaseStateSHA256.valid() ||
			!wire.PreflightEvidenceSHA256.valid() ||
			!wire.InitialManifestSHA256.valid() ||
			wire.PreflightJSONBase64URL == "" ||
			wire.InitialManifestJSONBase64URL == "" {
			return errors.New("invalid protected broker admission evidence payload")
		}
		*v = protectedBrokerAdmissionResponsePayloadWireV1{
			historical: discriminator.Outcome == protectedBrokerProviderAdmissionHistoricalV1,
			success:    &wire,
		}
		return nil
	case protectedBrokerProviderAdmissionFailureV1:
		var wire protectedBrokerAdmissionEvidenceFailureWireV1
		if err := decodeProtectedBrokerStrictJSONObjectV1(encoded, &wire); err != nil ||
			!wire.Class.valid() {
			return errors.New("invalid protected broker admission evidence payload")
		}
		class := wire.Class
		*v = protectedBrokerAdmissionResponsePayloadWireV1{failure: &class}
		return nil
	default:
		return errors.New("invalid protected broker admission evidence payload")
	}
}

type protectedBrokerAdmissionEvidenceV1 struct {
	historical            bool
	leaseID               Identifier
	leaseRevision         uint64
	leasePhase            protectedBrokerLeasePhaseV1
	leaseStateSHA256      protectedBrokerHashV1
	preflightSHA256       protectedBrokerHashV1
	initialManifestSHA256 protectedBrokerHashV1
}

func (v protectedBrokerAdmissionResponsePayloadWireV1) intoValidated(
	expectedHistorical bool,
	plan CompiledContainmentPlan,
	expectedBackend ProtectedBrokerBackendIdentityV1,
	brokerKey ed25519.PublicKey,
) (protectedBrokerAdmissionEvidenceV1, error) {
	if v.failure != nil || v.success == nil || v.historical != expectedHistorical {
		return protectedBrokerAdmissionEvidenceV1{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	leaseID, err := NewIdentifier(v.success.LeaseID)
	if err != nil {
		return protectedBrokerAdmissionEvidenceV1{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	preflightJSON, err := decodeProtectedBrokerProviderRecordV1(
		v.success.PreflightJSONBase64URL,
	)
	if err != nil {
		return protectedBrokerAdmissionEvidenceV1{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	manifestJSON, err := decodeProtectedBrokerProviderRecordV1(
		v.success.InitialManifestJSONBase64URL,
	)
	if err != nil {
		return protectedBrokerAdmissionEvidenceV1{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	preflight, err := validateProtectedBrokerContainmentPreflightV2(
		preflightJSON,
		plan,
		expectedBackend,
		brokerKey,
	)
	if err != nil ||
		preflight.EvidenceSHA256 != v.success.PreflightEvidenceSHA256 ||
		preflight.LeaseID != leaseID.String() {
		return protectedBrokerAdmissionEvidenceV1{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	manifest, err := validateProtectedBrokerInitialManifestV2(
		manifestJSON,
		preflight,
		brokerKey,
	)
	if err != nil || manifest.ManifestSHA256 != v.success.InitialManifestSHA256 {
		return protectedBrokerAdmissionEvidenceV1{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return protectedBrokerAdmissionEvidenceV1{
		historical:            v.historical,
		leaseID:               leaseID,
		leaseRevision:         v.success.LeaseRevision,
		leasePhase:            v.success.LeasePhase,
		leaseStateSHA256:      v.success.LeaseStateSHA256,
		preflightSHA256:       preflight.EvidenceSHA256,
		initialManifestSHA256: manifest.ManifestSHA256,
	}, nil
}

type protectedBrokerContainmentBackendIdentityWireV1 struct {
	Backend                    string                 `json:"backend"`
	BackendInstanceSHA256      protectedBrokerHashV1  `json:"backend_instance_sha256"`
	ExecutableSHA256           protectedBrokerHashV1  `json:"executable_sha256"`
	ExecutionEnvironmentSHA256 protectedBrokerHashV1  `json:"execution_environment_sha256"`
	ProfileSHA256              protectedBrokerHashV1  `json:"profile_sha256"`
	Epoch                      protectedBrokerEpochV1 `json:"epoch"`
	AttestorPublicKeySHA256    protectedBrokerHashV1  `json:"attestor_public_key_sha256"`
	IdentitySHA256             protectedBrokerHashV1  `json:"identity_sha256"`
}

type protectedBrokerNativeResourceCounterV2 struct {
	HighWater   uint64 `json:"high_water"`
	LimitEvents uint64 `json:"limit_events"`
}

type protectedBrokerCPUBandwidthCounterV2 struct {
	ThrottledUS      uint64 `json:"throttled_us"`
	ThrottledPeriods uint64 `json:"throttled_periods"`
}

type protectedBrokerAggregateOpenFilesProofV2 struct {
	TasksMax                    uint64 `json:"tasks_max"`
	NofilePerProcess            uint64 `json:"nofile_per_process"`
	RequestedAggregateOpenFiles uint64 `json:"requested_aggregate_open_files"`
	EnforcedCeiling             uint64 `json:"enforced_ceiling"`
}

type protectedBrokerWorkspaceAllocatedBytesSnapshotV2 struct {
	CurrentAllocatedBytes uint64 `json:"current_allocated_bytes"`
	HardLimitBytes        uint64 `json:"hard_limit_bytes"`
}

type protectedBrokerWorkspaceInodesSnapshotV2 struct {
	CurrentInodes   uint64 `json:"current_inodes"`
	HardLimitInodes uint64 `json:"hard_limit_inodes"`
}

type protectedBrokerLogicalFileSizeSnapshotV2 struct {
	CurrentMaxFileBytes uint64 `json:"current_max_file_bytes"`
	RlimitFsizeBytes    uint64 `json:"rlimit_fsize_bytes"`
}

type protectedBrokerCgroupEvidenceSourceV2 struct {
	CgroupSHA256 protectedBrokerHashV1 `json:"cgroup_sha256"`
}

type protectedBrokerQuotaEvidenceSourceV2 struct {
	QuotaProjectSHA256  protectedBrokerHashV1 `json:"quota_project_sha256"`
	WorkspaceRootSHA256 protectedBrokerHashV1 `json:"workspace_root_sha256"`
}

type protectedBrokerWorkerLimitEvidenceSourceV2 struct {
	CgroupSHA256         protectedBrokerHashV1 `json:"cgroup_sha256"`
	BackendProfileSHA256 protectedBrokerHashV1 `json:"backend_profile_sha256"`
}

type protectedBrokerLogicalScannerEvidenceSourceV2 struct {
	WorkspaceRootSHA256   protectedBrokerHashV1 `json:"workspace_root_sha256"`
	BackendProfileSHA256  protectedBrokerHashV1 `json:"backend_profile_sha256"`
	ScannerSnapshotSHA256 protectedBrokerHashV1 `json:"scanner_snapshot_sha256"`
}

type protectedBrokerResourceEvidenceSourcesV2 struct {
	Cgroup         protectedBrokerCgroupEvidenceSourceV2         `json:"cgroup"`
	Quota          protectedBrokerQuotaEvidenceSourceV2          `json:"quota"`
	WorkerLimits   protectedBrokerWorkerLimitEvidenceSourceV2    `json:"worker_limits"`
	LogicalScanner protectedBrokerLogicalScannerEvidenceSourceV2 `json:"logical_scanner"`
}

type protectedBrokerAggregateResourceEvidenceV2 struct {
	Sources                 protectedBrokerResourceEvidenceSourcesV2         `json:"sources"`
	MemoryBytes             protectedBrokerNativeResourceCounterV2           `json:"memory_bytes"`
	SwapBytes               protectedBrokerNativeResourceCounterV2           `json:"swap_bytes"`
	Tasks                   protectedBrokerNativeResourceCounterV2           `json:"tasks"`
	CPUBandwidth            protectedBrokerCPUBandwidthCounterV2             `json:"cpu_bandwidth"`
	AggregateOpenFiles      protectedBrokerAggregateOpenFilesProofV2         `json:"aggregate_open_files"`
	WorkspaceAllocatedBytes protectedBrokerWorkspaceAllocatedBytesSnapshotV2 `json:"workspace_allocated_bytes"`
	WorkspaceInodes         protectedBrokerWorkspaceInodesSnapshotV2         `json:"workspace_inodes"`
	LogicalFileSize         protectedBrokerLogicalFileSizeSnapshotV2         `json:"logical_file_size"`
}

type protectedBrokerResourceEvidenceSnapshotV2 struct {
	Schema         string                                     `json:"schema"`
	ObservedAtMS   uint64                                     `json:"observed_at_ms"`
	Evidence       protectedBrokerAggregateResourceEvidenceV2 `json:"evidence"`
	SnapshotSHA256 protectedBrokerHashV1                      `json:"snapshot_sha256"`
}

type protectedBrokerContainmentRealizationV1 struct {
	BackendSessionSHA256           protectedBrokerHashV1                     `json:"backend_session_sha256"`
	MountNamespaceSHA256           protectedBrokerHashV1                     `json:"mount_namespace_sha256"`
	PIDNamespaceSHA256             protectedBrokerHashV1                     `json:"pid_namespace_sha256"`
	UserNamespaceSHA256            protectedBrokerHashV1                     `json:"user_namespace_sha256"`
	NetworkNamespaceSHA256         protectedBrokerHashV1                     `json:"network_namespace_sha256"`
	CgroupSHA256                   protectedBrokerHashV1                     `json:"cgroup_sha256"`
	QuotaProjectSHA256             protectedBrokerHashV1                     `json:"quota_project_sha256"`
	WorkspaceRootSHA256            protectedBrokerHashV1                     `json:"workspace_root_sha256"`
	EvidenceStoreSHA256            protectedBrokerHashV1                     `json:"evidence_store_sha256"`
	InitialCandidateManifestSHA256 protectedBrokerHashV1                     `json:"initial_candidate_manifest_sha256"`
	InitialResourceEvidence        protectedBrokerResourceEvidenceSnapshotV2 `json:"initial_resource_evidence"`
	MinimumWorkerNofile            uint64                                    `json:"minimum_worker_nofile"`
	EffectiveNofilePerProcess      uint64                                    `json:"effective_nofile_per_process"`
}

type protectedBrokerContainmentPreflightWireV2 struct {
	Schema                   string                                          `json:"schema"`
	Scope                    providerV1ContainmentScopeDTO                   `json:"scope"`
	Contract                 providerV1ContainmentContractDTO                `json:"contract"`
	LeaseRequestSHA256       protectedBrokerHashV1                           `json:"lease_request_sha256"`
	BackendIdentity          protectedBrokerContainmentBackendIdentityWireV1 `json:"backend_identity"`
	Realization              protectedBrokerContainmentRealizationV1         `json:"realization"`
	LeaseID                  string                                          `json:"lease_id"`
	TranscriptZeroHeadSHA256 protectedBrokerHashV1                           `json:"transcript_zero_head_sha256"`
	Signature                string                                          `json:"signature"`
	EvidenceSHA256           protectedBrokerHashV1                           `json:"evidence_sha256"`
}

func validateProtectedBrokerContainmentPreflightV2(
	encoded []byte,
	plan CompiledContainmentPlan,
	expectedBackend ProtectedBrokerBackendIdentityV1,
	brokerKey ed25519.PublicKey,
) (protectedBrokerContainmentPreflightWireV2, error) {
	if len(encoded) == 0 || len(encoded) > MaxRecordBytes ||
		!plan.valid() || !expectedBackend.valid() ||
		len(brokerKey) != ed25519.PublicKeySize {
		return protectedBrokerContainmentPreflightWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	var request providerV1ContainmentLeaseRequestDTO
	if err := decodeProtectedBrokerStrictJSONObjectV1(
		plan.ContainmentRequestJSON(),
		&request,
	); err != nil {
		return protectedBrokerContainmentPreflightWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	var preflight protectedBrokerContainmentPreflightWireV2
	if err := decodeProtectedBrokerStrictJSONObjectV1(encoded, &preflight); err != nil {
		return protectedBrokerContainmentPreflightWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	canonical, err := json.Marshal(preflight)
	if err != nil || !bytes.Equal(canonical, encoded) ||
		preflight.Schema != protectedBrokerContainmentPreflightSchemaV2 ||
		preflight.Scope != request.Scope ||
		preflight.Contract != request.Contract ||
		preflight.LeaseRequestSHA256.String() != request.RequestSHA256 {
		return protectedBrokerContainmentPreflightWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	if _, err := NewIdentifier(preflight.LeaseID); err != nil {
		return protectedBrokerContainmentPreflightWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	if !validateProtectedBrokerEvidenceBackendV1(
		preflight.BackendIdentity,
		expectedBackend,
		brokerKey,
	) || !validateProtectedBrokerRealizationV1(
		preflight.Realization,
		request.Contract.Resources,
		expectedBackend,
	) {
		return protectedBrokerContainmentPreflightWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	expectedZero, err := hashProtectedBrokerJSONV1(
		protectedBrokerTranscriptZeroHashDomainV2,
		request.Scope.ScopeSHA256,
		request.Contract.ContractSHA256,
		expectedBackend.identitySHA256,
		preflight.LeaseID,
	)
	if err != nil || preflight.TranscriptZeroHeadSHA256 != expectedZero {
		return protectedBrokerContainmentPreflightWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	expectedEvidence, err := hashProtectedBrokerJSONV1(
		protectedBrokerPreflightHashDomainV2,
		preflight.Schema,
		preflight.Scope,
		preflight.Contract,
		preflight.LeaseRequestSHA256,
		preflight.BackendIdentity,
		preflight.Realization,
		preflight.LeaseID,
		preflight.TranscriptZeroHeadSHA256,
		preflight.Signature,
	)
	if err != nil || preflight.EvidenceSHA256 != expectedEvidence {
		return protectedBrokerContainmentPreflightWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerPreflightSignatureDomainV2,
		preflight.Signature,
		brokerKey,
		preflight.Schema,
		preflight.Scope,
		preflight.Contract,
		preflight.LeaseRequestSHA256,
		preflight.BackendIdentity,
		preflight.Realization,
		preflight.LeaseID,
		preflight.TranscriptZeroHeadSHA256,
	); err != nil {
		return protectedBrokerContainmentPreflightWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return preflight, nil
}

func validateProtectedBrokerEvidenceBackendV1(
	actual protectedBrokerContainmentBackendIdentityWireV1,
	expected ProtectedBrokerBackendIdentityV1,
	brokerKey ed25519.PublicKey,
) bool {
	if actual.Backend != protectedBrokerBackendKindV1 ||
		actual.BackendInstanceSHA256 != expected.backendInstanceSHA256 ||
		actual.ExecutableSHA256 != expected.executableSHA256 ||
		actual.ExecutionEnvironmentSHA256 != expected.executionEnvironmentSHA256 ||
		actual.ProfileSHA256 != expected.profileSHA256 ||
		actual.Epoch != expected.brokerEpoch ||
		actual.AttestorPublicKeySHA256 != expected.attestorPublicKeySHA256 ||
		actual.IdentitySHA256 != expected.identitySHA256 ||
		protectedBrokerRawHashV1(brokerKey) != actual.AttestorPublicKeySHA256 {
		return false
	}
	identitySHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerBackendIdentityHashDomainV1,
		actual.Backend,
		actual.BackendInstanceSHA256,
		actual.ExecutableSHA256,
		actual.ExecutionEnvironmentSHA256,
		actual.ProfileSHA256,
		actual.Epoch,
		actual.AttestorPublicKeySHA256,
	)
	return err == nil && identitySHA256 == actual.IdentitySHA256
}

func validateProtectedBrokerRealizationV1(
	realization protectedBrokerContainmentRealizationV1,
	limits providerV1ResourceLimitsDTO,
	backend ProtectedBrokerBackendIdentityV1,
) bool {
	if realization.MinimumWorkerNofile == 0 ||
		realization.EffectiveNofilePerProcess == 0 ||
		limits.Tasks == 0 {
		return false
	}
	maximumNofile := limits.AggregateOpenFiles / limits.Tasks
	if realization.EffectiveNofilePerProcess != maximumNofile ||
		realization.EffectiveNofilePerProcess < realization.MinimumWorkerNofile {
		return false
	}
	return validateProtectedBrokerResourceEvidenceSnapshotV2(
		realization.InitialResourceEvidence,
		realization,
		limits,
		backend,
	)
}

func validateProtectedBrokerResourceEvidenceSnapshotV2(
	snapshot protectedBrokerResourceEvidenceSnapshotV2,
	realization protectedBrokerContainmentRealizationV1,
	limits providerV1ResourceLimitsDTO,
	backend ProtectedBrokerBackendIdentityV1,
) bool {
	if snapshot.Schema != protectedBrokerResourceEvidenceSnapshotSchemaV2 {
		return false
	}
	expectedHash, err := hashProtectedBrokerJSONV1(
		protectedBrokerResourceEvidenceSnapshotHashDomainV2,
		snapshot.Schema,
		snapshot.ObservedAtMS,
		snapshot.Evidence,
	)
	if err != nil || snapshot.SnapshotSHA256 != expectedHash {
		return false
	}
	evidence := snapshot.Evidence
	sources := evidence.Sources
	if sources.Cgroup.CgroupSHA256 != realization.CgroupSHA256 ||
		sources.Quota.QuotaProjectSHA256 != realization.QuotaProjectSHA256 ||
		sources.Quota.WorkspaceRootSHA256 != realization.WorkspaceRootSHA256 ||
		sources.WorkerLimits.CgroupSHA256 != realization.CgroupSHA256 ||
		sources.WorkerLimits.BackendProfileSHA256 != backend.profileSHA256 ||
		sources.LogicalScanner.WorkspaceRootSHA256 != realization.WorkspaceRootSHA256 ||
		sources.LogicalScanner.BackendProfileSHA256 != backend.profileSHA256 {
		return false
	}
	openFiles := evidence.AggregateOpenFiles
	if openFiles.TasksMax == 0 ||
		openFiles.NofilePerProcess == 0 ||
		openFiles.RequestedAggregateOpenFiles == 0 ||
		openFiles.TasksMax > math.MaxUint64/openFiles.NofilePerProcess {
		return false
	}
	enforcedCeiling := openFiles.TasksMax * openFiles.NofilePerProcess
	if openFiles.EnforcedCeiling != enforcedCeiling ||
		enforcedCeiling > openFiles.RequestedAggregateOpenFiles ||
		openFiles.TasksMax != limits.Tasks ||
		openFiles.NofilePerProcess != realization.EffectiveNofilePerProcess ||
		openFiles.RequestedAggregateOpenFiles != limits.AggregateOpenFiles {
		return false
	}
	allocated := evidence.WorkspaceAllocatedBytes
	inodes := evidence.WorkspaceInodes
	logical := evidence.LogicalFileSize
	return allocated.HardLimitBytes != 0 &&
		allocated.CurrentAllocatedBytes <= allocated.HardLimitBytes &&
		allocated.HardLimitBytes == limits.WorkspaceAllocatedBytes &&
		inodes.HardLimitInodes != 0 &&
		inodes.CurrentInodes <= inodes.HardLimitInodes &&
		inodes.HardLimitInodes == limits.WorkspaceInodes &&
		logical.RlimitFsizeBytes != 0 &&
		logical.CurrentMaxFileBytes <= logical.RlimitFsizeBytes &&
		logical.RlimitFsizeBytes == limits.LogicalFileBytes
}

type protectedBrokerExplicitZeroTranscriptStateV2 struct {
	State          string                `json:"state"`
	ZeroHeadSHA256 protectedBrokerHashV1 `json:"zero_head_sha256"`
}

type protectedBrokerToolTranscriptManifestWireV2 struct {
	Schema                  string                                       `json:"schema"`
	ScopeSHA256             protectedBrokerHashV1                        `json:"scope_sha256"`
	WorkspaceID             string                                       `json:"workspace_id"`
	PreflightEvidenceSHA256 protectedBrokerHashV1                        `json:"preflight_evidence_sha256"`
	BackendIdentitySHA256   protectedBrokerHashV1                        `json:"backend_identity_sha256"`
	BrokerEpoch             protectedBrokerEpochV1                       `json:"broker_epoch"`
	LeaseID                 string                                       `json:"lease_id"`
	State                   protectedBrokerExplicitZeroTranscriptStateV2 `json:"state"`
	Signature               string                                       `json:"signature"`
	ManifestSHA256          protectedBrokerHashV1                        `json:"manifest_sha256"`
}

func validateProtectedBrokerInitialManifestV2(
	encoded []byte,
	preflight protectedBrokerContainmentPreflightWireV2,
	brokerKey ed25519.PublicKey,
) (protectedBrokerToolTranscriptManifestWireV2, error) {
	if len(encoded) == 0 || len(encoded) > MaxRecordBytes ||
		len(brokerKey) != ed25519.PublicKeySize {
		return protectedBrokerToolTranscriptManifestWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	var manifest protectedBrokerToolTranscriptManifestWireV2
	if err := decodeProtectedBrokerStrictJSONObjectV1(encoded, &manifest); err != nil {
		return protectedBrokerToolTranscriptManifestWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(canonical, encoded) ||
		manifest.Schema != protectedBrokerToolTranscriptManifestSchemaV2 ||
		manifest.ScopeSHA256.String() != preflight.Scope.ScopeSHA256 ||
		manifest.WorkspaceID != preflight.Scope.WorkspaceID ||
		manifest.PreflightEvidenceSHA256 != preflight.EvidenceSHA256 ||
		manifest.BackendIdentitySHA256 != preflight.BackendIdentity.IdentitySHA256 ||
		manifest.BrokerEpoch != preflight.BackendIdentity.Epoch ||
		manifest.LeaseID != preflight.LeaseID ||
		manifest.State.State != protectedBrokerTranscriptStateExplicitZeroV1 ||
		manifest.State.ZeroHeadSHA256 != preflight.TranscriptZeroHeadSHA256 {
		return protectedBrokerToolTranscriptManifestWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	expectedHash, err := hashProtectedBrokerJSONV1(
		protectedBrokerManifestHashDomainV2,
		manifest.Schema,
		manifest.ScopeSHA256,
		manifest.WorkspaceID,
		manifest.PreflightEvidenceSHA256,
		manifest.BackendIdentitySHA256,
		manifest.BrokerEpoch,
		manifest.LeaseID,
		manifest.State,
		manifest.Signature,
	)
	if err != nil || manifest.ManifestSHA256 != expectedHash {
		return protectedBrokerToolTranscriptManifestWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerManifestSignatureDomainV2,
		manifest.Signature,
		brokerKey,
		manifest.Schema,
		manifest.ScopeSHA256,
		manifest.WorkspaceID,
		manifest.PreflightEvidenceSHA256,
		manifest.BackendIdentitySHA256,
		manifest.BrokerEpoch,
		manifest.LeaseID,
		manifest.State,
	); err != nil {
		return protectedBrokerToolTranscriptManifestWireV2{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return manifest, nil
}
