package hazmat

import (
	"testing"

	"hazmat/credentials"
)

func TestDiagnosticFindingDefinitionsValidate(t *testing.T) {
	for _, id := range diagnosticFindingIDs() {
		def := diagnosticFinding(id)
		if err := def.Validate(); err != nil {
			t.Fatalf("%s: Validate(): %v", id, err)
		}
	}
}

func TestDiagnosticFindingResourcesHaveDefinitions(t *testing.T) {
	for _, id := range diagnosticFindingIDs() {
		def := diagnosticFinding(id)
		resource, ok := diagnosticResourceDefinitions[def.Resource]
		if !ok {
			t.Fatalf("%s: resource %s has no definition", id, def.Resource)
		}
		if resource.ID != def.Resource {
			t.Fatalf("%s: resource definition ID = %s, want %s", id, resource.ID, def.Resource)
		}
		if resource.Owner == "" {
			t.Fatalf("%s: resource %s has no owner", id, def.Resource)
		}
		if resource.DesiredState == "" {
			t.Fatalf("%s: resource %s has no desired state", id, def.Resource)
		}
	}
}

func TestRepairableDiagnosticFindingRequiresRepairActionAndVerification(t *testing.T) {
	def := diagnosticFindingDefinition{
		ID:             "test.repairable",
		Resource:       "test.resource",
		Title:          "Test repairable finding",
		Repairability:  diagnosticRepairConsent,
		Action:         "Repair the test resource.",
		SecurityImpact: "Test repair security impact.",
	}

	if err := def.Validate(); err == nil {
		t.Fatal("Validate() = nil, want missing repair action and verification error")
	}
}

func TestRepairableDiagnosticFindingRequiresReceipt(t *testing.T) {
	def := diagnosticFindingDefinition{
		ID:               "test.repairable",
		Resource:         "test.resource",
		Title:            "Test repairable finding",
		Repairability:    diagnosticRepairConsent,
		Action:           "Repair the test resource.",
		RepairAction:     "repair.test",
		Verification:     "verify.test",
		SecurityImpact:   "Test repair security impact.",
		RollbackBoundary: "test.rollback",
	}

	if err := def.Validate(); err == nil {
		t.Fatal("Validate() = nil, want missing repair receipt error")
	}
}

func TestSetupDiagnosticFindingsHaveRepairActions(t *testing.T) {
	ids := []diagnosticFindingID{
		findingSetupAgentUser,
		findingSetupAgentHome,
		findingSetupDevGroup,
		findingSetupHomeTraverse,
		findingSetupSudoers,
		findingSetupSeatbeltWrapper,
		findingSetupAgentEnv,
		findingSetupHostWrappers,
	}
	for _, id := range ids {
		def := diagnosticFinding(id)
		if !def.IsHazmatRepairable() {
			t.Fatalf("%s repairability = %s, want Hazmat-repairable setup finding", id, def.Repairability)
		}
		if _, ok := diagnosticRepairAction(def.RepairAction); !ok {
			t.Fatalf("%s repair action %s missing action definition", id, def.RepairAction)
		}
	}
}

func TestDiagnosticFindingRejectsUnknownRepairability(t *testing.T) {
	def := diagnosticFindingDefinition{
		ID:             "test.unknown",
		Resource:       "test.resource",
		Title:          "Test unknown finding",
		Repairability:  "maybe",
		Action:         "Repair the test resource.",
		SecurityImpact: "Test security impact.",
	}

	if err := def.Validate(); err == nil {
		t.Fatal("Validate() = nil, want unknown repairability error")
	}
}

func TestManualDiagnosticFindingCannotCarryRepairAction(t *testing.T) {
	def := diagnosticFindingDefinition{
		ID:             "test.manual",
		Resource:       "test.resource",
		Title:          "Test manual finding",
		Repairability:  diagnosticRepairManualExternal,
		Action:         "Repair the test resource outside Hazmat.",
		RepairAction:   "repair.test",
		SecurityImpact: "Test manual security impact.",
	}

	if err := def.Validate(); err == nil {
		t.Fatal("Validate() = nil, want non-repairable repair action error")
	}
}

func TestManualDiagnosticFindingCannotCarryRepairReceipt(t *testing.T) {
	def := diagnosticFindingDefinition{
		ID:             "test.manual",
		Resource:       "test.resource",
		Title:          "Test manual finding",
		Repairability:  diagnosticRepairManualExternal,
		Action:         "Repair the test resource outside Hazmat.",
		RepairReceipt:  "receipt.test",
		SecurityImpact: "Test manual security impact.",
	}

	if err := def.Validate(); err == nil {
		t.Fatal("Validate() = nil, want non-repairable repair receipt error")
	}
}

func TestDiagnosticCredentialFindingSelectsSpecificDefinitions(t *testing.T) {
	claude := credentialInventoryEntry{ID: credentials.HarnessClaudeState, AgentResidue: []credentialInventoryFinding{{Path: "/Users/agent/.claude.json"}}}
	if got := diagnosticCredentialFinding(claude).ID; got != findingCredentialClaudeStateResidue {
		t.Fatalf("claude state finding = %s, want %s", got, findingCredentialClaudeStateResidue)
	}

	cloud := credentialInventoryEntry{ID: credentials.CloudS3SecretKey, LegacyResidue: []credentialInventoryFinding{{Path: cloudCredentialPath}}}
	if got := diagnosticCredentialFinding(cloud).ID; got != findingCredentialCloudSecretKeyLegacy {
		t.Fatalf("cloud secret finding = %s, want %s", got, findingCredentialCloudSecretKeyLegacy)
	}

	adapter := credentialInventoryEntry{ID: credentials.HarnessAntigravityKeychain, Support: credentials.SupportAdapterRequired}
	if got := diagnosticCredentialFinding(adapter).ID; got != findingCredentialAdapterRequired {
		t.Fatalf("adapter finding = %s, want %s", got, findingCredentialAdapterRequired)
	}
}
