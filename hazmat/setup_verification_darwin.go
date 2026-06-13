//go:build darwin

package hazmat

import (
	"fmt"
	"os"
	"os/user"
	"strings"
)

type darwinSetupVerificationBackend struct{}

func newSetupVerificationBackend() setupVerificationBackend {
	return darwinSetupVerificationBackend{}
}

func (darwinSetupVerificationBackend) verifyAgentUser(ui *UI) {
	if u, err := user.Lookup(agentUser); err == nil {
		ui.TestPass(fmt.Sprintf("User '%s' exists (uid=%s)", agentUser, u.Uid))
	} else {
		ui.TestFailFinding(diagnosticFinding(findingSetupAgentUser), fmt.Sprintf("User '%s' not found", agentUser))
	}
}

func (darwinSetupVerificationBackend) verifyAgentHome(ui *UI) {
	if _, err := os.Stat(agentHome); err == nil {
		ui.TestPass(fmt.Sprintf("Home directory exists at %s", agentHome))
	} else {
		ui.TestFailFinding(diagnosticFinding(findingSetupAgentHome), fmt.Sprintf("Home directory missing: %s", agentHome))
	}
}

func (darwinSetupVerificationBackend) verifyHomeDirTraverse(ui *UI) {
	homeDir := os.Getenv("HOME")
	if homeAllowsAgentTraverse(homeDir) {
		ui.TestPass(fmt.Sprintf("Home directory ACL lets '%s' traverse to project directories", agentUser))
	} else {
		ui.TestWarnFinding(
			diagnosticFinding(findingSetupHomeTraverse),
			fmt.Sprintf("Home directory access for '%s' not detected — project directories may be inaccessible", agentUser),
			homeDir,
		)
	}
}

func (darwinSetupVerificationBackend) verifyPfAnchorLoaded(ui *UI) {
	if out, err := readPfAnchorFileRules(); err == nil && strings.Contains(out, "block") {
		n := len(strings.Split(strings.TrimSpace(out), "\n"))
		ui.TestPass(fmt.Sprintf("pf anchor file contains %d rules", n))
	} else {
		ui.TestFailFinding(diagnosticFinding(findingPFFirewall), "pf anchor file missing or empty")
	}
}

func (darwinSetupVerificationBackend) verifyPfEnabled(ui *UI) {
	if enabled, observed := pfRuntimeEnabledUnprivileged(); observed {
		if enabled {
			ui.TestPass("pf is enabled")
		} else {
			ui.TestFailFinding(diagnosticFinding(findingPFFirewall), "pf is not enabled")
		}
	} else {
		ui.TestSkip("pf runtime status is not available to read-only setup verification without privileged inspection")
	}
}

func (darwinSetupVerificationBackend) verifySudoers(ui *UI) {
	if launchSudoersInstalled() {
		ui.TestPass(fmt.Sprintf("Launch-helper sudoers file exists: %s", sudoersFile))
	} else {
		ui.TestFailFinding(diagnosticFinding(findingSetupSudoers), fmt.Sprintf("Launch-helper sudoers file missing: %s", sudoersFile))
	}
	helperPath := launchHelperPath()
	if info, err := os.Stat(helperPath); err == nil && info.Mode()&0o111 != 0 {
		ui.TestPass(fmt.Sprintf("Hazmat helper is installed and executable: %s", helperPath))
	} else {
		ui.TestFailFinding(diagnosticFinding(findingSetupSudoers), fmt.Sprintf("Hazmat helper missing or not executable: %s", helperPath))
	}
}

func (darwinSetupVerificationBackend) verifyDNSBlocklist(ui *UI) {
	// DNS blocklist is optional — was a prompted step during setup.
	if data, err := os.ReadFile("/etc/hosts"); err == nil &&
		strings.Contains(string(data), "AI Agent Blocklist") {
		n := strings.Count(string(data), "0.0.0.0 ")
		ui.TestPass(fmt.Sprintf("DNS blocklist active (%d domains in /etc/hosts)", n))
	} else {
		ui.TestWarnFinding(diagnosticFinding(findingDNSBlocklist), "DNS blocklist not installed in /etc/hosts")
	}
}

func (darwinSetupVerificationBackend) verifySeatbeltWrapper(ui *UI) {
	if executable, err := agentPathIsExecutable(seatbeltWrapperPath); err == nil && executable {
		ui.TestPass(fmt.Sprintf("Seatbelt wrapper installed and executable at %s", seatbeltWrapperPath))
	} else {
		ui.TestFailFinding(
			diagnosticFinding(findingSetupSeatbeltWrapper),
			fmt.Sprintf("Seatbelt wrapper missing or not executable: %s", seatbeltWrapperPath),
			seatbeltWrapperPath,
		)
	}
}

func (darwinSetupVerificationBackend) verifyAgentEnv(ui *UI) {
	// Agent shell env is advisory — wrappers work without it but PATH and
	// aliases inside agent-shell will be incomplete.
	if _, err := os.Stat(agentEnvPath); err == nil {
		ui.TestPass(fmt.Sprintf("Agent shell env installed at %s", agentEnvPath))
	} else {
		ui.TestWarnFinding(
			diagnosticFinding(findingSetupAgentEnv),
			fmt.Sprintf("Agent shell env missing: %s", agentEnvPath),
			agentEnvPath,
		)
	}
}

func (darwinSetupVerificationBackend) verifyHostWrappers(ui *UI) {
	for _, wrapper := range managedHostWrapperSpecs() {
		path := hostWrapperPath(wrapper.Name)
		if err := validateHostWrapper(path, wrapper.Subcommand); err != nil {
			ui.TestFailFinding(
				diagnosticFinding(findingSetupHostWrappers),
				fmt.Sprintf("Host wrapper drift: %v", err),
				path,
			)
		} else {
			ui.TestPass(fmt.Sprintf("Host wrapper installed with executable Hazmat target: %s", path))
		}
	}
}
