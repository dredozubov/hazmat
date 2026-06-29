package darwin

import (
	"fmt"
	"os"
	"runtime"

	"hazmat/runtimeprovider"
)

const (
	EnvExperimentalCurrentUser = "HAZMAT_EXPERIMENTAL_MACOS_CURRENT_USER"

	GapCurrentUserRunnerMissing          = "macos.current-user-runner-missing"
	GapCurrentUserExperimentalGateClosed = "macos.current-user-experimental-gate-closed"
	GapCurrentUserRuntimeNotDarwin       = "macos.current-user-runtime-not-darwin"
)

type CurrentUserAdmissionOptions struct {
	RuntimeGOOS      string
	ExperimentalGate bool
	RunnerAvailable  bool
}

type CurrentUserAdmissionPlan struct {
	Provider       runtimeprovider.ProviderStatusRecord `json:"provider"`
	CapabilityGaps []runtimeprovider.GapRecord          `json:"capability_gaps,omitempty"`
}

func CurrentUserGateEnabled() bool {
	return os.Getenv(EnvExperimentalCurrentUser) == "1"
}

func AdmitCurrentUser(opts CurrentUserAdmissionOptions) (CurrentUserAdmissionPlan, error) {
	descriptor, ok := runtimeprovider.DescriptorForKind(runtimeprovider.KindMacOSCurrentUser)
	if !ok {
		return CurrentUserAdmissionPlan{}, fmt.Errorf("runtime provider descriptor %q is missing", runtimeprovider.KindMacOSCurrentUser)
	}
	runtimeGOOS := opts.RuntimeGOOS
	if runtimeGOOS == "" {
		runtimeGOOS = runtime.GOOS
	}

	if runtimeGOOS != "darwin" {
		return CurrentUserAdmissionPlan{
			Provider: statusRecordFor(descriptor, runtimeprovider.StatusUnsupported),
			CapabilityGaps: []runtimeprovider.GapRecord{
				runtimeprovider.MustGapRecord(
					runtimeprovider.KindMacOSCurrentUser,
					runtimeprovider.StatusUnsupported,
					GapCurrentUserRuntimeNotDarwin,
					"macOS current-user Seatbelt admission requires a Darwin host",
					"unsupported",
				),
			},
		}, nil
	}

	gaps := make([]runtimeprovider.GapRecord, 0, 2)
	if !opts.ExperimentalGate {
		gaps = append(gaps, runtimeprovider.MustGapRecord(
			runtimeprovider.KindMacOSCurrentUser,
			runtimeprovider.StatusPlanOnly,
			GapCurrentUserExperimentalGateClosed,
			"macOS current-user Seatbelt launch requires "+EnvExperimentalCurrentUser+"=1",
			"plan-only",
		))
	}
	if !opts.RunnerAvailable {
		gaps = append(gaps, runtimeprovider.MustGapRecord(
			runtimeprovider.KindMacOSCurrentUser,
			runtimeprovider.StatusPlanOnly,
			GapCurrentUserRunnerMissing,
			"macOS current-user native runner is not implemented or admitted",
			"plan-only",
		))
	}

	status := runtimeprovider.StatusPlanOnly
	if len(gaps) == 0 {
		status = runtimeprovider.StatusExperimental
	}
	return CurrentUserAdmissionPlan{
		Provider:       statusRecordFor(descriptor, status),
		CapabilityGaps: gaps,
	}, nil
}

func (p CurrentUserAdmissionPlan) Executable() bool {
	return p.Provider.Executable && len(p.CapabilityGaps) == 0
}

func statusRecordFor(descriptor runtimeprovider.Descriptor, status runtimeprovider.Status) runtimeprovider.ProviderStatusRecord {
	descriptor.Status = status
	return descriptor.StatusRecord()
}
