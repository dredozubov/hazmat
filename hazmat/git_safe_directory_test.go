package main

import (
	"strings"
	"testing"
)

func TestManagedSafeDirectoryEntriesDeduplicatesAndSorts(t *testing.T) {
	t.Setenv("HOME", "/Users/dr")

	got := managedSafeDirectoryEntries([]string{"~/workspace", "/tmp/project", "/tmp/project", "~/workspace"})
	want := []string{"/Users/dr/workspace/*", "/tmp/project/*"}
	if len(got) != len(want) {
		t.Fatalf("managedSafeDirectoryEntries() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("managedSafeDirectoryEntries()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseSystemGitConfigOrigin(t *testing.T) {
	out := "file:/opt/homebrew/etc/gitconfig\tsafe.directory=/Users/dr/workspace/*\nfile:/opt/homebrew/etc/gitconfig\tuser.name=Agent\n"
	if got := parseSystemGitConfigOrigin(out); got != "/opt/homebrew/etc/gitconfig" {
		t.Fatalf("parseSystemGitConfigOrigin() = %q", got)
	}
}

func TestSyncHazmatSafeDirectoryConfigAddsManagedSection(t *testing.T) {
	updated, changed := syncHazmatSafeDirectoryConfig("", []string{"/Users/dr/workspace/*"})
	if !changed {
		t.Fatal("expected config to change")
	}
	if !strings.HasPrefix(updated, "[safe]\n") {
		t.Fatalf("updated config should start with [safe], got:\n%s", updated)
	}
	if !strings.Contains(updated, "[safe]\n\tdirectory = /Users/dr/workspace/*"+hazmatSafeDirMarker) {
		t.Fatalf("updated config missing managed safe.directory entry:\n%s", updated)
	}
}

func TestSyncHazmatSafeDirectoryConfigKeepsUserEntriesAndReplacesManagedOnes(t *testing.T) {
	content := strings.Join([]string{
		"[safe]",
		"\tdirectory = /keep/me",
		"\tdirectory = /old/path/*" + hazmatSafeDirMarker,
		"",
	}, "\n")

	updated, changed := syncHazmatSafeDirectoryConfig(content, []string{"/new/path/*"})
	if !changed {
		t.Fatal("expected config to change")
	}
	if !strings.Contains(updated, "\tdirectory = /keep/me\n") {
		t.Fatalf("updated config dropped user entry:\n%s", updated)
	}
	if strings.Contains(updated, "/old/path/*"+hazmatSafeDirMarker) {
		t.Fatalf("updated config kept stale managed entry:\n%s", updated)
	}
	if !strings.Contains(updated, "\tdirectory = /new/path/*"+hazmatSafeDirMarker) {
		t.Fatalf("updated config missing new managed entry:\n%s", updated)
	}
}

func TestSyncHazmatSafeDirectoryConfigRemovesManagedEntriesForRollback(t *testing.T) {
	content := strings.Join([]string{
		"[safe]",
		"\tdirectory = /keep/me",
		"\tdirectory = /managed/path/*" + hazmatSafeDirMarker,
		"",
	}, "\n")

	updated, changed := syncHazmatSafeDirectoryConfig(content, nil)
	if !changed {
		t.Fatal("expected config to change")
	}
	if !strings.Contains(updated, "\tdirectory = /keep/me\n") {
		t.Fatalf("updated config dropped user entry:\n%s", updated)
	}
	if strings.Contains(updated, hazmatSafeDirMarker) {
		t.Fatalf("updated config kept managed entry:\n%s", updated)
	}
}

func TestSyncHazmatSafeDirectoryConfigNoopWhenAlreadyMatches(t *testing.T) {
	content := strings.Join([]string{
		"[safe]",
		"\tdirectory = /Users/dr/workspace/*" + hazmatSafeDirMarker,
		"",
	}, "\n")

	updated, changed := syncHazmatSafeDirectoryConfig(content, []string{"/Users/dr/workspace/*"})
	if changed {
		t.Fatalf("expected no change, got:\n%s", updated)
	}
}
