package hazmat

import (
	"fmt"
	"os"

	linuxsetup "hazmat/internal/setup/linux"
)

func runLinuxRollback(ui *UI, _ *Runner, deleteUser, deleteGroup bool) error {
	if err := requireLinuxDiagnosticHost("linux.rollback"); err != nil {
		return err
	}

	fmt.Println()
	cBold.Println("  ┌────────────────────────────────────────────────┐")
	cBold.Println("  │  Linux agent-user rollback — undo setup state  │")
	cBold.Println("  └────────────────────────────────────────────────┘")
	fmt.Println()

	options := linuxRollbackOptions(ui, deleteUser, deleteGroup)
	if flagDryRun {
		cYellow.Println("  ────────────────────────────────────────────────────")
		cYellow.Println("  DRY RUN — no changes will be made.")
		cYellow.Println("  ────────────────────────────────────────────────────")
		fmt.Println()
		printLinuxRollbackDryRun(ui, linuxsetup.DryRunRollback(linuxsetup.Callbacks{}, options))
		return nil
	}
	if !ui.Ask("Rollback Linux agent-user setup changes?") {
		fmt.Println("  Aborted.")
		return nil
	}

	projectDir, _ := os.Getwd()
	backend := &diagnosticHostRepairBackend{
		ui:          ui,
		currentUser: linuxCurrentUserName(""),
		projectDir:  projectDir,
	}
	if err := linuxsetup.RunRollbackSteps(linuxDiagnosticSetupCallbacks(backend, linuxDiagnosticSetupRollback), options); err != nil {
		return err
	}
	rollbackProjectHooks(ui)

	fmt.Println()
	cGreen.Println("  Linux rollback complete.")
	fmt.Println("  Your project files were not touched.")
	return nil
}

func linuxRollbackOptions(ui *UI, deleteUser, deleteGroup bool) linuxsetup.RollbackOptions {
	return linuxsetup.RollbackOptions{
		DeleteToolHome:    deleteUser,
		DeleteAgentHome:   deleteUser,
		DeleteAgentUser:   deleteUser,
		DeleteSharedGroup: deleteGroup,
		Warn: func(message string) {
			if ui != nil {
				ui.WarnMsg(message)
			}
		},
	}
}

func printLinuxRollbackDryRun(ui *UI, records []linuxsetup.DryRunRecord) {
	for _, record := range records {
		label := fmt.Sprintf("Would run %s (%s)", record.Name, record.Resource)
		if record.Destructive {
			label += " only if its destructive flag is set"
		}
		ui.Ok(label)
	}
}
