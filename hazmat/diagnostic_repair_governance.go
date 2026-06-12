package hazmat

import "fmt"

type diagnosticRepairAuthority string
type diagnosticRepairApprovalModel string
type diagnosticRepairReversibility string
type diagnosticRepairProofLane string

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

	diagnosticRepairProofUnitTests                   diagnosticRepairProofLane = "tests.unit"
	diagnosticRepairProofDirtyStateConvergence       diagnosticRepairProofLane = "tests.dirty-state-convergence"
	diagnosticRepairProofVerifyAfterAction           diagnosticRepairProofLane = "tests.verify-after-action"
	diagnosticRepairProofClassification              diagnosticRepairProofLane = "tests.classification"
	diagnosticRepairProofGuardedRealHostSmoke        diagnosticRepairProofLane = "tests.guarded-real-host-smoke"
	diagnosticRepairProofSecretScan                  diagnosticRepairProofLane = "tests.secret-scan"
	diagnosticRepairProofTLASetupRollback            diagnosticRepairProofLane = "tla.MC_SetupRollback"
	diagnosticRepairProofTLASessionPermissionRepairs diagnosticRepairProofLane = "tla.MC_SessionPermissionRepairs"
	diagnosticRepairProofTLACredentialCapability     diagnosticRepairProofLane = "tla.MC_CredentialCapabilityLifecycle"
	diagnosticRepairProofTLASecretStoreRecovery      diagnosticRepairProofLane = "tla.MC_SecretStoreRecovery"
)

type diagnosticRepairClassPolicy struct {
	Repairability      diagnosticRepairability
	Authority          diagnosticRepairAuthority
	Approval           diagnosticRepairApprovalModel
	ExecutableByHazmat bool
	RollbackModel      string
	Preconditions      []string
	TestObligations    []string
	ProofLanes         []diagnosticRepairProofLane
	ProofNotes         string
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
	ProofLanes       []diagnosticRepairProofLane
	ProofNotes       string
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
		ProofLanes:         []diagnosticRepairProofLane{diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:         "Auto repairs rely on executable tests unless the action touches a verified subsystem, in which case the action-specific lanes add the required TLA spec.",
	},
	diagnosticRepairConsent: {
		Repairability:      diagnosticRepairConsent,
		Authority:          diagnosticRepairAuthorityRoot,
		Approval:           diagnosticRepairApprovalConsent,
		ExecutableByHazmat: true,
		RollbackModel:      "requires explicit rollback boundary and receipt before mutation",
		Preconditions:      []string{"typed finding", "user consent", "authority preflight", "receipt target", "verification target"},
		TestObligations:    []string{"unit coverage", "dirty-state convergence test", "verify-after-action test"},
		ProofLanes:         []diagnosticRepairProofLane{diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:         "Consent only authorizes execution; action-specific lanes still decide which TLA spec must be updated or rerun before implementation.",
	},
	diagnosticRepairManualExternal: {
		Repairability:      diagnosticRepairManualExternal,
		Authority:          diagnosticRepairAuthorityExternal,
		Approval:           diagnosticRepairApprovalManual,
		ExecutableByHazmat: false,
		RollbackModel:      "external system owner controls rollback",
		Preconditions:      []string{"typed finding", "manual owner identified"},
		TestObligations:    []string{"classification coverage", "no executable repair action"},
		ProofLanes:         []diagnosticRepairProofLane{diagnosticRepairProofClassification},
		ProofNotes:         "Manual-external findings stay outside the executor and are tested as classifications.",
	},
	diagnosticRepairUnsupported: {
		Repairability:      diagnosticRepairUnsupported,
		Authority:          diagnosticRepairAuthorityNone,
		Approval:           diagnosticRepairApprovalUnsupported,
		ExecutableByHazmat: false,
		RollbackModel:      "unsupported until a backend adapter or repair action exists",
		Preconditions:      []string{"typed finding", "unsupported boundary documented"},
		TestObligations:    []string{"classification coverage", "no executable repair action"},
		ProofLanes:         []diagnosticRepairProofLane{diagnosticRepairProofClassification},
		ProofNotes:         "Unsupported findings cannot gain execution until a typed action and proof lane are added.",
	},
	diagnosticRepairOptional: {
		Repairability:      diagnosticRepairOptional,
		Authority:          diagnosticRepairAuthorityExternal,
		Approval:           diagnosticRepairApprovalOptional,
		ExecutableByHazmat: false,
		RollbackModel:      "optional capability outside containment repair",
		Preconditions:      []string{"typed finding", "workflow need confirmed"},
		TestObligations:    []string{"classification coverage", "not included in containment repair plan"},
		ProofLanes:         []diagnosticRepairProofLane{diagnosticRepairProofClassification},
		ProofNotes:         "Optional findings are not containment repairs and stay out of automatic execution.",
	},
	diagnosticRepairInformational: {
		Repairability:      diagnosticRepairInformational,
		Authority:          diagnosticRepairAuthorityNone,
		Approval:           diagnosticRepairApprovalInformational,
		ExecutableByHazmat: false,
		RollbackModel:      "no mutation",
		Preconditions:      []string{"typed finding"},
		TestObligations:    []string{"classification coverage", "not included in containment repair plan"},
		ProofLanes:         []diagnosticRepairProofLane{diagnosticRepairProofClassification},
		ProofNotes:         "Informational findings carry no mutation proof obligation.",
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
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "This changes setup-owned project permissions; executor/init wiring must update or rerun MC_SetupRollback and cover init/check convergence.",
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
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofTLASessionPermissionRepairs, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Workspace ACL repair crosses setup-owned permissions and session-time permission-repair semantics; both TLA boundaries must stay aligned.",
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
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Managed shell-block repair is setup-owned state; TLA covers ordering and rollback boundary while convergence tests cover the init/check loop.",
	},
	"repair.setup.agent-user": {
		ID:               "repair.setup.agent-user",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairBoundedRollback,
		Preconditions:    []string{"caller approved account repair", "agent UID is available or already owned by the agent account", "setup ordering keeps privilege disabled until containment verifies"},
		Receipt:          "receipt.setup.agent-user",
		Verification:     "verify.setup.agent-user",
		RollbackBoundary: "setup.agent-account",
		TestObligations:  []string{"account setup unit tests", "init post-verification fixture"},
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Agent account repair is the first setup resource; any executor mutation must stay aligned with MC_SetupRollback before it can create or repair the account.",
	},
	"repair.setup.agent-home": {
		ID:               "repair.setup.agent-home",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairBoundedRollback,
		Preconditions:    []string{"agent user exists", "caller approved agent home repair", "agent home path matches the configured setup boundary"},
		Receipt:          "receipt.setup.agent-home",
		Verification:     "verify.setup.agent-home",
		RollbackBoundary: "setup.agent-account",
		TestObligations:  []string{"agent home setup unit tests", "post-init verification fixture"},
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Agent home baseline is setup-account state; mutation must follow the setup account boundary and rollback model.",
	},
	"repair.setup.dev-group": {
		ID:               "repair.setup.dev-group",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairBoundedRollback,
		Preconditions:    []string{"caller approved shared group repair", "agent user exists", "controlling user is known"},
		Receipt:          "receipt.setup.dev-group",
		Verification:     "verify.setup.dev-group",
		RollbackBoundary: "setup.workspace-permissions",
		TestObligations:  []string{"dev group membership tests", "init/check convergence fixture"},
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Shared group repair is setup-owned workspace state; executor mutation must remain aligned with MC_SetupRollback resource ordering.",
	},
	"repair.setup.home-traverse": {
		ID:               "repair.setup.home-traverse",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairReversibleByReceipt,
		Preconditions:    []string{"caller approved home traversal ACL repair", "controlling user home path is canonical", "agent user exists"},
		Receipt:          "receipt.setup.home-traverse",
		Verification:     "verify.setup.home-traverse",
		RollbackBoundary: "setup.workspace-permissions",
		TestObligations:  []string{"home traversal ACL unit tests", "init/check convergence fixture"},
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofTLASessionPermissionRepairs, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Home traversal repair bridges setup-owned workspace access and session permission repair semantics; both proof lanes must stay aligned before mutation.",
	},
	"repair.setup.sudoers": {
		ID:               "repair.setup.sudoers",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairBoundedRollback,
		Preconditions:    []string{"pf containment verifies", "launch helper verifies", "caller approved sudoers repair"},
		Receipt:          "receipt.setup.sudoers",
		Verification:     "verify.setup.sudoers",
		RollbackBoundary: "setup.sudoers",
		TestObligations:  []string{"sudoers construction tests", "privilege-after-containment regression"},
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Sudoers repair is the highest-risk setup boundary; executor mutation must preserve MC_SetupRollback's containment-before-privilege invariant.",
	},
	"repair.setup.seatbelt-wrapper": {
		ID:               "repair.setup.seatbelt-wrapper",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairBoundedRollback,
		Preconditions:    []string{"Hazmat binary is installed", "caller approved seatbelt wrapper repair", "wrapper target path is canonical"},
		Receipt:          "receipt.setup.seatbelt-wrapper",
		Verification:     "verify.setup.seatbelt-wrapper",
		RollbackBoundary: "setup.seatbelt",
		TestObligations:  []string{"seatbelt wrapper setup tests", "post-init verification fixture"},
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Seatbelt wrapper repair is setup-owned containment state; mutation must stay inside the setup/rollback boundary.",
	},
	"repair.setup.agent-env": {
		ID:               "repair.setup.agent-env",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityRoot,
		Privileged:       true,
		Reversibility:    diagnosticRepairReversibleByReceipt,
		Preconditions:    []string{"agent user exists", "caller approved agent shell environment repair", "managed shell block markers are valid"},
		Receipt:          "receipt.setup.agent-env",
		Verification:     "verify.setup.agent-env",
		RollbackBoundary: "setup.wrappers",
		TestObligations:  []string{"managed shell env tests", "post-init verification fixture"},
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Agent environment repair is setup wrapper state and must remain receipt-backed before executor mutation is enabled.",
	},
	"repair.setup.host-wrappers": {
		ID:               "repair.setup.host-wrappers",
		Repairability:    diagnosticRepairConsent,
		Authority:        diagnosticRepairAuthorityCurrentUser,
		Privileged:       false,
		Reversibility:    diagnosticRepairReversibleByReceipt,
		Preconditions:    []string{"caller approved host wrapper repair", "host wrapper directory is canonical", "Hazmat binary path verifies"},
		Receipt:          "receipt.setup.host-wrappers",
		Verification:     "verify.setup.host-wrappers",
		RollbackBoundary: "setup.wrappers",
		TestObligations:  []string{"host wrapper setup tests", "post-init verification fixture"},
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Host wrapper repair is setup wrapper state; current-user mutation still needs setup/rollback receipt semantics.",
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
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofGuardedRealHostSmoke, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "PF repair changes setup-owned network containment state; TLA governs privilege/containment ordering and guarded smoke tests cover real host behavior.",
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
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofGuardedRealHostSmoke, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "DNS blocklist repair changes setup-owned network state; TLA governs setup/rollback boundaries and tests cover concrete hosts-file edits.",
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
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofGuardedRealHostSmoke, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "LaunchDaemon persistence is setup-owned containment state; TLA must cover ordering and rollback scope before doctor/init can mutate it.",
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
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLACredentialCapability, diagnosticRepairProofTLASecretStoreRecovery, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofSecretScan, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Claude credential-state repair must preserve host-owned secret-store and no-agent-residue guarantees.",
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
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLACredentialCapability, diagnosticRepairProofTLASecretStoreRecovery, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofSecretScan, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Cloud credential migration is credential-capability reconciliation, not setup migration; concrete parsing stays in tests and secret scans.",
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
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLACredentialCapability, diagnosticRepairProofTLASecretStoreRecovery, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofSecretScan, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "Residue cleanup must retain latest host-owned credential material or conflict archives while removing stale agent-side residue.",
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
		ProofLanes:       []diagnosticRepairProofLane{diagnosticRepairProofTLASessionPermissionRepairs, diagnosticRepairProofUnitTests, diagnosticRepairProofDirtyStateConvergence, diagnosticRepairProofVerifyAfterAction},
		ProofNotes:       "This is a host permission repair class; MC_SessionPermissionRepairs must explicitly cover or be extended before executor mutation is enabled.",
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

func diagnosticRepairProofLaneStrings(lanes []diagnosticRepairProofLane) []string {
	out := make([]string, 0, len(lanes))
	for _, lane := range lanes {
		out = append(out, string(lane))
	}
	return out
}

func validDiagnosticRepairProofLane(lane diagnosticRepairProofLane) bool {
	switch lane {
	case diagnosticRepairProofUnitTests,
		diagnosticRepairProofDirtyStateConvergence,
		diagnosticRepairProofVerifyAfterAction,
		diagnosticRepairProofClassification,
		diagnosticRepairProofGuardedRealHostSmoke,
		diagnosticRepairProofSecretScan,
		diagnosticRepairProofTLASetupRollback,
		diagnosticRepairProofTLASessionPermissionRepairs,
		diagnosticRepairProofTLACredentialCapability,
		diagnosticRepairProofTLASecretStoreRecovery:
		return true
	default:
		return false
	}
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
	if len(p.ProofLanes) == 0 {
		return fmt.Errorf("%s: missing proof lanes", p.Repairability)
	}
	for _, lane := range p.ProofLanes {
		if !validDiagnosticRepairProofLane(lane) {
			return fmt.Errorf("%s: unknown proof lane %q", p.Repairability, lane)
		}
	}
	if p.ProofNotes == "" {
		return fmt.Errorf("%s: missing proof notes", p.Repairability)
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
	if len(a.ProofLanes) == 0 {
		return fmt.Errorf("%s: missing proof lanes", a.ID)
	}
	for _, lane := range a.ProofLanes {
		if !validDiagnosticRepairProofLane(lane) {
			return fmt.Errorf("%s: unknown proof lane %q", a.ID, lane)
		}
	}
	if a.ProofNotes == "" {
		return fmt.Errorf("%s: missing proof notes", a.ID)
	}
	return nil
}
