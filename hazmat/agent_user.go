package main

import (
	"fmt"
	"os"
	"os/user"
)

var lookupAgentUser = func() (*user.User, error) {
	return user.Lookup(agentUser)
}

func requireAgentUser() (*user.User, error) {
	agentInfo, err := lookupAgentUser()
	if err != nil {
		return nil, fmt.Errorf("agent user %q not found — run 'hazmat init' first", agentUser)
	}
	return agentInfo, nil
}

// requireInit verifies that hazmat init has been completed before allowing
// a session to start. Checks the three essential artifacts: agent user,
// sudoers rule (for passwordless hazmat-launch), and launch helper binary.
var requireInit = func() error {
	if _, err := lookupAgentUser(); err != nil {
		return fmt.Errorf("hazmat is not initialized — run 'hazmat init' first")
	}
	if _, err := os.Stat(sudoersFile); err != nil {
		return fmt.Errorf("hazmat is not initialized — sudoers rule missing, run 'hazmat init' first")
	}
	if _, err := os.Stat(launchHelper); err != nil {
		return fmt.Errorf("hazmat is not initialized — launch helper missing, run 'hazmat init' first")
	}
	return nil
}
