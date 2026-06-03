package hazmat

import (
	"fmt"
	"hazmat/internal/setup"
	"os/user"

	"github.com/spf13/cobra"
)

func currentUsername() (string, error) {
	cu, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}
	return cu.Username, nil
}

func launchSudoersEntry(currentUser string) (string, error) {
	return setup.LaunchSudoersEntry(sudoersEnv(), currentUser)
}

func launchSudoersInstalled() bool {
	return setup.LaunchSudoersInstalled(sudoersEnv())
}

func agentMaintenanceSudoersInstalled() bool {
	return setup.AgentMaintenanceSudoersInstalled(sudoersEnv())
}

func genericAgentPasswordlessAvailable() bool {
	return sudoNoPrompt("-u", agentUser, "whoami") == nil
}

func installLaunchSudoers(ui *UI, r *Runner, currentUser string) error {
	return setup.InstallLaunchSudoers(sudoersEnv(), ui, r, currentUser)
}

func installAgentMaintenanceSudoers(ui *UI, r *Runner, currentUser string) error {
	return setup.InstallAgentMaintenanceSudoers(sudoersEnv(), ui, r, currentUser)
}

func uninstallAgentMaintenanceSudoers(ui *UI, r *Runner) error {
	return setup.UninstallAgentMaintenanceSudoers(sudoersEnv(), ui, r)
}

func agentMaintenanceSudoersDefaultChoice(ui *UI) string {
	if ui != nil && ui.YesAll {
		return "install"
	}
	return "skip"
}

func maybeSetupOptionalAgentMaintenanceSudoers(ui *UI, r *Runner, currentUser string) error {
	return nativeServiceBackendForHost().MaybeSetupOptionalAgentMaintenanceSudoers(ui, r, currentUser)
}

func sudoersEnv() setup.SudoersEnv {
	return setup.SudoersEnv{
		AgentUser:                   agentUser,
		SudoersFile:                 sudoersFile,
		AgentMaintenanceSudoersFile: agentMaintenanceSudoersFile,
		LaunchHelperPath:            launchHelperPath,
		LaunchHelperUsesDigest:      launchHelperUsesDigest,
		RemoveInvalidFile: func(path string) error {
			return sudo("rm", "-f", path)
		},
	}
}

func newConfigSudoersCmd() *cobra.Command {
	var enableAgentMaintenance bool
	var disableAgentMaintenance bool

	cmd := &cobra.Command{
		Use:   "sudoers",
		Short: "Show or manage Hazmat's sudoers rules",
		Long: `Shows Hazmat's current sudoers state.

Hazmat always needs the narrow launch-helper rule for session starts.
You can also opt into a broader passwordless rule for generic
'sudo -u agent ...' maintenance commands if you prefer fewer password
prompts during manual agent-user work outside Hazmat's helper path.

Examples:
  hazmat config sudoers
  hazmat config sudoers --enable-agent-maintenance
  hazmat config sudoers --disable-agent-maintenance`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigSudoers(enableAgentMaintenance, disableAgentMaintenance)
		},
	}

	cmd.Flags().BoolVar(&enableAgentMaintenance, "enable-agent-maintenance", false,
		"Install the optional passwordless sudoers rule for manual generic 'sudo -u agent ...' commands")
	cmd.Flags().BoolVar(&disableAgentMaintenance, "disable-agent-maintenance", false,
		"Remove the optional passwordless sudoers rule for manual generic 'sudo -u agent ...' commands")
	return cmd
}

func runConfigSudoers(enableAgentMaintenance, disableAgentMaintenance bool) error {
	if enableAgentMaintenance && disableAgentMaintenance {
		return fmt.Errorf("choose only one of --enable-agent-maintenance or --disable-agent-maintenance")
	}

	ui := &UI{DryRun: flagDryRun, YesAll: flagYesAll}
	r := NewRunner(ui, flagVerbose, flagDryRun)

	if enableAgentMaintenance {
		currentUser, err := currentUsername()
		if err != nil {
			return err
		}
		ui.Step("Enable optional passwordless sudo for agent maintenance")
		return nativeServiceBackendForHost().InstallAgentMaintenanceSudoers(ui, r, currentUser)
	}

	if disableAgentMaintenance {
		ui.Step("Disable optional passwordless sudo for agent maintenance")
		return nativeServiceBackendForHost().UninstallAgentMaintenanceSudoers(ui, r)
	}

	backend := nativeServiceBackendForHost()
	fmt.Println()
	cBold.Println("  Passwordless sudo")
	fmt.Println()
	if backend.LaunchSudoersInstalled() {
		fmt.Printf("    Launch helper:        installed (%s)\n", sudoersFile)
	} else {
		fmt.Printf("    Launch helper:        missing (%s)\n", sudoersFile)
	}
	if backend.AgentMaintenanceSudoersInstalled() {
		fmt.Printf("    Agent maintenance:    enabled (%s)\n", agentMaintenanceSudoersFile)
	} else {
		fmt.Printf("    Agent maintenance:    disabled\n")
	}
	fmt.Printf("    sudo -u %s no prompt: %v\n", agentUser, backend.GenericAgentPasswordlessAvailable())
	fmt.Println()
	if !backend.AgentMaintenanceSudoersInstalled() {
		fmt.Println("  Enable with: hazmat config sudoers --enable-agent-maintenance")
		fmt.Println()
	}
	return nil
}
