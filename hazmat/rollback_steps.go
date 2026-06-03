package hazmat

import "hazmat/internal/setup"

type rollbackStepContext struct {
	ui          *UI
	runner      *Runner
	deleteUser  bool
	deleteGroup bool
}

type rollbackStep = setup.RollbackStep

func coreRollbackSteps() []rollbackStep {
	return setup.CoreRollbackSteps(setup.RollbackCallbacks{})
}

func destructiveRollbackSteps() []rollbackStep {
	return setup.DestructiveRollbackSteps(setup.RollbackCallbacks{}, rollbackOptions(rollbackStepContext{}))
}

func runRollbackSteps(ctx rollbackStepContext) error {
	return setup.RunRollbackSteps(rollbackCallbacks(ctx), rollbackOptions(ctx))
}

func rollbackCallbacks(ctx rollbackStepContext) setup.RollbackCallbacks {
	return setup.RollbackCallbacks{
		Sudoers: func() error {
			rollbackSudoers(ctx.ui, ctx.runner)
			return nil
		},
		LaunchDaemon: func() error {
			rollbackLaunchDaemon(ctx.ui, ctx.runner)
			return nil
		},
		PfFirewall: func() error {
			rollbackPfFirewall(ctx.ui, ctx.runner)
			return nil
		},
		DNSBlocklist: func() error {
			rollbackDNSBlocklist(ctx.ui, ctx.runner)
			return nil
		},
		Seatbelt: func() error {
			rollbackSeatbelt(ctx.ui, ctx.runner)
			return nil
		},
		Wrappers: func() error {
			rollbackUserExperience(ctx.ui, ctx.runner)
			rollbackZshCompletions(ctx.ui, ctx.runner)
			rollbackGitSafeDirectory(ctx.ui, ctx.runner)
			return nil
		},
		HomeDirTraverse: func() error {
			rollbackHomeDirTraverse(ctx.ui, ctx.runner)
			return nil
		},
		Umask: func() error {
			rollbackUmask(ctx.ui, ctx.runner)
			return nil
		},
		LocalRepo: func() error {
			rollbackLocalRepo(ctx.ui)
			return nil
		},
		AgentUser: func() error {
			rollbackAgentUser(ctx.ui, ctx.runner)
			return nil
		},
		DevGroup: func() error {
			rollbackDevGroup(ctx.ui, ctx.runner)
			return nil
		},
	}
}

func rollbackOptions(ctx rollbackStepContext) setup.RollbackOptions {
	return setup.RollbackOptions{
		DeleteUser:    ctx.deleteUser,
		DeleteGroup:   ctx.deleteGroup,
		AgentUserName: agentUser,
		AgentHome:     agentHome,
		GroupName:     sharedGroup,
		Warn: func(message string) {
			if ctx.ui != nil {
				ctx.ui.WarnMsg(message)
			}
		},
	}
}
