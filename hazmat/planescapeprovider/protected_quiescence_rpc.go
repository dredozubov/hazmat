package planescapeprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
)

const (
	protectedBrokerProviderQuiescenceRequestSchemaV1  = "execution.protected-broker.provider-quiescence-request.v1"
	protectedBrokerProviderQuiescenceResponseSchemaV1 = "execution.protected-broker.provider-quiescence-response.v1"

	protectedBrokerProviderQuiescenceOperationPayloadHashDomainV1 = "planescape.execution.protected-broker.provider-quiescence-operation-payload.v1\x00"
	protectedBrokerProviderQuiescenceRequestHashDomainV1          = "planescape.execution.protected-broker.provider-quiescence-request.v1\x00"
	protectedBrokerProviderQuiescenceRequestSignatureDomainV1     = "planescape.execution.protected-broker.provider-quiescence-request-signature.v1\x00"
	protectedBrokerProviderQuiescenceResponsePayloadHashDomainV1  = "planescape.execution.protected-broker.provider-quiescence-response-payload.v1\x00"
	protectedBrokerProviderQuiescenceResponseHashDomainV1         = "planescape.execution.protected-broker.provider-quiescence-response.v1\x00"
	protectedBrokerProviderQuiescenceResponseSignatureDomainV1    = "planescape.execution.protected-broker.provider-quiescence-response-signature.v1\x00"

	protectedBrokerProviderQuiescenceSequenceV1 = protectedBrokerRPCSequenceV1(1)
)

const (
	protectedBrokerProviderQuiescenceCurrentV1    = "current"
	protectedBrokerProviderQuiescenceHistoricalV1 = "historical"
	protectedBrokerProviderQuiescenceFailureV1    = "failure"
)

// ProtectedBrokerQuiescenceTransportConfigV1 binds one reconnectable dialer
// to immutable protected-broker client authority.
type ProtectedBrokerQuiescenceTransportConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

// ProtectedBrokerQuiescenceTransportV1 performs one authenticated Pause
// exchange per fresh connection. It retains no reusable transport session and
// accepts no non-Pause lifecycle operation.
type ProtectedBrokerQuiescenceTransportV1 struct {
	dialer ProtectedBrokerDialerV1
	client *ProtectedBrokerClientV1
}

func NewProtectedBrokerQuiescenceTransportV1(
	config ProtectedBrokerQuiescenceTransportConfigV1,
) (*ProtectedBrokerQuiescenceTransportV1, error) {
	if config.Dialer == nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if config.Client == nil || !config.Client.valid() {
		return nil, protectedBrokerError(ProtectedBrokerWrongBrokerIdentityV1)
	}
	return &ProtectedBrokerQuiescenceTransportV1{
		dialer: config.Dialer,
		client: config.Client,
	}, nil
}

func (t *ProtectedBrokerQuiescenceTransportV1) Operate(
	ctx context.Context,
	operation AgentOperation,
) (result Quiescence, returnErr error) {
	if t == nil || t.dialer == nil || t.client == nil || !t.client.valid() {
		return Quiescence{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if ctx == nil {
		return Quiescence{}, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	operationJSON, err := validateProtectedBrokerQuiescenceOperationV1(
		t.client,
		operation,
	)
	if err != nil {
		return Quiescence{}, err
	}
	if ctx.Err() != nil {
		return Quiescence{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}

	rawStream, err := t.dialer.DialContext(ctx)
	if err != nil {
		return Quiescence{}, mapProtectedBrokerDiscoveryIOError(ctx, err)
	}
	if rawStream == nil {
		return Quiescence{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	stream := &managedProtectedBrokerStreamV1{stream: rawStream}
	stopContext := func() {}
	defer func() {
		stopContext()
		if closeErr := stream.Close(); returnErr == nil && closeErr != nil {
			result = Quiescence{}
			returnErr = mapProtectedBrokerDiscoveryIOError(ctx, closeErr)
		}
	}()

	stopContext, err = bindProtectedBrokerStreamContextV1(ctx, stream)
	if err != nil {
		return Quiescence{}, err
	}
	session, err := t.client.Authenticate(stream)
	if err != nil {
		return Quiescence{},
			mapProtectedBrokerDiscoverySessionError(ctx, err)
	}
	request, err := newProtectedBrokerProviderQuiescenceRequestV1(
		t.client,
		session,
		protectedBrokerProviderQuiescenceSequenceV1,
		operation,
		operationJSON,
	)
	if err != nil {
		return Quiescence{}, err
	}
	if err := writeProtectedBrokerRPCJSONFrameV1(stream, request); err != nil {
		return Quiescence{},
			mapProtectedBrokerDiscoverySessionError(ctx, err)
	}

	var response protectedBrokerProviderQuiescenceResponseWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(stream, &response); err != nil {
		return Quiescence{},
			mapProtectedBrokerDiscoverySessionError(ctx, err)
	}
	result, err = response.validate(
		t.client,
		session,
		protectedBrokerProviderQuiescenceSequenceV1,
		request.RequestSHA256,
		operation,
	)
	if err != nil {
		return Quiescence{}, err
	}
	if ctx.Err() != nil {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return result, nil
}

func validateProtectedBrokerQuiescenceOperationV1(
	client *ProtectedBrokerClientV1,
	operation AgentOperation,
) ([]byte, error) {
	if client == nil || !client.valid() || !operation.dispatchablePause() {
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	operationJSON, err := (ProviderV1FrameCodec{}).EncodeOperation(operation)
	if err != nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	return operationJSON, nil
}

type protectedBrokerProviderQuiescenceRequestWireV1 struct {
	Schema                 string                       `json:"schema"`
	Sequence               protectedBrokerRPCSequenceV1 `json:"sequence"`
	ClientAuthoritySHA256  protectedBrokerHashV1        `json:"client_authority_sha256"`
	TransportSessionSHA256 protectedBrokerHashV1        `json:"transport_session_sha256"`
	BackendIdentitySHA256  protectedBrokerHashV1        `json:"backend_identity_sha256"`
	BrokerEpoch            protectedBrokerEpochV1       `json:"broker_epoch"`
	ProfileSHA256          protectedBrokerHashV1        `json:"profile_sha256"`
	OperationSHA256        protectedBrokerHashV1        `json:"operation_sha256"`
	OperationPayloadSHA256 protectedBrokerHashV1        `json:"operation_payload_sha256"`
	OperationJSONBase64URL string                       `json:"operation_json_b64"`
	RequestSHA256          protectedBrokerHashV1        `json:"request_sha256"`
	Signature              string                       `json:"signature"`
}

func newProtectedBrokerProviderQuiescenceRequestV1(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
	sequence protectedBrokerRPCSequenceV1,
	operation AgentOperation,
	operationJSON []byte,
) (protectedBrokerProviderQuiescenceRequestWireV1, error) {
	if client == nil ||
		!client.valid() ||
		sequence == 0 ||
		!operation.dispatchablePause() {
		return protectedBrokerProviderQuiescenceRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	if err := validateProtectedBrokerProviderSessionV1(client, session); err != nil {
		return protectedBrokerProviderQuiescenceRequestWireV1{}, err
	}
	expectedOperationJSON, err := validateProtectedBrokerQuiescenceOperationV1(
		client,
		operation,
	)
	if err != nil || !bytes.Equal(operationJSON, expectedOperationJSON) {
		return protectedBrokerProviderQuiescenceRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	operationSHA256, err := parseProtectedBrokerHashV1(
		operation.CanonicalHash().String(),
	)
	if err != nil {
		return protectedBrokerProviderQuiescenceRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	operationPayloadSHA256 := hashProtectedBrokerBytesV1(
		protectedBrokerProviderQuiescenceOperationPayloadHashDomainV1,
		operationJSON,
	)
	requestSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderQuiescenceRequestHashDomainV1,
		protectedBrokerProviderQuiescenceRequestSchemaV1,
		sequence,
		session.clientAuthoritySHA256,
		session.transportSessionSHA256,
		session.backendIdentitySHA256,
		session.brokerEpoch,
		session.profileSHA256,
		operationSHA256,
		operationPayloadSHA256,
	)
	if err != nil {
		return protectedBrokerProviderQuiescenceRequestWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerProviderQuiescenceRequestSignatureDomainV1,
		client.clientKey,
		protectedBrokerProviderQuiescenceRequestSchemaV1,
		sequence,
		session.clientAuthoritySHA256,
		session.transportSessionSHA256,
		session.backendIdentitySHA256,
		session.brokerEpoch,
		session.profileSHA256,
		operationSHA256,
		operationPayloadSHA256,
		requestSHA256,
	)
	if err != nil {
		return protectedBrokerProviderQuiescenceRequestWireV1{}, err
	}
	return protectedBrokerProviderQuiescenceRequestWireV1{
		Schema:                 protectedBrokerProviderQuiescenceRequestSchemaV1,
		Sequence:               sequence,
		ClientAuthoritySHA256:  session.clientAuthoritySHA256,
		TransportSessionSHA256: session.transportSessionSHA256,
		BackendIdentitySHA256:  session.backendIdentitySHA256,
		BrokerEpoch:            session.brokerEpoch,
		ProfileSHA256:          session.profileSHA256,
		OperationSHA256:        operationSHA256,
		OperationPayloadSHA256: operationPayloadSHA256,
		OperationJSONBase64URL: base64.RawURLEncoding.EncodeToString(operationJSON),
		RequestSHA256:          requestSHA256,
		Signature:              signature,
	}, nil
}

type protectedBrokerProviderQuiescenceResponseWireV1 struct {
	Schema                 string                                             `json:"schema"`
	Sequence               protectedBrokerRPCSequenceV1                       `json:"sequence"`
	ClientAuthoritySHA256  protectedBrokerHashV1                              `json:"client_authority_sha256"`
	TransportSessionSHA256 protectedBrokerHashV1                              `json:"transport_session_sha256"`
	BackendIdentitySHA256  protectedBrokerHashV1                              `json:"backend_identity_sha256"`
	BrokerEpoch            protectedBrokerEpochV1                             `json:"broker_epoch"`
	ProfileSHA256          protectedBrokerHashV1                              `json:"profile_sha256"`
	RequestSHA256          protectedBrokerHashV1                              `json:"request_sha256"`
	Payload                protectedBrokerProviderQuiescenceResponsePayloadV1 `json:"payload"`
	ResponsePayloadSHA256  protectedBrokerHashV1                              `json:"response_payload_sha256"`
	ResponseSHA256         protectedBrokerHashV1                              `json:"response_sha256"`
	Signature              string                                             `json:"signature"`
}

func (v protectedBrokerProviderQuiescenceResponseWireV1) validate(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
	expectedSequence protectedBrokerRPCSequenceV1,
	expectedRequestSHA256 protectedBrokerHashV1,
	operation AgentOperation,
) (Quiescence, error) {
	if v.Schema != protectedBrokerProviderQuiescenceResponseSchemaV1 {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerInvalidSchemaV1)
	}
	if v.Sequence != expectedSequence {
		if v.Sequence < expectedSequence {
			return Quiescence{},
				protectedBrokerError(ProtectedBrokerReplayedSequenceV1)
		}
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerSequenceGapV1)
	}
	if v.ClientAuthoritySHA256 != session.clientAuthoritySHA256 {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerClientAuthorityMismatchV1)
	}
	if v.TransportSessionSHA256 != session.transportSessionSHA256 {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerTransportSessionMismatchV1)
	}
	if client == nil ||
		v.BackendIdentitySHA256 != client.expectedBackend.identitySHA256 ||
		v.BrokerEpoch != client.expectedBackend.brokerEpoch ||
		v.ProfileSHA256 != client.expectedBackend.profileSHA256 {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerServiceBindingMismatchV1)
	}
	if v.RequestSHA256 != expectedRequestSHA256 {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerRequestHashMismatchV1)
	}
	expectedPayloadSHA256, err := hashProtectedBrokerJSONValueV1(
		protectedBrokerProviderQuiescenceResponsePayloadHashDomainV1,
		v.Payload,
	)
	if err != nil || v.ResponsePayloadSHA256 != expectedPayloadSHA256 {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	expectedResponseSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderQuiescenceResponseHashDomainV1,
		protectedBrokerProviderQuiescenceResponseSchemaV1,
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
		return Quiescence{}, err
	}
	if v.ResponseSHA256 != expectedResponseSHA256 {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerProviderQuiescenceResponseSignatureDomainV1,
		v.Signature,
		client.brokerKey,
		protectedBrokerProviderQuiescenceResponseSchemaV1,
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
		return Quiescence{}, err
	}
	return v.Payload.intoValidated(
		operation,
		client,
		v.ResponseSHA256,
	)
}

type protectedBrokerProviderQuiescenceSuccessWireV1 struct {
	Outcome                 string                `json:"outcome"`
	QuiescenceSHA256        protectedBrokerHashV1 `json:"quiescence_sha256"`
	QuiescenceJSONBase64URL string                `json:"quiescence_json_b64"`
}

type protectedBrokerProviderQuiescenceFailureWireV1 struct {
	Outcome string                        `json:"outcome"`
	Class   protectedBrokerFailureClassV1 `json:"class"`
}

type protectedBrokerProviderQuiescenceResponsePayloadV1 struct {
	historical bool
	success    *protectedBrokerProviderQuiescenceSuccessWireV1
	failure    *protectedBrokerFailureClassV1
}

func (v protectedBrokerProviderQuiescenceResponsePayloadV1) MarshalJSON() ([]byte, error) {
	if v.success != nil && v.failure == nil {
		wire := *v.success
		if v.historical {
			wire.Outcome = protectedBrokerProviderQuiescenceHistoricalV1
		} else {
			wire.Outcome = protectedBrokerProviderQuiescenceCurrentV1
		}
		return json.Marshal(wire)
	}
	if v.success == nil && v.failure != nil && v.failure.valid() {
		return json.Marshal(protectedBrokerProviderQuiescenceFailureWireV1{
			Outcome: protectedBrokerProviderQuiescenceFailureV1,
			Class:   *v.failure,
		})
	}
	return nil, errors.New("invalid protected broker provider quiescence payload")
}

func (v *protectedBrokerProviderQuiescenceResponsePayloadV1) UnmarshalJSON(
	encoded []byte,
) error {
	if v == nil {
		return errors.New("invalid protected broker provider quiescence payload")
	}
	var discriminator struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(encoded, &discriminator); err != nil {
		return errors.New("invalid protected broker provider quiescence payload")
	}
	switch discriminator.Outcome {
	case protectedBrokerProviderQuiescenceCurrentV1,
		protectedBrokerProviderQuiescenceHistoricalV1:
		var wire protectedBrokerProviderQuiescenceSuccessWireV1
		if err := decodeProtectedBrokerStrictJSONObjectV1(
			encoded,
			&wire,
		); err != nil ||
			!wire.QuiescenceSHA256.valid() ||
			wire.QuiescenceJSONBase64URL == "" {
			return errors.New("invalid protected broker provider quiescence payload")
		}
		*v = protectedBrokerProviderQuiescenceResponsePayloadV1{
			historical: discriminator.Outcome ==
				protectedBrokerProviderQuiescenceHistoricalV1,
			success: &wire,
		}
		return nil
	case protectedBrokerProviderQuiescenceFailureV1:
		var wire protectedBrokerProviderQuiescenceFailureWireV1
		if err := decodeProtectedBrokerStrictJSONObjectV1(
			encoded,
			&wire,
		); err != nil ||
			!wire.Class.valid() {
			return errors.New("invalid protected broker provider quiescence payload")
		}
		class := wire.Class
		*v = protectedBrokerProviderQuiescenceResponsePayloadV1{
			failure: &class,
		}
		return nil
	default:
		return errors.New("invalid protected broker provider quiescence payload")
	}
}

func (v protectedBrokerProviderQuiescenceResponsePayloadV1) intoValidated(
	operation AgentOperation,
	client *ProtectedBrokerClientV1,
	responseSHA256 protectedBrokerHashV1,
) (Quiescence, error) {
	if v.failure != nil {
		return Quiescence{}, newProtectedBrokerProviderQuiescenceFailureV1(
			*v.failure,
			client.expectedBackend,
			responseSHA256,
		)
	}
	if v.success == nil {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	quiescenceJSON, err := decodeProtectedBrokerProviderRecordV1(
		v.success.QuiescenceJSONBase64URL,
	)
	if err != nil {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	response, err := (ProviderV1FrameCodec{}).DecodeOperation(quiescenceJSON)
	if err != nil {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	quiescence, ok := response.(Quiescence)
	if !ok ||
		quiescence.CanonicalHash().String() !=
			v.success.QuiescenceSHA256.String() {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if quiescence.SessionID() != operation.SessionID() {
		return Quiescence{},
			protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return quiescence, nil
}

func newProtectedBrokerProviderQuiescenceFailureV1(
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
