package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveReferenceDirsDeduplicates(t *testing.T) {
	workspace := t.TempDir()
	refA := filepath.Join(workspace, "ref-a")
	refB := filepath.Join(workspace, "ref-b")
	for _, dir := range []string{refA, refB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	refAResolved, err := filepath.EvalSymlinks(refA)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", refA, err)
	}
	refBResolved, err := filepath.EvalSymlinks(refB)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", refB, err)
	}

	got, err := resolveReferenceDirs([]string{refA, refA, refB}, workspace)
	if err != nil {
		t.Fatalf("resolveReferenceDirs returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 unique references, got %d (%v)", len(got), got)
	}
	if got[0] != refAResolved || got[1] != refBResolved {
		t.Fatalf("unexpected reference order/content: %v", got)
	}
}

func TestResolveReferenceDirsRejectsPathsOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}

	if _, err := resolveReferenceDirs([]string{outside}, workspace); err == nil {
		t.Fatal("expected outside reference path to be rejected")
	}
}

func TestAgentEnvPairsExposeWorkspaceSession(t *testing.T) {
	cfg := sessionConfig{
		WorkspaceRoot: "/Users/dr/workspace",
		ProjectDir:    "/Users/dr/workspace/project",
		ReferenceDirs: []string{
			"/Users/dr/workspace/ref-a",
			"/Users/dr/workspace/ref-b",
		},
	}

	pairs := agentEnvPairs(cfg)
	values := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			t.Fatalf("malformed env pair: %q", pair)
		}
		values[key] = value
	}

	if values["SANDBOX_WORKSPACE_ROOT"] != cfg.WorkspaceRoot {
		t.Fatalf("SANDBOX_WORKSPACE_ROOT = %q, want %q", values["SANDBOX_WORKSPACE_ROOT"], cfg.WorkspaceRoot)
	}
	if values["SANDBOX_PROJECT_DIR"] != cfg.ProjectDir {
		t.Fatalf("SANDBOX_PROJECT_DIR = %q, want %q", values["SANDBOX_PROJECT_DIR"], cfg.ProjectDir)
	}

	var refs []string
	if err := json.Unmarshal([]byte(values["SANDBOX_REFERENCE_DIRS_JSON"]), &refs); err != nil {
		t.Fatalf("unmarshal SANDBOX_REFERENCE_DIRS_JSON: %v", err)
	}
	if len(refs) != len(cfg.ReferenceDirs) {
		t.Fatalf("reference count = %d, want %d", len(refs), len(cfg.ReferenceDirs))
	}
}
