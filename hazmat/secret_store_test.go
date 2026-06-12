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
