package hazmat

import (
	"strings"
	"testing"
)

func TestPlanDiagnosticRepairsBuildsConsentRepairItem(t *testing.T) {
	ui := &UI{}
	ui.stepLabel = "Hardening gaps"
	ui.recordTypedFinding(uiFindingWarning, diagnosticFinding(findingAgentUmask), "umask missing")

	plan := planDiagnosticRepairs(ui.findings, ui.recommendations())
	if plan.Mutating {
		t.Fatal("plan mutating = true, want non-mutating preview")
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
}

func TestPlanDiagnosticRepairsExplainsBlockedAndSkippedItems(t *testing.T) {
	ui := &UI{}
	ui.stepLabel = "Mixed findings"
	ui.recordTypedFinding(uiFindingFailure, diagnosticFinding(findingDockerSocketPermissions), "docker socket is too broad")
	ui.recordTypedFinding(uiFindingWarning, diagnosticFinding(findingAgentSSHKey), "ssh key missing")
	ui.recordFinding(uiFindingWarning, "legacy untyped warning")

	plan := planDiagnosticRepairs(ui.findings, ui.recommendations())
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
			if item.BlockedReason == "" || item.ExecutableByHazmat {
				t.Fatalf("untyped skipped item = %+v, want blocked non-executable", item)
			}
		}
	}
	if !sawUntyped {
		t.Fatalf("skipped items = %+v, want untyped skipped item", plan.SkippedItems)
	}
}
