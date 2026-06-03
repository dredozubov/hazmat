package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"hazmat/harnesses"
)

func TestLoadMissingStateReturnsEmptySnapshot(t *testing.T) {
	snapshot, err := Store{Path: filepath.Join(t.TempDir(), "state.json")}.Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if snapshot.InitVersion != "" || snapshot.HasHarnessState() {
		t.Fatalf("snapshot = %+v, want empty", snapshot)
	}
}

func TestSaveVersionPreservesHarnessState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := Store{
		Path: path,
		Now:  func() time.Time { return time.Date(2026, 6, 3, 12, 30, 0, 0, time.FixedZone("test", 4*60*60)) },
	}
	if err := store.Write(Snapshot{
		Harnesses: map[harnesses.ID]HarnessState{
			harnesses.Claude: {
				StateVersion:    "1",
				LastImportRunAt: "2026-06-01T00:00:00Z",
			},
		},
	}); err != nil {
		t.Fatalf("Write(): %v", err)
	}

	if err := store.SaveVersion("0.4.0"); err != nil {
		t.Fatalf("SaveVersion(): %v", err)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if snapshot.InitVersion != "0.4.0" || snapshot.InitDate != "2026-06-03T08:30:00Z" {
		t.Fatalf("core state = %+v", snapshot)
	}
	if snapshot.Harnesses[harnesses.Claude].LastImportRunAt != "2026-06-01T00:00:00Z" {
		t.Fatalf("harness state was not preserved: %+v", snapshot.Harnesses)
	}
}

func TestRemoveIgnoresMissingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := (Store{Path: path}).Remove(); err != nil {
		t.Fatalf("Remove(): %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("state file exists after Remove(), err=%v", err)
	}
}
