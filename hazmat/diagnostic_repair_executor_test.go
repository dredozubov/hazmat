package hazmat

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExecuteDiagnosticRepairPlanRequiresMutationPolicy(t *testing.T) {
	ui := &UI{}
	ui.stepLabel = "Hardening gaps"
	ui.recordTypedFinding(uiFindingWarning, diagnosticFinding(findingAgentUmask), "umask missing")
	plan := planDiagnosticRepairs(ui.findings, ui.recommendations(), diagnosticRepairExecutionRequest{Command: "check"})
	backend := &recordingDiagnosticRepairBackend{}

	executed := executeDiagnosticRepairPlan(plan, backend)
	if executed.Mutating || executed.Mode != "preview" {
		t.Fatalf("executed plan = mode %q mutating=%v, want read-only preview", executed.Mode, executed.Mutating)
	}
	if backend.applyCalls != 0 || backend.verifyCalls != 0 {
		t.Fatalf("backend calls = apply %d verify %d, want none", backend.applyCalls, backend.verifyCalls)
	}
	if len(executed.AppliedReceipts) != 0 || len(executed.FailedVerifications) != 0 {
		t.Fatalf("execution state = receipts %+v failures %+v, want none", executed.AppliedReceipts, executed.FailedVerifications)
	}
}

func TestExecuteDiagnosticRepairPlanAppliesAndVerifies(t *testing.T) {
	plan := planForSingleFinding(findingAgentUmask)
	backend := &recordingDiagnosticRepairBackend{
		applyEvidence:  []string{"applied managed umask block"},
		verifyEvidence: []string{"verified umask 007"},
	}

	executed := executeDiagnosticRepairPlan(plan, backend)
	if executed.Mode != "executed" || !executed.Mutating {
		t.Fatalf("executed plan header = mode %q mutating=%v", executed.Mode, executed.Mutating)
	}
	if backend.applyCalls != 1 || backend.verifyCalls != 1 {
		t.Fatalf("backend calls = apply %d verify %d, want 1/1", backend.applyCalls, backend.verifyCalls)
	}
	if len(executed.Items) != 1 || executed.Items[0].Status != diagnosticRepairStatusRepaired {
		t.Fatalf("items = %+v, want repaired item", executed.Items)
	}
	if len(executed.AppliedReceipts) != 1 || !executed.AppliedReceipts[0].Verified {
		t.Fatalf("receipts = %+v, want verified receipt", executed.AppliedReceipts)
	}
	receipt := executed.AppliedReceipts[0]
	if receipt.ID != "receipt.agent-shell.umask" || receipt.Action != "repair.agent-shell.umask" {
		t.Fatalf("receipt identity = %+v, want umask repair receipt", receipt)
	}
	if receipt.ResourceID != "agent-shell.umask" || receipt.ResourceOwner != "setup.agent-shell" {
		t.Fatalf("receipt resource = %+v, want agent shell owner", receipt)
	}
	if receipt.Authority != string(diagnosticRepairAuthorityRoot) || receipt.Reversibility != string(diagnosticRepairReversibleByReceipt) {
		t.Fatalf("receipt authority = %+v, want root reversible receipt", receipt)
	}
	if receipt.Verification != "verify.agent-shell.umask" || receipt.RollbackBoundary != "setup.agent-shell" {
		t.Fatalf("receipt rollback contract = %+v, want umask verification and rollback boundary", receipt)
	}
	if receipt.RollbackModel == "" {
		t.Fatalf("receipt rollback model empty: %+v", receipt)
	}
	if !containsPlanString(receipt.ProofLanes, string(diagnosticRepairProofTLASetupRollback)) {
		t.Fatalf("receipt proof lanes = %v, want setup/rollback TLA lane", receipt.ProofLanes)
	}
	if !containsPlanString(receipt.Details, "applied managed umask block") || !containsPlanString(receipt.Details, "verified umask 007") {
		t.Fatalf("receipt details = %v, want apply and verification evidence", receipt.Details)
	}
	if _, err := time.Parse(time.RFC3339, receipt.CreatedAt); err != nil {
		t.Fatalf("receipt created_at = %q, want RFC3339 timestamp: %v", receipt.CreatedAt, err)
	}
	if len(executed.FailedVerifications) != 0 {
		t.Fatalf("failed verifications = %+v, want none", executed.FailedVerifications)
	}
	if !containsPlanString(executed.Items[0].Details, "verified umask 007") {
		t.Fatalf("item details = %+v, want verification evidence", executed.Items[0].Details)
	}
}

func TestExecuteDiagnosticRepairPlanFailedVerificationKeepsEvidence(t *testing.T) {
	plan := planForSingleFinding(findingAgentUmask)
	backend := &recordingDiagnosticRepairBackend{
		applyEvidence:  []string{"applied managed umask block"},
		verifyEvidence: []string{"observed umask 022"},
		verifyErr:      errors.New("umask still wrong"),
	}

	executed := executeDiagnosticRepairPlan(plan, backend)
	if len(executed.Items) != 1 || executed.Items[0].Status != diagnosticRepairStatusStillFailing {
		t.Fatalf("items = %+v, want still-failing item", executed.Items)
	}
	if len(executed.AppliedReceipts) != 0 {
		t.Fatalf("receipts = %+v, want none for failed verification", executed.AppliedReceipts)
	}
	if len(executed.FailedVerifications) != 1 {
		t.Fatalf("failed verifications = %+v, want one", executed.FailedVerifications)
	}
	details := strings.Join(executed.FailedVerifications[0].Details, "\n")
	if !strings.Contains(details, "observed umask 022") || strings.Contains(details, "hazmat init") {
		t.Fatalf("failure details = %q, want evidence without init-loop advice", details)
	}
	if executed.Items[0].BlockedReason == "" || executed.Items[0].Reason == "" {
		t.Fatalf("failed item = %+v, want blocked reason and next classification", executed.Items[0])
	}
}

func planForSingleFinding(id diagnosticFindingID) diagnosticRepairPlan {
	ui := &UI{}
	ui.stepLabel = "executor test"
	ui.recordTypedFinding(uiFindingWarning, diagnosticFinding(id), "dirty state")
	return planDiagnosticRepairs(ui.findings, ui.recommendations(), diagnosticRepairExecutionRequest{Command: "doctor", Fix: true, YesAll: true})
}

type recordingDiagnosticRepairBackend struct {
	applyCalls     int
	verifyCalls    int
	applyEvidence  []string
	verifyEvidence []string
	applyErr       error
	verifyErr      error
}

func (b *recordingDiagnosticRepairBackend) ApplyDiagnosticRepair(diagnosticRepairActionDefinition, diagnosticRepairPlanItem) diagnosticRepairStepResult {
	b.applyCalls++
	return diagnosticRepairStepResult{Evidence: append([]string(nil), b.applyEvidence...), Err: b.applyErr}
}

func (b *recordingDiagnosticRepairBackend) VerifyDiagnosticRepair(diagnosticRepairActionDefinition, diagnosticRepairPlanItem) diagnosticRepairStepResult {
	b.verifyCalls++
	return diagnosticRepairStepResult{Evidence: append([]string(nil), b.verifyEvidence...), Err: b.verifyErr}
}
