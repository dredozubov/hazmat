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
	if out, err := sudoOutput("pfctl", "-a", pfAnchorName, "-sr"); err == nil &&
		strings.Contains(out, "block") {
		n := len(strings.Split(strings.TrimSpace(out), "\n"))
		ui.TestPass(fmt.Sprintf("pf anchor loaded with %d rules", n))
	} else {
		ui.TestFailFinding(diagnosticFinding(findingPFFirewall), "pf anchor not loaded or empty")
	}
}

func (darwinSetupVerificationBackend) verifyPfEnabled(ui *UI) {
	if out, err := sudoOutput("pfctl", "-si"); err == nil &&
		strings.Contains(out, "Status: Enabled") {
		ui.TestPass("pf is enabled")
	} else {
		ui.TestFailFinding(diagnosticFinding(findingPFFirewall), "pf is not enabled")
	}
}

func (darwinSetupVerificationBackend) verifySudoers(ui *UI) {
	if err := sudo("-u", agentUser, "whoami"); err == nil {
		ui.TestPass(fmt.Sprintf("Passwordless sudo works (%s → %s)", os.Getenv("USER"), agentUser))
	} else {
		ui.TestFailFinding(diagnosticFinding(findingSetupSudoers), "Passwordless sudo not working")
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
	if info, err := os.Stat(seatbeltWrapperPath); err == nil && info.Mode()&0o111 != 0 {
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
	for _, wrapper := range []string{hostClaudeWrapperName, hostExecWrapperName, hostShellWrapperName} {
		path := hostWrapperPath(wrapper)
		if info, err := os.Stat(path); err == nil && info.Mode()&0o111 != 0 {
			ui.TestPass(fmt.Sprintf("Host wrapper installed: %s", path))
		} else {
			ui.TestFailFinding(
				diagnosticFinding(findingSetupHostWrappers),
				fmt.Sprintf("Host wrapper missing or not executable: %s", path),
				path,
			)
		}
	}
}
