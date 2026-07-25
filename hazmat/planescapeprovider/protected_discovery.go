package planescapeprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"
	"time"
)

const (
	// MaxProtectedBrokerRPCFrameBytesV1 is the exact protected broker RPC JSON
	// ceiling published by Planescape. The four-byte length prefix is excluded.
	MaxProtectedBrokerRPCFrameBytesV1 = 4 * 1024 * 1024

	protectedBrokerProviderDiscoveryRequestSchemaV1  = "execution.protected-broker.provider-discovery-request.v1"
	protectedBrokerProviderDiscoveryResponseSchemaV1 = "execution.protected-broker.provider-discovery-response.v1"

	protectedBrokerProviderDiscoveryRequestPayloadHashDomainV1  = "planescape.execution.protected-broker.provider-discovery-request-payload.v1\x00"
	protectedBrokerProviderDiscoveryRequestHashDomainV1         = "planescape.execution.protected-broker.provider-discovery-request.v1\x00"
	protectedBrokerProviderDiscoveryRequestSignatureDomainV1    = "planescape.execution.protected-broker.provider-discovery-request-signature.v1\x00"
	protectedBrokerProviderDiscoveryResponsePayloadHashDomainV1 = "planescape.execution.protected-broker.provider-discovery-response-payload.v1\x00"
	protectedBrokerProviderDiscoveryResponseHashDomainV1        = "planescape.execution.protected-broker.provider-discovery-response.v1\x00"
	protectedBrokerProviderDiscoveryResponseSignatureDomainV1   = "planescape.execution.protected-broker.provider-discovery-response-signature.v1\x00"

	protectedBrokerProviderDiscoverySequenceV1 = protectedBrokerRPCSequenceV1(1)
	maxEncodedProviderRecordBytesV1            = 4 * ((MaxRecordBytes + 2) / 3)
)

const (
	ProtectedBrokerInvalidRequestV1           ProtectedBrokerTransportErrorClassV1 = "invalid_request"
	ProtectedBrokerInvalidPayloadV1           ProtectedBrokerTransportErrorClassV1 = "invalid_payload"
	ProtectedBrokerSequenceGapV1              ProtectedBrokerTransportErrorClassV1 = "sequence_gap"
	ProtectedBrokerClientAuthorityMismatchV1  ProtectedBrokerTransportErrorClassV1 = "client_authority_mismatch"
	ProtectedBrokerTransportSessionMismatchV1 ProtectedBrokerTransportErrorClassV1 = "transport_session_mismatch"
	ProtectedBrokerServiceBindingMismatchV1   ProtectedBrokerTransportErrorClassV1 = "service_binding_mismatch"
	ProtectedBrokerRequestHashMismatchV1      ProtectedBrokerTransportErrorClassV1 = "request_hash_mismatch"
	ProtectedBrokerResponseHashMismatchV1     ProtectedBrokerTransportErrorClassV1 = "response_hash_mismatch"
)

// ProtectedBrokerStreamV1 is the minimum stream capability required for a
// context-bound protected broker exchange.
type ProtectedBrokerStreamV1 interface {
	io.ReadWriteCloser
	SetDeadline(time.Time) error
}

// ProtectedBrokerDialerV1 creates one fresh protected broker stream. DialContext
// must honor cancellation and must not select another transport on failure.
type ProtectedBrokerDialerV1 interface {
	DialContext(context.Context) (ProtectedBrokerStreamV1, error)
}

// ProtectedBrokerDialFuncV1 adapts a context-aware dial function.
type ProtectedBrokerDialFuncV1 func(context.Context) (ProtectedBrokerStreamV1, error)

func (f ProtectedBrokerDialFuncV1) DialContext(
	ctx context.Context,
) (ProtectedBrokerStreamV1, error) {
	if f == nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return f(ctx)
}

type ProtectedBrokerDiscoveryTransportConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

// ProtectedBrokerDiscoveryTransportV1 performs exactly one authenticated
// provider discovery exchange per connection. It has no admission, operation,
// lifecycle, shell, or fallback capability.
type ProtectedBrokerDiscoveryTransportV1 struct {
	dialer ProtectedBrokerDialerV1
	client *ProtectedBrokerClientV1
}

var _ FrameTransport = (*ProtectedBrokerDiscoveryTransportV1)(nil)

func NewProtectedBrokerDiscoveryTransportV1(
	config ProtectedBrokerDiscoveryTransportConfigV1,
) (*ProtectedBrokerDiscoveryTransportV1, error) {
	if config.Dialer == nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if config.Client == nil || !config.Client.valid() {
		return nil, protectedBrokerError(ProtectedBrokerWrongBrokerIdentityV1)
	}
	return &ProtectedBrokerDiscoveryTransportV1{
		dialer: config.Dialer,
		client: config.Client,
	}, nil
}

func (t *ProtectedBrokerDiscoveryTransportV1) RoundTrip(
	ctx context.Context,
	providerFrame []byte,
) (response []byte, returnErr error) {
	if t == nil || t.dialer == nil || t.client == nil || !t.client.valid() {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if ctx == nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	if err := requireExactProviderDiscoveryV1(providerFrame); err != nil {
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
			response = nil
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
	request, err := newProtectedBrokerProviderDiscoveryRequestV1(
		t.client,
		session,
		providerFrame,
	)
	if err != nil {
		return nil, err
	}
	if err := writeProtectedBrokerRPCJSONFrameV1(stream, request); err != nil {
		return nil, mapProtectedBrokerDiscoverySessionError(ctx, err)
	}

	var wireResponse protectedBrokerProviderDiscoveryResponseWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(stream, &wireResponse); err != nil {
		return nil, mapProtectedBrokerDiscoverySessionError(ctx, err)
	}
	capabilities, err := wireResponse.validate(
		t.client,
		session,
		request.RequestSHA256,
	)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return capabilities, nil
}

func requireExactProviderDiscoveryV1(providerFrame []byte) error {
	expected, err := (ProviderV1FrameCodec{}).EncodeDiscovery()
	if err != nil || !bytes.Equal(providerFrame, expected) {
		return protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	return nil
}

type protectedBrokerProviderDiscoveryRequestWireV1 struct {
	Schema                 string                       `json:"schema"`
	Sequence               protectedBrokerRPCSequenceV1 `json:"sequence"`
	ClientAuthoritySHA256  protectedBrokerHashV1        `json:"client_authority_sha256"`
	TransportSessionSHA256 protectedBrokerHashV1        `json:"transport_session_sha256"`
	BackendIdentitySHA256  protectedBrokerHashV1        `json:"backend_identity_sha256"`
	BrokerEpoch            protectedBrokerEpochV1       `json:"broker_epoch"`
	ProfileSHA256          protectedBrokerHashV1        `json:"profile_sha256"`
	DiscoverySHA256        protectedBrokerHashV1        `json:"discovery_sha256"`
	RequestPayloadSHA256   protectedBrokerHashV1        `json:"request_payload_sha256"`
	DiscoveryJSONBase64URL string                       `json:"discovery_json_b64"`
	RequestSHA256          protectedBrokerHashV1        `json:"request_sha256"`
	Signature              string                       `json:"signature"`
}

func newProtectedBrokerProviderDiscoveryRequestV1(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
	discoveryJSON []byte,
) (protectedBrokerProviderDiscoveryRequestWireV1, error) {
	if client == nil || !client.valid() {
		return protectedBrokerProviderDiscoveryRequestWireV1{},
			protectedBrokerError(ProtectedBrokerWrongBrokerIdentityV1)
	}
	if err := validateProtectedBrokerProviderSessionV1(client, session); err != nil {
		return protectedBrokerProviderDiscoveryRequestWireV1{}, err
	}
	if err := requireExactProviderDiscoveryV1(discoveryJSON); err != nil {
		return protectedBrokerProviderDiscoveryRequestWireV1{}, err
	}
	record, err := decodeProviderV1Record(discoveryJSON)
	if err != nil || record.kind != providerV1KindDiscovery {
		return protectedBrokerProviderDiscoveryRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	discoverySHA256, err := parseProtectedBrokerHashV1(
		providerV1CanonicalHash(record.canonicalPreimage),
	)
	if err != nil {
		return protectedBrokerProviderDiscoveryRequestWireV1{},
			protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	requestPayloadSHA256 := hashProtectedBrokerBytesV1(
		protectedBrokerProviderDiscoveryRequestPayloadHashDomainV1,
		discoveryJSON,
	)
	requestSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderDiscoveryRequestHashDomainV1,
		protectedBrokerProviderDiscoveryRequestSchemaV1,
		protectedBrokerProviderDiscoverySequenceV1,
		session.clientAuthoritySHA256,
		session.transportSessionSHA256,
		session.backendIdentitySHA256,
		session.brokerEpoch,
		session.profileSHA256,
		discoverySHA256,
		requestPayloadSHA256,
	)
	if err != nil {
		return protectedBrokerProviderDiscoveryRequestWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerProviderDiscoveryRequestSignatureDomainV1,
		client.clientKey,
		protectedBrokerProviderDiscoveryRequestSchemaV1,
		protectedBrokerProviderDiscoverySequenceV1,
		session.clientAuthoritySHA256,
		session.transportSessionSHA256,
		session.backendIdentitySHA256,
		session.brokerEpoch,
		session.profileSHA256,
		discoverySHA256,
		requestPayloadSHA256,
		requestSHA256,
	)
	if err != nil {
		return protectedBrokerProviderDiscoveryRequestWireV1{}, err
	}
	return protectedBrokerProviderDiscoveryRequestWireV1{
		Schema:                 protectedBrokerProviderDiscoveryRequestSchemaV1,
		Sequence:               protectedBrokerProviderDiscoverySequenceV1,
		ClientAuthoritySHA256:  session.clientAuthoritySHA256,
		TransportSessionSHA256: session.transportSessionSHA256,
		BackendIdentitySHA256:  session.backendIdentitySHA256,
		BrokerEpoch:            session.brokerEpoch,
		ProfileSHA256:          session.profileSHA256,
		DiscoverySHA256:        discoverySHA256,
		RequestPayloadSHA256:   requestPayloadSHA256,
		DiscoveryJSONBase64URL: base64.RawURLEncoding.EncodeToString(discoveryJSON),
		RequestSHA256:          requestSHA256,
		Signature:              signature,
	}, nil
}

type protectedBrokerProviderDiscoveryResponseWireV1 struct {
	Schema                    string                       `json:"schema"`
	Sequence                  protectedBrokerRPCSequenceV1 `json:"sequence"`
	ClientAuthoritySHA256     protectedBrokerHashV1        `json:"client_authority_sha256"`
	TransportSessionSHA256    protectedBrokerHashV1        `json:"transport_session_sha256"`
	BackendIdentitySHA256     protectedBrokerHashV1        `json:"backend_identity_sha256"`
	BrokerEpoch               protectedBrokerEpochV1       `json:"broker_epoch"`
	ProfileSHA256             protectedBrokerHashV1        `json:"profile_sha256"`
	RequestSHA256             protectedBrokerHashV1        `json:"request_sha256"`
	CapabilitiesSHA256        protectedBrokerHashV1        `json:"capabilities_sha256"`
	ResponsePayloadSHA256     protectedBrokerHashV1        `json:"response_payload_sha256"`
	CapabilitiesJSONBase64URL string                       `json:"capabilities_json_b64"`
	ResponseSHA256            protectedBrokerHashV1        `json:"response_sha256"`
	Signature                 string                       `json:"signature"`
}

func (v protectedBrokerProviderDiscoveryResponseWireV1) validate(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
	expectedRequestSHA256 protectedBrokerHashV1,
) ([]byte, error) {
	if v.Schema != protectedBrokerProviderDiscoveryResponseSchemaV1 {
		return nil, protectedBrokerError(ProtectedBrokerInvalidSchemaV1)
	}
	if v.Sequence != protectedBrokerProviderDiscoverySequenceV1 {
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
	capabilitiesJSON, err := decodeProtectedBrokerProviderRecordV1(
		v.CapabilitiesJSONBase64URL,
	)
	if err != nil {
		return nil, err
	}
	expectedPayloadSHA256 := hashProtectedBrokerBytesV1(
		protectedBrokerProviderDiscoveryResponsePayloadHashDomainV1,
		capabilitiesJSON,
	)
	if v.ResponsePayloadSHA256 != expectedPayloadSHA256 {
		return nil, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	expectedResponseSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderDiscoveryResponseHashDomainV1,
		protectedBrokerProviderDiscoveryResponseSchemaV1,
		v.Sequence,
		v.ClientAuthoritySHA256,
		v.TransportSessionSHA256,
		v.BackendIdentitySHA256,
		v.BrokerEpoch,
		v.ProfileSHA256,
		v.RequestSHA256,
		v.CapabilitiesSHA256,
		v.ResponsePayloadSHA256,
	)
	if err != nil {
		return nil, err
	}
	if v.ResponseSHA256 != expectedResponseSHA256 {
		return nil, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerProviderDiscoveryResponseSignatureDomainV1,
		v.Signature,
		client.brokerKey,
		protectedBrokerProviderDiscoveryResponseSchemaV1,
		v.Sequence,
		v.ClientAuthoritySHA256,
		v.TransportSessionSHA256,
		v.BackendIdentitySHA256,
		v.BrokerEpoch,
		v.ProfileSHA256,
		v.RequestSHA256,
		v.CapabilitiesSHA256,
		v.ResponsePayloadSHA256,
		v.ResponseSHA256,
	); err != nil {
		return nil, err
	}
	capabilities, err := (ProviderV1FrameCodec{}).DecodeCapabilities(capabilitiesJSON)
	if err != nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	if capabilities.CanonicalHash().String() != v.CapabilitiesSHA256.String() {
		return nil, protectedBrokerError(ProtectedBrokerResponseHashMismatchV1)
	}
	if !protectedBrokerStockLinuxCapabilitiesMatchV1(
		capabilities,
		client.expectedBackend,
	) {
		return nil, protectedBrokerError(ProtectedBrokerServiceBindingMismatchV1)
	}
	return capabilitiesJSON, nil
}

func validateProtectedBrokerProviderSessionV1(
	client *ProtectedBrokerClientV1,
	session AuthenticatedBrokerClientSessionV1,
) error {
	expectedAuthority, err := protectedBrokerClientAuthorityHashV1(
		client.clientKeySHA256,
		client.expectedBackend,
	)
	if err != nil {
		return err
	}
	if session.clientKeySHA256 != client.clientKeySHA256 ||
		session.clientAuthoritySHA256 != expectedAuthority {
		return protectedBrokerError(ProtectedBrokerClientAuthorityMismatchV1)
	}
	if session.backendIdentitySHA256 != client.expectedBackend.identitySHA256 ||
		session.brokerEpoch != client.expectedBackend.brokerEpoch ||
		session.profileSHA256 != client.expectedBackend.profileSHA256 {
		return protectedBrokerError(ProtectedBrokerServiceBindingMismatchV1)
	}
	if !session.transportSessionSHA256.valid() {
		return protectedBrokerError(ProtectedBrokerTransportSessionMismatchV1)
	}
	return nil
}

func protectedBrokerStockLinuxCapabilitiesMatchV1(
	capabilities ProviderCapabilities,
	backend ProtectedBrokerBackendIdentityV1,
) bool {
	expectedCapabilities := []Capability{
		CapabilityArtifactRead,
		CapabilityToolExecute,
		CapabilityWorkspaceRead,
		CapabilityWorkspaceWrite,
	}
	expectedResources := []ResourceDimension{
		ResourceCPUTime,
		ResourceMemoryBytes,
		ResourceOpenFiles,
		ResourceProcessCount,
		ResourceWorkspaceBytes,
		ResourceWorkspaceEntries,
	}
	return capabilities.valid() &&
		capabilities.ProviderID().String() == backend.identitySHA256.String() &&
		capabilities.ProviderEpoch().Uint64() == uint64(backend.brokerEpoch) &&
		capabilities.Profile() == ProfileStockLinux &&
		slices.Equal(capabilities.Capabilities(), expectedCapabilities) &&
		slices.Equal(capabilities.ResourceDimensions(), expectedResources)
}

func decodeProtectedBrokerProviderRecordV1(value string) ([]byte, error) {
	if len(value) == 0 || len(value) > maxEncodedProviderRecordBytesV1 {
		return nil, protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil ||
		len(decoded) == 0 ||
		len(decoded) > MaxRecordBytes ||
		base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, protectedBrokerError(ProtectedBrokerInvalidPayloadV1)
	}
	return decoded, nil
}

type protectedBrokerRPCSequenceV1 uint64

func (v protectedBrokerRPCSequenceV1) MarshalJSON() ([]byte, error) {
	if v == 0 {
		return nil, errors.New("invalid protected broker RPC sequence")
	}
	return json.Marshal(uint64(v))
}

func (v *protectedBrokerRPCSequenceV1) UnmarshalJSON(encoded []byte) error {
	if v == nil || bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
		return errors.New("invalid protected broker RPC sequence")
	}
	var value uint64
	if err := json.Unmarshal(encoded, &value); err != nil || value == 0 {
		return errors.New("invalid protected broker RPC sequence")
	}
	*v = protectedBrokerRPCSequenceV1(value)
	return nil
}

func writeProtectedBrokerRPCJSONFrameV1(writer io.Writer, value any) error {
	if writer == nil {
		return protectedBrokerError(ProtectedBrokerIOV1)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	if len(payload) == 0 || len(payload) > MaxProtectedBrokerRPCFrameBytesV1 {
		return protectedBrokerError(ProtectedBrokerFrameTooLargeV1)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeProtectedBrokerBytes(writer, header[:]); err != nil {
		return err
	}
	if err := writeProtectedBrokerBytes(writer, payload); err != nil {
		return err
	}
	if flusher, ok := writer.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return mapProtectedBrokerIOError(err)
		}
	}
	return nil
}

func readProtectedBrokerRPCJSONFrameV1(reader io.Reader, target any) error {
	policy, err := protectedJSONFieldPolicy(target)
	if err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	if reader == nil {
		return protectedBrokerError(ProtectedBrokerIOV1)
	}
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return mapProtectedBrokerIOError(err)
	}
	length := uint64(binary.BigEndian.Uint32(header[:]))
	if length == 0 || length > MaxProtectedBrokerRPCFrameBytesV1 {
		return protectedBrokerError(ProtectedBrokerFrameTooLargeV1)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return mapProtectedBrokerIOError(err)
	}
	if err := validateProtectedBrokerJSON(payload, policy); err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	return nil
}

type managedProtectedBrokerStreamV1 struct {
	stream   ProtectedBrokerStreamV1
	close    sync.Once
	closeErr error
}

func (s *managedProtectedBrokerStreamV1) Read(value []byte) (int, error) {
	return s.stream.Read(value)
}

func (s *managedProtectedBrokerStreamV1) Write(value []byte) (int, error) {
	return s.stream.Write(value)
}

func (s *managedProtectedBrokerStreamV1) SetDeadline(deadline time.Time) error {
	return s.stream.SetDeadline(deadline)
}

func (s *managedProtectedBrokerStreamV1) Close() error {
	s.close.Do(func() {
		s.closeErr = s.stream.Close()
	})
	return s.closeErr
}

func bindProtectedBrokerStreamContextV1(
	ctx context.Context,
	stream *managedProtectedBrokerStreamV1,
) (func(), error) {
	if deadline, ok := ctx.Deadline(); ok {
		if err := stream.SetDeadline(deadline); err != nil {
			return func() {}, mapProtectedBrokerDiscoveryIOError(ctx, err)
		}
	}
	if ctx.Done() == nil {
		return func() {}, nil
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if err := stream.SetDeadline(time.Now()); err != nil {
				_ = stream.Close()
			}
		case <-stop:
		}
	}()
	return func() {
		close(stop)
	}, nil
}

func mapProtectedBrokerDiscoverySessionError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return err
}

func mapProtectedBrokerDiscoveryIOError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return mapProtectedBrokerIOError(err)
}
