package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizePathForClaude(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/Users/dr/workspace/sandboxing", "-Users-dr-workspace-sandboxing"},
		{"/Users/dr/workspace/my-project", "-Users-dr-workspace-my-project"},
		{"/tmp/foo", "-tmp-foo"},
		{"simple", "simple"},
		{"/a/b/c", "-a-b-c"},
	}
	for _, tt := range tests {
		got := sanitizePathForClaude(tt.input)
		if got != tt.want {
			t.Errorf("sanitizePathForClaude(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectResumeFlagsNone(t *testing.T) {
	r, target, c := detectResumeFlags([]string{"-p", "hello", "--model", "sonnet"})
	if r || c || target != "" {
		t.Fatalf("expected no resume/continue, got resume=%v target=%q continue=%v", r, target, c)
	}
}

func TestDetectResumeFlagsContinue(t *testing.T) {
	for _, flag := range []string{"--continue", "-c"} {
		r, _, c := detectResumeFlags([]string{flag})
		if r {
			t.Fatalf("%s: unexpected resume=true", flag)
		}
		if !c {
			t.Fatalf("%s: expected continue=true", flag)
		}
	}
}

func TestDetectResumeFlagsResumeNoTarget(t *testing.T) {
	for _, flag := range []string{"--resume", "-r"} {
		r, target, c := detectResumeFlags([]string{flag})
		if !r {
			t.Fatalf("%s: expected resume=true", flag)
		}
		if c {
			t.Fatalf("%s: unexpected continue=true", flag)
		}
		if target != "" {
			t.Fatalf("%s: expected empty target, got %q", flag, target)
		}
	}
}

func TestDetectResumeFlagsResumeWithUUID(t *testing.T) {
	uuid := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	r, target, _ := detectResumeFlags([]string{"--resume", uuid})
	if !r {
		t.Fatal("expected resume=true")
	}
	if target != uuid {
		t.Fatalf("target = %q, want %q", target, uuid)
	}
}

func TestDetectResumeFlagsResumeEquals(t *testing.T) {
	uuid := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	r, target, _ := detectResumeFlags([]string{"--resume=" + uuid})
	if !r {
		t.Fatal("expected resume=true")
	}
	if target != uuid {
		t.Fatalf("target = %q, want %q", target, uuid)
	}
}

func TestDetectResumeFlagsResumeSkipsFlags(t *testing.T) {
	// --resume followed by a flag (not a session ID) should not capture it.
	r, target, _ := detectResumeFlags([]string{"--resume", "--model", "sonnet"})
	if !r {
		t.Fatal("expected resume=true")
	}
	if target != "" {
		t.Fatalf("target should be empty when next arg is a flag, got %q", target)
	}
}

func TestDetectResumeFlagsMixedWithOtherArgs(t *testing.T) {
	r, target, c := detectResumeFlags([]string{
		"-p", "hello", "--continue", "--model", "sonnet",
	})
	if r {
		t.Fatal("unexpected resume=true")
	}
	if !c {
		t.Fatal("expected continue=true")
	}
	if target != "" {
		t.Fatalf("expected empty target, got %q", target)
	}
}

func TestSelectResumeSessionFilesContinueChoosesNewest(t *testing.T) {
	files := []resumeSessionFile{
		{name: "older.jsonl", modTime: time.Unix(10, 0)},
		{name: "newer.jsonl", modTime: time.Unix(20, 0)},
	}

	selected := selectResumeSessionFiles(files, "", true)
	if len(selected) != 1 || selected[0].name != "newer.jsonl" {
		t.Fatalf("selected = %+v, want newer.jsonl", selected)
	}
}

func TestSyncResumeSessionFilesCopiesRequestedTarget(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "keep.jsonl"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "pick.jsonl"), []byte("pick"), 0o644); err != nil {
		t.Fatal(err)
	}

	synced, err := syncResumeSessionFiles(srcDir, destDir, "pick", false)
	if err != nil {
		t.Fatalf("syncResumeSessionFiles: %v", err)
	}
	if synced != 1 {
		t.Fatalf("synced = %d, want 1", synced)
	}
	if _, err := os.Stat(filepath.Join(destDir, "pick.jsonl")); err != nil {
		t.Fatalf("pick.jsonl not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "keep.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("keep.jsonl should not be copied, got err=%v", err)
	}
}

func TestSyncResumeSessionFilesContinueCopiesOnlyLatest(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()

	oldPath := filepath.Join(srcDir, "old.jsonl")
	newPath := filepath.Join(srcDir, "new.jsonl")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Unix(10, 0)
	newTime := time.Unix(20, 0)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	synced, err := syncResumeSessionFiles(srcDir, destDir, "", true)
	if err != nil {
		t.Fatalf("syncResumeSessionFiles: %v", err)
	}
	if synced != 1 {
		t.Fatalf("synced = %d, want 1", synced)
	}
	if _, err := os.Stat(filepath.Join(destDir, "new.jsonl")); err != nil {
		t.Fatalf("new.jsonl not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "old.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("old.jsonl should not be copied, got err=%v", err)
	}
}

func TestCopyResumeSessionFileAtomicCopy(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "session.jsonl")
	destPath := filepath.Join(destDir, "session.jsonl")
	content := []byte(`{"role":"user","content":"hello"}` + "\n")

	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyResumeSessionFile(srcPath, destPath); err != nil {
		t.Fatalf("copyResumeSessionFile: %v", err)
	}

	// Content is faithfully copied.
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("dest content = %q, want %q", got, content)
	}

	// Destination gets mode 0600.
	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("dest mode = %04o, want 0600", perm)
	}

	// Source is untouched.
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(srcData) != string(content) {
		t.Fatalf("source was modified: %q", srcData)
	}
}

func TestCopyResumeSessionFileCleansTempOnError(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "session.jsonl")
	if err := os.WriteFile(srcPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Make dest directory read-only so CreateTemp fails.
	if err := os.Chmod(destDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(destDir, 0o700) })

	err := copyResumeSessionFile(srcPath, filepath.Join(destDir, "session.jsonl"))
	if err == nil {
		t.Fatal("expected error when dest dir is read-only")
	}

	// No temp files left behind.
	os.Chmod(destDir, 0o700)
	entries, _ := os.ReadDir(destDir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".jsonl" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestListResumeSessionFilesIgnoresNonJsonl(t *testing.T) {
	dir := t.TempDir()

	// Create a mix: .jsonl files, .json, directory, dotfile.
	for _, name := range []string{"b.jsonl", "a.jsonl", "c.json", ".hidden"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := listResumeSessionFiles(dir)
	if err != nil {
		t.Fatalf("listResumeSessionFiles: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	// Sorted by name.
	if files[0].name != "a.jsonl" || files[1].name != "b.jsonl" {
		t.Fatalf("files = %v, want [a.jsonl, b.jsonl]", []string{files[0].name, files[1].name})
	}
}

func TestListResumeSessionFilesEmptyDir(t *testing.T) {
	dir := t.TempDir()

	files, err := listResumeSessionFiles(dir)
	if err != nil {
		t.Fatalf("listResumeSessionFiles: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("got %d files, want 0", len(files))
	}
}

func TestInvokerSessionDirExactMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projectDir := "/Users/dr/workspace/foo"
	sanitized := sanitizePathForClaude(projectDir)
	dir := filepath.Join(home, ".claude", "projects", sanitized)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := invokerSessionDir(projectDir)
	if got != dir {
		t.Fatalf("invokerSessionDir = %q, want %q", got, dir)
	}
}

func TestInvokerSessionDirPrefixMatchForLongPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Build a project path whose sanitized form exceeds 200 chars.
	long := "/Users/dr/workspace/" + strings.Repeat("abcdefghij", 25)
	sanitized := sanitizePathForClaude(long)
	if len(sanitized) <= maxSanitizedLength {
		t.Fatalf("test setup: sanitized path should exceed %d chars, got %d", maxSanitizedLength, len(sanitized))
	}

	prefix := sanitized[:maxSanitizedLength]
	dirName := prefix + "-somehash123"
	dir := filepath.Join(home, ".claude", "projects", dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := invokerSessionDir(long)
	if got != dir {
		t.Fatalf("invokerSessionDir = %q, want %q", got, dir)
	}
}

func TestInvokerSessionDirReturnsEmptyWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := invokerSessionDir("/nonexistent/project")
	if got != "" {
		t.Fatalf("invokerSessionDir = %q, want empty", got)
	}
}

func TestSyncResumeSessionFilesReplacesSymlinks(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()

	content := []byte("real content")
	if err := os.WriteFile(filepath.Join(srcDir, "session.jsonl"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink in dest pointing to a non-existent target.
	symPath := filepath.Join(destDir, "session.jsonl")
	if err := os.Symlink("/nonexistent/target", symPath); err != nil {
		t.Fatal(err)
	}

	synced, err := syncResumeSessionFiles(srcDir, destDir, "session", false)
	if err != nil {
		t.Fatalf("syncResumeSessionFiles: %v", err)
	}
	if synced != 1 {
		t.Fatalf("synced = %d, want 1", synced)
	}

	// Symlink replaced with real file.
	info, err := os.Lstat(symPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("symlink was not replaced")
	}

	got, err := os.ReadFile(symPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

func TestSyncResumeSessionFilesLeavesExistingRegularFiles(t *testing.T) {
	srcDir := t.TempDir()
	destDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "session.jsonl"), []byte("host"), 0o644); err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(destDir, "session.jsonl")
	if err := os.WriteFile(destPath, []byte("agent"), 0o600); err != nil {
		t.Fatal(err)
	}

	synced, err := syncResumeSessionFiles(srcDir, destDir, "", false)
	if err != nil {
		t.Fatalf("syncResumeSessionFiles: %v", err)
	}
	if synced != 0 {
		t.Fatalf("synced = %d, want 0", synced)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "agent" {
		t.Fatalf("dest contents = %q, want agent copy preserved", data)
	}
}
