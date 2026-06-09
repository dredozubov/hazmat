package hazmat

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRunBackupCommandUsesExplicitProjectScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "workspace"), 0o700); err != nil {
		t.Fatalf("create fake workspace: %v", err)
	}
	project := t.TempDir()
	want, err := resolveDir(project, true)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}

	var got string
	err = runBackupCommand(true, project, func(projectDir string) error {
		got = projectDir
		return nil
	})
	if err != nil {
		t.Fatalf("runBackupCommand: %v", err)
	}
	if got != want {
		t.Fatalf("cloud backup project = %q, want %q", got, want)
	}
}

func TestRunBackupCommandDefaultsToCurrentProjectScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "workspace"), 0o700); err != nil {
		t.Fatalf("create fake workspace: %v", err)
	}
	project := t.TempDir()
	t.Chdir(project)
	want, err := resolveDir("", true)
	if err != nil {
		t.Fatalf("resolve cwd project: %v", err)
	}

	var got string
	err = runBackupCommand(true, "", func(projectDir string) error {
		got = projectDir
		return nil
	})
	if err != nil {
		t.Fatalf("runBackupCommand: %v", err)
	}
	if got != want {
		t.Fatalf("cloud backup project = %q, want %q", got, want)
	}
}

func TestRunRestoreCommandUsesExplicitCloudProjectScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "workspace"), 0o700); err != nil {
		t.Fatalf("create fake workspace: %v", err)
	}
	project := t.TempDir()
	want, err := resolveDir(project, true)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}

	var got string
	err = runRestoreCommand(true, project, 3, func(projectDir string) error {
		got = projectDir
		return nil
	}, func(string, int) error {
		t.Fatal("local restore should not run for --cloud")
		return nil
	})
	if err != nil {
		t.Fatalf("runRestoreCommand: %v", err)
	}
	if got != want {
		t.Fatalf("cloud restore project = %q, want %q", got, want)
	}
}

func TestRunRestoreCommandUsesExplicitLocalProjectScope(t *testing.T) {
	project := t.TempDir()
	want, err := resolveDir(project, true)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}

	var gotProject string
	var gotSession int
	err = runRestoreCommand(false, project, 3, func(string) error {
		t.Fatal("cloud restore should not run without --cloud")
		return nil
	}, func(projectDir string, sessionIdx int) error {
		gotProject = projectDir
		gotSession = sessionIdx
		return nil
	})
	if err != nil {
		t.Fatalf("runRestoreCommand: %v", err)
	}
	if gotProject != want || gotSession != 3 {
		t.Fatalf("local restore args = %q, %d; want %q, 3", gotProject, gotSession, want)
	}
}

func TestRunBackupCommandRejectsLocalModeWithoutCallingCloudBackup(t *testing.T) {
	called := false
	err := runBackupCommand(false, "", func(string) error {
		called = true
		return errors.New("should not be called")
	})
	if err == nil {
		t.Fatal("runBackupCommand succeeded without --cloud")
	}
	if called {
		t.Fatal("cloud backup was called without --cloud")
	}
}
