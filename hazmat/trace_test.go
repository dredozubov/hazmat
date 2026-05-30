package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestClaudeTraceLaunchArgsAddsYesBeforeForwardedArgs(t *testing.T) {
	got := claudeTraceLaunchArgs([]string{"--no-backup", "-p", "say ok"})
	want := []string{"claude", "--yes", "--no-backup", "-p", "say ok"}
	if !slices.Equal(got, want) {
		t.Fatalf("claudeTraceLaunchArgs = %v, want %v", got, want)
	}
}

func TestTraceLaunchArgsAddsYesBeforeForwardedArgs(t *testing.T) {
	spec, ok := traceHarnessSpecByID(HarnessCodex)
	if !ok {
		t.Fatal("missing codex trace harness spec")
	}
	got := traceLaunchArgs(spec, []string{"--no-backup", "exec", "say ok"})
	want := []string{"codex", "--yes", "--no-backup", "exec", "say ok"}
	if !slices.Equal(got, want) {
		t.Fatalf("traceLaunchArgs = %v, want %v", got, want)
	}
}

func TestSanitizeTraceLabel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"Baseline Run", "baseline-run"},
		{"  sandbox/logout? probe!  ", "sandbox-logout-probe"},
		{"keep.ok_value-1", "keep.ok_value-1"},
	}
	for _, tc := range tests {
		if got := sanitizeTraceLabel(tc.in); got != tc.want {
			t.Fatalf("sanitizeTraceLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSupportedTraceHarnessSpecsCoverManagedHarnesses(t *testing.T) {
	got := make(map[HarnessID]bool)
	for _, spec := range supportedTraceHarnessSpecs() {
		got[spec.ID] = true
		if spec.CommandName == "" {
			t.Fatalf("%s CommandName is empty", spec.ID)
		}
		if len(spec.ProcessFilters) == 0 {
			t.Fatalf("%s ProcessFilters is empty", spec.ID)
		}
		if len(spec.SampleArgs) == 0 {
			t.Fatalf("%s SampleArgs is empty", spec.ID)
		}
		if spec.Parser == nil {
			t.Fatalf("%s Parser is nil", spec.ID)
		}
	}
	for _, managed := range managedHarnessRegistry {
		if !got[managed.Spec.ID] {
			t.Fatalf("managed harness %q is missing from trace harness specs", managed.Spec.ID)
		}
	}
}

func TestPrepareClaudeTraceDirUsesPrivateTimestampedDirectory(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 28, 18, 30, 0, 0, time.UTC)

	dir, label, err := prepareClaudeTraceDir(claudeTraceOptions{
		OutRoot: root,
		Name:    "Baseline Run",
	}, now)
	if err != nil {
		t.Fatalf("prepareClaudeTraceDir: %v", err)
	}
	if label != "baseline-run" {
		t.Fatalf("label = %q, want baseline-run", label)
	}
	if !strings.HasPrefix(filepath.Base(dir), "20260528T183000Z-claude-baseline-run") {
		t.Fatalf("trace dir = %s", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat trace dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("trace dir mode = %04o, want 0700", info.Mode().Perm())
	}
}

func TestPrepareTraceDirUsesHarnessName(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 28, 18, 30, 0, 0, time.UTC)

	dir, label, err := prepareTraceDir(traceOptions{
		Harness: HarnessGemini,
		OutRoot: root,
		Name:    "Network None",
	}, now)
	if err != nil {
		t.Fatalf("prepareTraceDir: %v", err)
	}
	if label != "network-none" {
		t.Fatalf("label = %q, want network-none", label)
	}
	if !strings.HasPrefix(filepath.Base(dir), "20260528T183000Z-gemini-network-none") {
		t.Fatalf("trace dir = %s", dir)
	}
}

func TestProcessSampleLineRelevantUsesHarnessFilters(t *testing.T) {
	spec, ok := traceHarnessSpecByID(HarnessOpenCode)
	if !ok {
		t.Fatal("missing opencode trace harness spec")
	}
	if !processSampleLineRelevant("123 1 1 agent S 00:00 /Users/agent/.opencode/bin/opencode", spec) {
		t.Fatal("expected opencode agent process to be relevant")
	}
	if !processSampleLineRelevant("123 1 1 dr S 00:00 /Users/dr/.local/bin/hazmat opencode", spec) {
		t.Fatal("expected hazmat process to be relevant")
	}
	if processSampleLineRelevant("123 1 1 dr S 00:00 /usr/bin/unrelated", spec) {
		t.Fatal("unexpected unrelated process relevance")
	}
}
