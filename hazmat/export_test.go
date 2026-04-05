package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestCleanClaudeBundleRelativePath(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: ".", want: ""},                                        // skipped
		{input: "..", wantErr: true},                                  // escape
		{input: "../escape", wantErr: true},                           // escape
		{input: "/absolute/path", wantErr: true},                      // absolute
		{input: "session/tool-results/foo.txt", want: "session/tool-results/foo.txt"},
		{input: "session", want: "session"},
		{input: "session/..", want: ""},                                // cleans to "." — skipped
		{input: "./session", want: "session"},                         // cleaned
		{input: "", want: ""},                                         // cleans to "." — skipped
	}

	for _, tt := range tests {
		got, err := cleanClaudeBundleRelativePath(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("cleanClaudeBundleRelativePath(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("cleanClaudeBundleRelativePath(%q): %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("cleanClaudeBundleRelativePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestClaudeProjectDirExactMatch(t *testing.T) {
	home := t.TempDir()
	projectDir := "/Users/dr/workspace/foo"
	sanitized := sanitizePathForClaude(projectDir)
	dir := filepath.Join(home, ".claude", "projects", sanitized)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := claudeProjectDir(home, projectDir)
	if got != dir {
		t.Fatalf("claudeProjectDir = %q, want %q", got, dir)
	}
}

func TestClaudeProjectDirLongPathPrefix(t *testing.T) {
	home := t.TempDir()
	long := "/Users/dr/workspace/" + strings.Repeat("abcdefghij", 25)
	sanitized := sanitizePathForClaude(long)
	prefix := sanitized[:maxSanitizedLength]
	dirName := prefix + "-bun123hash"
	dir := filepath.Join(home, ".claude", "projects", dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := claudeProjectDir(home, long)
	if got != dir {
		t.Fatalf("claudeProjectDir = %q, want %q", got, dir)
	}
}

func TestClaudeProjectDirReturnsEmptyWhenMissing(t *testing.T) {
	home := t.TempDir()
	got := claudeProjectDir(home, "/nonexistent/project")
	if got != "" {
		t.Fatalf("claudeProjectDir = %q, want empty", got)
	}

	got = claudeProjectDir("", "/any/project")
	if got != "" {
		t.Fatalf("claudeProjectDir with empty home = %q, want empty", got)
	}
}

func TestCopyDirTree(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()

	// Build nested tree: src/a.txt, src/sub/b.txt, src/sub/deep/c.txt
	if err := os.MkdirAll(filepath.Join(src, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"a.txt":          "content-a",
		"sub/b.txt":      "content-b",
		"sub/deep/c.txt": "content-c",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(src, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	destTarget := filepath.Join(dest, "copied")
	if err := copyDirTree(src, destTarget); err != nil {
		t.Fatalf("copyDirTree: %v", err)
	}

	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(destTarget, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("%s content = %q, want %q", rel, got, want)
		}
		info, err := os.Stat(filepath.Join(destTarget, rel))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("%s mode = %04o, want 0600", rel, perm)
		}
	}
}

func TestRewriteClaudeSessionIndexEntry(t *testing.T) {
	original := map[string]any{
		"sessionId":   "abc",
		"fullPath":    "/old/path.jsonl",
		"fileMtime":   int64(1000),
		"projectPath": "/original/project",
		"created":     "2024-01-01T00:00:00Z",
		"summary":     "keep this",
		"customField": 42,
	}

	modTime := time.Unix(2000, 0)
	got := rewriteClaudeSessionIndexEntry(original, "abc", "/new/path.jsonl", "/fallback", modTime)

	// Overwritten fields.
	if got["fullPath"] != "/new/path.jsonl" {
		t.Fatalf("fullPath = %v, want /new/path.jsonl", got["fullPath"])
	}
	if got["fileMtime"] != modTime.UnixMilli() {
		t.Fatalf("fileMtime = %v, want %d", got["fileMtime"], modTime.UnixMilli())
	}

	// Preserved fields.
	if got["projectPath"] != "/original/project" {
		t.Fatalf("projectPath = %v, want /original/project (should preserve non-empty)", got["projectPath"])
	}
	if got["created"] != "2024-01-01T00:00:00Z" {
		t.Fatalf("created = %v, should be preserved", got["created"])
	}
	if got["summary"] != "keep this" {
		t.Fatalf("summary = %v, should be carried through", got["summary"])
	}
	if got["customField"] != 42 {
		t.Fatalf("customField = %v, should be carried through", got["customField"])
	}

	// Original map must not be mutated.
	if original["fullPath"] != "/old/path.jsonl" {
		t.Fatal("original map was mutated")
	}
}

func TestRewriteClaudeSessionIndexEntryFallbackProjectPath(t *testing.T) {
	original := map[string]any{
		"sessionId":   "abc",
		"projectPath": "",
	}

	got := rewriteClaudeSessionIndexEntry(original, "abc", "/dest.jsonl", "/fallback/project", time.Unix(1, 0))
	if got["projectPath"] != "/fallback/project" {
		t.Fatalf("projectPath = %v, want /fallback/project (should use fallback when empty)", got["projectPath"])
	}
}

func TestSynthesizeClaudeSessionIndexEntry(t *testing.T) {
	modTime := time.Unix(1700000000, 0)
	got := synthesizeClaudeSessionIndexEntry("sess-1", "/dest/sess-1.jsonl", "/proj", modTime)

	if got["sessionId"] != "sess-1" {
		t.Fatalf("sessionId = %v", got["sessionId"])
	}
	if got["fullPath"] != "/dest/sess-1.jsonl" {
		t.Fatalf("fullPath = %v", got["fullPath"])
	}
	if got["fileMtime"] != modTime.UnixMilli() {
		t.Fatalf("fileMtime = %v, want %d", got["fileMtime"], modTime.UnixMilli())
	}
	if got["projectPath"] != "/proj" {
		t.Fatalf("projectPath = %v", got["projectPath"])
	}
	// Timestamps are UTC RFC3339Nano.
	ts := modTime.UTC().Format(time.RFC3339Nano)
	if got["created"] != ts {
		t.Fatalf("created = %v, want %s", got["created"], ts)
	}
	if got["modified"] != ts {
		t.Fatalf("modified = %v, want %s", got["modified"], ts)
	}
}

func TestSessionIDFromIndexEntry(t *testing.T) {
	if got := sessionIDFromIndexEntry(map[string]any{"sessionId": "abc"}); got != "abc" {
		t.Fatalf("got %q, want abc", got)
	}
	if got := sessionIDFromIndexEntry(map[string]any{}); got != "" {
		t.Fatalf("missing key: got %q, want empty", got)
	}
	if got := sessionIDFromIndexEntry(map[string]any{"sessionId": 123}); got != "" {
		t.Fatalf("wrong type: got %q, want empty", got)
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
