package hazmat

import (
	"errors"
	"strings"
	"testing"

	"hazmat/internal/setup"
)

func TestValidateAgentEnvContentAcceptsManagedTemplate(t *testing.T) {
	if err := validateAgentEnvContent([]byte(setup.AgentEnvContent(defaultAgentPath))); err != nil {
		t.Fatalf("validateAgentEnvContent() = %v, want nil", err)
	}
}

func TestValidateAgentEnvContentRejectsStaleTemplate(t *testing.T) {
	stale := strings.Replace(setup.AgentEnvContent(defaultAgentPath), `export HOMEBREW_NO_AUTO_UPDATE="${HOMEBREW_NO_AUTO_UPDATE:-1}"`+"\n", "", 1)
	err := validateAgentEnvContent([]byte(stale))
	if err == nil || !strings.Contains(err.Error(), "content drifted") {
		t.Fatalf("validateAgentEnvContent() = %v, want content drift error", err)
	}
}

func TestValidateAgentEnvFileSurfacesReadFailure(t *testing.T) {
	errBoom := errors.New("agent read failed")
	err := validateAgentEnvFile(func(string) ([]byte, error) {
		return nil, errBoom
	})
	if err == nil || !strings.Contains(err.Error(), "agent env file missing or unreadable") || !errors.Is(err, errBoom) {
		t.Fatalf("validateAgentEnvFile() = %v, want wrapped read failure", err)
	}
}

func TestValidateAgentEnvFileAcceptsNewlineTerminatedRead(t *testing.T) {
	err := validateAgentEnvFile(func(path string) ([]byte, error) {
		if path != agentEnvPath {
			t.Fatalf("read path = %q, want %q", path, agentEnvPath)
		}
		return []byte(setup.AgentEnvContent(defaultAgentPath)), nil
	})
	if err != nil {
		t.Fatalf("validateAgentEnvFile() = %v, want nil", err)
	}
}
