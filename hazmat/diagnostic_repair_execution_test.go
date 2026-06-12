package hazmat

import (
	"slices"
	"testing"
)

func TestDecideDiagnosticRepairExecutionCheckIsReadOnly(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "check"})

	if policy.Mode != "read-only" || policy.MutationAllowed || policy.RequiresFix {
		t.Fatalf("policy = %+v, want read-only check", policy)
	}
	if !slices.Contains(policy.Examples, "hazmat doctor --fix") || !slices.Contains(policy.Examples, "hazmat doctor --dry-run") || indexOf(policy.Examples, "hazmat doctor --dry-run") > indexOf(policy.Examples, "hazmat doctor --fix") {
		t.Fatalf("examples = %v, want explicit dry-run preview before direct fix path", policy.Examples)
	}
}

func TestDecideDiagnosticRepairExecutionInitIsPostVerification(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "init", Fix: true, YesAll: true})

	if policy.Mode != "post-init-verify" || policy.MutationAllowed || policy.RequiresFix || policy.RequiresYes {
		t.Fatalf("policy = %+v, want read-only post-init verification", policy)
	}
	if slices.Contains(policy.Examples, "hazmat init") || !slices.Contains(policy.Examples, "hazmat doctor --fix --yes") || !slices.Contains(policy.Examples, "hazmat doctor --dry-run") {
		t.Fatalf("examples = %v, want repair path and explicit dry-run preview without init loop", policy.Examples)
	}
}

func TestDecideDiagnosticRepairExecutionDoctorPlanOnlyByDefault(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "doctor"})

	if policy.Mode != "plan-only" || policy.MutationAllowed || !policy.RequiresFix {
		t.Fatalf("policy = %+v, want plan-only doctor requiring --fix", policy)
	}
	if !slices.Contains(policy.Examples, "hazmat doctor --dry-run") || !slices.Contains(policy.Examples, "hazmat doctor --fix") {
		t.Fatalf("examples = %v, want explicit dry-run preview and fix path", policy.Examples)
	}
}

func TestDecideDiagnosticRepairExecutionDryRunOverridesFix(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{
		Command: "doctor",
		Fix:     true,
		YesAll:  true,
		DryRun:  true,
	})

	if policy.Mode != "dry-run" || policy.MutationAllowed || policy.RequiresFix || policy.RequiresYes {
		t.Fatalf("policy = %+v, want non-mutating dry-run override", policy)
	}
	if !slices.Contains(policy.Examples, "hazmat doctor --dry-run") || !slices.Contains(policy.Examples, "hazmat doctor --fix") {
		t.Fatalf("examples = %v, want dry-run preview and fix path", policy.Examples)
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

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return len(values)
}
