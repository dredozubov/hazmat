package hazmat

import "hazmat/hostfacts"

func generateSBPL(cfg sessionConfig) string {
	policy, err := buildNativeSessionPolicy(cfg)
	if err != nil {
		panic(err)
	}
	sbpl, err := compileDarwinSBPLChecked(policy)
	if err != nil {
		panic(err)
	}
	return sbpl
}

func buildSandboxLaunchSpec(agent string, cfg sessionConfig, profile sandboxPolicyProfile) (sandboxLaunchSpec, error) {
	return buildSandboxLaunchSpecWithPlan(agent, cfg, buildSessionBackendPlan(cfg, sessionModeDockerSandbox), profile)
}

func buildSessionBackendPlan(cfg sessionConfig, mode sessionMode) sessionBackendPlan {
	return buildSessionPlanForHostFacts(cfg.Target, cfg, mode, false, currentHostFacts()).Backend
}

func buildSessionBackendPlanForGOOS(cfg sessionConfig, mode sessionMode, goos string) sessionBackendPlan {
	facts := currentHostFacts().DTO()
	facts.Platform.GOOS = goos
	return buildSessionPlanForHostFacts(cfg.Target, cfg, mode, false, hostfacts.MustNew(facts)).Backend
}
