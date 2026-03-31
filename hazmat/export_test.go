package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeClaudeSessionID(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "abc-123", want: "abc-123"},
		{input: "abc-123.jsonl", want: "abc-123"},
		{input: "", wantErr: true},
		{input: ".", wantErr: true},
		{input: "../escape", wantErr: true},
		{input: "dir/session", wantErr: true},
	}

	for _, tt := range tests {
		got, err := normalizeClaudeSessionID(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("normalizeClaudeSessionID(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeClaudeSessionID(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeClaudeSessionID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStagedClaudeSessionIndexEntryRewritesPathsAndKeepsMetadata(t *testing.T) {
	stagingDir := t.TempDir()
	sessionID := "1234"
	destPath := filepath.Join(t.TempDir(), sessionID+".jsonl")
	modTime := time.Unix(1700000000, 0)

	index := claudeSessionsIndex{
		Version:      1,
		OriginalPath: "/Users/dr/workspace/hazmat",
		Entries: []map[string]any{
			{
				"sessionId":   sessionID,
				"fullPath":    "/Users/agent/.claude/projects/foo/1234.jsonl",
				"fileMtime":   1,
				"summary":     "keep me",
				"projectPath": "/Users/dr/workspace/hazmat/subdir",
			},
		},
	}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "sessions-index.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	entry, originalPath, err := stagedClaudeSessionIndexEntry(stagingDir, sessionID, destPath, "/fallback/project", modTime)
	if err != nil {
		t.Fatalf("stagedClaudeSessionIndexEntry: %v", err)
	}
	if originalPath != index.OriginalPath {
		t.Fatalf("originalPath = %q, want %q", originalPath, index.OriginalPath)
	}
	if entry["fullPath"] != destPath {
		t.Fatalf("fullPath = %v, want %q", entry["fullPath"], destPath)
	}
	if entry["summary"] != "keep me" {
		t.Fatalf("summary = %v, want keep me", entry["summary"])
	}
	if entry["projectPath"] != "/Users/dr/workspace/hazmat/subdir" {
		t.Fatalf("projectPath = %v, want source projectPath preserved", entry["projectPath"])
	}
	if entry["fileMtime"] != modTime.UnixMilli() {
		t.Fatalf("fileMtime = %v, want %d", entry["fileMtime"], modTime.UnixMilli())
	}
}

func TestStagedClaudeSessionIndexEntrySynthesizesWhenMissing(t *testing.T) {
	stagingDir := t.TempDir()
	sessionID := "abcd"
	destPath := filepath.Join(t.TempDir(), sessionID+".jsonl")
	modTime := time.Unix(1700000100, 0)

	entry, originalPath, err := stagedClaudeSessionIndexEntry(stagingDir, sessionID, destPath, "/fallback/project", modTime)
	if err != nil {
		t.Fatalf("stagedClaudeSessionIndexEntry: %v", err)
	}
	if originalPath != "/fallback/project" {
		t.Fatalf("originalPath = %q, want fallback project", originalPath)
	}
	if entry["sessionId"] != sessionID {
		t.Fatalf("sessionId = %v, want %q", entry["sessionId"], sessionID)
	}
	if entry["fullPath"] != destPath {
		t.Fatalf("fullPath = %v, want %q", entry["fullPath"], destPath)
	}
	if entry["projectPath"] != "/fallback/project" {
		t.Fatalf("projectPath = %v, want fallback project", entry["projectPath"])
	}
}

func TestInstallStagedClaudeSessionBundleCopiesAndReplacesBundleDir(t *testing.T) {
	stagingDir := t.TempDir()
	destDir := t.TempDir()
	sessionID := "1234"

	if err := os.WriteFile(filepath.Join(stagingDir, sessionID+".jsonl"), []byte("fresh transcript"), 0o600); err != nil {
		t.Fatal(err)
	}
	srcSidecar := filepath.Join(stagingDir, sessionID, "tool-results")
	if err := os.MkdirAll(srcSidecar, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcSidecar, "result.txt"), []byte("fresh result"), 0o600); err != nil {
		t.Fatal(err)
	}

	destSidecar := filepath.Join(destDir, sessionID)
	if err := os.MkdirAll(destSidecar, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destSidecar, "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := installStagedClaudeSessionBundle(stagingDir, destDir, sessionID); err != nil {
		t.Fatalf("installStagedClaudeSessionBundle: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(destDir, sessionID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fresh transcript" {
		t.Fatalf("transcript = %q, want fresh transcript", data)
	}
	if _, err := os.Stat(filepath.Join(destSidecar, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale sidecar file should be removed, got err=%v", err)
	}
	sidecarData, err := os.ReadFile(filepath.Join(destSidecar, "tool-results", "result.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sidecarData) != "fresh result" {
		t.Fatalf("sidecar file = %q, want fresh result", sidecarData)
	}
}

func TestUpsertClaudeSessionsIndexReplacesExistingEntry(t *testing.T) {
	indexPath := filepath.Join(t.TempDir(), "sessions-index.json")
	index := claudeSessionsIndex{
		Version:      1,
		OriginalPath: "/Users/dr/workspace/hazmat",
		Entries: []map[string]any{
			{"sessionId": "keep", "fullPath": "keep.jsonl"},
			{"sessionId": "replace", "fullPath": "old.jsonl"},
		},
	}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	entry := map[string]any{"sessionId": "replace", "fullPath": "new.jsonl"}
	if err := upsertClaudeSessionsIndex(indexPath, "/Users/dr/workspace/hazmat", entry); err != nil {
		t.Fatalf("upsertClaudeSessionsIndex: %v", err)
	}

	updatedData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var updated claudeSessionsIndex
	if err := json.Unmarshal(updatedData, &updated); err != nil {
		t.Fatal(err)
	}

	if len(updated.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(updated.Entries))
	}
	for _, existing := range updated.Entries {
		if sessionIDFromIndexEntry(existing) == "replace" && existing["fullPath"] != "new.jsonl" {
			t.Fatalf("replaced entry fullPath = %v, want new.jsonl", existing["fullPath"])
		}
	}
}

func TestExtractTarArchiveRejectsEscape(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o600, Size: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := extractTarArchive(bytes.NewReader(buf.Bytes()), t.TempDir()); err == nil {
		t.Fatal("expected tar path escape to be rejected")
	}
}
