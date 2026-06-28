package linux

import (
	"fmt"
	"strings"

	linuxspec "hazmat/containment/linux"
	platformlinux "hazmat/platform/linux"
	"hazmat/sessionmeta"
)

type Stage string

const (
	StageValidated  Stage = "validated"
	StageFDSClosed  Stage = "fds_closed"
	StageNamespaces Stage = "namespaces"
	StageMounts     Stage = "mounts"
	StageNetwork    Stage = "network"
	StagePrivileges Stage = "privileges"
	StageNoNewPrivs Stage = "no_new_privs"
	StageLandlock   Stage = "landlock"
	StageSeccomp    Stage = "seccomp"
	StageMetadata   Stage = "metadata"
	StageExec       Stage = "exec"
)

type FDClosurePlan struct {
	Preserve []int `json:"preserve"`
	CloseMin int   `json:"close_min"`
}

type NamespacePlan struct {
	User    bool `json:"user"`
	Mount   bool `json:"mount"`
	Network bool `json:"network"`
}

type NetworkAdmission struct {
	Mode            sessionmeta.NetworkMode `json:"mode"`
	EgressFiltering bool                    `json:"egress_filtering"`
	Detail          string                  `json:"detail"`
}

type IdentityAdmission struct {
	Lane           linuxspec.IdentityLane   `json:"lane"`
	HelperStrategy linuxspec.HelperStrategy `json:"helper_strategy"`
	RunAs          string                   `json:"run_as"`
	DropToAgent    bool                     `json:"drop_to_agent"`
}

type AdmissionPlan struct {
	Stages     []Stage           `json:"stages"`
	Identity   IdentityAdmission `json:"identity"`
	FDs        FDClosurePlan     `json:"fds"`
	Namespaces NamespacePlan     `json:"namespaces"`
	Network    NetworkAdmission  `json:"network"`
	Policy     PolicyPlan        `json:"policy"`
	Metadata   bool              `json:"metadata"`
	Exec       bool              `json:"exec"`
}

type GapError struct {
	Gaps []linuxspec.CapabilityGap
}

func (e GapError) Error() string {
	if len(e.Gaps) == 0 {
		return "linux admission gaps"
	}
	codes := make([]string, 0, len(e.Gaps))
	for _, gap := range e.Gaps {
		codes = append(codes, gap.Code)
	}
	return "linux admission gaps: " + strings.Join(codes, ", ")
}

func AdmitCurrentUser(spec linuxspec.LaunchSpec, report platformlinux.Report) (AdmissionPlan, error) {
	var plan AdmissionPlan
	if err := validateCurrentUserSpec(spec); err != nil {
		return plan, err
	}
	plan = initializedAdmissionPlan(spec, "current-user", false)
	if gaps := admissionGaps(spec, report); len(gaps) > 0 {
		return plan, GapError{Gaps: gaps}
	}

	plan.FDs = FDClosurePlan{Preserve: []int{0, 1, 2}, CloseMin: 3}
	plan.Namespaces = NamespacePlan{
		User:    true,
		Mount:   true,
		Network: spec.Network.UseNetworkNamespace,
	}
	plan.Network = networkAdmission(spec.Network, "current user")
	policy, err := BuildPolicyPlan(spec)
	if err != nil {
		return plan, err
	}
	plan.Policy = policy
	plan.Metadata = true
	plan.Exec = true
	plan.Stages = append(plan.Stages,
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
	)
	return plan, nil
}

func AdmitAgentUser(spec linuxspec.LaunchSpec, report platformlinux.Report) (AdmissionPlan, error) {
	var plan AdmissionPlan
	if err := validateAgentUserSpec(spec); err != nil {
		return plan, err
	}
	plan = initializedAdmissionPlan(spec, "agent", true)
	if gaps := agentUserAdmissionGaps(report); len(gaps) > 0 {
		return plan, GapError{Gaps: gaps}
	}

	plan.FDs = FDClosurePlan{Preserve: []int{0, 1, 2}, CloseMin: 3}
	plan.Namespaces = NamespacePlan{
		User:    false,
		Mount:   true,
		Network: spec.Network.UseNetworkNamespace,
	}
	plan.Network = networkAdmission(spec.Network, "agent user")
	policy, err := BuildPolicyPlan(spec)
	if err != nil {
		return plan, err
	}
	plan.Policy = policy
	plan.Metadata = true
	plan.Exec = true
	plan.Stages = append(plan.Stages,
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
	)
	return plan, nil
}

func initializedAdmissionPlan(spec linuxspec.LaunchSpec, runAs string, dropToAgent bool) AdmissionPlan {
	return AdmissionPlan{
		Stages: []Stage{StageValidated},
		Identity: IdentityAdmission{
			Lane:           spec.Identity,
			HelperStrategy: spec.HelperStrategy,
			RunAs:          runAs,
			DropToAgent:    dropToAgent,
		},
	}
}

func validateCurrentUserSpec(spec linuxspec.LaunchSpec) error {
	if spec.FormatVersion != linuxspec.LaunchSpecFormatVersion {
		return fmt.Errorf("linux current-user admission requires launch spec format %d", linuxspec.LaunchSpecFormatVersion)
	}
	if spec.Backend != linuxspec.BackendLinuxNative {
		return fmt.Errorf("linux current-user admission requires backend %q", linuxspec.BackendLinuxNative)
	}
	if spec.Identity != linuxspec.IdentityCurrentUser {
		return fmt.Errorf("linux current-user admission requires identity %q", linuxspec.IdentityCurrentUser)
	}
	if spec.HelperStrategy != linuxspec.HelperRootlessUserNS {
		return fmt.Errorf("linux current-user admission requires helper_strategy %q", linuxspec.HelperRootlessUserNS)
	}
	switch spec.Phase {
	case linuxspec.PhasePlanOnly, linuxspec.PhaseExperimental:
	default:
		return fmt.Errorf("linux current-user admission requires phase %q or %q", linuxspec.PhasePlanOnly, linuxspec.PhaseExperimental)
	}
	return nil
}

func validateAgentUserSpec(spec linuxspec.LaunchSpec) error {
	if spec.FormatVersion != linuxspec.LaunchSpecFormatVersion {
		return fmt.Errorf("linux agent-user admission requires launch spec format %d", linuxspec.LaunchSpecFormatVersion)
	}
	if spec.Backend != linuxspec.BackendLinuxNative {
		return fmt.Errorf("linux agent-user admission requires backend %q", linuxspec.BackendLinuxNative)
	}
	if spec.Identity != linuxspec.IdentityAgentUser {
		return fmt.Errorf("linux agent-user admission requires identity %q", linuxspec.IdentityAgentUser)
	}
	if spec.HelperStrategy != linuxspec.HelperRoot {
		return fmt.Errorf("linux agent-user admission requires helper_strategy %q", linuxspec.HelperRoot)
	}
	switch spec.Phase {
	case linuxspec.PhasePlanOnly, linuxspec.PhaseExperimental:
	default:
		return fmt.Errorf("linux agent-user admission requires phase %q or %q", linuxspec.PhasePlanOnly, linuxspec.PhaseExperimental)
	}
	return nil
}

func admissionGaps(spec linuxspec.LaunchSpec, report platformlinux.Report) []linuxspec.CapabilityGap {
	var gaps []linuxspec.CapabilityGap
	if report.RuntimeOS != "linux" {
		gaps = append(gaps, linuxspec.CapabilityGap{
			Code:    linuxspec.GapRuntimeNotLinux,
			Message: "inspected runtime is not Linux",
			State:   report.RuntimeOS,
		})
	}
	for _, requirement := range []struct {
		feature platformlinux.FeatureReport
		code    string
		message string
	}{
		{feature: report.Features.UserNamespaces, code: linuxspec.GapUserNamespaceUnavailable, message: "user namespace support is required for current-user Linux admission"},
		{feature: report.Features.MountNamespaces, code: linuxspec.GapMountNamespaceUnavailable, message: "mount namespace support is required for current-user Linux admission"},
		{feature: report.Features.Landlock, code: linuxspec.GapLandlockUnavailable, message: "Landlock support is required for current-user Linux admission"},
		{feature: report.Features.Seccomp, code: linuxspec.GapSeccompUnavailable, message: "seccomp support is required for current-user Linux admission"},
	} {
		if requirement.feature.State != platformlinux.FeatureAvailable {
			gaps = append(gaps, linuxspec.CapabilityGap{
				Code:    requirement.code,
				Message: requirement.message,
				Source:  requirement.feature.Source,
				State:   string(requirement.feature.State),
			})
		}
	}
	if spec.Network.UseNetworkNamespace && report.Features.NetworkNamespaces.State != platformlinux.FeatureAvailable {
		gaps = append(gaps, linuxspec.CapabilityGap{
			Code:    linuxspec.GapNetworkNamespaceUnavailable,
			Message: "network namespace support is required for network=none",
			Source:  report.Features.NetworkNamespaces.Source,
			State:   string(report.Features.NetworkNamespaces.State),
		})
	}
	return gaps
}

func agentUserAdmissionGaps(report platformlinux.Report) []linuxspec.CapabilityGap {
	var gaps []linuxspec.CapabilityGap
	if report.RuntimeOS != "linux" {
		gaps = append(gaps, linuxspec.CapabilityGap{
			Code:    linuxspec.GapRuntimeNotLinux,
			Message: "inspected runtime is not Linux",
			State:   report.RuntimeOS,
		})
	}
	if report.Features.CgroupV2.State != platformlinux.FeatureAvailable {
		gaps = append(gaps, linuxspec.CapabilityGap{
			Code:    linuxspec.GapCgroupV2Unavailable,
			Message: "cgroup v2 support is required for agent-user Linux admission",
			Source:  report.Features.CgroupV2.Source,
			State:   string(report.Features.CgroupV2.State),
		})
	}
	for _, gap := range report.AgentUserBackend.CapabilityGaps {
		gaps = append(gaps, linuxspec.CapabilityGap{
			Code:    gap.ID,
			Message: gap.Message,
			State:   gap.State,
		})
	}
	return dedupeGaps(gaps)
}

func dedupeGaps(gaps []linuxspec.CapabilityGap) []linuxspec.CapabilityGap {
	if len(gaps) < 2 {
		return gaps
	}
	seen := make(map[string]struct{}, len(gaps))
	out := make([]linuxspec.CapabilityGap, 0, len(gaps))
	for _, gap := range gaps {
		if _, ok := seen[gap.Code]; ok {
			continue
		}
		seen[gap.Code] = struct{}{}
		out = append(out, gap)
	}
	return out
}

func networkAdmission(spec linuxspec.NetworkSpec, identity string) NetworkAdmission {
	if spec.UseNetworkNamespace {
		return NetworkAdmission{
			Mode:            spec.Mode,
			EgressFiltering: true,
			Detail:          "network namespace with loopback only and no external route",
		}
	}
	return NetworkAdmission{
		Mode:            spec.Mode,
		EgressFiltering: false,
		Detail:          "host network as " + identity + "; no egress filtering is claimed",
	}
}
