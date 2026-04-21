package main

import (
	"os/exec"
)

// newAgentSeatbeltCommand builds the narrow sudo+hazmat-launch command used to
// execute a fixed script as the agent user under a prepared native policy.
func newAgentSeatbeltCommand(cfg sessionConfig, script string, args ...string) (*exec.Cmd, func(), error) {
	policy, err := prepareNativeLaunchPolicy(cfg)
	if err != nil {
		return nil, nil, err
	}

	full := nativeLaunchSudoArgs(cfg, policy, nil, script, args...)
	return newSudoCommand(full...), policy.Cleanup, nil
}
