package hazmat

import (
	"bytes"
	"fmt"

	"hazmat/internal/setup"
)

func validateAgentEnvContent(data []byte) error {
	want := []byte(setup.AgentEnvContent(defaultAgentPath))
	if !bytes.Equal(data, want) {
		return fmt.Errorf("agent env content drifted from Hazmat-managed template")
	}
	return nil
}

func validateAgentEnvFile(read func(string) ([]byte, error)) error {
	data, err := read(agentEnvPath)
	if err != nil {
		return fmt.Errorf("agent env file missing or unreadable: %w", err)
	}
	return validateAgentEnvContent(data)
}
