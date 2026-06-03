package setup

import "fmt"

type RollbackCallbacks struct {
	Sudoers         func() error
	LaunchDaemon    func() error
	PfFirewall      func() error
	DNSBlocklist    func() error
	Seatbelt        func() error
	Wrappers        func() error
	HomeDirTraverse func() error
	Umask           func() error
	LocalRepo       func() error
	AgentUser       func() error
	DevGroup        func() error
}

type RollbackOptions struct {
	DeleteUser    bool
	DeleteGroup   bool
	AgentUserName string
	AgentHome     string
	GroupName     string
	Warn          func(string)
}

type RollbackStep struct {
	Name     string
	Resource Resource
	Run      func() error
}

func CoreRollbackSteps(callbacks RollbackCallbacks) []RollbackStep {
	return []RollbackStep{
		{
			Name:     "rollbackSudoers",
			Resource: ResourceSudoers,
			Run:      callbacks.Sudoers,
		},
		{
			Name:     "rollbackLaunchDaemon",
			Resource: ResourceLaunchDaemon,
			Run:      callbacks.LaunchDaemon,
		},
		{
			Name:     "rollbackPfFirewall",
			Resource: ResourcePfAnchor,
			Run:      callbacks.PfFirewall,
		},
		{
			Name:     "rollbackDNSBlocklist",
			Resource: ResourceDNSBlocklist,
			Run:      callbacks.DNSBlocklist,
		},
		{
			Name:     "rollbackSeatbelt",
			Resource: ResourceSeatbelt,
			Run:      callbacks.Seatbelt,
		},
		{
			Name:     "rollbackWrappers",
			Resource: ResourceWrappers,
			Run:      callbacks.Wrappers,
		},
		{
			Name:     "rollbackHomeDirTraverse",
			Resource: ResourceHomeDirTraverse,
			Run:      callbacks.HomeDirTraverse,
		},
		{
			Name:     "rollbackUmask",
			Resource: ResourceUmask,
			Run:      callbacks.Umask,
		},
		{
			Name:     "rollbackLocalRepo",
			Resource: ResourceLocalRepo,
			Run:      callbacks.LocalRepo,
		},
	}
}

func DestructiveRollbackSteps(callbacks RollbackCallbacks, options RollbackOptions) []RollbackStep {
	return []RollbackStep{
		{
			Name:     "rollbackAgentUser",
			Resource: ResourceAgentUser,
			Run: func() error {
				if options.DeleteUser {
					return runConfigured("rollbackAgentUser", callbacks.AgentUser)
				}
				warn(options, fmt.Sprintf("Agent user '%s' not removed. Use --delete-user to delete the account and %s.", options.AgentUserName, options.AgentHome))
				return nil
			},
		},
		{
			Name:     "rollbackDevGroup",
			Resource: ResourceDevGroup,
			Run: func() error {
				if options.DeleteGroup {
					return runConfigured("rollbackDevGroup", callbacks.DevGroup)
				}
				warn(options, fmt.Sprintf("Group '%s' not removed. Use --delete-group to delete it.", options.GroupName))
				return nil
			},
		},
	}
}

func RunRollbackSteps(callbacks RollbackCallbacks, options RollbackOptions) error {
	for _, step := range CoreRollbackSteps(callbacks) {
		if err := runRollbackStep(step); err != nil {
			return err
		}
	}
	for _, step := range DestructiveRollbackSteps(callbacks, options) {
		if err := runRollbackStep(step); err != nil {
			return err
		}
	}
	return nil
}

func runRollbackStep(step RollbackStep) error {
	return runConfigured(step.Name, step.Run)
}

func runConfigured(name string, run func() error) error {
	if run == nil {
		return fmt.Errorf("rollback step %s is not configured", name)
	}
	return run()
}

func warn(options RollbackOptions, message string) {
	if options.Warn != nil {
		options.Warn(message)
	}
}
