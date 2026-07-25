package planescapeprovider

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestProtectedBrokerProviderToolMatchesPublishedFixture(t *testing.T) {
	fixture := loadProtectedBrokerDiscoveryFixture(t)
	interop := loadProtectedBrokerInteropFixture(t)
	_, _, _, client, clientNonce := protectedBrokerFixtureClient(t, interop)
	session, err := runProtectedBrokerStaticHandshake(
		t,
		client,
		clientNonce,
		fixture.SharedHandshakePublic.ClientHelloJSON,
		fixture.SharedHandshakePublic.ServerChallengeJSON,
		fixture.SharedHandshakePublic.ClientFinishJSON,
		fixture.SharedHandshakePublic.ServerAcceptedJSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	session.transportSessionSHA256, err = parseProtectedBrokerHashV1(
		fixture.Handshake.ReconnectTransportSessionSHA256,
	)
	if err != nil {
		t.Fatal(err)
	}
	operation, normalizedRecord := mustProtectedBrokerProviderToolFixtureOperation(
		t,
		fixture,
	)
	operationJSON, err := (ProviderV1FrameCodec{}).EncodeOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	request, err := newProtectedBrokerProviderToolRequestV1(
		client,
		session,
		protectedBrokerProviderToolSequenceV1,
		operation,
		operationJSON,
		normalizedRecord,
	)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(requestJSON); got != fixture.ProviderToolRPC.Request.Wire.CanonicalJSON {
		t.Fatalf(
			"provider Tool request differs from Rust fixture\n got: %s\nwant: %s",
			got,
			fixture.ProviderToolRPC.Request.Wire.CanonicalJSON,
		)
	}
	var framedRequest bytes.Buffer
	if err := writeProtectedBrokerRPCJSONFrameV1(
		&framedRequest,
		request,
	); err != nil {
		t.Fatal(err)
	}
	requireProtectedBrokerDiscoveryBase64Bytes(
		t,
		framedRequest.Bytes(),
		fixture.ProviderToolRPC.Request.Wire.FramedBytesB64,
	)
	requireProtectedBrokerDiscoveryHashVector(
		t,
		fixture.ProviderToolRPC.Request.OperationPayloadHash,
		protectedBrokerProviderToolOperationPayloadHashDomainV1,
		operationJSON,
	)
	requireProtectedBrokerDiscoveryHashVector(
		t,
		fixture.ProviderToolRPC.Request.NormalizedPayloadHash,
		protectedBrokerProviderToolNormalizedPayloadHashDomainV1,
		normalizedRecord,
	)
	requireProtectedBrokerDiscoveryJSONHashVector(
		t,
		fixture.ProviderToolRPC.Request.Hash,
		protectedBrokerProviderToolRequestHashDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.OperationSHA256,
		request.OperationPayloadSHA256,
		request.NormalizedRecordPayloadSHA256,
	)
	requireProtectedBrokerDiscoveryJSONSignatureVector(
		t,
		fixture.ProviderToolRPC.Request.Signature,
		protectedBrokerProviderToolRequestSignatureDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.OperationSHA256,
		request.OperationPayloadSHA256,
		request.NormalizedRecordPayloadSHA256,
		request.RequestSHA256,
	)

	responseFrame := decodeProtectedBrokerDiscoveryBase64(
		t,
		fixture.ProviderToolRPC.Response.Wire.FramedBytesB64,
	)
	var response protectedBrokerProviderToolResponseWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(
		bytes.NewReader(responseFrame),
		&response,
	); err != nil {
		t.Fatal(err)
	}
	result, err := response.validate(
		client,
		session,
		protectedBrokerProviderToolSequenceV1,
		request.RequestSHA256,
		operation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.CanonicalHash().String() !=
		fixture.ProviderToolRPC.Result.CanonicalSHA256 {
		t.Fatalf("provider Tool result = %+v", result)
	}
	requireProtectedBrokerAdmissionJSONValueHashVector(
		t,
		fixture.ProviderToolRPC.Response.PayloadHash,
		protectedBrokerProviderToolResponsePayloadHashDomainV1,
		response.Payload,
	)
	requireProtectedBrokerDiscoveryJSONHashVector(
		t,
		fixture.ProviderToolRPC.Response.Hash,
		protectedBrokerProviderToolResponseHashDomainV1,
		response.Schema,
		response.Sequence,
		response.ClientAuthoritySHA256,
		response.TransportSessionSHA256,
		response.BackendIdentitySHA256,
		response.BrokerEpoch,
		response.ProfileSHA256,
		response.RequestSHA256,
		response.ResponsePayloadSHA256,
	)
	requireProtectedBrokerDiscoveryJSONSignatureVector(
		t,
		fixture.ProviderToolRPC.Response.Signature,
		protectedBrokerProviderToolResponseSignatureDomainV1,
		response.Schema,
		response.Sequence,
		response.ClientAuthoritySHA256,
		response.TransportSessionSHA256,
		response.BackendIdentitySHA256,
		response.BrokerEpoch,
		response.ProfileSHA256,
		response.RequestSHA256,
		response.ResponsePayloadSHA256,
		response.ResponseSHA256,
	)
}

func mustProtectedBrokerProviderToolFixtureOperation(
	t *testing.T,
	fixture protectedBrokerDiscoveryFixtureV1,
) (AgentOperation, []byte) {
	t.Helper()
	record, err := decodeProviderV1Record(
		[]byte(fixture.ProviderToolRPC.Operation.CanonicalJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	canonical, ok := record.value.(AgentOperation)
	if !ok {
		t.Fatal("provider Tool fixture has no AgentOperation")
	}
	normalizedRecord, err := base64.RawURLEncoding.Strict().DecodeString(
		fixture.ProviderToolRPC.NormalizedRecord.BytesBase64URL,
	)
	if err != nil {
		t.Fatal(err)
	}
	input, err := NewOperationInput(OperationInputValues{
		OperationID:      canonical.OperationID().String(),
		Kind:             canonical.Kind(),
		Nonce:            canonical.Nonce().String(),
		PayloadHash:      canonical.PayloadHash().String(),
		NormalizedRecord: normalizedRecord,
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := newAgentOperation(
		canonical.SessionID(),
		canonical.Sequence(),
		canonical.PlanHash(),
		input,
	)
	if err != nil {
		t.Fatal(err)
	}
	if operation.CanonicalHash() != canonical.CanonicalHash() {
		t.Fatal("normalized record changed canonical AgentOperation")
	}
	return operation, normalizedRecord
}

func TestProtectedBrokerEndpointToolUsesFreshAuthenticatedConnections(
	t *testing.T,
) {
	harness := newProtectedBrokerToolHarness(t)
	endpoint := mustProtectedBrokerToolEndpoint(t, harness)
	operation, normalizedRecord := mustProtectedBrokerProviderToolFixtureOperation(
		t,
		loadProtectedBrokerDiscoveryFixture(t),
	)

	first, err := endpoint.Operate(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	harness.historical = true
	second, err := endpoint.Operate(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	firstResult, firstOK := first.(OperationResult)
	secondResult, secondOK := second.(OperationResult)
	if !firstOK ||
		!secondOK ||
		firstResult != harness.result ||
		secondResult != harness.result {
		t.Fatalf("provider Tool results = %+v, %+v", first, second)
	}

	firstObservation := harness.nextObservation(t)
	secondObservation := harness.nextObservation(t)
	for index, observation := range []protectedBrokerToolObservation{
		firstObservation,
		secondObservation,
	} {
		if observation.err != nil {
			t.Fatalf("exchange %d: %v", index, observation.err)
		}
		if observation.request.Sequence !=
			protectedBrokerProviderToolSequenceV1 {
			t.Fatalf(
				"exchange %d sequence = %d",
				index,
				observation.request.Sequence,
			)
		}
		if observation.operation.CanonicalHash() !=
			operation.CanonicalHash() ||
			!bytes.Equal(observation.normalizedRecord, normalizedRecord) {
			t.Fatalf("exchange %d changed Tool request", index)
		}
	}
	if firstObservation.request.ClientAuthoritySHA256 !=
		secondObservation.request.ClientAuthoritySHA256 {
		t.Fatal("fresh Tool connection changed stable client authority")
	}
	if firstObservation.request.TransportSessionSHA256 ==
		secondObservation.request.TransportSessionSHA256 {
		t.Fatal("Tool RPC reused a transport session")
	}
	harness.requireClosedOnce(t)
}

func TestProtectedBrokerClientRunsToolThroughExactProtectedEndpoint(
	t *testing.T,
) {
	ctx := context.Background()
	discoveryHarness := newProtectedBrokerDiscoveryHarness(t)
	admissionHarness := newProtectedBrokerAdmissionHarness(t)
	toolHarness := newProtectedBrokerToolHarness(t)
	toolHarness.resultForOperation = func(
		operation AgentOperation,
	) OperationResult {
		return newProtectedBrokerToolResultForTest(
			t,
			operation.SessionID().String(),
			operation.OperationID().String(),
			operation.Sequence().Uint64(),
			ResultCompleted,
		)
	}

	var dialMu sync.Mutex
	dialCount := 0
	dialer := ProtectedBrokerDialFuncV1(
		func(ctx context.Context) (ProtectedBrokerStreamV1, error) {
			dialMu.Lock()
			index := dialCount
			dialCount++
			dialMu.Unlock()
			switch index {
			case 0:
				return discoveryHarness.DialContext(ctx)
			case 1:
				return admissionHarness.DialContext(ctx)
			case 2:
				return toolHarness.DialContext(ctx)
			default:
				return nil, errors.New("unexpected protected broker dial")
			}
		},
	)
	endpoint, err := NewProtectedBrokerEndpointV1(
		ProtectedBrokerEndpointConfigV1{
			Dialer: dialer,
			Client: admissionHarness.client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(ClientConfig{
		Endpoint: endpoint,
		Store:    &MemoryStore{},
		Now:      func() time.Time { return time.UnixMilli(9_000).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := client.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	admissionInput, err := NewAdmissionInput(admissionHarness.plan)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.Admit(ctx, discovery, admissionInput)
	if err != nil {
		discoveryObservation := discoveryHarness.nextObservation(t)
		admissionObservation := admissionHarness.nextObservation(t)
		t.Fatalf(
			"admit: %v; discovery=%v admission=%v",
			err,
			discoveryObservation.err,
			admissionObservation.err,
		)
	}
	normalizedRecord := []byte(`{"command":"client-protected-tool"}`)
	operationInput, err := NewOperationInput(OperationInputValues{
		OperationID:      "client-protected-tool",
		Kind:             OperationTool,
		Nonce:            "client-protected-tool-nonce",
		PayloadHash:      fingerprint("a"),
		NormalizedRecord: normalizedRecord,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.RunTool(ctx, operationInput)
	if err != nil {
		t.Fatal(err)
	}
	if result.OperationID() != operationInput.OperationID() ||
		result.ResultKind() != ResultCompleted {
		t.Fatalf("protected client Tool result = %+v", result)
	}
	discoveryObservation := discoveryHarness.nextObservation(t)
	admissionObservation := admissionHarness.nextObservation(t)
	toolObservation := toolHarness.nextObservation(t)
	if discoveryObservation.err != nil ||
		admissionObservation.err != nil ||
		toolObservation.err != nil {
		t.Fatalf(
			"protected lifecycle errors = %v, %v, %v",
			discoveryObservation.err,
			admissionObservation.err,
			toolObservation.err,
		)
	}
	if !bytes.Equal(toolObservation.normalizedRecord, normalizedRecord) ||
		toolObservation.operation.OperationID() !=
			operationInput.OperationID() {
		t.Fatal("protected endpoint changed client Tool request")
	}
	dialMu.Lock()
	gotDials := dialCount
	dialMu.Unlock()
	if gotDials != 3 {
		t.Fatalf("protected lifecycle dial count = %d, want 3", gotDials)
	}
	discoveryHarness.requireClosedOnce(t)
	admissionHarness.requireClosedOnce(t)
	toolHarness.requireClosedOnce(t)
}

func TestProtectedBrokerEndpointToolMapsBrokerFailuresStably(t *testing.T) {
	tests := map[protectedBrokerFailureClassV1]ProviderErrorCode{
		protectedBrokerFailureUnsupportedPlatformV1:   ProviderErrorUnsupported,
		protectedBrokerFailureUnavailableAuthorityV1:  ProviderErrorUnavailable,
		protectedBrokerFailureStaleIdentityV1:         ProviderErrorStaleEpoch,
		protectedBrokerFailureInvalidContractV1:       ProviderErrorInvalid,
		protectedBrokerFailureSourceMismatchV1:        ProviderErrorConflict,
		protectedBrokerFailureProtocolConflictV1:      ProviderErrorConflict,
		protectedBrokerFailureResourceTripV1:          ProviderErrorConflict,
		protectedBrokerFailureUnconfirmedQuiescenceV1: ProviderErrorConflict,
		protectedBrokerFailureEvidenceConflictV1:      ProviderErrorConflict,
	}
	for class, want := range tests {
		t.Run(string(class), func(t *testing.T) {
			harness := newProtectedBrokerToolHarness(t)
			harness.brokerFailure = &class
			endpoint := mustProtectedBrokerToolEndpoint(t, harness)
			operation, _ := mustProtectedBrokerProviderToolFixtureOperation(
				t,
				loadProtectedBrokerDiscoveryFixture(t),
			)
			response, err := endpoint.Operate(
				context.Background(),
				operation,
			)
			if response != nil {
				t.Fatal("broker failure returned an operation result")
			}
			var failure *ProviderFailure
			if !errors.As(err, &failure) ||
				failure.Code() != want ||
				failure.RetryFrom() != TransitionActivate ||
				failure.ProviderID().String() !=
					harness.identity.identitySHA256.String() ||
				failure.ProviderEpoch().Uint64() !=
					uint64(harness.identity.brokerEpoch) {
				t.Fatalf("provider Tool failure = %#v, %v", failure, err)
			}
			if observation := harness.nextObservation(t); observation.err != nil {
				t.Fatal(observation.err)
			}
			harness.requireClosedOnce(t)
		})
	}
}

func TestProtectedBrokerEndpointToolRejectsHostileResponses(t *testing.T) {
	fixture := loadProtectedBrokerDiscoveryFixture(t)
	operation, _ := mustProtectedBrokerProviderToolFixtureOperation(
		t,
		fixture,
	)
	tests := map[string]struct {
		configure func(*protectedBrokerToolHarness)
		want      ProtectedBrokerTransportErrorClassV1
	}{
		"client authority substitution": {
			configure: func(h *protectedBrokerToolHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderToolResponseWireV1,
				) {
					response.ClientAuthoritySHA256 =
						protectedBrokerHashV1(fingerprint("0"))
				}
			},
			want: ProtectedBrokerClientAuthorityMismatchV1,
		},
		"transport session substitution": {
			configure: func(h *protectedBrokerToolHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderToolResponseWireV1,
				) {
					response.TransportSessionSHA256 =
						protectedBrokerHashV1(fingerprint("1"))
				}
			},
			want: ProtectedBrokerTransportSessionMismatchV1,
		},
		"backend identity substitution": {
			configure: func(h *protectedBrokerToolHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderToolResponseWireV1,
				) {
					response.BackendIdentitySHA256 =
						protectedBrokerHashV1(fingerprint("4"))
				}
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"sequence gap": {
			configure: func(h *protectedBrokerToolHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderToolResponseWireV1,
				) {
					response.Sequence = 2
				}
			},
			want: ProtectedBrokerSequenceGapV1,
		},
		"request substitution": {
			configure: func(h *protectedBrokerToolHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderToolResponseWireV1,
				) {
					response.RequestSHA256 =
						protectedBrokerHashV1(fingerprint("2"))
				}
			},
			want: ProtectedBrokerRequestHashMismatchV1,
		},
		"response payload hash substitution": {
			configure: func(h *protectedBrokerToolHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderToolResponseWireV1,
				) {
					response.ResponsePayloadSHA256 =
						protectedBrokerHashV1(fingerprint("3"))
				}
			},
			want: ProtectedBrokerResponseHashMismatchV1,
		},
		"wrong response signer": {
			configure: func(h *protectedBrokerToolHarness) {
				h.responseSigner = ed25519.NewKeyFromSeed(
					bytes.Repeat([]byte{0xa5}, ed25519.SeedSize),
				)
			},
			want: ProtectedBrokerInvalidSignatureV1,
		},
		"crosswired operation result": {
			configure: func(h *protectedBrokerToolHarness) {
				h.result = newProtectedBrokerToolResultForTest(
					t,
					operation.SessionID().String(),
					"foreign-operation",
					operation.Sequence().Uint64(),
					ResultCompleted,
				)
			},
			want: ProtectedBrokerInvalidEvidenceV1,
		},
		"nonterminal result": {
			configure: func(h *protectedBrokerToolHarness) {
				h.result = newProtectedBrokerToolResultForTest(
					t,
					operation.SessionID().String(),
					operation.OperationID().String(),
					operation.Sequence().Uint64(),
					ResultAccepted,
				)
			},
			want: ProtectedBrokerInvalidEvidenceV1,
		},
		"unknown envelope field": {
			configure: func(h *protectedBrokerToolHarness) {
				h.rawMutation = func(encoded []byte) []byte {
					var value map[string]json.RawMessage
					if err := json.Unmarshal(encoded, &value); err != nil {
						t.Fatal(err)
					}
					value["unexpected"] = json.RawMessage("true")
					mutated, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					return mutated
				}
			},
			want: ProtectedBrokerInvalidFrameV1,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			harness := newProtectedBrokerToolHarness(t)
			test.configure(harness)
			endpoint := mustProtectedBrokerToolEndpoint(t, harness)
			response, err := endpoint.Operate(
				context.Background(),
				operation,
			)
			if response != nil {
				t.Fatal("hostile response returned an operation result")
			}
			requireProtectedBrokerErrorClass(t, err, test.want)
			if observation := harness.nextObservation(t); observation.err != nil {
				t.Fatal(observation.err)
			}
			harness.requireClosedOnce(t)
		})
	}
}

func TestProtectedBrokerEndpointToolFailsBeforeDialWithoutExactToolPayload(
	t *testing.T,
) {
	harness := newProtectedBrokerToolHarness(t)
	dialed := false
	dialer := ProtectedBrokerDialFuncV1(
		func(context.Context) (ProtectedBrokerStreamV1, error) {
			dialed = true
			return nil, errors.New("must not dial")
		},
	)
	endpoint, err := NewProtectedBrokerEndpointV1(
		ProtectedBrokerEndpointConfigV1{
			Dialer: dialer,
			Client: harness.client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture := loadProtectedBrokerDiscoveryFixture(t)
	record, err := decodeProviderV1Record(
		[]byte(fixture.ProviderToolRPC.Operation.CanonicalJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	operation := record.value.(AgentOperation)
	response, err := endpoint.Operate(context.Background(), operation)
	if response != nil {
		t.Fatal("Tool without normalized bytes returned a response")
	}
	requireProtectedBrokerErrorClass(
		t,
		err,
		ProtectedBrokerInvalidRequestV1,
	)
	if dialed {
		t.Fatal("invalid Tool operation reached dial boundary")
	}
}

func TestProtectedBrokerEndpointToolCloseFailureWithholdsSuccess(t *testing.T) {
	harness := newProtectedBrokerToolHarness(t)
	harness.clientCloseErr = errors.New("raw-close-peer-material")
	endpoint := mustProtectedBrokerToolEndpoint(t, harness)
	operation, _ := mustProtectedBrokerProviderToolFixtureOperation(
		t,
		loadProtectedBrokerDiscoveryFixture(t),
	)
	response, err := endpoint.Operate(context.Background(), operation)
	if response != nil {
		t.Fatal("close failure returned an operation result")
	}
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerIOV1)
	if observation := harness.nextObservation(t); observation.err != nil {
		t.Fatal(observation.err)
	}
	harness.requireClosedOnce(t)
}

func mustProtectedBrokerToolEndpoint(
	t *testing.T,
	harness *protectedBrokerToolHarness,
) *ProtectedBrokerEndpointV1 {
	t.Helper()
	endpoint, err := NewProtectedBrokerEndpointV1(
		ProtectedBrokerEndpointConfigV1{
			Dialer: harness,
			Client: harness.client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func newProtectedBrokerToolResultForTest(
	t *testing.T,
	sessionID string,
	operationID string,
	sequence uint64,
	kind ResultKind,
) OperationResult {
	t.Helper()
	dto := providerV1OperationResultDTO{
		Schema:            providerV1SchemaOperationResult,
		SessionID:         sessionID,
		OperationID:       operationID,
		OperationSequence: sequence,
		ResultKind:        string(kind),
		ArtifactHash:      fingerprint("a"),
		EvidenceHash:      fingerprint("b"),
	}
	preimage, err := dto.canonicalPreimage()
	if err != nil {
		t.Fatal(err)
	}
	dto.CanonicalHash = providerV1CanonicalHash(preimage)
	result, err := NewOperationResult(OperationResultInput{
		SessionID:     dto.SessionID,
		OperationID:   dto.OperationID,
		Sequence:      dto.OperationSequence,
		ResultKind:    ResultKind(dto.ResultKind),
		ArtifactHash:  dto.ArtifactHash,
		EvidenceHash:  dto.EvidenceHash,
		CanonicalHash: dto.CanonicalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type protectedBrokerToolObservation struct {
	request          protectedBrokerProviderToolRequestWireV1
	operation        AgentOperation
	normalizedRecord []byte
	err              error
}

type protectedBrokerToolHarness struct {
	client             *ProtectedBrokerClientV1
	brokerKey          ed25519.PrivateKey
	clientPublicKey    ed25519.PublicKey
	identity           ProtectedBrokerBackendIdentityV1
	result             OperationResult
	resultForOperation func(AgentOperation) OperationResult

	mu               sync.Mutex
	nextServerNonce  byte
	streams          []*protectedBrokerTrackingStream
	historical       bool
	brokerFailure    *protectedBrokerFailureClassV1
	responseMutation func(*protectedBrokerProviderToolResponseWireV1)
	rawMutation      func([]byte) []byte
	responseSigner   ed25519.PrivateKey
	clientCloseErr   error
	observations     chan protectedBrokerToolObservation
}

func newProtectedBrokerToolHarness(t *testing.T) *protectedBrokerToolHarness {
	t.Helper()
	interop := loadProtectedBrokerInteropFixture(t)
	brokerKey, clientKey, identity, client, _ :=
		protectedBrokerFixtureClient(t, interop)
	fixture := loadProtectedBrokerDiscoveryFixture(t)
	response, err := (ProviderV1FrameCodec{}).DecodeOperation(
		[]byte(fixture.ProviderToolRPC.Result.CanonicalJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := response.(OperationResult)
	if !ok {
		t.Fatal("provider Tool fixture has no OperationResult")
	}
	return &protectedBrokerToolHarness{
		client:          client,
		brokerKey:       brokerKey,
		clientPublicKey: clientKey.Public().(ed25519.PublicKey),
		identity:        identity,
		result:          result,
		nextServerNonce: 0x90,
		observations:    make(chan protectedBrokerToolObservation, 8),
	}
}

func (h *protectedBrokerToolHarness) DialContext(
	context.Context,
) (ProtectedBrokerStreamV1, error) {
	client, server := net.Pipe()
	h.mu.Lock()
	h.nextServerNonce++
	serverNonce := h.nextServerNonce
	stream := &protectedBrokerTrackingStream{
		Conn:     client,
		closeErr: h.clientCloseErr,
	}
	h.streams = append(h.streams, stream)
	h.mu.Unlock()
	go h.serve(server, serverNonce)
	return stream, nil
}

func (h *protectedBrokerToolHarness) serve(
	stream net.Conn,
	serverNonce byte,
) {
	observation := protectedBrokerToolObservation{}
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

	var request protectedBrokerProviderToolRequestWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(stream, &request); err != nil {
		observation.err = err
		return
	}
	observation.request = request
	operation, normalizedRecord, err :=
		validateProtectedBrokerProviderToolRequestForTest(
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
	observation.operation = operation
	observation.normalizedRecord = normalizedRecord

	signer := h.responseSigner
	if signer == nil {
		signer = h.brokerKey
	}
	result := h.result
	if h.resultForOperation != nil {
		result = h.resultForOperation(operation)
	}
	response, err := newProtectedBrokerProviderToolResponseForTest(
		request,
		result,
		h.historical,
		h.brokerFailure,
		signer,
	)
	if err != nil {
		observation.err = err
		return
	}
	if h.responseMutation != nil {
		h.responseMutation(&response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		observation.err = err
		return
	}
	if h.rawMutation != nil {
		encoded = h.rawMutation(encoded)
	}
	if err := writeProtectedBrokerRPCRawFrameForTest(
		stream,
		encoded,
	); err != nil {
		observation.err = err
	}
}

func (h *protectedBrokerToolHarness) nextObservation(
	t *testing.T,
) protectedBrokerToolObservation {
	t.Helper()
	select {
	case observation := <-h.observations:
		return observation
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for provider Tool observation")
		return protectedBrokerToolObservation{}
	}
}

func (h *protectedBrokerToolHarness) requireClosedOnce(t *testing.T) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for index, stream := range h.streams {
		if stream.closeCount != 1 {
			t.Fatalf(
				"provider Tool stream %d close count = %d, want 1",
				index,
				stream.closeCount,
			)
		}
	}
}

func validateProtectedBrokerProviderToolRequestForTest(
	request protectedBrokerProviderToolRequestWireV1,
	clientAuthoritySHA256 protectedBrokerHashV1,
	transportSessionSHA256 protectedBrokerHashV1,
	identity ProtectedBrokerBackendIdentityV1,
	clientKey ed25519.PublicKey,
) (AgentOperation, []byte, error) {
	if request.Schema != protectedBrokerProviderToolRequestSchemaV1 ||
		request.Sequence != protectedBrokerProviderToolSequenceV1 ||
		request.ClientAuthoritySHA256 != clientAuthoritySHA256 ||
		request.TransportSessionSHA256 != transportSessionSHA256 ||
		request.BackendIdentitySHA256 != identity.identitySHA256 ||
		request.BrokerEpoch != identity.brokerEpoch ||
		request.ProfileSHA256 != identity.profileSHA256 {
		return AgentOperation{}, nil,
			errors.New("invalid provider Tool request binding")
	}
	operationJSON, err := decodeProtectedBrokerProviderRecordV1(
		request.OperationJSONBase64URL,
	)
	if err != nil {
		return AgentOperation{}, nil, err
	}
	normalizedRecord, err := decodeProtectedBrokerProviderRecordV1(
		request.NormalizedRecordBase64URL,
	)
	if err != nil {
		return AgentOperation{}, nil, err
	}
	if request.OperationPayloadSHA256 != hashProtectedBrokerBytesV1(
		protectedBrokerProviderToolOperationPayloadHashDomainV1,
		operationJSON,
	) ||
		request.NormalizedRecordPayloadSHA256 != hashProtectedBrokerBytesV1(
			protectedBrokerProviderToolNormalizedPayloadHashDomainV1,
			normalizedRecord,
		) {
		return AgentOperation{}, nil,
			errors.New("invalid provider Tool request payload hash")
	}
	expectedRequestSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderToolRequestHashDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.OperationSHA256,
		request.OperationPayloadSHA256,
		request.NormalizedRecordPayloadSHA256,
	)
	if err != nil {
		return AgentOperation{}, nil, err
	}
	if request.RequestSHA256 != expectedRequestSHA256 {
		return AgentOperation{}, nil,
			errors.New("invalid provider Tool request hash")
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerProviderToolRequestSignatureDomainV1,
		request.Signature,
		clientKey,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.OperationSHA256,
		request.OperationPayloadSHA256,
		request.NormalizedRecordPayloadSHA256,
		request.RequestSHA256,
	); err != nil {
		return AgentOperation{}, nil, err
	}
	record, err := decodeProviderV1Record(operationJSON)
	if err != nil {
		return AgentOperation{}, nil, err
	}
	operation, ok := record.value.(AgentOperation)
	if !ok ||
		operation.Kind() != OperationTool ||
		request.OperationSHA256.String() !=
			operation.CanonicalHash().String() {
		return AgentOperation{}, nil,
			errors.New("invalid provider Tool operation")
	}
	return operation, normalizedRecord, nil
}

func newProtectedBrokerProviderToolResponseForTest(
	request protectedBrokerProviderToolRequestWireV1,
	result OperationResult,
	historical bool,
	failure *protectedBrokerFailureClassV1,
	brokerKey ed25519.PrivateKey,
) (protectedBrokerProviderToolResponseWireV1, error) {
	var payload protectedBrokerProviderToolResponsePayloadV1
	if failure != nil {
		class := *failure
		payload.failure = &class
	} else {
		resultJSON, err := encodeProviderV1DTO(
			providerV1KindOperationResult,
			providerV1OperationResultDTO{
				Schema:            providerV1SchemaOperationResult,
				SessionID:         result.SessionID().String(),
				OperationID:       result.OperationID().String(),
				OperationSequence: result.Sequence().Uint64(),
				ResultKind:        string(result.ResultKind()),
				ArtifactHash:      result.ArtifactHash().String(),
				EvidenceHash:      result.EvidenceHash().String(),
				CanonicalHash:     result.CanonicalHash().String(),
			},
		)
		if err != nil {
			return protectedBrokerProviderToolResponseWireV1{}, err
		}
		resultSHA256, err := parseProtectedBrokerHashV1(
			result.CanonicalHash().String(),
		)
		if err != nil {
			return protectedBrokerProviderToolResponseWireV1{}, err
		}
		payload = protectedBrokerProviderToolResponsePayloadV1{
			historical: historical,
			success: &protectedBrokerProviderToolSuccessWireV1{
				OperationResultSHA256: resultSHA256,
				OperationResultJSONBase64URL: base64.RawURLEncoding.
					EncodeToString(resultJSON),
			},
		}
	}
	responsePayloadSHA256, err := hashProtectedBrokerJSONValueV1(
		protectedBrokerProviderToolResponsePayloadHashDomainV1,
		payload,
	)
	if err != nil {
		return protectedBrokerProviderToolResponseWireV1{}, err
	}
	responseSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderToolResponseHashDomainV1,
		protectedBrokerProviderToolResponseSchemaV1,
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
		return protectedBrokerProviderToolResponseWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerProviderToolResponseSignatureDomainV1,
		brokerKey,
		protectedBrokerProviderToolResponseSchemaV1,
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
		return protectedBrokerProviderToolResponseWireV1{}, err
	}
	return protectedBrokerProviderToolResponseWireV1{
		Schema:                 protectedBrokerProviderToolResponseSchemaV1,
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
