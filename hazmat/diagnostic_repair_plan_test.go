package hazmat

import (
	"strings"
	"testing"
)

func TestPlanDiagnosticRepairsBuildsConsentRepairItem(t *testing.T) {
	ui := &UI{}
	ui.stepLabel = "Hardening gaps"
	ui.recordTypedFinding(uiFindingWarning, diagnosticFinding(findingAgentUmask), "umask missing")

	plan := planDiagnosticRepairs(ui.findings, ui.recommendations(), diagnosticRepairExecutionRequest{Command: "doctor"})
	if plan.Mutating {
		t.Fatal("plan mutating = true, want non-mutating preview")
	}
	if plan.Execution.Mode != "plan-only" {
		t.Fatalf("execution policy = %+v, want plan-only doctor", plan.Execution)
	}
	if len(plan.TrustBoundaries) == 0 {
		t.Fatal("plan trust boundaries missing")
	}
	if len(plan.Items) != 1 {
		t.Fatalf("items = %+v, want one repair item", plan.Items)
	}
	item := plan.Items[0]
	if item.RepairAction != "repair.agent-shell.umask" || item.Verification != "verify.agent-shell.umask" {
		t.Fatalf("item repair contract = %+v, want umask action and verification", item)
	}
	if item.ConsentPrompt == "" || !strings.Contains(item.ConsentPrompt, "Restore the agent umask") {
		t.Fatalf("consent prompt = %q", item.ConsentPrompt)
	}
	if item.Authority != string(diagnosticRepairAuthorityRoot) || !item.Privileged {
		t.Fatalf("authority = %q privileged=%v, want privileged root", item.Authority, item.Privileged)
	}
	if item.SafetyRationale == "" {
		t.Fatal("missing safety rationale")
	}
	if len(item.ProofLanes) == 0 || item.ProofNotes == "" {
		t.Fatalf("proof metadata = lanes %v notes %q, want declared proof lane", item.ProofLanes, item.ProofNotes)
	}
	if !containsPlanString(item.ProofLanes, string(diagnosticRepairProofTLASetupRollback)) {
		t.Fatalf("proof lanes = %v, want setup/rollback TLA lane", item.ProofLanes)
	}
	if item.SourceTrust != "hazmat-typed-registry" || !containsPlanString(item.Guardrails, "generated-repair-plan") {
		t.Fatalf("item trust = %q guardrails=%v", item.SourceTrust, item.Guardrails)
	}
}

func TestPlanDiagnosticRepairsExplainsBlockedAndSkippedItems(t *testing.T) {
	ui := &UI{}
	ui.stepLabel = "Mixed findings"
	ui.recordTypedFinding(uiFindingFailure, diagnosticFinding(findingDockerSocketPermissions), "docker socket is too broad")
	ui.recordTypedFinding(uiFindingWarning, diagnosticFinding(findingAgentSSHKey), "ssh key missing")
	ui.recordFinding(uiFindingWarning, "legacy untyped warning")

	plan := planDiagnosticRepairs(ui.findings, ui.recommendations(), diagnosticRepairExecutionRequest{Command: "doctor"})
	if len(plan.ManualItems) != 1 {
		t.Fatalf("manual items = %+v, want docker item", plan.ManualItems)
	}
	if plan.ManualItems[0].BlockedReason == "" || plan.ManualItems[0].Authority != string(diagnosticRepairAuthorityExternal) {
		t.Fatalf("manual item = %+v, want external blocked reason", plan.ManualItems[0])
	}
	if len(plan.SkippedItems) != 2 {
		t.Fatalf("skipped items = %+v, want optional plus untyped", plan.SkippedItems)
	}
	var sawUntyped bool
	for _, item := range plan.SkippedItems {
		if item.Status == "untyped" {
			sawUntyped = true
			if item.BlockedReason == "" || item.ExecutableByHazmat || item.RepairAction != "" {
				t.Fatalf("untyped skipped item = %+v, want blocked non-executable", item)
			}
			if item.SourceTrust != "observed-untyped-text" {
				t.Fatalf("untyped source trust = %q", item.SourceTrust)
			}
		}
	}
	if !sawUntyped {
		t.Fatalf("skipped items = %+v, want untyped skipped item", plan.SkippedItems)
	}
	wantSummary := diagnosticRepairPlanSummary{
		Executable:          0,
		Manual:              1,
		Skipped:             2,
		Applied:             0,
		FailedVerifications: 0,
		RemainingExecutable: 0,
		Remaining:           3,
	}
	if plan.Summary != wantSummary {
		t.Fatalf("summary = %+v, want %+v", plan.Summary, wantSummary)
	}
}

func TestPlanDiagnosticRepairsMarksRepoControlledMetadataAsEvidenceOnly(t *testing.T) {
	ui := &UI{}
	ui.stepLabel = "Project toolchain"
	ui.recordTypedFinding(
		uiFindingWarning,
		diagnosticFinding(findingIntegrationToolchain),
		"beads: no toolchain path resolved",
	)

	plan := planDiagnosticRepairs(ui.findings, ui.recommendations(), diagnosticRepairExecutionRequest{Command: "doctor"})
	if !hasTrustBoundary(plan, "repo-integration-metadata") || !hasTrustBoundary(plan, "generated-repair-plan") {
		t.Fatalf("trust boundaries = %+v", plan.TrustBoundaries)
	}
	if len(plan.SkippedItems) != 1 {
		t.Fatalf("skipped items = %+v, want informational integration item", plan.SkippedItems)
	}
	item := plan.SkippedItems[0]
	if item.SourceTrust != "repo-observed-metadata" {
		t.Fatalf("source trust = %q, want repo-observed-metadata", item.SourceTrust)
	}
	if !containsPlanString(item.Guardrails, "repo-integration-metadata") || item.ExecutableByHazmat || item.RepairAction != "" {
		t.Fatalf("integration item = %+v, want guarded non-executable evidence", item)
	}
}

func hasTrustBoundary(plan diagnosticRepairPlan, id string) bool {
	for _, boundary := range plan.TrustBoundaries {
		if boundary.ID == id {
			return true
		}
	}
	return false
}

func containsPlanString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
