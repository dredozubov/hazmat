package main

type nativeAccountBackend interface {
	SetupAgentUser(ui *UI, r *Runner) error
	SetupDevGroup(ui *UI, r *Runner, currentUser string) error
	RollbackAgentUser(ui *UI, r *Runner)
	RollbackDevGroup(ui *UI, r *Runner)
	UIDTaken(uid string) (bool, error)
	GIDTaken(gid string) (bool, error)
	GroupMembershipContains(group, username string) (bool, error)
}

var nativeAccountBackendFactory = newNativeAccountBackend

func nativeAccountBackendForHost() nativeAccountBackend {
	return nativeAccountBackendFactory()
}
