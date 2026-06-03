// Package linuxspec compiles Hazmat's backend-neutral containment contract into
// a plan-only Linux native launch spec. It does not create namespaces, mount
// filesystems, or execute helpers.
package linuxspec

import (
	"encoding/json"

	"hazmat/containment"
	platformlinux "hazmat/platform/linux"
	"hazmat/sessionmeta"
)

const (
	// LaunchSpecFormatVersion is the current Linux launch-spec schema version.
	LaunchSpecFormatVersion = 1

	BackendLinuxNative = "linux-native"
	PhasePlanOnly      = "plan-only"
)

// CompileOptions supplies inspected host capability facts.
type CompileOptions struct {
	Platform platformlinux.Report
}

// LaunchSpec is a JSON-friendly Linux native launch plan. It is intentionally
// declarative so tests and future helpers can validate it before execution.
type LaunchSpec struct {
	FormatVersion    int                  `json:"format_version"`
	Backend          string               `json:"backend"`
	Phase            string               `json:"phase"`
	Mounts           []BindMount          `json:"mounts"`
	AgentHome        AgentHomeSpec        `json:"agent_home"`
	Temp             TempSpec             `json:"temp"`
	CredentialDenies []CredentialDenySpec `json:"credential_denies,omitempty"`
	Network          NetworkSpec          `json:"network"`
	Process          ProcessSpec          `json:"process"`
	CapabilityGaps   []CapabilityGap      `json:"capability_gaps,omitempty"`
}

// BindMount describes a bind mount the future launch helper should create.
type BindMount struct {
	Source string                 `json:"source"`
	Target string                 `json:"target"`
	Access containment.PathAccess `json:"access"`
}

// AgentHomeSpec describes the agent home visible inside the namespace.
type AgentHomeSpec struct {
	Path string `json:"path"`
}

// TempSpec describes session temp state. Tmpfs is preferred because it avoids
// cross-session durable state in the future Linux backend.
type TempSpec struct {
	Path  string `json:"path"`
	Tmpfs bool   `json:"tmpfs"`
}

// CredentialDenySpec records a path that the Linux compiler refused to cover
// with project/read/write mounts.
type CredentialDenySpec struct {
	Path string `json:"path"`
}

// NetworkSpec is the Linux-specific rendering of the network policy.
type NetworkSpec struct {
	Mode                sessionmeta.NetworkMode `json:"mode"`
	UseNetworkNamespace bool                    `json:"use_network_namespace"`
	Loopback            bool                    `json:"loopback"`
	DenyAllEgress       bool                    `json:"deny_all_egress"`
}

// ProcessSpec captures launch-helper invariants that must remain true before
// exec once Linux launch moves beyond plan-only.
type ProcessSpec struct {
	CloseInheritedFDs bool `json:"close_inherited_fds"`
	NoNewPrivs        bool `json:"no_new_privs"`
	DropCapabilities  bool `json:"drop_capabilities"`
}

// CapabilityGap records why a compiled plan is not yet executable.
type CapabilityGap struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
	State   string `json:"state,omitempty"`
}

// Compile converts a backend-neutral contract into a Linux launch spec without
// executing it.
func Compile(contract containment.Contract, opts CompileOptions) (LaunchSpec, error) {
	if err := validateContract(contract); err != nil {
		return LaunchSpec{}, err
	}

	spec := LaunchSpec{
		FormatVersion: LaunchSpecFormatVersion,
		Backend:       BackendLinuxNative,
		Phase:         PhasePlanOnly,
		Mounts:        compileMounts(contract),
		AgentHome: AgentHomeSpec{
			Path: contract.AgentHome.Path,
		},
		Temp: TempSpec{
			Path:  contract.Temp.Path,
			Tmpfs: true,
		},
		CredentialDenies: compileCredentialDenies(contract),
		Network:          compileNetwork(contract.Network.Mode),
		Process: ProcessSpec{
			CloseInheritedFDs: true,
			NoNewPrivs:        true,
			DropCapabilities:  true,
		},
		CapabilityGaps: capabilityGaps(opts.Platform),
	}
	return spec, nil
}

// MarshalJSON encodes a launch spec as indented JSON.
func MarshalJSON(spec LaunchSpec) ([]byte, error) {
	return json.MarshalIndent(spec, "", "  ")
}

func validateContract(contract containment.Contract) error {
	return contract.Validate()
}

func compileMounts(contract containment.Contract) []BindMount {
	mounts := []BindMount{{
		Source: contract.Project.Path,
		Target: contract.Project.Path,
		Access: containment.PathReadWrite,
	}}
	for _, path := range contract.EffectiveReadOnlyDirs() {
		mounts = append(mounts, BindMount{
			Source: path,
			Target: path,
			Access: containment.PathReadOnly,
		})
	}
	for _, path := range contract.EffectiveWritableDirs() {
		mounts = append(mounts, BindMount{
			Source: path,
			Target: path,
			Access: containment.PathReadWrite,
		})
	}
	return mounts
}

func compileCredentialDenies(contract containment.Contract) []CredentialDenySpec {
	paths := contract.CredentialDenyPaths()
	if len(paths) == 0 {
		return nil
	}
	denies := make([]CredentialDenySpec, 0, len(paths))
	for _, path := range paths {
		denies = append(denies, CredentialDenySpec{Path: path})
	}
	return denies
}

func compileNetwork(mode sessionmeta.NetworkMode) NetworkSpec {
	normalized := sessionmeta.NormalizeNetworkMode(mode)
	spec := NetworkSpec{Mode: normalized}
	if normalized == sessionmeta.NetworkNone {
		spec.UseNetworkNamespace = true
		spec.Loopback = true
		spec.DenyAllEgress = true
	}
	return spec
}

func capabilityGaps(report platformlinux.Report) []CapabilityGap {
	gaps := []CapabilityGap{{
		Code:    "linux-native-launch-helper-missing",
		Message: "Linux native launch helper is not implemented; spec is plan-only",
	}}
	if report.RuntimeOS != "" && report.RuntimeOS != "linux" {
		gaps = append(gaps, CapabilityGap{
			Code:    "runtime-not-linux",
			Message: "inspected runtime is not Linux",
			State:   report.RuntimeOS,
		})
	}
	for _, item := range []struct {
		code    string
		message string
		feature platformlinux.FeatureReport
	}{
		{"user-namespaces-unavailable", "user namespace support is not positively available", report.Features.UserNamespaces},
		{"cgroup-v2-unavailable", "cgroup v2 support is not positively available", report.Features.CgroupV2},
		{"landlock-unavailable", "Landlock support is not positively available", report.Features.Landlock},
		{"seccomp-unavailable", "seccomp support is not positively available", report.Features.Seccomp},
		{"network-namespaces-unavailable", "network namespace support is not positively available", report.Features.NetworkNamespaces},
	} {
		if item.feature.State != "" && item.feature.State != platformlinux.FeatureAvailable {
			gaps = append(gaps, CapabilityGap{
				Code:    item.code,
				Message: item.message,
				Source:  item.feature.Source,
				State:   string(item.feature.State),
			})
		}
	}
	return gaps
}
