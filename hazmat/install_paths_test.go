package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchHelperPathUsesLocalInstallForLocalHazmat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	savedExecutable := currentExecutablePath
	currentExecutablePath = func() (string, error) {
		return filepath.Join(home, ".local", "bin", "hazmat"), nil
	}
	defer func() {
		currentExecutablePath = savedExecutable
	}()

	got := launchHelperPath()
	want := filepath.Join(home, ".local", "libexec", "hazmat-launch")
	if got != want {
		t.Fatalf("launchHelperPath() = %q, want %q", got, want)
	}
}

func TestLaunchHelperPathHonorsEnvironmentOverride(t *testing.T) {
	t.Setenv("HAZMAT_LAUNCH_HELPER", "/tmp/custom-hazmat-launch")

	if got := launchHelperPath(); got != "/tmp/custom-hazmat-launch" {
		t.Fatalf("launchHelperPath() = %q, want environment override", got)
	}
}

func TestLaunchSudoersEntryPinsUserLocalHelperWithDigest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	helperPath := filepath.Join(home, ".local", "libexec", "hazmat-launch")
	if err := os.MkdirAll(filepath.Dir(helperPath), 0o755); err != nil {
		t.Fatalf("mkdir helper dir: %v", err)
	}
	if err := os.WriteFile(helperPath, []byte("hazmat-launch"), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	savedExecutable := currentExecutablePath
	currentExecutablePath = func() (string, error) {
		return filepath.Join(home, ".local", "bin", "hazmat"), nil
	}
	defer func() {
		currentExecutablePath = savedExecutable
	}()

	entry, err := launchSudoersEntry("dr")
	if err != nil {
		t.Fatalf("launchSudoersEntry(): %v", err)
	}
	if !strings.Contains(entry, "sha256:") {
		t.Fatalf("launchSudoersEntry() = %q, want sha256 digest", entry)
	}
	if !strings.Contains(entry, helperPath) {
		t.Fatalf("launchSudoersEntry() = %q, want helper path %q", entry, helperPath)
	}
}
