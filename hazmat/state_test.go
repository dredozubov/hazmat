package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSemverCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.2.0", -1},
		{"0.2.0", "0.1.0", 1},
		{"0.2.0", "0.2.0", 0},
		{"0.1.0", "0.3.0", -1},
		{"1.0.0", "0.9.9", 1},
		{"v0.2.0", "0.2.0", 0},      // v prefix stripped
		{"0.2.0-dirty", "0.2.0", 0}, // -dirty stripped
		{"dev", "0.3.0", 1},         // dev > any release
		{"0.3.0", "dev", -1},        // release < dev
		{"dev", "dev", 0},
		{"abc123", "0.1.0", 1}, // commit hash = dev
	}
	for _, tc := range tests {
		got := semverCompare(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("semverCompare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestPendingMigrations(t *testing.T) {
	t.Run("fresh install", func(t *testing.T) {
		chain := pendingMigrations("", "0.3.0")
		if chain != nil {
			t.Errorf("expected nil for fresh install, got %d migrations", len(chain))
		}
	})

	t.Run("already current", func(t *testing.T) {
		chain := pendingMigrations("0.3.0", "0.3.0")
		if chain != nil {
			t.Errorf("expected nil for current version, got %d migrations", len(chain))
		}
	})

	t.Run("one step", func(t *testing.T) {
		chain := pendingMigrations("0.2.0", "0.3.0")
		if len(chain) != 1 {
			t.Fatalf("expected 1 migration, got %d", len(chain))
		}
		if chain[0].From != "0.2.0" || chain[0].To != "0.3.0" {
			t.Errorf("wrong migration: %s→%s", chain[0].From, chain[0].To)
		}
	})

	t.Run("two steps", func(t *testing.T) {
		chain := pendingMigrations("0.1.0", "0.3.0")
		if len(chain) != 2 {
			t.Fatalf("expected 2 migrations, got %d", len(chain))
		}
		if chain[0].From != "0.1.0" || chain[0].To != "0.2.0" {
			t.Errorf("first: %s→%s", chain[0].From, chain[0].To)
		}
		if chain[1].From != "0.2.0" || chain[1].To != "0.3.0" {
			t.Errorf("second: %s→%s", chain[1].From, chain[1].To)
		}
	})

	t.Run("dev version", func(t *testing.T) {
		chain := pendingMigrations("0.1.0", "dev")
		if chain != nil {
			t.Errorf("expected nil for dev target, got %d", len(chain))
		}
	})
}

func TestSaveStatePreservesHarnessState(t *testing.T) {
	originalStateFilePath := stateFilePath
	stateFilePath = filepath.Join(t.TempDir(), "state.json")
	t.Cleanup(func() {
		stateFilePath = originalStateFilePath
	})

	if err := updateHarnessState(HarnessClaude, func(state HarnessState) HarnessState {
		state.StateVersion = "1"
		state.LastImportRunAt = "2026-04-01T10:00:00Z"
		return state
	}); err != nil {
		t.Fatalf("updateHarnessState: %v", err)
	}

	if err := saveState("0.3.0"); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	state, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if state.InitVersion != "0.3.0" {
		t.Fatalf("InitVersion = %q, want %q", state.InitVersion, "0.3.0")
	}

	claude, ok := state.Harnesses[HarnessClaude]
	if !ok {
		t.Fatalf("expected %q harness state to be preserved", HarnessClaude)
	}
	if claude.StateVersion != "1" {
		t.Fatalf("StateVersion = %q, want %q", claude.StateVersion, "1")
	}
	if claude.LastImportRunAt != "2026-04-01T10:00:00Z" {
		t.Fatalf("LastImportRunAt = %q, want preserved value", claude.LastImportRunAt)
	}
}

func TestRunDownMigrationsRemovesHarnessOnlyStateFile(t *testing.T) {
	originalStateFilePath := stateFilePath
	stateFilePath = filepath.Join(t.TempDir(), "state.json")
	t.Cleanup(func() {
		stateFilePath = originalStateFilePath
	})

	if err := claudeCodeHarness.RecordInstalled(); err != nil {
		t.Fatalf("RecordInstalled: %v", err)
	}

	runDownMigrations(&UI{}, nil)

	if _, err := os.Stat(stateFilePath); !os.IsNotExist(err) {
		t.Fatalf("state file still exists after rollback cleanup, err=%v", err)
	}
}
