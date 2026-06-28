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

type AdmissionPlan struct {
	Stages     []Stage          `json:"stages"`
	FDs        FDClosurePlan    `json:"fds"`
	Namespaces NamespacePlan    `json:"namespaces"`
	Network    NetworkAdmission `json:"network"`
	Metadata   bool             `json:"metadata"`
	Exec       bool             `json:"exec"`
}

type GapError struct {
	Gaps []linuxspec.CapabilityGap
}

func (e GapError) Error() string {
	if len(e.Gaps) == 0 {
		return "linux current-user admission gaps"
	}
	codes := make([]string, 0, len(e.Gaps))
	for _, gap := range e.Gaps {
		codes = append(codes, gap.Code)
	}
	return "linux current-user admission gaps: " + strings.Join(codes, ", ")
}

func AdmitCurrentUser(spec linuxspec.LaunchSpec, report platformlinux.Report) (AdmissionPlan, error) {
	var plan AdmissionPlan
	if err := validateCurrentUserSpec(spec); err != nil {
		return plan, err
	}
	plan.Stages = []Stage{StageValidated}
	if gaps := admissionGaps(spec, report); len(gaps) > 0 {
		return plan, GapError{Gaps: gaps}
	}

	plan.FDs = FDClosurePlan{Preserve: []int{0, 1, 2}, CloseMin: 3}
	plan.Namespaces = NamespacePlan{
		User:    true,
		Mount:   true,
		Network: spec.Network.UseNetworkNamespace,
	}
	plan.Network = networkAdmission(spec.Network)
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
	if spec.Phase != linuxspec.PhasePlanOnly {
		return fmt.Errorf("linux current-user admission requires phase %q until the experimental runner is wired", linuxspec.PhasePlanOnly)
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

func networkAdmission(spec linuxspec.NetworkSpec) NetworkAdmission {
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
		Detail:          "host network as current user; no egress filtering is claimed",
	}
}
