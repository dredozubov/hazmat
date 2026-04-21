package main

import "fmt"

type setupVerificationContext struct {
	ui       *UI
	verifier setupVerificationBackend
}

type setupVerificationBackend interface {
	verifyAgentUser(*UI)
	verifyAgentHome(*UI)
	verifyHomeDirTraverse(*UI)
	verifyPfAnchorLoaded(*UI)
	verifyPfEnabled(*UI)
	verifySudoers(*UI)
	verifyDNSBlocklist(*UI)
	verifySeatbeltWrapper(*UI)
	verifyAgentEnv(*UI)
	verifyHostWrappers(*UI)
}

type setupVerificationStep struct {
	name        string
	tlaResource setupRollbackTLAResource
	run         func(setupVerificationContext)
}

func setupVerificationSteps() []setupVerificationStep {
	return []setupVerificationStep{
		{
			name:        "verifyAgentUser",
			tlaResource: tlaResourceAgentUser,
			run: func(ctx setupVerificationContext) {
				ctx.verifier.verifyAgentUser(ctx.ui)
			},
		},
		{
			name:        "verifyAgentHome",
			tlaResource: tlaResourceAgentUser,
			run: func(ctx setupVerificationContext) {
				ctx.verifier.verifyAgentHome(ctx.ui)
			},
		},
		{
			name:        "verifyHomeDirTraverse",
			tlaResource: tlaResourceHomeDirTraverse,
			run: func(ctx setupVerificationContext) {
				ctx.verifier.verifyHomeDirTraverse(ctx.ui)
			},
		},
		{
			name:        "verifyPfAnchorLoaded",
			tlaResource: tlaResourcePfAnchor,
			run: func(ctx setupVerificationContext) {
				ctx.verifier.verifyPfAnchorLoaded(ctx.ui)
			},
		},
		{
			name:        "verifyPfEnabled",
			tlaResource: tlaResourcePfAnchor,
			run: func(ctx setupVerificationContext) {
				ctx.verifier.verifyPfEnabled(ctx.ui)
			},
		},
		{
			name:        "verifySudoers",
			tlaResource: tlaResourceSudoers,
			run: func(ctx setupVerificationContext) {
				ctx.verifier.verifySudoers(ctx.ui)
			},
		},
		{
			name:        "verifyDNSBlocklist",
			tlaResource: tlaResourceDNSBlocklist,
			run: func(ctx setupVerificationContext) {
				ctx.verifier.verifyDNSBlocklist(ctx.ui)
			},
		},
		{
			name:        "verifySeatbeltWrapper",
			tlaResource: tlaResourceSeatbelt,
			run: func(ctx setupVerificationContext) {
				ctx.verifier.verifySeatbeltWrapper(ctx.ui)
			},
		},
		{
			name:        "verifyAgentEnv",
			tlaResource: tlaResourceWrappers,
			run: func(ctx setupVerificationContext) {
				ctx.verifier.verifyAgentEnv(ctx.ui)
			},
		},
		{
			name:        "verifyHostWrappers",
			tlaResource: tlaResourceWrappers,
			run: func(ctx setupVerificationContext) {
				ctx.verifier.verifyHostWrappers(ctx.ui)
			},
		},
	}
}

func runSetupVerificationSteps(ctx setupVerificationContext) {
	for _, step := range setupVerificationSteps() {
		step.run(ctx)
	}
}

// verifySetup re-checks key invariants after all steps complete.
// All operations here are read-only; no Runner needed.
// Uses TestPass/TestFail/TestWarn so callers (hazmat status) can check
// ui.Fail > 0 and return a non-zero exit code when the sandbox is broken.
func verifySetup(ui *UI) {
	ui.Step("Verify setup")
	fmt.Println()

	runSetupVerificationSteps(setupVerificationContext{
		ui:       ui,
		verifier: newSetupVerificationBackend(),
	})
}
