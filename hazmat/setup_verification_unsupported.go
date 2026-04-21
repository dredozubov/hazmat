//go:build !darwin

package main

import (
	"fmt"
	"runtime"
)

type unsupportedSetupVerificationBackend struct{}

func newSetupVerificationBackend() setupVerificationBackend {
	return unsupportedSetupVerificationBackend{}
}

func (unsupportedSetupVerificationBackend) verifyAgentUser(ui *UI) {
	verifyUnsupportedSetupResource(ui, tlaResourceAgentUser)
}

func (unsupportedSetupVerificationBackend) verifyAgentHome(ui *UI) {
	verifyUnsupportedSetupResource(ui, tlaResourceAgentUser)
}

func (unsupportedSetupVerificationBackend) verifyHomeDirTraverse(ui *UI) {
	verifyUnsupportedSetupResource(ui, tlaResourceHomeDirTraverse)
}

func (unsupportedSetupVerificationBackend) verifyPfAnchorLoaded(ui *UI) {
	verifyUnsupportedSetupResource(ui, tlaResourcePfAnchor)
}

func (unsupportedSetupVerificationBackend) verifyPfEnabled(ui *UI) {
	verifyUnsupportedSetupResource(ui, tlaResourcePfAnchor)
}

func (unsupportedSetupVerificationBackend) verifySudoers(ui *UI) {
	verifyUnsupportedSetupResource(ui, tlaResourceSudoers)
}

func (unsupportedSetupVerificationBackend) verifyDNSBlocklist(ui *UI) {
	verifyUnsupportedSetupResource(ui, tlaResourceDNSBlocklist)
}

func (unsupportedSetupVerificationBackend) verifySeatbeltWrapper(ui *UI) {
	verifyUnsupportedSetupResource(ui, tlaResourceSeatbelt)
}

func (unsupportedSetupVerificationBackend) verifyAgentEnv(ui *UI) {
	verifyUnsupportedSetupResource(ui, tlaResourceWrappers)
}

func (unsupportedSetupVerificationBackend) verifyHostWrappers(ui *UI) {
	verifyUnsupportedSetupResource(ui, tlaResourceWrappers)
}

func verifyUnsupportedSetupResource(ui *UI, resource setupRollbackTLAResource) {
	ui.TestFail(fmt.Sprintf("native setup verification for %s is not implemented on %s", resource, runtime.GOOS))
}
