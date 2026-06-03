package hazmat

import (
	"os"
	"runtime"

	"hazmat/hostfacts"
	"hazmat/sessionbackend"
	"hazmat/sessionplanner"
)

type sessionBackendPlan = sessionbackend.Plan

func buildSessionBackendPlan(cfg sessionConfig, mode sessionMode) sessionBackendPlan {
	return buildSessionBackendPlanForHostFacts(cfg, mode, currentHostFacts())
}

func buildSessionBackendPlanForGOOS(cfg sessionConfig, mode sessionMode, goos string) sessionBackendPlan {
	facts := currentHostFacts()
	facts.Platform.GOOS = goos
	return buildSessionBackendPlanForHostFacts(cfg, mode, facts)
}

func buildSessionBackendPlanForHostFacts(cfg sessionConfig, mode sessionMode, facts hostfacts.Facts) sessionBackendPlan {
	return sessionplanner.BuildBackendPlan(sessionbackend.Input{
		Target:             cfg.Target,
		Mode:               mode,
		ProjectDir:         cfg.ProjectDir,
		ReadOnlyDirs:       cfg.ReadDirs,
		ReadWriteDirs:      cfg.WriteDirs,
		NetworkMode:        cfg.NetworkMode,
		Integrations:       cfg.ActiveIntegrations,
		IntegrationEnvKeys: integrationEnvKeys(cfg.IntegrationEnv),
		HostFacts:          facts,
	})
}

func currentHostFacts() hostfacts.Facts {
	return hostfacts.Facts{
		Platform:    hostfacts.Platform{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
		AgentHome:   agentHome,
		InvokerHome: os.Getenv("HOME"),
	}.Normalized()
}
