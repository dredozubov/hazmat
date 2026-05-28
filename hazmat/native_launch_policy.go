package main

type nativeLaunchPolicyArtifact struct {
	Path    string
	cleanup func()
}

func (a nativeLaunchPolicyArtifact) Cleanup() {
	if a.cleanup != nil {
		a.cleanup()
	}
}

func prepareNativeLaunchPolicy(cfg sessionConfig) (nativeLaunchPolicyArtifact, error) {
	return prepareNativeLaunchPolicyWithPlan(cfg, nativeLaunchPlanForConfig(cfg))
}

func prepareNativeLaunchPolicyWithPlan(cfg sessionConfig, plan sessionBackendPlan) (nativeLaunchPolicyArtifact, error) {
	return newNativeLaunchBackend().PreparePolicy(nativeLaunchPolicyRequest{
		Config: cfg,
		Plan:   plan,
	})
}
