package main

import (
	"fmt"
	"os"
)

type initStepContext struct {
	ui                 *UI
	runner             *Runner
	currentUser        string
	bootstrapSelection string
}

type initStep struct {
	name        string
	tlaResource setupRollbackTLAResource
	run         func(initStepContext) error
}

func initSetupSteps() []initStep {
	return []initStep{
		{
			name:        "setupAgentUser",
			tlaResource: tlaResourceAgentUser,
			run: func(ctx initStepContext) error {
				return setupAgentUser(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "setupDevGroup",
			tlaResource: tlaResourceDevGroup,
			run: func(ctx initStepContext) error {
				return setupDevGroup(ctx.ui, ctx.runner, ctx.currentUser)
			},
		},
		{
			name:        "setupHomeDirTraverse",
			tlaResource: tlaResourceHomeDirTraverse,
			run: func(ctx initStepContext) error {
				return setupHomeDirTraverse(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "setupLocalRepo",
			tlaResource: tlaResourceLocalRepo,
			run: func(ctx initStepContext) error {
				return setupLocalRepo(ctx.ui)
			},
		},
		{
			name:        "setupHardeningGaps",
			tlaResource: tlaResourceUmask,
			run: func(ctx initStepContext) error {
				return setupHardeningGaps(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "setupSeatbelt",
			tlaResource: tlaResourceSeatbelt,
			run: func(ctx initStepContext) error {
				return setupSeatbelt(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "setupWrappers",
			tlaResource: tlaResourceWrappers,
			run: func(ctx initStepContext) error {
				if err := setupUserExperience(ctx.ui, ctx.runner); err != nil {
					return err
				}
				if err := setupZshCompletions(ctx.ui, ctx.runner); err != nil {
					return err
				}
				return setupGitSafeDirectory(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "setupPfFirewall",
			tlaResource: tlaResourcePfAnchor,
			run: func(ctx initStepContext) error {
				return setupPfFirewall(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "setupDNSBlocklist",
			tlaResource: tlaResourceDNSBlocklist,
			run: func(ctx initStepContext) error {
				return setupDNSBlocklist(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "setupLaunchDaemon",
			tlaResource: tlaResourceLaunchDaemon,
			run: func(ctx initStepContext) error {
				return setupLaunchDaemon(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "setupLaunchHelper",
			tlaResource: tlaResourceLaunchHelper,
			run: func(ctx initStepContext) error {
				return setupLaunchHelper(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "setupSudoers",
			tlaResource: tlaResourceSudoers,
			run: func(ctx initStepContext) error {
				return setupSudoers(ctx.ui, ctx.runner, ctx.currentUser)
			},
		},
		{
			name:        "maybeSetupOptionalAgentMaintenanceSudoers",
			tlaResource: tlaResourceMaintenanceSudoers,
			run: func(ctx initStepContext) error {
				return maybeSetupOptionalAgentMaintenanceSudoers(ctx.ui, ctx.runner, ctx.currentUser)
			},
		},
		{
			name:        "setupSelectedHarness",
			tlaResource: tlaResourceClaudeCode,
			run: func(ctx initStepContext) error {
				if err := runInitSelectedBootstrap(ctx.ui, ctx.runner, ctx.bootstrapSelection); err != nil {
					return err
				}
				setupAgentConfigPermissions(ctx.bootstrapSelection)
				return nil
			},
		},
		{
			name:        "setupAgentCredentials",
			tlaResource: tlaResourceCredentials,
			run: func(ctx initStepContext) error {
				setupAgentCredentials(ctx.ui, ctx.bootstrapSelection)
				return nil
			},
		},
	}
}

func runInitSetupSteps(ctx initStepContext) error {
	for _, step := range initSetupSteps() {
		if err := step.run(ctx); err != nil {
			return err
		}
	}
	return nil
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
