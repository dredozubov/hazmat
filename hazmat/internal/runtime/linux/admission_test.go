package linux

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"hazmat/containment"
	linuxspec "hazmat/containment/linux"
	platformlinux "hazmat/platform/linux"
	"hazmat/runtimeprovider"
	"hazmat/sessionmeta"
)

func TestAdmitCurrentUserOrderMatchesLinuxNativeLaunchModel(t *testing.T) {
	spec := currentUserSpec(t, sessionmeta.NetworkNone)
	plan, err := AdmitCurrentUser(spec, availableReport())
	if err != nil {
		t.Fatalf("AdmitCurrentUser: %v", err)
	}
	wantStages := []Stage{
		StageValidated,
		StageFDSClosed,
		StageNamespaces,
		StageMounts,
		StageNetwork,
		StagePrivileges,
		StageNoNewPrivs,
		StageLandlock,
		StageSeccomp,
		StageMetadata,
		StageExec,
	}
	if !reflect.DeepEqual(plan.Stages, wantStages) {
		t.Fatalf("Stages = %#v, want %#v", plan.Stages, wantStages)
	}
	if plan.FDs.CloseMin != 3 || !reflect.DeepEqual(plan.FDs.Preserve, []int{0, 1, 2}) {
		t.Fatalf("FDs = %+v, want stdio preserved and fd>=3 closed", plan.FDs)
	}
	if !plan.Namespaces.User || !plan.Namespaces.Mount || !plan.Namespaces.Network {
		t.Fatalf("Namespaces = %+v, want user/mount/network", plan.Namespaces)
	}
	if !plan.Metadata || !plan.Exec {
		t.Fatalf("Metadata/Exec = %v/%v, want both true after enforcement", plan.Metadata, plan.Exec)
	}
}

func TestAdmitAgentUserOrderMatchesLinuxNativeLaunchModel(t *testing.T) {
	spec := agentUserSpec(t, sessionmeta.NetworkNone)
	plan, err := AdmitAgentUser(spec, agentUserReadyReport())
	if err != nil {
		t.Fatalf("AdmitAgentUser: %v", err)
	}
	wantStages := []Stage{
		StageValidated,
		StageFDSClosed,
		StageNamespaces,
		StageMounts,
		StageNetwork,
		StagePrivileges,
		StageNoNewPrivs,
		StageLandlock,
		StageSeccomp,
		StageMetadata,
		StageExec,
	}
	if !reflect.DeepEqual(plan.Stages, wantStages) {
		t.Fatalf("Stages = %#v, want %#v", plan.Stages, wantStages)
	}
	if plan.Identity.Lane != linuxspec.IdentityAgentUser ||
		plan.Identity.HelperStrategy != linuxspec.HelperRoot ||
		plan.Identity.RunAs != "agent" ||
		!plan.Identity.DropToAgent {
		t.Fatalf("Identity = %+v, want agent-user/root-helper drop", plan.Identity)
	}
	if plan.Namespaces.User || !plan.Namespaces.Mount || !plan.Namespaces.Network {
		t.Fatalf("Namespaces = %+v, want root-helper mount/network without rootless userns", plan.Namespaces)
	}
	if !plan.Metadata || !plan.Exec {
		t.Fatalf("Metadata/Exec = %v/%v, want both true after enforcement", plan.Metadata, plan.Exec)
	}
}

func TestAdmitAgentUserRejectsSetupGapsBeforeSideEffects(t *testing.T) {
	spec := agentUserSpec(t, sessionmeta.NetworkNone)
	report := agentUserReadyReport()
	report.Features.CgroupV2 = platformlinux.FeatureReport{State: platformlinux.FeatureUnavailable, Source: "cgroup"}
	report.AgentUserBackend.CapabilityGaps = []runtimeprovider.GapRecord{
		runtimeprovider.MustGapRecord(runtimeprovider.KindLinuxAgentUser, runtimeprovider.StatusSetupRequired, linuxspec.GapSetupRequired, "persistent Linux agent-user setup resources are missing", "setup-required"),
		runtimeprovider.MustGapRecord(runtimeprovider.KindLinuxAgentUser, runtimeprovider.StatusSetupRequired, linuxspec.GapNativeLaunchHelperMissing, "Linux agent-user root helper is not installed or verified", "setup-required"),
	}

	plan, err := AdmitAgentUser(spec, report)
	var gapErr GapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("err = %v, want GapError", err)
	}
	if !reflect.DeepEqual(plan.Stages, []Stage{StageValidated}) {
		t.Fatalf("Stages = %#v, want validation only before setup gap failure", plan.Stages)
	}
	for _, code := range []string{
		linuxspec.GapSetupRequired,
		linuxspec.GapNativeLaunchHelperMissing,
		linuxspec.GapCgroupV2Unavailable,
	} {
		if !hasGap(gapErr.Gaps, code) {
			t.Fatalf("gaps missing %q: %+v", code, gapErr.Gaps)
		}
	}
}

func TestAdmitCurrentUserRejectsMissingCapabilitiesBeforeSideEffects(t *testing.T) {
	spec := currentUserSpec(t, sessionmeta.NetworkNone)
	report := availableReport()
	report.RuntimeOS = "darwin"
	report.Features.UserNamespaces = platformlinux.FeatureReport{State: platformlinux.FeatureUnavailable, Source: "userns"}
	report.Features.MountNamespaces = platformlinux.FeatureReport{State: platformlinux.FeatureUnknown, Source: "mntns"}
	report.Features.Landlock = platformlinux.FeatureReport{State: platformlinux.FeatureUnknown, Source: "landlock"}
	report.Features.Seccomp = platformlinux.FeatureReport{State: platformlinux.FeatureUnavailable, Source: "seccomp"}
	report.Features.NetworkNamespaces = platformlinux.FeatureReport{State: platformlinux.FeatureUnknown, Source: "netns"}

	plan, err := AdmitCurrentUser(spec, report)
	var gapErr GapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("err = %v, want GapError", err)
	}
	if !reflect.DeepEqual(plan.Stages, []Stage{StageValidated}) {
		t.Fatalf("Stages = %#v, want validation only before capability failure", plan.Stages)
	}
	for _, code := range []string{
		linuxspec.GapRuntimeNotLinux,
		linuxspec.GapUserNamespaceUnavailable,
		linuxspec.GapMountNamespaceUnavailable,
		linuxspec.GapLandlockUnavailable,
		linuxspec.GapSeccompUnavailable,
		linuxspec.GapNetworkNamespaceUnavailable,
	} {
		if !hasGap(gapErr.Gaps, code) {
			t.Fatalf("gaps missing %q: %+v", code, gapErr.Gaps)
		}
	}
}

func TestAdmitCurrentUserNetworkDefaultMakesNoEgressFilteringClaim(t *testing.T) {
	spec := currentUserSpec(t, sessionmeta.NetworkDefault)
	report := availableReport()
	report.Features.NetworkNamespaces = platformlinux.FeatureReport{State: platformlinux.FeatureUnavailable, Source: "netns"}

	plan, err := AdmitCurrentUser(spec, report)
	if err != nil {
		t.Fatalf("AdmitCurrentUser: %v", err)
	}
	if plan.Namespaces.Network {
		t.Fatalf("Namespaces.Network = true, want no network namespace for default mode")
	}
	if plan.Network.Mode != sessionmeta.NetworkDefault || plan.Network.EgressFiltering {
		t.Fatalf("Network = %+v, want host network with no egress filtering claim", plan.Network)
	}
}

func TestAdmitCurrentUserRejectsNonCurrentUserSpec(t *testing.T) {
	spec := currentUserSpec(t, sessionmeta.NetworkNone)
	spec.Identity = linuxspec.IdentityAgentUser
	spec.HelperStrategy = linuxspec.HelperRoot
	_, err := AdmitCurrentUser(spec, availableReport())
	if err == nil {
		t.Fatal("AdmitCurrentUser accepted agent-user spec")
	}
}

func TestAdmitAgentUserRejectsCurrentUserSpecWithoutFallback(t *testing.T) {
	_, err := AdmitAgentUser(currentUserSpec(t, sessionmeta.NetworkNone), agentUserReadyReport())
	if err == nil {
		t.Fatal("AdmitAgentUser accepted current-user spec")
	}
	if !strings.Contains(err.Error(), `identity "agent-user"`) {
		t.Fatalf("err = %v, want explicit agent-user identity requirement", err)
	}
}

func TestAdmitAgentUserRejectsHelperStrategyDowngrade(t *testing.T) {
	spec := agentUserSpec(t, sessionmeta.NetworkNone)
	spec.HelperStrategy = linuxspec.HelperRootlessUserNS
	_, err := AdmitAgentUser(spec, agentUserReadyReport())
	if err == nil {
		t.Fatal("AdmitAgentUser accepted rootless helper strategy")
	}
	if !strings.Contains(err.Error(), `helper_strategy "root-helper"`) {
		t.Fatalf("err = %v, want explicit root-helper requirement", err)
	}
}

func currentUserSpec(t *testing.T, network sessionmeta.NetworkMode) linuxspec.LaunchSpec {
	t.Helper()
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{{Path: "/home/user/.ssh"}})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project:      containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		ReadOnlyDirs: containment.PathGrants([]string{"/opt/sdk"}, containment.PathReadOnly),
		AgentHome: containment.AgentHomePolicy{
			Path:           "/tmp/hazmat-session/home",
			Mode:           containment.AgentHomeModeSessionLocal,
			PersistentPath: "/home/user",
		},
		Temp:    containment.TempPolicy{Path: "/tmp/hazmat-session"},
		Network: containment.NetworkPolicy{Mode: network},
		Process: containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := linuxspec.Compile(contract, linuxspec.CompileOptions{
		Platform:       availableReport(),
		Identity:       linuxspec.IdentityCurrentUser,
		HelperStrategy: linuxspec.HelperRootlessUserNS,
	})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func agentUserSpec(t *testing.T, network sessionmeta.NetworkMode) linuxspec.LaunchSpec {
	t.Helper()
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{{Path: "/home/agent/.ssh"}})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project:      containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		ReadOnlyDirs: containment.PathGrants([]string{"/opt/sdk"}, containment.PathReadOnly),
		AgentHome: containment.AgentHomePolicy{
			Path:           "/tmp/hazmat-session/home",
			Mode:           containment.AgentHomeModeSessionLocal,
			PersistentPath: "/home/agent",
		},
		Temp:    containment.TempPolicy{Path: "/tmp/hazmat-session"},
		Network: containment.NetworkPolicy{Mode: network},
		Process: containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := linuxspec.Compile(contract, linuxspec.CompileOptions{
		Platform:       availableReport(),
		Identity:       linuxspec.IdentityAgentUser,
		HelperStrategy: linuxspec.HelperRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func agentUserReadyReport() platformlinux.Report {
	report := availableReport()
	report.AgentUserBackend = platformlinux.NativeBackendStatus{CapabilityOK: true}
	return report
}

func availableReport() platformlinux.Report {
	return platformlinux.Report{
		RuntimeOS: "linux",
		Features: platformlinux.FeatureSet{
			UserNamespaces:    availableFeature(),
			MountNamespaces:   availableFeature(),
			CgroupV2:          availableFeature(),
			Landlock:          availableFeature(),
			Seccomp:           availableFeature(),
			NetworkNamespaces: availableFeature(),
		},
	}
}

func availableFeature() platformlinux.FeatureReport {
	return platformlinux.FeatureReport{State: platformlinux.FeatureAvailable}
}

func hasGap(gaps []linuxspec.CapabilityGap, code string) bool {
	for _, gap := range gaps {
		if gap.Code == code {
			return true
		}
	}
	return false
}
