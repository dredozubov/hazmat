package darwin

import (
	"slices"
	"testing"

	"hazmat/runtimeprovider"
)

func TestAdmitCurrentUserRejectsNonDarwinHost(t *testing.T) {
	plan, err := AdmitCurrentUser(CurrentUserAdmissionOptions{
		RuntimeGOOS:      "linux",
		ExperimentalGate: true,
		RunnerAvailable:  true,
	})
	if err != nil {
		t.Fatalf("AdmitCurrentUser: %v", err)
	}
	if plan.Provider.Status != runtimeprovider.StatusUnsupported || plan.Executable() {
		t.Fatalf("Provider = %+v, executable=%v; want unsupported non-executable", plan.Provider, plan.Executable())
	}
	if got := gapIDs(plan.CapabilityGaps); !slices.Contains(got, GapCurrentUserRuntimeNotDarwin) {
		t.Fatalf("gaps = %v, want %s", got, GapCurrentUserRuntimeNotDarwin)
	}
}

func TestAdmitCurrentUserReportsGateAndRunnerGaps(t *testing.T) {
	plan, err := AdmitCurrentUser(CurrentUserAdmissionOptions{
		RuntimeGOOS: "darwin",
	})
	if err != nil {
		t.Fatalf("AdmitCurrentUser: %v", err)
	}
	if plan.Provider.Provider != runtimeprovider.KindMacOSCurrentUser ||
		plan.Provider.Status != runtimeprovider.StatusPlanOnly ||
		plan.Provider.Executable ||
		plan.Executable() {
		t.Fatalf("Provider = %+v, executable=%v; want plan-only current-user", plan.Provider, plan.Executable())
	}
	got := gapIDs(plan.CapabilityGaps)
	for _, want := range []string{GapCurrentUserExperimentalGateClosed, GapCurrentUserRunnerMissing} {
		if !slices.Contains(got, want) {
			t.Fatalf("gaps = %v, want %s", got, want)
		}
	}
}

func TestAdmitCurrentUserExperimentalOnlyWithGateAndRunner(t *testing.T) {
	plan, err := AdmitCurrentUser(CurrentUserAdmissionOptions{
		RuntimeGOOS:      "darwin",
		ExperimentalGate: true,
		RunnerAvailable:  true,
	})
	if err != nil {
		t.Fatalf("AdmitCurrentUser: %v", err)
	}
	if plan.Provider.Status != runtimeprovider.StatusExperimental ||
		!plan.Provider.Executable ||
		!plan.Executable() ||
		len(plan.CapabilityGaps) != 0 {
		t.Fatalf("Plan = %+v, want executable experimental without gaps", plan)
	}

	descriptor, ok := runtimeprovider.DescriptorForKind(runtimeprovider.KindMacOSCurrentUser)
	if !ok {
		t.Fatal("macOS current-user descriptor missing")
	}
	if descriptor.Status != runtimeprovider.StatusPlanOnly {
		t.Fatalf("descriptor status mutated to %s, want plan-only", descriptor.Status)
	}
}

func TestCurrentUserGateEnabledUsesExplicitEnv(t *testing.T) {
	t.Setenv(EnvExperimentalCurrentUser, "")
	if CurrentUserGateEnabled() {
		t.Fatal("CurrentUserGateEnabled true for empty env")
	}
	t.Setenv(EnvExperimentalCurrentUser, "1")
	if !CurrentUserGateEnabled() {
		t.Fatal("CurrentUserGateEnabled false for enabled env")
	}
	t.Setenv(EnvExperimentalCurrentUser, "true")
	if CurrentUserGateEnabled() {
		t.Fatal("CurrentUserGateEnabled accepted non-1 value")
	}
}

func gapIDs(gaps []runtimeprovider.GapRecord) []string {
	out := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		out = append(out, gap.ID)
	}
	return out
}
