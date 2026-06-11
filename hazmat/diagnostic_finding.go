package hazmat

import (
	"fmt"
	"sort"
	"strings"
)

type diagnosticFindingID string
type diagnosticResourceID string
type diagnosticRepairActionID string
type diagnosticRepairReceiptID string
type diagnosticVerificationID string

type diagnosticRepairability string

const (
	diagnosticRepairAuto           diagnosticRepairability = "auto"
	diagnosticRepairConsent        diagnosticRepairability = "consent-required"
	diagnosticRepairManualExternal diagnosticRepairability = "manual-external"
	diagnosticRepairUnsupported    diagnosticRepairability = "unsupported"
	diagnosticRepairOptional       diagnosticRepairability = "optional"
	diagnosticRepairInformational  diagnosticRepairability = "informational"
)

type diagnosticResourceDefinition struct {
	ID           diagnosticResourceID
	Owner        string
	DesiredState string
}

type diagnosticFindingDefinition struct {
	ID               diagnosticFindingID
	Resource         diagnosticResourceID
	Title            string
	Repairability    diagnosticRepairability
	Action           string
	RepairAction     diagnosticRepairActionID
	RepairReceipt    diagnosticRepairReceiptID
	Verification     diagnosticVerificationID
	SecurityImpact   string
	RollbackBoundary string
	GroupKey         string
}

func (d diagnosticFindingDefinition) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("missing finding id")
	}
	if d.Resource == "" {
		return fmt.Errorf("%s: missing resource", d.ID)
	}
	if strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("%s: missing title", d.ID)
	}
	if d.Repairability == "" {
		return fmt.Errorf("%s: missing repairability", d.ID)
	}
	switch d.Repairability {
	case diagnosticRepairAuto, diagnosticRepairConsent, diagnosticRepairManualExternal,
		diagnosticRepairUnsupported, diagnosticRepairOptional, diagnosticRepairInformational:
	default:
		return fmt.Errorf("%s: unknown repairability %q", d.ID, d.Repairability)
	}
	if strings.TrimSpace(d.Action) == "" {
		return fmt.Errorf("%s: missing action text", d.ID)
	}
	if strings.TrimSpace(d.SecurityImpact) == "" {
		return fmt.Errorf("%s: missing security impact", d.ID)
	}
	if d.IsHazmatRepairable() {
		if d.RepairAction == "" {
			return fmt.Errorf("%s: repairable finding has no repair action", d.ID)
		}
		if d.RepairReceipt == "" {
			return fmt.Errorf("%s: repairable finding has no repair receipt", d.ID)
		}
		if d.Verification == "" {
			return fmt.Errorf("%s: repairable finding has no verification", d.ID)
		}
		if strings.TrimSpace(d.RollbackBoundary) == "" {
			return fmt.Errorf("%s: repairable finding has no rollback boundary", d.ID)
		}
	} else if d.RepairAction != "" {
		return fmt.Errorf("%s: non-repairable finding must not name a repair action", d.ID)
	} else if d.RepairReceipt != "" {
		return fmt.Errorf("%s: non-repairable finding must not name a repair receipt", d.ID)
	}
	return nil
}

func (d diagnosticFindingDefinition) IsHazmatRepairable() bool {
	return d.Repairability == diagnosticRepairAuto || d.Repairability == diagnosticRepairConsent
}

func (d diagnosticFindingDefinition) RecommendationKey() string {
	if d.GroupKey != "" {
		return d.GroupKey
	}
	return string(d.ID)
}

func mustDiagnosticFinding(def diagnosticFindingDefinition) diagnosticFindingDefinition {
	if err := def.Validate(); err != nil {
		panic(err)
	}
	return def
}

const (
	findingWorkspaceSetgid                diagnosticFindingID = "workspace.setgid-inheritance"
	findingWorkspaceAccess                diagnosticFindingID = "workspace.access"
	findingAgentHomeReadable              diagnosticFindingID = "agent-home.host-readable"
	findingAgentUmask                     diagnosticFindingID = "agent-shell.umask"
	findingDockerSocketPermissions        diagnosticFindingID = "docker.socket-permissions"
	findingPFFirewall                     diagnosticFindingID = "network.pf-firewall"
	findingDNSBlocklist                   diagnosticFindingID = "network.dns-blocklist"
	findingLaunchdPersistence             diagnosticFindingID = "network.launchd-persistence"
	findingCredentialClaudeStateResidue   diagnosticFindingID = "credential.claude-state-residue"
	findingCredentialCloudSecretKeyLegacy diagnosticFindingID = "credential.cloud-secret-key-legacy"
	findingCredentialResidue              diagnosticFindingID = "credential.residue"
	findingCredentialAdapterRequired      diagnosticFindingID = "credential.adapter-required"
	findingAgentSSHKey                    diagnosticFindingID = "agent-tooling.ssh-key"
	findingAnthropicAPIKey                diagnosticFindingID = "agent-tooling.anthropic-api-key"
	findingAgentGitIdentity               diagnosticFindingID = "agent-tooling.git-identity"
	findingClaudeProjectPermissions       diagnosticFindingID = "claude.project-permissions"
	findingAgentToolPath                  diagnosticFindingID = "project-toolchain.agent-path"
	findingIntegrationToolchain           diagnosticFindingID = "project-toolchain.integration-toolchain"
	findingGolangCILintAccess             diagnosticFindingID = "project-toolchain.golangci-lint"
	findingTLA2ToolsJar                   diagnosticFindingID = "project-toolchain.tla2tools-jar"
)

var diagnosticResourceDefinitions = map[diagnosticResourceID]diagnosticResourceDefinition{
	"workspace.group-inheritance": {
		ID:           "workspace.group-inheritance",
		Owner:        "setup.workspace",
		DesiredState: fmt.Sprintf("new files under the project inherit group `%s`", sharedGroup),
	},
	"workspace.access": {
		ID:           "workspace.access",
		Owner:        "setup.workspace",
		DesiredState: "host and agent can both traverse and write expected shared project state",
	},
	"agent-home.permissions": {
		ID:           "agent-home.permissions",
		Owner:        "setup.agent-account",
		DesiredState: "agent home state is private unless a path is intentionally shared",
	},
	"agent-shell.umask": {
		ID:           "agent-shell.umask",
		Owner:        "setup.agent-shell",
		DesiredState: "agent shells create collaboration files with group access and without world access",
	},
	"docker.socket": {
		ID:           "docker.socket",
		Owner:        "host.docker",
		DesiredState: "Docker socket exposure is absent or owner-only before autonomous agent runs",
	},
	"network.pf": {
		ID:           "network.pf",
		Owner:        "setup.network",
		DesiredState: "Hazmat-managed pf anchor is present, loaded, and enforcing intended egress blocks",
	},
	"network.dns-blocklist": {
		ID:           "network.dns-blocklist",
		Owner:        "setup.network",
		DesiredState: "Hazmat-managed DNS blocklist entries resolve blocked domains to 0.0.0.0 or fail",
	},
	"network.launchd-persistence": {
		ID:           "network.launchd-persistence",
		Owner:        "setup.network",
		DesiredState: "Hazmat network policy reloads after reboot through managed launchd state",
	},
	"credential.claude-state": {
		ID:           "credential.claude-state",
		Owner:        "credential.inventory",
		DesiredState: "Claude state lives in the host-owned secret store without stale agent-home residue",
	},
	"credential.cloud-secret-key": {
		ID:           "credential.cloud-secret-key",
		Owner:        "credential.inventory",
		DesiredState: "cloud secret keys live in the host-owned secret store, not legacy plaintext paths",
	},
	"credential.residue": {
		ID:           "credential.residue",
		Owner:        "credential.inventory",
		DesiredState: "credential material is represented by a registered backend and no stale residue",
	},
	"credential.backend-adapter": {
		ID:           "credential.backend-adapter",
		Owner:        "credential.inventory",
		DesiredState: "configured credential surfaces have a backend adapter before Hazmat promises lifecycle guarantees",
	},
	"agent-tooling.ssh-key": {
		ID:           "agent-tooling.ssh-key",
		Owner:        "agent.tooling",
		DesiredState: "optional Git SSH credentials exist only when the workflow needs them",
	},
	"agent-tooling.anthropic-auth": {
		ID:           "agent-tooling.anthropic-auth",
		Owner:        "agent.tooling",
		DesiredState: "optional provider auth is configured through Hazmat-owned credential surfaces when needed",
	},
	"agent-tooling.git-identity": {
		ID:           "agent-tooling.git-identity",
		Owner:        "agent.tooling",
		DesiredState: "agent Git identity is configured when the agent account is expected to create commits",
	},
	"claude.project-permissions": {
		ID:           "claude.project-permissions",
		Owner:        "agent.tooling",
		DesiredState: "Claude project directories are writable by the shared group for resume/export sync",
	},
	"project-toolchain.agent-path": {
		ID:           "project-toolchain.agent-path",
		Owner:        "project.toolchain",
		DesiredState: "required project tools are executable by the agent user",
	},
	"project-toolchain.integration": {
		ID:           "project-toolchain.integration",
		Owner:        "project.toolchain",
		DesiredState: "integration metadata explains any extra readable toolchain paths the agent needs",
	},
	"project-toolchain.golangci-lint": {
		ID:           "project-toolchain.golangci-lint",
		Owner:        "project.toolchain",
		DesiredState: "golangci-lint is executable by the agent when Go lint workflows require it",
	},
	"project-toolchain.tla2tools": {
		ID:           "project-toolchain.tla2tools",
		Owner:        "project.toolchain",
		DesiredState: "tla2tools.jar is readable when formal-methods checks require it",
	},
}

var diagnosticFindingDefinitions = map[diagnosticFindingID]diagnosticFindingDefinition{
	findingWorkspaceSetgid: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingWorkspaceSetgid,
		Resource:         "workspace.group-inheritance",
		Title:            "Repair workspace group inheritance",
		Repairability:    diagnosticRepairConsent,
		Action:           fmt.Sprintf("Repair workspace group ownership and setgid state through Hazmat, then verify new files inherit `%s`.", sharedGroup),
		RepairAction:     "repair.workspace.setgid",
		RepairReceipt:    "receipt.workspace.setgid",
		Verification:     "verify.workspace.setgid",
		SecurityImpact:   "Incorrect group inheritance can break host/agent collaboration and make init appear non-convergent.",
		RollbackBoundary: "setup.workspace-permissions",
	}),
	findingWorkspaceAccess: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingWorkspaceAccess,
		Resource:         "workspace.access",
		Title:            "Repair workspace group access",
		Repairability:    diagnosticRepairConsent,
		Action:           "Repair dev-group membership and workspace ACL defaults through Hazmat, then rerun the workspace access checks.",
		RepairAction:     "repair.workspace.access",
		RepairReceipt:    "receipt.workspace.access",
		Verification:     "verify.workspace.access",
		SecurityImpact:   "Missing workspace access prevents the agent and host from sharing project state reliably.",
		RollbackBoundary: "setup.workspace-permissions",
	}),
	findingAgentHomeReadable: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingAgentHomeReadable,
		Resource:         "agent-home.permissions",
		Title:            "Restrict agent home permissions",
		Repairability:    diagnosticRepairConsent,
		Action:           fmt.Sprintf("Restrict `%s` after confirming no intentional shared files depend on broader agent-home access.", agentHome),
		RepairAction:     "repair.agent-home.permissions",
		RepairReceipt:    "receipt.agent-home.permissions",
		Verification:     "verify.agent-home.permissions",
		SecurityImpact:   "Host readability of agent shell state weakens user isolation expectations and should be explicit.",
		RollbackBoundary: "setup.agent-account",
	}),
	findingAgentUmask: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingAgentUmask,
		Resource:         "agent-shell.umask",
		Title:            "Restore the agent umask",
		Repairability:    diagnosticRepairConsent,
		Action:           "Restore Hazmat's managed agent shell block with `umask 007`, then verify new agent files are not world-readable.",
		RepairAction:     "repair.agent-shell.umask",
		RepairReceipt:    "receipt.agent-shell.umask",
		Verification:     "verify.agent-shell.umask",
		SecurityImpact:   "Permissive defaults can create files that are broader than Hazmat's intended collaboration model.",
		RollbackBoundary: "setup.agent-shell",
	}),
	findingDockerSocketPermissions: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:             findingDockerSocketPermissions,
		Resource:       "docker.socket",
		Title:          "Restrict the Docker socket",
		Repairability:  diagnosticRepairManualExternal,
		Action:         "Set the Docker socket to owner-only access or disable Docker socket exposure before autonomous agent runs.",
		SecurityImpact: "A readable Docker socket can let the agent escape containment through host Docker.",
	}),
	findingPFFirewall: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingPFFirewall,
		Resource:         "network.pf",
		Title:            "Restore and reload the packet-filter rules",
		Repairability:    diagnosticRepairConsent,
		Action:           "Restore Hazmat-managed pf configuration, reload pf, and validate with the static and live firewall checks.",
		RepairAction:     "repair.network.pf",
		RepairReceipt:    "receipt.network.pf",
		Verification:     "verify.network.pf",
		SecurityImpact:   "Broken pf rules can leave the agent with unintended network egress.",
		RollbackBoundary: "setup.network-pf",
	}),
	findingDNSBlocklist: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingDNSBlocklist,
		Resource:         "network.dns-blocklist",
		Title:            "Restore the DNS blocklist",
		Repairability:    diagnosticRepairConsent,
		Action:           "Restore Hazmat-managed DNS blocklist entries and verify blocked domains resolve to 0.0.0.0 or fail.",
		RepairAction:     "repair.network.dns-blocklist",
		RepairReceipt:    "receipt.network.dns-blocklist",
		Verification:     "verify.network.dns-blocklist",
		SecurityImpact:   "Missing DNS blocklist entries reopen known exfiltration paths.",
		RollbackBoundary: "setup.network-dns-blocklist",
	}),
	findingLaunchdPersistence: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingLaunchdPersistence,
		Resource:         "network.launchd-persistence",
		Title:            "Restore firewall persistence",
		Repairability:    diagnosticRepairConsent,
		Action:           "Restore the Hazmat LaunchDaemon and pf.conf anchor reference, then verify rules survive reload.",
		RepairAction:     "repair.network.persistence",
		RepairReceipt:    "receipt.network.persistence",
		Verification:     "verify.network.persistence",
		SecurityImpact:   "Missing persistence can silently drop network policy after reboot.",
		RollbackBoundary: "setup.network-persistence",
	}),
	findingCredentialClaudeStateResidue: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingCredentialClaudeStateResidue,
		Resource:         "credential.claude-state",
		Title:            "Repair stale Claude state residue",
		Repairability:    diagnosticRepairConsent,
		Action:           "Harvest or remove stale agent-home Claude state after verifying the host-owned secret store has the latest value.",
		RepairAction:     "repair.credential.claude-state",
		RepairReceipt:    "receipt.credential.claude-state",
		Verification:     "verify.credential.claude-state",
		SecurityImpact:   "Credential residue in the agent home weakens the host-owned secret-store boundary.",
		RollbackBoundary: "credentials.host-secret-store",
	}),
	findingCredentialCloudSecretKeyLegacy: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingCredentialCloudSecretKeyLegacy,
		Resource:         "credential.cloud-secret-key",
		Title:            "Migrate legacy cloud secret key",
		Repairability:    diagnosticRepairConsent,
		Action:           "Migrate the legacy cloud secret key into the host-owned secret store and verify the legacy file is no longer authoritative.",
		RepairAction:     "repair.credential.cloud-secret-key",
		RepairReceipt:    "receipt.credential.cloud-secret-key",
		Verification:     "verify.credential.cloud-secret-key",
		SecurityImpact:   "Legacy cloud credential files bypass the typed credential registry.",
		RollbackBoundary: "credentials.host-secret-store",
	}),
	findingCredentialResidue: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingCredentialResidue,
		Resource:         "credential.residue",
		Title:            "Repair credential store drift",
		Repairability:    diagnosticRepairConsent,
		Action:           "Migrate or remove stale credential material using the registered credential backend for this surface.",
		RepairAction:     "repair.credential.residue",
		RepairReceipt:    "receipt.credential.residue",
		Verification:     "verify.credential.residue",
		SecurityImpact:   "Credential residue can bypass Hazmat's host-owned credential lifecycle.",
		RollbackBoundary: "credentials.host-secret-store",
	}),
	findingCredentialAdapterRequired: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:             findingCredentialAdapterRequired,
		Resource:       "credential.backend-adapter",
		Title:          "Avoid credentials without a backend adapter",
		Repairability:  diagnosticRepairUnsupported,
		Action:         "Do not rely on this credential path until Hazmat has a backend adapter for it, or use a supported credential backend.",
		SecurityImpact: "Unsupported credential backends cannot participate in Hazmat's delivery and cleanup guarantees.",
	}),
	findingAgentSSHKey: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:             findingAgentSSHKey,
		Resource:       "agent-tooling.ssh-key",
		Title:          "Create an agent SSH key if Git SSH access is needed",
		Repairability:  diagnosticRepairOptional,
		Action:         "Treat this as an optional capability unless the current workflow requires Git SSH access; key registration with a remote provider remains external.",
		SecurityImpact: "Missing SSH keys block optional Git SSH workflows but do not weaken containment.",
	}),
	findingAnthropicAPIKey: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:             findingAnthropicAPIKey,
		Resource:       "agent-tooling.anthropic-auth",
		Title:          "Configure Claude authentication if needed",
		Repairability:  diagnosticRepairOptional,
		Action:         "Treat this as optional unless Claude API-key sessions are required; use the host-owned secret store or Claude login flow when needed.",
		SecurityImpact: "Missing provider auth blocks optional harness use but does not weaken containment.",
	}),
	findingAgentGitIdentity: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:             findingAgentGitIdentity,
		Resource:       "agent-tooling.git-identity",
		Title:          "Configure the agent Git identity",
		Repairability:  diagnosticRepairOptional,
		Action:         "Configure agent Git identity only when commits from the agent account are expected.",
		SecurityImpact: "Missing Git identity affects developer ergonomics, not containment.",
	}),
	findingClaudeProjectPermissions: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:               findingClaudeProjectPermissions,
		Resource:         "claude.project-permissions",
		Title:            "Repair Claude project resume/export permissions",
		Repairability:    diagnosticRepairConsent,
		Action:           "Repair group-writable permissions for each affected Claude project directory, then verify resume/export can write temp files.",
		RepairAction:     "repair.claude.project-permissions",
		RepairReceipt:    "receipt.claude.project-permissions",
		Verification:     "verify.claude.project-permissions",
		SecurityImpact:   "Restrictive Claude project directories break host resume/export synchronization.",
		RollbackBoundary: "agent-home.claude-projects",
		GroupKey:         "claude.project-permissions",
	}),
	findingAgentToolPath: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:             findingAgentToolPath,
		Resource:       "project-toolchain.agent-path",
		Title:          "Install or expose the missing project tool",
		Repairability:  diagnosticRepairManualExternal,
		Action:         "Install the missing tool for the agent user, or update the active integration so its toolchain is readable and on the agent PATH.",
		SecurityImpact: "Missing tools block selected project workflows but do not weaken containment.",
	}),
	findingIntegrationToolchain: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:             findingIntegrationToolchain,
		Resource:       "project-toolchain.integration",
		Title:          "Classify unresolved integration toolchain metadata",
		Repairability:  diagnosticRepairInformational,
		Action:         "Treat this as informational when command probes pass; add a resolver only if the integration needs extra read-only paths or permission repairs.",
		SecurityImpact: "Unresolved metadata can make diagnostics noisy even when the tool is actually executable.",
	}),
	findingGolangCILintAccess: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:             findingGolangCILintAccess,
		Resource:       "project-toolchain.golangci-lint",
		Title:          "Expose golangci-lint to the agent user",
		Repairability:  diagnosticRepairManualExternal,
		Action:         "Install golangci-lint via Homebrew or repair Homebrew permissions so the agent user can execute it.",
		SecurityImpact: "Missing lint tooling blocks optional project checks but does not weaken containment.",
	}),
	findingTLA2ToolsJar: mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:             findingTLA2ToolsJar,
		Resource:       "project-toolchain.tla2tools",
		Title:          "Expose tla2tools.jar to Hazmat",
		Repairability:  diagnosticRepairManualExternal,
		Action:         "Set TLA2TOOLS_JAR to a readable tla2tools.jar path or place it at the default workspace location.",
		SecurityImpact: "Missing TLA tooling blocks formal-methods workflows but does not weaken containment.",
	}),
}

func diagnosticFinding(id diagnosticFindingID) diagnosticFindingDefinition {
	def, ok := diagnosticFindingDefinitions[id]
	if !ok {
		panic(fmt.Sprintf("unknown diagnostic finding id %q", id))
	}
	return def
}

func diagnosticFindingIDs() []diagnosticFindingID {
	ids := make([]diagnosticFindingID, 0, len(diagnosticFindingDefinitions))
	for id := range diagnosticFindingDefinitions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func diagnosticCredentialFinding(entry credentialInventoryEntry) diagnosticFindingDefinition {
	switch entry.Status() {
	case credentialInventoryAdapterRequired:
		return diagnosticFinding(findingCredentialAdapterRequired)
	case credentialInventoryNeedsRepair:
		switch entry.ID {
		case credentialHarnessClaudeState:
			return diagnosticFinding(findingCredentialClaudeStateResidue)
		case credentialCloudS3SecretKey:
			return diagnosticFinding(findingCredentialCloudSecretKeyLegacy)
		default:
			return diagnosticFinding(findingCredentialResidue)
		}
	case credentialInventoryConfigured, credentialInventoryNotConfigured, credentialInventoryExternal, credentialInventoryError:
		panic(fmt.Sprintf("credential entry %s has no diagnostic finding for status %s", entry.ID, entry.Status()))
	}
	panic(fmt.Sprintf("credential entry %s has unknown diagnostic status %s", entry.ID, entry.Status()))
}
