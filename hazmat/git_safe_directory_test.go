package main

import (
	"reflect"
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

func TestSafeDirectoryCoversExactAndWildcardEntries(t *testing.T) {
	repoDir := "/Users/dr/workspace/stack-matrix/pydantic-ai"
	for _, tc := range []struct {
		name    string
		entries []string
		want    bool
	}{
		{
			name:    "exact match",
			entries: []string{repoDir},
			want:    true,
		},
		{
			name:    "workspace wildcard",
			entries: []string{"/Users/dr/workspace/*"},
			want:    true,
		},
		{
			name:    "global wildcard",
			entries: []string{"*"},
			want:    true,
		},
		{
			name:    "unrelated path",
			entries: []string{"/tmp/elsewhere/*"},
			want:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeDirectoryCovers(tc.entries, repoDir); got != tc.want {
				t.Fatalf("safeDirectoryCovers(%v, %q) = %v, want %v", tc.entries, repoDir, got, tc.want)
			}
		})
	}
}

func TestEnsureAgentGitSafeDirectoryAddsExactRepoTrust(t *testing.T) {
	savedDetect := detectGitRepoTopLevel
	savedSystem := readSystemGitSafeDirectoryEntries
	savedAgent := readAgentGlobalGitSafeDirectoryEntries
	savedAppend := appendAgentGlobalSafeDirectoryEntry
	t.Cleanup(func() {
		detectGitRepoTopLevel = savedDetect
		readSystemGitSafeDirectoryEntries = savedSystem
		readAgentGlobalGitSafeDirectoryEntries = savedAgent
		appendAgentGlobalSafeDirectoryEntry = savedAppend
	})

	repoDir := "/Users/dr/workspace/stack-matrix/pydantic-ai"
	detectGitRepoTopLevel = func(projectDir string) (string, bool) {
		return repoDir, true
	}
	readSystemGitSafeDirectoryEntries = func() ([]string, error) {
		return nil, nil
	}
	agentEntries := []string(nil)
	readAgentGlobalGitSafeDirectoryEntries = func() ([]string, error) {
		return append([]string(nil), agentEntries...), nil
	}
	appendCalls := 0
	appendAgentGlobalSafeDirectoryEntry = func(entry string) error {
		appendCalls++
		agentEntries = append(agentEntries, entry)
		return nil
	}

	changed, err := ensureAgentGitSafeDirectory("/tmp/project")
	if err != nil {
		t.Fatalf("ensureAgentGitSafeDirectory() error = %v", err)
	}
	if !changed {
		t.Fatal("ensureAgentGitSafeDirectory() = false, want true")
	}
	if appendCalls != 1 {
		t.Fatalf("appendAgentGlobalSafeDirectoryEntry called %d times, want 1", appendCalls)
	}
	if len(agentEntries) != 1 || agentEntries[0] != repoDir {
		t.Fatalf("agentEntries = %v, want [%q]", agentEntries, repoDir)
	}
}

func TestAppendAgentGlobalSafeDirectoryCommandUsesRootWorkingDir(t *testing.T) {
	repoDir := "/Users/dr/workspace/stack-matrix/pydantic-ai"

	cmd := appendAgentGlobalSafeDirectoryCommand(repoDir)

	if cmd.Dir != "/" {
		t.Fatalf("appendAgentGlobalSafeDirectoryCommand().Dir = %q, want %q", cmd.Dir, "/")
	}

	wantArgs := []string{
		"sudo",
		"-u",
		agentUser,
		"-H",
		"git",
		"config",
		"--global",
		"--add",
		"safe.directory",
		repoDir,
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("appendAgentGlobalSafeDirectoryCommand().Args = %v, want %v", cmd.Args, wantArgs)
	}
}
