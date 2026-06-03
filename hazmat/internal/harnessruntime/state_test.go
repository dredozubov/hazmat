package harnessruntime

import (
	"errors"
	"testing"
	"time"

	"hazmat/harnesses"
)

type fakeStateStore struct {
	snapshot  Snapshot
	loadErr   error
	writeErr  error
	removeErr error
	writes    int
	removes   int
}

func (s *fakeStateStore) Load() (Snapshot, error) {
	if s.loadErr != nil {
		return Snapshot{}, s.loadErr
	}
	return s.snapshot, nil
}

func (s *fakeStateStore) Write(snapshot Snapshot) error {
	s.writes++
	if s.writeErr != nil {
		return s.writeErr
	}
	s.snapshot = snapshot
	return nil
}

func (s *fakeStateStore) Remove() error {
	s.removes++
	if s.removeErr != nil {
		return s.removeErr
	}
	s.snapshot = Snapshot{}
	return nil
}

func TestRecordInstalledInitializesHarnessState(t *testing.T) {
	store := &fakeStateStore{}

	if err := RecordInstalled(store, harnesses.MustSpec(harnesses.Claude)); err != nil {
		t.Fatalf("RecordInstalled: %v", err)
	}

	if store.writes != 1 {
		t.Fatalf("writes = %d, want 1", store.writes)
	}
	state, ok := store.snapshot.Harnesses[harnesses.Claude]
	if !ok {
		t.Fatal("Claude harness state was not recorded")
	}
	if state.StateVersion != harnesses.ClaudeStateVersion {
		t.Fatalf("StateVersion = %q, want %q", state.StateVersion, harnesses.ClaudeStateVersion)
	}
}

func TestRecordImportRunAtSetsTimestampAndVersion(t *testing.T) {
	store := &fakeStateStore{}
	at := time.Date(2026, 6, 3, 10, 11, 12, 0, time.FixedZone("test", 3*60*60))

	if err := RecordImportRunAt(store, harnesses.MustSpec(harnesses.Codex), at); err != nil {
		t.Fatalf("RecordImportRunAt: %v", err)
	}

	state := store.snapshot.Harnesses[harnesses.Codex]
	if state.StateVersion != harnesses.CodexStateVersion {
		t.Fatalf("StateVersion = %q, want %q", state.StateVersion, harnesses.CodexStateVersion)
	}
	if state.LastImportRunAt != "2026-06-03T07:11:12Z" {
		t.Fatalf("LastImportRunAt = %q, want UTC RFC3339 timestamp", state.LastImportRunAt)
	}
}

func TestRemoveHarnessStateDeletesHarnessOnlySnapshot(t *testing.T) {
	store := &fakeStateStore{
		snapshot: Snapshot{
			Harnesses: map[harnesses.ID]State{
				harnesses.Claude: {StateVersion: harnesses.ClaudeStateVersion},
			},
		},
	}

	if err := RemoveHarnessState(store, harnesses.Claude); err != nil {
		t.Fatalf("RemoveHarnessState: %v", err)
	}

	if store.removes != 1 {
		t.Fatalf("removes = %d, want 1", store.removes)
	}
	if store.writes != 0 {
		t.Fatalf("writes = %d, want 0", store.writes)
	}
	if store.snapshot.HasHarnessState() {
		t.Fatalf("snapshot still has harness state: %+v", store.snapshot.Harnesses)
	}
}

func TestRemoveHarnessStatePreservesCoreAndOtherHarnesses(t *testing.T) {
	store := &fakeStateStore{
		snapshot: Snapshot{
			InitVersion: "0.4.0",
			InitDate:    "2026-06-03T00:00:00Z",
			Harnesses: map[harnesses.ID]State{
				harnesses.Claude: {StateVersion: harnesses.ClaudeStateVersion},
				harnesses.Codex:  {StateVersion: harnesses.CodexStateVersion},
			},
		},
	}

	if err := RemoveHarnessState(store, harnesses.Claude); err != nil {
		t.Fatalf("RemoveHarnessState: %v", err)
	}

	if store.writes != 1 {
		t.Fatalf("writes = %d, want 1", store.writes)
	}
	if store.removes != 0 {
		t.Fatalf("removes = %d, want 0", store.removes)
	}
	if _, ok := store.snapshot.Harnesses[harnesses.Claude]; ok {
		t.Fatal("Claude harness state was not removed")
	}
	if _, ok := store.snapshot.Harnesses[harnesses.Codex]; !ok {
		t.Fatal("Codex harness state was not preserved")
	}
	if store.snapshot.InitVersion != "0.4.0" {
		t.Fatalf("InitVersion = %q, want preserved core version", store.snapshot.InitVersion)
	}
}

func TestStateTransitionsReturnStoreErrors(t *testing.T) {
	loadErr := errors.New("load failed")
	if err := RecordInstalled(&fakeStateStore{loadErr: loadErr}, harnesses.MustSpec(harnesses.Claude)); !errors.Is(err, loadErr) {
		t.Fatalf("RecordInstalled error = %v, want %v", err, loadErr)
	}

	writeErr := errors.New("write failed")
	if err := RecordInstalled(&fakeStateStore{writeErr: writeErr}, harnesses.MustSpec(harnesses.Claude)); !errors.Is(err, writeErr) {
		t.Fatalf("RecordInstalled error = %v, want %v", err, writeErr)
	}

	removeErr := errors.New("remove failed")
	store := &fakeStateStore{
		removeErr: removeErr,
		snapshot:  Snapshot{Harnesses: map[harnesses.ID]State{harnesses.Claude: {StateVersion: "1"}}},
	}
	if err := RemoveHarnessState(store, harnesses.Claude); !errors.Is(err, removeErr) {
		t.Fatalf("RemoveHarnessState error = %v, want %v", err, removeErr)
	}
}
