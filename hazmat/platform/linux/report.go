// Package linux reports side-effect-free Linux platform facts for Hazmat
// planning. It does not perform setup, create namespaces, or launch helpers.
package linux

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"hazmat/runtimeprovider"
)

// FeatureState describes whether a Linux primitive is usable by a future
// native backend from the inspected host facts.
type FeatureState string

const (
	FeatureAvailable   FeatureState = "available"
	FeatureUnavailable FeatureState = "unavailable"
	FeatureUnknown     FeatureState = "unknown"
)

const (
	GapNativeLaunchHelperMissing   = "linux.native-launch-helper-missing"
	GapRuntimeNotLinux             = "linux.runtime-not-linux"
	GapUserNamespaceUnavailable    = "linux.user-namespace-unavailable"
	GapMountNamespaceUnavailable   = "linux.mount-namespace-unavailable"
	GapCgroupV2Unavailable         = "linux.cgroup-v2-unavailable"
	GapLandlockUnavailable         = "linux.landlock-unavailable"
	GapSeccompUnavailable          = "linux.seccomp-unavailable"
	GapNetworkNamespaceUnavailable = "linux.network-namespace-unavailable"
	GapSetupRequired               = "linux.setup-required"
	GapHelperStrategyUnsupported   = "linux.helper-strategy-unsupported"
	GapDistroUnsupported           = "linux.distro-unsupported"
)

type HelperStrategy string

const (
	HelperRootlessUserNS HelperStrategy = "rootless-userns"
	HelperRoot           HelperStrategy = "root-helper"
)

// FeatureReport describes one Linux kernel or host capability.
type FeatureReport struct {
	State  FeatureState `json:"state"`
	Detail string       `json:"detail,omitempty"`
	Source string       `json:"source,omitempty"`
}

// Available reports whether the feature was positively detected.
func (f FeatureReport) Available() bool {
	return f.State == FeatureAvailable
}

// DistroInfo contains /etc/os-release identity fields.
type DistroInfo struct {
	ID              string   `json:"id,omitempty"`
	IDLike          []string `json:"id_like,omitempty"`
	Name            string   `json:"name,omitempty"`
	PrettyName      string   `json:"pretty_name,omitempty"`
	VersionID       string   `json:"version_id,omitempty"`
	VersionCodename string   `json:"version_codename,omitempty"`
}

// KernelInfo contains kernel facts relevant to capability planning.
type KernelInfo struct {
	Release string `json:"release,omitempty"`
}

// FeatureSet groups Linux primitives Hazmat cares about for native launch
// planning.
type FeatureSet struct {
	UserNamespaces    FeatureReport `json:"user_namespaces"`
	MountNamespaces   FeatureReport `json:"mount_namespaces"`
	CgroupV2          FeatureReport `json:"cgroup_v2"`
	Landlock          FeatureReport `json:"landlock"`
	Seccomp           FeatureReport `json:"seccomp"`
	NetworkNamespaces FeatureReport `json:"network_namespaces"`
}

// NativeBackendStatus explains why a Linux native lane remains non-executable.
type NativeBackendStatus struct {
	Supported      bool                                  `json:"supported"`
	Phase          string                                `json:"phase"`
	Provider       *runtimeprovider.ProviderStatusRecord `json:"provider,omitempty"`
	Reasons        []string                              `json:"reasons,omitempty"`
	CapabilityGaps []runtimeprovider.GapRecord           `json:"capability_gaps,omitempty"`
	CapabilityOK   bool                                  `json:"capability_ok"`
}

// Report is the reusable JSON-friendly Linux platform inspection result.
type Report struct {
	RuntimeOS        string              `json:"runtime_os"`
	Distro           DistroInfo          `json:"distro,omitempty"`
	Kernel           KernelInfo          `json:"kernel,omitempty"`
	Features         FeatureSet          `json:"features"`
	NativeBackend    NativeBackendStatus `json:"native_backend"`
	AgentUserBackend NativeBackendStatus `json:"agent_user_backend"`
}

// InspectOptions configures side-effect-free host inspection. Root is useful
// for tests and container images; leave it empty for the host root.
type InspectOptions struct {
	Root                    string
	RuntimeOS               string
	AgentUserHelperStrategy HelperStrategy
}

// InspectHost inspects the current host without performing setup or launch.
func InspectHost() Report {
	return Inspect(InspectOptions{
		Root:      string(os.PathSeparator),
		RuntimeOS: runtime.GOOS,
	})
}

// Inspect returns a Linux platform report from read-only filesystem probes.
func Inspect(opts InspectOptions) Report {
	root := opts.Root
	if root == "" {
		root = string(os.PathSeparator)
	}
	runtimeOS := strings.TrimSpace(opts.RuntimeOS)
	if runtimeOS == "" {
		runtimeOS = runtime.GOOS
	}

	report := Report{
		RuntimeOS: runtimeOS,
		Distro:    inspectDistro(root),
		Kernel: KernelInfo{
			Release: strings.TrimSpace(readFileString(root, "/proc/sys/kernel/osrelease")),
		},
	}
	report.Features = FeatureSet{
		UserNamespaces:    inspectUserNamespaces(root),
		MountNamespaces:   inspectMountNamespaces(root),
		CgroupV2:          inspectCgroupV2(root),
		Landlock:          inspectLandlock(root),
		Seccomp:           inspectSeccomp(root),
		NetworkNamespaces: inspectNetworkNamespaces(root),
	}
	report.NativeBackend = nativeBackendStatus(runtimeOS, report.Distro, report.Features)
	report.AgentUserBackend = agentUserBackendStatus(runtimeOS, report.Distro, report.Features, opts.AgentUserHelperStrategy)
	return report
}

func inspectDistro(root string) DistroInfo {
	values := readOSRelease(root, "/etc/os-release")
	if len(values) == 0 {
		values = readOSRelease(root, "/usr/lib/os-release")
	}
	idLike := strings.Fields(values["ID_LIKE"])
	return DistroInfo{
		ID:              values["ID"],
		IDLike:          idLike,
		Name:            values["NAME"],
		PrettyName:      values["PRETTY_NAME"],
		VersionID:       values["VERSION_ID"],
		VersionCodename: values["VERSION_CODENAME"],
	}
}

func inspectUserNamespaces(root string) FeatureReport {
	const source = "/proc/sys/kernel/unprivileged_userns_clone"
	raw, ok := readFile(root, source)
	if ok {
		switch strings.TrimSpace(string(raw)) {
		case "1":
			return FeatureReport{State: FeatureAvailable, Detail: "unprivileged user namespace creation is enabled", Source: source}
		case "0":
			return FeatureReport{State: FeatureUnavailable, Detail: "unprivileged user namespace creation is disabled", Source: source}
		default:
			return FeatureReport{State: FeatureUnknown, Detail: "unrecognized unprivileged_userns_clone value", Source: source}
		}
	}
	if exists(root, "/proc/self/uid_map") {
		return FeatureReport{State: FeatureUnknown, Detail: "kernel exposes user namespace state, but unprivileged creation policy was not found", Source: "/proc/self/uid_map"}
	}
	return FeatureReport{State: FeatureUnknown, Detail: "unprivileged user namespace policy was not found", Source: source}
}

func inspectCgroupV2(root string) FeatureReport {
	const controllers = "/sys/fs/cgroup/cgroup.controllers"
	if raw, ok := readFile(root, controllers); ok {
		detail := "unified cgroup v2 hierarchy is mounted"
		if fields := strings.Fields(string(raw)); len(fields) > 0 {
			detail = "unified cgroup v2 hierarchy is mounted with controllers: " + strings.Join(fields, ",")
		}
		return FeatureReport{State: FeatureAvailable, Detail: detail, Source: controllers}
	}
	if procFilesystemsContains(root, "cgroup2") {
		return FeatureReport{State: FeatureUnknown, Detail: "kernel reports cgroup2 support, but unified hierarchy mount was not found", Source: "/proc/filesystems"}
	}
	return FeatureReport{State: FeatureUnavailable, Detail: "cgroup2 filesystem support was not detected", Source: "/proc/filesystems"}
}

func inspectLandlock(root string) FeatureReport {
	const source = "/sys/kernel/security/landlock/abi"
	raw, ok := readFile(root, source)
	if !ok {
		return FeatureReport{State: FeatureUnknown, Detail: "Landlock ABI file was not found", Source: source}
	}
	abi, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return FeatureReport{State: FeatureUnknown, Detail: "Landlock ABI value is not numeric", Source: source}
	}
	if abi <= 0 {
		return FeatureReport{State: FeatureUnavailable, Detail: fmt.Sprintf("Landlock ABI version %d is not usable", abi), Source: source}
	}
	return FeatureReport{State: FeatureAvailable, Detail: fmt.Sprintf("Landlock ABI version %d", abi), Source: source}
}

func inspectSeccomp(root string) FeatureReport {
	const actions = "/proc/sys/kernel/seccomp/actions_avail"
	if raw, ok := readFile(root, actions); ok {
		fields := strings.Fields(string(raw))
		if len(fields) > 0 {
			return FeatureReport{State: FeatureAvailable, Detail: "seccomp actions available: " + strings.Join(fields, ","), Source: actions}
		}
		return FeatureReport{State: FeatureUnknown, Detail: "seccomp actions file was empty", Source: actions}
	}
	if status := readFileString(root, "/proc/self/status"); strings.Contains(status, "\nSeccomp:") || strings.HasPrefix(status, "Seccomp:") {
		return FeatureReport{State: FeatureAvailable, Detail: "process status exposes seccomp mode", Source: "/proc/self/status"}
	}
	return FeatureReport{State: FeatureUnknown, Detail: "seccomp support was not detected from procfs", Source: actions}
}

func inspectMountNamespaces(root string) FeatureReport {
	const source = "/proc/self/ns/mnt"
	if exists(root, source) {
		return FeatureReport{State: FeatureAvailable, Detail: "current process has a mount namespace handle", Source: source}
	}
	if procFilesystemsContains(root, "nsfs") {
		return FeatureReport{State: FeatureUnknown, Detail: "kernel reports namespace filesystem support, but current mount namespace handle was not found", Source: "/proc/filesystems"}
	}
	return FeatureReport{State: FeatureUnknown, Detail: "mount namespace handle was not found", Source: source}
}

func inspectNetworkNamespaces(root string) FeatureReport {
	const source = "/proc/self/ns/net"
	if exists(root, source) {
		return FeatureReport{State: FeatureAvailable, Detail: "current process has a network namespace handle", Source: source}
	}
	if procFilesystemsContains(root, "nsfs") {
		return FeatureReport{State: FeatureUnknown, Detail: "kernel reports namespace filesystem support, but current network namespace handle was not found", Source: "/proc/filesystems"}
	}
	return FeatureReport{State: FeatureUnknown, Detail: "network namespace handle was not found", Source: source}
}

func nativeBackendStatus(runtimeOS string, distro DistroInfo, features FeatureSet) NativeBackendStatus {
	provider := providerStatus(runtimeprovider.KindLinuxCurrentUser)
	status := NativeBackendStatus{
		Supported: false,
		Phase:     string(runtimeprovider.StatusPlanOnly),
		Provider:  &provider,
		Reasons: []string{
			"Linux native launch helper is not implemented yet",
			"Linux current-user runtime remains plan-only until helper admission and VM smoke evidence land",
		},
		CapabilityGaps: []runtimeprovider.GapRecord{
			runtimeprovider.MustGapRecord(
				runtimeprovider.KindLinuxCurrentUser,
				runtimeprovider.StatusPlanOnly,
				GapNativeLaunchHelperMissing,
				"Linux native launch helper is not implemented yet",
				"plan-only",
			),
		},
	}
	if runtimeOS != "linux" {
		status.Reasons = append(status.Reasons, "inspected runtime is "+runtimeOS+", not linux")
		status.CapabilityGaps = append(status.CapabilityGaps, runtimeprovider.MustGapRecord(
			runtimeprovider.KindLinuxCurrentUser,
			runtimeprovider.StatusPlanOnly,
			GapRuntimeNotLinux,
			"the inspected runtime is not Linux",
			runtimeOS,
		))
	}
	status.CapabilityOK = true
	for _, requirement := range nativeFeatureRequirements(features) {
		if requirement.feature.State != FeatureAvailable {
			status.CapabilityOK = false
			status.CapabilityGaps = append(status.CapabilityGaps, runtimeprovider.MustGapRecord(
				runtimeprovider.KindLinuxCurrentUser,
				runtimeprovider.StatusPlanOnly,
				requirement.id,
				requirement.message,
				string(requirement.feature.State),
			))
		}
	}
	if gap, ok := distroGap(runtimeprovider.KindLinuxCurrentUser, runtimeprovider.StatusPlanOnly, distro); ok {
		status.CapabilityOK = false
		status.CapabilityGaps = append(status.CapabilityGaps, gap)
	}
	return status
}

func agentUserBackendStatus(runtimeOS string, distro DistroInfo, features FeatureSet, helperStrategy HelperStrategy) NativeBackendStatus {
	provider := providerStatus(runtimeprovider.KindLinuxAgentUser)
	status := NativeBackendStatus{
		Supported: false,
		Phase:     string(runtimeprovider.StatusSetupRequired),
		Provider:  &provider,
		Reasons: []string{
			"Linux agent-user setup resources are missing or not verified",
			"Linux root-helper runtime is not implemented yet",
		},
		CapabilityGaps: []runtimeprovider.GapRecord{
			runtimeprovider.MustGapRecord(
				runtimeprovider.KindLinuxAgentUser,
				runtimeprovider.StatusSetupRequired,
				GapSetupRequired,
				"persistent Linux agent-user setup resources are missing",
				"setup-required",
			),
			runtimeprovider.MustGapRecord(
				runtimeprovider.KindLinuxAgentUser,
				runtimeprovider.StatusSetupRequired,
				GapNativeLaunchHelperMissing,
				"Linux agent-user root helper is not installed or verified",
				"setup-required",
			),
		},
		CapabilityOK: true,
	}
	if runtimeOS != "linux" {
		status.CapabilityOK = false
		status.Reasons = append(status.Reasons, "inspected runtime is "+runtimeOS+", not linux")
		status.CapabilityGaps = append(status.CapabilityGaps, runtimeprovider.MustGapRecord(
			runtimeprovider.KindLinuxAgentUser,
			runtimeprovider.StatusSetupRequired,
			GapRuntimeNotLinux,
			"the inspected runtime is not Linux",
			runtimeOS,
		))
	}
	if strategy := normalizeAgentUserHelperStrategy(helperStrategy); strategy != HelperRoot {
		status.CapabilityOK = false
		status.CapabilityGaps = append(status.CapabilityGaps, runtimeprovider.MustGapRecord(
			runtimeprovider.KindLinuxAgentUser,
			runtimeprovider.StatusSetupRequired,
			GapHelperStrategyUnsupported,
			"linux-agent-user requires root-helper",
			string(strategy),
		))
	}
	if features.CgroupV2.State != FeatureAvailable {
		status.CapabilityOK = false
		status.CapabilityGaps = append(status.CapabilityGaps, runtimeprovider.MustGapRecord(
			runtimeprovider.KindLinuxAgentUser,
			runtimeprovider.StatusSetupRequired,
			GapCgroupV2Unavailable,
			"resource controls cannot be attached",
			string(features.CgroupV2.State),
		))
	}
	if gap, ok := distroGap(runtimeprovider.KindLinuxAgentUser, runtimeprovider.StatusSetupRequired, distro); ok {
		status.CapabilityOK = false
		status.CapabilityGaps = append(status.CapabilityGaps, gap)
	}
	return status
}

func normalizeAgentUserHelperStrategy(strategy HelperStrategy) HelperStrategy {
	if strategy == "" {
		return HelperRoot
	}
	return strategy
}

type featureRequirement struct {
	feature FeatureReport
	id      string
	message string
}

func nativeFeatureRequirements(features FeatureSet) []featureRequirement {
	return []featureRequirement{
		{feature: features.UserNamespaces, id: GapUserNamespaceUnavailable, message: "selected strategy needs user namespaces and the host disables them"},
		{feature: features.MountNamespaces, id: GapMountNamespaceUnavailable, message: "helper cannot create the required mount namespace"},
		{feature: features.CgroupV2, id: GapCgroupV2Unavailable, message: "resource controls cannot be attached"},
		{feature: features.Landlock, id: GapLandlockUnavailable, message: "Landlock is unavailable and the spec did not accept the gap"},
		{feature: features.Seccomp, id: GapSeccompUnavailable, message: "seccomp is unavailable and the spec did not accept the gap"},
		{feature: features.NetworkNamespaces, id: GapNetworkNamespaceUnavailable, message: "--network none cannot be enforced"},
	}
}

func distroGap(provider runtimeprovider.Kind, status runtimeprovider.Status, distro DistroInfo) (runtimeprovider.GapRecord, bool) {
	id := strings.ToLower(strings.TrimSpace(distro.ID))
	if id == "" {
		return runtimeprovider.MustGapRecord(provider, status, GapDistroUnsupported, "Linux distro could not be identified", "unknown"), true
	}
	switch id {
	case "ubuntu", "debian", "fedora", "arch":
		return runtimeprovider.GapRecord{}, false
	default:
		return runtimeprovider.MustGapRecord(provider, status, GapDistroUnsupported, "Linux distro is not in the validated setup matrix", id), true
	}
}

func providerStatus(kind runtimeprovider.Kind) runtimeprovider.ProviderStatusRecord {
	if descriptor, ok := runtimeprovider.DescriptorForKind(kind); ok {
		return descriptor.StatusRecord()
	}
	return runtimeprovider.ProviderStatusRecord{Provider: kind, Status: runtimeprovider.StatusUnsupported}
}

func readOSRelease(root, path string) map[string]string {
	raw, ok := readFile(root, path)
	if !ok {
		return nil
	}
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = parseOSReleaseValue(strings.TrimSpace(value))
	}
	return values
}

func parseOSReleaseValue(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			if unquoted, err := strconv.Unquote(value); err == nil {
				return unquoted
			}
			return strings.Trim(value, `"'`)
		}
	}
	return value
}

func procFilesystemsContains(root, name string) bool {
	raw, ok := readFile(root, "/proc/filesystems")
	if !ok {
		return false
	}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 0 && fields[len(fields)-1] == name {
			return true
		}
	}
	return false
}

func readFileString(root, path string) string {
	raw, ok := readFile(root, path)
	if !ok {
		return ""
	}
	return string(raw)
}

func readFile(root, path string) ([]byte, bool) {
	data, err := os.ReadFile(rootedPath(root, path))
	if err != nil {
		return nil, false
	}
	return data, true
}

func exists(root, path string) bool {
	_, err := os.Stat(rootedPath(root, path))
	return err == nil
}

func rootedPath(root, path string) string {
	path = strings.TrimPrefix(path, string(os.PathSeparator))
	if root == "" || root == string(os.PathSeparator) {
		return string(os.PathSeparator) + path
	}
	return filepath.Join(root, path)
}
