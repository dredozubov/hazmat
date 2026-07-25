package planescapeprovider

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProtectedBrokerProviderQuiescenceMatchesPublishedFixture(
	t *testing.T,
) {
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
		fixture.Handshake.QuiescenceTransportSessionSHA256,
	)
	if err != nil {
		t.Fatal(err)
	}
	operation := mustProtectedBrokerProviderQuiescenceFixtureOperation(
		t,
		fixture,
	)
	operationJSON, err := (ProviderV1FrameCodec{}).EncodeOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	request, err := newProtectedBrokerProviderQuiescenceRequestV1(
		client,
		session,
		protectedBrokerProviderQuiescenceSequenceV1,
		operation,
		operationJSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(requestJSON); got !=
		fixture.ProviderQuiescenceRPC.Request.Wire.CanonicalJSON {
		t.Fatalf(
			"provider quiescence request differs from Rust fixture\n got: %s\nwant: %s",
			got,
			fixture.ProviderQuiescenceRPC.Request.Wire.CanonicalJSON,
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
		fixture.ProviderQuiescenceRPC.Request.Wire.FramedBytesB64,
	)
	requireProtectedBrokerDiscoveryHashVector(
		t,
		fixture.ProviderQuiescenceRPC.Request.OperationPayloadHash,
		protectedBrokerProviderQuiescenceOperationPayloadHashDomainV1,
		operationJSON,
	)
	requireProtectedBrokerDiscoveryJSONHashVector(
		t,
		fixture.ProviderQuiescenceRPC.Request.Hash,
		protectedBrokerProviderQuiescenceRequestHashDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.OperationSHA256,
		request.OperationPayloadSHA256,
	)
	requireProtectedBrokerDiscoveryJSONSignatureVector(
		t,
		fixture.ProviderQuiescenceRPC.Request.Signature,
		protectedBrokerProviderQuiescenceRequestSignatureDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.OperationSHA256,
		request.OperationPayloadSHA256,
		request.RequestSHA256,
	)

	responseFrame := decodeProtectedBrokerDiscoveryBase64(
		t,
		fixture.ProviderQuiescenceRPC.Response.Wire.FramedBytesB64,
	)
	var response protectedBrokerProviderQuiescenceResponseWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(
		bytes.NewReader(responseFrame),
		&response,
	); err != nil {
		t.Fatal(err)
	}
	quiescence, err := response.validate(
		client,
		session,
		protectedBrokerProviderQuiescenceSequenceV1,
		request.RequestSHA256,
		operation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if quiescence.CanonicalHash().String() !=
		fixture.ProviderQuiescenceRPC.Quiescence.CanonicalSHA256 {
		t.Fatalf("provider quiescence result = %+v", quiescence)
	}
	requireProtectedBrokerAdmissionJSONValueHashVector(
		t,
		fixture.ProviderQuiescenceRPC.Response.PayloadHash,
		protectedBrokerProviderQuiescenceResponsePayloadHashDomainV1,
		response.Payload,
	)
	requireProtectedBrokerDiscoveryJSONHashVector(
		t,
		fixture.ProviderQuiescenceRPC.Response.Hash,
		protectedBrokerProviderQuiescenceResponseHashDomainV1,
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
		fixture.ProviderQuiescenceRPC.Response.Signature,
		protectedBrokerProviderQuiescenceResponseSignatureDomainV1,
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

func mustProtectedBrokerProviderQuiescenceFixtureOperation(
	t *testing.T,
	fixture protectedBrokerDiscoveryFixtureV1,
) AgentOperation {
	t.Helper()
	record, err := decodeProviderV1Record(
		[]byte(fixture.ProviderQuiescenceRPC.Operation.CanonicalJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	operation, ok := record.value.(AgentOperation)
	if !ok || !operation.dispatchablePause() {
		t.Fatal("provider quiescence fixture has no Pause AgentOperation")
	}
	return operation
}

func TestProtectedBrokerProviderQuiescenceRejectsHostileRequests(
	t *testing.T,
) {
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
		fixture.Handshake.QuiescenceTransportSessionSHA256,
	)
	if err != nil {
		t.Fatal(err)
	}
	operation := mustProtectedBrokerProviderQuiescenceFixtureOperation(
		t,
		fixture,
	)
	operationJSON, err := (ProviderV1FrameCodec{}).EncodeOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	foreignOperation := mustProtectedBrokerPauseOperationForTest(
		t,
		operation.SessionID().String(),
		"foreign-pause-operation",
		operation.Sequence().Uint64(),
	)
	toolOperation, _ := mustProtectedBrokerProviderToolFixtureOperation(
		t,
		fixture,
	)
	toolJSON, err := (ProviderV1FrameCodec{}).EncodeOperation(toolOperation)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		invoke func() error
		want   ProtectedBrokerTransportErrorClassV1
	}{
		"reused RPC sequence": {
			invoke: func() error {
				_, err := newProtectedBrokerProviderQuiescenceRequestV1(
					client,
					session,
					2,
					operation,
					operationJSON,
				)
				return err
			},
			want: ProtectedBrokerReplayedSequenceV1,
		},
		"wrong operation kind": {
			invoke: func() error {
				_, err := newProtectedBrokerProviderQuiescenceRequestV1(
					client,
					session,
					protectedBrokerProviderQuiescenceSequenceV1,
					toolOperation,
					toolJSON,
				)
				return err
			},
			want: ProtectedBrokerInvalidRequestV1,
		},
		"operation payload substitution": {
			invoke: func() error {
				substituted := append(
					append([]byte{}, operationJSON...),
					' ',
				)
				_, err := newProtectedBrokerProviderQuiescenceRequestV1(
					client,
					session,
					protectedBrokerProviderQuiescenceSequenceV1,
					operation,
					substituted,
				)
				return err
			},
			want: ProtectedBrokerInvalidRequestV1,
		},
		"operation identity crosswire": {
			invoke: func() error {
				_, err := newProtectedBrokerProviderQuiescenceRequestV1(
					client,
					session,
					protectedBrokerProviderQuiescenceSequenceV1,
					foreignOperation,
					operationJSON,
				)
				return err
			},
			want: ProtectedBrokerInvalidRequestV1,
		},
		"client authority substitution": {
			invoke: func() error {
				substituted := session
				substituted.clientAuthoritySHA256 =
					protectedBrokerHashV1(fingerprint("0"))
				_, err := newProtectedBrokerProviderQuiescenceRequestV1(
					client,
					substituted,
					protectedBrokerProviderQuiescenceSequenceV1,
					operation,
					operationJSON,
				)
				return err
			},
			want: ProtectedBrokerClientAuthorityMismatchV1,
		},
		"transport session substitution": {
			invoke: func() error {
				substituted := session
				substituted.transportSessionSHA256 = ""
				_, err := newProtectedBrokerProviderQuiescenceRequestV1(
					client,
					substituted,
					protectedBrokerProviderQuiescenceSequenceV1,
					operation,
					operationJSON,
				)
				return err
			},
			want: ProtectedBrokerTransportSessionMismatchV1,
		},
		"backend identity substitution": {
			invoke: func() error {
				substituted := session
				substituted.backendIdentitySHA256 =
					protectedBrokerHashV1(fingerprint("1"))
				_, err := newProtectedBrokerProviderQuiescenceRequestV1(
					client,
					substituted,
					protectedBrokerProviderQuiescenceSequenceV1,
					operation,
					operationJSON,
				)
				return err
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"broker epoch substitution": {
			invoke: func() error {
				substituted := session
				substituted.brokerEpoch++
				_, err := newProtectedBrokerProviderQuiescenceRequestV1(
					client,
					substituted,
					protectedBrokerProviderQuiescenceSequenceV1,
					operation,
					operationJSON,
				)
				return err
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"profile substitution": {
			invoke: func() error {
				substituted := session
				substituted.profileSHA256 =
					protectedBrokerHashV1(fingerprint("2"))
				_, err := newProtectedBrokerProviderQuiescenceRequestV1(
					client,
					substituted,
					protectedBrokerProviderQuiescenceSequenceV1,
					operation,
					operationJSON,
				)
				return err
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			requireProtectedBrokerErrorClass(
				t,
				test.invoke(),
				test.want,
			)
		})
	}
}

func TestProtectedBrokerClientRunsToolThenPauseOnFreshConnections(
	t *testing.T,
) {
	ctx := context.Background()
	discoveryHarness := newProtectedBrokerDiscoveryHarness(t)
	admissionHarness := newProtectedBrokerAdmissionHarness(t)
	toolHarness := newProtectedBrokerToolHarness(t)
	quiescenceHarness := newProtectedBrokerQuiescenceHarness(t)
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
	quiescenceHarness.quiescenceForOperation = func(
		operation AgentOperation,
	) Quiescence {
		return newProtectedBrokerQuiescenceForTest(
			t,
			operation.SessionID().String(),
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
			case 3:
				return quiescenceHarness.DialContext(ctx)
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
		t.Fatal(err)
	}
	toolInput, err := NewOperationInput(OperationInputValues{
		OperationID:      "protected-tool-sequence-1",
		Kind:             OperationTool,
		Nonce:            "protected-tool-sequence-1-nonce",
		PayloadHash:      fingerprint("a"),
		NormalizedRecord: []byte(`{"command":"sequence-one"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	toolResult, err := session.RunTool(ctx, toolInput)
	if err != nil {
		t.Fatal(err)
	}
	if toolResult.Sequence().Uint64() != 1 ||
		toolResult.OperationID() != toolInput.OperationID() {
		t.Fatalf("protected Tool result = %+v", toolResult)
	}
	pauseInput, err := NewOperationInput(OperationInputValues{
		OperationID: "protected-pause-sequence-2",
		Kind:        OperationPause,
		Nonce:       "protected-pause-sequence-2-nonce",
		PayloadHash: fingerprint("b"),
	})
	if err != nil {
		t.Fatal(err)
	}
	quiescence, err := session.Quiesce(ctx, pauseInput)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, ok := session.ID()
	if !ok || quiescence.SessionID() != sessionID {
		t.Fatalf("protected quiescence = %+v", quiescence)
	}

	discoveryObservation := discoveryHarness.nextObservation(t)
	admissionObservation := admissionHarness.nextObservation(t)
	toolObservation := toolHarness.nextObservation(t)
	quiescenceObservation := quiescenceHarness.nextObservation(t)
	if discoveryObservation.err != nil ||
		admissionObservation.err != nil ||
		toolObservation.err != nil ||
		quiescenceObservation.err != nil {
		t.Fatalf(
			"protected lifecycle errors = %v, %v, %v, %v",
			discoveryObservation.err,
			admissionObservation.err,
			toolObservation.err,
			quiescenceObservation.err,
		)
	}
	if toolObservation.operation.Sequence().Uint64() != 1 ||
		quiescenceObservation.operation.Sequence().Uint64() != 2 {
		t.Fatalf(
			"provider operation sequences = %d, %d",
			toolObservation.operation.Sequence().Uint64(),
			quiescenceObservation.operation.Sequence().Uint64(),
		)
	}
	if toolObservation.request.Sequence != protectedBrokerProviderToolSequenceV1 ||
		quiescenceObservation.request.Sequence !=
			protectedBrokerProviderQuiescenceSequenceV1 {
		t.Fatalf(
			"connection-local RPC sequences = %d, %d",
			toolObservation.request.Sequence,
			quiescenceObservation.request.Sequence,
		)
	}
	if toolObservation.request.ClientAuthoritySHA256 !=
		quiescenceObservation.request.ClientAuthoritySHA256 {
		t.Fatal("Tool and Pause changed reconnect-stable client authority")
	}
	if toolObservation.request.TransportSessionSHA256 ==
		quiescenceObservation.request.TransportSessionSHA256 {
		t.Fatal("Tool and Pause reused a transport session")
	}
	dialMu.Lock()
	gotDials := dialCount
	dialMu.Unlock()
	if gotDials != 4 {
		t.Fatalf("protected lifecycle dial count = %d, want 4", gotDials)
	}
	discoveryHarness.requireClosedOnce(t)
	admissionHarness.requireClosedOnce(t)
	toolHarness.requireClosedOnce(t)
	quiescenceHarness.requireClosedOnce(t)
}

func TestProtectedBrokerEndpointQuiescenceCurrentThenHistoricalReplay(
	t *testing.T,
) {
	harness := newProtectedBrokerQuiescenceHarness(t)
	endpoint := mustProtectedBrokerQuiescenceEndpoint(t, harness)
	operation := mustProtectedBrokerPauseOperationForTest(
		t,
		"lease:provider-quiescence-replay",
		"pause-operation-replay",
		2,
	)
	harness.quiescence = newProtectedBrokerQuiescenceForTest(
		t,
		operation.SessionID().String(),
	)

	first, err := endpoint.Operate(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	harness.setHistorical(true)
	second, err := endpoint.Operate(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	firstQuiescence, firstOK := first.(Quiescence)
	secondQuiescence, secondOK := second.(Quiescence)
	if !firstOK ||
		!secondOK ||
		firstQuiescence != harness.quiescence ||
		secondQuiescence != harness.quiescence {
		t.Fatalf("provider quiescence results = %+v, %+v", first, second)
	}

	firstObservation := harness.nextObservation(t)
	secondObservation := harness.nextObservation(t)
	if firstObservation.err != nil || secondObservation.err != nil {
		t.Fatalf(
			"quiescence replay errors = %v, %v",
			firstObservation.err,
			secondObservation.err,
		)
	}
	if firstObservation.historical || !secondObservation.historical {
		t.Fatalf(
			"quiescence outcomes historical = %t, %t",
			firstObservation.historical,
			secondObservation.historical,
		)
	}
	for index, observation := range []protectedBrokerQuiescenceObservation{
		firstObservation,
		secondObservation,
	} {
		if observation.request.Sequence !=
			protectedBrokerProviderQuiescenceSequenceV1 {
			t.Fatalf(
				"exchange %d RPC sequence = %d",
				index,
				observation.request.Sequence,
			)
		}
		if observation.operation.Sequence().Uint64() != 2 ||
			observation.operation.CanonicalHash() !=
				operation.CanonicalHash() {
			t.Fatalf("exchange %d changed Pause operation", index)
		}
	}
	if firstObservation.request.ClientAuthoritySHA256 !=
		secondObservation.request.ClientAuthoritySHA256 {
		t.Fatal("fresh Pause connection changed stable client authority")
	}
	if firstObservation.request.TransportSessionSHA256 ==
		secondObservation.request.TransportSessionSHA256 {
		t.Fatal("Pause replay reused a transport session")
	}
	harness.requireClosedOnce(t)
}

func TestProtectedBrokerQuiescenceCanonicalEvidence(t *testing.T) {
	sessionID := "lease:provider-quiescence-canonical"
	quiescence := newProtectedBrokerQuiescenceForTest(t, sessionID)
	encoded, err := encodeProtectedBrokerQuiescenceForTest(quiescence)
	if err != nil {
		t.Fatal(err)
	}
	expected := fmt.Sprintf(
		`{"schema":"%s","session_id":"%s","quiescence_hash":"%s","resource_evidence_hash":"%s","canonical_hash":"%s"}`,
		providerV1SchemaQuiescence,
		sessionID,
		quiescence.QuiescenceHash().String(),
		quiescence.ResourceEvidenceHash().String(),
		quiescence.CanonicalHash().String(),
	)
	if string(encoded) != expected {
		t.Fatalf(
			"canonical provider quiescence\n got: %s\nwant: %s",
			encoded,
			expected,
		)
	}
	response, err := (ProviderV1FrameCodec{}).DecodeOperation(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decoded, ok := response.(Quiescence)
	if !ok || decoded != quiescence {
		t.Fatalf("decoded quiescence = %+v", response)
	}

	harness := newProtectedBrokerQuiescenceHarness(t)
	harness.quiescence = quiescence
	endpoint := mustProtectedBrokerQuiescenceEndpoint(t, harness)
	operation := mustProtectedBrokerPauseOperationForTest(
		t,
		sessionID,
		"pause-operation-canonical",
		2,
	)
	operationJSON, err := (ProviderV1FrameCodec{}).EncodeOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	result, err := endpoint.Operate(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if result != quiescence {
		t.Fatalf("protected canonical quiescence = %+v", result)
	}
	observation := harness.nextObservation(t)
	if observation.err != nil {
		t.Fatal(observation.err)
	}
	decodedOperationJSON, err := decodeProtectedBrokerProviderRecordV1(
		observation.request.OperationJSONBase64URL,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decodedOperationJSON, operationJSON) {
		t.Fatal("protected Pause request changed canonical operation JSON")
	}
	harness.requireClosedOnce(t)
}

func TestProtectedBrokerEndpointQuiescenceMapsBrokerFailuresStably(
	t *testing.T,
) {
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
			harness := newProtectedBrokerQuiescenceHarness(t)
			harness.brokerFailure = &class
			endpoint := mustProtectedBrokerQuiescenceEndpoint(t, harness)
			operation := mustProtectedBrokerPauseOperationForTest(
				t,
				"lease:provider-quiescence-failure",
				"pause-operation-failure",
				2,
			)
			response, err := endpoint.Operate(
				context.Background(),
				operation,
			)
			if response != nil {
				t.Fatal("broker failure returned quiescence evidence")
			}
			var failure *ProviderFailure
			if !errors.As(err, &failure) ||
				failure.Code() != want ||
				failure.RetryFrom() != TransitionActivate ||
				failure.ProviderID().String() !=
					harness.identity.identitySHA256.String() ||
				failure.ProviderEpoch().Uint64() !=
					uint64(harness.identity.brokerEpoch) {
				t.Fatalf(
					"provider quiescence failure = %#v, %v",
					failure,
					err,
				)
			}
			if strings.Contains(err.Error(), string(class)) {
				t.Fatalf("provider failure leaked broker class: %v", err)
			}
			if observation := harness.nextObservation(t); observation.err != nil {
				t.Fatal(observation.err)
			}
			harness.requireClosedOnce(t)
		})
	}
}

func TestProtectedBrokerEndpointQuiescenceRejectsHostileResponses(
	t *testing.T,
) {
	operation := mustProtectedBrokerPauseOperationForTest(
		t,
		"lease:provider-quiescence-hostile",
		"pause-operation-hostile",
		2,
	)
	foreignRecord, err := encodeProviderV1DTO(
		providerV1KindOperationResult,
		providerV1OperationResultDTO{
			Schema:            providerV1SchemaOperationResult,
			SessionID:         operation.SessionID().String(),
			OperationID:       operation.OperationID().String(),
			OperationSequence: operation.Sequence().Uint64(),
			ResultKind:        string(ResultCompleted),
			ArtifactHash:      fingerprint("a"),
			EvidenceHash:      fingerprint("b"),
			CanonicalHash: newProtectedBrokerToolResultForTest(
				t,
				operation.SessionID().String(),
				operation.OperationID().String(),
				operation.Sequence().Uint64(),
				ResultCompleted,
			).CanonicalHash().String(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]struct {
		configure func(*protectedBrokerQuiescenceHarness)
		want      ProtectedBrokerTransportErrorClassV1
	}{
		"schema substitution": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderQuiescenceResponseWireV1,
				) {
					response.Schema = "execution.protected-broker.foreign.v1"
				}
			},
			want: ProtectedBrokerInvalidSchemaV1,
		},
		"client authority substitution": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderQuiescenceResponseWireV1,
				) {
					response.ClientAuthoritySHA256 =
						protectedBrokerHashV1(fingerprint("0"))
				}
			},
			want: ProtectedBrokerClientAuthorityMismatchV1,
		},
		"transport session substitution": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderQuiescenceResponseWireV1,
				) {
					response.TransportSessionSHA256 =
						protectedBrokerHashV1(fingerprint("1"))
				}
			},
			want: ProtectedBrokerTransportSessionMismatchV1,
		},
		"backend identity substitution": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderQuiescenceResponseWireV1,
				) {
					response.BackendIdentitySHA256 =
						protectedBrokerHashV1(fingerprint("2"))
				}
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"broker epoch substitution": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderQuiescenceResponseWireV1,
				) {
					response.BrokerEpoch++
				}
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"profile substitution": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderQuiescenceResponseWireV1,
				) {
					response.ProfileSHA256 =
						protectedBrokerHashV1(fingerprint("3"))
				}
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"sequence gap": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderQuiescenceResponseWireV1,
				) {
					response.Sequence = 2
				}
			},
			want: ProtectedBrokerSequenceGapV1,
		},
		"request substitution": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderQuiescenceResponseWireV1,
				) {
					response.RequestSHA256 =
						protectedBrokerHashV1(fingerprint("4"))
				}
			},
			want: ProtectedBrokerRequestHashMismatchV1,
		},
		"response payload hash substitution": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderQuiescenceResponseWireV1,
				) {
					response.ResponsePayloadSHA256 =
						protectedBrokerHashV1(fingerprint("5"))
				}
			},
			want: ProtectedBrokerResponseHashMismatchV1,
		},
		"response hash substitution": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseMutation = func(
					response *protectedBrokerProviderQuiescenceResponseWireV1,
				) {
					response.ResponseSHA256 =
						protectedBrokerHashV1(fingerprint("6"))
				}
			},
			want: ProtectedBrokerResponseHashMismatchV1,
		},
		"wrong response signer": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.responseSigner = ed25519.NewKeyFromSeed(
					bytes.Repeat([]byte{0xa6}, ed25519.SeedSize),
				)
			},
			want: ProtectedBrokerInvalidSignatureV1,
		},
		"crosswired provider session": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.quiescence = newProtectedBrokerQuiescenceForTest(
					t,
					"lease:provider-quiescence-foreign",
				)
			},
			want: ProtectedBrokerInvalidEvidenceV1,
		},
		"quiescence hash substitution": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				hash := protectedBrokerHashV1(fingerprint("7"))
				h.quiescenceHashOverride = &hash
			},
			want: ProtectedBrokerResponseHashMismatchV1,
		},
		"unknown provider evidence field": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.quiescenceRecordMutation = func(encoded []byte) []byte {
					mutated := append([]byte{}, encoded[:len(encoded)-1]...)
					return append(mutated, []byte(`,"unexpected":true}`)...)
				}
			},
			want: ProtectedBrokerInvalidEvidenceV1,
		},
		"invalid provider evidence base64": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				value := "not+base64"
				h.quiescenceBase64Override = &value
			},
			want: ProtectedBrokerInvalidPayloadV1,
		},
		"foreign provider record kind": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.quiescenceRecordMutation = func([]byte) []byte {
					return append([]byte{}, foreignRecord...)
				}
			},
			want: ProtectedBrokerInvalidEvidenceV1,
		},
		"unknown response envelope field": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.rawMutation = func(encoded []byte) []byte {
					mutated := append([]byte{}, encoded[:len(encoded)-1]...)
					return append(mutated, []byte(`,"unexpected":true}`)...)
				}
			},
			want: ProtectedBrokerInvalidFrameV1,
		},
		"unknown response payload field": {
			configure: func(h *protectedBrokerQuiescenceHarness) {
				h.rawMutation = func(encoded []byte) []byte {
					return bytes.Replace(
						encoded,
						[]byte(`"payload":{"outcome":`),
						[]byte(`"payload":{"unexpected":true,"outcome":`),
						1,
					)
				}
			},
			want: ProtectedBrokerInvalidFrameV1,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			harness := newProtectedBrokerQuiescenceHarness(t)
			harness.quiescence = newProtectedBrokerQuiescenceForTest(
				t,
				operation.SessionID().String(),
			)
			test.configure(harness)
			endpoint := mustProtectedBrokerQuiescenceEndpoint(t, harness)
			response, err := endpoint.Operate(
				context.Background(),
				operation,
			)
			if response != nil {
				t.Fatal("hostile response returned quiescence evidence")
			}
			requireProtectedBrokerErrorClass(t, err, test.want)
			if observation := harness.nextObservation(t); observation.err != nil {
				t.Fatal(observation.err)
			}
			harness.requireClosedOnce(t)
		})
	}
}

func TestProtectedBrokerQuiescenceRejectsWrongKindsBeforeDial(t *testing.T) {
	harness := newProtectedBrokerQuiescenceHarness(t)
	var dialMu sync.Mutex
	dials := 0
	dialer := ProtectedBrokerDialFuncV1(
		func(context.Context) (ProtectedBrokerStreamV1, error) {
			dialMu.Lock()
			dials++
			dialMu.Unlock()
			return nil, errors.New("must not dial")
		},
	)
	transport, err := NewProtectedBrokerQuiescenceTransportV1(
		ProtectedBrokerQuiescenceTransportConfigV1{
			Dialer: dialer,
			Client: harness.client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	toolOperation, _ := mustProtectedBrokerProviderToolFixtureOperation(
		t,
		loadProtectedBrokerDiscoveryFixture(t),
	)
	if result, err := transport.Operate(
		context.Background(),
		toolOperation,
	); result.valid() {
		t.Fatal("quiescence transport returned evidence for Tool")
	} else {
		requireProtectedBrokerErrorClass(
			t,
			err,
			ProtectedBrokerInvalidRequestV1,
		)
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
	for _, kind := range []OperationKind{
		OperationAgentStart,
		OperationWorkspace,
		OperationCancel,
		OperationFreeze,
	} {
		operation := mustProtectedBrokerOperationForKindForTest(
			t,
			"lease:provider-quiescence-wrong-kind",
			"wrong-kind-"+string(kind),
			2,
			kind,
		)
		response, err := endpoint.Operate(context.Background(), operation)
		if response != nil {
			t.Fatalf("%s returned a lifecycle response", kind)
		}
		requireProtectedBrokerErrorClass(
			t,
			err,
			ProtectedBrokerInvalidRequestV1,
		)
	}
	response, err := endpoint.Operate(context.Background(), AgentOperation{})
	if response != nil {
		t.Fatal("zero operation returned a lifecycle response")
	}
	requireProtectedBrokerErrorClass(
		t,
		err,
		ProtectedBrokerInvalidRequestV1,
	)
	dialMu.Lock()
	gotDials := dials
	dialMu.Unlock()
	if gotDials != 0 {
		t.Fatalf("wrong lifecycle kinds dialed %d times", gotDials)
	}
}

func TestProtectedBrokerEndpointQuiescenceCloseFailureWithholdsSuccess(
	t *testing.T,
) {
	harness := newProtectedBrokerQuiescenceHarness(t)
	const rawCloseError = "raw-quiescence-close-peer-material"
	harness.clientCloseErr = errors.New(rawCloseError)
	operation := mustProtectedBrokerPauseOperationForTest(
		t,
		"lease:provider-quiescence-close",
		"pause-operation-close",
		2,
	)
	harness.quiescence = newProtectedBrokerQuiescenceForTest(
		t,
		operation.SessionID().String(),
	)
	endpoint := mustProtectedBrokerQuiescenceEndpoint(t, harness)
	response, err := endpoint.Operate(context.Background(), operation)
	if response != nil {
		t.Fatal("close failure returned quiescence evidence")
	}
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerIOV1)
	if strings.Contains(err.Error(), rawCloseError) {
		t.Fatalf("close failure leaked peer material: %v", err)
	}
	if observation := harness.nextObservation(t); observation.err != nil {
		t.Fatal(observation.err)
	}
	harness.requireClosedOnce(t)
}

func mustProtectedBrokerQuiescenceEndpoint(
	t *testing.T,
	harness *protectedBrokerQuiescenceHarness,
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

func mustProtectedBrokerPauseOperationForTest(
	t *testing.T,
	sessionID string,
	operationID string,
	sequence uint64,
) AgentOperation {
	t.Helper()
	return mustProtectedBrokerOperationForKindForTest(
		t,
		sessionID,
		operationID,
		sequence,
		OperationPause,
	)
}

func mustProtectedBrokerOperationForKindForTest(
	t *testing.T,
	sessionID string,
	operationID string,
	sequence uint64,
	kind OperationKind,
) AgentOperation {
	t.Helper()
	input := OperationInputValues{
		OperationID: operationID,
		Kind:        kind,
		Nonce:       operationID + "-nonce",
		PayloadHash: fingerprint("9"),
	}
	if kind == OperationTool {
		input.NormalizedRecord = []byte(`{"command":"protected-kind-test"}`)
	}
	operationInput, err := NewOperationInput(input)
	if err != nil {
		t.Fatal(err)
	}
	boundSessionID, err := NewIdentifier(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	boundSequence, err := NewOperationSequence(sequence)
	if err != nil {
		t.Fatal(err)
	}
	planHash, err := ParseFingerprint(fingerprint("8"))
	if err != nil {
		t.Fatal(err)
	}
	operation, err := newAgentOperation(
		boundSessionID,
		boundSequence,
		planHash,
		operationInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func newProtectedBrokerQuiescenceForTest(
	t *testing.T,
	sessionID string,
) Quiescence {
	t.Helper()
	dto := providerV1QuiescenceDTO{
		Schema:               providerV1SchemaQuiescence,
		SessionID:            sessionID,
		QuiescenceHash:       fingerprint("d"),
		ResourceEvidenceHash: fingerprint("e"),
	}
	preimage, err := dto.canonicalPreimage()
	if err != nil {
		t.Fatal(err)
	}
	dto.CanonicalHash = providerV1CanonicalHash(preimage)
	quiescence, err := NewQuiescence(QuiescenceInput{
		SessionID:            dto.SessionID,
		QuiescenceHash:       dto.QuiescenceHash,
		ResourceEvidenceHash: dto.ResourceEvidenceHash,
		CanonicalHash:        dto.CanonicalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	return quiescence
}

func encodeProtectedBrokerQuiescenceForTest(
	quiescence Quiescence,
) ([]byte, error) {
	return encodeProviderV1DTO(
		providerV1KindQuiescence,
		providerV1QuiescenceDTO{
			Schema:               providerV1SchemaQuiescence,
			SessionID:            quiescence.SessionID().String(),
			QuiescenceHash:       quiescence.QuiescenceHash().String(),
			ResourceEvidenceHash: quiescence.ResourceEvidenceHash().String(),
			CanonicalHash:        quiescence.CanonicalHash().String(),
		},
	)
}

type protectedBrokerQuiescenceObservation struct {
	request    protectedBrokerProviderQuiescenceRequestWireV1
	operation  AgentOperation
	historical bool
	err        error
}

type protectedBrokerQuiescenceHarness struct {
	client                 *ProtectedBrokerClientV1
	brokerKey              ed25519.PrivateKey
	clientPublicKey        ed25519.PublicKey
	identity               ProtectedBrokerBackendIdentityV1
	quiescence             Quiescence
	quiescenceForOperation func(AgentOperation) Quiescence

	mu                       sync.Mutex
	nextServerNonce          byte
	streams                  []*protectedBrokerTrackingStream
	historical               bool
	brokerFailure            *protectedBrokerFailureClassV1
	quiescenceHashOverride   *protectedBrokerHashV1
	quiescenceBase64Override *string
	quiescenceRecordMutation func([]byte) []byte
	responseMutation         func(*protectedBrokerProviderQuiescenceResponseWireV1)
	rawMutation              func([]byte) []byte
	responseSigner           ed25519.PrivateKey
	clientCloseErr           error
	observations             chan protectedBrokerQuiescenceObservation
}

func newProtectedBrokerQuiescenceHarness(
	t *testing.T,
) *protectedBrokerQuiescenceHarness {
	t.Helper()
	interop := loadProtectedBrokerInteropFixture(t)
	brokerKey, clientKey, identity, client, _ :=
		protectedBrokerFixtureClient(t, interop)
	return &protectedBrokerQuiescenceHarness{
		client:          client,
		brokerKey:       brokerKey,
		clientPublicKey: clientKey.Public().(ed25519.PublicKey),
		identity:        identity,
		quiescence: newProtectedBrokerQuiescenceForTest(
			t,
			"lease:provider-quiescence-default",
		),
		nextServerNonce: 0xa0,
		observations:    make(chan protectedBrokerQuiescenceObservation, 8),
	}
}

func (h *protectedBrokerQuiescenceHarness) setHistorical(value bool) {
	h.mu.Lock()
	h.historical = value
	h.mu.Unlock()
}

func (h *protectedBrokerQuiescenceHarness) DialContext(
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

func (h *protectedBrokerQuiescenceHarness) serve(
	stream net.Conn,
	serverNonce byte,
) {
	observation := protectedBrokerQuiescenceObservation{}
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

	var request protectedBrokerProviderQuiescenceRequestWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(stream, &request); err != nil {
		observation.err = err
		return
	}
	observation.request = request
	operation, err := validateProtectedBrokerProviderQuiescenceRequestForTest(
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

	h.mu.Lock()
	historical := h.historical
	h.mu.Unlock()
	observation.historical = historical
	signer := h.responseSigner
	if signer == nil {
		signer = h.brokerKey
	}
	quiescence := h.quiescence
	if h.quiescenceForOperation != nil {
		quiescence = h.quiescenceForOperation(operation)
	}
	response, err := newProtectedBrokerProviderQuiescenceResponseForTest(
		request,
		quiescence,
		historical,
		h.brokerFailure,
		h.quiescenceHashOverride,
		h.quiescenceBase64Override,
		h.quiescenceRecordMutation,
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

func (h *protectedBrokerQuiescenceHarness) nextObservation(
	t *testing.T,
) protectedBrokerQuiescenceObservation {
	t.Helper()
	select {
	case observation := <-h.observations:
		return observation
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for provider quiescence observation")
		return protectedBrokerQuiescenceObservation{}
	}
}

func (h *protectedBrokerQuiescenceHarness) requireClosedOnce(t *testing.T) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for index, stream := range h.streams {
		if stream.closeCount != 1 {
			t.Fatalf(
				"provider quiescence stream %d close count = %d, want 1",
				index,
				stream.closeCount,
			)
		}
	}
}

func validateProtectedBrokerProviderQuiescenceRequestForTest(
	request protectedBrokerProviderQuiescenceRequestWireV1,
	clientAuthoritySHA256 protectedBrokerHashV1,
	transportSessionSHA256 protectedBrokerHashV1,
	identity ProtectedBrokerBackendIdentityV1,
	clientKey ed25519.PublicKey,
) (AgentOperation, error) {
	if request.Schema != protectedBrokerProviderQuiescenceRequestSchemaV1 ||
		request.Sequence != protectedBrokerProviderQuiescenceSequenceV1 ||
		request.ClientAuthoritySHA256 != clientAuthoritySHA256 ||
		request.TransportSessionSHA256 != transportSessionSHA256 ||
		request.BackendIdentitySHA256 != identity.identitySHA256 ||
		request.BrokerEpoch != identity.brokerEpoch ||
		request.ProfileSHA256 != identity.profileSHA256 {
		return AgentOperation{},
			errors.New("invalid provider quiescence request binding")
	}
	operationJSON, err := decodeProtectedBrokerProviderRecordV1(
		request.OperationJSONBase64URL,
	)
	if err != nil {
		return AgentOperation{}, err
	}
	if request.OperationPayloadSHA256 != hashProtectedBrokerBytesV1(
		protectedBrokerProviderQuiescenceOperationPayloadHashDomainV1,
		operationJSON,
	) {
		return AgentOperation{},
			errors.New("invalid provider quiescence operation payload hash")
	}
	expectedRequestSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderQuiescenceRequestHashDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.OperationSHA256,
		request.OperationPayloadSHA256,
	)
	if err != nil {
		return AgentOperation{}, err
	}
	if request.RequestSHA256 != expectedRequestSHA256 {
		return AgentOperation{},
			errors.New("invalid provider quiescence request hash")
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerProviderQuiescenceRequestSignatureDomainV1,
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
		request.RequestSHA256,
	); err != nil {
		return AgentOperation{}, err
	}
	record, err := decodeProviderV1Record(operationJSON)
	if err != nil {
		return AgentOperation{}, err
	}
	operation, ok := record.value.(AgentOperation)
	if !ok ||
		!operation.dispatchablePause() ||
		request.OperationSHA256.String() !=
			operation.CanonicalHash().String() {
		return AgentOperation{},
			errors.New("invalid provider quiescence operation")
	}
	return operation, nil
}

func newProtectedBrokerProviderQuiescenceResponseForTest(
	request protectedBrokerProviderQuiescenceRequestWireV1,
	quiescence Quiescence,
	historical bool,
	failure *protectedBrokerFailureClassV1,
	quiescenceHashOverride *protectedBrokerHashV1,
	quiescenceBase64Override *string,
	recordMutation func([]byte) []byte,
	brokerKey ed25519.PrivateKey,
) (protectedBrokerProviderQuiescenceResponseWireV1, error) {
	var payload protectedBrokerProviderQuiescenceResponsePayloadV1
	if failure != nil {
		class := *failure
		payload.failure = &class
	} else {
		quiescenceJSON, err := encodeProtectedBrokerQuiescenceForTest(
			quiescence,
		)
		if err != nil {
			return protectedBrokerProviderQuiescenceResponseWireV1{}, err
		}
		if recordMutation != nil {
			quiescenceJSON = recordMutation(quiescenceJSON)
		}
		quiescenceSHA256, err := parseProtectedBrokerHashV1(
			quiescence.CanonicalHash().String(),
		)
		if err != nil {
			return protectedBrokerProviderQuiescenceResponseWireV1{}, err
		}
		if quiescenceHashOverride != nil {
			quiescenceSHA256 = *quiescenceHashOverride
		}
		quiescenceBase64URL := base64.RawURLEncoding.EncodeToString(
			quiescenceJSON,
		)
		if quiescenceBase64Override != nil {
			quiescenceBase64URL = *quiescenceBase64Override
		}
		payload = protectedBrokerProviderQuiescenceResponsePayloadV1{
			historical: historical,
			success: &protectedBrokerProviderQuiescenceSuccessWireV1{
				QuiescenceSHA256:        quiescenceSHA256,
				QuiescenceJSONBase64URL: quiescenceBase64URL,
			},
		}
	}
	responsePayloadSHA256, err := hashProtectedBrokerJSONValueV1(
		protectedBrokerProviderQuiescenceResponsePayloadHashDomainV1,
		payload,
	)
	if err != nil {
		return protectedBrokerProviderQuiescenceResponseWireV1{}, err
	}
	responseSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderQuiescenceResponseHashDomainV1,
		protectedBrokerProviderQuiescenceResponseSchemaV1,
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
		return protectedBrokerProviderQuiescenceResponseWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerProviderQuiescenceResponseSignatureDomainV1,
		brokerKey,
		protectedBrokerProviderQuiescenceResponseSchemaV1,
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
		return protectedBrokerProviderQuiescenceResponseWireV1{}, err
	}
	return protectedBrokerProviderQuiescenceResponseWireV1{
		Schema:                 protectedBrokerProviderQuiescenceResponseSchemaV1,
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
