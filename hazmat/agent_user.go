package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
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

// agentOwnsFile returns true if the file at path is owned by the agent user
// (by uid). Returns false on any error or ownership mismatch — that's the
// safe default for callers asking "should we re-import to fix ownership?"
// because false will trigger a re-write that re-establishes correct ownership.
//
// Exposed as a var so tests can override (real test fixtures are owned by
// the test runner, not the agent, so the production check would always
// fire).
var agentOwnsFile = func(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	agentInfo, err := lookupAgentUser()
	if err != nil {
		return false
	}
	agentUID, err := strconv.ParseUint(agentInfo.Uid, 10, 32)
	if err != nil {
		return false
	}
	return uint64(stat.Uid) == agentUID
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
	if _, err := os.Stat(launchHelperPath()); err != nil {
		return fmt.Errorf("hazmat is not initialized — launch helper missing, run 'hazmat init' first")
	}
	return nil
}
