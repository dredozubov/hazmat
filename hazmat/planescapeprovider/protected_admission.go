package planescapeprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

const (
	protectedBrokerProviderAdmissionRequestSchemaV1  = "execution.protected-broker.provider-admission-request.v1"
	protectedBrokerProviderAdmissionResponseSchemaV1 = "execution.protected-broker.provider-admission-response.v1"

	protectedBrokerProviderAdmissionRequestPayloadHashDomainV1  = "planescape.execution.protected-broker.provider-admission-request-payload.v1\x00"
	protectedBrokerProviderAdmissionRequestHashDomainV1         = "planescape.execution.protected-broker.provider-admission-request.v1\x00"
	protectedBrokerProviderAdmissionRequestSignatureDomainV1    = "planescape.execution.protected-broker.provider-admission-request-signature.v1\x00"
	protectedBrokerProviderAdmissionResponsePayloadHashDomainV1 = "planescape.execution.protected-broker.provider-admission-response-payload.v1\x00"
	protectedBrokerProviderAdmissionResponseHashDomainV1        = "planescape.execution.protected-broker.provider-admission-response.v1\x00"
	protectedBrokerProviderAdmissionResponseSignatureDomainV1   = "planescape.execution.protected-broker.provider-admission-response-signature.v1\x00"

	protectedBrokerProviderAdmissionSequenceV1 = protectedBrokerRPCSequenceV1(1)
)

const (
	ProtectedBrokerReplayedSequenceV1 ProtectedBrokerTransportErrorClassV1 = "replayed_sequence"
	ProtectedBrokerInvalidEvidenceV1  ProtectedBrokerTransportErrorClassV1 = "invalid_evidence"
)

type protectedBrokerFailureClassV1 string

const (
	protectedBrokerFailureUnsupportedPlatformV1   protectedBrokerFailureClassV1 = "unsupported_platform"
	protectedBrokerFailureUnavailableAuthorityV1  protectedBrokerFailureClassV1 = "unavailable_authority"
	protectedBrokerFailureStaleIdentityV1         protectedBrokerFailureClassV1 = "stale_identity"
	protectedBrokerFailureInvalidContractV1       protectedBrokerFailureClassV1 = "invalid_contract"
	protectedBrokerFailureSourceMismatchV1        protectedBrokerFailureClassV1 = "source_mismatch"
	protectedBrokerFailureProtocolConflictV1      protectedBrokerFailureClassV1 = "protocol_conflict"
	protectedBrokerFailureResourceTripV1          protectedBrokerFailureClassV1 = "resource_trip"
	protectedBrokerFailureUnconfirmedQuiescenceV1 protectedBrokerFailureClassV1 = "unconfirmed_quiescence"
	protectedBrokerFailureEvidenceConflictV1      protectedBrokerFailureClassV1 = "evidence_conflict"
	protectedBrokerProviderAdmissionCurrentV1     string                        = "current"
	protectedBrokerProviderAdmissionHistoricalV1  string                        = "historical"
	protectedBrokerProviderAdmissionFailureV1     string                        = "failure"
)

func (v protectedBrokerFailureClassV1) valid() bool {
	switch v {
	case protectedBrokerFailureUnsupportedPlatformV1,
		protectedBrokerFailureUnavailableAuthorityV1,
		protectedBrokerFailureStaleIdentityV1,
		protectedBrokerFailureInvalidContractV1,
		protectedBrokerFailureSourceMismatchV1,
		protectedBrokerFailureProtocolConflictV1,
		protectedBrokerFailureResourceTripV1,
		protectedBrokerFailureUnconfirmedQuiescenceV1,
		protectedBrokerFailureEvidenceConflictV1:
		return true
	default:
		return false
	}
}

func (v protectedBrokerFailureClassV1) MarshalJSON() ([]byte, error) {
	if !v.valid() {
		return nil, errors.New("invalid protected broker failure class")
	}
	return json.Marshal(string(v))
}

func (v *protectedBrokerFailureClassV1) UnmarshalJSON(encoded []byte) error {
	if v == nil || bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
		return errors.New("invalid protected broker failure class")
	}
	var value string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return errors.New("invalid protected broker failure class")
	}
	parsed := protectedBrokerFailureClassV1(value)
	if !parsed.valid() {
		return errors.New("invalid protected broker failure class")
	}
	*v = parsed
	return nil
}

// ProtectedBrokerAdmissionTransportConfigV1 binds one reconnectable dialer to
// immutable protected-broker client authority.
type ProtectedBrokerAdmissionTransportConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

// ProtectedBrokerAdmissionTransportV1 performs one authenticated compiled-plan
// admission exchange per fresh connection. It retains no reusable transport
// session and exposes no requirement-only admission path.
type ProtectedBrokerAdmissionTransportV1 struct {
	dialer ProtectedBrokerDialerV1
	client *ProtectedBrokerClientV1
}

func NewProtectedBrokerAdmissionTransportV1(
	config ProtectedBrokerAdmissionTransportConfigV1,
) (*ProtectedBrokerAdmissionTransportV1, error) {
	if config.Dialer == nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if config.Client == nil || !config.Client.valid() {
		return nil, protectedBrokerError(ProtectedBrokerWrongBrokerIdentityV1)
	}
	return &ProtectedBrokerAdmissionTransportV1{
		dialer: config.Dialer,
		client: config.Client,
	}, nil
}

func (t *ProtectedBrokerAdmissionTransportV1) Admit(
	ctx context.Context,
	plan CompiledContainmentPlan,
) (admission SessionAdmission, returnErr error) {
	if t == nil || t.dialer == nil || t.client == nil || !t.client.valid() {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if ctx == nil {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	planJSON, err := validateProtectedBrokerAdmissionPlanV1(t.client, plan)
	if err != nil {
		return SessionAdmission{}, err
	}
	if ctx.Err() != nil {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}

	rawStream, err := t.dialer.DialContext(ctx)
	if err != nil {
		return SessionAdmission{}, mapProtectedBrokerDiscoveryIOError(ctx, err)
	}
	if rawStream == nil {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	stream := &managedProtectedBrokerStreamV1{stream: rawStream}
	stopContext := func() {}
	defer func() {
		stopContext()
		if closeErr := stream.Close(); returnErr == nil && closeErr != nil {
			admission = SessionAdmission{}
			returnErr = mapProtectedBrokerDiscoveryIOError(ctx, closeErr)
		}
	}()

	stopContext, err = bindProtectedBrokerStreamContextV1(ctx, stream)
	if err != nil {
		return SessionAdmission{}, err
	}
	session, err := t.client.Authenticate(stream)
	if err != nil {
		return SessionAdmission{}, mapProtectedBrokerDiscoverySessionError(ctx, err)
	}
	request, err := newProtectedBrokerProviderAdmissionRequestV1(
		t.client,
		session,
		protectedBrokerProviderAdmissionSequenceV1,
		plan,
		planJSON,
	)
	if err != nil {
		return SessionAdmission{}, err
	}
	if err := writeProtectedBrokerRPCJSONFrameV1(stream, request); err != nil {
		return SessionAdmission{}, mapProtectedBrokerDiscoverySessionError(ctx, err)
	}

	var response protectedBrokerProviderAdmissionResponseWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(stream, &response); err != nil {
		return SessionAdmission{}, mapProtectedBrokerDiscoverySessionError(ctx, err)
	}
	admission, err = response.validate(
		t.client,
		session,
		protectedBrokerProviderAdmissionSequenceV1,
		request.RequestSHA256,
		plan,
	)
	if err != nil {
		return SessionAdmission{}, err
	}
	if ctx.Err() != nil {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return admission, nil
}

func validateProtectedBrokerAdmissionPlanV1(
	client *ProtectedBrokerClientV1,
	plan CompiledContainmentPlan,
) ([]byte, error) {
	if client == nil || !client.valid() || !plan.valid() {
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	expectedAuthority, err := protectedBrokerClientAuthorityHashV1(
		client.clientKeySHA256,
		client.expectedBackend,
	)
	if err != nil {
		return nil, err
	}
	if plan.AuthorityHash().String() != expectedAuthority.String() {
		return nil, protectedBrokerError(ProtectedBrokerClientAuthorityMismatchV1)
	}
	capabilities, err := protectedBrokerStockLinuxCapabilitiesV1(client.expectedBackend)
	if err != nil {
		return nil, err
	}
	if err := plan.ValidateProvider(capabilities); err != nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	planJSON, err := (ProviderV1FrameCodec{}).EncodeCompiledContainmentPlan(plan)
	if err != nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	return planJSON, nil
}

func protectedBrokerStockLinuxCapabilitiesV1(
	backend ProtectedBrokerBackendIdentityV1,
) (ProviderCapabilities, error) {
	if !backend.valid() {
		return ProviderCapabilities{}, protectedBrokerError(ProtectedBrokerServiceBindingMismatchV1)
	}
	dto := providerV1CapabilitiesDTO{
		Schema:          providerV1SchemaCapabilities,
		ProviderID:      backend.identitySHA256.String(),
		ProviderEpoch:   uint64(backend.brokerEpoch),
		ProfileID:       string(ProfileStockLinux),
		ProtocolVersion: ProtocolVersionV1,
		Capabilities: []string{
			string(CapabilityArtifactRead),
			string(CapabilityToolExecute),
			string(CapabilityWorkspaceRead),
			string(CapabilityWorkspaceWrite),
		},
		ResourceDimensions: []string{
			string(ResourceCPUTime),
			string(ResourceMemoryBytes),
			string(ResourceOpenFiles),
			string(ResourceProcessCount),
			string(ResourceWorkspaceBytes),
			string(ResourceWorkspaceEntries),
		},
	}
	capabilityPreimage, err := dto.capabilitySetPreimage()
	if err != nil {
		return ProviderCapabilities{}, protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	dto.CapabilityHash = providerV1CanonicalHash(capabilityPreimage)
	canonicalPreimage, err := dto.canonicalPreimage()
	if err != nil {
		return ProviderCapabilities{}, protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	dto.CanonicalHash = providerV1CanonicalHash(canonicalPreimage)
	encoded, err := encodeProviderV1DTO(providerV1KindCapabilities, dto)
	if err != nil {
		return ProviderCapabilities{}, protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	capabilities, err := (ProviderV1FrameCodec{}).DecodeCapabilities(encoded)
	if err != nil {
		return ProviderCapabilities{}, protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	return capabilities, nil
}

type protectedBrokerProviderAdmissionRequestWireV1 struct {
	Schema                 string                       `json:"schema"`
	Sequence               protectedBrokerRPCSequenceV1 `json:"sequence"`
	ClientAuthoritySHA256  protectedBrokerHashV1        `json:"client_authority_sha256"`
	TransportSessionSHA256 protectedBrokerHashV1        `json:"transport_session_sha256"`
	BackendIdentitySHA256  protectedBrokerHashV1        `json:"backend_identity_sha256"`
	BrokerEpoch            protectedBrokerEpochV1       `json:"broker_epoch"`
	ProfileSHA256          protectedBrokerHashV1        `json:"profile_sha256"`
	CompiledPlanSHA256     protectedBrokerHashV1        `json:"compiled_plan_sha256"`
	RequestPayloadSHA256   protectedBrokerHashV1        `json:"request_payload_sha256"`
	CompiledPlanJSONBase64 string                       `json:"compiled_plan_json_b64"`
	RequestSHA256          protectedBrokerHashV1        `json:"request_sha256"`
	Signature              string                       `json:"signature"`
}

func newProtectedBrokerProviderAdmissionRequestV1(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
	sequence protectedBrokerRPCSequenceV1,
	plan CompiledContainmentPlan,
	planJSON []byte,
) (protectedBrokerProviderAdmissionRequestWireV1, error) {
	if client == nil || !client.valid() || sequence == 0 || !plan.valid() {
		return protectedBrokerProviderAdmissionRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	if err := validateProtectedBrokerProviderSessionV1(client, session); err != nil {
		return protectedBrokerProviderAdmissionRequestWireV1{}, err
	}
	expectedPlanJSON, err := validateProtectedBrokerAdmissionPlanV1(client, plan)
	if err != nil || !bytes.Equal(expectedPlanJSON, planJSON) {
		return protectedBrokerProviderAdmissionRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	compiledPlanSHA256, err := parseProtectedBrokerHashV1(plan.CanonicalHash().String())
	if err != nil {
		return protectedBrokerProviderAdmissionRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	requestPayloadSHA256 := hashProtectedBrokerBytesV1(
		protectedBrokerProviderAdmissionRequestPayloadHashDomainV1,
		planJSON,
	)
	requestSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderAdmissionRequestHashDomainV1,
		protectedBrokerProviderAdmissionRequestSchemaV1,
		sequence,
		session.clientAuthoritySHA256,
		session.transportSessionSHA256,
		session.backendIdentitySHA256,
		session.brokerEpoch,
		session.profileSHA256,
		compiledPlanSHA256,
		requestPayloadSHA256,
	)
	if err != nil {
		return protectedBrokerProviderAdmissionRequestWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerProviderAdmissionRequestSignatureDomainV1,
		client.clientKey,
		protectedBrokerProviderAdmissionRequestSchemaV1,
		sequence,
		session.clientAuthoritySHA256,
		session.transportSessionSHA256,
		session.backendIdentitySHA256,
		session.brokerEpoch,
		session.profileSHA256,
		compiledPlanSHA256,
		requestPayloadSHA256,
		requestSHA256,
	)
	if err != nil {
		return protectedBrokerProviderAdmissionRequestWireV1{}, err
	}
	return protectedBrokerProviderAdmissionRequestWireV1{
		Schema:                 protectedBrokerProviderAdmissionRequestSchemaV1,
		Sequence:               sequence,
		ClientAuthoritySHA256:  session.clientAuthoritySHA256,
		TransportSessionSHA256: session.transportSessionSHA256,
		BackendIdentitySHA256:  session.backendIdentitySHA256,
		BrokerEpoch:            session.brokerEpoch,
		ProfileSHA256:          session.profileSHA256,
		CompiledPlanSHA256:     compiledPlanSHA256,
		RequestPayloadSHA256:   requestPayloadSHA256,
		CompiledPlanJSONBase64: base64.RawURLEncoding.EncodeToString(planJSON),
		RequestSHA256:          requestSHA256,
		Signature:              signature,
	}, nil
}

type protectedBrokerProviderAdmissionResponseWireV1 struct {
	Schema                 string                                            `json:"schema"`
	Sequence               protectedBrokerRPCSequenceV1                      `json:"sequence"`
	ClientAuthoritySHA256  protectedBrokerHashV1                             `json:"client_authority_sha256"`
	TransportSessionSHA256 protectedBrokerHashV1                             `json:"transport_session_sha256"`
	BackendIdentitySHA256  protectedBrokerHashV1                             `json:"backend_identity_sha256"`
	BrokerEpoch            protectedBrokerEpochV1                            `json:"broker_epoch"`
	ProfileSHA256          protectedBrokerHashV1                             `json:"profile_sha256"`
	RequestSHA256          protectedBrokerHashV1                             `json:"request_sha256"`
	Payload                protectedBrokerProviderAdmissionResponsePayloadV1 `json:"payload"`
	ResponsePayloadSHA256  protectedBrokerHashV1                             `json:"response_payload_sha256"`
	ResponseSHA256         protectedBrokerHashV1                             `json:"response_sha256"`
	Signature              string                                            `json:"signature"`
}

func (v protectedBrokerProviderAdmissionResponseWireV1) validate(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
	expectedSequence protectedBrokerRPCSequenceV1,
	expectedRequestSHA256 protectedBrokerHashV1,
	plan CompiledContainmentPlan,
) (SessionAdmission, error) {
	if v.Schema != protectedBrokerProviderAdmissionResponseSchemaV1 {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerInvalidSchemaV1)
	}
	if v.Sequence != expectedSequence {
		if v.Sequence < expectedSequence {
			return SessionAdmission{}, protectedBrokerError(ProtectedBrokerReplayedSequenceV1)
		}
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerSequenceGapV1)
	}
	if v.ClientAuthoritySHA256 != session.clientAuthoritySHA256 {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerClientAuthorityMismatchV1)
	}
	if v.TransportSessionSHA256 != session.transportSessionSHA256 {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerTransportSessionMismatchV1)
	}
	if client == nil ||
		v.BackendIdentitySHA256 != client.expectedBackend.identitySHA256 ||
		v.BrokerEpoch != client.expectedBackend.brokerEpoch ||
		v.ProfileSHA256 != client.expectedBackend.profileSHA256 {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerServiceBindingMismatchV1)
	}
	if v.RequestSHA256 != expectedRequestSHA256 {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerRequestHashMismatchV1)
	}
	expectedPayloadSHA256, err := hashProtectedBrokerJSONValueV1(
		protectedBrokerProviderAdmissionResponsePayloadHashDomainV1,
		v.Payload,
	)
	if err != nil || v.ResponsePayloadSHA256 != expectedPayloadSHA256 {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	expectedResponseSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderAdmissionResponseHashDomainV1,
		protectedBrokerProviderAdmissionResponseSchemaV1,
		v.Sequence,
		v.ClientAuthoritySHA256,
		v.TransportSessionSHA256,
		v.BackendIdentitySHA256,
		v.BrokerEpoch,
		v.ProfileSHA256,
		v.RequestSHA256,
		v.ResponsePayloadSHA256,
	)
	if err != nil {
		return SessionAdmission{}, err
	}
	if v.ResponseSHA256 != expectedResponseSHA256 {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerProviderAdmissionResponseSignatureDomainV1,
		v.Signature,
		client.brokerKey,
		protectedBrokerProviderAdmissionResponseSchemaV1,
		v.Sequence,
		v.ClientAuthoritySHA256,
		v.TransportSessionSHA256,
		v.BackendIdentitySHA256,
		v.BrokerEpoch,
		v.ProfileSHA256,
		v.RequestSHA256,
		v.ResponsePayloadSHA256,
		v.ResponseSHA256,
	); err != nil {
		return SessionAdmission{}, err
	}
	return v.Payload.intoValidated(plan, client, v.ResponseSHA256)
}

type protectedBrokerProviderAdmissionSuccessWireV1 struct {
	Outcome                       string                                        `json:"outcome"`
	SessionAdmissionSHA256        protectedBrokerHashV1                         `json:"session_admission_sha256"`
	SessionAdmissionJSONBase64URL string                                        `json:"session_admission_json_b64"`
	Evidence                      protectedBrokerAdmissionResponsePayloadWireV1 `json:"evidence"`
}

type protectedBrokerProviderAdmissionFailureWireV1 struct {
	Outcome string                        `json:"outcome"`
	Class   protectedBrokerFailureClassV1 `json:"class"`
}

type protectedBrokerProviderAdmissionResponsePayloadV1 struct {
	historical bool
	success    *protectedBrokerProviderAdmissionSuccessWireV1
	failure    *protectedBrokerFailureClassV1
}

func (v protectedBrokerProviderAdmissionResponsePayloadV1) MarshalJSON() ([]byte, error) {
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
		return json.Marshal(protectedBrokerProviderAdmissionFailureWireV1{
			Outcome: protectedBrokerProviderAdmissionFailureV1,
			Class:   *v.failure,
		})
	}
	return nil, errors.New("invalid protected broker provider admission payload")
}

func (v *protectedBrokerProviderAdmissionResponsePayloadV1) UnmarshalJSON(
	encoded []byte,
) error {
	if v == nil {
		return errors.New("invalid protected broker provider admission payload")
	}
	var discriminator struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(encoded, &discriminator); err != nil {
		return errors.New("invalid protected broker provider admission payload")
	}
	switch discriminator.Outcome {
	case protectedBrokerProviderAdmissionCurrentV1,
		protectedBrokerProviderAdmissionHistoricalV1:
		var wire protectedBrokerProviderAdmissionSuccessWireV1
		if err := decodeProtectedBrokerStrictJSONObjectV1(encoded, &wire); err != nil ||
			!wire.SessionAdmissionSHA256.valid() ||
			wire.SessionAdmissionJSONBase64URL == "" {
			return errors.New("invalid protected broker provider admission payload")
		}
		*v = protectedBrokerProviderAdmissionResponsePayloadV1{
			historical: discriminator.Outcome == protectedBrokerProviderAdmissionHistoricalV1,
			success:    &wire,
		}
		return nil
	case protectedBrokerProviderAdmissionFailureV1:
		var wire protectedBrokerProviderAdmissionFailureWireV1
		if err := decodeProtectedBrokerStrictJSONObjectV1(encoded, &wire); err != nil ||
			!wire.Class.valid() {
			return errors.New("invalid protected broker provider admission payload")
		}
		class := wire.Class
		*v = protectedBrokerProviderAdmissionResponsePayloadV1{failure: &class}
		return nil
	default:
		return errors.New("invalid protected broker provider admission payload")
	}
}

func (v protectedBrokerProviderAdmissionResponsePayloadV1) intoValidated(
	plan CompiledContainmentPlan,
	client *ProtectedBrokerClientV1,
	responseSHA256 protectedBrokerHashV1,
) (SessionAdmission, error) {
	if v.failure != nil {
		return SessionAdmission{}, newProtectedBrokerProviderFailureV1(
			*v.failure,
			client.expectedBackend,
			responseSHA256,
		)
	}
	if v.success == nil {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	admissionJSON, err := decodeProtectedBrokerProviderRecordV1(
		v.success.SessionAdmissionJSONBase64URL,
	)
	if err != nil {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	admission, err := (ProviderV1FrameCodec{}).DecodeAdmission(admissionJSON)
	if err != nil {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	if admission.CanonicalHash().String() != v.success.SessionAdmissionSHA256.String() {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if err := plan.ValidateSessionAdmission(admission); err != nil {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	evidence, err := v.success.Evidence.intoValidated(
		v.historical,
		plan,
		client.expectedBackend,
		client.brokerKey,
	)
	if err != nil {
		return SessionAdmission{}, err
	}
	if evidence.leaseID != admission.SessionID() {
		return SessionAdmission{}, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return admission, nil
}

func newProtectedBrokerProviderFailureV1(
	class protectedBrokerFailureClassV1,
	backend ProtectedBrokerBackendIdentityV1,
	responseSHA256 protectedBrokerHashV1,
) error {
	code := ProviderErrorConflict
	switch class {
	case protectedBrokerFailureUnsupportedPlatformV1:
		code = ProviderErrorUnsupported
	case protectedBrokerFailureUnavailableAuthorityV1:
		code = ProviderErrorUnavailable
	case protectedBrokerFailureStaleIdentityV1:
		code = ProviderErrorStaleEpoch
	case protectedBrokerFailureInvalidContractV1:
		code = ProviderErrorInvalid
	case protectedBrokerFailureSourceMismatchV1,
		protectedBrokerFailureProtocolConflictV1,
		protectedBrokerFailureResourceTripV1,
		protectedBrokerFailureUnconfirmedQuiescenceV1,
		protectedBrokerFailureEvidenceConflictV1:
		code = ProviderErrorConflict
	default:
		return protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	failure, err := NewProviderFailure(ProviderFailureInput{
		Code:          code,
		ProviderID:    backend.identitySHA256.String(),
		ProviderEpoch: uint64(backend.brokerEpoch),
		RetryFrom:     TransitionAdmit,
		CanonicalHash: responseSHA256.String(),
	})
	if err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	return failure
}

func hashProtectedBrokerJSONValueV1(
	domain string,
	value any,
) (protectedBrokerHashV1, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	hasher := sha256.New()
	_, _ = io.WriteString(hasher, domain)
	_, _ = hasher.Write(encoded)
	return protectedBrokerHashV1("sha256:" + hex.EncodeToString(hasher.Sum(nil))), nil
}

func decodeProtectedBrokerStrictJSONObjectV1(encoded []byte, target any) error {
	policy, err := protectedJSONFieldPolicy(target)
	if err != nil {
		return err
	}
	if err := validateProtectedBrokerJSON(encoded, policy); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("protected broker JSON has trailing data")
	}
	return nil
}
