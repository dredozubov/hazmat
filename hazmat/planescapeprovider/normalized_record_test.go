package planescapeprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

type orderedCheckpointStore struct {
	MemoryStore
	saves []string
}

func (s *orderedCheckpointStore) Save(
	ctx context.Context,
	key string,
	value []byte,
) error {
	s.saves = append(s.saves, key)
	return s.MemoryStore.Save(ctx, key, value)
}

func (s *orderedCheckpointStore) resetSaves() {
	s.saves = nil
}

func (s *orderedCheckpointStore) replace(key string, value []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[key] = append([]byte(nil), value...)
}

func TestOperationInputOwnsToolOnlyNormalizedRecord(t *testing.T) {
	source := []byte{0x00, 0x01, 0xfe, 0xff}
	input, err := NewOperationInput(OperationInputValues{
		OperationID:      "tool-normalized",
		Kind:             OperationTool,
		Nonce:            "nonce-normalized",
		PayloadHash:      fingerprint("a"),
		NormalizedRecord: source,
	})
	if err != nil {
		t.Fatal(err)
	}
	source[0] = 0xff
	if got := input.NormalizedRecord(); !bytes.Equal(
		got,
		[]byte{0x00, 0x01, 0xfe, 0xff},
	) {
		t.Fatalf("constructor retained caller bytes: %x", got)
	}
	exposed := input.NormalizedRecord()
	exposed[1] = 0xff
	if got := input.NormalizedRecord(); !bytes.Equal(
		got,
		[]byte{0x00, 0x01, 0xfe, 0xff},
	) {
		t.Fatalf("accessor exposed mutable bytes: %x", got)
	}

	sessionID, err := NewIdentifier("session-normalized")
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := NewOperationSequence(1)
	if err != nil {
		t.Fatal(err)
	}
	planHash, err := ParseFingerprint(fingerprint("b"))
	if err != nil {
		t.Fatal(err)
	}
	operation, err := newAgentOperation(sessionID, sequence, planHash, input)
	if err != nil {
		t.Fatal(err)
	}
	comparable := map[AgentOperation]struct{}{operation: {}}
	if _, ok := comparable[operation]; !ok {
		t.Fatal("AgentOperation lost value comparability")
	}

	for _, kind := range []OperationKind{
		OperationAgentStart,
		OperationWorkspace,
		OperationPause,
		OperationCancel,
		OperationFreeze,
		OperationCloseout,
	} {
		withoutRecord, err := NewOperationInput(OperationInputValues{
			OperationID: "non-tool-" + string(kind),
			Kind:        kind,
			Nonce:       "nonce-" + string(kind),
			PayloadHash: fingerprint("c"),
		})
		if err != nil {
			t.Fatalf("%s without normalized record: %v", kind, err)
		}
		if got := withoutRecord.NormalizedRecord(); len(got) != 0 {
			t.Fatalf("%s retained normalized bytes", kind)
		}
		if _, err := NewOperationInput(OperationInputValues{
			OperationID:      "non-tool-record-" + string(kind),
			Kind:             kind,
			Nonce:            "nonce-record-" + string(kind),
			PayloadHash:      fingerprint("d"),
			NormalizedRecord: []byte("forbidden"),
		}); err == nil {
			t.Fatalf("%s accepted Tool-only normalized bytes", kind)
		}
	}

	for _, normalized := range [][]byte{nil, make([]byte, MaxRecordBytes+1)} {
		if _, err := NewOperationInput(OperationInputValues{
			OperationID:      "invalid-tool-record",
			Kind:             OperationTool,
			Nonce:            "invalid-tool-record-nonce",
			PayloadHash:      fingerprint("e"),
			NormalizedRecord: normalized,
		}); err == nil {
			t.Fatal("Tool accepted an empty or oversized normalized record")
		}
	}
}

func TestClientPersistsToolRecordSidecarBeforeCheckpointAndReplaysIt(
	t *testing.T,
) {
	ctx := context.Background()
	plan := mustCompiledPlan(t)
	result := mustResult(t, "tool-sidecar", 1, ResultCompleted, "a", "b", "c")
	endpoint := &scriptedEndpoint{
		capabilities: mustReleasedCapabilities(t),
		admission:    mustAdmission(t, plan),
		operations:   []operationReply{{response: result}},
	}
	store := &orderedCheckpointStore{}
	client, err := NewClient(ClientConfig{
		Endpoint: endpoint,
		Store:    store,
		Now:      func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := client.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session := mustSession(t, client, discovery, plan)
	store.resetSaves()

	normalized := []byte("opaque-normalized-tool-record")
	input, err := NewOperationInput(OperationInputValues{
		OperationID:      "tool-sidecar",
		Kind:             OperationTool,
		Nonce:            "tool-sidecar-nonce",
		PayloadHash:      fingerprint("a"),
		NormalizedRecord: normalized,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.RunTool(ctx, input); err != nil {
		t.Fatal(err)
	}

	sidecarKey := normalizedRecordStoreKey(input.normalizedRecord.sha256)
	sessionID, ok := session.ID()
	if !ok {
		t.Fatal("session has no identity")
	}
	checkpointKey := sessionCheckpointStoreKey(sessionID)
	if len(store.saves) < 2 ||
		store.saves[0] != sidecarKey ||
		store.saves[1] != checkpointKey {
		t.Fatalf("save order = %v, want sidecar before checkpoint", store.saves)
	}
	sidecar, ok, err := store.Load(ctx, sidecarKey)
	if err != nil || !ok || !bytes.Equal(sidecar, normalized) {
		t.Fatalf("sidecar = %q, %v, %v", sidecar, ok, err)
	}
	checkpointBytes, ok, err := store.Load(ctx, checkpointKey)
	if err != nil || !ok {
		t.Fatalf("checkpoint = %v, %v", ok, err)
	}
	if bytes.Contains(checkpointBytes, normalized) {
		t.Fatal("session checkpoint embedded normalized bytes")
	}
	var checkpoint checkpointDTO
	if err := json.Unmarshal(checkpointBytes, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if len(checkpoint.Operations) != 1 ||
		checkpoint.Operations[0].NormalizedRecordSHA256 !=
			input.normalizedRecord.sha256.String() {
		t.Fatalf("checkpoint sidecar reference = %+v", checkpoint.Operations)
	}
	if len(endpoint.operated) != 1 ||
		!bytes.Equal(endpoint.operated[0].normalized.Bytes(), normalized) {
		t.Fatal("first operation lost normalized bytes")
	}

	restartedEndpoint := &scriptedEndpoint{
		capabilities: mustReleasedCapabilities(t),
		operations:   []operationReply{{response: result}},
	}
	restarted, err := NewClient(ClientConfig{
		Endpoint: restartedEndpoint,
		Store:    store,
		Now:      func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	restartedDiscovery, err := restarted.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restartedSession, err := restarted.Reconnect(
		ctx,
		restartedDiscovery,
		sessionID.String(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restartedSession.Replay(ctx, "tool-sidecar"); err != nil {
		t.Fatal(err)
	}
	if len(restartedEndpoint.operated) != 1 ||
		!bytes.Equal(restartedEndpoint.operated[0].normalized.Bytes(), normalized) {
		t.Fatal("restart replay lost exact normalized bytes")
	}
}

func TestClientRejectsToolRecordSidecarSubstitutionAndReplayConflict(
	t *testing.T,
) {
	t.Run("sidecar substitution", func(t *testing.T) {
		ctx := context.Background()
		plan := mustCompiledPlan(t)
		result := mustResult(t, "tool-substitution", 1, ResultCompleted, "a", "b", "c")
		endpoint := &scriptedEndpoint{
			capabilities: mustReleasedCapabilities(t),
			admission:    mustAdmission(t, plan),
			operations:   []operationReply{{response: result}},
		}
		store := &orderedCheckpointStore{}
		client, err := NewClient(ClientConfig{
			Endpoint: endpoint,
			Store:    store,
			Now:      func() time.Time { return testNow },
		})
		if err != nil {
			t.Fatal(err)
		}
		discovery, err := client.Discover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		session := mustSession(t, client, discovery, plan)
		input := mustOperation(
			t,
			"tool-substitution",
			OperationTool,
			"tool-substitution-nonce",
			"a",
		)
		if _, err := session.RunTool(ctx, input); err != nil {
			t.Fatal(err)
		}
		store.replace(
			normalizedRecordStoreKey(input.normalizedRecord.sha256),
			[]byte("hostile-substitution"),
		)

		restartedEndpoint := &scriptedEndpoint{
			capabilities: mustReleasedCapabilities(t),
		}
		restarted, err := NewClient(ClientConfig{
			Endpoint: restartedEndpoint,
			Store:    store,
			Now:      func() time.Time { return testNow },
		})
		if err != nil {
			t.Fatal(err)
		}
		restartedDiscovery, err := restarted.Discover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		sessionID, ok := session.ID()
		if !ok {
			t.Fatal("session has no identity")
		}
		_, err = restarted.Reconnect(
			ctx,
			restartedDiscovery,
			sessionID.String(),
		)
		requireClass(t, err, ErrorConflict)
		if len(restartedEndpoint.operated) != 0 {
			t.Fatal("substituted sidecar reached endpoint")
		}
	})

	t.Run("same operation identity different normalized bytes", func(t *testing.T) {
		ctx := context.Background()
		plan := mustCompiledPlan(t)
		endpoint := &scriptedEndpoint{
			capabilities: mustReleasedCapabilities(t),
			admission:    mustAdmission(t, plan),
			operations: []operationReply{{
				response: mustResult(
					t,
					"tool-replay-conflict",
					1,
					ResultCompleted,
					"a",
					"b",
					"c",
				),
			}},
		}
		client := mustClient(t, endpoint)
		discovery, err := client.Discover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		session := mustSession(t, client, discovery, plan)
		first := mustOperation(
			t,
			"tool-replay-conflict",
			OperationTool,
			"tool-replay-conflict-nonce",
			"a",
		)
		if _, err := session.RunTool(ctx, first); err != nil {
			t.Fatal(err)
		}
		second, err := NewOperationInput(OperationInputValues{
			OperationID:      first.OperationID().String(),
			Kind:             first.Kind(),
			Nonce:            first.Nonce().String(),
			PayloadHash:      first.PayloadHash().String(),
			NormalizedRecord: []byte("different-normalized-record"),
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = session.RunTool(ctx, second)
		requireClass(t, err, ErrorConflict)
		if len(endpoint.operated) != 1 {
			t.Fatal("conflicting replay reached endpoint")
		}
	})
}
