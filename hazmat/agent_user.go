package hazmat

import (
	"fmt"
	"os"
	"os/user"
)

var lookupAgentUser = func() (*user.User, error) {
	return user.Lookup(agentUser)
}

var statSetupArtifact = os.Stat

const initBaselineAdvice = "fresh host: run 'hazmat init' first"
const initDriftRepairAdvice = "setup drift: run 'hazmat doctor --fix' (preview: 'hazmat doctor --dry-run')"

func requireAgentUser() (*user.User, error) {
	agentInfo, err := lookupAgentUser()
	if err != nil {
		return nil, fmt.Errorf("agent user %q not found — %s", agentUser, initBaselineAdvice)
	}
	return agentInfo, nil
}

// requireInit verifies that hazmat init has been completed before allowing
// a session to start. Checks the three essential artifacts: agent user,
// sudoers rule (for passwordless hazmat-launch), and launch helper binary.
var requireInit = func() error {
	if _, err := lookupAgentUser(); err != nil {
		return fmt.Errorf("hazmat is not initialized — %s", initBaselineAdvice)
	}
	if _, err := statSetupArtifact(sudoersFile); err != nil {
		return fmt.Errorf("hazmat setup drift detected — sudoers rule missing; %s", initDriftRepairAdvice)
	}
	if _, err := statSetupArtifact(launchHelperPath()); err != nil {
		return fmt.Errorf("hazmat setup drift detected — launch helper missing; %s", initDriftRepairAdvice)
	}
	return nil
}
