package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

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
	helperPath := launchHelperPath()
	commandSpec := helperPath

	if launchHelperUsesDigest(helperPath) {
		data, err := os.ReadFile(helperPath)
		if err != nil {
			return "", fmt.Errorf("read %s for sudoers digest: %w", helperPath, err)
		}
		sum := sha256.Sum256(data)
		commandSpec = fmt.Sprintf("sha256:%s %s", hex.EncodeToString(sum[:]), helperPath)
	}

	return fmt.Sprintf("%s ALL=(%s) NOPASSWD: %s\n", currentUser, agentUser, commandSpec), nil
}

func agentMaintenanceSudoersEntry(currentUser string) string {
	return strings.Join([]string{
		"# Optional hazmat agent-maintenance passwordless rule.",
		"# Broader than the default launch-helper rule: allows the current user",
		"# to run arbitrary commands as the agent user without a password.",
		fmt.Sprintf("%s ALL=(%s) NOPASSWD: ALL", currentUser, agentUser),
		"",
	}, "\n")
}

func launchSudoersInstalled() bool {
	_, err := os.Stat(sudoersFile)
	return err == nil
}

func agentMaintenanceSudoersInstalled() bool {
	_, err := os.Stat(agentMaintenanceSudoersFile)
	return err == nil
}

func genericAgentPasswordlessAvailable() bool {
	return sudoNoPrompt("-u", agentUser, "whoami") == nil
}

func writeManagedSudoersFile(r *Runner, reason, path, content string) error {
	if err := r.SudoWriteFile(reason, path, content); err != nil {
		return err
	}
	if err := r.Sudo("set "+filepath.Base(path)+" permissions", "chmod", "440", path); err != nil {
		return err
	}
	if err := r.Sudo("validate "+filepath.Base(path)+" syntax", "visudo", "-c", "-f", path); err != nil {
		sudo("rm", "-f", path) //nolint:errcheck // cleanup after failed validation
		return fmt.Errorf("sudoers syntax invalid for %s — entry removed: %w", path, err)
	}
	return nil
}

func installLaunchSudoers(ui *UI, r *Runner, currentUser string) error {
	helperPath := launchHelperPath()
	entry, err := launchSudoersEntry(currentUser)
	if err != nil {
		return err
	}
	if data, err := r.SudoOutput("cat", sudoersFile); err == nil &&
		strings.Contains(data, strings.TrimSpace(entry)) {
		ui.SkipDone(fmt.Sprintf("Sudoers entry already targets %s", helperPath))
		return nil
	} else if err == nil && strings.Contains(data, currentUser) {
		ui.WarnMsg(fmt.Sprintf("Existing sudoers entry does not target %s — replacing with narrow rule", helperPath))
	}

	if err := writeManagedSudoersFile(r,
		"write launch-helper sudoers entry for passwordless agent access",
		sudoersFile,
		entry,
	); err != nil {
		return fmt.Errorf("write launch-helper sudoers: %w", err)
	}

	ui.Ok(fmt.Sprintf("Sudoers entry written: %s can run %s as %s without password",
		currentUser, helperPath, agentUser))
	return nil
}

func installAgentMaintenanceSudoers(ui *UI, r *Runner, currentUser string) error {
	entry := agentMaintenanceSudoersEntry(currentUser)
	if data, err := r.SudoOutput("cat", agentMaintenanceSudoersFile); err == nil &&
		strings.Contains(data, "NOPASSWD: ALL") &&
		strings.Contains(data, currentUser) {
		ui.SkipDone(fmt.Sprintf("Optional agent-maintenance sudoers entry already present at %s", agentMaintenanceSudoersFile))
		return nil
	} else if err == nil {
		ui.WarnMsg(fmt.Sprintf("Existing optional agent-maintenance sudoers entry will be replaced at %s", agentMaintenanceSudoersFile))
	}

	if err := writeManagedSudoersFile(r,
		"write optional passwordless sudoers entry for generic agent-user commands",
		agentMaintenanceSudoersFile,
		entry,
	); err != nil {
		return fmt.Errorf("write optional agent-maintenance sudoers: %w", err)
	}

	ui.Ok(fmt.Sprintf("Optional passwordless sudo enabled: %s can run generic commands as %s without password",
		currentUser, agentUser))
	return nil
}

func uninstallAgentMaintenanceSudoers(ui *UI, r *Runner) error {
	if _, err := os.Stat(agentMaintenanceSudoersFile); os.IsNotExist(err) {
		ui.SkipDone("Optional agent-maintenance sudoers entry not present")
		return nil
	}
	if err := r.Sudo("remove optional agent-maintenance sudoers entry", "rm", "-f", agentMaintenanceSudoersFile); err != nil {
		return err
	}
	ui.Ok(fmt.Sprintf("Removed %s", agentMaintenanceSudoersFile))
	return nil
}

func agentMaintenanceSudoersDefaultChoice(ui *UI) string {
	if ui != nil && ui.YesAll {
		return "install"
	}
	return "skip"
}

func maybeSetupOptionalAgentMaintenanceSudoers(ui *UI, r *Runner, currentUser string) error {
	ui.Step("Optional passwordless sudo for agent maintenance")

	if agentMaintenanceSudoersInstalled() {
		ui.SkipDone(fmt.Sprintf("Optional agent-maintenance sudoers entry already present at %s", agentMaintenanceSudoersFile))
		return nil
	}

	fmt.Println("  Hazmat can also install a broader optional sudoers rule for day-to-day")
	fmt.Println("  agent maintenance commands such as bootstrap and other generic")
	fmt.Println("  'sudo -u agent ...' flows.")
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
				Description: "Lets Hazmat run generic 'sudo -u agent ...' commands without repeated password prompts.",
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
prompts during bootstrap and other agent-user flows.

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
		"Install the optional passwordless sudoers rule for generic 'sudo -u agent ...' commands")
	cmd.Flags().BoolVar(&disableAgentMaintenance, "disable-agent-maintenance", false,
		"Remove the optional passwordless sudoers rule for generic 'sudo -u agent ...' commands")
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
		return installAgentMaintenanceSudoers(ui, r, currentUser)
	}

	if disableAgentMaintenance {
		ui.Step("Disable optional passwordless sudo for agent maintenance")
		return uninstallAgentMaintenanceSudoers(ui, r)
	}

	fmt.Println()
	cBold.Println("  Passwordless sudo")
	fmt.Println()
	if launchSudoersInstalled() {
		fmt.Printf("    Launch helper:        installed (%s)\n", sudoersFile)
	} else {
		fmt.Printf("    Launch helper:        missing (%s)\n", sudoersFile)
	}
	if agentMaintenanceSudoersInstalled() {
		fmt.Printf("    Agent maintenance:    enabled (%s)\n", agentMaintenanceSudoersFile)
	} else {
		fmt.Printf("    Agent maintenance:    disabled\n")
	}
	fmt.Printf("    sudo -u %s no prompt: %v\n", agentUser, genericAgentPasswordlessAvailable())
	fmt.Println()
	if !agentMaintenanceSudoersInstalled() {
		fmt.Println("  Enable with: hazmat config sudoers --enable-agent-maintenance")
		fmt.Println()
	}
	return nil
}
