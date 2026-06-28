package linux

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"hazmat/runtimeprovider"
)

func TestInspectReportsDetectedLinuxFeatures(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "/etc/os-release", `ID=ubuntu
ID_LIKE="debian"
NAME="Ubuntu"
PRETTY_NAME="Ubuntu 24.04.2 LTS"
VERSION_ID="24.04"
VERSION_CODENAME=noble
`)
	writeFile(t, root, "/proc/sys/kernel/osrelease", "6.8.0-64-generic\n")
	writeFile(t, root, "/proc/sys/kernel/unprivileged_userns_clone", "1\n")
	writeFile(t, root, "/sys/fs/cgroup/cgroup.controllers", "cpu memory pids\n")
	writeFile(t, root, "/sys/kernel/security/landlock/abi", "5\n")
	writeFile(t, root, "/proc/sys/kernel/seccomp/actions_avail", "kill_process kill_thread errno trap log allow\n")
	writeFile(t, root, "/proc/self/ns/mnt", "")
	writeFile(t, root, "/proc/self/ns/net", "")

	report := Inspect(InspectOptions{Root: root, RuntimeOS: "linux"})
	if report.RuntimeOS != "linux" {
		t.Fatalf("RuntimeOS = %q", report.RuntimeOS)
	}
	if report.Distro.ID != "ubuntu" || report.Distro.VersionCodename != "noble" {
		t.Fatalf("Distro = %+v", report.Distro)
	}
	if len(report.Distro.IDLike) != 1 || report.Distro.IDLike[0] != "debian" {
		t.Fatalf("Distro.IDLike = %v", report.Distro.IDLike)
	}
	if report.Kernel.Release != "6.8.0-64-generic" {
		t.Fatalf("Kernel.Release = %q", report.Kernel.Release)
	}
	for name, feature := range map[string]FeatureReport{
		"user_namespaces":    report.Features.UserNamespaces,
		"mount_namespaces":   report.Features.MountNamespaces,
		"cgroup_v2":          report.Features.CgroupV2,
		"landlock":           report.Features.Landlock,
		"seccomp":            report.Features.Seccomp,
		"network_namespaces": report.Features.NetworkNamespaces,
	} {
		if !feature.Available() {
			t.Fatalf("%s feature = %+v, want available", name, feature)
		}
	}
	if report.NativeBackend.Supported {
		t.Fatal("Linux native backend should remain unsupported")
	}
	if report.NativeBackend.Phase != "plan-only" || !report.NativeBackend.CapabilityOK {
		t.Fatalf("NativeBackend = %+v", report.NativeBackend)
	}
	if report.NativeBackend.Provider == nil ||
		report.NativeBackend.Provider.Provider != "linux-current-user" ||
		report.NativeBackend.Provider.Status != "plan-only" ||
		report.NativeBackend.Provider.Executable {
		t.Fatalf("NativeBackend.Provider = %+v, want linux-current-user plan-only", report.NativeBackend.Provider)
	}
	if !nativeBackendHasGap(report.NativeBackend, "linux.native-launch-helper-missing") {
		t.Fatalf("NativeBackend.CapabilityGaps = %+v, want native helper gap", report.NativeBackend.CapabilityGaps)
	}
	if report.AgentUserBackend.Phase != "setup-required" || !report.AgentUserBackend.CapabilityOK {
		t.Fatalf("AgentUserBackend = %+v, want setup-required with host capabilities ok", report.AgentUserBackend)
	}
	if report.AgentUserBackend.Provider == nil ||
		report.AgentUserBackend.Provider.Provider != "linux-agent-user" ||
		report.AgentUserBackend.Provider.Status != "setup-required" ||
		report.AgentUserBackend.Provider.Executable {
		t.Fatalf("AgentUserBackend.Provider = %+v, want linux-agent-user setup-required", report.AgentUserBackend.Provider)
	}
	for _, id := range []string{
		"linux.setup-required",
		"linux.native-launch-helper-missing",
	} {
		if !nativeBackendHasGap(report.AgentUserBackend, id) {
			t.Fatalf("AgentUserBackend.CapabilityGaps missing %q: %+v", id, report.AgentUserBackend.CapabilityGaps)
		}
	}
	if rendered := strings.Join(runtimeprovider.RenderGaps(report.AgentUserBackend.CapabilityGaps), "\n"); !strings.Contains(rendered, "linux.setup-required:") {
		t.Fatalf("rendered agent-user gaps missing setup-required text:\n%s", rendered)
	}
	if len(report.NativeBackend.Reasons) == 0 {
		t.Fatal("NativeBackend.Reasons is empty")
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("Report should be JSON encodable: %v", err)
	}
}

func TestInspectReportsCapabilityGaps(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "/usr/lib/os-release", `ID=debian
NAME=Debian
`)
	writeFile(t, root, "/proc/sys/kernel/unprivileged_userns_clone", "0\n")
	writeFile(t, root, "/proc/filesystems", "nodev\tcgroup2\nnodev\tnsfs\n")
	writeFile(t, root, "/proc/self/status", "Name:\ttest\nSeccomp:\t2\n")

	report := Inspect(InspectOptions{Root: root, RuntimeOS: "darwin"})
	if report.Distro.ID != "debian" {
		t.Fatalf("Distro.ID = %q", report.Distro.ID)
	}
	if report.Features.UserNamespaces.State != FeatureUnavailable {
		t.Fatalf("UserNamespaces = %+v, want unavailable", report.Features.UserNamespaces)
	}
	if report.Features.CgroupV2.State != FeatureUnknown {
		t.Fatalf("CgroupV2 = %+v, want unknown without mounted hierarchy", report.Features.CgroupV2)
	}
	if report.Features.Landlock.State != FeatureUnknown {
		t.Fatalf("Landlock = %+v, want unknown", report.Features.Landlock)
	}
	if report.Features.Seccomp.State != FeatureAvailable {
		t.Fatalf("Seccomp = %+v, want available from proc status", report.Features.Seccomp)
	}
	if report.Features.MountNamespaces.State != FeatureUnknown {
		t.Fatalf("MountNamespaces = %+v, want unknown from nsfs only", report.Features.MountNamespaces)
	}
	if report.Features.NetworkNamespaces.State != FeatureUnknown {
		t.Fatalf("NetworkNamespaces = %+v, want unknown from nsfs only", report.Features.NetworkNamespaces)
	}
	if report.NativeBackend.CapabilityOK {
		t.Fatalf("NativeBackend.CapabilityOK = true for gaps: %+v", report.NativeBackend)
	}
	foundRuntimeReason := false
	for _, reason := range report.NativeBackend.Reasons {
		if reason == "inspected runtime is darwin, not linux" {
			foundRuntimeReason = true
			break
		}
	}
	if !foundRuntimeReason {
		t.Fatalf("NativeBackend.Reasons missing runtime reason: %v", report.NativeBackend.Reasons)
	}
	for _, id := range []string{
		"linux.runtime-not-linux",
		"linux.user-namespace-unavailable",
		"linux.cgroup-v2-unavailable",
		"linux.landlock-unavailable",
		"linux.mount-namespace-unavailable",
		"linux.network-namespace-unavailable",
	} {
		if !nativeBackendHasGap(report.NativeBackend, id) {
			t.Fatalf("NativeBackend.CapabilityGaps missing %q: %+v", id, report.NativeBackend.CapabilityGaps)
		}
	}
}

func TestInspectReportsAgentUserSetupAndStrategyGaps(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "/etc/os-release", `ID=gentoo
NAME=Gentoo
`)
	writeFile(t, root, "/proc/sys/kernel/unprivileged_userns_clone", "1\n")
	writeFile(t, root, "/sys/kernel/security/landlock/abi", "5\n")
	writeFile(t, root, "/proc/sys/kernel/seccomp/actions_avail", "kill_process allow\n")
	writeFile(t, root, "/proc/self/ns/mnt", "")
	writeFile(t, root, "/proc/self/ns/net", "")

	report := Inspect(InspectOptions{
		Root:                    root,
		RuntimeOS:               "linux",
		AgentUserHelperStrategy: HelperRootlessUserNS,
	})
	if report.AgentUserBackend.CapabilityOK {
		t.Fatalf("AgentUserBackend.CapabilityOK = true for gaps: %+v", report.AgentUserBackend)
	}
	for _, id := range []string{
		"linux.setup-required",
		"linux.helper-strategy-unsupported",
		"linux.cgroup-v2-unavailable",
		"linux.distro-unsupported",
	} {
		if !nativeBackendHasGap(report.AgentUserBackend, id) {
			t.Fatalf("AgentUserBackend.CapabilityGaps missing %q: %+v", id, report.AgentUserBackend.CapabilityGaps)
		}
	}
	rendered := strings.Join(runtimeprovider.RenderGaps(report.AgentUserBackend.CapabilityGaps), "\n")
	if !strings.Contains(rendered, "linux.helper-strategy-unsupported:") ||
		strings.Contains(rendered, "fall back") {
		t.Fatalf("rendered agent-user gaps should name strategy refusal without fallback advice:\n%s", rendered)
	}
}

func TestInspectDoesNotMutateFixtureRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "/etc/os-release", "ID=ubuntu\n")
	writeFile(t, root, "/proc/sys/kernel/unprivileged_userns_clone", "1\n")
	writeFile(t, root, "/sys/fs/cgroup/cgroup.controllers", "cpu memory pids\n")

	before := snapshotFiles(t, root)
	_ = Inspect(InspectOptions{Root: root, RuntimeOS: "linux"})
	after := snapshotFiles(t, root)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("Inspect mutated fixture root:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestInspectReportsDistroVariants(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		data string
		want DistroInfo
	}{
		{
			name: "ubuntu",
			path: "/etc/os-release",
			data: `ID=ubuntu
ID_LIKE="debian"
NAME="Ubuntu"
PRETTY_NAME="Ubuntu 24.04.2 LTS"
VERSION_ID="24.04"
VERSION_CODENAME=noble
`,
			want: DistroInfo{ID: "ubuntu", IDLike: []string{"debian"}, Name: "Ubuntu", PrettyName: "Ubuntu 24.04.2 LTS", VersionID: "24.04", VersionCodename: "noble"},
		},
		{
			name: "debian",
			path: "/usr/lib/os-release",
			data: `ID=debian
NAME="Debian GNU/Linux"
VERSION_ID="12"
VERSION_CODENAME=bookworm
`,
			want: DistroInfo{ID: "debian", IDLike: []string{}, Name: "Debian GNU/Linux", VersionID: "12", VersionCodename: "bookworm"},
		},
		{
			name: "fedora",
			path: "/etc/os-release",
			data: `ID=fedora
ID_LIKE="rhel centos"
NAME="Fedora Linux"
VERSION_ID="40"
`,
			want: DistroInfo{ID: "fedora", IDLike: []string{"rhel", "centos"}, Name: "Fedora Linux", VersionID: "40"},
		},
		{
			name: "arch",
			path: "/etc/os-release",
			data: `ID=arch
NAME="Arch Linux"
PRETTY_NAME="Arch Linux"
`,
			want: DistroInfo{ID: "arch", IDLike: []string{}, Name: "Arch Linux", PrettyName: "Arch Linux"},
		},
		{
			name: "unknown",
			want: DistroInfo{IDLike: []string{}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.path != "" {
				writeFile(t, root, tc.path, tc.data)
			}

			report := Inspect(InspectOptions{Root: root, RuntimeOS: "linux"})
			if !reflect.DeepEqual(report.Distro, tc.want) {
				t.Fatalf("Distro = %#v, want %#v", report.Distro, tc.want)
			}
		})
	}
}

func snapshotFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return files
}

func writeFile(t *testing.T, root, path, data string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", full, err)
	}
}

func nativeBackendHasGap(status NativeBackendStatus, id string) bool {
	for _, gap := range status.CapabilityGaps {
		if gap.ID == id {
			return true
		}
	}
	return false
}
