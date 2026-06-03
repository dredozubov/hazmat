//go:build darwin

package hazmat

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	setupdarwin "hazmat/internal/setup/darwin"
)

type darwinNativeServiceBackend struct{}

func newNativeServiceBackend() nativeServiceBackend {
	return darwinNativeServiceBackend{}
}

func (b darwinNativeServiceBackend) service() setupdarwin.ServiceBackend {
	return setupdarwin.NewServiceBackend(setupdarwin.ServiceEnv{
		AgentUser:                   agentUser,
		PfAnchorFile:                pfAnchorFile,
		PfDaemonPlist:               pfDaemonPlist,
		SudoersFile:                 sudoersFile,
		AgentMaintenanceSudoersFile: agentMaintenanceSudoersFile,
		HostsMarker:                 hostsMarker,
		SystemLaunchHelper:          systemLaunchHelper,
		HostPfctlPath:               hostPfctlPath,
		HostLaunchctlPath:           hostLaunchctlPath,
		LaunchHelperPath:            launchHelperPath,
		BrewLaunchHelperPaths: []string{
			"/opt/homebrew/opt/hazmat/libexec/hazmat-launch",
			"/usr/local/opt/hazmat/libexec/hazmat-launch",
		},
	})
}

func (b darwinNativeServiceBackend) SetupLaunchHelper(ui *UI, r *Runner) error {
	return b.service().SetupLaunchHelper(ui, r, func(helperPath string) error {
		return ensureAgentCanTraverseLaunchHelper(r, helperPath)
	})
}

func (b darwinNativeServiceBackend) SetupSudoers(ui *UI, r *Runner, currentUser string) error {
	return b.service().SetupSudoers(ui, currentUser, agentUser, func() error {
		return installLaunchSudoers(ui, r, currentUser)
	})
}

func (darwinNativeServiceBackend) MaybeSetupOptionalAgentMaintenanceSudoers(ui *UI, r *Runner, currentUser string) error {
	ui.Step("Optional passwordless sudo for agent maintenance")

	if agentMaintenanceSudoersInstalled() {
		ui.SkipDone(fmt.Sprintf("Optional agent-maintenance sudoers entry already present at %s", agentMaintenanceSudoersFile))
		return nil
	}

	fmt.Println("  Hazmat can also install a broader optional sudoers rule for day-to-day")
	fmt.Println("  manual agent-user commands that bypass Hazmat's helper-backed")
	fmt.Println("  maintenance path and use raw 'sudo -u agent ...' instead.")
	fmt.Println()
	cDim.Println("  This is broader than the default launch-helper rule.")
	cDim.Println("  Only enable it if you want to stop repeated password prompts for")
	cDim.Println("  generic agent-user commands.")
	cDim.Println("  Interactive init leaves this opt-in; 'hazmat init --yes' installs")
	cDim.Println("  it by default for smoother non-interactive use.")
	fmt.Println()

	defaultChoice := agentMaintenanceSudoersDefaultChoice(ui)
	choice, err := ui.Choose(
		"Optional agent-maintenance passwordless sudo:",
		[]UIChoice{
			{
				Key:         "install",
				Label:       "Install opt-in rule",
				Description: "Lets you run raw generic 'sudo -u agent ...' commands without repeated password prompts.",
			},
			{
				Key:         "skip",
				Label:       "Keep narrow default",
				Description: "Leaves generic agent-user commands on normal sudo prompts.",
			},
		},
		defaultChoice,
	)
	if err != nil {
		return err
	}
	if choice != "install" {
		ui.WarnMsg("Leaving generic agent-user commands on standard sudo prompts")
		return nil
	}

	return installAgentMaintenanceSudoers(ui, r, currentUser)
}

func (b darwinNativeServiceBackend) SetupPfFirewall(ui *UI, r *Runner) error {
	return b.service().SetupPfFirewall(ui, r)
}

func (b darwinNativeServiceBackend) SetupDNSBlocklist(ui *UI, r *Runner) error {
	return b.service().SetupDNSBlocklist(ui, r)
}

func (b darwinNativeServiceBackend) SetupLaunchDaemon(ui *UI, r *Runner) error {
	return b.service().SetupLaunchDaemon(ui, r)
}

func (b darwinNativeServiceBackend) RollbackLaunchDaemon(ui *UI, r *Runner) {
	b.service().RollbackLaunchDaemon(ui, r)
}

func (b darwinNativeServiceBackend) RollbackPfFirewall(ui *UI, r *Runner) {
	b.service().RollbackPfFirewall(ui, r)
}

func (b darwinNativeServiceBackend) RollbackDNSBlocklist(ui *UI, r *Runner) {
	b.service().RollbackDNSBlocklist(ui, r)
}

func (b darwinNativeServiceBackend) RollbackSudoers(ui *UI, r *Runner) {
	b.service().RollbackSudoers(ui, r)
}

func (darwinNativeServiceBackend) InstallAgentMaintenanceSudoers(ui *UI, r *Runner, currentUser string) error {
	return installAgentMaintenanceSudoers(ui, r, currentUser)
}

func (darwinNativeServiceBackend) UninstallAgentMaintenanceSudoers(ui *UI, r *Runner) error {
	return uninstallAgentMaintenanceSudoers(ui, r)
}

func (darwinNativeServiceBackend) LaunchSudoersInstalled() bool {
	_, err := os.Stat(sudoersFile)
	return err == nil
}

func (darwinNativeServiceBackend) AgentMaintenanceSudoersInstalled() bool {
	_, err := os.Stat(agentMaintenanceSudoersFile)
	return err == nil
}

func (darwinNativeServiceBackend) GenericAgentPasswordlessAvailable() bool {
	return sudoNoPrompt("-u", agentUser, "whoami") == nil
}

func (b darwinNativeServiceBackend) FindBrewLaunchHelper() string {
	return b.service().FindBrewLaunchHelper()
}

// pfctlLoadRules runs "sudo pfctl -f /etc/pf.conf", capturing stderr so
// parse errors are surfaced rather than silently swallowed.
func pfctlLoadRules() error {
	cmd := newSudoCommand(hostPfctlPath, "-f", "/etc/pf.conf")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// launchctlBootstrap runs "sudo launchctl bootstrap system <plist>".
// Treats "already loaded" as success so the step stays idempotent.
func launchctlBootstrap(plist string) error {
	cmd := newSudoCommand(hostLaunchctlPath, "bootstrap", "system", plist)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		// "Bootstrap failed: 5: Input/output error" = service already loaded
		if strings.Contains(msg, "Bootstrap failed: 5") ||
			strings.Contains(msg, "already loaded") ||
			strings.Contains(msg, "service already exists") {
			return nil
		}
		return fmt.Errorf("launchctl bootstrap: %s", strings.TrimSpace(msg))
	}
	return nil
}
