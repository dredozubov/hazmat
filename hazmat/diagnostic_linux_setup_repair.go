package hazmat

import (
	"fmt"
	"runtime"

	"hazmat/internal/setup"
	linuxsetup "hazmat/internal/setup/linux"
)

type linuxDiagnosticSetupOperation string

const (
	linuxDiagnosticSetupApply  linuxDiagnosticSetupOperation = "apply"
	linuxDiagnosticSetupVerify linuxDiagnosticSetupOperation = "verify"
)

type linuxSetupDiagnosticInfo struct {
	ResourceID      diagnosticResourceID
	FindingID       diagnosticFindingID
	Owner           string
	DesiredState    string
	Title           string
	Action          string
	SecurityImpact  string
	Authority       diagnosticRepairAuthority
	Privileged      bool
	Reversibility   diagnosticRepairReversibility
	Preconditions   []string
	TestObligations []string
	ProofNotes      string
}

var linuxDiagnosticRuntimeOS = func() string {
	return runtime.GOOS
}

var linuxDiagnosticSetupCallbacks = func(_ *diagnosticHostRepairBackend, operation linuxDiagnosticSetupOperation) linuxsetup.Callbacks {
	callback := func(resource setup.Resource) linuxsetup.Callback {
		return func() error {
			return fmt.Errorf("linux setup %s for %s is waiting for root-helper backend implementation", operation, resource)
		}
	}
	return linuxsetup.Callbacks{
		DistroProfile:   callback(setup.ResourceLinuxDistroProfile),
		AgentUser:       callback(setup.ResourceLinuxAgentUser),
		SharedGroup:     callback(setup.ResourceLinuxSharedGroup),
		AgentHome:       callback(setup.ResourceLinuxAgentHome),
		WorkspaceAccess: callback(setup.ResourceLinuxWorkspaceAccess),
		ToolHome:        callback(setup.ResourceLinuxToolHome),
		FirewallPolicy:  callback(setup.ResourceLinuxFirewallPolicy),
		ResolverPolicy:  callback(setup.ResourceLinuxResolverPolicy),
		CgroupRoot:      callback(setup.ResourceLinuxCgroupRoot),
		LaunchHelper:    callback(setup.ResourceLinuxLaunchHelper),
		Sudoers:         callback(setup.ResourceLinuxSudoers),
	}
}

func init() {
	registerLinuxSetupDiagnostics()
}

func registerLinuxSetupDiagnostics() {
	for _, spec := range linuxsetup.RepairSpecs() {
		info, ok := linuxSetupDiagnosticInfos[spec.Resource]
		if !ok {
			panic(fmt.Sprintf("missing Linux setup diagnostic metadata for %s", spec.Resource))
		}
		diagnosticResourceDefinitions[info.ResourceID] = diagnosticResourceDefinition{
			ID:           info.ResourceID,
			Owner:        info.Owner,
			DesiredState: info.DesiredState,
		}
		diagnosticFindingDefinitions[info.FindingID] = mustDiagnosticFinding(diagnosticFindingDefinition{
			ID:               info.FindingID,
			Resource:         info.ResourceID,
			Title:            info.Title,
			Repairability:    diagnosticRepairConsent,
			Action:           info.Action,
			RepairAction:     diagnosticRepairActionID(spec.ActionID),
			RepairReceipt:    diagnosticRepairReceiptID(spec.ReceiptID),
			Verification:     diagnosticVerificationID(spec.VerificationID),
			SecurityImpact:   info.SecurityImpact,
			RollbackBoundary: spec.RollbackBoundary,
			GroupKey:         "linux.setup",
		})
		diagnosticRepairActionDefinitions[diagnosticRepairActionID(spec.ActionID)] = diagnosticRepairActionDefinition{
			ID:               diagnosticRepairActionID(spec.ActionID),
			Repairability:    diagnosticRepairConsent,
			Authority:        info.Authority,
			Privileged:       info.Privileged,
			Reversibility:    info.Reversibility,
			Preconditions:    info.Preconditions,
			Receipt:          diagnosticRepairReceiptID(spec.ReceiptID),
			Verification:     diagnosticVerificationID(spec.VerificationID),
			RollbackBoundary: spec.RollbackBoundary,
			TestObligations:  info.TestObligations,
			ProofLanes: []diagnosticRepairProofLane{
				diagnosticRepairProofTLASetupRollback,
				diagnosticRepairProofUnitTests,
				diagnosticRepairProofDirtyStateConvergence,
				diagnosticRepairProofVerifyAfterAction,
			},
			ProofNotes: info.ProofNotes,
		}
		diagnosticHostRepairApplyHandlers[diagnosticRepairActionID(spec.ActionID)] = applyLinuxSetupRepair
		diagnosticHostRepairVerifyHandlers[diagnosticVerificationID(spec.VerificationID)] = verifyLinuxSetupRepair
	}
}

func applyLinuxSetupRepair(b *diagnosticHostRepairBackend, _ *Runner, action diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
	if err := requireLinuxDiagnosticHost(action.ID); err != nil {
		return err
	}
	return linuxsetup.RunRepairAction(string(action.ID), linuxDiagnosticSetupCallbacks(b, linuxDiagnosticSetupApply))
}

func verifyLinuxSetupRepair(b *diagnosticHostRepairBackend, action diagnosticRepairActionDefinition, _ diagnosticRepairPlanItem) error {
	if err := requireLinuxDiagnosticHost(action.ID); err != nil {
		return err
	}
	return linuxsetup.VerifyRepairAction(string(action.Verification), linuxDiagnosticSetupCallbacks(b, linuxDiagnosticSetupVerify))
}

func requireLinuxDiagnosticHost(actionID diagnosticRepairActionID) error {
	if goos := linuxDiagnosticRuntimeOS(); goos != "linux" {
		return fmt.Errorf("%s requires Linux host diagnostics, got %s", actionID, goos)
	}
	return nil
}

var linuxSetupDiagnosticInfos = map[setup.Resource]linuxSetupDiagnosticInfo{
	setup.ResourceLinuxDistroProfile: {
		ResourceID:      "linux.setup.distro-profile",
		FindingID:       "linux.setup.distro-profile",
		Owner:           "linux.setup.profile",
		DesiredState:    "Linux distro, kernel, namespace, Landlock, seccomp, cgroup, service-manager, and helper-strategy facts are recorded as diagnostic evidence",
		Title:           "Refresh the Linux distro profile",
		Action:          "Refresh the Linux distro profile through Hazmat's modeled Linux setup backend, then verify the recorded facts before enabling agent-user setup.",
		SecurityImpact:  "Stale host capability facts can make Linux setup choose an unsupported helper or containment profile.",
		Authority:       diagnosticRepairAuthorityCurrentUser,
		Privileged:      false,
		Reversibility:   diagnosticRepairReversibleByReceipt,
		Preconditions:   []string{"Linux host facts are readable", "selected helper strategy is explicit", "profile path is managed by Hazmat"},
		TestObligations: []string{"Linux profile unit tests", "read-only diagnostic fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux profile repair is setup-owned evidence, not launch authority; MC_SetupRollback governs its position before account, cgroup, and sudoers setup.",
	},
	setup.ResourceLinuxAgentUser: {
		ResourceID:      "linux.setup.agent-user",
		FindingID:       "linux.setup.agent-user",
		Owner:           "linux.setup.identity",
		DesiredState:    "dedicated locked Linux agent account exists with expected home, shell, and ownership policy",
		Title:           "Restore the Linux agent account",
		Action:          "Restore the dedicated Linux agent account through Hazmat's modeled Linux setup backend, then verify identity state before privilege setup continues.",
		SecurityImpact:  "Missing or malformed agent identity removes the host-user isolation boundary for the Linux agent-user lane.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"caller approved account repair", "uid and home policy are selected", "sudoers privilege is not installed before containment resources verify"},
		TestObligations: []string{"Linux account graph tests", "dirty setup convergence fixture", "destructive rollback fixture"},
		ProofNotes:      "Linux agent-user repair is identity setup state; MC_SetupRollback proves privilege is added only after required containment resources.",
	},
	setup.ResourceLinuxSharedGroup: {
		ResourceID:      "linux.setup.shared-group",
		FindingID:       "linux.setup.shared-group",
		Owner:           "linux.setup.identity",
		DesiredState:    "controlled Linux shared group exists with only the memberships needed for workspace collaboration",
		Title:           "Restore the Linux shared group",
		Action:          "Restore the Linux shared group through Hazmat's modeled Linux setup backend, then verify host and agent membership.",
		SecurityImpact:  "Incorrect group state can either block collaboration or grant broader workspace access than the contract allows.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"caller approved group repair", "agent account policy is known", "workspace access repair is bounded to managed paths"},
		TestObligations: []string{"Linux shared-group graph tests", "membership convergence fixture", "destructive rollback fixture"},
		ProofNotes:      "Linux shared-group repair can outlive the agent user only as unprivileged residue; MC_SetupRollback owns that preservation boundary.",
	},
	setup.ResourceLinuxAgentHome: {
		ResourceID:      "linux.setup.agent-home",
		FindingID:       "linux.setup.agent-home",
		Owner:           "linux.setup.identity",
		DesiredState:    "Linux agent HOME and required XDG parents exist with agent ownership and restrictive modes",
		Title:           "Restore the Linux agent home",
		Action:          "Restore the Linux agent home through Hazmat's modeled Linux setup backend, then verify ownership and mode before launch.",
		SecurityImpact:  "Wrong home ownership can leak agent state or prevent credential and harness materialization.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"agent user exists or will be repaired first", "home path is within Hazmat's Linux setup boundary", "caller approved home repair"},
		TestObligations: []string{"Linux home graph tests", "ownership convergence fixture", "destructive rollback fixture"},
		ProofNotes:      "Linux agent-home repair is setup identity state and must be removed only under the explicit destructive home flag.",
	},
	setup.ResourceLinuxWorkspaceAccess: {
		ResourceID:      "linux.setup.workspace-access",
		FindingID:       "linux.setup.workspace-access",
		Owner:           "linux.setup.workspace",
		DesiredState:    "selected project workspace has only the Hazmat-managed traversal and ACL/group access required by the launch contract",
		Title:           "Restore Linux workspace access",
		Action:          "Restore Linux workspace access through Hazmat's modeled Linux setup backend, then verify the selected project path is reachable by the agent.",
		SecurityImpact:  "Broken workspace access blocks launches; overly broad access weakens host-user file isolation.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"project path is canonical", "agent account and shared group exist", "repair is scoped to the selected workspace"},
		TestObligations: []string{"Linux workspace graph tests", "bounded ACL fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux workspace repair is project-scoped setup state; MC_SetupRollback requires it to be removed before destructive identity deletion.",
	},
	setup.ResourceLinuxToolHome: {
		ResourceID:      "linux.setup.tool-home",
		FindingID:       "linux.setup.tool-home",
		Owner:           "linux.setup.identity",
		DesiredState:    "optional Linux agent tool/cache root is agent-owned and not writable by the host user unless explicitly modeled",
		Title:           "Restore the Linux tool home",
		Action:          "Restore the Linux tool home through Hazmat's modeled Linux setup backend, then verify ownership and mode.",
		SecurityImpact:  "Host-owned or broadly writable tool cache state can bypass the intended agent identity boundary.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"agent home exists", "tool-home path is managed by Hazmat", "caller approved tool-home repair"},
		TestObligations: []string{"Linux tool-home graph tests", "ownership convergence fixture", "destructive rollback fixture"},
		ProofNotes:      "Linux tool-home repair is preserved by default and deleted only under the explicit destructive tool-home flag modeled by MC_SetupRollback.",
	},
	setup.ResourceLinuxFirewallPolicy: {
		ResourceID:      "linux.setup.firewall-policy",
		FindingID:       "linux.setup.firewall-policy",
		Owner:           "linux.setup.network",
		DesiredState:    "Hazmat-owned Linux firewall policy root exists for supported egress modes without unmanaged policy takeover",
		Title:           "Restore the Linux firewall policy",
		Action:          "Restore the Linux firewall policy through Hazmat's modeled Linux setup backend, then verify managed policy ownership.",
		SecurityImpact:  "Missing firewall setup can leave requested Linux egress modes unenforced.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"caller approved network repair", "policy root is Hazmat-managed", "unsupported host policy conflicts are absent"},
		TestObligations: []string{"Linux firewall graph tests", "policy ownership fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux firewall repair is setup-owned network state and must be removed after sudoers/helper privilege is revoked.",
	},
	setup.ResourceLinuxResolverPolicy: {
		ResourceID:      "linux.setup.resolver-policy",
		FindingID:       "linux.setup.resolver-policy",
		Owner:           "linux.setup.network",
		DesiredState:    "Hazmat-owned Linux resolver policy root exists for supported DNS modes without unmanaged resolver takeover",
		Title:           "Restore the Linux resolver policy",
		Action:          "Restore the Linux resolver policy through Hazmat's modeled Linux setup backend, then verify managed resolver ownership.",
		SecurityImpact:  "Missing resolver setup can leave requested DNS restrictions unenforced.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"caller approved resolver repair", "resolver policy root is Hazmat-managed", "unsupported resolver conflicts are absent"},
		TestObligations: []string{"Linux resolver graph tests", "resolver ownership fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux resolver repair is setup-owned network state and must be removed after sudoers/helper privilege is revoked.",
	},
	setup.ResourceLinuxCgroupRoot: {
		ResourceID:      "linux.setup.cgroup-root",
		FindingID:       "linux.setup.cgroup-root",
		Owner:           "linux.setup.resources",
		DesiredState:    "Linux cgroup v2 subtree or delegation exists for the selected agent-user resource profile",
		Title:           "Restore the Linux cgroup root",
		Action:          "Restore the Linux cgroup root through Hazmat's modeled Linux setup backend, then verify cgroup v2 delegation before sudoers setup.",
		SecurityImpact:  "Missing cgroup setup prevents resource controls and weakens the agent-user runtime boundary.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"cgroup v2 is available", "service-manager strategy is supported", "caller approved cgroup repair"},
		TestObligations: []string{"Linux cgroup graph tests", "capability-gap fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux cgroup repair is a containment prerequisite for sudoers; MC_SetupRollback proves sudoers is created after it and removed before it.",
	},
	setup.ResourceLinuxLaunchHelper: {
		ResourceID:      "linux.setup.launch-helper",
		FindingID:       "linux.setup.launch-helper",
		Owner:           "linux.setup.privilege",
		DesiredState:    "Linux launch helper is installed at the fixed managed path with expected owner, mode, and digest",
		Title:           "Restore the Linux launch helper",
		Action:          "Restore the Linux launch helper through Hazmat's modeled Linux setup backend, then verify owner, mode, digest, and command boundary.",
		SecurityImpact:  "A stale or unmanaged helper can turn the agent-user lane into an unbounded privileged command surface.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"fixed helper path is selected", "caller approved helper repair", "helper digest is from the installed Hazmat binary"},
		TestObligations: []string{"Linux helper graph tests", "fixed-path digest fixture", "verify-after-action fixture"},
		ProofNotes:      "Linux launch-helper repair is privileged setup state; MC_SetupRollback requires helper access to be disabled before weaker resources are removed.",
	},
	setup.ResourceLinuxSudoers: {
		ResourceID:      "linux.setup.sudoers",
		FindingID:       "linux.setup.sudoers",
		Owner:           "linux.setup.privilege",
		DesiredState:    "narrow Linux sudoers rule allows only the fixed Hazmat helper path after containment prerequisites verify",
		Title:           "Restore the Linux sudoers rule",
		Action:          "Restore the Linux sudoers rule through Hazmat's modeled Linux setup backend only after helper, cgroup, and containment prerequisites verify.",
		SecurityImpact:  "Broad or premature sudoers grants are the highest-risk Linux setup failure mode.",
		Authority:       diagnosticRepairAuthorityRoot,
		Privileged:      true,
		Reversibility:   diagnosticRepairBoundedRollback,
		Preconditions:   []string{"helper verifies", "cgroup and containment resources verify", "caller approved sudoers repair"},
		TestObligations: []string{"Linux sudoers graph tests", "privilege-last regression", "verify-after-action fixture"},
		ProofNotes:      "Linux sudoers repair is the privilege boundary; MC_SetupRollback proves it is installed last and removed first.",
	},
}
