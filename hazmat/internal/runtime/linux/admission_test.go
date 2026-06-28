package linux

import (
	"errors"
	"reflect"
	"testing"

	"hazmat/containment"
	linuxspec "hazmat/containment/linux"
	platformlinux "hazmat/platform/linux"
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
