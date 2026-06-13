package hazmat

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReadAgentSecretFileTreatsInaccessibleMissingPathAsAbsent(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.Mkdir(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(blocked, 0o700)
	})

	savedPathForDirectIO := agentPathForDirectIO
	agentPathForDirectIO = func(string) string {
		return filepath.Join(blocked, "missing.json")
	}
	t.Cleanup(func() { agentPathForDirectIO = savedPathForDirectIO })

	savedNewAgentCommand := newAgentCommand
	newAgentCommand = func(args ...string) *exec.Cmd {
		switch args[0] {
		case "cat":
			return exec.Command("sh", "-c", "exit 1")
		case "test":
			return exec.Command("sh", "-c", "exit 1")
		default:
			t.Fatalf("unexpected agent command: %v", args)
			return exec.Command("sh", "-c", "exit 1")
		}
	}
	t.Cleanup(func() { newAgentCommand = savedNewAgentCommand })

	raw, ok, err := readAgentSecretFile(agentHome + "/.codex/auth.json")
	if err != nil {
		t.Fatalf("readAgentSecretFile: %v", err)
	}
	if ok || raw != nil {
		t.Fatalf("readAgentSecretFile = %q, %v; want absent", raw, ok)
	}
}

func TestSessionHomeRuntimeSecretPathsRequireAgentIO(t *testing.T) {
	sessionPath := filepath.Join(defaultSessionHomeRoot, "session-123", "home", ".codex", "auth.json")
	if !requiresAgentSecretFileIO(sessionPath) {
		t.Fatalf("requiresAgentSecretFileIO(%q) = false, want true", sessionPath)
	}

	outside := filepath.Join(t.TempDir(), "hazmat-home", "session-123", "home", ".codex", "auth.json")
	if requiresAgentSecretFileIO(outside) {
		t.Fatalf("requiresAgentSecretFileIO(%q) = true, want false for non-default temp path", outside)
	}
}

func TestSessionHomeRuntimeSecretPathsRejectUnsafeSessionID(t *testing.T) {
	path := filepath.Join(defaultSessionHomeRoot, ".hidden", "home", ".codex", "auth.json")
	if requiresAgentSecretFileIO(path) {
		t.Fatalf("requiresAgentSecretFileIO(%q) = true, want false", path)
	}
}

func TestReadAgentSecretFileKeepsAgentTestInfrastructureFailuresFatal(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.Mkdir(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(blocked, 0o700)
	})

	savedPathForDirectIO := agentPathForDirectIO
	agentPathForDirectIO = func(string) string {
		return filepath.Join(blocked, "auth.json")
	}
	t.Cleanup(func() { agentPathForDirectIO = savedPathForDirectIO })

	savedNewAgentCommand := newAgentCommand
	newAgentCommand = func(args ...string) *exec.Cmd {
		switch args[0] {
		case "cat":
			return exec.Command("sh", "-c", "exit 1")
		case "test":
			return exec.Command("sh", "-c", "exit 126")
		default:
			t.Fatalf("unexpected agent command: %v", args)
			return exec.Command("sh", "-c", "exit 1")
		}
	}
	t.Cleanup(func() { newAgentCommand = savedNewAgentCommand })

	if _, _, err := readAgentSecretFile(agentHome + "/.codex/auth.json"); err == nil {
		t.Fatal("readAgentSecretFile = nil error, want fatal agent command failure")
	}
}
