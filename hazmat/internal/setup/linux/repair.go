package linux

import (
	"fmt"

	"hazmat/internal/setup"
)

type RepairSpec struct {
	Resource         setup.Resource
	ActionID         string
	ReceiptID        string
	VerificationID   string
	RollbackBoundary string
}

type RepairStep struct {
	Spec RepairSpec
	Step Step
}

func RepairSpecs() []RepairSpec {
	return []RepairSpec{
		repairSpec(setup.ResourceLinuxDistroProfile, "distro-profile"),
		repairSpec(setup.ResourceLinuxAgentUser, "agent-user"),
		repairSpec(setup.ResourceLinuxSharedGroup, "shared-group"),
		repairSpec(setup.ResourceLinuxAgentHome, "agent-home"),
		repairSpec(setup.ResourceLinuxWorkspaceAccess, "workspace-access"),
		repairSpec(setup.ResourceLinuxToolHome, "tool-home"),
		repairSpec(setup.ResourceLinuxFirewallPolicy, "firewall-policy"),
		repairSpec(setup.ResourceLinuxResolverPolicy, "resolver-policy"),
		repairSpec(setup.ResourceLinuxCgroupRoot, "cgroup-root"),
		repairSpec(setup.ResourceLinuxLaunchHelper, "launch-helper"),
		repairSpec(setup.ResourceLinuxSudoers, "sudoers"),
	}
}

func SetupRepairSteps(callbacks Callbacks) []RepairStep {
	specs := repairSpecsByResource()
	steps := SetupSteps(callbacks)
	out := make([]RepairStep, 0, len(steps))
	for _, step := range steps {
		spec, ok := specs[step.Resource]
		if !ok {
			continue
		}
		out = append(out, RepairStep{
			Spec: spec,
			Step: step,
		})
	}
	return out
}

func RunRepairAction(actionID string, callbacks Callbacks) error {
	for _, step := range SetupRepairSteps(callbacks) {
		if step.Spec.ActionID == actionID {
			return runStep(step.Step)
		}
	}
	return fmt.Errorf("unknown Linux setup repair action %s", actionID)
}

func VerifyRepairAction(verificationID string, callbacks Callbacks) error {
	spec, ok := RepairSpecForVerification(verificationID)
	if !ok {
		return fmt.Errorf("unknown Linux setup verification %s", verificationID)
	}
	return runConfigured("linuxVerify"+resourceSuffix(spec.Resource), callbackForResource(callbacks, spec.Resource))
}

func RepairSpecForAction(actionID string) (RepairSpec, bool) {
	for _, spec := range RepairSpecs() {
		if spec.ActionID == actionID {
			return spec, true
		}
	}
	return RepairSpec{}, false
}

func RepairSpecForVerification(verificationID string) (RepairSpec, bool) {
	for _, spec := range RepairSpecs() {
		if spec.VerificationID == verificationID {
			return spec, true
		}
	}
	return RepairSpec{}, false
}

func repairSpec(resource setup.Resource, suffix string) RepairSpec {
	return RepairSpec{
		Resource:         resource,
		ActionID:         "repair.linux-setup." + suffix,
		ReceiptID:        "receipt.linux-setup." + suffix,
		VerificationID:   "verify.linux-setup." + suffix,
		RollbackBoundary: "linux.setup." + suffix,
	}
}

func repairSpecsByResource() map[setup.Resource]RepairSpec {
	specs := make(map[setup.Resource]RepairSpec, len(RepairSpecs()))
	for _, spec := range RepairSpecs() {
		specs[spec.Resource] = spec
	}
	return specs
}

func callbackForResource(callbacks Callbacks, resource setup.Resource) Callback {
	switch resource {
	case setup.ResourceLinuxDistroProfile:
		return callbacks.DistroProfile
	case setup.ResourceLinuxAgentUser:
		return callbacks.AgentUser
	case setup.ResourceLinuxSharedGroup:
		return callbacks.SharedGroup
	case setup.ResourceLinuxAgentHome:
		return callbacks.AgentHome
	case setup.ResourceLinuxWorkspaceAccess:
		return callbacks.WorkspaceAccess
	case setup.ResourceLinuxToolHome:
		return callbacks.ToolHome
	case setup.ResourceLinuxFirewallPolicy:
		return callbacks.FirewallPolicy
	case setup.ResourceLinuxResolverPolicy:
		return callbacks.ResolverPolicy
	case setup.ResourceLinuxCgroupRoot:
		return callbacks.CgroupRoot
	case setup.ResourceLinuxLaunchHelper:
		return callbacks.LaunchHelper
	case setup.ResourceLinuxSudoers:
		return callbacks.Sudoers
	case setup.ResourceAgentUser,
		setup.ResourceDevGroup,
		setup.ResourceHomeDirTraverse,
		setup.ResourceLocalRepo,
		setup.ResourceHardeningGaps,
		setup.ResourceUmask,
		setup.ResourceSeatbelt,
		setup.ResourceWrappers,
		setup.ResourcePfAnchor,
		setup.ResourceDNSBlocklist,
		setup.ResourceLaunchDaemon,
		setup.ResourceLaunchHelper,
		setup.ResourceSudoers,
		setup.ResourceMaintenanceSudoers,
		setup.ResourceClaudeCode,
		setup.ResourceCredentials:
		return nil
	default:
		return nil
	}
}

func resourceSuffix(resource setup.Resource) string {
	switch resource {
	case setup.ResourceLinuxDistroProfile:
		return "DistroProfile"
	case setup.ResourceLinuxAgentUser:
		return "AgentUser"
	case setup.ResourceLinuxSharedGroup:
		return "SharedGroup"
	case setup.ResourceLinuxAgentHome:
		return "AgentHome"
	case setup.ResourceLinuxWorkspaceAccess:
		return "WorkspaceAccess"
	case setup.ResourceLinuxToolHome:
		return "ToolHome"
	case setup.ResourceLinuxFirewallPolicy:
		return "FirewallPolicy"
	case setup.ResourceLinuxResolverPolicy:
		return "ResolverPolicy"
	case setup.ResourceLinuxCgroupRoot:
		return "CgroupRoot"
	case setup.ResourceLinuxLaunchHelper:
		return "LaunchHelper"
	case setup.ResourceLinuxSudoers:
		return "Sudoers"
	case setup.ResourceAgentUser,
		setup.ResourceDevGroup,
		setup.ResourceHomeDirTraverse,
		setup.ResourceLocalRepo,
		setup.ResourceHardeningGaps,
		setup.ResourceUmask,
		setup.ResourceSeatbelt,
		setup.ResourceWrappers,
		setup.ResourcePfAnchor,
		setup.ResourceDNSBlocklist,
		setup.ResourceLaunchDaemon,
		setup.ResourceLaunchHelper,
		setup.ResourceSudoers,
		setup.ResourceMaintenanceSudoers,
		setup.ResourceClaudeCode,
		setup.ResourceCredentials:
		return "Unknown"
	default:
		return "Unknown"
	}
}
