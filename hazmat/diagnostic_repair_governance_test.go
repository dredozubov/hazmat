package hazmat

import "testing"

func TestDiagnosticRepairClassPoliciesValidate(t *testing.T) {
	for _, repairability := range []diagnosticRepairability{
		diagnosticRepairAuto,
		diagnosticRepairConsent,
		diagnosticRepairManualExternal,
		diagnosticRepairUnsupported,
		diagnosticRepairOptional,
		diagnosticRepairInformational,
	} {
		policy, ok := diagnosticRepairClassPolicyFor(repairability)
		if !ok {
			t.Fatalf("%s: missing class policy", repairability)
		}
		if policy.Repairability != repairability {
			t.Fatalf("%s: policy repairability = %s", repairability, policy.Repairability)
		}
		if err := policy.Validate(); err != nil {
			t.Fatalf("%s: Validate(): %v", repairability, err)
		}
		if len(policy.ProofLanes) == 0 || policy.ProofNotes == "" {
			t.Fatalf("%s: missing proof lane metadata", repairability)
		}
	}
}

func TestDiagnosticRepairActionsValidateAndMatchFindings(t *testing.T) {
	for id, action := range diagnosticRepairActionDefinitions {
		if action.ID != id {
			t.Fatalf("%s: action ID = %s", id, action.ID)
		}
		if err := action.Validate(); err != nil {
			t.Fatalf("%s: Validate(): %v", id, err)
		}
		if len(action.ProofLanes) == 0 || action.ProofNotes == "" {
			t.Fatalf("%s: missing proof lane metadata", id)
		}
	}

	for _, id := range diagnosticFindingIDs() {
		finding := diagnosticFinding(id)
		if !finding.IsHazmatRepairable() {
			continue
		}
		action, ok := diagnosticRepairAction(finding.RepairAction)
		if !ok {
			t.Fatalf("%s: repair action %s has no governance definition", id, finding.RepairAction)
		}
		if action.Repairability != finding.Repairability {
			t.Fatalf("%s: action repairability = %s, want %s", id, action.Repairability, finding.Repairability)
		}
		if action.Receipt != finding.RepairReceipt {
			t.Fatalf("%s: action receipt = %s, want %s", id, action.Receipt, finding.RepairReceipt)
		}
		if action.Verification != finding.Verification {
			t.Fatalf("%s: action verification = %s, want %s", id, action.Verification, finding.Verification)
		}
		if action.RollbackBoundary != finding.RollbackBoundary {
			t.Fatalf("%s: action rollback boundary = %s, want %s", id, action.RollbackBoundary, finding.RollbackBoundary)
		}
	}
}

func TestRepairActionsDeclareExpectedTLALanes(t *testing.T) {
	tests := []struct {
		action diagnosticRepairActionID
		lanes  []diagnosticRepairProofLane
	}{
		{"repair.workspace.setgid", []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback}},
		{"repair.workspace.access", []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback, diagnosticRepairProofTLASessionPermissionRepairs}},
		{"repair.agent-home.permissions", []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback}},
		{"repair.agent-shell.umask", []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback}},
		{"repair.network.pf", []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback}},
		{"repair.network.dns-blocklist", []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback}},
		{"repair.network.persistence", []diagnosticRepairProofLane{diagnosticRepairProofTLASetupRollback}},
		{"repair.credential.claude-state", []diagnosticRepairProofLane{diagnosticRepairProofTLACredentialCapability, diagnosticRepairProofTLASecretStoreRecovery}},
		{"repair.credential.cloud-secret-key", []diagnosticRepairProofLane{diagnosticRepairProofTLACredentialCapability, diagnosticRepairProofTLASecretStoreRecovery}},
		{"repair.credential.residue", []diagnosticRepairProofLane{diagnosticRepairProofTLACredentialCapability, diagnosticRepairProofTLASecretStoreRecovery}},
		{"repair.claude.project-permissions", []diagnosticRepairProofLane{diagnosticRepairProofTLASessionPermissionRepairs}},
	}

	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			action, ok := diagnosticRepairAction(tt.action)
			if !ok {
				t.Fatalf("missing action %s", tt.action)
			}
			for _, lane := range tt.lanes {
				if !diagnosticRepairActionHasProofLane(action, lane) {
					t.Fatalf("%s proof lanes = %v, want %s", tt.action, action.ProofLanes, lane)
				}
			}
			if !diagnosticRepairActionHasProofLane(action, diagnosticRepairProofDirtyStateConvergence) &&
				!diagnosticRepairActionHasProofLane(action, diagnosticRepairProofGuardedRealHostSmoke) {
				t.Fatalf("%s proof lanes = %v, want convergence or guarded smoke lane", tt.action, action.ProofLanes)
			}
		})
	}
}

func diagnosticRepairActionHasProofLane(action diagnosticRepairActionDefinition, want diagnosticRepairProofLane) bool {
	for _, lane := range action.ProofLanes {
		if lane == want {
			return true
		}
	}
	return false
}

func TestNonExecutableRepairClassesStayNonExecutable(t *testing.T) {
	for _, id := range diagnosticFindingIDs() {
		finding := diagnosticFinding(id)
		policy, ok := diagnosticRepairClassPolicyFor(finding.Repairability)
		if !ok {
			t.Fatalf("%s: missing policy for %s", id, finding.Repairability)
		}
		if finding.IsHazmatRepairable() {
			continue
		}
		if policy.ExecutableByHazmat {
			t.Fatalf("%s: non-repairable finding class %s is executable", id, finding.Repairability)
		}
		if finding.RepairAction != "" {
			t.Fatalf("%s: non-repairable finding has repair action %s", id, finding.RepairAction)
		}
	}
}
