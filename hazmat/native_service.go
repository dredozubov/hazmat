package main

type nativeServiceBackend interface {
	SetupLaunchHelper(ui *UI, r *Runner) error
	SetupSudoers(ui *UI, r *Runner, currentUser string) error
	MaybeSetupOptionalAgentMaintenanceSudoers(ui *UI, r *Runner, currentUser string) error
	SetupPfFirewall(ui *UI, r *Runner) error
	SetupDNSBlocklist(ui *UI, r *Runner) error
	SetupLaunchDaemon(ui *UI, r *Runner) error
	RollbackLaunchDaemon(ui *UI, r *Runner)
	RollbackPfFirewall(ui *UI, r *Runner)
	RollbackDNSBlocklist(ui *UI, r *Runner)
	RollbackSudoers(ui *UI, r *Runner)
	InstallAgentMaintenanceSudoers(ui *UI, r *Runner, currentUser string) error
	UninstallAgentMaintenanceSudoers(ui *UI, r *Runner) error
	LaunchSudoersInstalled() bool
	AgentMaintenanceSudoersInstalled() bool
	GenericAgentPasswordlessAvailable() bool
	FindBrewLaunchHelper() string
}

var nativeServiceBackendFactory = newNativeServiceBackend

func nativeServiceBackendForHost() nativeServiceBackend {
	return nativeServiceBackendFactory()
}
