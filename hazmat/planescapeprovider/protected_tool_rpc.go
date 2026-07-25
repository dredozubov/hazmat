package planescapeprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
)

const (
	protectedBrokerProviderToolRequestSchemaV1  = "execution.protected-broker.provider-tool-request.v1"
	protectedBrokerProviderToolResponseSchemaV1 = "execution.protected-broker.provider-tool-response.v1"

	protectedBrokerProviderToolOperationPayloadHashDomainV1  = "planescape.execution.protected-broker.provider-tool-operation-payload.v1\x00"
	protectedBrokerProviderToolNormalizedPayloadHashDomainV1 = "planescape.execution.protected-broker.provider-tool-normalized-payload.v1\x00"
	protectedBrokerProviderToolRequestHashDomainV1           = "planescape.execution.protected-broker.provider-tool-request.v1\x00"
	protectedBrokerProviderToolRequestSignatureDomainV1      = "planescape.execution.protected-broker.provider-tool-request-signature.v1\x00"
	protectedBrokerProviderToolResponsePayloadHashDomainV1   = "planescape.execution.protected-broker.provider-tool-response-payload.v1\x00"
	protectedBrokerProviderToolResponseHashDomainV1          = "planescape.execution.protected-broker.provider-tool-response.v1\x00"
	protectedBrokerProviderToolResponseSignatureDomainV1     = "planescape.execution.protected-broker.provider-tool-response-signature.v1\x00"

	protectedBrokerProviderToolSequenceV1 = protectedBrokerRPCSequenceV1(1)
)

const (
	protectedBrokerProviderToolCurrentV1    = "current"
	protectedBrokerProviderToolHistoricalV1 = "historical"
	protectedBrokerProviderToolFailureV1    = "failure"
)

// ProtectedBrokerToolTransportConfigV1 binds one reconnectable dialer to
// immutable protected-broker client authority.
type ProtectedBrokerToolTransportConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

// ProtectedBrokerToolTransportV1 performs one authenticated Tool exchange per
// fresh connection. It retains no reusable transport session and accepts no
// non-Tool lifecycle operation.
type ProtectedBrokerToolTransportV1 struct {
	dialer ProtectedBrokerDialerV1
	client *ProtectedBrokerClientV1
}

func NewProtectedBrokerToolTransportV1(
	config ProtectedBrokerToolTransportConfigV1,
) (*ProtectedBrokerToolTransportV1, error) {
	if config.Dialer == nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if config.Client == nil || !config.Client.valid() {
		return nil, protectedBrokerError(ProtectedBrokerWrongBrokerIdentityV1)
	}
	return &ProtectedBrokerToolTransportV1{
		dialer: config.Dialer,
		client: config.Client,
	}, nil
}

func (t *ProtectedBrokerToolTransportV1) Operate(
	ctx context.Context,
	operation AgentOperation,
) (result OperationResult, returnErr error) {
	if t == nil || t.dialer == nil || t.client == nil || !t.client.valid() {
		return OperationResult{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if ctx == nil {
		return OperationResult{}, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	operationJSON, normalizedRecord, err :=
		validateProtectedBrokerToolOperationV1(t.client, operation)
	if err != nil {
		return OperationResult{}, err
	}
	if ctx.Err() != nil {
		return OperationResult{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}

	rawStream, err := t.dialer.DialContext(ctx)
	if err != nil {
		return OperationResult{}, mapProtectedBrokerDiscoveryIOError(ctx, err)
	}
	if rawStream == nil {
		return OperationResult{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	stream := &managedProtectedBrokerStreamV1{stream: rawStream}
	stopContext := func() {}
	defer func() {
		stopContext()
		if closeErr := stream.Close(); returnErr == nil && closeErr != nil {
			result = OperationResult{}
			returnErr = mapProtectedBrokerDiscoveryIOError(ctx, closeErr)
		}
	}()

	stopContext, err = bindProtectedBrokerStreamContextV1(ctx, stream)
	if err != nil {
		return OperationResult{}, err
	}
	session, err := t.client.Authenticate(stream)
	if err != nil {
		return OperationResult{},
			mapProtectedBrokerDiscoverySessionError(ctx, err)
	}
	request, err := newProtectedBrokerProviderToolRequestV1(
		t.client,
		session,
		protectedBrokerProviderToolSequenceV1,
		operation,
		operationJSON,
		normalizedRecord,
	)
	if err != nil {
		return OperationResult{}, err
	}
	if err := writeProtectedBrokerRPCJSONFrameV1(stream, request); err != nil {
		return OperationResult{},
			mapProtectedBrokerDiscoverySessionError(ctx, err)
	}

	var response protectedBrokerProviderToolResponseWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(stream, &response); err != nil {
		return OperationResult{},
			mapProtectedBrokerDiscoverySessionError(ctx, err)
	}
	result, err = response.validate(
		t.client,
		session,
		protectedBrokerProviderToolSequenceV1,
		request.RequestSHA256,
		operation,
	)
	if err != nil {
		return OperationResult{}, err
	}
	if ctx.Err() != nil {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return result, nil
}

func validateProtectedBrokerToolOperationV1(
	client *ProtectedBrokerClientV1,
	operation AgentOperation,
) ([]byte, []byte, error) {
	if client == nil || !client.valid() || !operation.dispatchableTool() {
		return nil, nil,
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	operationJSON, err := (ProviderV1FrameCodec{}).EncodeOperation(operation)
	if err != nil {
		return nil, nil,
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	normalizedRecord := operation.normalized.Bytes()
	if len(normalizedRecord) == 0 || len(normalizedRecord) > MaxRecordBytes {
		return nil, nil,
			protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	return operationJSON, normalizedRecord, nil
}

type protectedBrokerProviderToolRequestWireV1 struct {
	Schema                        string                       `json:"schema"`
	Sequence                      protectedBrokerRPCSequenceV1 `json:"sequence"`
	ClientAuthoritySHA256         protectedBrokerHashV1        `json:"client_authority_sha256"`
	TransportSessionSHA256        protectedBrokerHashV1        `json:"transport_session_sha256"`
	BackendIdentitySHA256         protectedBrokerHashV1        `json:"backend_identity_sha256"`
	BrokerEpoch                   protectedBrokerEpochV1       `json:"broker_epoch"`
	ProfileSHA256                 protectedBrokerHashV1        `json:"profile_sha256"`
	OperationSHA256               protectedBrokerHashV1        `json:"operation_sha256"`
	OperationPayloadSHA256        protectedBrokerHashV1        `json:"operation_payload_sha256"`
	NormalizedRecordPayloadSHA256 protectedBrokerHashV1        `json:"normalized_record_payload_sha256"`
	OperationJSONBase64URL        string                       `json:"operation_json_b64"`
	NormalizedRecordBase64URL     string                       `json:"normalized_record_b64"`
	RequestSHA256                 protectedBrokerHashV1        `json:"request_sha256"`
	Signature                     string                       `json:"signature"`
}

func newProtectedBrokerProviderToolRequestV1(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
	sequence protectedBrokerRPCSequenceV1,
	operation AgentOperation,
	operationJSON []byte,
	normalizedRecord []byte,
) (protectedBrokerProviderToolRequestWireV1, error) {
	if client == nil ||
		!client.valid() ||
		sequence == 0 ||
		!operation.dispatchableTool() {
		return protectedBrokerProviderToolRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	if err := validateProtectedBrokerProviderSessionV1(client, session); err != nil {
		return protectedBrokerProviderToolRequestWireV1{}, err
	}
	expectedOperationJSON, expectedNormalizedRecord, err :=
		validateProtectedBrokerToolOperationV1(client, operation)
	if err != nil ||
		!bytes.Equal(operationJSON, expectedOperationJSON) ||
		!bytes.Equal(normalizedRecord, expectedNormalizedRecord) {
		return protectedBrokerProviderToolRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	operationSHA256, err := parseProtectedBrokerHashV1(
		operation.CanonicalHash().String(),
	)
	if err != nil {
		return protectedBrokerProviderToolRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	operationPayloadSHA256 := hashProtectedBrokerBytesV1(
		protectedBrokerProviderToolOperationPayloadHashDomainV1,
		operationJSON,
	)
	normalizedRecordPayloadSHA256 := hashProtectedBrokerBytesV1(
		protectedBrokerProviderToolNormalizedPayloadHashDomainV1,
		normalizedRecord,
	)
	requestSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderToolRequestHashDomainV1,
		protectedBrokerProviderToolRequestSchemaV1,
		sequence,
		session.clientAuthoritySHA256,
		session.transportSessionSHA256,
		session.backendIdentitySHA256,
		session.brokerEpoch,
		session.profileSHA256,
		operationSHA256,
		operationPayloadSHA256,
		normalizedRecordPayloadSHA256,
	)
	if err != nil {
		return protectedBrokerProviderToolRequestWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerProviderToolRequestSignatureDomainV1,
		client.clientKey,
		protectedBrokerProviderToolRequestSchemaV1,
		sequence,
		session.clientAuthoritySHA256,
		session.transportSessionSHA256,
		session.backendIdentitySHA256,
		session.brokerEpoch,
		session.profileSHA256,
		operationSHA256,
		operationPayloadSHA256,
		normalizedRecordPayloadSHA256,
		requestSHA256,
	)
	if err != nil {
		return protectedBrokerProviderToolRequestWireV1{}, err
	}
	return protectedBrokerProviderToolRequestWireV1{
		Schema:                        protectedBrokerProviderToolRequestSchemaV1,
		Sequence:                      sequence,
		ClientAuthoritySHA256:         session.clientAuthoritySHA256,
		TransportSessionSHA256:        session.transportSessionSHA256,
		BackendIdentitySHA256:         session.backendIdentitySHA256,
		BrokerEpoch:                   session.brokerEpoch,
		ProfileSHA256:                 session.profileSHA256,
		OperationSHA256:               operationSHA256,
		OperationPayloadSHA256:        operationPayloadSHA256,
		NormalizedRecordPayloadSHA256: normalizedRecordPayloadSHA256,
		OperationJSONBase64URL:        base64.RawURLEncoding.EncodeToString(operationJSON),
		NormalizedRecordBase64URL:     base64.RawURLEncoding.EncodeToString(normalizedRecord),
		RequestSHA256:                 requestSHA256,
		Signature:                     signature,
	}, nil
}

type protectedBrokerProviderToolResponseWireV1 struct {
	Schema                 string                                       `json:"schema"`
	Sequence               protectedBrokerRPCSequenceV1                 `json:"sequence"`
	ClientAuthoritySHA256  protectedBrokerHashV1                        `json:"client_authority_sha256"`
	TransportSessionSHA256 protectedBrokerHashV1                        `json:"transport_session_sha256"`
	BackendIdentitySHA256  protectedBrokerHashV1                        `json:"backend_identity_sha256"`
	BrokerEpoch            protectedBrokerEpochV1                       `json:"broker_epoch"`
	ProfileSHA256          protectedBrokerHashV1                        `json:"profile_sha256"`
	RequestSHA256          protectedBrokerHashV1                        `json:"request_sha256"`
	Payload                protectedBrokerProviderToolResponsePayloadV1 `json:"payload"`
	ResponsePayloadSHA256  protectedBrokerHashV1                        `json:"response_payload_sha256"`
	ResponseSHA256         protectedBrokerHashV1                        `json:"response_sha256"`
	Signature              string                                       `json:"signature"`
}

func (v protectedBrokerProviderToolResponseWireV1) validate(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
	expectedSequence protectedBrokerRPCSequenceV1,
	expectedRequestSHA256 protectedBrokerHashV1,
	operation AgentOperation,
) (OperationResult, error) {
	if v.Schema != protectedBrokerProviderToolResponseSchemaV1 {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerInvalidSchemaV1)
	}
	if v.Sequence != expectedSequence {
		if v.Sequence < expectedSequence {
			return OperationResult{},
				protectedBrokerError(ProtectedBrokerReplayedSequenceV1)
		}
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerSequenceGapV1)
	}
	if v.ClientAuthoritySHA256 != session.clientAuthoritySHA256 {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerClientAuthorityMismatchV1)
	}
	if v.TransportSessionSHA256 != session.transportSessionSHA256 {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerTransportSessionMismatchV1)
	}
	if client == nil ||
		v.BackendIdentitySHA256 != client.expectedBackend.identitySHA256 ||
		v.BrokerEpoch != client.expectedBackend.brokerEpoch ||
		v.ProfileSHA256 != client.expectedBackend.profileSHA256 {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerServiceBindingMismatchV1)
	}
	if v.RequestSHA256 != expectedRequestSHA256 {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerRequestHashMismatchV1)
	}
	expectedPayloadSHA256, err := hashProtectedBrokerJSONValueV1(
		protectedBrokerProviderToolResponsePayloadHashDomainV1,
		v.Payload,
	)
	if err != nil || v.ResponsePayloadSHA256 != expectedPayloadSHA256 {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	expectedResponseSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderToolResponseHashDomainV1,
		protectedBrokerProviderToolResponseSchemaV1,
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
		return OperationResult{}, err
	}
	if v.ResponseSHA256 != expectedResponseSHA256 {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerProviderToolResponseSignatureDomainV1,
		v.Signature,
		client.brokerKey,
		protectedBrokerProviderToolResponseSchemaV1,
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
		return OperationResult{}, err
	}
	return v.Payload.intoValidated(
		operation,
		client,
		v.ResponseSHA256,
	)
}

type protectedBrokerProviderToolSuccessWireV1 struct {
	Outcome                      string                `json:"outcome"`
	OperationResultSHA256        protectedBrokerHashV1 `json:"operation_result_sha256"`
	OperationResultJSONBase64URL string                `json:"operation_result_json_b64"`
}

type protectedBrokerProviderToolFailureWireV1 struct {
	Outcome string                        `json:"outcome"`
	Class   protectedBrokerFailureClassV1 `json:"class"`
}

type protectedBrokerProviderToolResponsePayloadV1 struct {
	historical bool
	success    *protectedBrokerProviderToolSuccessWireV1
	failure    *protectedBrokerFailureClassV1
}

func (v protectedBrokerProviderToolResponsePayloadV1) MarshalJSON() ([]byte, error) {
	if v.success != nil && v.failure == nil {
		wire := *v.success
		if v.historical {
			wire.Outcome = protectedBrokerProviderToolHistoricalV1
		} else {
			wire.Outcome = protectedBrokerProviderToolCurrentV1
		}
		return json.Marshal(wire)
	}
	if v.success == nil && v.failure != nil && v.failure.valid() {
		return json.Marshal(protectedBrokerProviderToolFailureWireV1{
			Outcome: protectedBrokerProviderToolFailureV1,
			Class:   *v.failure,
		})
	}
	return nil, errors.New("invalid protected broker provider Tool payload")
}

func (v *protectedBrokerProviderToolResponsePayloadV1) UnmarshalJSON(
	encoded []byte,
) error {
	if v == nil {
		return errors.New("invalid protected broker provider Tool payload")
	}
	var discriminator struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(encoded, &discriminator); err != nil {
		return errors.New("invalid protected broker provider Tool payload")
	}
	switch discriminator.Outcome {
	case protectedBrokerProviderToolCurrentV1,
		protectedBrokerProviderToolHistoricalV1:
		var wire protectedBrokerProviderToolSuccessWireV1
		if err := decodeProtectedBrokerStrictJSONObjectV1(
			encoded,
			&wire,
		); err != nil ||
			!wire.OperationResultSHA256.valid() ||
			wire.OperationResultJSONBase64URL == "" {
			return errors.New("invalid protected broker provider Tool payload")
		}
		*v = protectedBrokerProviderToolResponsePayloadV1{
			historical: discriminator.Outcome ==
				protectedBrokerProviderToolHistoricalV1,
			success: &wire,
		}
		return nil
	case protectedBrokerProviderToolFailureV1:
		var wire protectedBrokerProviderToolFailureWireV1
		if err := decodeProtectedBrokerStrictJSONObjectV1(
			encoded,
			&wire,
		); err != nil ||
			!wire.Class.valid() {
			return errors.New("invalid protected broker provider Tool payload")
		}
		class := wire.Class
		*v = protectedBrokerProviderToolResponsePayloadV1{
			failure: &class,
		}
		return nil
	default:
		return errors.New("invalid protected broker provider Tool payload")
	}
}

func (v protectedBrokerProviderToolResponsePayloadV1) intoValidated(
	operation AgentOperation,
	client *ProtectedBrokerClientV1,
	responseSHA256 protectedBrokerHashV1,
) (OperationResult, error) {
	if v.failure != nil {
		return OperationResult{}, newProtectedBrokerProviderToolFailureV1(
			*v.failure,
			client.expectedBackend,
			responseSHA256,
		)
	}
	if v.success == nil {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	resultJSON, err := decodeProtectedBrokerProviderRecordV1(
		v.success.OperationResultJSONBase64URL,
	)
	if err != nil {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	response, err := (ProviderV1FrameCodec{}).DecodeOperation(resultJSON)
	if err != nil {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	result, ok := response.(OperationResult)
	if !ok ||
		result.CanonicalHash().String() !=
			v.success.OperationResultSHA256.String() {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if result.SessionID() != operation.SessionID() ||
		result.OperationID() != operation.OperationID() ||
		result.Sequence() != operation.Sequence() {
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	switch result.ResultKind() {
	case ResultCompleted, ResultCancelled, ResultFailed:
		return result, nil
	default:
		return OperationResult{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
}

func newProtectedBrokerProviderToolFailureV1(
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
		RetryFrom:     TransitionActivate,
		CanonicalHash: responseSHA256.String(),
	})
	if err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	return failure
}
