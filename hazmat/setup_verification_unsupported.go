//go:build !darwin

package hazmat

import (
	"fmt"
	"runtime"
)

type unsupportedSetupVerificationBackend struct{}

func newSetupVerificationBackend() setupVerificationBackend {
	return unsupportedSetupVerificationBackend{}
}

func (unsupportedSetupVerificationBackend) verifyAgentUser(ui *UI) {
	verifyUnsupportedSetupFinding(ui, findingSetupAgentUser)
}

func (unsupportedSetupVerificationBackend) verifyAgentHome(ui *UI) {
	verifyUnsupportedSetupFinding(ui, findingSetupAgentHome)
}

func (unsupportedSetupVerificationBackend) verifyHomeDirTraverse(ui *UI) {
	verifyUnsupportedSetupFinding(ui, findingSetupHomeTraverse)
}

func (unsupportedSetupVerificationBackend) verifyPfAnchorLoaded(ui *UI) {
	verifyUnsupportedSetupFinding(ui, findingPFFirewall)
}

func (unsupportedSetupVerificationBackend) verifyPfEnabled(ui *UI) {
	verifyUnsupportedSetupFinding(ui, findingPFFirewall)
}

func (unsupportedSetupVerificationBackend) verifySudoers(ui *UI) {
	verifyUnsupportedSetupFinding(ui, findingSetupSudoers)
}

func (unsupportedSetupVerificationBackend) verifyDNSBlocklist(ui *UI) {
	verifyUnsupportedSetupFinding(ui, findingDNSBlocklist)
}

func (unsupportedSetupVerificationBackend) verifySeatbeltWrapper(ui *UI) {
	verifyUnsupportedSetupFinding(ui, findingSetupSeatbeltWrapper)
}

func (unsupportedSetupVerificationBackend) verifyAgentEnv(ui *UI) {
	verifyUnsupportedSetupFinding(ui, findingSetupAgentEnv)
}

func (unsupportedSetupVerificationBackend) verifyHostWrappers(ui *UI) {
	verifyUnsupportedSetupFinding(ui, findingSetupHostWrappers)
}

func verifyUnsupportedSetupFinding(ui *UI, finding diagnosticFindingID) {
	ui.TestFailFinding(
		diagnosticFinding(finding),
		fmt.Sprintf("native setup verification for %s is not implemented on %s", finding, runtime.GOOS),
	)
}
