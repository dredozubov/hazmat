package hazmat

import "testing"

func TestDecideDiagnosticRepairExecutionCheckIsReadOnly(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "check"})

	if policy.Mode != "read-only" || policy.MutationAllowed || policy.RequiresFix {
		t.Fatalf("policy = %+v, want read-only check", policy)
	}
}

func TestDecideDiagnosticRepairExecutionDoctorPlanOnlyByDefault(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "doctor"})

	if policy.Mode != "plan-only" || policy.MutationAllowed || !policy.RequiresFix {
		t.Fatalf("policy = %+v, want plan-only doctor requiring --fix", policy)
	}
}

func TestDecideDiagnosticRepairExecutionBlocksNonInteractiveFixWithoutYes(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "doctor", Fix: true})

	if policy.Mode != "blocked-noninteractive" || policy.MutationAllowed || !policy.RequiresYes {
		t.Fatalf("policy = %+v, want blocked non-interactive fix without --yes", policy)
	}
}

func TestDecideDiagnosticRepairExecutionAllowsFixYes(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "doctor", Fix: true, YesAll: true})

	if policy.Mode != "fix-yes" || !policy.MutationAllowed || !policy.RequiresYes {
		t.Fatalf("policy = %+v, want --fix --yes mutation allowance", policy)
	}
}

func TestDecideDiagnosticRepairExecutionAllowsInteractiveFixWithConsent(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "doctor", Fix: true, Interactive: true})

	if policy.Mode != "fix-interactive" || !policy.MutationAllowed || !policy.RequiresInteractiveConsent {
		t.Fatalf("policy = %+v, want interactive fix consent", policy)
	}
}
