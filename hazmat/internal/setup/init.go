package setup

import "fmt"

type InitCallbacks struct {
	AgentUser                  func() error
	DevGroup                   func() error
	HomeDirTraverse            func() error
	LocalRepo                  func() error
	HardeningGaps              func() error
	Seatbelt                   func() error
	Wrappers                   func() error
	PfFirewall                 func() error
	DNSBlocklist               func() error
	LaunchDaemon               func() error
	LaunchHelper               func() error
	Sudoers                    func() error
	OptionalMaintenanceSudoers func() error
	SelectedHarness            func() error
	AgentCredentials           func() error
}

type InitStep struct {
	Name     string
	Resource Resource
	Run      func() error
}

func InitSetupSteps(callbacks InitCallbacks) []InitStep {
	return []InitStep{
		{
			Name:     "setupAgentUser",
			Resource: ResourceAgentUser,
			Run:      callbacks.AgentUser,
		},
		{
			Name:     "setupDevGroup",
			Resource: ResourceDevGroup,
			Run:      callbacks.DevGroup,
		},
		{
			Name:     "setupHomeDirTraverse",
			Resource: ResourceHomeDirTraverse,
			Run:      callbacks.HomeDirTraverse,
		},
		{
			Name:     "setupLocalRepo",
			Resource: ResourceLocalRepo,
			Run:      callbacks.LocalRepo,
		},
		{
			Name:     "setupHardeningGaps",
			Resource: ResourceHardeningGaps,
			Run:      callbacks.HardeningGaps,
		},
		{
			Name:     "setupSeatbelt",
			Resource: ResourceSeatbelt,
			Run:      callbacks.Seatbelt,
		},
		{
			Name:     "setupWrappers",
			Resource: ResourceWrappers,
			Run:      callbacks.Wrappers,
		},
		{
			Name:     "setupPfFirewall",
			Resource: ResourcePfAnchor,
			Run:      callbacks.PfFirewall,
		},
		{
			Name:     "setupDNSBlocklist",
			Resource: ResourceDNSBlocklist,
			Run:      callbacks.DNSBlocklist,
		},
		{
			Name:     "setupLaunchDaemon",
			Resource: ResourceLaunchDaemon,
			Run:      callbacks.LaunchDaemon,
		},
		{
			Name:     "setupLaunchHelper",
			Resource: ResourceLaunchHelper,
			Run:      callbacks.LaunchHelper,
		},
		{
			Name:     "setupSudoers",
			Resource: ResourceSudoers,
			Run:      callbacks.Sudoers,
		},
		{
			Name:     "maybeSetupOptionalAgentMaintenanceSudoers",
			Resource: ResourceMaintenanceSudoers,
			Run:      callbacks.OptionalMaintenanceSudoers,
		},
		{
			Name:     "setupSelectedHarness",
			Resource: ResourceClaudeCode,
			Run:      callbacks.SelectedHarness,
		},
		{
			Name:     "setupAgentCredentials",
			Resource: ResourceCredentials,
			Run:      callbacks.AgentCredentials,
		},
	}
}

func RunInitSetupSteps(callbacks InitCallbacks) error {
	for _, step := range InitSetupSteps(callbacks) {
		if step.Run == nil {
			return fmt.Errorf("setup step %s is not configured", step.Name)
		}
		if err := step.Run(); err != nil {
			return err
		}
	}
	return nil
}
