package hazmat

import (
	"os"
	"runtime"

	"hazmat/hostfacts"
	"hazmat/sessionbackend"
	"hazmat/sessionplanner"
)

type sessionBackendPlan = sessionbackend.Plan

func buildSessionPlanForHostFacts(target string, cfg sessionConfig, mode sessionMode, skipSnapshot bool, facts hostfacts.HostFacts) sessionplanner.Plan {
	authority := newSessionPlanAuthority(target, cfg, mode, skipSnapshot)
	return sessionplanner.Build(sessionplanner.Input{
		Contract:            authority.ContractInput(),
		Backend:             authority.BackendInput(facts),
		HarnessRequirements: authority.HarnessRequirements(),
	})
}

func currentHostFacts() hostfacts.HostFacts {
	return hostfacts.MustNew(hostfacts.Facts{
		Platform:    hostfacts.Platform{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
		AgentHome:   agentHome,
		InvokerHome: os.Getenv("HOME"),
	})
}
