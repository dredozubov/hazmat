package hazmat

import (
	"fmt"
	"hazmat/internal/setup"
	"os"
)

type initStepContext struct {
	ui                 *UI
	runner             *Runner
	currentUser        string
	bootstrapSelection string
}

type initStep = setup.InitStep

func initSetupSteps() []initStep {
	return setup.InitSetupSteps(setup.InitCallbacks{})
}

func runInitSetupSteps(ctx initStepContext) error {
	return setup.RunInitSetupSteps(setup.InitCallbacks{
		AgentUser: func() error {
			return setupAgentUser(ctx.ui, ctx.runner)
		},
		DevGroup: func() error {
			return setupDevGroup(ctx.ui, ctx.runner, ctx.currentUser)
		},
		HomeDirTraverse: func() error {
			return setupHomeDirTraverse(ctx.ui, ctx.runner)
		},
		LocalRepo: func() error {
			return setupLocalRepo(ctx.ui)
		},
		HardeningGaps: func() error {
			return setupHardeningGaps(ctx.ui, ctx.runner)
		},
		Seatbelt: func() error {
			return setupSeatbelt(ctx.ui, ctx.runner)
		},
		Wrappers: func() error {
			if err := setupUserExperience(ctx.ui, ctx.runner); err != nil {
				return err
			}
			if err := setupZshCompletions(ctx.ui, ctx.runner); err != nil {
				return err
			}
			return setupGitSafeDirectory(ctx.ui, ctx.runner)
		},
		PfFirewall: func() error {
			return setupPfFirewall(ctx.ui, ctx.runner)
		},
		DNSBlocklist: func() error {
			return setupDNSBlocklist(ctx.ui, ctx.runner)
		},
		LaunchDaemon: func() error {
			return setupLaunchDaemon(ctx.ui, ctx.runner)
		},
		LaunchHelper: func() error {
			return setupLaunchHelper(ctx.ui, ctx.runner)
		},
		Sudoers: func() error {
			return setupSudoers(ctx.ui, ctx.runner, ctx.currentUser)
		},
		OptionalMaintenanceSudoers: func() error {
			return maybeSetupOptionalAgentMaintenanceSudoers(ctx.ui, ctx.runner, ctx.currentUser)
		},
		SelectedHarness: func() error {
			if err := runInitSelectedBootstrap(ctx.ui, ctx.runner, ctx.bootstrapSelection); err != nil {
				return err
			}
			setupAgentConfigPermissions(ctx.bootstrapSelection)
			return nil
		},
		AgentCredentials: func() error {
			setupAgentCredentials(ctx.ui, ctx.bootstrapSelection)
			return nil
		},
	})
}

func setupAgentConfigPermissions(bootstrapSelection string) {
	if flagDryRun {
		return
	}

	// Make agent config files and directories group-writable by dev so
	// hazmat commands (config agent, resume, etc.) can modify them without
	// sudo. Both dr and agent are in the dev group. Setgid on directories
	// ensures new content inherits the dev group.
	for _, path := range []string{
		agentHome + "/.zshrc",
		agentHome + "/.gitconfig",
	} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			sudo("touch", path) //nolint:errcheck // best-effort within verified init step; step-level errors handled by MC_SetupRollback
		}
		sudo("chown", agentUser+":"+sharedGroup, path) //nolint:errcheck // best-effort ownership
		sudo("chmod", "0660", path)                    //nolint:errcheck // best-effort permissions
	}
	dirs := []string{agentHome + "/.config/git"}
	if bootstrapSelection == string(HarnessClaude) {
		dirs = append(dirs, agentHome+"/.claude", agentHome+"/.claude/projects")
	}
	for _, dir := range dirs {
		sudo("mkdir", "-p", dir)                      //nolint:errcheck // best-effort within verified init step
		sudo("chown", agentUser+":"+sharedGroup, dir) //nolint:errcheck // best-effort ownership
		sudo("chmod", "2770", dir)                    //nolint:errcheck // best-effort permissions
	}
}

func setupAgentCredentials(ui *UI, bootstrapSelection string) {
	// Git identity is needed for any harness, not just Claude. Preserve the
	// existing prompt conditions so this refactor only changes structure.
	if flagDryRun || !ui.IsInteractive() || bootstrapSelection == "" {
		return
	}
	if err := runConfigAgent(ui); err != nil {
		cYellow.Printf("\n  Agent config skipped: %v\n", err)
		fmt.Println("  Run 'hazmat config agent' later to set credentials.")
	}
}
