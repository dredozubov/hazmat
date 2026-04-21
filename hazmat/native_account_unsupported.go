//go:build !darwin

package main

import (
	"fmt"
	"runtime"
)

type unsupportedNativeAccountBackend struct{}

func newNativeAccountBackend() nativeAccountBackend {
	return unsupportedNativeAccountBackend{}
}

func (unsupportedNativeAccountBackend) SetupAgentUser(*UI, *Runner) error {
	return unsupportedNativeAccountError()
}

func (unsupportedNativeAccountBackend) SetupDevGroup(*UI, *Runner, string) error {
	return unsupportedNativeAccountError()
}

func (unsupportedNativeAccountBackend) RollbackAgentUser(ui *UI, _ *Runner) {
	ui.Step(fmt.Sprintf("Delete '%s' user and home directory", agentUser))
	ui.WarnMsg(unsupportedNativeAccountError().Error())
}

func (unsupportedNativeAccountBackend) RollbackDevGroup(ui *UI, _ *Runner) {
	ui.Step(fmt.Sprintf("Delete '%s' group", sharedGroup))
	ui.WarnMsg(unsupportedNativeAccountError().Error())
}

func (unsupportedNativeAccountBackend) UIDTaken(string) (bool, error) {
	return false, unsupportedNativeAccountError()
}

func (unsupportedNativeAccountBackend) GIDTaken(string) (bool, error) {
	return false, unsupportedNativeAccountError()
}

func (unsupportedNativeAccountBackend) GroupMembershipContains(string, string) (bool, error) {
	return false, unsupportedNativeAccountError()
}

func unsupportedNativeAccountError() error {
	return fmt.Errorf("native account provisioning is not implemented on %s yet; supported platform is macOS", runtime.GOOS)
}
