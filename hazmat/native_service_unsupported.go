//go:build !darwin

package main

import (
	"fmt"
	"runtime"
)

type unsupportedNativeServiceBackend struct{}

func newNativeServiceBackend() nativeServiceBackend {
	return unsupportedNativeServiceBackend{}
}

func (unsupportedNativeServiceBackend) SetupLaunchHelper(*UI, *Runner) error {
	return unsupportedNativeServiceError()
}

func (unsupportedNativeServiceBackend) SetupSudoers(*UI, *Runner, string) error {
	return unsupportedNativeServiceError()
}

func (unsupportedNativeServiceBackend) MaybeSetupOptionalAgentMaintenanceSudoers(*UI, *Runner, string) error {
	return unsupportedNativeServiceError()
}

func (unsupportedNativeServiceBackend) SetupPfFirewall(*UI, *Runner) error {
	return unsupportedNativeServiceError()
}

func (unsupportedNativeServiceBackend) SetupDNSBlocklist(*UI, *Runner) error {
	return unsupportedNativeServiceError()
}

func (unsupportedNativeServiceBackend) SetupLaunchDaemon(*UI, *Runner) error {
	return unsupportedNativeServiceError()
}

func (unsupportedNativeServiceBackend) RollbackLaunchDaemon(ui *UI, _ *Runner) {
	ui.Step("Remove LaunchDaemon")
	ui.WarnMsg(unsupportedNativeServiceError().Error())
}

func (unsupportedNativeServiceBackend) RollbackPfFirewall(ui *UI, _ *Runner) {
	ui.Step("Remove pf anchor")
	ui.WarnMsg(unsupportedNativeServiceError().Error())
}

func (unsupportedNativeServiceBackend) RollbackDNSBlocklist(ui *UI, _ *Runner) {
	ui.Step("Remove DNS blocklist")
	ui.WarnMsg(unsupportedNativeServiceError().Error())
}

func (unsupportedNativeServiceBackend) RollbackSudoers(ui *UI, _ *Runner) {
	ui.Step("Remove sudoers entries")
	ui.WarnMsg(unsupportedNativeServiceError().Error())
}

func (unsupportedNativeServiceBackend) InstallAgentMaintenanceSudoers(*UI, *Runner, string) error {
	return unsupportedNativeServiceError()
}

func (unsupportedNativeServiceBackend) UninstallAgentMaintenanceSudoers(*UI, *Runner) error {
	return unsupportedNativeServiceError()
}

func (unsupportedNativeServiceBackend) LaunchSudoersInstalled() bool {
	return false
}

func (unsupportedNativeServiceBackend) AgentMaintenanceSudoersInstalled() bool {
	return false
}

func (unsupportedNativeServiceBackend) GenericAgentPasswordlessAvailable() bool {
	return false
}

func (unsupportedNativeServiceBackend) FindBrewLaunchHelper() string {
	return ""
}

func unsupportedNativeServiceError() error {
	return fmt.Errorf("native network/service setup is not implemented on %s yet; supported platform is macOS", runtime.GOOS)
}

func pfctlLoadRules() error {
	return unsupportedNativeServiceError()
}

func launchctlBootstrap(string) error {
	return unsupportedNativeServiceError()
}
