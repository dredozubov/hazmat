package hazmat

import (
	"slices"
	"strings"
	"testing"
)

func TestDecideDiagnosticRepairExecutionCheckIsReadOnly(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "check"})

	if policy.Mode != "read-only" || policy.MutationAllowed || policy.RequiresFix {
		t.Fatalf("policy = %+v, want read-only check", policy)
	}
	wantExamples := []string{"hazmat doctor --fix", "hazmat doctor --dry-run"}
	if !slices.Equal(policy.Examples, wantExamples) {
		t.Fatalf("examples = %v, want %v", policy.Examples, wantExamples)
	}
}

func TestDecideDiagnosticRepairExecutionStatusIsReadOnly(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "status"})

	if policy.Mode != "read-only" || policy.MutationAllowed || policy.RequiresFix || policy.RequiresYes {
		t.Fatalf("policy = %+v, want read-only status", policy)
	}
	if policy.Command != "status" {
		t.Fatalf("command = %q, want status", policy.Command)
	}
	if !strings.Contains(policy.Reason, "hazmat status --full") {
		t.Fatalf("reason = %q, want status-specific read-only reason", policy.Reason)
	}
	wantExamples := []string{"hazmat doctor --fix", "hazmat doctor --dry-run"}
	if !slices.Equal(policy.Examples, wantExamples) {
		t.Fatalf("examples = %v, want %v", policy.Examples, wantExamples)
	}
}

func TestDecideDiagnosticRepairExecutionInitIsPostVerification(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "init", Fix: true, YesAll: true})

	if policy.Mode != "post-init-verify" || policy.MutationAllowed || policy.RequiresFix || policy.RequiresYes {
		t.Fatalf("policy = %+v, want read-only post-init verification", policy)
	}
	wantExamples := []string{"hazmat doctor --fix", "hazmat doctor --dry-run"}
	if !slices.Equal(policy.Examples, wantExamples) {
		t.Fatalf("examples = %v, want %v without init loop", policy.Examples, wantExamples)
	}
}

func TestDecideDiagnosticRepairExecutionDoctorPlanOnlyByDefault(t *testing.T) {
	policy := decideDiagnosticRepairExecution(diagnosticRepairExecutionRequest{Command: "doctor"})

	if policy.Mode != "plan-only" || policy.MutationAllowed || !policy.RequiresFix {
		t.Fatalf("policy = %+v, want plan-only doctor requiring --fix", policy)
	}
	wantExamples := []string{"hazmat doctor --fix", "hazmat doctor --dry-run", "hazmat doctor --dry-run --json"}
	if !slices.Equal(policy.Examples, wantExamples) {
		t.Fatalf("examples = %v, want %v", policy.Examples, wantExamples)
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
	wantExamples := []string{"hazmat doctor --fix", "hazmat doctor --dry-run"}
	if !slices.Equal(policy.Examples, wantExamples) {
		t.Fatalf("examples = %v, want %v", policy.Examples, wantExamples)
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
