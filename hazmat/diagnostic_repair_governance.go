package hazmat

import "fmt"

type diagnosticRepairAuthority string
type diagnosticRepairApprovalModel string
type diagnosticRepairReversibility string

const (
	diagnosticRepairAuthorityNone            diagnosticRepairAuthority = "none"
	diagnosticRepairAuthorityCurrentUser     diagnosticRepairAuthority = "current-user"
	diagnosticRepairAuthorityRoot            diagnosticRepairAuthority = "root"
	diagnosticRepairAuthorityCredentialStore diagnosticRepairAuthority = "credential-store"
	diagnosticRepairAuthorityExternal        diagnosticRepairAuthority = "external"

	diagnosticRepairApprovalNone          diagnosticRepairApprovalModel = "none"
	diagnosticRepairApprovalConsent       diagnosticRepairApprovalModel = "explicit-consent"
	diagnosticRepairApprovalManual        diagnosticRepairApprovalModel = "manual"
	diagnosticRepairApprovalUnsupported   diagnosticRepairApprovalModel = "unsupported"
	diagnosticRepairApprovalOptional      diagnosticRepairApprovalModel = "optional"
	diagnosticRepairApprovalInformational diagnosticRepairApprovalModel = "informational"

	diagnosticRepairReversibleByReceipt diagnosticRepairReversibility = "reversible-by-receipt"
	diagnosticRepairBoundedRollback     diagnosticRepairReversibility = "bounded-rollback"
)

type diagnosticRepairClassPolicy struct {
	Repairability      diagnosticRepairability
	Authority          diagnosticRepairAuthority
	Approval           diagnosticRepairApprovalModel
	ExecutableByHazmat bool
	RollbackModel      string
	Preconditions      []string
	TestObligations    []string
}

type diagnosticRepairActionDefinition struct {
	ID               diagnosticRepairActionID
	Repairability    diagnosticRepairability
	Authority        diagnosticRepairAuthority
	Privileged       bool
	Reversibility    diagnosticRepairReversibility
	Preconditions    []string
	Receipt          diagnosticRepairReceiptID
	Verification     diagnosticVerificationID
	RollbackBoundary string
	TestObligations  []string
}

var diagnosticRepairClassPolicies = map[diagnosticRepairability]diagnosticRepairClassPolicy{
	diagnosticRepairAuto: {
		Repairability:      diagnosticRepairAuto,
		Authority:          diagnosticRepairAuthorityCurrentUser,
		Approval:           diagnosticRepairApprovalNone,
		ExecutableByHazmat: true,
		RollbackModel:      "only for idempotent, non-privileged changes with receipts and verification",
		Preconditions:      []string{"typed finding", "non-privileged action", "receipt target", "verification target"},
		TestObligations:    []string{"unit coverage", "idempotence test", "verify-after-action test"},
	},
	diagnosticRepairConsent: {
		Repairability:      diagnosticRepairConsent,
		Authority:          diagnosticRepairAuthorityRoot,
		Approval:           diagnosticRepairApprovalConsent,
		ExecutableByHazmat: true,
		RollbackModel:      "requires explicit rollback boundary and receipt before mutation",
		Preconditions:      []string{"typed finding", "user consent", "authority preflight", "receipt target", "verification target"},
		TestObligations:    []string{"unit coverage", "dirty-state convergence test", "verify-after-action test"},
	},
	diagnosticRepairManualExternal: {
		Repairability:      diagnosticRepairManualExternal,
		Authority:          diagnosticRepairAuthorityExternal,
		Approval:           diagnosticRepairApprovalManual,
		ExecutableByHazmat: false,
		RollbackModel:      "external system owner controls rollback",
		Preconditions:      []string{"typed finding", "manual owner identified"},
		TestObligations:    []string{"classification coverage", "no executable repair action"},
	},
	diagnosticRepairUnsupported: {
		Repairability:      diagnosticRepairUnsupported,
		Authority:          diagnosticRepairAuthorityNone,
		Approval:           diagnosticRepairApprovalUnsupported,
		ExecutableByHazmat: false,
		RollbackModel:      "unsupported until a backend adapter or repair action exists",
		Preconditions:      []string{"typed finding", "unsupported boundary documented"},
		TestObligations:    []string{"classification coverage", "no executable repair action"},
	},
	diagnosticRepairOptional: {
		Repairability:      diagnosticRepairOptional,
		Authority:          diagnosticRepairAuthorityExternal,
		Approval:           diagnosticRepairApprovalOptional,
		ExecutableByHazmat: false,
		RollbackModel:      "optional capability outside containment repair",
		Preconditions:      []string{"typed finding", "workflow need confirmed"},
		TestObligations:    []string{"classification coverage", "not included in containment repair plan"},
	},
	diagnosticRepairInformational: {
		Repairability:      diagnosticRepairInformational,
		Authority:          diagnosticRepairAuthorityNone,
		Approval:           diagnosticRepairApprovalInformational,
		ExecutableByHazmat: false,
		RollbackModel:      "no mutation",
		Preconditions:      []string{"typed finding"},
		TestObligations:    []string{"classification coverage", "not included in containment repair plan"},
	},
}

var diagnosticRepairActionDefinitions = map[diagnosticRepairActionID]diagnosticRepairActionDefinition{
	"repair.workspace.setgid": {
		ID:               "repair.workspace.setgid",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairBoundedRollback,
		Preconditions:    []string{"project path is canonical", "shared group exists", "caller approved permission repair"},
		Receipt:          "receipt.workspace.setgid",
		Verification:     "verify.workspace.setgid",
		RollbackBoundary: "setup.workspace-permissions",
		TestObligations:  []string{"workspace permission unit tests", "init-check convergence fixture"},
	},
	"repair.workspace.access": {
		ID:               "repair.workspace.access",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairBoundedRollback,
		Preconditions:    []string{"project path is canonical", "agent user exists", "shared group exists", "caller approved ACL repair"},
		Receipt:          "receipt.workspace.access",
		Verification:     "verify.workspace.access",
		RollbackBoundary: "setup.workspace-permissions",
		TestObligations:  []string{"workspace ACL unit tests", "dirty workspace convergence fixture"},
	},
	"repair.agent-home.permissions": {
		ID:               "repair.agent-home.permissions",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairBoundedRollback,
		Preconditions:    []string{"agent user exists", "agent home path matches configured agent", "caller approved privacy repair"},
		Receipt:          "receipt.agent-home.permissions",
		Verification:     "verify.agent-home.permissions",
		RollbackBoundary: "setup.agent-account",
		TestObligations:  []string{"agent-home permission unit tests", "rollback boundary regression"},
	},
	"repair.agent-shell.umask": {
		ID:               "repair.agent-shell.umask",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairReversibleByReceipt,
		Preconditions:    []string{"agent user exists", "managed shell block can be located or appended", "caller approved shell profile repair"},
		Receipt:          "receipt.agent-shell.umask",
		Verification:     "verify.agent-shell.umask",
		RollbackBoundary: "setup.agent-shell",
		TestObligations:  []string{"managed block unit tests", "idempotent init/check fixture"},
	},
	"repair.network.pf": {
		ID:               "repair.network.pf",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairReversibleByReceipt,
		Preconditions:    []string{"macOS pf available", "managed anchor path is canonical", "caller approved firewall repair"},
		Receipt:          "receipt.network.pf",
		Verification:     "verify.network.pf",
		RollbackBoundary: "setup.network-pf",
		TestObligations:  []string{"pf config unit tests", "static firewall verification fixture"},
	},
	"repair.network.dns-blocklist": {
		ID:               "repair.network.dns-blocklist",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairReversibleByReceipt,
		Preconditions:    []string{"hosts file is readable", "managed block markers are valid", "caller approved DNS repair"},
		Receipt:          "receipt.network.dns-blocklist",
		Verification:     "verify.network.dns-blocklist",
		RollbackBoundary: "setup.network-dns-blocklist",
		TestObligations:  []string{"hosts block unit tests", "DNS blocklist verification fixture"},
	},
	"repair.network.persistence": {
		ID:               "repair.network.persistence",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairReversibleByReceipt,
		Preconditions:    []string{"LaunchDaemon path is canonical", "pf.conf managed anchor is valid", "caller approved persistence repair"},
		Receipt:          "receipt.network.persistence",
		Verification:     "verify.network.persistence",
		RollbackBoundary: "setup.network-persistence",
		TestObligations:  []string{"launchd plist unit tests", "persistence verification fixture"},
	},
	"repair.credential.claude-state": {
		ID:               "repair.credential.claude-state",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityCredentialStore,
		Privileged:       false,
		Reversibility:    diagnosticRepairReversibleByReceipt,
		Preconditions:    []string{"host-owned secret store is readable", "agent-home residue path is canonical", "caller approved credential migration"},
		Receipt:          "receipt.credential.claude-state",
		Verification:     "verify.credential.claude-state",
		RollbackBoundary: "credentials.host-secret-store",
		TestObligations:  []string{"credential inventory regression", "secret redaction test"},
	},
	"repair.credential.cloud-secret-key": {
		ID:               "repair.credential.cloud-secret-key",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityCredentialStore,
		Privileged:       false,
		Reversibility:    diagnosticRepairReversibleByReceipt,
		Preconditions:    []string{"legacy credential path is canonical", "host-owned secret store is writable", "caller approved credential migration"},
		Receipt:          "receipt.credential.cloud-secret-key",
		Verification:     "verify.credential.cloud-secret-key",
		RollbackBoundary: "credentials.host-secret-store",
		TestObligations:  []string{"credential inventory regression", "secret pattern scan"},
	},
	"repair.credential.residue": {
		ID:               "repair.credential.residue",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityCredentialStore,
		Privileged:       false,
		Reversibility:    diagnosticRepairReversibleByReceipt,
		Preconditions:    []string{"registered credential backend exists", "residue path is canonical", "caller approved credential cleanup"},
		Receipt:          "receipt.credential.residue",
		Verification:     "verify.credential.residue",
		RollbackBoundary: "credentials.host-secret-store",
		TestObligations:  []string{"credential inventory regression", "secret redaction test"},
	},
	"repair.claude.project-permissions": {
		ID:               "repair.claude.project-permissions",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairBoundedRollback,
		Preconditions:    []string{"Claude project path is under agent home", "shared group exists", "caller approved project permission repair"},
		Receipt:          "receipt.claude.project-permissions",
		Verification:     "verify.claude.project-permissions",
		RollbackBoundary: "agent-home.claude-projects",
		TestObligations:  []string{"Claude project permission unit tests", "resume/export dirty-state fixture"},
	},
}

func diagnosticRepairClassPolicyFor(repairability diagnosticRepairability) (diagnosticRepairClassPolicy, bool) {
	policy, ok := diagnosticRepairClassPolicies[repairability]
	return policy, ok
}

func diagnosticRepairAction(id diagnosticRepairActionID) (diagnosticRepairActionDefinition, bool) {
	def, ok := diagnosticRepairActionDefinitions[id]
	return def, ok
}

func (p diagnosticRepairClassPolicy) Validate() error {
	if p.Repairability == "" {
		return fmt.Errorf("missing repairability")
	}
	if p.Authority == "" {
		return fmt.Errorf("%s: missing authority", p.Repairability)
	}
	if p.Approval == "" {
		return fmt.Errorf("%s: missing approval model", p.Repairability)
	}
	if p.RollbackModel == "" {
		return fmt.Errorf("%s: missing rollback model", p.Repairability)
	}
	if len(p.Preconditions) == 0 {
		return fmt.Errorf("%s: missing preconditions", p.Repairability)
	}
	if len(p.TestObligations) == 0 {
		return fmt.Errorf("%s: missing test obligations", p.Repairability)
	}
	return nil
}

func (a diagnosticRepairActionDefinition) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("missing repair action id")
	}
	if a.Repairability == "" {
		return fmt.Errorf("%s: missing repairability", a.ID)
	}
	if a.Authority == "" {
		return fmt.Errorf("%s: missing authority", a.ID)
	}
	if a.Reversibility == "" {
		return fmt.Errorf("%s: missing reversibility", a.ID)
	}
	if len(a.Preconditions) == 0 {
		return fmt.Errorf("%s: missing preconditions", a.ID)
	}
	if a.Receipt == "" {
		return fmt.Errorf("%s: missing receipt", a.ID)
	}
	if a.Verification == "" {
		return fmt.Errorf("%s: missing verification", a.ID)
	}
	if a.RollbackBoundary == "" {
		return fmt.Errorf("%s: missing rollback boundary", a.ID)
	}
	if len(a.TestObligations) == 0 {
		return fmt.Errorf("%s: missing test obligations", a.ID)
	}
	return nil
}
