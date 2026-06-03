package hazmat

import (
	"fmt"
	"hazmat/internal/setup"
)

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

type setupVerificationStep = setup.VerificationStep

func setupVerificationSteps() []setupVerificationStep {
	return setup.VerificationSteps(setup.VerificationCallbacks{})
}

func runSetupVerificationSteps(ctx setupVerificationContext) error {
	return setup.RunVerificationSteps(setup.VerificationCallbacks{
		AgentUser: func() {
			ctx.verifier.verifyAgentUser(ctx.ui)
		},
		AgentHome: func() {
			ctx.verifier.verifyAgentHome(ctx.ui)
		},
		HomeDirTraverse: func() {
			ctx.verifier.verifyHomeDirTraverse(ctx.ui)
		},
		PfAnchorLoaded: func() {
			ctx.verifier.verifyPfAnchorLoaded(ctx.ui)
		},
		PfEnabled: func() {
			ctx.verifier.verifyPfEnabled(ctx.ui)
		},
		Sudoers: func() {
			ctx.verifier.verifySudoers(ctx.ui)
		},
		DNSBlocklist: func() {
			ctx.verifier.verifyDNSBlocklist(ctx.ui)
		},
		SeatbeltWrapper: func() {
			ctx.verifier.verifySeatbeltWrapper(ctx.ui)
		},
		AgentEnv: func() {
			ctx.verifier.verifyAgentEnv(ctx.ui)
		},
		HostWrappers: func() {
			ctx.verifier.verifyHostWrappers(ctx.ui)
		},
	})
}

// verifySetup re-checks key invariants after all steps complete.
// All operations here are read-only; no Runner needed.
// Uses TestPass/TestFail/TestWarn so callers (hazmat status) can check
// ui.Fail > 0 and return a non-zero exit code when the sandbox is broken.
func verifySetup(ui *UI) {
	ui.Step("Verify setup")
	fmt.Println()

	if err := runSetupVerificationSteps(setupVerificationContext{
		ui:       ui,
		verifier: newSetupVerificationBackend(),
	}); err != nil {
		ui.TestFail(fmt.Sprintf("setup verification misconfigured: %v", err))
	}
}
