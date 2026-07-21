package planescapeprovider

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)

type operationReply struct {
	response OperationResponse
	err      error
}

type freezeReply struct {
	ack FreezeAck
	err error
}

type cancellationReply struct {
	ack CancellationAck
	err error
}

type scriptedEndpoint struct {
	capabilities  ProviderCapabilities
	discoverErr   error
	admission     SessionAdmission
	admitErr      error
	operations    []operationReply
	freezes       []freezeReply
	cancellations []cancellationReply
	calls         []string
	admitCalls    int
}

func (s *scriptedEndpoint) Discover(_ context.Context) (ProviderCapabilities, error) {
	s.calls = append(s.calls, "discover")
	return s.capabilities, s.discoverErr
}

func (s *scriptedEndpoint) Admit(_ context.Context, _ ExecutionRequirement) (SessionAdmission, error) {
	s.calls = append(s.calls, "admit")
	s.admitCalls++
	return s.admission, s.admitErr
}

func (s *scriptedEndpoint) Operate(_ context.Context, operation AgentOperation) (OperationResponse, error) {
	s.calls = append(s.calls, "operate:"+operation.OperationID().String())
	if len(s.operations) == 0 {
		return nil, errors.New("unexpected operation")
	}
	reply := s.operations[0]
	s.operations = s.operations[1:]
	return reply.response, reply.err
}

func (s *scriptedEndpoint) Freeze(_ context.Context, request Freeze) (FreezeAck, error) {
	s.calls = append(s.calls, "freeze:"+request.FreezeID().String())
	if len(s.freezes) == 0 {
		return FreezeAck{}, errors.New("unexpected freeze")
	}
	reply := s.freezes[0]
	s.freezes = s.freezes[1:]
	return reply.ack, reply.err
}

func (s *scriptedEndpoint) Cancel(_ context.Context, request Cancellation) (CancellationAck, error) {
	s.calls = append(s.calls, "cancel:"+request.CancellationID().String())
	if len(s.cancellations) == 0 {
		return CancellationAck{}, errors.New("unexpected cancellation")
	}
	reply := s.cancellations[0]
	s.cancellations = s.cancellations[1:]
	return reply.ack, reply.err
}

func fingerprint(char string) string {
	return "sha256:" + strings.Repeat(char, 64)
}

func mustCapabilities(t *testing.T, profile Profile, capabilities []Capability) ProviderCapabilities {
	t.Helper()
	value, err := NewProviderCapabilities(ProviderCapabilitiesInput{
		ProviderID:         "provider-1",
		ProviderEpoch:      7,
		Profile:            profile,
		CapabilityHash:     fingerprint("a"),
		CanonicalHash:      fingerprint("b"),
		Capabilities:       capabilities,
		ResourceDimensions: []ResourceDimension{ResourceCPUTime, ResourceMemoryBytes, ResourceWorkspaceBytes},
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustRequirement(t *testing.T) ExecutionRequirement {
	t.Helper()
	value, err := NewExecutionRequirement(ExecutionRequirementInput{
		RequirementID:              "requirement-1",
		ControllerAttemptID:        "attempt-1",
		AuthorityHash:              fingerprint("1"),
		RequiredCapabilities:       []Capability{CapabilityToolExecute, CapabilityWorkspaceRead},
		RequiredResourceDimensions: []ResourceDimension{ResourceMemoryBytes, ResourceWorkspaceBytes},
		EvidenceProfileHash:        fingerprint("2"),
		CanonicalHash:              fingerprint("3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustAdmission(t *testing.T, requirement ExecutionRequirement) SessionAdmission {
	t.Helper()
	value, err := NewSessionAdmission(SessionAdmissionInput{
		SessionID:             "session-1",
		ProviderID:            "provider-1",
		ProviderEpoch:         7,
		RequirementHash:       requirement.CanonicalHash().String(),
		CompiledPlanHash:      fingerprint("4"),
		SessionCapabilityHash: fingerprint("5"),
		ExpiresAt:             testNow.Add(time.Hour),
		CanonicalHash:         fingerprint("6"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustOperation(t *testing.T, id string, kind OperationKind, nonce, payload, canonical string) OperationInput {
	t.Helper()
	value, err := NewOperationInput(OperationInputValues{
		OperationID:   id,
		Kind:          kind,
		Nonce:         nonce,
		PayloadHash:   fingerprint(payload),
		CanonicalHash: fingerprint(canonical),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustResult(t *testing.T, operationID string, sequence uint64, result ResultKind, artifact, evidence, canonical string) OperationResult {
	t.Helper()
	value, err := NewOperationResult(OperationResultInput{
		SessionID:     "session-1",
		OperationID:   operationID,
		Sequence:      sequence,
		ResultKind:    result,
		ArtifactHash:  fingerprint(artifact),
		EvidenceHash:  fingerprint(evidence),
		CanonicalHash: fingerprint(canonical),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustQuiescence(t *testing.T) Quiescence {
	t.Helper()
	value, err := NewQuiescence(QuiescenceInput{
		SessionID:            "session-1",
		QuiescenceHash:       fingerprint("9"),
		ResourceEvidenceHash: fingerprint("a"),
		CanonicalHash:        fingerprint("b"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustFreezeAck(t *testing.T) FreezeAck {
	t.Helper()
	value, err := NewFreezeAck(FreezeAckInput{
		SessionID:      "session-1",
		FreezeID:       "freeze-1",
		QuiescenceHash: fingerprint("9"),
		FrozenAt:       testNow.Add(time.Minute),
		CanonicalHash:  fingerprint("c"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustCloseout(t *testing.T, closeoutID string) Closeout {
	t.Helper()
	value, err := NewCloseout(CloseoutInput{
		SessionID:           "session-1",
		CloseoutID:          closeoutID,
		TerminalOutcome:     OutcomeSucceeded,
		QuiescenceHash:      fingerprint("9"),
		LogicalEvidenceHash: fingerprint("d"),
		NativeExtensionHash: fingerprint("e"),
		CanonicalHash:       fingerprint("f"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustClient(t *testing.T, endpoint Endpoint) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{Endpoint: endpoint, Store: &MemoryStore{}, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func mustSession(t *testing.T, client *Client, discovery Discovery, requirement ExecutionRequirement) Session {
	t.Helper()
	input, err := NewAdmissionInput(requirement, ProfilePortable)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.Admit(context.Background(), discovery, input)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func requireClass(t *testing.T, err error, want ErrorClass) {
	t.Helper()
	if err == nil {
		t.Fatalf("error class = nil, want %s", want)
	}
	var providerError *Error
	if !errors.As(err, &providerError) {
		t.Fatalf("error %T = %v, want planescape provider error", err, err)
	}
	if providerError.Class() != want {
		t.Fatalf("error class = %s, want %s", providerError.Class(), want)
	}
}

func TestClientMapsFullLifecycleAndReplay(t *testing.T) {
	requirement := mustRequirement(t)
	endpoint := &scriptedEndpoint{
		capabilities: mustCapabilities(t, ProfileStockLinux, []Capability{CapabilityArtifactRead, CapabilityToolExecute, CapabilityWorkspaceRead, CapabilityWorkspaceWrite}),
		admission:    mustAdmission(t, requirement),
		operations: []operationReply{
			{response: mustResult(t, "launch-1", 1, ResultAccepted, "7", "8", "9")},
			{response: mustResult(t, "tool-1", 2, ResultCompleted, "a", "b", "c")},
			{response: mustQuiescence(t)},
			{response: mustCloseout(t, "closeout-terminal-1")},
			{response: mustResult(t, "tool-1", 2, ResultCompleted, "a", "b", "c")},
		},
		freezes: []freezeReply{{ack: mustFreezeAck(t)}},
	}
	client := mustClient(t, endpoint)
	discovery, err := client.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session := mustSession(t, client, discovery, requirement)

	if _, err := session.Launch(context.Background(), mustOperation(t, "launch-1", OperationAgentStart, "nonce-launch", "7", "8")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.RunTool(context.Background(), mustOperation(t, "tool-1", OperationTool, "nonce-tool", "a", "b")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Quiesce(context.Background(), mustOperation(t, "pause-1", OperationPause, "nonce-pause", "c", "d")); err != nil {
		t.Fatal(err)
	}
	freezeInput, err := NewFreezeInput(FreezeInputValues{FreezeID: "freeze-1", Nonce: "nonce-freeze", CanonicalHash: fingerprint("e")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Freeze(context.Background(), freezeInput); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Closeout(context.Background(), mustOperation(t, "closeout-operation-1", OperationCloseout, "nonce-closeout", "f", "1"), "closeout-terminal-1"); err != nil {
		t.Fatal(err)
	}

	evidence, err := session.Evidence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(evidence.Artifacts()); got != 2 {
		t.Fatalf("artifact count = %d, want 2", got)
	}
	if got, ok := evidence.ResourceEvidence(); !ok || got.String() != fingerprint("a") {
		t.Fatalf("resource evidence = %q, %v", got.String(), ok)
	}
	if got, ok := evidence.LogicalEvidence(); !ok || got.String() != fingerprint("d") {
		t.Fatalf("logical evidence = %q, %v", got.String(), ok)
	}
	if got, ok := evidence.NativeExtension(); !ok || got.String() != fingerprint("e") {
		t.Fatalf("native extension = %q, %v", got.String(), ok)
	}

	reconnected, err := client.Reconnect(context.Background(), discovery, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconnected.Replay(context.Background(), "tool-1"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(endpoint.calls, ","); got != "discover,admit,operate:launch-1,operate:tool-1,operate:pause-1,freeze:freeze-1,operate:closeout-operation-1,operate:tool-1" {
		t.Fatalf("calls = %s", got)
	}
}

func TestClientCancellationBindsEvidence(t *testing.T) {
	requirement := mustRequirement(t)
	ack, err := NewCancellationAck(CancellationAckInput{
		SessionID:           "session-1",
		CancellationID:      "cancel-1",
		TerminalOutcome:     OutcomeCancelled,
		LogicalEvidenceHash: fingerprint("e"),
		CanonicalHash:       fingerprint("f"),
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &scriptedEndpoint{
		capabilities:  mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead}),
		admission:     mustAdmission(t, requirement),
		operations:    []operationReply{{response: mustResult(t, "launch-1", 1, ResultAccepted, "7", "8", "9")}},
		cancellations: []cancellationReply{{ack: ack}},
	}
	client := mustClient(t, endpoint)
	discovery, err := client.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session := mustSession(t, client, discovery, requirement)
	if _, err := session.Launch(context.Background(), mustOperation(t, "launch-1", OperationAgentStart, "nonce-launch", "7", "8")); err != nil {
		t.Fatal(err)
	}
	cancelInput, err := NewCancellationInput(CancellationInputValues{CancellationID: "cancel-1", Reason: "operator_cancelled", Nonce: "nonce-cancel", CanonicalHash: fingerprint("a")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Cancel(context.Background(), cancelInput); err != nil {
		t.Fatal(err)
	}
	evidence, err := session.Evidence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := evidence.LogicalEvidence(); !ok || got.String() != fingerprint("e") {
		t.Fatalf("logical evidence = %q, %v", got.String(), ok)
	}
}

func TestClientReplaysExactFreezeAndCancellation(t *testing.T) {
	t.Run("freeze", func(t *testing.T) {
		requirement := mustRequirement(t)
		endpoint := &scriptedEndpoint{
			capabilities: mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead}),
			admission:    mustAdmission(t, requirement),
			operations: []operationReply{
				{response: mustResult(t, "launch-1", 1, ResultAccepted, "7", "8", "9")},
				{response: mustQuiescence(t)},
			},
			freezes: []freezeReply{{ack: mustFreezeAck(t)}, {ack: mustFreezeAck(t)}},
		}
		client := mustClient(t, endpoint)
		discovery, err := client.Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		session := mustSession(t, client, discovery, requirement)
		if _, err := session.Launch(context.Background(), mustOperation(t, "launch-1", OperationAgentStart, "nonce-launch", "7", "8")); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Quiesce(context.Background(), mustOperation(t, "pause-1", OperationPause, "nonce-pause", "9", "a")); err != nil {
			t.Fatal(err)
		}
		input, err := NewFreezeInput(FreezeInputValues{FreezeID: "freeze-1", Nonce: "nonce-freeze", CanonicalHash: fingerprint("b")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.Freeze(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Freeze(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		requirement := mustRequirement(t)
		ack, err := NewCancellationAck(CancellationAckInput{
			SessionID:           "session-1",
			CancellationID:      "cancel-1",
			TerminalOutcome:     OutcomeCancelled,
			LogicalEvidenceHash: fingerprint("e"),
			CanonicalHash:       fingerprint("f"),
		})
		if err != nil {
			t.Fatal(err)
		}
		endpoint := &scriptedEndpoint{
			capabilities:  mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead}),
			admission:     mustAdmission(t, requirement),
			operations:    []operationReply{{response: mustResult(t, "launch-1", 1, ResultAccepted, "7", "8", "9")}},
			cancellations: []cancellationReply{{ack: ack}, {ack: ack}},
		}
		client := mustClient(t, endpoint)
		discovery, err := client.Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		session := mustSession(t, client, discovery, requirement)
		if _, err := session.Launch(context.Background(), mustOperation(t, "launch-1", OperationAgentStart, "nonce-launch", "7", "8")); err != nil {
			t.Fatal(err)
		}
		input, err := NewCancellationInput(CancellationInputValues{CancellationID: "cancel-1", Reason: "operator_cancelled", Nonce: "nonce-cancel", CanonicalHash: fingerprint("a")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.Cancel(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Cancel(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	})
}

func TestClientMapsProviderDenialAndRejectsRestartedEpoch(t *testing.T) {
	t.Run("denial", func(t *testing.T) {
		requirement := mustRequirement(t)
		denial, err := NewProviderFailure(ProviderFailureInput{
			Code:          ProviderErrorUnsupported,
			ProviderID:    "provider-1",
			ProviderEpoch: 7,
			RetryFrom:     TransitionAdmit,
			CanonicalHash: fingerprint("a"),
		})
		if err != nil {
			t.Fatal(err)
		}
		endpoint := &scriptedEndpoint{
			capabilities: mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead}),
			admitErr:     denial,
		}
		client := mustClient(t, endpoint)
		discovery, err := client.Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		input, err := NewAdmissionInput(requirement, ProfilePortable)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Admit(context.Background(), discovery, input)
		requireClass(t, err, ErrorUnsupported)
	})

	t.Run("restart", func(t *testing.T) {
		requirement := mustRequirement(t)
		endpoint := &scriptedEndpoint{
			capabilities: mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead}),
			admission:    mustAdmission(t, requirement),
		}
		client := mustClient(t, endpoint)
		discovery, err := client.Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_ = mustSession(t, client, discovery, requirement)
		endpoint.capabilities, err = NewProviderCapabilities(ProviderCapabilitiesInput{
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
		restartedDiscovery, err := client.Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Reconnect(context.Background(), restartedDiscovery, "session-1")
		requireClass(t, err, ErrorConflict)
	})
}

func TestClientUnavailableOperationPreservesReplay(t *testing.T) {
	requirement := mustRequirement(t)
	endpoint := &scriptedEndpoint{
		capabilities: mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead}),
		admission:    mustAdmission(t, requirement),
		operations: []operationReply{
			{response: mustResult(t, "launch-1", 1, ResultAccepted, "7", "8", "9")},
			{err: context.DeadlineExceeded},
			{response: mustResult(t, "tool-1", 2, ResultCompleted, "a", "b", "c")},
		},
	}
	client := mustClient(t, endpoint)
	discovery, err := client.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session := mustSession(t, client, discovery, requirement)
	if _, err := session.Launch(context.Background(), mustOperation(t, "launch-1", OperationAgentStart, "nonce-launch", "7", "8")); err != nil {
		t.Fatal(err)
	}
	tool := mustOperation(t, "tool-1", OperationTool, "nonce-tool", "a", "b")
	_, err = session.RunTool(context.Background(), tool)
	requireClass(t, err, ErrorUnavailable)
	if _, err := session.Replay(context.Background(), "tool-1"); err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsCapabilityDowngradeWithoutAdmitting(t *testing.T) {
	requirement := mustRequirement(t)
	endpoint := &scriptedEndpoint{
		capabilities: mustCapabilities(t, ProfilePortable, []Capability{CapabilityWorkspaceRead}),
		admission:    mustAdmission(t, requirement),
	}
	client := mustClient(t, endpoint)
	discovery, err := client.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input, err := NewAdmissionInput(requirement, ProfilePortable)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Admit(context.Background(), discovery, input)
	requireClass(t, err, ErrorUnsupported)
	if endpoint.admitCalls != 0 {
		t.Fatalf("admit calls = %d, want 0", endpoint.admitCalls)
	}
}

func TestClientRejectsMalformedAndCrossSessionResults(t *testing.T) {
	requirement := mustRequirement(t)
	wrongSession, err := NewOperationResult(OperationResultInput{
		SessionID:     "session-other",
		OperationID:   "launch-1",
		Sequence:      1,
		ResultKind:    ResultAccepted,
		ArtifactHash:  fingerprint("7"),
		EvidenceHash:  fingerprint("8"),
		CanonicalHash: fingerprint("9"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, response := range map[string]OperationResponse{
		"malformed":     OperationResult{},
		"cross-session": wrongSession,
	} {
		t.Run(name, func(t *testing.T) {
			endpoint := &scriptedEndpoint{
				capabilities: mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead}),
				admission:    mustAdmission(t, requirement),
				operations:   []operationReply{{response: response}},
			}
			client := mustClient(t, endpoint)
			discovery, err := client.Discover(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			session := mustSession(t, client, discovery, requirement)
			_, err = session.Launch(context.Background(), mustOperation(t, "launch-1", OperationAgentStart, "nonce-launch", "7", "8"))
			requireClass(t, err, ErrorConflict)
			_, err = session.Replay(context.Background(), "launch-1")
			requireClass(t, err, ErrorConflict)
		})
	}
}

func TestClientRejectsConflictingDuplicateAndMissingProvider(t *testing.T) {
	_, err := NewClient(ClientConfig{Store: &MemoryStore{}})
	requireClass(t, err, ErrorUnavailable)

	requirement := mustRequirement(t)
	endpoint := &scriptedEndpoint{
		capabilities: mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead}),
		admission:    mustAdmission(t, requirement),
		operations: []operationReply{
			{response: mustResult(t, "launch-1", 1, ResultAccepted, "7", "8", "9")},
			{response: mustResult(t, "tool-1", 2, ResultAccepted, "a", "b", "c")},
		},
	}
	client := mustClient(t, endpoint)
	discovery, err := client.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session := mustSession(t, client, discovery, requirement)
	if _, err := session.Launch(context.Background(), mustOperation(t, "launch-1", OperationAgentStart, "nonce-launch", "7", "8")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.RunTool(context.Background(), mustOperation(t, "tool-1", OperationTool, "nonce-one", "a", "b")); err != nil {
		t.Fatal(err)
	}
	_, err = session.RunTool(context.Background(), mustOperation(t, "tool-1", OperationTool, "nonce-two", "c", "d"))
	requireClass(t, err, ErrorConflict)
	if got := len(endpoint.calls); got != 4 {
		t.Fatalf("calls = %d, want 4", got)
	}
}
