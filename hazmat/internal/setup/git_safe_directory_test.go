package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupGitSafeDirectoryWritesManagedEntries(t *testing.T) {
	tmp := t.TempDir()
	gitconfig := filepath.Join(tmp, "gitconfig")
	var syncedWanted []string
	env := GitSafeDirectoryEnv{
		SystemGitConfigPath: func() string { return gitconfig },
		ManagedEntries:      func() []string { return []string{"/Users/dr/workspace/*"} },
		SyncConfig: func(content string, wanted []string) (string, bool) {
			syncedWanted = append([]string(nil), wanted...)
			return content + "[safe]\n\tdirectory = " + wanted[0] + " # hazmat-managed\n", true
		},
	}
	runner := newFakeToolingRunner(t)

	if err := SetupGitSafeDirectory(env, &fakeToolingUI{}, runner); err != nil {
		t.Fatalf("SetupGitSafeDirectory: %v", err)
	}

	if len(syncedWanted) != 1 || syncedWanted[0] != "/Users/dr/workspace/*" {
		t.Fatalf("synced wanted = %v", syncedWanted)
	}
	if got := runner.sudoWrites[gitconfig]; !strings.Contains(got, "/Users/dr/workspace/* # hazmat-managed") {
		t.Fatalf("gitconfig write = %q", got)
	}
}

func TestSetupGitSafeDirectorySkipsWithoutReadDirs(t *testing.T) {
	var synced bool
	env := GitSafeDirectoryEnv{
		SystemGitConfigPath: func() string { return filepath.Join(t.TempDir(), "gitconfig") },
		ManagedEntries:      func() []string { return nil },
		SyncConfig: func(string, []string) (string, bool) {
			synced = true
			return "", false
		},
	}

	if err := SetupGitSafeDirectory(env, &fakeToolingUI{}, newFakeToolingRunner(t)); err != nil {
		t.Fatalf("SetupGitSafeDirectory: %v", err)
	}
	if synced {
		t.Fatal("SyncConfig ran even though there were no managed entries")
	}
}

func TestRollbackGitSafeDirectoryRemovesManagedEntries(t *testing.T) {
	tmp := t.TempDir()
	gitconfig := filepath.Join(tmp, "gitconfig")
	if err := os.WriteFile(gitconfig, []byte("[safe]\n\tdirectory = old # hazmat-managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := GitSafeDirectoryEnv{
		SystemGitConfigPath: func() string { return gitconfig },
		SyncConfig: func(string, []string) (string, bool) {
			return "", true
		},
	}
	runner := newFakeToolingRunner(t)

	RollbackGitSafeDirectory(env, &fakeToolingUI{}, runner)

	if got := runner.sudoWrites[gitconfig]; got != "" {
		t.Fatalf("rollback gitconfig write = %q, want empty cleaned config", got)
	}
}
