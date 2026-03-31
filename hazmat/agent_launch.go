package main

import (
	"fmt"
	"os"
	"os/exec"
)

// newAgentSeatbeltCommand builds the narrow sudo+hazmat-launch command used to
// execute a fixed script as the agent user under a generated SBPL policy.
func newAgentSeatbeltCommand(cfg sessionConfig, script string, args ...string) (*exec.Cmd, func(), error) {
	pid := os.Getpid()

	policy := generateSBPL(cfg)
	policyFile := fmt.Sprintf("/private/tmp/hazmat-%d.sb", pid)
	if err := os.WriteFile(policyFile, []byte(policy), 0o644); err != nil {
		return nil, nil, fmt.Errorf("write seatbelt policy: %w", err)
	}
	if err := os.Chmod(policyFile, 0o644); err != nil {
		_ = os.Remove(policyFile)
		return nil, nil, fmt.Errorf("set seatbelt policy mode: %w", err)
	}

	full := []string{
		"-u", agentUser,
		launchHelper, policyFile,
		"/usr/bin/env", "-i",
	}
	full = append(full, agentEnvPairs(cfg)...)
	full = append(full, "/bin/zsh", "-lc", script, "zsh")
	full = append(full, args...)

	cleanup := func() {
		_ = os.Remove(policyFile)
	}

	return exec.Command("sudo", full...), cleanup, nil
}
