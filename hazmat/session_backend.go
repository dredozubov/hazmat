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
	return buildSessionPlanForHostFacts(cfg.Target, cfg, mode, false, facts).Backend
}

func buildSessionPlanForHostFacts(target string, cfg sessionConfig, mode sessionMode, skipSnapshot bool, facts hostfacts.Facts) sessionplanner.Plan {
	return sessionplanner.Build(sessionplanner.Input{
		Contract:            buildSessionContractPlanInput(target, cfg, mode, skipSnapshot),
		Backend:             buildSessionBackendPlanInput(target, cfg, mode, facts),
		HarnessRequirements: buildSessionHarnessRequirements(cfg),
	})
}

func buildSessionBackendPlanInput(target string, cfg sessionConfig, mode sessionMode, facts hostfacts.Facts) sessionbackend.Input {
	return sessionbackend.Input{
		Target:             target,
		Mode:               mode,
		ProjectDir:         cfg.ProjectDir,
		ReadOnlyDirs:       cfg.ReadDirs,
		ReadWriteDirs:      cfg.WriteDirs,
		NetworkMode:        cfg.NetworkMode,
		Integrations:       cfg.ActiveIntegrations,
		IntegrationEnvKeys: integrationEnvKeys(cfg.IntegrationEnv),
		HostFacts:          facts,
	}
}

func buildSessionHarnessRequirements(cfg sessionConfig) []sessionplanner.HarnessRequirement {
	if cfg.HarnessID == "" {
		return nil
	}
	return []sessionplanner.HarnessRequirement{{
		ID:     string(cfg.HarnessID),
		Reason: "session target harness",
	}}
}

func currentHostFacts() hostfacts.Facts {
	return hostfacts.Facts{
		Platform:    hostfacts.Platform{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
		AgentHome:   agentHome,
		InvokerHome: os.Getenv("HOME"),
	}.Normalized()
}
