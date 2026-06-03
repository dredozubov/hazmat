package setup

import "fmt"

type VerificationCallbacks struct {
	AgentUser       func()
	AgentHome       func()
	HomeDirTraverse func()
	PfAnchorLoaded  func()
	PfEnabled       func()
	Sudoers         func()
	DNSBlocklist    func()
	SeatbeltWrapper func()
	AgentEnv        func()
	HostWrappers    func()
}

type VerificationStep struct {
	Name     string
	Resource Resource
	Run      func()
}

func VerificationSteps(callbacks VerificationCallbacks) []VerificationStep {
	return []VerificationStep{
		{
			Name:     "verifyAgentUser",
			Resource: ResourceAgentUser,
			Run:      callbacks.AgentUser,
		},
		{
			Name:     "verifyAgentHome",
			Resource: ResourceAgentUser,
			Run:      callbacks.AgentHome,
		},
		{
			Name:     "verifyHomeDirTraverse",
			Resource: ResourceHomeDirTraverse,
			Run:      callbacks.HomeDirTraverse,
		},
		{
			Name:     "verifyPfAnchorLoaded",
			Resource: ResourcePfAnchor,
			Run:      callbacks.PfAnchorLoaded,
		},
		{
			Name:     "verifyPfEnabled",
			Resource: ResourcePfAnchor,
			Run:      callbacks.PfEnabled,
		},
		{
			Name:     "verifySudoers",
			Resource: ResourceSudoers,
			Run:      callbacks.Sudoers,
		},
		{
			Name:     "verifyDNSBlocklist",
			Resource: ResourceDNSBlocklist,
			Run:      callbacks.DNSBlocklist,
		},
		{
			Name:     "verifySeatbeltWrapper",
			Resource: ResourceSeatbelt,
			Run:      callbacks.SeatbeltWrapper,
		},
		{
			Name:     "verifyAgentEnv",
			Resource: ResourceWrappers,
			Run:      callbacks.AgentEnv,
		},
		{
			Name:     "verifyHostWrappers",
			Resource: ResourceWrappers,
			Run:      callbacks.HostWrappers,
		},
	}
}

func RunVerificationSteps(callbacks VerificationCallbacks) error {
	for _, step := range VerificationSteps(callbacks) {
		if step.Run == nil {
			return fmt.Errorf("setup verification step %s is not configured", step.Name)
		}
		step.Run()
	}
	return nil
}
