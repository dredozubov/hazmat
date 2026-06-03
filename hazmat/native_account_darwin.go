//go:build darwin

package hazmat

import setupdarwin "hazmat/internal/setup/darwin"

type darwinNativeAccountBackend struct {
	impl setupdarwin.AccountBackend
}

func newNativeAccountBackend() nativeAccountBackend {
	return darwinNativeAccountBackend{
		impl: setupdarwin.NewAccountBackend(setupdarwin.AccountEnv{
			AgentUser:        agentUser,
			AgentUID:         agentUID,
			AgentHome:        agentHome,
			SharedGroup:      sharedGroup,
			SharedGID:        sharedGID,
			DsclPath:         hostDsclPath,
			GeneratePassword: generateRandomPassword,
		}),
	}
}

func (b darwinNativeAccountBackend) SetupAgentUser(ui *UI, runner *Runner) error {
	return b.impl.SetupAgentUser(ui, runner, runner.DryRun)
}

func (b darwinNativeAccountBackend) SetupDevGroup(ui *UI, runner *Runner, currentUser string) error {
	return b.impl.SetupDevGroup(ui, runner, currentUser)
}

func (b darwinNativeAccountBackend) RollbackAgentUser(ui *UI, runner *Runner) {
	b.impl.RollbackAgentUser(ui, runner)
}

func (b darwinNativeAccountBackend) RollbackDevGroup(ui *UI, runner *Runner) {
	b.impl.RollbackDevGroup(ui, runner)
}

func (b darwinNativeAccountBackend) UIDTaken(uid string) (bool, error) {
	return b.impl.UIDTaken(uid)
}

func (b darwinNativeAccountBackend) GIDTaken(gid string) (bool, error) {
	return b.impl.GIDTaken(gid)
}

func (b darwinNativeAccountBackend) GroupMembershipContains(group, username string) (bool, error) {
	return b.impl.GroupMembershipContains(group, username)
}
