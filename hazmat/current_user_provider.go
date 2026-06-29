package hazmat

import (
	"fmt"
	"runtime"
	"strings"

	darwinruntime "hazmat/internal/runtime/darwin"
	"hazmat/runtimeprovider"
)

type currentUserSessionDirs struct {
	Root       string
	Home       string
	CacheHome  string
	ConfigHome string
	DataHome   string
	TempDir    string
}

func parseSessionRuntimeProvider(raw string, explicit bool) (runtimeprovider.Kind, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if explicit {
			return "", false, fmt.Errorf("--provider requires a value (macos-current-user or macos-agent-user)")
		}
		return "", false, nil
	}
	provider := runtimeprovider.Kind(raw)
	switch provider {
	case runtimeprovider.KindMacOSCurrentUser, runtimeprovider.KindMacOSAgentUser:
		return provider, true, nil
	default:
		return "", false, fmt.Errorf("unknown runtime provider %q (want macos-current-user or macos-agent-user)", raw)
	}
}

func admitLiveCurrentUserProvider() error {
	plan, err := currentUserAdmissionPlan(false)
	if err != nil {
		return err
	}
	if plan.Executable() {
		return nil
	}
	lines := runtimeprovider.RenderGaps(plan.CapabilityGaps)
	if len(lines) == 0 {
		return fmt.Errorf("runtime provider %s is not executable", runtimeprovider.KindMacOSCurrentUser)
	}
	return fmt.Errorf("runtime provider %s is not executable:\n%s", runtimeprovider.KindMacOSCurrentUser, strings.Join(lines, "\n"))
}

func applyRuntimeProviderAdmission(cfg *sessionConfig, planOnly bool) error {
	if cfg.RuntimeProvider == "" && cfg.RuntimeProviderExplicit {
		return fmt.Errorf("runtime provider is empty")
	}
	if cfg.RuntimeProvider == "" || cfg.RuntimeProvider == runtimeprovider.KindMacOSAgentUser {
		return nil
	}
	if cfg.RuntimeProvider != runtimeprovider.KindMacOSCurrentUser {
		return fmt.Errorf("unsupported runtime provider %s", cfg.RuntimeProvider)
	}
	plan, err := currentUserAdmissionPlan(planOnly)
	if err != nil {
		return err
	}
	cfg.RuntimeProviderStatus = &plan.Provider
	cfg.RuntimeProviderGaps = append([]runtimeprovider.GapRecord(nil), plan.CapabilityGaps...)
	if !planOnly && !plan.Executable() {
		return admitLiveCurrentUserProvider()
	}
	if len(plan.CapabilityGaps) > 0 {
		for _, line := range runtimeprovider.RenderGaps(plan.CapabilityGaps) {
			cfg.SessionNotes = append(cfg.SessionNotes, "macOS current-user provider gap: "+line)
		}
	}
	return nil
}

func currentUserAdmissionPlan(planOnly bool) (darwinruntime.CurrentUserAdmissionPlan, error) {
	return darwinruntime.AdmitCurrentUser(darwinruntime.CurrentUserAdmissionOptions{
		RuntimeGOOS:      runtime.GOOS,
		ExperimentalGate: darwinruntime.CurrentUserGateEnabled(),
		RunnerAvailable:  launchHelperSupportsCurrentUser(launchHelperPath()),
	})
}
