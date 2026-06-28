package linuxspec

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"hazmat/containment"
	platformlinux "hazmat/platform/linux"
	"hazmat/sessionmeta"
)

var updateGoldenBaselines = flag.Bool("update-golden", false, "update golden baseline files")

func TestCompileBuildsPlanOnlyLaunchSpec(t *testing.T) {
	contract := testContract(t)
	report := platformlinux.Report{
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

	spec, err := Compile(contract, CompileOptions{Platform: report})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if spec.FormatVersion != LaunchSpecFormatVersion || spec.Backend != BackendLinuxNative || spec.Phase != PhasePlanOnly {
		t.Fatalf("unexpected spec identity: %+v", spec)
	}
	if spec.Identity != IdentityCurrentUser || spec.HelperStrategy != HelperRootlessUserNS {
		t.Fatalf("identity/helper = %q/%q, want current-user/rootless-userns", spec.Identity, spec.HelperStrategy)
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
	if !spec.Process.CloseInheritedFDs || !spec.Process.NoNewPrivs || !spec.Process.DropCapabilities || !spec.Process.AllowFork {
		t.Fatalf("Process = %+v", spec.Process)
	}
	if len(spec.CredentialDenies) != 1 || spec.CredentialDenies[0].Path != "/home/agent/.ssh" {
		t.Fatalf("CredentialDenies = %+v", spec.CredentialDenies)
	}
	if len(spec.CapabilityGaps) != 1 || spec.CapabilityGaps[0].Code != GapNativeLaunchHelperMissing {
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

func TestCompileRecordsBothIdentityLanesFromSameContract(t *testing.T) {
	contract := testContract(t)
	report := availableReport()
	current, err := Compile(contract, CompileOptions{
		Platform: report,
		Identity: IdentityCurrentUser,
	})
	if err != nil {
		t.Fatalf("Compile current-user: %v", err)
	}
	agent, err := Compile(contract, CompileOptions{
		Platform: report,
		Identity: IdentityAgentUser,
	})
	if err != nil {
		t.Fatalf("Compile agent-user: %v", err)
	}
	if current.Identity != IdentityCurrentUser || current.HelperStrategy != HelperRootlessUserNS {
		t.Fatalf("current identity/helper = %q/%q", current.Identity, current.HelperStrategy)
	}
	if agent.Identity != IdentityAgentUser || agent.HelperStrategy != HelperRoot {
		t.Fatalf("agent identity/helper = %q/%q", agent.Identity, agent.HelperStrategy)
	}
	if !reflect.DeepEqual(current.Mounts, agent.Mounts) ||
		current.AgentHome != agent.AgentHome ||
		current.Temp != agent.Temp ||
		current.Network != agent.Network ||
		current.Process != agent.Process {
		t.Fatalf("lanes diverged on contract-derived fields:\ncurrent=%+v\nagent=%+v", current, agent)
	}
}

func TestCompileRejectsIdentityHelperStrategyMismatch(t *testing.T) {
	cases := []CompileOptions{
		{Identity: IdentityCurrentUser, HelperStrategy: HelperRoot},
		{Identity: IdentityAgentUser, HelperStrategy: HelperRootlessUserNS},
		{Identity: "other"},
		{Identity: IdentityCurrentUser, HelperStrategy: "other"},
	}
	for _, opts := range cases {
		t.Run(string(opts.Identity)+"/"+string(opts.HelperStrategy), func(t *testing.T) {
			_, err := Compile(testContract(t), opts)
			if err == nil {
				t.Fatalf("Compile accepted opts %+v", opts)
			}
		})
	}
}

func TestCompileNetworkDefaultMakesNoEgressFilteringClaim(t *testing.T) {
	input := testContractInput(containment.PathGrants([]string{"/opt/sdk"}, containment.PathReadOnly))
	input.Network = containment.NetworkPolicy{Mode: sessionmeta.NetworkDefault}
	input.Process = containment.ProcessPolicy{AllowFork: false}
	contract, err := containment.NewContract(input, goldenCredentialFloor(t, "/home/agent"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := Compile(contract, CompileOptions{Platform: availableReport()})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if spec.Network.Mode != sessionmeta.NetworkDefault ||
		spec.Network.UseNetworkNamespace ||
		spec.Network.Loopback ||
		spec.Network.DenyAllEgress {
		t.Fatalf("Network = %+v, want default without egress filtering claim", spec.Network)
	}
	if spec.Process.AllowFork {
		t.Fatalf("Process.AllowFork = true, want false from contract")
	}
}

func TestGoldenLinuxLaunchSpecBaseline(t *testing.T) {
	contract, err := containment.NewContract(containment.ContractInput{
		Project: containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		ReadOnlyDirs: containment.PathGrants([]string{
			"/opt/sdk",
			"/workspace/reference",
		}, containment.PathReadOnly),
		ReadWriteDirs: containment.PathGrants([]string{
			"/workspace/project/.cache",
		}, containment.PathReadWrite),
		AgentHome: containment.AgentHomePolicy{Path: "/home/agent"},
		Temp:      containment.TempPolicy{Path: "/tmp/hazmat-session"},
		Network:   containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:   containment.ProcessPolicy{AllowFork: true},
	}, goldenCredentialFloor(t, "/home/agent"))
	if err != nil {
		t.Fatalf("NewContract fixture: %v", err)
	}
	spec, err := Compile(contract, CompileOptions{
		Platform: platformlinux.Report{
			RuntimeOS: "linux",
			Features: platformlinux.FeatureSet{
				UserNamespaces:    platformlinux.FeatureReport{State: platformlinux.FeatureAvailable, Source: "golden"},
				MountNamespaces:   platformlinux.FeatureReport{State: platformlinux.FeatureAvailable, Source: "golden"},
				CgroupV2:          platformlinux.FeatureReport{State: platformlinux.FeatureAvailable, Source: "golden"},
				Landlock:          platformlinux.FeatureReport{State: platformlinux.FeatureAvailable, Source: "golden"},
				Seccomp:           platformlinux.FeatureReport{State: platformlinux.FeatureAvailable, Source: "golden"},
				NetworkNamespaces: platformlinux.FeatureReport{State: platformlinux.FeatureAvailable, Source: "golden"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Compile fixture: %v", err)
	}
	assertGoldenJSON(t, "launch/linux-native.json", spec)
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
			MountNamespaces:   platformlinux.FeatureReport{State: platformlinux.FeatureUnknown, Source: "mntns"},
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
		GapNativeLaunchHelperMissing,
		GapRuntimeNotLinux,
		GapUserNamespaceUnavailable,
		GapMountNamespaceUnavailable,
		GapLandlockUnavailable,
		GapNetworkNamespaceUnavailable,
	} {
		if !hasGap(spec.CapabilityGaps, code) {
			t.Fatalf("CapabilityGaps missing %q: %+v", code, spec.CapabilityGaps)
		}
	}
	if hasGap(spec.CapabilityGaps, GapCgroupV2Unavailable) || hasGap(spec.CapabilityGaps, GapSeccompUnavailable) {
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

func goldenCredentialFloor(t *testing.T, home string) containment.CredentialFloor {
	t.Helper()
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{
		{Path: home + "/.ssh"},
		{Path: home + "/.aws"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return floor
}

func availableFeature() platformlinux.FeatureReport {
	return platformlinux.FeatureReport{State: platformlinux.FeatureAvailable}
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

func hasGap(gaps []CapabilityGap, code string) bool {
	for _, gap := range gaps {
		if gap.Code == code {
			return true
		}
	}
	return false
}

func assertGoldenJSON(t *testing.T, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	assertGolden(t, name, prettyJSON(t, data)+"\n")
}

func prettyJSON(t *testing.T, data []byte) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("unmarshal JSON: %v\n%s", err, string(data))
	}
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal indent JSON: %v", err)
	}
	return string(out)
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", filepath.FromSlash(name))
	if *updateGoldenBaselines {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nRun `go test ./containment/linux -update-golden` from hazmat/ to refresh baselines.", path, err)
	}
	if got != string(want) {
		t.Fatalf("%s changed; run `go test ./containment/linux -update-golden` only after reviewing the diff.\n--- want\n%s\n--- got\n%s", name, string(want), got)
	}
}
