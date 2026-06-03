package backupruntime

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPreSessionSnapshotSkipDoesNothing(t *testing.T) {
	var called bool
	var stderr bytes.Buffer

	PreSessionSnapshot(PreSessionSnapshotOptions{
		ProjectDir: "/work/project",
		Command:    "exec",
		Skip:       true,
		Stderr:     &stderr,
		Snapshot: func(string, string, ...string) error {
			called = true
			return nil
		},
	})

	if called {
		t.Fatal("snapshot was called despite Skip")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestPreSessionSnapshotSuccessReportsDuration(t *testing.T) {
	var gotProject string
	var gotCommand string
	var gotIgnores []string
	var stderr bytes.Buffer
	ticks := []time.Time{
		time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 3, 12, 0, 1, 500_000_000, time.UTC),
	}

	PreSessionSnapshot(PreSessionSnapshotOptions{
		ProjectDir:     "/work/project",
		Command:        "claude",
		BackupExcludes: []string{"node_modules", ".git"},
		Stderr:         &stderr,
		Now: func() time.Time {
			tick := ticks[0]
			ticks = ticks[1:]
			return tick
		},
		Snapshot: func(projectDir, command string, ignoreRules ...string) error {
			gotProject = projectDir
			gotCommand = command
			gotIgnores = append([]string(nil), ignoreRules...)
			return nil
		},
	})

	if gotProject != "/work/project" || gotCommand != "claude" || !slices.Equal(gotIgnores, []string{"node_modules", ".git"}) {
		t.Fatalf("snapshot args = %q %q %v", gotProject, gotCommand, gotIgnores)
	}
	if got := stderr.String(); got != "  Snapshot: /work/project ... done (1.5s)\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestPreSessionSnapshotFailureWarnsAndDoesNotPropagate(t *testing.T) {
	var stderr bytes.Buffer

	PreSessionSnapshot(PreSessionSnapshotOptions{
		ProjectDir: "/work/project",
		Command:    "exec",
		Stderr:     &stderr,
		Snapshot: func(string, string, ...string) error {
			return errors.New("repo unavailable")
		},
	})

	got := stderr.String()
	for _, want := range []string{
		"  Snapshot: /work/project ... ",
		"Warning: pre-session snapshot failed: repo unavailable",
		"Session will proceed without a restore point.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr missing %q:\n%s", want, got)
		}
	}
}

func TestPreSessionSnapshotMissingSnapshotFunctionWarns(t *testing.T) {
	var stderr bytes.Buffer

	PreSessionSnapshot(PreSessionSnapshotOptions{
		ProjectDir: "/work/project",
		Command:    "exec",
		Stderr:     &stderr,
	})

	got := stderr.String()
	if !strings.Contains(got, "Warning: pre-session snapshot failed: snapshot function is not configured") {
		t.Fatalf("stderr missing missing-snapshot warning:\n%s", got)
	}
}
