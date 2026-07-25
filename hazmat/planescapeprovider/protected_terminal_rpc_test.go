package planescapeprovider

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type protectedBrokerProviderTerminalFixtureV1 struct {
	Handshake struct {
		FreezeTransportSessionSHA256       string `json:"freeze_transport_session_sha256"`
		CloseoutTransportSessionSHA256     string `json:"closeout_transport_session_sha256"`
		CancellationTransportSessionSHA256 string `json:"cancellation_transport_session_sha256"`
	} `json:"handshake"`
	ProviderTerminalRPC struct {
		Freeze       protectedBrokerProviderTerminalCaseFixtureV1 `json:"freeze"`
		Closeout     protectedBrokerProviderTerminalCaseFixtureV1 `json:"closeout"`
		Cancellation protectedBrokerProviderTerminalCaseFixtureV1 `json:"cancellation"`
	} `json:"provider_terminal_rpc"`
}

type protectedBrokerProviderTerminalCaseFixtureV1 struct {
	RequestRecord      protectedBrokerDiscoveryRecordFixtureV1 `json:"request_record"`
	ResponseRecord     protectedBrokerDiscoveryRecordFixtureV1 `json:"response_record"`
	Request            protectedBrokerTerminalWireFixtureV1    `json:"request"`
	CurrentResponse    protectedBrokerTerminalWireFixtureV1    `json:"current_response"`
	HistoricalResponse protectedBrokerTerminalWireFixtureV1    `json:"historical_response"`
}

type protectedBrokerTerminalWireFixtureV1 struct {
	Wire protectedBrokerDiscoveryRPCWireFixtureV1 `json:"wire"`
}

func TestProtectedBrokerProviderTerminalMatchesExactRustVectors(t *testing.T) {
	fixture := loadProtectedBrokerProviderTerminalFixture(t)
	interop := loadProtectedBrokerInteropFixture(t)
	_, _, _, client, _ := protectedBrokerFixtureClient(t, interop)

	tests := []struct {
		name             string
		kind             protectedBrokerProviderTerminalKindV1
		transportSession string
		fixture          protectedBrokerProviderTerminalCaseFixtureV1
	}{
		{
			name:             "freeze",
			kind:             protectedBrokerProviderTerminalFreezeV1,
			transportSession: fixture.Handshake.FreezeTransportSessionSHA256,
			fixture:          fixture.ProviderTerminalRPC.Freeze,
		},
		{
			name:             "closeout",
			kind:             protectedBrokerProviderTerminalCloseoutV1,
			transportSession: fixture.Handshake.CloseoutTransportSessionSHA256,
			fixture:          fixture.ProviderTerminalRPC.Closeout,
		},
		{
			name:             "cancellation",
			kind:             protectedBrokerProviderTerminalCancellationV1,
			transportSession: fixture.Handshake.CancellationTransportSessionSHA256,
			fixture:          fixture.ProviderTerminalRPC.Cancellation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := decodeProtectedBrokerTerminalRequestFixture(
				t,
				test.kind,
				test.fixture.RequestRecord.CanonicalJSON,
			)
			record, err := newProtectedBrokerProviderTerminalRecordV1(request)
			if err != nil {
				t.Fatal(err)
			}
			session := protectedBrokerTerminalVectorSession(
				t,
				client,
				test.transportSession,
			)
			wire, err := newProtectedBrokerProviderTerminalRequestWireV1(
				client,
				session,
				protectedBrokerProviderTerminalSequenceV1,
				record,
			)
			if err != nil {
				t.Fatal(err)
			}
			requireProtectedBrokerWireJSON(
				t,
				wire,
				test.fixture.Request.Wire.CanonicalJSON,
			)
			if string(record.encoded) !=
				test.fixture.RequestRecord.CanonicalJSON {
				t.Fatal("terminal request record differs from Rust vector")
			}

			responses := []struct {
				name       string
				wire       string
				historical bool
			}{
				{
					name: "current",
					wire: test.fixture.CurrentResponse.Wire.
						CanonicalJSON,
				},
				{
					name:       "historical",
					wire:       test.fixture.HistoricalResponse.Wire.CanonicalJSON,
					historical: true,
				},
			}
			for _, responseTest := range responses {
				t.Run(responseTest.name, func(t *testing.T) {
					response := decodeProtectedBrokerTerminalResponseFixture(
						t,
						responseTest.wire,
					)
					result, err := response.validate(
						client,
						session,
						protectedBrokerProviderTerminalSequenceV1,
						wire.RequestSHA256,
						record,
					)
					if err != nil {
						t.Fatal(err)
					}
					if !result.valid() {
						t.Fatal("Rust terminal vector returned invalid result")
					}
					hash, historical := protectedBrokerTerminalResultForTest(
						t,
						result,
					)
					if hash != test.fixture.ResponseRecord.CanonicalSHA256 {
						t.Fatalf(
							"terminal response hash = %s, want %s",
							hash,
							test.fixture.ResponseRecord.CanonicalSHA256,
						)
					}
					if historical != responseTest.historical {
						t.Fatalf(
							"historical = %t, want %t",
							historical,
							responseTest.historical,
						)
					}
				})
			}
		})
	}
}

func TestProtectedBrokerProviderTerminalRejectsRustReplayVectorBeforeEffect(
	t *testing.T,
) {
	fixture := loadProtectedBrokerProviderTerminalFixture(t)
	interop := loadProtectedBrokerInteropFixture(t)
	_, _, _, client, _ := protectedBrokerFixtureClient(t, interop)
	request := decodeProtectedBrokerTerminalRequestFixture(
		t,
		protectedBrokerProviderTerminalCancellationV1,
		fixture.ProviderTerminalRPC.Cancellation.RequestRecord.CanonicalJSON,
	)
	record, err := newProtectedBrokerProviderTerminalRecordV1(request)
	if err != nil {
		t.Fatal(err)
	}
	session := protectedBrokerTerminalVectorSession(
		t,
		client,
		fixture.Handshake.CancellationTransportSessionSHA256,
	)
	_, err = newProtectedBrokerProviderTerminalRequestWireV1(
		client,
		session,
		protectedBrokerRPCSequenceV1(2),
		record,
	)
	requireProtectedBrokerErrorClass(
		t,
		err,
		ProtectedBrokerReplayedSequenceV1,
	)
}

func loadProtectedBrokerProviderTerminalFixture(
	t *testing.T,
) protectedBrokerProviderTerminalFixtureV1 {
	t.Helper()
	encoded, err := os.ReadFile(
		"testdata/protected_broker.v1/protected_broker_v1.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	var fixture protectedBrokerProviderTerminalFixtureV1
	if err := json.Unmarshal(encoded, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Handshake.FreezeTransportSessionSHA256 == "" ||
		fixture.Handshake.CloseoutTransportSessionSHA256 == "" ||
		fixture.Handshake.CancellationTransportSessionSHA256 == "" {
		t.Fatal("published fixture omits terminal transport sessions")
	}
	return fixture
}

func decodeProtectedBrokerTerminalRequestFixture(
	t *testing.T,
	kind protectedBrokerProviderTerminalKindV1,
	encoded string,
) protectedBrokerProviderTerminalRequestV1 {
	t.Helper()
	record, err := decodeProviderV1Record([]byte(encoded))
	if err != nil {
		t.Fatal(err)
	}
	switch kind {
	case protectedBrokerProviderTerminalFreezeV1:
		request, ok := record.value.(Freeze)
		if !ok {
			t.Fatal("Freeze vector has wrong provider record")
		}
		return protectedBrokerProviderFreezeRequestV1{request: request}
	case protectedBrokerProviderTerminalCloseoutV1:
		operation, ok := record.value.(AgentOperation)
		if !ok || !operation.dispatchableCloseout() {
			t.Fatal("Closeout vector has wrong provider record")
		}
		return protectedBrokerProviderCloseoutRequestV1{
			operation: operation,
		}
	case protectedBrokerProviderTerminalCancellationV1:
		request, ok := record.value.(Cancellation)
		if !ok {
			t.Fatal("Cancellation vector has wrong provider record")
		}
		return protectedBrokerProviderCancellationRequestV1{
			request: request,
		}
	default:
		t.Fatal("unsupported terminal vector kind")
		return nil
	}
}

func decodeProtectedBrokerTerminalResponseFixture(
	t *testing.T,
	encoded string,
) protectedBrokerProviderTerminalResponseWireV1 {
	t.Helper()
	var response protectedBrokerProviderTerminalResponseWireV1
	if err := decodeProtectedBrokerStrictJSONObjectV1(
		[]byte(encoded),
		&response,
	); err != nil {
		t.Fatal(err)
	}
	return response
}

func protectedBrokerTerminalVectorSession(
	t *testing.T,
	client *ProtectedBrokerClientV1,
	transportSession string,
) AuthenticatedBrokerClientSessionV1 {
	t.Helper()
	transportSessionSHA256, err := parseProtectedBrokerHashV1(
		transportSession,
	)
	if err != nil {
		t.Fatal(err)
	}
	clientAuthoritySHA256, err := protectedBrokerClientAuthorityHashV1(
		client.clientKeySHA256,
		client.expectedBackend,
	)
	if err != nil {
		t.Fatal(err)
	}
	return AuthenticatedBrokerClientSessionV1{
		backendIdentitySHA256:  client.expectedBackend.identitySHA256,
		brokerEpoch:            client.expectedBackend.brokerEpoch,
		profileSHA256:          client.expectedBackend.profileSHA256,
		clientKeySHA256:        client.clientKeySHA256,
		clientAuthoritySHA256:  clientAuthoritySHA256,
		transportSessionSHA256: transportSessionSHA256,
	}
}

func protectedBrokerTerminalResultForTest(
	t *testing.T,
	result protectedBrokerProviderTerminalResultV1,
) (string, bool) {
	t.Helper()
	switch value := result.(type) {
	case protectedBrokerProviderFreezeResultV1:
		return value.acknowledgement.CanonicalHash().String(),
			value.historical
	case protectedBrokerProviderCloseoutResultV1:
		return value.closeout.CanonicalHash().String(), value.historical
	case protectedBrokerProviderCancellationResultV1:
		return value.acknowledgement.CanonicalHash().String(),
			value.historical
	default:
		t.Fatal("unsupported terminal result")
		return "", false
	}
}

type protectedBrokerTerminalHarness struct {
	client          *ProtectedBrokerClientV1
	brokerKey       ed25519.PrivateKey
	clientPublicKey ed25519.PublicKey
	identity        ProtectedBrokerBackendIdentityV1
	fixture         protectedBrokerProviderTerminalFixtureV1

	mu                     sync.Mutex
	nextServerNonce        byte
	streams                []*protectedBrokerTrackingStream
	historical             bool
	closeAfterRequest      bool
	responseRecordOverride func(
		protectedBrokerProviderTerminalKindV1,
	) []byte
	observations chan protectedBrokerTerminalObservation
}

type protectedBrokerTerminalObservation struct {
	request protectedBrokerProviderTerminalRequestWireV1
	record  protectedBrokerProviderTerminalRecordV1
	err     error
}

func newProtectedBrokerTerminalHarness(
	t *testing.T,
) *protectedBrokerTerminalHarness {
	t.Helper()
	interop := loadProtectedBrokerInteropFixture(t)
	brokerKey, clientKey, identity, client, _ :=
		protectedBrokerFixtureClient(t, interop)
	return &protectedBrokerTerminalHarness{
		client:          client,
		brokerKey:       brokerKey,
		clientPublicKey: clientKey.Public().(ed25519.PublicKey),
		identity:        identity,
		fixture:         loadProtectedBrokerProviderTerminalFixture(t),
		nextServerNonce: 0xb0,
		observations:    make(chan protectedBrokerTerminalObservation, 8),
	}
}

func (h *protectedBrokerTerminalHarness) DialContext(
	context.Context,
) (ProtectedBrokerStreamV1, error) {
	client, server := net.Pipe()
	h.mu.Lock()
	h.nextServerNonce++
	serverNonce := h.nextServerNonce
	stream := &protectedBrokerTrackingStream{Conn: client}
	h.streams = append(h.streams, stream)
	h.mu.Unlock()
	go h.serve(server, serverNonce)
	return stream, nil
}

func (h *protectedBrokerTerminalHarness) serve(
	stream net.Conn,
	serverNonce byte,
) {
	observation := protectedBrokerTerminalObservation{}
	defer func() {
		_ = stream.Close()
		h.observations <- observation
	}()
	_ = stream.SetDeadline(time.Now().Add(5 * time.Second))
	codec := ProtectedBrokerFrameCodecV1{}

	var hello protectedBrokerClientHelloWireV1
	if err := codec.ReadJSONFrame(stream, &hello); err != nil {
		observation.err = err
		return
	}
	if err := validateProtectedBrokerClientHelloForTest(
		hello,
		h.client.clientKeySHA256,
	); err != nil {
		observation.err = err
		return
	}
	clientAuthority, err := protectedBrokerClientAuthorityHashV1(
		h.client.clientKeySHA256,
		h.identity,
	)
	if err != nil {
		observation.err = err
		return
	}
	challenge, err := newProtectedBrokerServerChallengeForTest(
		hello,
		repeatedProtectedBrokerNonce(serverNonce),
		h.identity,
		clientAuthority,
		h.brokerKey,
	)
	if err != nil {
		observation.err = err
		return
	}
	if err := codec.WriteJSONFrame(stream, challenge); err != nil {
		observation.err = err
		return
	}

	var finish protectedBrokerClientFinishWireV1
	if err := codec.ReadJSONFrame(stream, &finish); err != nil {
		observation.err = err
		return
	}
	transportSession, err := protectedBrokerTransportSessionHashV1(
		hello,
		challenge,
		clientAuthority,
		h.client.clientKeySHA256,
	)
	if err != nil {
		observation.err = err
		return
	}
	if err := validateProtectedBrokerClientFinishForTest(
		finish,
		challenge,
		h.client.clientKeySHA256,
		clientAuthority,
		transportSession,
		h.clientPublicKey,
	); err != nil {
		observation.err = err
		return
	}
	accepted, err := newProtectedBrokerServerAcceptedForTest(
		finish,
		clientAuthority,
		transportSession,
		h.brokerKey,
	)
	if err != nil {
		observation.err = err
		return
	}
	if err := codec.WriteJSONFrame(stream, accepted); err != nil {
		observation.err = err
		return
	}

	var request protectedBrokerProviderTerminalRequestWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(stream, &request); err != nil {
		observation.err = err
		return
	}
	observation.request = request
	record, err := validateProtectedBrokerProviderTerminalRequestForTest(
		request,
		clientAuthority,
		transportSession,
		h.identity,
		h.clientPublicKey,
	)
	if err != nil {
		observation.err = err
		return
	}
	observation.record = record
	if h.closeAfterRequest {
		return
	}

	responseRecord := h.responseRecord(record.kind)
	if h.responseRecordOverride != nil {
		responseRecord = h.responseRecordOverride(record.kind)
	}
	response, err := newProtectedBrokerProviderTerminalResponseForTest(
		request,
		record.kind,
		responseRecord,
		h.historical,
		h.brokerKey,
	)
	if err != nil {
		observation.err = err
		return
	}
	if err := writeProtectedBrokerRPCJSONFrameV1(stream, response); err != nil {
		observation.err = err
	}
}

func (h *protectedBrokerTerminalHarness) responseRecord(
	kind protectedBrokerProviderTerminalKindV1,
) []byte {
	switch kind {
	case protectedBrokerProviderTerminalFreezeV1:
		return []byte(
			h.fixture.ProviderTerminalRPC.Freeze.ResponseRecord.CanonicalJSON,
		)
	case protectedBrokerProviderTerminalCloseoutV1:
		return []byte(
			h.fixture.ProviderTerminalRPC.Closeout.ResponseRecord.CanonicalJSON,
		)
	case protectedBrokerProviderTerminalCancellationV1:
		return []byte(
			h.fixture.ProviderTerminalRPC.Cancellation.ResponseRecord.CanonicalJSON,
		)
	default:
		return nil
	}
}

func (h *protectedBrokerTerminalHarness) nextObservation(
	t *testing.T,
) protectedBrokerTerminalObservation {
	t.Helper()
	select {
	case observation := <-h.observations:
		return observation
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for provider terminal observation")
		return protectedBrokerTerminalObservation{}
	}
}

func (h *protectedBrokerTerminalHarness) dialCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.streams)
}

func (h *protectedBrokerTerminalHarness) requireClosedOnce(t *testing.T) {
	t.Helper()
	h.mu.Lock()
	streams := append([]*protectedBrokerTrackingStream(nil), h.streams...)
	h.mu.Unlock()
	if len(streams) == 0 {
		t.Fatal("provider terminal transport was never dialed")
	}
	for index, stream := range streams {
		if got := stream.closed(); got != 1 {
			t.Fatalf(
				"provider terminal stream %d close count = %d, want 1",
				index,
				got,
			)
		}
	}
}

func validateProtectedBrokerProviderTerminalRequestForTest(
	request protectedBrokerProviderTerminalRequestWireV1,
	clientAuthoritySHA256 protectedBrokerHashV1,
	transportSessionSHA256 protectedBrokerHashV1,
	identity ProtectedBrokerBackendIdentityV1,
	clientKey ed25519.PublicKey,
) (protectedBrokerProviderTerminalRecordV1, error) {
	kind, err := protectedBrokerProviderTerminalKindFromSchemaForTest(
		request.Schema,
	)
	if err != nil ||
		request.Sequence != protectedBrokerProviderTerminalSequenceV1 ||
		request.ClientAuthoritySHA256 != clientAuthoritySHA256 ||
		request.TransportSessionSHA256 != transportSessionSHA256 ||
		request.BackendIdentitySHA256 != identity.identitySHA256 ||
		request.BrokerEpoch != identity.brokerEpoch ||
		request.ProfileSHA256 != identity.profileSHA256 {
		return protectedBrokerProviderTerminalRecordV1{},
			errors.New("invalid provider terminal request binding")
	}
	recordJSON, err := decodeProtectedBrokerProviderRecordV1(
		request.TerminalRecordJSONBase64URL,
	)
	if err != nil {
		return protectedBrokerProviderTerminalRecordV1{}, err
	}
	if request.TerminalRecordPayloadSHA256 != hashProtectedBrokerBytesV1(
		kind.recordPayloadHashDomain(),
		recordJSON,
	) {
		return protectedBrokerProviderTerminalRecordV1{},
			errors.New("invalid provider terminal record payload hash")
	}
	expectedRequestSHA256, err := hashProtectedBrokerJSONV1(
		kind.requestHashDomain(),
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.TerminalRecordSHA256,
		request.TerminalRecordPayloadSHA256,
	)
	if err != nil {
		return protectedBrokerProviderTerminalRecordV1{}, err
	}
	if request.RequestSHA256 != expectedRequestSHA256 {
		return protectedBrokerProviderTerminalRecordV1{},
			errors.New("invalid provider terminal request hash")
	}
	if err := verifyProtectedBrokerJSONV1(
		kind.requestSignatureDomain(),
		request.Signature,
		clientKey,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.TerminalRecordSHA256,
		request.TerminalRecordPayloadSHA256,
		request.RequestSHA256,
	); err != nil {
		return protectedBrokerProviderTerminalRecordV1{}, err
	}
	terminalRequest, err := parseProtectedBrokerProviderTerminalRequestForTest(
		kind,
		recordJSON,
	)
	if err != nil {
		return protectedBrokerProviderTerminalRecordV1{}, err
	}
	record, err := newProtectedBrokerProviderTerminalRecordV1(terminalRequest)
	if err != nil {
		return protectedBrokerProviderTerminalRecordV1{}, err
	}
	if record.canonicalHash != request.TerminalRecordSHA256 ||
		string(record.encoded) != string(recordJSON) {
		return protectedBrokerProviderTerminalRecordV1{},
			errors.New("invalid provider terminal record binding")
	}
	return record, nil
}

func protectedBrokerProviderTerminalKindFromSchemaForTest(
	schema string,
) (protectedBrokerProviderTerminalKindV1, error) {
	switch schema {
	case protectedBrokerProviderFreezeRequestSchemaV1:
		return protectedBrokerProviderTerminalFreezeV1, nil
	case protectedBrokerProviderCloseoutRequestSchemaV1:
		return protectedBrokerProviderTerminalCloseoutV1, nil
	case protectedBrokerProviderCancellationRequestSchemaV1:
		return protectedBrokerProviderTerminalCancellationV1, nil
	default:
		return 0, errors.New("invalid provider terminal request schema")
	}
}

func parseProtectedBrokerProviderTerminalRequestForTest(
	kind protectedBrokerProviderTerminalKindV1,
	encoded []byte,
) (protectedBrokerProviderTerminalRequestV1, error) {
	record, err := decodeProviderV1Record(encoded)
	if err != nil {
		return nil, err
	}
	switch kind {
	case protectedBrokerProviderTerminalFreezeV1:
		request, ok := record.value.(Freeze)
		if !ok {
			return nil, errors.New("provider terminal record is not Freeze")
		}
		return protectedBrokerProviderFreezeRequestV1{request: request}, nil
	case protectedBrokerProviderTerminalCloseoutV1:
		operation, ok := record.value.(AgentOperation)
		if !ok || !operation.dispatchableCloseout() {
			return nil, errors.New(
				"provider terminal record is not Closeout",
			)
		}
		return protectedBrokerProviderCloseoutRequestV1{
			operation: operation,
		}, nil
	case protectedBrokerProviderTerminalCancellationV1:
		request, ok := record.value.(Cancellation)
		if !ok {
			return nil, errors.New(
				"provider terminal record is not Cancellation",
			)
		}
		return protectedBrokerProviderCancellationRequestV1{
			request: request,
		}, nil
	default:
		return nil, errors.New("unsupported provider terminal kind")
	}
}

func newProtectedBrokerProviderTerminalResponseForTest(
	request protectedBrokerProviderTerminalRequestWireV1,
	kind protectedBrokerProviderTerminalKindV1,
	responseRecordJSON []byte,
	historical bool,
	brokerKey ed25519.PrivateKey,
) (protectedBrokerProviderTerminalResponseWireV1, error) {
	recordSHA256, err := protectedBrokerTerminalResponseRecordHashForTest(
		responseRecordJSON,
	)
	if err != nil {
		return protectedBrokerProviderTerminalResponseWireV1{}, err
	}
	payload := protectedBrokerProviderTerminalResponsePayloadV1{
		historical: historical,
		success: &protectedBrokerProviderTerminalSuccessWireV1{
			TerminalRecordSHA256: recordSHA256,
			TerminalRecordJSONBase64URL: base64.RawURLEncoding.
				EncodeToString(responseRecordJSON),
		},
	}
	responsePayloadSHA256, err := hashProtectedBrokerJSONValueV1(
		kind.responsePayloadHashDomain(),
		payload,
	)
	if err != nil {
		return protectedBrokerProviderTerminalResponseWireV1{}, err
	}
	responseSHA256, err := hashProtectedBrokerJSONV1(
		kind.responseHashDomain(),
		kind.responseSchema(),
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.RequestSHA256,
		responsePayloadSHA256,
	)
	if err != nil {
		return protectedBrokerProviderTerminalResponseWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		kind.responseSignatureDomain(),
		brokerKey,
		kind.responseSchema(),
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.RequestSHA256,
		responsePayloadSHA256,
		responseSHA256,
	)
	if err != nil {
		return protectedBrokerProviderTerminalResponseWireV1{}, err
	}
	return protectedBrokerProviderTerminalResponseWireV1{
		Schema:                 kind.responseSchema(),
		Sequence:               request.Sequence,
		ClientAuthoritySHA256:  request.ClientAuthoritySHA256,
		TransportSessionSHA256: request.TransportSessionSHA256,
		BackendIdentitySHA256:  request.BackendIdentitySHA256,
		BrokerEpoch:            request.BrokerEpoch,
		ProfileSHA256:          request.ProfileSHA256,
		RequestSHA256:          request.RequestSHA256,
		Payload:                payload,
		ResponsePayloadSHA256:  responsePayloadSHA256,
		ResponseSHA256:         responseSHA256,
		Signature:              signature,
	}, nil
}

func protectedBrokerTerminalResponseRecordHashForTest(
	encoded []byte,
) (protectedBrokerHashV1, error) {
	record, err := decodeProviderV1Record(encoded)
	if err != nil {
		return "", err
	}
	var hash Fingerprint
	switch value := record.value.(type) {
	case FreezeAck:
		hash = value.CanonicalHash()
	case Closeout:
		hash = value.CanonicalHash()
	case CancellationAck:
		hash = value.CanonicalHash()
	default:
		return "", errors.New("invalid provider terminal response record")
	}
	return parseProtectedBrokerHashV1(hash.String())
}

func TestProtectedBrokerEndpointRunsExactTerminalKindsOnFreshAuthenticatedConnections(
	t *testing.T,
) {
	ctx := context.Background()
	harness := newProtectedBrokerTerminalHarness(t)
	endpoint, err := NewProtectedBrokerEndpointV1(
		ProtectedBrokerEndpointConfigV1{
			Dialer: harness,
			Client: harness.client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture := harness.fixture.ProviderTerminalRPC
	freezeRequest := decodeProtectedBrokerTerminalRequestFixture(
		t,
		protectedBrokerProviderTerminalFreezeV1,
		fixture.Freeze.RequestRecord.CanonicalJSON,
	).(protectedBrokerProviderFreezeRequestV1)
	closeoutRequest := decodeProtectedBrokerTerminalRequestFixture(
		t,
		protectedBrokerProviderTerminalCloseoutV1,
		fixture.Closeout.RequestRecord.CanonicalJSON,
	).(protectedBrokerProviderCloseoutRequestV1)
	cancellationRequest := decodeProtectedBrokerTerminalRequestFixture(
		t,
		protectedBrokerProviderTerminalCancellationV1,
		fixture.Cancellation.RequestRecord.CanonicalJSON,
	).(protectedBrokerProviderCancellationRequestV1)

	freeze, err := endpoint.Freeze(ctx, freezeRequest.request)
	if err != nil {
		t.Fatal(err)
	}
	if freeze.CanonicalHash().String() !=
		fixture.Freeze.ResponseRecord.CanonicalSHA256 {
		t.Fatal("protected endpoint returned wrong Freeze acknowledgement")
	}
	response, err := endpoint.Operate(ctx, closeoutRequest.operation)
	if err != nil {
		t.Fatal(err)
	}
	closeout, ok := response.(Closeout)
	if !ok ||
		closeout.CanonicalHash().String() !=
			fixture.Closeout.ResponseRecord.CanonicalSHA256 ||
		closeout.TerminalOutcome() != OutcomeSucceeded {
		t.Fatal("protected endpoint returned wrong successful Closeout")
	}
	cancellation, err := endpoint.Cancel(
		ctx,
		cancellationRequest.request,
	)
	if err != nil {
		t.Fatal(err)
	}
	if cancellation.CanonicalHash().String() !=
		fixture.Cancellation.ResponseRecord.CanonicalSHA256 ||
		cancellation.TerminalOutcome() != OutcomeCancelled {
		t.Fatal("protected endpoint returned wrong Cancellation acknowledgement")
	}

	schemas := []string{
		protectedBrokerProviderFreezeRequestSchemaV1,
		protectedBrokerProviderCloseoutRequestSchemaV1,
		protectedBrokerProviderCancellationRequestSchemaV1,
	}
	sessions := make(map[protectedBrokerHashV1]struct{}, len(schemas))
	var authority protectedBrokerHashV1
	for index, schema := range schemas {
		observation := harness.nextObservation(t)
		if observation.err != nil {
			t.Fatal(observation.err)
		}
		if observation.request.Schema != schema {
			t.Fatalf(
				"terminal request %d schema = %q, want %q",
				index,
				observation.request.Schema,
				schema,
			)
		}
		if observation.request.Sequence !=
			protectedBrokerProviderTerminalSequenceV1 {
			t.Fatalf(
				"terminal request %d sequence = %d, want 1",
				index,
				observation.request.Sequence,
			)
		}
		if index == 0 {
			authority = observation.request.ClientAuthoritySHA256
		} else if observation.request.ClientAuthoritySHA256 != authority {
			t.Fatal("fresh terminal auth changed durable client authority")
		}
		sessions[observation.request.TransportSessionSHA256] = struct{}{}
	}
	if len(sessions) != len(schemas) {
		t.Fatal("terminal RPCs reused an authenticated transport session")
	}
	if got := harness.dialCount(); got != len(schemas) {
		t.Fatalf("terminal dial count = %d, want %d", got, len(schemas))
	}
	harness.requireClosedOnce(t)
}

func TestProtectedBrokerEndpointCloseoutUsesOneConfiguredTCPAddress(
	t *testing.T,
) {
	harness := newProtectedBrokerTerminalHarness(t)
	listener := mustProtectedBrokerLoopbackListener(t)
	t.Cleanup(func() {
		_ = listener.Close()
	})
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptTCP()
		if err != nil {
			serverDone <- err
			return
		}
		harness.serve(connection, 0xd1)
		serverDone <- nil
	}()

	target := listener.Addr().(*net.TCPAddr).AddrPort()
	dialer, err := NewProtectedBrokerTCPDialerV1(target, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := NewProtectedBrokerEndpointV1(
		ProtectedBrokerEndpointConfigV1{
			Dialer: dialer,
			Client: harness.client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := decodeProtectedBrokerTerminalRequestFixture(
		t,
		protectedBrokerProviderTerminalCloseoutV1,
		harness.fixture.ProviderTerminalRPC.Closeout.RequestRecord.
			CanonicalJSON,
	).(protectedBrokerProviderCloseoutRequestV1)
	response, err := endpoint.Operate(context.Background(), request.operation)
	if err != nil {
		t.Fatal(err)
	}
	closeout, ok := response.(Closeout)
	if !ok ||
		closeout.TerminalOutcome() != OutcomeSucceeded ||
		closeout.CanonicalHash().String() !=
			harness.fixture.ProviderTerminalRPC.Closeout.ResponseRecord.
				CanonicalSHA256 {
		t.Fatal("configured TCP endpoint returned wrong Closeout")
	}
	observation := harness.nextObservation(t)
	if observation.err != nil {
		t.Fatal(observation.err)
	}
	if observation.request.Schema !=
		protectedBrokerProviderCloseoutRequestSchemaV1 {
		t.Fatalf(
			"configured TCP request schema = %q",
			observation.request.Schema,
		)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("configured TCP terminal server did not finish")
	}
	requireNoProtectedBrokerTCPAccept(t, listener)
}

func TestProtectedBrokerTerminalRejectsWrongKindBeforeDial(t *testing.T) {
	harness := newProtectedBrokerTerminalHarness(t)
	transport, err := NewProtectedBrokerCloseoutTransportV1(
		ProtectedBrokerCloseoutTransportConfigV1{
			Dialer: harness,
			Client: harness.client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	operation, _ := mustProtectedBrokerProviderToolFixtureOperation(
		t,
		loadProtectedBrokerDiscoveryFixture(t),
	)
	closeout, err := transport.Closeout(context.Background(), operation)
	if closeout.valid() {
		t.Fatal("Tool operation returned Closeout evidence")
	}
	requireProtectedBrokerErrorClass(
		t,
		err,
		ProtectedBrokerInvalidRequestV1,
	)
	if got := harness.dialCount(); got != 0 {
		t.Fatalf("wrong terminal kind dialed provider %d times", got)
	}
}

func TestProtectedBrokerTerminalProviderAbsenceAndDeathNeverFallback(
	t *testing.T,
) {
	fixture := loadProtectedBrokerProviderTerminalFixture(t)
	freezeRequest := decodeProtectedBrokerTerminalRequestFixture(
		t,
		protectedBrokerProviderTerminalFreezeV1,
		fixture.ProviderTerminalRPC.Freeze.RequestRecord.CanonicalJSON,
	).(protectedBrokerProviderFreezeRequestV1)
	cancellationRequest := decodeProtectedBrokerTerminalRequestFixture(
		t,
		protectedBrokerProviderTerminalCancellationV1,
		fixture.ProviderTerminalRPC.Cancellation.RequestRecord.CanonicalJSON,
	).(protectedBrokerProviderCancellationRequestV1)

	t.Run("absent before authentication", func(t *testing.T) {
		harness := newProtectedBrokerTerminalHarness(t)
		const secret = "ssh://operator:private-key@terminal-provider"
		dials := 0
		dialer := ProtectedBrokerDialFuncV1(
			func(context.Context) (ProtectedBrokerStreamV1, error) {
				dials++
				return nil, errors.New(secret)
			},
		)
		transport, err := NewProtectedBrokerFreezeTransportV1(
			ProtectedBrokerFreezeTransportConfigV1{
				Dialer: dialer,
				Client: harness.client,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		_, err = transport.Freeze(
			context.Background(),
			freezeRequest.request,
		)
		requireProtectedBrokerErrorClass(
			t,
			err,
			ProtectedBrokerIOV1,
		)
		if dials != 1 {
			t.Fatalf("absent provider dial count = %d, want 1", dials)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("terminal absence diagnostic leaked authority: %q", err)
		}
	})

	t.Run("dies after authenticated request", func(t *testing.T) {
		harness := newProtectedBrokerTerminalHarness(t)
		harness.closeAfterRequest = true
		transport, err := NewProtectedBrokerCancellationTransportV1(
			ProtectedBrokerCancellationTransportConfigV1{
				Dialer: harness,
				Client: harness.client,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		_, err = transport.Cancel(
			context.Background(),
			cancellationRequest.request,
		)
		requireProtectedBrokerErrorClass(
			t,
			err,
			ProtectedBrokerUnavailableV1,
		)
		if got := harness.dialCount(); got != 1 {
			t.Fatalf("dead provider dial count = %d, want 1", got)
		}
		for _, secret := range []string{
			cancellationRequest.request.Reason(),
			cancellationRequest.request.Nonce().String(),
			cancellationRequest.request.CanonicalHash().String(),
		} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf(
					"terminal death diagnostic leaked request material: %q",
					err,
				)
			}
		}
		observation := harness.nextObservation(t)
		if observation.err != nil {
			t.Fatal(observation.err)
		}
		harness.requireClosedOnce(t)
	})
}

func TestProtectedBrokerTerminalRejectsSignedResponseKindCrosswireWithoutFallback(
	t *testing.T,
) {
	harness := newProtectedBrokerTerminalHarness(t)
	harness.responseRecordOverride = func(
		kind protectedBrokerProviderTerminalKindV1,
	) []byte {
		if kind != protectedBrokerProviderTerminalFreezeV1 {
			return nil
		}
		return []byte(
			harness.fixture.ProviderTerminalRPC.Cancellation.
				ResponseRecord.CanonicalJSON,
		)
	}
	transport, err := NewProtectedBrokerFreezeTransportV1(
		ProtectedBrokerFreezeTransportConfigV1{
			Dialer: harness,
			Client: harness.client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := decodeProtectedBrokerTerminalRequestFixture(
		t,
		protectedBrokerProviderTerminalFreezeV1,
		harness.fixture.ProviderTerminalRPC.Freeze.RequestRecord.CanonicalJSON,
	).(protectedBrokerProviderFreezeRequestV1)
	acknowledgement, err := transport.Freeze(
		context.Background(),
		request.request,
	)
	if acknowledgement.valid() {
		t.Fatal("CancellationAck crosswire returned Freeze evidence")
	}
	requireProtectedBrokerErrorClass(
		t,
		err,
		ProtectedBrokerInvalidEvidenceV1,
	)
	if got := harness.dialCount(); got != 1 {
		t.Fatalf("crosswire dial count = %d, want 1", got)
	}
	observation := harness.nextObservation(t)
	if observation.err != nil {
		t.Fatal(observation.err)
	}
	harness.requireClosedOnce(t)
}
