package main

import (
	"os"
	"path/filepath"
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
