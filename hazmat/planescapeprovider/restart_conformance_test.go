package planescapeprovider

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"
)

type frameReply struct {
	response []byte
	err      error
}

type lifecycleFrameTransport struct {
	replies  []frameReply
	requests [][]byte
}

func (t *lifecycleFrameTransport) RoundTrip(_ context.Context, request []byte) ([]byte, error) {
	t.requests = append(t.requests, append([]byte(nil), request...))
	if len(t.replies) == 0 {
		return nil, errors.New("unexpected frame")
	}
	reply := t.replies[0]
	t.replies = t.replies[1:]
	return append([]byte(nil), reply.response...), reply.err
}

type lifecycleFrameCodec struct {
	capabilities      ProviderCapabilities
	admission         SessionAdmission
	operationReplies  []OperationResponse
	operationRequests []AgentOperation
}

func (*lifecycleFrameCodec) EncodeDiscovery() ([]byte, error) {
	return []byte(`{"kind":"discover"}`), nil
}

func (c *lifecycleFrameCodec) DecodeCapabilities([]byte) (ProviderCapabilities, error) {
	return c.capabilities, nil
}

func (*lifecycleFrameCodec) EncodeCompiledContainmentPlan(CompiledContainmentPlan) ([]byte, error) {
	return []byte(`{"kind":"admit"}`), nil
}

func (c *lifecycleFrameCodec) DecodeAdmission([]byte) (SessionAdmission, error) {
	return c.admission, nil
}

func (c *lifecycleFrameCodec) EncodeOperation(request AgentOperation) ([]byte, error) {
	c.operationRequests = append(c.operationRequests, request)
	return []byte(`{"kind":"operate"}`), nil
}

func (c *lifecycleFrameCodec) DecodeOperation([]byte) (OperationResponse, error) {
	if len(c.operationReplies) == 0 {
		return nil, errors.New("unexpected operation response")
	}
	reply := c.operationReplies[0]
	c.operationReplies = c.operationReplies[1:]
	return reply, nil
}

func (*lifecycleFrameCodec) EncodeFreeze(Freeze) ([]byte, error) {
	return nil, errors.New("unexpected freeze")
}

func (*lifecycleFrameCodec) DecodeFreezeAck([]byte) (FreezeAck, error) {
	return FreezeAck{}, errors.New("unexpected freeze acknowledgement")
}

func (*lifecycleFrameCodec) EncodeCancellation(Cancellation) ([]byte, error) {
	return nil, errors.New("unexpected cancellation")
}

func (*lifecycleFrameCodec) DecodeCancellationAck([]byte) (CancellationAck, error) {
	return CancellationAck{}, errors.New("unexpected cancellation acknowledgement")
}

type operationBinding struct {
	sessionID     string
	operationID   string
	sequence      uint64
	kind          OperationKind
	planHash      string
	nonce         string
	payloadHash   string
	canonicalHash string
}

func bindingOf(operation AgentOperation) operationBinding {
	return operationBinding{
		sessionID:     operation.SessionID().String(),
		operationID:   operation.OperationID().String(),
		sequence:      operation.Sequence().Uint64(),
		kind:          operation.Kind(),
		planHash:      operation.PlanHash().String(),
		nonce:         operation.Nonce().String(),
		payloadHash:   operation.PayloadHash().String(),
		canonicalHash: operation.CanonicalHash().String(),
	}
}

func newFramedTestClient(t *testing.T, codec FrameCodec, transport FrameTransport, store CheckpointStore) *Client {
	t.Helper()
	endpoint, err := NewFramedEndpoint(FramedEndpointConfig{
		Transport:      transport,
		Codec:          codec,
		BackendBinding: testBackendIdentityBinding(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(ClientConfig{
		Endpoint: endpoint,
		Store:    store,
		Now:      func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestFramedClientReconnectReplaysAmbiguousOperationExactly(t *testing.T) {
	ctx := context.Background()
	plan := mustCompiledPlan(t)
	capabilities := mustReleasedCapabilities(t)
	store, err := NewFileStore(filepath.Join(t.TempDir(), "provider-checkpoints"))
	if err != nil {
		t.Fatal(err)
	}

	firstCodec := &lifecycleFrameCodec{
		capabilities: capabilities,
		admission:    mustAdmission(t, plan),
		operationReplies: []OperationResponse{
			mustResult(t, "launch-1", 1, ResultAccepted, "7", "8", "9"),
		},
	}
	firstTransport := &lifecycleFrameTransport{replies: []frameReply{
		{response: []byte(`{}`)},
		{response: []byte(`{}`)},
		{response: []byte(`{}`)},
		{err: io.ErrUnexpectedEOF},
	}}
	firstClient := newFramedTestClient(t, firstCodec, firstTransport, store)
	discovery, err := firstClient.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session := mustSession(t, firstClient, discovery, plan)
	if _, err := session.Launch(ctx, mustOperation(t, "launch-1", OperationAgentStart, "nonce-launch", "7", "8")); err != nil {
		t.Fatal(err)
	}
	tool := mustOperation(t, "tool-1", OperationTool, "nonce-tool", "a", "b")
	if _, err := session.RunTool(ctx, tool); err == nil {
		t.Fatal("disconnected tool operation succeeded")
	} else {
		requireClass(t, err, ErrorUnavailable)
	}
	if got := len(firstCodec.operationRequests); got != 2 {
		t.Fatalf("first process operation requests = %d, want 2", got)
	}
	if got := len(firstTransport.requests); got != 4 {
		t.Fatalf("first process frames = %d, want 4", got)
	}

	staleCapabilities, err := NewProviderCapabilities(ProviderCapabilitiesInput{
		ProviderID:         "provider-1",
		ProviderEpoch:      8,
		Profile:            ProfilePortable,
		CapabilityHash:     fingerprint("a"),
		CanonicalHash:      fingerprint("b"),
		Capabilities:       []Capability{CapabilityToolExecute, CapabilityWorkspaceRead},
		ResourceDimensions: []ResourceDimension{ResourceCPUTime, ResourceMemoryBytes, ResourceWorkspaceBytes},
	})
	if err != nil {
		t.Fatal(err)
	}
	staleCodec := &lifecycleFrameCodec{capabilities: staleCapabilities}
	staleTransport := &lifecycleFrameTransport{replies: []frameReply{{response: []byte(`{}`)}}}
	staleClient := newFramedTestClient(t, staleCodec, staleTransport, store)
	_, err = staleClient.Discover(ctx)
	requireClass(t, err, ErrorConflict)
	if len(staleCodec.operationRequests) != 0 || len(staleTransport.requests) != 1 {
		t.Fatal("stale reconnect reached an effect-bearing provider path")
	}

	restartedCodec := &lifecycleFrameCodec{
		capabilities: capabilities,
		operationReplies: []OperationResponse{
			mustResult(t, "tool-1", 2, ResultCompleted, "a", "b", "c"),
			mustResult(t, "tool-1", 2, ResultCompleted, "a", "b", "c"),
		},
	}
	restartedTransport := &lifecycleFrameTransport{replies: []frameReply{
		{response: []byte(`{}`)},
		{response: []byte(`{}`)},
		{response: []byte(`{}`)},
	}}
	restartedClient := newFramedTestClient(t, restartedCodec, restartedTransport, store)
	restartedDiscovery, err := restartedClient.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restartedClient.Reconnect(ctx, restartedDiscovery, "session-other"); err == nil {
		t.Fatal("cross-session handle reconnected")
	} else {
		requireClass(t, err, ErrorConflict)
	}
	reconnected, err := restartedClient.Reconnect(ctx, restartedDiscovery, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconnected.Replay(ctx, "tool-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := reconnected.RunTool(ctx, tool); err != nil {
		t.Fatal(err)
	}

	wantBinding := operationBinding{
		sessionID:     "session-1",
		operationID:   "tool-1",
		sequence:      2,
		kind:          OperationTool,
		planHash:      plan.CanonicalHash().String(),
		nonce:         "nonce-tool",
		payloadHash:   fingerprint("a"),
		canonicalHash: fingerprint("b"),
	}
	if got := bindingOf(firstCodec.operationRequests[1]); got != wantBinding {
		t.Fatalf("disconnected operation binding = %+v, want %+v", got, wantBinding)
	}
	if got := len(restartedCodec.operationRequests); got != 2 {
		t.Fatalf("restarted operation requests = %d, want 2", got)
	}
	for index, operation := range restartedCodec.operationRequests {
		if got := bindingOf(operation); got != wantBinding {
			t.Fatalf("restarted operation binding %d = %+v, want %+v", index, got, wantBinding)
		}
	}

	evidence, err := reconnected.Evidence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(evidence.Artifacts()); got != 2 {
		t.Fatalf("deduplicated artifact count = %d, want 2", got)
	}
	if got := len(evidence.OperationEvidence()); got != 2 {
		t.Fatalf("deduplicated operation evidence count = %d, want 2", got)
	}
}
