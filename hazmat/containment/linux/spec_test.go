package linuxspec

import (
	"bytes"
	"strings"
	"testing"

	"hazmat/containment"
	platformlinux "hazmat/platform/linux"
	"hazmat/sessionmeta"
)

func TestCompileBuildsPlanOnlyLaunchSpec(t *testing.T) {
	contract := testContract(t)
	report := platformlinux.Report{
		RuntimeOS: "linux",
		Features: platformlinux.FeatureSet{
			UserNamespaces:    availableFeature(),
			CgroupV2:          availableFeature(),
			Landlock:          availableFeature(),
			Seccomp:           availableFeature(),
			NetworkNamespaces: availableFeature(),
		},
	}

	spec, err := Compile(contract, CompileOptions{Platform: report})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if spec.FormatVersion != LaunchSpecFormatVersion || spec.Backend != BackendLinuxNative || spec.Phase != PhasePlanOnly {
		t.Fatalf("unexpected spec identity: %+v", spec)
	}
	wantMounts := []BindMount{
		{Source: "/workspace/project", Target: "/workspace/project", Access: containment.PathReadWrite},
		{Source: "/opt/sdk", Target: "/opt/sdk", Access: containment.PathReadOnly},
		{Source: "/workspace", Target: "/workspace", Access: containment.PathReadOnly},
		{Source: "/var/cache/build", Target: "/var/cache/build", Access: containment.PathReadWrite},
	}
	if len(spec.Mounts) != len(wantMounts) {
		t.Fatalf("Mounts = %+v, want %+v", spec.Mounts, wantMounts)
	}
	for i := range wantMounts {
		if spec.Mounts[i] != wantMounts[i] {
			t.Fatalf("Mounts[%d] = %+v, want %+v", i, spec.Mounts[i], wantMounts[i])
		}
	}
	if spec.Temp.Path != "/tmp/hazmat-session" || !spec.Temp.Tmpfs {
		t.Fatalf("Temp = %+v", spec.Temp)
	}
	if !spec.Network.UseNetworkNamespace || !spec.Network.Loopback || !spec.Network.DenyAllEgress {
		t.Fatalf("Network = %+v, want network namespace deny-all", spec.Network)
	}
	if !spec.Process.CloseInheritedFDs || !spec.Process.NoNewPrivs || !spec.Process.DropCapabilities {
		t.Fatalf("Process = %+v", spec.Process)
	}
	if len(spec.CredentialDenies) != 1 || spec.CredentialDenies[0].Path != "/home/agent/.ssh" {
		t.Fatalf("CredentialDenies = %+v", spec.CredentialDenies)
	}
	if len(spec.CapabilityGaps) != 1 || spec.CapabilityGaps[0].Code != "linux-native-launch-helper-missing" {
		t.Fatalf("CapabilityGaps = %+v", spec.CapabilityGaps)
	}
	raw, err := MarshalJSON(spec)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"backend": "linux-native"`)) {
		t.Fatalf("marshaled spec missing backend:\n%s", string(raw))
	}
}

func TestNewContractRejectsCredentialDenyOverlap(t *testing.T) {
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{{Path: "/home/agent/.ssh"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = containment.NewContract(testContractInput(containment.PathGrants([]string{
		"/opt/sdk",
		"/workspace",
		"/home/agent",
	}, containment.PathReadOnly)), floor)
	if err == nil {
		t.Fatal("NewContract succeeded for credential deny overlap")
	}
	if !strings.Contains(err.Error(), `"/home/agent" overlaps credential deny path "/home/agent/.ssh"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileRejectsUnconstructedCredentialFloor(t *testing.T) {
	contract := containment.Contract{
		Project:          containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		AgentHome:        containment.AgentHomePolicy{Path: "/home/agent"},
		Temp:             containment.TempPolicy{Path: "/tmp/hazmat-session"},
		CredentialDenies: []containment.CredentialDeny{{Path: "/home/agent/.ssh"}},
		Network:          containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:          containment.ProcessPolicy{AllowFork: true},
	}

	_, err := Compile(contract, CompileOptions{})
	if err == nil {
		t.Fatal("Compile succeeded without structural credential floor")
	}
	if !strings.Contains(err.Error(), "credential deny floor is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileReportsPlatformCapabilityGaps(t *testing.T) {
	report := platformlinux.Report{
		RuntimeOS: "darwin",
		Features: platformlinux.FeatureSet{
			UserNamespaces:    platformlinux.FeatureReport{State: platformlinux.FeatureUnavailable, Source: "userns"},
			CgroupV2:          platformlinux.FeatureReport{State: platformlinux.FeatureAvailable},
			Landlock:          platformlinux.FeatureReport{State: platformlinux.FeatureUnknown, Source: "landlock"},
			Seccomp:           platformlinux.FeatureReport{State: platformlinux.FeatureAvailable},
			NetworkNamespaces: platformlinux.FeatureReport{State: platformlinux.FeatureUnknown, Source: "netns"},
		},
	}

	spec, err := Compile(testContract(t), CompileOptions{Platform: report})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, code := range []string{
		"linux-native-launch-helper-missing",
		"runtime-not-linux",
		"user-namespaces-unavailable",
		"landlock-unavailable",
		"network-namespaces-unavailable",
	} {
		if !hasGap(spec.CapabilityGaps, code) {
			t.Fatalf("CapabilityGaps missing %q: %+v", code, spec.CapabilityGaps)
		}
	}
	if hasGap(spec.CapabilityGaps, "cgroup-v2-unavailable") || hasGap(spec.CapabilityGaps, "seccomp-unavailable") {
		t.Fatalf("CapabilityGaps included available features: %+v", spec.CapabilityGaps)
	}
}

func testContract(t *testing.T) containment.Contract {
	t.Helper()
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{
		{Path: "/home/agent/.ssh"},
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(testContractInput(containment.PathGrants([]string{
		"/opt/sdk",
		"/workspace",
	}, containment.PathReadOnly)), floor)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func testContractInput(readOnlyDirs []containment.PathGrant) containment.ContractInput {
	return containment.ContractInput{
		Project:      containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		ReadOnlyDirs: readOnlyDirs,
		ReadWriteDirs: containment.PathGrants([]string{
			"/workspace/project/tmp",
			"/var/cache/build",
		}, containment.PathReadWrite),
		AgentHome: containment.AgentHomePolicy{Path: "/home/agent"},
		Temp:      containment.TempPolicy{Path: "/tmp/hazmat-session"},
		Network:   containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:   containment.ProcessPolicy{AllowFork: true},
	}
}

func availableFeature() platformlinux.FeatureReport {
	return platformlinux.FeatureReport{State: platformlinux.FeatureAvailable}
}

func hasGap(gaps []CapabilityGap, code string) bool {
	for _, gap := range gaps {
		if gap.Code == code {
			return true
		}
	}
	return false
}
