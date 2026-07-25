package planescapeprovider

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestProtectedBrokerProviderAdmissionMatchesPublishedFixture(t *testing.T) {
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
	plan, err := (ProviderV1FrameCodec{}).DecodeCompiledContainmentPlan(
		[]byte(fixture.ProviderAdmissionRPC.CompiledPlan.CanonicalJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	sequence := protectedBrokerRPCSequenceV1(2)
	planJSON, err := (ProviderV1FrameCodec{}).EncodeCompiledContainmentPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	request, err := newProtectedBrokerProviderAdmissionRequestV1(
		client,
		session,
		sequence,
		plan,
		planJSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(requestJSON); got != fixture.ProviderAdmissionRPC.Request.Wire.CanonicalJSON {
		t.Fatalf("provider admission request differs from Rust fixture\n got: %s\nwant: %s", got, fixture.ProviderAdmissionRPC.Request.Wire.CanonicalJSON)
	}
	var framedRequest bytes.Buffer
	if err := writeProtectedBrokerRPCJSONFrameV1(&framedRequest, request); err != nil {
		t.Fatal(err)
	}
	requireProtectedBrokerDiscoveryBase64Bytes(
		t,
		framedRequest.Bytes(),
		fixture.ProviderAdmissionRPC.Request.Wire.FramedBytesB64,
	)
	requireProtectedBrokerDiscoveryHashVector(
		t,
		fixture.ProviderAdmissionRPC.Request.PayloadHash,
		protectedBrokerProviderAdmissionRequestPayloadHashDomainV1,
		planJSON,
	)
	requireProtectedBrokerDiscoveryJSONHashVector(
		t,
		fixture.ProviderAdmissionRPC.Request.Hash,
		protectedBrokerProviderAdmissionRequestHashDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.CompiledPlanSHA256,
		request.RequestPayloadSHA256,
	)
	requireProtectedBrokerDiscoveryJSONSignatureVector(
		t,
		fixture.ProviderAdmissionRPC.Request.Signature,
		protectedBrokerProviderAdmissionRequestSignatureDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.CompiledPlanSHA256,
		request.RequestPayloadSHA256,
		request.RequestSHA256,
	)

	responseFrame := decodeProtectedBrokerDiscoveryBase64(
		t,
		fixture.ProviderAdmissionRPC.Response.Wire.FramedBytesB64,
	)
	var response protectedBrokerProviderAdmissionResponseWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(
		bytes.NewReader(responseFrame),
		&response,
	); err != nil {
		t.Fatal(err)
	}
	admission, err := response.validate(
		client,
		session,
		sequence,
		request.RequestSHA256,
		plan,
	)
	if admission.valid() {
		t.Fatal("published broker failure returned an admission")
	}
	var failure *ProviderFailure
	if !errors.As(err, &failure) ||
		failure.Code() != ProviderErrorUnsupported ||
		failure.CanonicalHash().String() != response.ResponseSHA256.String() {
		t.Fatalf("published broker failure = %#v, %v", failure, err)
	}
	requireProtectedBrokerAdmissionJSONValueHashVector(
		t,
		fixture.ProviderAdmissionRPC.Response.PayloadHash,
		protectedBrokerProviderAdmissionResponsePayloadHashDomainV1,
		response.Payload,
	)
	requireProtectedBrokerDiscoveryJSONHashVector(
		t,
		fixture.ProviderAdmissionRPC.Response.Hash,
		protectedBrokerProviderAdmissionResponseHashDomainV1,
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
		fixture.ProviderAdmissionRPC.Response.Signature,
		protectedBrokerProviderAdmissionResponseSignatureDomainV1,
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

func TestProtectedBrokerProviderAdmissionReconnectsFreshAndValidatesSuccess(
	t *testing.T,
) {
	harness := newProtectedBrokerAdmissionHarness(t)
	transport := mustProtectedBrokerAdmissionTransport(t, harness, harness.client)

	first, err := transport.Admit(context.Background(), harness.plan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := transport.Admit(context.Background(), harness.plan)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalHash() != second.CanonicalHash() {
		t.Fatal("admission changed across reconnect")
	}
	firstExchange := harness.nextObservation(t)
	secondExchange := harness.nextObservation(t)
	if firstExchange.err != nil {
		t.Fatal(firstExchange.err)
	}
	if secondExchange.err != nil {
		t.Fatal(secondExchange.err)
	}
	if firstExchange.request.Sequence != protectedBrokerProviderAdmissionSequenceV1 ||
		secondExchange.request.Sequence != protectedBrokerProviderAdmissionSequenceV1 {
		t.Fatal("admission did not restart at sequence 1")
	}
	if firstExchange.request.ClientAuthoritySHA256 != secondExchange.request.ClientAuthoritySHA256 {
		t.Fatal("stable admission authority changed")
	}
	if firstExchange.request.TransportSessionSHA256 == secondExchange.request.TransportSessionSHA256 {
		t.Fatal("admission transport session was reused")
	}
	wantPlan, err := (ProviderV1FrameCodec{}).EncodeCompiledContainmentPlan(harness.plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []protectedBrokerProviderAdmissionRequestWireV1{
		firstExchange.request,
		secondExchange.request,
	} {
		gotPlan, err := decodeProtectedBrokerProviderRecordV1(
			request.CompiledPlanJSONBase64,
		)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotPlan, wantPlan) {
			t.Fatal("admission did not transmit the exact compiled plan")
		}
	}
	harness.requireClosedOnce(t)
}

func TestProtectedBrokerProviderAdmissionAcceptsHistoricalEvidence(t *testing.T) {
	harness := newProtectedBrokerAdmissionHarness(t)
	harness.historical = true
	transport := mustProtectedBrokerAdmissionTransport(t, harness, harness.client)
	admission, err := transport.Admit(context.Background(), harness.plan)
	if err != nil {
		t.Fatal(err)
	}
	if !admission.valid() {
		t.Fatal("historical response did not return a validated admission")
	}
	observation := harness.nextObservation(t)
	if observation.err != nil {
		t.Fatal(observation.err)
	}
	harness.requireClosedOnce(t)
}

func TestProtectedBrokerEndpointPersistsAdmissionAcrossFreshDiscoveryAndAdmissionSessions(
	t *testing.T,
) {
	discoveryHarness := newProtectedBrokerDiscoveryHarness(t)
	admissionHarness := newProtectedBrokerAdmissionHarness(t)
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
	store := &MemoryStore{}
	client, err := NewClient(ClientConfig{
		Endpoint: endpoint,
		Store:    store,
		Now: func() time.Time {
			return time.UnixMilli(9_000).UTC()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := client.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input, err := NewAdmissionInput(admissionHarness.plan)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.Admit(context.Background(), discovery, input)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, ok := session.ID()
	if !ok {
		t.Fatal("productive protected endpoint returned no session")
	}
	state, err := client.loadState(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if state.providerID != admissionHarness.plan.ProviderID() ||
		state.epoch != admissionHarness.plan.ProviderEpoch() ||
		state.profile != admissionHarness.plan.ProviderProfile() ||
		state.requirementHash != admissionHarness.plan.Requirement().CanonicalHash() ||
		state.planHash != admissionHarness.plan.CanonicalHash() ||
		state.sessionCapabilityHash != admissionHarness.plan.ProviderCapabilityHash() {
		t.Fatal("durable state weakened protected admission bindings")
	}
	discoveryObservation := discoveryHarness.nextObservation(t)
	admissionObservation := admissionHarness.nextObservation(t)
	if discoveryObservation.err != nil {
		t.Fatal(discoveryObservation.err)
	}
	if admissionObservation.err != nil {
		t.Fatal(admissionObservation.err)
	}
	if discoveryObservation.request.TransportSessionSHA256 ==
		admissionObservation.request.TransportSessionSHA256 {
		t.Fatal("discovery and admission reused a transport session")
	}

	vectors := loadReleasedProviderV1Vectors(t)
	operation := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindOperation),
	).value.(AgentOperation)
	_, err = endpoint.Operate(context.Background(), operation)
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerInvalidRequestV1)
	_, err = endpoint.Freeze(context.Background(), Freeze{})
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerInvalidRequestV1)
	_, err = endpoint.Cancel(context.Background(), Cancellation{})
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerInvalidRequestV1)

	dialMu.Lock()
	gotDials := dialCount
	dialMu.Unlock()
	if gotDials != 2 {
		t.Fatalf("protected endpoint dial count = %d, want 2", gotDials)
	}
	discoveryHarness.requireClosedOnce(t)
	admissionHarness.requireClosedOnce(t)
}

func TestProtectedBrokerEndpointAdmissionUsesOneExactTCPAddress(t *testing.T) {
	harness := newProtectedBrokerAdmissionHarness(t)
	listener := mustProtectedBrokerLoopbackListener(t)
	t.Cleanup(func() {
		_ = listener.Close()
	})
	serverResult := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptTCP()
		if err != nil {
			serverResult <- err
			return
		}
		harness.serve(connection, 0x79)
		serverResult <- nil
	}()
	target := listener.Addr().(*net.TCPAddr).AddrPort()
	dialer, err := NewProtectedBrokerTCPDialerV1(target, time.Second)
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
	admission, err := endpoint.Admit(context.Background(), harness.plan)
	if err != nil {
		t.Fatal(err)
	}
	if !admission.valid() {
		t.Fatal("exact-address endpoint returned no admission")
	}
	observation := harness.nextObservation(t)
	if observation.err != nil {
		t.Fatal(observation.err)
	}
	select {
	case err := <-serverResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exact-address admission server did not finish")
	}
}

func TestProtectedBrokerProviderAdmissionMapsBrokerFailuresStably(t *testing.T) {
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
			harness := newProtectedBrokerAdmissionHarness(t)
			harness.brokerFailure = &class
			transport := mustProtectedBrokerAdmissionTransport(
				t,
				harness,
				harness.client,
			)
			admission, err := transport.Admit(context.Background(), harness.plan)
			if admission.valid() {
				t.Fatal("broker failure returned admission")
			}
			var failure *ProviderFailure
			if !errors.As(err, &failure) || failure.Code() != want {
				t.Fatalf("broker failure = %#v, %v; want %s", failure, err, want)
			}
			_ = harness.nextObservation(t)
			harness.requireClosedOnce(t)
		})
	}
}

func TestProtectedBrokerProviderAdmissionRejectsPlanBeforeDial(t *testing.T) {
	harness := newProtectedBrokerAdmissionHarness(t)
	tests := map[string]func(CompiledContainmentPlan) CompiledContainmentPlan{
		"invalid": func(CompiledContainmentPlan) CompiledContainmentPlan {
			return CompiledContainmentPlan{}
		},
		"cross authority": func(plan CompiledContainmentPlan) CompiledContainmentPlan {
			plan.requirement.authorityHash = mustFingerprintForTest(
				t,
				repeatedProtectedBrokerTestHash(0xa1),
			)
			return plan
		},
		"provider": func(plan CompiledContainmentPlan) CompiledContainmentPlan {
			plan.providerID = mustIdentifierForTest(
				t,
				repeatedProtectedBrokerTestHash(0xa2),
			)
			return plan
		},
		"epoch": func(plan CompiledContainmentPlan) CompiledContainmentPlan {
			plan.providerEpoch++
			return plan
		},
		"capability": func(plan CompiledContainmentPlan) CompiledContainmentPlan {
			plan.providerCapability = mustFingerprintForTest(
				t,
				repeatedProtectedBrokerTestHash(0xa3),
			)
			return plan
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			called := false
			dialer := ProtectedBrokerDialFuncV1(
				func(context.Context) (ProtectedBrokerStreamV1, error) {
					called = true
					return nil, errors.New("must not dial")
				},
			)
			transport := mustProtectedBrokerAdmissionTransport(
				t,
				dialer,
				harness.client,
			)
			admission, err := transport.Admit(
				context.Background(),
				mutate(harness.plan),
			)
			if admission.valid() || err == nil {
				t.Fatal("invalid plan reached durable admission")
			}
			if called {
				t.Fatal("invalid or cross-authority plan reached dial")
			}
		})
	}
}

func TestProtectedBrokerAdmissionHasNoRequirementOnlySurface(t *testing.T) {
	harness := newProtectedBrokerAdmissionHarness(t)
	transport := mustProtectedBrokerAdmissionTransport(t, harness, harness.client)
	endpoint, err := NewProtectedBrokerEndpointV1(
		ProtectedBrokerEndpointConfigV1{
			Dialer: harness,
			Client: harness.client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	type requirementOnlyAdmitter interface {
		Admit(context.Context, ExecutionRequirement) (SessionAdmission, error)
	}
	if _, ok := any(transport).(requirementOnlyAdmitter); ok {
		t.Fatal("protected admission transport exposes requirement-only admission")
	}
	if _, ok := any(endpoint).(requirementOnlyAdmitter); ok {
		t.Fatal("protected endpoint exposes requirement-only admission")
	}
}

func TestProtectedBrokerProviderAdmissionRejectsHostileResponses(t *testing.T) {
	tests := map[string]struct {
		configure func(*protectedBrokerAdmissionHarness)
		want      ProtectedBrokerTransportErrorClassV1
	}{
		"schema": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.Schema = "execution.protected-broker.provider-admission-response.v2"
				}
			},
			want: ProtectedBrokerInvalidSchemaV1,
		},
		"sequence gap": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.Sequence = 2
				}
			},
			want: ProtectedBrokerSequenceGapV1,
		},
		"client authority": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.ClientAuthoritySHA256 = mustProtectedBrokerDiscoveryHash(
						t,
						repeatedProtectedBrokerTestHash(0xb1),
					)
				}
			},
			want: ProtectedBrokerClientAuthorityMismatchV1,
		},
		"transport session": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.TransportSessionSHA256 = mustProtectedBrokerDiscoveryHash(
						t,
						repeatedProtectedBrokerTestHash(0xb2),
					)
				}
			},
			want: ProtectedBrokerTransportSessionMismatchV1,
		},
		"backend": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.BackendIdentitySHA256 = mustProtectedBrokerDiscoveryHash(
						t,
						repeatedProtectedBrokerTestHash(0xb3),
					)
				}
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"epoch": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.BrokerEpoch++
				}
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"profile": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.ProfileSHA256 = mustProtectedBrokerDiscoveryHash(
						t,
						repeatedProtectedBrokerTestHash(0xb4),
					)
				}
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"request": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.RequestSHA256 = mustProtectedBrokerDiscoveryHash(
						t,
						repeatedProtectedBrokerTestHash(0xb5),
					)
				}
			},
			want: ProtectedBrokerRequestHashMismatchV1,
		},
		"payload hash": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.ResponsePayloadSHA256 = mustProtectedBrokerDiscoveryHash(
						t,
						repeatedProtectedBrokerTestHash(0xb6),
					)
				}
			},
			want: ProtectedBrokerResponseHashMismatchV1,
		},
		"response hash": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.ResponseSHA256 = mustProtectedBrokerDiscoveryHash(
						t,
						repeatedProtectedBrokerTestHash(0xb7),
					)
				}
			},
			want: ProtectedBrokerResponseHashMismatchV1,
		},
		"wrong signer": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseSigner = ed25519.NewKeyFromSeed(
					bytes.Repeat([]byte{0xb8}, ed25519.SeedSize),
				)
			},
			want: ProtectedBrokerInvalidSignatureV1,
		},
		"signature": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.responseMutation = func(response *protectedBrokerProviderAdmissionResponseWireV1) {
					response.Signature = mutateProtectedBrokerSignature(response.Signature)
				}
			},
			want: ProtectedBrokerInvalidSignatureV1,
		},
		"unknown field": {
			configure: func(h *protectedBrokerAdmissionHarness) {
				h.rawResponseMutation = func(response []byte) []byte {
					return append(
						append([]byte(nil), response[:len(response)-1]...),
						[]byte(`,"unexpected":true}`)...,
					)
				}
			},
			want: ProtectedBrokerInvalidFrameV1,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			harness := newProtectedBrokerAdmissionHarness(t)
			test.configure(harness)
			transport := mustProtectedBrokerAdmissionTransport(
				t,
				harness,
				harness.client,
			)
			admission, err := transport.Admit(context.Background(), harness.plan)
			if admission.valid() {
				t.Fatal("hostile response returned an admission")
			}
			requireProtectedBrokerErrorClass(t, err, test.want)
			_ = harness.nextObservation(t)
			harness.requireClosedOnce(t)
		})
	}
}

func TestProtectedBrokerProviderAdmissionRejectsHostileSuccessEvidence(t *testing.T) {
	tests := map[string]func(*protectedBrokerProviderAdmissionResponsePayloadV1){
		"session admission hash": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			payload.success.SessionAdmissionSHA256 = mustProtectedBrokerDiscoveryHash(
				t,
				repeatedProtectedBrokerTestHash(0xc1),
			)
		},
		"session provider": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			payload.success.SessionAdmissionJSONBase64URL =
				mutatedProtectedBrokerSessionAdmissionForTest(
					t,
					payload.success.SessionAdmissionJSONBase64URL,
					func(dto *providerV1AdmissionDTO) {
						dto.ProviderID = repeatedProtectedBrokerTestHash(0xc2)
					},
				)
		},
		"session capability": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			payload.success.SessionAdmissionJSONBase64URL =
				mutatedProtectedBrokerSessionAdmissionForTest(
					t,
					payload.success.SessionAdmissionJSONBase64URL,
					func(dto *providerV1AdmissionDTO) {
						dto.SessionCapabilityHash = repeatedProtectedBrokerTestHash(0xc3)
					},
				)
		},
		"session requirement": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			payload.success.SessionAdmissionJSONBase64URL =
				mutatedProtectedBrokerSessionAdmissionForTest(
					t,
					payload.success.SessionAdmissionJSONBase64URL,
					func(dto *providerV1AdmissionDTO) {
						dto.RequirementHash = repeatedProtectedBrokerTestHash(0xc4)
					},
				)
		},
		"session compiled plan": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			payload.success.SessionAdmissionJSONBase64URL =
				mutatedProtectedBrokerSessionAdmissionForTest(
					t,
					payload.success.SessionAdmissionJSONBase64URL,
					func(dto *providerV1AdmissionDTO) {
						dto.CompiledPlanHash = repeatedProtectedBrokerTestHash(0xc5)
					},
				)
		},
		"session deadline": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			payload.success.SessionAdmissionJSONBase64URL =
				mutatedProtectedBrokerSessionAdmissionForTest(
					t,
					payload.success.SessionAdmissionJSONBase64URL,
					func(dto *providerV1AdmissionDTO) {
						dto.ExpiresAtMS++
					},
				)
		},
		"nested outcome": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			payload.success.Evidence.historical = !payload.historical
		},
		"nested lease": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			payload.success.Evidence.success.LeaseID = "different-lease"
		},
		"nested failure": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			class := protectedBrokerFailureEvidenceConflictV1
			payload.success.Evidence = protectedBrokerAdmissionResponsePayloadWireV1{
				failure: &class,
			}
		},
		"preflight signature": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			var preflight protectedBrokerContainmentPreflightWireV2
			decodeProtectedBrokerAdmissionEvidenceForTest(
				t,
				payload.success.Evidence.success.PreflightJSONBase64URL,
				&preflight,
			)
			preflight.Signature = mutateProtectedBrokerSignature(preflight.Signature)
			payload.success.Evidence.success.PreflightJSONBase64URL =
				encodeProtectedBrokerAdmissionEvidenceForTest(t, preflight)
		},
		"manifest signature": func(payload *protectedBrokerProviderAdmissionResponsePayloadV1) {
			var manifest protectedBrokerToolTranscriptManifestWireV2
			decodeProtectedBrokerAdmissionEvidenceForTest(
				t,
				payload.success.Evidence.success.InitialManifestJSONBase64URL,
				&manifest,
			)
			manifest.Signature = mutateProtectedBrokerSignature(manifest.Signature)
			payload.success.Evidence.success.InitialManifestJSONBase64URL =
				encodeProtectedBrokerAdmissionEvidenceForTest(t, manifest)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			harness := newProtectedBrokerAdmissionHarness(t)
			harness.payloadMutation = mutate
			transport := mustProtectedBrokerAdmissionTransport(
				t,
				harness,
				harness.client,
			)
			admission, err := transport.Admit(context.Background(), harness.plan)
			if admission.valid() {
				t.Fatal("hostile nested evidence returned an admission")
			}
			var transportErr *ProtectedBrokerTransportErrorV1
			if !errors.As(err, &transportErr) ||
				(transportErr.Class() != ProtectedBrokerInvalidEvidenceV1 &&
					transportErr.Class() != ProtectedBrokerResponseHashMismatchV1) {
				t.Fatalf("hostile nested evidence error = %T %v", err, err)
			}
			_ = harness.nextObservation(t)
			harness.requireClosedOnce(t)
		})
	}

	t.Run("signed preflight resource mismatch", func(t *testing.T) {
		harness := newProtectedBrokerAdmissionHarness(t)
		harness.payloadMutation = func(
			payload *protectedBrokerProviderAdmissionResponsePayloadV1,
		) {
			var preflight protectedBrokerContainmentPreflightWireV2
			decodeProtectedBrokerAdmissionEvidenceForTest(
				t,
				payload.success.Evidence.success.PreflightJSONBase64URL,
				&preflight,
			)
			preflight.Realization.EffectiveNofilePerProcess++
			resignProtectedBrokerPreflightForTest(t, &preflight, harness.brokerKey)
			payload.success.Evidence.success.PreflightEvidenceSHA256 =
				preflight.EvidenceSHA256
			payload.success.Evidence.success.PreflightJSONBase64URL =
				encodeProtectedBrokerAdmissionEvidenceForTest(t, preflight)
		}
		transport := mustProtectedBrokerAdmissionTransport(
			t,
			harness,
			harness.client,
		)
		admission, err := transport.Admit(context.Background(), harness.plan)
		if admission.valid() {
			t.Fatal("signed invalid preflight returned an admission")
		}
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerInvalidEvidenceV1)
		_ = harness.nextObservation(t)
		harness.requireClosedOnce(t)
	})

	t.Run("signed nonzero initial manifest binding", func(t *testing.T) {
		harness := newProtectedBrokerAdmissionHarness(t)
		harness.payloadMutation = func(
			payload *protectedBrokerProviderAdmissionResponsePayloadV1,
		) {
			var manifest protectedBrokerToolTranscriptManifestWireV2
			decodeProtectedBrokerAdmissionEvidenceForTest(
				t,
				payload.success.Evidence.success.InitialManifestJSONBase64URL,
				&manifest,
			)
			manifest.State.ZeroHeadSHA256 = mustProtectedBrokerHashValueForTest(
				repeatedProtectedBrokerTestHash(0xc6),
			)
			resignProtectedBrokerInitialManifestForTest(
				t,
				&manifest,
				harness.brokerKey,
			)
			payload.success.Evidence.success.InitialManifestSHA256 =
				manifest.ManifestSHA256
			payload.success.Evidence.success.InitialManifestJSONBase64URL =
				encodeProtectedBrokerAdmissionEvidenceForTest(t, manifest)
		}
		transport := mustProtectedBrokerAdmissionTransport(
			t,
			harness,
			harness.client,
		)
		admission, err := transport.Admit(context.Background(), harness.plan)
		if admission.valid() {
			t.Fatal("signed invalid initial manifest returned an admission")
		}
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerInvalidEvidenceV1)
		_ = harness.nextObservation(t)
		harness.requireClosedOnce(t)
	})
}

func TestProtectedBrokerProviderAdmissionContextAndCloseBehavior(t *testing.T) {
	t.Run("canceled before dial", func(t *testing.T) {
		harness := newProtectedBrokerAdmissionHarness(t)
		called := false
		dialer := ProtectedBrokerDialFuncV1(
			func(context.Context) (ProtectedBrokerStreamV1, error) {
				called = true
				return nil, errors.New("must not dial")
			},
		)
		transport := mustProtectedBrokerAdmissionTransport(t, dialer, harness.client)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := transport.Admit(ctx, harness.plan)
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerUnavailableV1)
		if called {
			t.Fatal("canceled admission dialed")
		}
	})

	t.Run("cancellation interrupts handshake", func(t *testing.T) {
		harness := newProtectedBrokerAdmissionHarness(t)
		harness.blockAfterHello = true
		transport := mustProtectedBrokerAdmissionTransport(t, harness, harness.client)
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, err := transport.Admit(ctx, harness.plan)
			result <- err
		}()
		harness.waitUntilBlocked(t)
		cancel()
		select {
		case err := <-result:
			requireProtectedBrokerErrorClass(t, err, ProtectedBrokerUnavailableV1)
		case <-time.After(2 * time.Second):
			t.Fatal("cancellation did not interrupt admission handshake")
		}
		_ = harness.nextObservation(t)
		harness.requireClosedOnce(t)
	})

	t.Run("deadline is applied", func(t *testing.T) {
		harness := newProtectedBrokerAdmissionHarness(t)
		harness.blockAfterHello = true
		transport := mustProtectedBrokerAdmissionTransport(t, harness, harness.client)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		wantDeadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("test context has no deadline")
		}
		_, err := transport.Admit(ctx, harness.plan)
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerUnavailableV1)
		_ = harness.nextObservation(t)
		streams := harness.snapshotStreams()
		if len(streams) != 1 {
			t.Fatalf("stream count = %d, want 1", len(streams))
		}
		deadlines := streams[0].snapshotDeadlines()
		if len(deadlines) == 0 || !deadlines[0].Equal(wantDeadline) {
			t.Fatalf("initial stream deadline = %v, want %v", deadlines, wantDeadline)
		}
		harness.requireClosedOnce(t)
	})

	t.Run("close failure withholds success", func(t *testing.T) {
		harness := newProtectedBrokerAdmissionHarness(t)
		harness.clientCloseErr = errors.New("raw-close-peer-material")
		transport := mustProtectedBrokerAdmissionTransport(t, harness, harness.client)
		admission, err := transport.Admit(context.Background(), harness.plan)
		if admission.valid() {
			t.Fatal("close failure returned a validated admission")
		}
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerIOV1)
		_ = harness.nextObservation(t)
		harness.requireClosedOnce(t)
	})
}

type protectedBrokerAdmissionHarness struct {
	client          *ProtectedBrokerClientV1
	brokerKey       ed25519.PrivateKey
	clientPublicKey ed25519.PublicKey
	identity        ProtectedBrokerBackendIdentityV1
	plan            CompiledContainmentPlan

	mu                  sync.Mutex
	nextServerNonce     byte
	streams             []*protectedBrokerTrackingStream
	historical          bool
	brokerFailure       *protectedBrokerFailureClassV1
	payloadMutation     func(*protectedBrokerProviderAdmissionResponsePayloadV1)
	responseMutation    func(*protectedBrokerProviderAdmissionResponseWireV1)
	rawResponseMutation func([]byte) []byte
	responseSigner      ed25519.PrivateKey
	blockAfterHello     bool
	clientCloseErr      error
	observations        chan protectedBrokerAdmissionObservation
	blocked             chan struct{}
}

type protectedBrokerAdmissionObservation struct {
	request protectedBrokerProviderAdmissionRequestWireV1
	err     error
}

func newProtectedBrokerAdmissionHarness(t *testing.T) *protectedBrokerAdmissionHarness {
	t.Helper()
	interop := loadProtectedBrokerInteropFixture(t)
	brokerKey, clientKey, identity, client, _ := protectedBrokerFixtureClient(t, interop)
	fixture := loadProtectedBrokerDiscoveryFixture(t)
	plan, err := (ProviderV1FrameCodec{}).DecodeCompiledContainmentPlan(
		[]byte(fixture.ProviderAdmissionRPC.CompiledPlan.CanonicalJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	return &protectedBrokerAdmissionHarness{
		client:          client,
		brokerKey:       brokerKey,
		clientPublicKey: clientKey.Public().(ed25519.PublicKey),
		identity:        identity,
		plan:            plan,
		nextServerNonce: 0x70,
		observations:    make(chan protectedBrokerAdmissionObservation, 8),
		blocked:         make(chan struct{}),
	}
}

func (h *protectedBrokerAdmissionHarness) DialContext(
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

func (h *protectedBrokerAdmissionHarness) serve(
	stream net.Conn,
	serverNonce byte,
) {
	observation := protectedBrokerAdmissionObservation{}
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
	if h.blockAfterHello {
		close(h.blocked)
		var one [1]byte
		_, observation.err = stream.Read(one[:])
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
	var request protectedBrokerProviderAdmissionRequestWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(stream, &request); err != nil {
		observation.err = err
		return
	}
	observation.request = request
	if err := validateProtectedBrokerAdmissionRequestForTest(
		request,
		clientAuthority,
		transportSession,
		h.identity,
		h.plan,
		h.clientPublicKey,
	); err != nil {
		observation.err = err
		return
	}
	var payload protectedBrokerProviderAdmissionResponsePayloadV1
	if h.brokerFailure != nil {
		class := *h.brokerFailure
		payload.failure = &class
	} else {
		payload, err = newProtectedBrokerAdmissionSuccessPayloadForTest(
			h.plan,
			h.identity,
			h.brokerKey,
			h.historical,
		)
		if err != nil {
			observation.err = err
			return
		}
	}
	if h.payloadMutation != nil {
		h.payloadMutation(&payload)
	}
	signer := h.responseSigner
	if signer == nil {
		signer = h.brokerKey
	}
	response, err := newProtectedBrokerAdmissionResponseForTest(
		request,
		payload,
		signer,
	)
	if err != nil {
		observation.err = err
		return
	}
	if h.responseMutation != nil {
		h.responseMutation(&response)
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		observation.err = err
		return
	}
	if h.rawResponseMutation != nil {
		responseJSON = h.rawResponseMutation(responseJSON)
	}
	observation.err = writeProtectedBrokerRPCRawFrameForTest(stream, responseJSON)
}

func (h *protectedBrokerAdmissionHarness) nextObservation(
	t *testing.T,
) protectedBrokerAdmissionObservation {
	t.Helper()
	select {
	case observation := <-h.observations:
		return observation
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for protected broker admission server")
		return protectedBrokerAdmissionObservation{}
	}
}

func (h *protectedBrokerAdmissionHarness) waitUntilBlocked(t *testing.T) {
	t.Helper()
	select {
	case <-h.blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("protected broker admission server did not block")
	}
}

func (h *protectedBrokerAdmissionHarness) snapshotStreams() []*protectedBrokerTrackingStream {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*protectedBrokerTrackingStream(nil), h.streams...)
}

func (h *protectedBrokerAdmissionHarness) requireClosedOnce(t *testing.T) {
	t.Helper()
	streams := h.snapshotStreams()
	if len(streams) == 0 {
		t.Fatal("protected broker admission was never dialed")
	}
	for index, stream := range streams {
		if got := stream.closed(); got != 1 {
			t.Fatalf("stream[%d] close count = %d, want 1", index, got)
		}
	}
}

func validateProtectedBrokerAdmissionRequestForTest(
	request protectedBrokerProviderAdmissionRequestWireV1,
	clientAuthoritySHA256 protectedBrokerHashV1,
	transportSessionSHA256 protectedBrokerHashV1,
	identity ProtectedBrokerBackendIdentityV1,
	plan CompiledContainmentPlan,
	clientKey ed25519.PublicKey,
) error {
	if request.Schema != protectedBrokerProviderAdmissionRequestSchemaV1 ||
		request.Sequence != protectedBrokerProviderAdmissionSequenceV1 ||
		request.ClientAuthoritySHA256 != clientAuthoritySHA256 ||
		request.TransportSessionSHA256 != transportSessionSHA256 ||
		request.BackendIdentitySHA256 != identity.identitySHA256 ||
		request.BrokerEpoch != identity.brokerEpoch ||
		request.ProfileSHA256 != identity.profileSHA256 ||
		request.CompiledPlanSHA256.String() != plan.CanonicalHash().String() {
		return errors.New("invalid protected provider admission request binding")
	}
	planJSON, err := decodeProtectedBrokerProviderRecordV1(
		request.CompiledPlanJSONBase64,
	)
	if err != nil {
		return err
	}
	expectedPlanJSON, err := (ProviderV1FrameCodec{}).EncodeCompiledContainmentPlan(plan)
	if err != nil || !bytes.Equal(planJSON, expectedPlanJSON) {
		return errors.New("protected provider admission plan changed")
	}
	expectedPayload := hashProtectedBrokerBytesV1(
		protectedBrokerProviderAdmissionRequestPayloadHashDomainV1,
		planJSON,
	)
	if request.RequestPayloadSHA256 != expectedPayload {
		return errors.New("invalid protected provider admission payload hash")
	}
	expectedRequest, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderAdmissionRequestHashDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.CompiledPlanSHA256,
		request.RequestPayloadSHA256,
	)
	if err != nil || request.RequestSHA256 != expectedRequest {
		return errors.New("invalid protected provider admission request hash")
	}
	return verifyProtectedBrokerJSONV1(
		protectedBrokerProviderAdmissionRequestSignatureDomainV1,
		request.Signature,
		clientKey,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.CompiledPlanSHA256,
		request.RequestPayloadSHA256,
		request.RequestSHA256,
	)
}

func newProtectedBrokerAdmissionResponseForTest(
	request protectedBrokerProviderAdmissionRequestWireV1,
	payload protectedBrokerProviderAdmissionResponsePayloadV1,
	brokerKey ed25519.PrivateKey,
) (protectedBrokerProviderAdmissionResponseWireV1, error) {
	responsePayloadSHA256, err := hashProtectedBrokerJSONValueV1(
		protectedBrokerProviderAdmissionResponsePayloadHashDomainV1,
		payload,
	)
	if err != nil {
		return protectedBrokerProviderAdmissionResponseWireV1{}, err
	}
	responseSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderAdmissionResponseHashDomainV1,
		protectedBrokerProviderAdmissionResponseSchemaV1,
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
		return protectedBrokerProviderAdmissionResponseWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerProviderAdmissionResponseSignatureDomainV1,
		brokerKey,
		protectedBrokerProviderAdmissionResponseSchemaV1,
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
		return protectedBrokerProviderAdmissionResponseWireV1{}, err
	}
	return protectedBrokerProviderAdmissionResponseWireV1{
		Schema:                 protectedBrokerProviderAdmissionResponseSchemaV1,
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

func newProtectedBrokerAdmissionSuccessPayloadForTest(
	plan CompiledContainmentPlan,
	identity ProtectedBrokerBackendIdentityV1,
	brokerKey ed25519.PrivateKey,
	historical bool,
) (protectedBrokerProviderAdmissionResponsePayloadV1, error) {
	const leaseID = "protected-provider-lease"
	admissionJSON, admission, err := protectedBrokerSessionAdmissionForTest(
		plan,
		leaseID,
		nil,
	)
	if err != nil {
		return protectedBrokerProviderAdmissionResponsePayloadV1{}, err
	}
	preflightJSON, preflight, err := protectedBrokerPreflightForTest(
		plan,
		identity,
		brokerKey,
		leaseID,
	)
	if err != nil {
		return protectedBrokerProviderAdmissionResponsePayloadV1{}, err
	}
	manifestJSON, manifest, err := protectedBrokerInitialManifestForTest(
		preflight,
		brokerKey,
	)
	if err != nil {
		return protectedBrokerProviderAdmissionResponsePayloadV1{}, err
	}
	nested := protectedBrokerAdmissionResponsePayloadWireV1{
		historical: historical,
		success: &protectedBrokerAdmissionEvidenceSuccessWireV1{
			LeaseID:                      leaseID,
			LeaseRevision:                1,
			LeasePhase:                   protectedBrokerLeaseActiveV1,
			LeaseStateSHA256:             mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xd1)),
			PreflightEvidenceSHA256:      preflight.EvidenceSHA256,
			InitialManifestSHA256:        manifest.ManifestSHA256,
			PreflightJSONBase64URL:       base64.RawURLEncoding.EncodeToString(preflightJSON),
			InitialManifestJSONBase64URL: base64.RawURLEncoding.EncodeToString(manifestJSON),
		},
	}
	admissionSHA256, err := parseProtectedBrokerHashV1(
		admission.CanonicalHash().String(),
	)
	if err != nil {
		return protectedBrokerProviderAdmissionResponsePayloadV1{}, err
	}
	return protectedBrokerProviderAdmissionResponsePayloadV1{
		historical: historical,
		success: &protectedBrokerProviderAdmissionSuccessWireV1{
			SessionAdmissionSHA256:        admissionSHA256,
			SessionAdmissionJSONBase64URL: base64.RawURLEncoding.EncodeToString(admissionJSON),
			Evidence:                      nested,
		},
	}, nil
}

func protectedBrokerSessionAdmissionForTest(
	plan CompiledContainmentPlan,
	sessionID string,
	mutate func(*providerV1AdmissionDTO),
) ([]byte, SessionAdmission, error) {
	dto := providerV1AdmissionDTO{
		Schema:                providerV1SchemaAdmission,
		SessionID:             sessionID,
		ProviderID:            plan.ProviderID().String(),
		ProviderEpoch:         plan.ProviderEpoch().Uint64(),
		RequirementHash:       plan.Requirement().CanonicalHash().String(),
		CompiledPlanHash:      plan.CanonicalHash().String(),
		SessionCapabilityHash: plan.ProviderCapabilityHash().String(),
		ExpiresAtMS:           plan.containmentRequest.deadlineAtMS,
	}
	if mutate != nil {
		mutate(&dto)
	}
	preimage, err := dto.canonicalPreimage()
	if err != nil {
		return nil, SessionAdmission{}, err
	}
	dto.CanonicalHash = providerV1CanonicalHash(preimage)
	encoded, err := encodeProviderV1DTO(providerV1KindAdmission, dto)
	if err != nil {
		return nil, SessionAdmission{}, err
	}
	admission, err := (ProviderV1FrameCodec{}).DecodeAdmission(encoded)
	if err != nil {
		return nil, SessionAdmission{}, err
	}
	return encoded, admission, nil
}

func mutatedProtectedBrokerSessionAdmissionForTest(
	t *testing.T,
	encoded string,
	mutate func(*providerV1AdmissionDTO),
) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var dto providerV1AdmissionDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		t.Fatal(err)
	}
	mutate(&dto)
	preimage, err := dto.canonicalPreimage()
	if err != nil {
		t.Fatal(err)
	}
	dto.CanonicalHash = providerV1CanonicalHash(preimage)
	raw, err = encodeProviderV1DTO(providerV1KindAdmission, dto)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func protectedBrokerPreflightForTest(
	plan CompiledContainmentPlan,
	identity ProtectedBrokerBackendIdentityV1,
	brokerKey ed25519.PrivateKey,
	leaseID string,
) ([]byte, protectedBrokerContainmentPreflightWireV2, error) {
	var request providerV1ContainmentLeaseRequestDTO
	if err := json.Unmarshal(plan.ContainmentRequestJSON(), &request); err != nil {
		return nil, protectedBrokerContainmentPreflightWireV2{}, err
	}
	backend := protectedBrokerContainmentBackendIdentityWireV1{
		Backend:                    protectedBrokerBackendKindV1,
		BackendInstanceSHA256:      identity.backendInstanceSHA256,
		ExecutableSHA256:           identity.executableSHA256,
		ExecutionEnvironmentSHA256: identity.executionEnvironmentSHA256,
		ProfileSHA256:              identity.profileSHA256,
		Epoch:                      identity.brokerEpoch,
		AttestorPublicKeySHA256:    identity.attestorPublicKeySHA256,
		IdentitySHA256:             identity.identitySHA256,
	}
	cgroup := mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xd2))
	quota := mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xd3))
	root := mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xd4))
	nofile := request.Contract.Resources.AggregateOpenFiles /
		request.Contract.Resources.Tasks
	evidence := protectedBrokerAggregateResourceEvidenceV2{
		Sources: protectedBrokerResourceEvidenceSourcesV2{
			Cgroup: protectedBrokerCgroupEvidenceSourceV2{
				CgroupSHA256: cgroup,
			},
			Quota: protectedBrokerQuotaEvidenceSourceV2{
				QuotaProjectSHA256:  quota,
				WorkspaceRootSHA256: root,
			},
			WorkerLimits: protectedBrokerWorkerLimitEvidenceSourceV2{
				CgroupSHA256:         cgroup,
				BackendProfileSHA256: identity.profileSHA256,
			},
			LogicalScanner: protectedBrokerLogicalScannerEvidenceSourceV2{
				WorkspaceRootSHA256:   root,
				BackendProfileSHA256:  identity.profileSHA256,
				ScannerSnapshotSHA256: mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xd5)),
			},
		},
		AggregateOpenFiles: protectedBrokerAggregateOpenFilesProofV2{
			TasksMax:                    request.Contract.Resources.Tasks,
			NofilePerProcess:            nofile,
			RequestedAggregateOpenFiles: request.Contract.Resources.AggregateOpenFiles,
			EnforcedCeiling:             request.Contract.Resources.Tasks * nofile,
		},
		WorkspaceAllocatedBytes: protectedBrokerWorkspaceAllocatedBytesSnapshotV2{
			HardLimitBytes: request.Contract.Resources.WorkspaceAllocatedBytes,
		},
		WorkspaceInodes: protectedBrokerWorkspaceInodesSnapshotV2{
			HardLimitInodes: request.Contract.Resources.WorkspaceInodes,
		},
		LogicalFileSize: protectedBrokerLogicalFileSizeSnapshotV2{
			RlimitFsizeBytes: request.Contract.Resources.LogicalFileBytes,
		},
	}
	snapshot := protectedBrokerResourceEvidenceSnapshotV2{
		Schema:       protectedBrokerResourceEvidenceSnapshotSchemaV2,
		ObservedAtMS: 1,
		Evidence:     evidence,
	}
	snapshotHash, err := hashProtectedBrokerJSONV1(
		protectedBrokerResourceEvidenceSnapshotHashDomainV2,
		snapshot.Schema,
		snapshot.ObservedAtMS,
		snapshot.Evidence,
	)
	if err != nil {
		return nil, protectedBrokerContainmentPreflightWireV2{}, err
	}
	snapshot.SnapshotSHA256 = snapshotHash
	realization := protectedBrokerContainmentRealizationV1{
		BackendSessionSHA256:           mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xd6)),
		MountNamespaceSHA256:           mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xd7)),
		PIDNamespaceSHA256:             mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xd8)),
		UserNamespaceSHA256:            mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xd9)),
		NetworkNamespaceSHA256:         mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xda)),
		CgroupSHA256:                   cgroup,
		QuotaProjectSHA256:             quota,
		WorkspaceRootSHA256:            root,
		EvidenceStoreSHA256:            mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xdb)),
		InitialCandidateManifestSHA256: mustProtectedBrokerHashValueForTest(repeatedProtectedBrokerTestHash(0xdc)),
		InitialResourceEvidence:        snapshot,
		MinimumWorkerNofile:            1,
		EffectiveNofilePerProcess:      nofile,
	}
	leaseRequestSHA256, err := parseProtectedBrokerHashV1(request.RequestSHA256)
	if err != nil {
		return nil, protectedBrokerContainmentPreflightWireV2{}, err
	}
	transcriptZero, err := hashProtectedBrokerJSONV1(
		protectedBrokerTranscriptZeroHashDomainV2,
		request.Scope.ScopeSHA256,
		request.Contract.ContractSHA256,
		identity.identitySHA256,
		leaseID,
	)
	if err != nil {
		return nil, protectedBrokerContainmentPreflightWireV2{}, err
	}
	preflight := protectedBrokerContainmentPreflightWireV2{
		Schema:                   protectedBrokerContainmentPreflightSchemaV2,
		Scope:                    request.Scope,
		Contract:                 request.Contract,
		LeaseRequestSHA256:       leaseRequestSHA256,
		BackendIdentity:          backend,
		Realization:              realization,
		LeaseID:                  leaseID,
		TranscriptZeroHeadSHA256: transcriptZero,
	}
	preflight.Signature, err = signProtectedBrokerJSONV1(
		protectedBrokerPreflightSignatureDomainV2,
		brokerKey,
		preflight.Schema,
		preflight.Scope,
		preflight.Contract,
		preflight.LeaseRequestSHA256,
		preflight.BackendIdentity,
		preflight.Realization,
		preflight.LeaseID,
		preflight.TranscriptZeroHeadSHA256,
	)
	if err != nil {
		return nil, protectedBrokerContainmentPreflightWireV2{}, err
	}
	preflight.EvidenceSHA256, err = hashProtectedBrokerJSONV1(
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
	if err != nil {
		return nil, protectedBrokerContainmentPreflightWireV2{}, err
	}
	encoded, err := json.Marshal(preflight)
	return encoded, preflight, err
}

func protectedBrokerInitialManifestForTest(
	preflight protectedBrokerContainmentPreflightWireV2,
	brokerKey ed25519.PrivateKey,
) ([]byte, protectedBrokerToolTranscriptManifestWireV2, error) {
	scopeSHA256, err := parseProtectedBrokerHashV1(preflight.Scope.ScopeSHA256)
	if err != nil {
		return nil, protectedBrokerToolTranscriptManifestWireV2{}, err
	}
	manifest := protectedBrokerToolTranscriptManifestWireV2{
		Schema:                  protectedBrokerToolTranscriptManifestSchemaV2,
		ScopeSHA256:             scopeSHA256,
		WorkspaceID:             preflight.Scope.WorkspaceID,
		PreflightEvidenceSHA256: preflight.EvidenceSHA256,
		BackendIdentitySHA256:   preflight.BackendIdentity.IdentitySHA256,
		BrokerEpoch:             preflight.BackendIdentity.Epoch,
		LeaseID:                 preflight.LeaseID,
		State: protectedBrokerExplicitZeroTranscriptStateV2{
			State:          protectedBrokerTranscriptStateExplicitZeroV1,
			ZeroHeadSHA256: preflight.TranscriptZeroHeadSHA256,
		},
	}
	manifest.Signature, err = signProtectedBrokerJSONV1(
		protectedBrokerManifestSignatureDomainV2,
		brokerKey,
		manifest.Schema,
		manifest.ScopeSHA256,
		manifest.WorkspaceID,
		manifest.PreflightEvidenceSHA256,
		manifest.BackendIdentitySHA256,
		manifest.BrokerEpoch,
		manifest.LeaseID,
		manifest.State,
	)
	if err != nil {
		return nil, protectedBrokerToolTranscriptManifestWireV2{}, err
	}
	manifest.ManifestSHA256, err = hashProtectedBrokerJSONV1(
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
	if err != nil {
		return nil, protectedBrokerToolTranscriptManifestWireV2{}, err
	}
	encoded, err := json.Marshal(manifest)
	return encoded, manifest, err
}

func resignProtectedBrokerPreflightForTest(
	t *testing.T,
	preflight *protectedBrokerContainmentPreflightWireV2,
	brokerKey ed25519.PrivateKey,
) {
	t.Helper()
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerPreflightSignatureDomainV2,
		brokerKey,
		preflight.Schema,
		preflight.Scope,
		preflight.Contract,
		preflight.LeaseRequestSHA256,
		preflight.BackendIdentity,
		preflight.Realization,
		preflight.LeaseID,
		preflight.TranscriptZeroHeadSHA256,
	)
	if err != nil {
		t.Fatal(err)
	}
	preflight.Signature = signature
	hash, err := hashProtectedBrokerJSONV1(
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
	if err != nil {
		t.Fatal(err)
	}
	preflight.EvidenceSHA256 = hash
}

func resignProtectedBrokerInitialManifestForTest(
	t *testing.T,
	manifest *protectedBrokerToolTranscriptManifestWireV2,
	brokerKey ed25519.PrivateKey,
) {
	t.Helper()
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerManifestSignatureDomainV2,
		brokerKey,
		manifest.Schema,
		manifest.ScopeSHA256,
		manifest.WorkspaceID,
		manifest.PreflightEvidenceSHA256,
		manifest.BackendIdentitySHA256,
		manifest.BrokerEpoch,
		manifest.LeaseID,
		manifest.State,
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Signature = signature
	hash, err := hashProtectedBrokerJSONV1(
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
	if err != nil {
		t.Fatal(err)
	}
	manifest.ManifestSHA256 = hash
}

func mustProtectedBrokerAdmissionTransport(
	t *testing.T,
	dialer ProtectedBrokerDialerV1,
	client *ProtectedBrokerClientV1,
) *ProtectedBrokerAdmissionTransportV1 {
	t.Helper()
	transport, err := NewProtectedBrokerAdmissionTransportV1(
		ProtectedBrokerAdmissionTransportConfigV1{
			Dialer: dialer,
			Client: client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func mustProtectedBrokerHashValueForTest(value string) protectedBrokerHashV1 {
	hash, err := parseProtectedBrokerHashV1(value)
	if err != nil {
		panic(err)
	}
	return hash
}

func mustFingerprintForTest(t *testing.T, value string) Fingerprint {
	t.Helper()
	fingerprint, err := ParseFingerprint(value)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func mustIdentifierForTest(t *testing.T, value string) Identifier {
	t.Helper()
	identifier, err := NewIdentifier(value)
	if err != nil {
		t.Fatal(err)
	}
	return identifier
}

func decodeProtectedBrokerAdmissionEvidenceForTest(
	t *testing.T,
	encoded string,
	target any,
) {
	t.Helper()
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}

func encodeProtectedBrokerAdmissionEvidenceForTest(
	t *testing.T,
	value any,
) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func requireProtectedBrokerAdmissionJSONValueHashVector(
	t *testing.T,
	vector protectedBrokerDiscoveryHashFixtureV1,
	domain string,
	value any,
) {
	t.Helper()
	requireProtectedBrokerDiscoveryBase64Bytes(t, []byte(domain), vector.DomainBase64)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	preimage := append(append([]byte(nil), domain...), encoded...)
	requireProtectedBrokerDiscoveryBase64Bytes(t, preimage, vector.PreimageBase64)
	hash, err := hashProtectedBrokerJSONValueV1(domain, value)
	if err != nil {
		t.Fatal(err)
	}
	if hash.String() != vector.SHA256 {
		t.Fatalf("published hash = %q, derived %q", vector.SHA256, hash.String())
	}
}

func writeOversizedProtectedBrokerAdmissionFrameForTest(
	writer net.Conn,
) error {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], MaxProtectedBrokerRPCFrameBytesV1+1)
	return writeProtectedBrokerBytes(writer, header[:])
}
