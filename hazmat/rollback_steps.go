package main

import "fmt"

type rollbackStepContext struct {
	ui          *UI
	runner      *Runner
	deleteUser  bool
	deleteGroup bool
}

type rollbackStep struct {
	name        string
	tlaResource setupRollbackTLAResource
	run         func(rollbackStepContext)
}

func coreRollbackSteps() []rollbackStep {
	return []rollbackStep{
		{
			name:        "rollbackSudoers",
			tlaResource: tlaResourceSudoers,
			run: func(ctx rollbackStepContext) {
				rollbackSudoers(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "rollbackLaunchDaemon",
			tlaResource: tlaResourceLaunchDaemon,
			run: func(ctx rollbackStepContext) {
				rollbackLaunchDaemon(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "rollbackPfFirewall",
			tlaResource: tlaResourcePfAnchor,
			run: func(ctx rollbackStepContext) {
				rollbackPfFirewall(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "rollbackDNSBlocklist",
			tlaResource: tlaResourceDNSBlocklist,
			run: func(ctx rollbackStepContext) {
				rollbackDNSBlocklist(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "rollbackSeatbelt",
			tlaResource: tlaResourceSeatbelt,
			run: func(ctx rollbackStepContext) {
				rollbackSeatbelt(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "rollbackWrappers",
			tlaResource: tlaResourceWrappers,
			run: func(ctx rollbackStepContext) {
				rollbackUserExperience(ctx.ui, ctx.runner)
				rollbackZshCompletions(ctx.ui, ctx.runner)
				rollbackGitSafeDirectory(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "rollbackHomeDirTraverse",
			tlaResource: tlaResourceHomeDirTraverse,
			run: func(ctx rollbackStepContext) {
				rollbackHomeDirTraverse(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "rollbackUmask",
			tlaResource: tlaResourceUmask,
			run: func(ctx rollbackStepContext) {
				rollbackUmask(ctx.ui, ctx.runner)
			},
		},
		{
			name:        "rollbackLocalRepo",
			tlaResource: tlaResourceLocalRepo,
			run: func(ctx rollbackStepContext) {
				rollbackLocalRepo(ctx.ui)
			},
		},
	}
}

func destructiveRollbackSteps() []rollbackStep {
	return []rollbackStep{
		{
			name:        "rollbackAgentUser",
			tlaResource: tlaResourceAgentUser,
			run: func(ctx rollbackStepContext) {
				if ctx.deleteUser {
					rollbackAgentUser(ctx.ui, ctx.runner)
				} else {
					ctx.ui.WarnMsg(fmt.Sprintf("Agent user '%s' not removed. Use --delete-user to delete the account and %s.", agentUser, agentHome))
				}
			},
		},
		{
			name:        "rollbackDevGroup",
			tlaResource: tlaResourceDevGroup,
			run: func(ctx rollbackStepContext) {
				if ctx.deleteGroup {
					rollbackDevGroup(ctx.ui, ctx.runner)
				} else {
					ctx.ui.WarnMsg(fmt.Sprintf("Group '%s' not removed. Use --delete-group to delete it.", sharedGroup))
				}
			},
		},
	}
}

func runRollbackSteps(ctx rollbackStepContext) {
	for _, step := range coreRollbackSteps() {
		step.run(ctx)
	}
	for _, step := range destructiveRollbackSteps() {
		step.run(ctx)
	}
}
