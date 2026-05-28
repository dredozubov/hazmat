package linux

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
