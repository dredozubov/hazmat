package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedBlockRoundTrip(t *testing.T) {
	original := "export FOO=1\nexport BAR=2\n"
	updated := upsertManagedBlock(original, userPathBlockStart, userPathBlockEnd,
		`export PATH="$HOME/.local/bin:$PATH"`)

	if got := removeManagedBlock(updated, userPathBlockStart, userPathBlockEnd); got != original {
		t.Fatalf("round-trip mismatch:\nwant %q\ngot  %q", original, got)
	}
}

func TestUpsertManagedBlockReplacesExisting(t *testing.T) {
	initial := managedBlock(userPathBlockStart, userPathBlockEnd, "export PATH=old") + "\nexport FOO=1\n"
	updated := upsertManagedBlock(initial, userPathBlockStart, userPathBlockEnd, "export PATH=new")

	if count := len([]rune(updated)); count == 0 {
		t.Fatal("updated block should not be empty")
	}
	if want := "export PATH=new"; !containsLine(updated, want) {
		t.Fatalf("expected updated content %q in %q", want, updated)
	}
	if containsLine(updated, "export PATH=old") {
		t.Fatalf("old managed content was not replaced: %q", updated)
	}
}

func TestIsWithinDir(t *testing.T) {
	base := "/Users/dr/workspace"

	if !isWithinDir(base, "/Users/dr/workspace/project") {
		t.Fatal("expected child path to be inside base")
	}
	if !isWithinDir(base, "/Users/dr/workspace") {
		t.Fatal("expected base path to be inside base")
	}
	if isWithinDir(base, "/Users/dr/project") {
		t.Fatal("expected unrelated path to be outside base")
	}
}

func containsLine(s, want string) bool {
	for _, line := range strings.Split(s, "\n") {
		if line == want {
			return true
		}
	}
	return false
}

func TestCurrentUserShellProfileZsh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")

	profile, ok := currentUserShellProfile()
	if !ok {
		t.Fatal("expected zsh profile to be detected")
	}
	if profile.rcPath != filepath.Join(os.Getenv("HOME"), ".zshrc") {
		t.Fatalf("rcPath = %q", profile.rcPath)
	}
	if len(profile.pathBlockLines) != 1 || profile.pathBlockLines[0] != `export PATH="$HOME/.local/bin:$PATH"` {
		t.Fatalf("unexpected path block lines: %#v", profile.pathBlockLines)
	}
}

func TestCurrentUserShellProfileFish(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/opt/homebrew/bin/fish")

	profile, ok := currentUserShellProfile()
	if !ok {
		t.Fatal("expected fish profile to be detected")
	}
	if profile.rcPath != filepath.Join(os.Getenv("HOME"), ".config", "fish", "config.fish") {
		t.Fatalf("rcPath = %q", profile.rcPath)
	}
	if len(profile.pathBlockLines) != 3 {
		t.Fatalf("unexpected fish path block lines: %#v", profile.pathBlockLines)
	}
}
