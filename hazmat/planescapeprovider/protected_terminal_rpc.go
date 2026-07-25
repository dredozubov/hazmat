package planescapeprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
)

const (
	protectedBrokerProviderFreezeRequestSchemaV1        = "execution.protected-broker.provider-freeze-request.v1"
	protectedBrokerProviderFreezeResponseSchemaV1       = "execution.protected-broker.provider-freeze-response.v1"
	protectedBrokerProviderCloseoutRequestSchemaV1      = "execution.protected-broker.provider-closeout-request.v1"
	protectedBrokerProviderCloseoutResponseSchemaV1     = "execution.protected-broker.provider-closeout-response.v1"
	protectedBrokerProviderCancellationRequestSchemaV1  = "execution.protected-broker.provider-cancellation-request.v1"
	protectedBrokerProviderCancellationResponseSchemaV1 = "execution.protected-broker.provider-cancellation-response.v1"

	protectedBrokerProviderFreezeRecordPayloadHashDomainV1         = "planescape.execution.protected-broker.provider-freeze-record-payload.v1\x00"
	protectedBrokerProviderFreezeRequestHashDomainV1               = "planescape.execution.protected-broker.provider-freeze-request.v1\x00"
	protectedBrokerProviderFreezeRequestSignatureDomainV1          = "planescape.execution.protected-broker.provider-freeze-request-signature.v1\x00"
	protectedBrokerProviderFreezeResponsePayloadHashDomainV1       = "planescape.execution.protected-broker.provider-freeze-response-payload.v1\x00"
	protectedBrokerProviderFreezeResponseHashDomainV1              = "planescape.execution.protected-broker.provider-freeze-response.v1\x00"
	protectedBrokerProviderFreezeResponseSignatureDomainV1         = "planescape.execution.protected-broker.provider-freeze-response-signature.v1\x00"
	protectedBrokerProviderCloseoutRecordPayloadHashDomainV1       = "planescape.execution.protected-broker.provider-closeout-record-payload.v1\x00"
	protectedBrokerProviderCloseoutRequestHashDomainV1             = "planescape.execution.protected-broker.provider-closeout-request.v1\x00"
	protectedBrokerProviderCloseoutRequestSignatureDomainV1        = "planescape.execution.protected-broker.provider-closeout-request-signature.v1\x00"
	protectedBrokerProviderCloseoutResponsePayloadHashDomainV1     = "planescape.execution.protected-broker.provider-closeout-response-payload.v1\x00"
	protectedBrokerProviderCloseoutResponseHashDomainV1            = "planescape.execution.protected-broker.provider-closeout-response.v1\x00"
	protectedBrokerProviderCloseoutResponseSignatureDomainV1       = "planescape.execution.protected-broker.provider-closeout-response-signature.v1\x00"
	protectedBrokerProviderCancellationRecordPayloadHashDomainV1   = "planescape.execution.protected-broker.provider-cancellation-record-payload.v1\x00"
	protectedBrokerProviderCancellationRequestHashDomainV1         = "planescape.execution.protected-broker.provider-cancellation-request.v1\x00"
	protectedBrokerProviderCancellationRequestSignatureDomainV1    = "planescape.execution.protected-broker.provider-cancellation-request-signature.v1\x00"
	protectedBrokerProviderCancellationResponsePayloadHashDomainV1 = "planescape.execution.protected-broker.provider-cancellation-response-payload.v1\x00"
	protectedBrokerProviderCancellationResponseHashDomainV1        = "planescape.execution.protected-broker.provider-cancellation-response.v1\x00"
	protectedBrokerProviderCancellationResponseSignatureDomainV1   = "planescape.execution.protected-broker.provider-cancellation-response-signature.v1\x00"

	protectedBrokerProviderTerminalSequenceV1 = protectedBrokerRPCSequenceV1(1)
)

const (
	protectedBrokerProviderTerminalCurrentV1    = "current"
	protectedBrokerProviderTerminalHistoricalV1 = "historical"
	protectedBrokerProviderTerminalFailureV1    = "failure"
)

type protectedBrokerProviderTerminalKindV1 uint8

const (
	protectedBrokerProviderTerminalFreezeV1 protectedBrokerProviderTerminalKindV1 = iota + 1
	protectedBrokerProviderTerminalCloseoutV1
	protectedBrokerProviderTerminalCancellationV1
)

func (k protectedBrokerProviderTerminalKindV1) valid() bool {
	switch k {
	case protectedBrokerProviderTerminalFreezeV1,
		protectedBrokerProviderTerminalCloseoutV1,
		protectedBrokerProviderTerminalCancellationV1:
		return true
	default:
		return false
	}
}

func (k protectedBrokerProviderTerminalKindV1) requestSchema() string {
	switch k {
	case protectedBrokerProviderTerminalFreezeV1:
		return protectedBrokerProviderFreezeRequestSchemaV1
	case protectedBrokerProviderTerminalCloseoutV1:
		return protectedBrokerProviderCloseoutRequestSchemaV1
	case protectedBrokerProviderTerminalCancellationV1:
		return protectedBrokerProviderCancellationRequestSchemaV1
	default:
		return ""
	}
}

func (k protectedBrokerProviderTerminalKindV1) responseSchema() string {
	switch k {
	case protectedBrokerProviderTerminalFreezeV1:
		return protectedBrokerProviderFreezeResponseSchemaV1
	case protectedBrokerProviderTerminalCloseoutV1:
		return protectedBrokerProviderCloseoutResponseSchemaV1
	case protectedBrokerProviderTerminalCancellationV1:
		return protectedBrokerProviderCancellationResponseSchemaV1
	default:
		return ""
	}
}

func (k protectedBrokerProviderTerminalKindV1) recordPayloadHashDomain() string {
	switch k {
	case protectedBrokerProviderTerminalFreezeV1:
		return protectedBrokerProviderFreezeRecordPayloadHashDomainV1
	case protectedBrokerProviderTerminalCloseoutV1:
		return protectedBrokerProviderCloseoutRecordPayloadHashDomainV1
	case protectedBrokerProviderTerminalCancellationV1:
		return protectedBrokerProviderCancellationRecordPayloadHashDomainV1
	default:
		return ""
	}
}

func (k protectedBrokerProviderTerminalKindV1) requestHashDomain() string {
	switch k {
	case protectedBrokerProviderTerminalFreezeV1:
		return protectedBrokerProviderFreezeRequestHashDomainV1
	case protectedBrokerProviderTerminalCloseoutV1:
		return protectedBrokerProviderCloseoutRequestHashDomainV1
	case protectedBrokerProviderTerminalCancellationV1:
		return protectedBrokerProviderCancellationRequestHashDomainV1
	default:
		return ""
	}
}

func (k protectedBrokerProviderTerminalKindV1) requestSignatureDomain() string {
	switch k {
	case protectedBrokerProviderTerminalFreezeV1:
		return protectedBrokerProviderFreezeRequestSignatureDomainV1
	case protectedBrokerProviderTerminalCloseoutV1:
		return protectedBrokerProviderCloseoutRequestSignatureDomainV1
	case protectedBrokerProviderTerminalCancellationV1:
		return protectedBrokerProviderCancellationRequestSignatureDomainV1
	default:
		return ""
	}
}

func (k protectedBrokerProviderTerminalKindV1) responsePayloadHashDomain() string {
	switch k {
	case protectedBrokerProviderTerminalFreezeV1:
		return protectedBrokerProviderFreezeResponsePayloadHashDomainV1
	case protectedBrokerProviderTerminalCloseoutV1:
		return protectedBrokerProviderCloseoutResponsePayloadHashDomainV1
	case protectedBrokerProviderTerminalCancellationV1:
		return protectedBrokerProviderCancellationResponsePayloadHashDomainV1
	default:
		return ""
	}
}

func (k protectedBrokerProviderTerminalKindV1) responseHashDomain() string {
	switch k {
	case protectedBrokerProviderTerminalFreezeV1:
		return protectedBrokerProviderFreezeResponseHashDomainV1
	case protectedBrokerProviderTerminalCloseoutV1:
		return protectedBrokerProviderCloseoutResponseHashDomainV1
	case protectedBrokerProviderTerminalCancellationV1:
		return protectedBrokerProviderCancellationResponseHashDomainV1
	default:
		return ""
	}
}

func (k protectedBrokerProviderTerminalKindV1) responseSignatureDomain() string {
	switch k {
	case protectedBrokerProviderTerminalFreezeV1:
		return protectedBrokerProviderFreezeResponseSignatureDomainV1
	case protectedBrokerProviderTerminalCloseoutV1:
		return protectedBrokerProviderCloseoutResponseSignatureDomainV1
	case protectedBrokerProviderTerminalCancellationV1:
		return protectedBrokerProviderCancellationResponseSignatureDomainV1
	default:
		return ""
	}
}

func (k protectedBrokerProviderTerminalKindV1) retryFrom() Transition {
	switch k {
	case protectedBrokerProviderTerminalFreezeV1:
		return TransitionFreeze
	case protectedBrokerProviderTerminalCloseoutV1:
		return TransitionCloseout
	case protectedBrokerProviderTerminalCancellationV1:
		return TransitionCancel
	default:
		return ""
	}
}

// Each exported terminal transport fixes one request variant at construction.
// The shared implementation is package-private so callers cannot cross-wire a
// terminal record with another RPC schema.
type ProtectedBrokerFreezeTransportConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

type ProtectedBrokerCloseoutTransportConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

type ProtectedBrokerCancellationTransportConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

type ProtectedBrokerFreezeTransportV1 struct {
	terminal *protectedBrokerProviderTerminalTransportV1
}

type ProtectedBrokerCloseoutTransportV1 struct {
	terminal *protectedBrokerProviderTerminalTransportV1
}

type ProtectedBrokerCancellationTransportV1 struct {
	terminal *protectedBrokerProviderTerminalTransportV1
}

type protectedBrokerProviderTerminalTransportV1 struct {
	dialer ProtectedBrokerDialerV1
	client *ProtectedBrokerClientV1
}

func NewProtectedBrokerFreezeTransportV1(
	config ProtectedBrokerFreezeTransportConfigV1,
) (*ProtectedBrokerFreezeTransportV1, error) {
	terminal, err := newProtectedBrokerProviderTerminalTransportV1(
		config.Dialer,
		config.Client,
	)
	if err != nil {
		return nil, err
	}
	return &ProtectedBrokerFreezeTransportV1{terminal: terminal}, nil
}

func NewProtectedBrokerCloseoutTransportV1(
	config ProtectedBrokerCloseoutTransportConfigV1,
) (*ProtectedBrokerCloseoutTransportV1, error) {
	terminal, err := newProtectedBrokerProviderTerminalTransportV1(
		config.Dialer,
		config.Client,
	)
	if err != nil {
		return nil, err
	}
	return &ProtectedBrokerCloseoutTransportV1{terminal: terminal}, nil
}

func NewProtectedBrokerCancellationTransportV1(
	config ProtectedBrokerCancellationTransportConfigV1,
) (*ProtectedBrokerCancellationTransportV1, error) {
	terminal, err := newProtectedBrokerProviderTerminalTransportV1(
		config.Dialer,
		config.Client,
	)
	if err != nil {
		return nil, err
	}
	return &ProtectedBrokerCancellationTransportV1{terminal: terminal}, nil
}

func newProtectedBrokerProviderTerminalTransportV1(
	dialer ProtectedBrokerDialerV1,
	client *ProtectedBrokerClientV1,
) (*protectedBrokerProviderTerminalTransportV1, error) {
	if dialer == nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if client == nil || !client.valid() {
		return nil, protectedBrokerError(ProtectedBrokerWrongBrokerIdentityV1)
	}
	return &protectedBrokerProviderTerminalTransportV1{
		dialer: dialer,
		client: client,
	}, nil
}

func (t *ProtectedBrokerFreezeTransportV1) Freeze(
	ctx context.Context,
	request Freeze,
) (FreezeAck, error) {
	if t == nil || t.terminal == nil {
		return FreezeAck{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	result, err := t.terminal.exchange(
		ctx,
		protectedBrokerProviderFreezeRequestV1{request: request},
	)
	if err != nil {
		return FreezeAck{}, err
	}
	freeze, ok := result.(protectedBrokerProviderFreezeResultV1)
	if !ok || !freeze.valid() {
		return FreezeAck{}, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return freeze.acknowledgement, nil
}

func (t *ProtectedBrokerCloseoutTransportV1) Closeout(
	ctx context.Context,
	operation AgentOperation,
) (Closeout, error) {
	if t == nil || t.terminal == nil {
		return Closeout{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	result, err := t.terminal.exchange(
		ctx,
		protectedBrokerProviderCloseoutRequestV1{operation: operation},
	)
	if err != nil {
		return Closeout{}, err
	}
	closeout, ok := result.(protectedBrokerProviderCloseoutResultV1)
	if !ok || !closeout.valid() {
		return Closeout{}, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return closeout.closeout, nil
}

func (t *ProtectedBrokerCancellationTransportV1) Cancel(
	ctx context.Context,
	request Cancellation,
) (CancellationAck, error) {
	if t == nil || t.terminal == nil {
		return CancellationAck{}, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	result, err := t.terminal.exchange(
		ctx,
		protectedBrokerProviderCancellationRequestV1{request: request},
	)
	if err != nil {
		return CancellationAck{}, err
	}
	cancellation, ok := result.(protectedBrokerProviderCancellationResultV1)
	if !ok || !cancellation.valid() {
		return CancellationAck{}, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return cancellation.acknowledgement, nil
}

func (t *protectedBrokerProviderTerminalTransportV1) exchange(
	ctx context.Context,
	request protectedBrokerProviderTerminalRequestV1,
) (
	result protectedBrokerProviderTerminalResultV1,
	returnErr error,
) {
	if t == nil || t.dialer == nil || t.client == nil || !t.client.valid() {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if ctx == nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	record, err := newProtectedBrokerProviderTerminalRecordV1(request)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}

	rawStream, err := t.dialer.DialContext(ctx)
	if err != nil {
		return nil, mapProtectedBrokerDiscoveryIOError(ctx, err)
	}
	if rawStream == nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	stream := &managedProtectedBrokerStreamV1{stream: rawStream}
	stopContext := func() {}
	defer func() {
		stopContext()
		if closeErr := stream.Close(); returnErr == nil && closeErr != nil {
			result = nil
			returnErr = mapProtectedBrokerDiscoveryIOError(ctx, closeErr)
		}
	}()

	stopContext, err = bindProtectedBrokerStreamContextV1(ctx, stream)
	if err != nil {
		return nil, err
	}
	session, err := t.client.Authenticate(stream)
	if err != nil {
		return nil, mapProtectedBrokerDiscoverySessionError(ctx, err)
	}
	wire, err := newProtectedBrokerProviderTerminalRequestWireV1(
		t.client,
		session,
		protectedBrokerProviderTerminalSequenceV1,
		record,
	)
	if err != nil {
		return nil, err
	}
	if err := writeProtectedBrokerRPCJSONFrameV1(stream, wire); err != nil {
		return nil, mapProtectedBrokerDiscoverySessionError(ctx, err)
	}

	var response protectedBrokerProviderTerminalResponseWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(stream, &response); err != nil {
		return nil, mapProtectedBrokerDiscoverySessionError(ctx, err)
	}
	result, err = response.validate(
		t.client,
		session,
		protectedBrokerProviderTerminalSequenceV1,
		wire.RequestSHA256,
		record,
	)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return result, nil
}

type protectedBrokerProviderTerminalRequestV1 interface {
	protectedBrokerProviderTerminalRequest()
	kind() protectedBrokerProviderTerminalKindV1
	valid() bool
	encode() ([]byte, error)
	canonicalHash() Fingerprint
	validateResponse(
		[]byte,
		protectedBrokerHashV1,
		bool,
	) (protectedBrokerProviderTerminalResultV1, error)
}

type protectedBrokerProviderFreezeRequestV1 struct {
	request Freeze
}

func (protectedBrokerProviderFreezeRequestV1) protectedBrokerProviderTerminalRequest() {}
func (protectedBrokerProviderFreezeRequestV1) kind() protectedBrokerProviderTerminalKindV1 {
	return protectedBrokerProviderTerminalFreezeV1
}
func (v protectedBrokerProviderFreezeRequestV1) valid() bool {
	return v.request.valid()
}
func (v protectedBrokerProviderFreezeRequestV1) encode() ([]byte, error) {
	return (ProviderV1FrameCodec{}).EncodeFreeze(v.request)
}
func (v protectedBrokerProviderFreezeRequestV1) canonicalHash() Fingerprint {
	return v.request.CanonicalHash()
}
func (v protectedBrokerProviderFreezeRequestV1) validateResponse(
	encoded []byte,
	advertisedHash protectedBrokerHashV1,
	historical bool,
) (protectedBrokerProviderTerminalResultV1, error) {
	acknowledgement, err := (ProviderV1FrameCodec{}).DecodeFreezeAck(encoded)
	if err != nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	if acknowledgement.CanonicalHash().String() != advertisedHash.String() {
		return nil, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if acknowledgement.SessionID() != v.request.SessionID() ||
		acknowledgement.FreezeID() != v.request.FreezeID() ||
		acknowledgement.QuiescenceHash() != v.request.QuiescenceHash() {
		return nil, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return protectedBrokerProviderFreezeResultV1{
		acknowledgement: acknowledgement,
		historical:      historical,
	}, nil
}

type protectedBrokerProviderCloseoutRequestV1 struct {
	operation AgentOperation
}

func (protectedBrokerProviderCloseoutRequestV1) protectedBrokerProviderTerminalRequest() {}
func (protectedBrokerProviderCloseoutRequestV1) kind() protectedBrokerProviderTerminalKindV1 {
	return protectedBrokerProviderTerminalCloseoutV1
}
func (v protectedBrokerProviderCloseoutRequestV1) valid() bool {
	return v.operation.dispatchableCloseout()
}
func (v protectedBrokerProviderCloseoutRequestV1) encode() ([]byte, error) {
	return (ProviderV1FrameCodec{}).EncodeOperation(v.operation)
}
func (v protectedBrokerProviderCloseoutRequestV1) canonicalHash() Fingerprint {
	return v.operation.CanonicalHash()
}
func (v protectedBrokerProviderCloseoutRequestV1) validateResponse(
	encoded []byte,
	advertisedHash protectedBrokerHashV1,
	historical bool,
) (protectedBrokerProviderTerminalResultV1, error) {
	response, err := (ProviderV1FrameCodec{}).DecodeOperation(encoded)
	if err != nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	closeout, ok := response.(Closeout)
	if !ok {
		return nil, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	if closeout.CanonicalHash().String() != advertisedHash.String() {
		return nil, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if closeout.SessionID() != v.operation.SessionID() ||
		closeout.CloseoutID() != v.operation.OperationID() {
		return nil, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return protectedBrokerProviderCloseoutResultV1{
		closeout:   closeout,
		historical: historical,
	}, nil
}

type protectedBrokerProviderCancellationRequestV1 struct {
	request Cancellation
}

func (protectedBrokerProviderCancellationRequestV1) protectedBrokerProviderTerminalRequest() {}
func (protectedBrokerProviderCancellationRequestV1) kind() protectedBrokerProviderTerminalKindV1 {
	return protectedBrokerProviderTerminalCancellationV1
}
func (v protectedBrokerProviderCancellationRequestV1) valid() bool {
	return v.request.valid()
}
func (v protectedBrokerProviderCancellationRequestV1) encode() ([]byte, error) {
	return (ProviderV1FrameCodec{}).EncodeCancellation(v.request)
}
func (v protectedBrokerProviderCancellationRequestV1) canonicalHash() Fingerprint {
	return v.request.CanonicalHash()
}
func (v protectedBrokerProviderCancellationRequestV1) validateResponse(
	encoded []byte,
	advertisedHash protectedBrokerHashV1,
	historical bool,
) (protectedBrokerProviderTerminalResultV1, error) {
	acknowledgement, err :=
		(ProviderV1FrameCodec{}).DecodeCancellationAck(encoded)
	if err != nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	if acknowledgement.CanonicalHash().String() != advertisedHash.String() {
		return nil, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if acknowledgement.SessionID() != v.request.SessionID() ||
		acknowledgement.CancellationID() != v.request.CancellationID() ||
		acknowledgement.TerminalOutcome() != OutcomeCancelled {
		return nil, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return protectedBrokerProviderCancellationResultV1{
		acknowledgement: acknowledgement,
		historical:      historical,
	}, nil
}

type protectedBrokerProviderTerminalRecordV1 struct {
	request       protectedBrokerProviderTerminalRequestV1
	kind          protectedBrokerProviderTerminalKindV1
	canonicalHash protectedBrokerHashV1
	encoded       []byte
}

func newProtectedBrokerProviderTerminalRecordV1(
	request protectedBrokerProviderTerminalRequestV1,
) (protectedBrokerProviderTerminalRecordV1, error) {
	if request == nil || !request.kind().valid() || !request.valid() {
		return protectedBrokerProviderTerminalRecordV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	encoded, err := request.encode()
	if err != nil || len(encoded) == 0 || len(encoded) > MaxRecordBytes {
		return protectedBrokerProviderTerminalRecordV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	canonicalHash, err := parseProtectedBrokerHashV1(
		request.canonicalHash().String(),
	)
	if err != nil {
		return protectedBrokerProviderTerminalRecordV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	return protectedBrokerProviderTerminalRecordV1{
		request:       request,
		kind:          request.kind(),
		canonicalHash: canonicalHash,
		encoded:       append([]byte(nil), encoded...),
	}, nil
}

func (v protectedBrokerProviderTerminalRecordV1) valid() bool {
	if v.request == nil ||
		!v.kind.valid() ||
		v.request.kind() != v.kind ||
		!v.request.valid() ||
		!v.canonicalHash.valid() ||
		len(v.encoded) == 0 ||
		len(v.encoded) > MaxRecordBytes {
		return false
	}
	expected, err := v.request.encode()
	if err != nil || !bytes.Equal(v.encoded, expected) {
		return false
	}
	expectedHash, err := parseProtectedBrokerHashV1(
		v.request.canonicalHash().String(),
	)
	return err == nil && v.canonicalHash == expectedHash
}

type protectedBrokerProviderTerminalResultV1 interface {
	protectedBrokerProviderTerminalResult()
	valid() bool
}

type protectedBrokerProviderFreezeResultV1 struct {
	acknowledgement FreezeAck
	historical      bool
}

func (protectedBrokerProviderFreezeResultV1) protectedBrokerProviderTerminalResult() {}
func (v protectedBrokerProviderFreezeResultV1) valid() bool {
	return v.acknowledgement.valid()
}

type protectedBrokerProviderCloseoutResultV1 struct {
	closeout   Closeout
	historical bool
}

func (protectedBrokerProviderCloseoutResultV1) protectedBrokerProviderTerminalResult() {}
func (v protectedBrokerProviderCloseoutResultV1) valid() bool {
	return v.closeout.valid()
}

type protectedBrokerProviderCancellationResultV1 struct {
	acknowledgement CancellationAck
	historical      bool
}

func (protectedBrokerProviderCancellationResultV1) protectedBrokerProviderTerminalResult() {}
func (v protectedBrokerProviderCancellationResultV1) valid() bool {
	return v.acknowledgement.valid() &&
		v.acknowledgement.TerminalOutcome() == OutcomeCancelled
}

type protectedBrokerProviderTerminalRequestWireV1 struct {
	Schema                      string                       `json:"schema"`
	Sequence                    protectedBrokerRPCSequenceV1 `json:"sequence"`
	ClientAuthoritySHA256       protectedBrokerHashV1        `json:"client_authority_sha256"`
	TransportSessionSHA256      protectedBrokerHashV1        `json:"transport_session_sha256"`
	BackendIdentitySHA256       protectedBrokerHashV1        `json:"backend_identity_sha256"`
	BrokerEpoch                 protectedBrokerEpochV1       `json:"broker_epoch"`
	ProfileSHA256               protectedBrokerHashV1        `json:"profile_sha256"`
	TerminalRecordSHA256        protectedBrokerHashV1        `json:"terminal_record_sha256"`
	TerminalRecordPayloadSHA256 protectedBrokerHashV1        `json:"terminal_record_payload_sha256"`
	TerminalRecordJSONBase64URL string                       `json:"terminal_record_json_b64"`
	RequestSHA256               protectedBrokerHashV1        `json:"request_sha256"`
	Signature                   string                       `json:"signature"`
}

func newProtectedBrokerProviderTerminalRequestWireV1(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
	sequence protectedBrokerRPCSequenceV1,
	record protectedBrokerProviderTerminalRecordV1,
) (protectedBrokerProviderTerminalRequestWireV1, error) {
	if client == nil || !client.valid() || !record.valid() {
		return protectedBrokerProviderTerminalRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	if sequence != protectedBrokerProviderTerminalSequenceV1 {
		return protectedBrokerProviderTerminalRequestWireV1{},
			protectedBrokerError(ProtectedBrokerReplayedSequenceV1)
	}
	if err := validateProtectedBrokerProviderSessionV1(client, session); err != nil {
		return protectedBrokerProviderTerminalRequestWireV1{}, err
	}
	recordPayloadSHA256 := hashProtectedBrokerBytesV1(
		record.kind.recordPayloadHashDomain(),
		record.encoded,
	)
	requestSHA256, err := hashProtectedBrokerJSONV1(
		record.kind.requestHashDomain(),
		record.kind.requestSchema(),
		sequence,
		session.clientAuthoritySHA256,
		session.transportSessionSHA256,
		session.backendIdentitySHA256,
		session.brokerEpoch,
		session.profileSHA256,
		record.canonicalHash,
		recordPayloadSHA256,
	)
	if err != nil {
		return protectedBrokerProviderTerminalRequestWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		record.kind.requestSignatureDomain(),
		client.clientKey,
		record.kind.requestSchema(),
		sequence,
		session.clientAuthoritySHA256,
		session.transportSessionSHA256,
		session.backendIdentitySHA256,
		session.brokerEpoch,
		session.profileSHA256,
		record.canonicalHash,
		recordPayloadSHA256,
		requestSHA256,
	)
	if err != nil {
		return protectedBrokerProviderTerminalRequestWireV1{}, err
	}
	return protectedBrokerProviderTerminalRequestWireV1{
		Schema:                      record.kind.requestSchema(),
		Sequence:                    sequence,
		ClientAuthoritySHA256:       session.clientAuthoritySHA256,
		TransportSessionSHA256:      session.transportSessionSHA256,
		BackendIdentitySHA256:       session.backendIdentitySHA256,
		BrokerEpoch:                 session.brokerEpoch,
		ProfileSHA256:               session.profileSHA256,
		TerminalRecordSHA256:        record.canonicalHash,
		TerminalRecordPayloadSHA256: recordPayloadSHA256,
		TerminalRecordJSONBase64URL: base64.RawURLEncoding.EncodeToString(
			record.encoded,
		),
		RequestSHA256: requestSHA256,
		Signature:     signature,
	}, nil
}

type protectedBrokerProviderTerminalResponseWireV1 struct {
	Schema                 string                                           `json:"schema"`
	Sequence               protectedBrokerRPCSequenceV1                     `json:"sequence"`
	ClientAuthoritySHA256  protectedBrokerHashV1                            `json:"client_authority_sha256"`
	TransportSessionSHA256 protectedBrokerHashV1                            `json:"transport_session_sha256"`
	BackendIdentitySHA256  protectedBrokerHashV1                            `json:"backend_identity_sha256"`
	BrokerEpoch            protectedBrokerEpochV1                           `json:"broker_epoch"`
	ProfileSHA256          protectedBrokerHashV1                            `json:"profile_sha256"`
	RequestSHA256          protectedBrokerHashV1                            `json:"request_sha256"`
	Payload                protectedBrokerProviderTerminalResponsePayloadV1 `json:"payload"`
	ResponsePayloadSHA256  protectedBrokerHashV1                            `json:"response_payload_sha256"`
	ResponseSHA256         protectedBrokerHashV1                            `json:"response_sha256"`
	Signature              string                                           `json:"signature"`
}

func (v protectedBrokerProviderTerminalResponseWireV1) validate(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
	expectedSequence protectedBrokerRPCSequenceV1,
	expectedRequestSHA256 protectedBrokerHashV1,
	record protectedBrokerProviderTerminalRecordV1,
) (protectedBrokerProviderTerminalResultV1, error) {
	if !record.valid() {
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	if expectedSequence != protectedBrokerProviderTerminalSequenceV1 {
		return nil, protectedBrokerError(ProtectedBrokerReplayedSequenceV1)
	}
	if v.Schema != record.kind.responseSchema() {
		return nil, protectedBrokerError(ProtectedBrokerInvalidSchemaV1)
	}
	if v.Sequence != expectedSequence {
		if v.Sequence < expectedSequence {
			return nil, protectedBrokerError(ProtectedBrokerReplayedSequenceV1)
		}
		return nil, protectedBrokerError(ProtectedBrokerSequenceGapV1)
	}
	if v.ClientAuthoritySHA256 != session.clientAuthoritySHA256 {
		return nil, protectedBrokerError(ProtectedBrokerClientAuthorityMismatchV1)
	}
	if v.TransportSessionSHA256 != session.transportSessionSHA256 {
		return nil, protectedBrokerError(ProtectedBrokerTransportSessionMismatchV1)
	}
	if client == nil ||
		v.BackendIdentitySHA256 != client.expectedBackend.identitySHA256 ||
		v.BrokerEpoch != client.expectedBackend.brokerEpoch ||
		v.ProfileSHA256 != client.expectedBackend.profileSHA256 {
		return nil, protectedBrokerError(ProtectedBrokerServiceBindingMismatchV1)
	}
	if v.RequestSHA256 != expectedRequestSHA256 {
		return nil, protectedBrokerError(ProtectedBrokerRequestHashMismatchV1)
	}
	expectedPayloadSHA256, err := hashProtectedBrokerJSONValueV1(
		record.kind.responsePayloadHashDomain(),
		v.Payload,
	)
	if err != nil || v.ResponsePayloadSHA256 != expectedPayloadSHA256 {
		return nil, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	expectedResponseSHA256, err := hashProtectedBrokerJSONV1(
		record.kind.responseHashDomain(),
		record.kind.responseSchema(),
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
		return nil, err
	}
	if v.ResponseSHA256 != expectedResponseSHA256 {
		return nil, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if err := verifyProtectedBrokerJSONV1(
		record.kind.responseSignatureDomain(),
		v.Signature,
		client.brokerKey,
		record.kind.responseSchema(),
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
		return nil, err
	}
	return v.Payload.intoValidated(record, client, v.ResponseSHA256)
}

type protectedBrokerProviderTerminalSuccessWireV1 struct {
	Outcome                     string                `json:"outcome"`
	TerminalRecordSHA256        protectedBrokerHashV1 `json:"terminal_record_sha256"`
	TerminalRecordJSONBase64URL string                `json:"terminal_record_json_b64"`
}

type protectedBrokerProviderTerminalFailureWireV1 struct {
	Outcome string                        `json:"outcome"`
	Class   protectedBrokerFailureClassV1 `json:"class"`
}

type protectedBrokerProviderTerminalResponsePayloadV1 struct {
	historical bool
	success    *protectedBrokerProviderTerminalSuccessWireV1
	failure    *protectedBrokerFailureClassV1
}

func (v protectedBrokerProviderTerminalResponsePayloadV1) MarshalJSON() ([]byte, error) {
	if v.success != nil && v.failure == nil {
		wire := *v.success
		if v.historical {
			wire.Outcome = protectedBrokerProviderTerminalHistoricalV1
		} else {
			wire.Outcome = protectedBrokerProviderTerminalCurrentV1
		}
		return json.Marshal(wire)
	}
	if v.success == nil && v.failure != nil && v.failure.valid() {
		return json.Marshal(protectedBrokerProviderTerminalFailureWireV1{
			Outcome: protectedBrokerProviderTerminalFailureV1,
			Class:   *v.failure,
		})
	}
	return nil, errors.New("invalid protected broker provider terminal payload")
}

func (v *protectedBrokerProviderTerminalResponsePayloadV1) UnmarshalJSON(
	encoded []byte,
) error {
	if v == nil {
		return errors.New("invalid protected broker provider terminal payload")
	}
	var discriminator struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(encoded, &discriminator); err != nil {
		return errors.New("invalid protected broker provider terminal payload")
	}
	switch discriminator.Outcome {
	case protectedBrokerProviderTerminalCurrentV1,
		protectedBrokerProviderTerminalHistoricalV1:
		var wire protectedBrokerProviderTerminalSuccessWireV1
		if err := decodeProtectedBrokerStrictJSONObjectV1(
			encoded,
			&wire,
		); err != nil ||
			!wire.TerminalRecordSHA256.valid() ||
			wire.TerminalRecordJSONBase64URL == "" {
			return errors.New("invalid protected broker provider terminal payload")
		}
		*v = protectedBrokerProviderTerminalResponsePayloadV1{
			historical: discriminator.Outcome ==
				protectedBrokerProviderTerminalHistoricalV1,
			success: &wire,
		}
		return nil
	case protectedBrokerProviderTerminalFailureV1:
		var wire protectedBrokerProviderTerminalFailureWireV1
		if err := decodeProtectedBrokerStrictJSONObjectV1(
			encoded,
			&wire,
		); err != nil ||
			!wire.Class.valid() {
			return errors.New("invalid protected broker provider terminal payload")
		}
		class := wire.Class
		*v = protectedBrokerProviderTerminalResponsePayloadV1{
			failure: &class,
		}
		return nil
	default:
		return errors.New("invalid protected broker provider terminal payload")
	}
}

func (v protectedBrokerProviderTerminalResponsePayloadV1) intoValidated(
	record protectedBrokerProviderTerminalRecordV1,
	client *ProtectedBrokerClientV1,
	responseSHA256 protectedBrokerHashV1,
) (protectedBrokerProviderTerminalResultV1, error) {
	if v.failure != nil {
		return nil, newProtectedBrokerProviderTerminalFailureV1(
			*v.failure,
			client.expectedBackend,
			responseSHA256,
			record.kind.retryFrom(),
		)
	}
	if v.success == nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	terminalRecordJSON, err := decodeProtectedBrokerProviderRecordV1(
		v.success.TerminalRecordJSONBase64URL,
	)
	if err != nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidEvidenceV1)
	}
	return record.request.validateResponse(
		terminalRecordJSON,
		v.success.TerminalRecordSHA256,
		v.historical,
	)
}

func newProtectedBrokerProviderTerminalFailureV1(
	class protectedBrokerFailureClassV1,
	backend ProtectedBrokerBackendIdentityV1,
	responseSHA256 protectedBrokerHashV1,
	retryFrom Transition,
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
	if !retryFrom.valid() {
		return protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	failure, err := NewProviderFailure(ProviderFailureInput{
		Code:          code,
		ProviderID:    backend.identitySHA256.String(),
		ProviderEpoch: uint64(backend.brokerEpoch),
		RetryFrom:     retryFrom,
		CanonicalHash: responseSHA256.String(),
	})
	if err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	return failure
}
