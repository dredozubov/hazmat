package hazmat

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestInitThenCheckDoesNotRecommendInitForSameFinding(t *testing.T) {
	initUI := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "init"}}
	initUI.stepLabel = "Verify setup"
	initUI.TestFailFinding(diagnosticFinding(findingSetupSudoers), "Passwordless sudo not working")
	initReport := initUI.diagnosticReport()
	if diagnosticReportAdviceMentions(initReport, "hazmat init") {
		t.Fatalf("init report loops back to init: %s", diagnosticReportJSON(t, initReport))
	}

	checkUI := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "check"}}
	checkUI.stepLabel = "Verify setup"
	checkUI.TestFailFinding(diagnosticFinding(findingSetupSudoers), "Passwordless sudo still not working")
	checkReport := checkUI.diagnosticReport()
	if diagnosticReportAdviceMentions(checkReport, "hazmat init") {
		t.Fatalf("check report after init loops back to init: %s", diagnosticReportJSON(t, checkReport))
	}
	assertReportHasFinding(t, checkReport, findingSetupSudoers)
}

func TestDoctorFixThenCheckDoesNotRecommendInitForSameFinding(t *testing.T) {
	doctorUI := &UI{
		YesAll: true,
		RepairExecution: diagnosticRepairExecutionRequest{
			Command: "doctor",
			Fix:     true,
			YesAll:  true,
		},
		RepairBackend: &recordingDiagnosticRepairBackend{
			applyEvidence:  []string{"applied managed umask block"},
			verifyEvidence: []string{"observed umask 022"},
			verifyErr:      errors.New("umask still wrong"),
		},
	}
	doctorUI.stepLabel = "Hardening gaps"
	doctorUI.TestWarnFinding(diagnosticFinding(findingAgentUmask), "umask missing")
	doctorReport := doctorUI.diagnosticReport()
	if len(doctorReport.RepairPlan.FailedVerifications) != 1 {
		t.Fatalf("doctor failed verifications = %+v, want one failed verification", doctorReport.RepairPlan.FailedVerifications)
	}
	if diagnosticReportAdviceMentions(doctorReport, "hazmat init") {
		t.Fatalf("doctor report loops back to init: %s", diagnosticReportJSON(t, doctorReport))
	}
	if diagnosticReportAdviceMentions(doctorReport, "hazmat doctor --fix") {
		t.Fatalf("doctor report offers mutating retry after failed verification: %s", diagnosticReportJSON(t, doctorReport))
	}

	checkUI := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "check"}}
	checkUI.stepLabel = "Hardening gaps"
	checkUI.TestWarnFinding(diagnosticFinding(findingAgentUmask), "umask still missing")
	checkReport := checkUI.diagnosticReport()
	if diagnosticReportAdviceMentions(checkReport, "hazmat init") {
		t.Fatalf("check report after doctor fix loops back to init: %s", diagnosticReportJSON(t, checkReport))
	}
	assertReportHasFinding(t, checkReport, findingAgentUmask)
}

func TestSessionHomeAdapterBlockersHaveInspectOnlyGuidance(t *testing.T) {
	for _, req := range []diagnosticRepairExecutionRequest{
		{Command: "check"},
		{Command: "doctor", DryRun: true},
	} {
		t.Run(req.Command, func(t *testing.T) {
			ui := &UI{
				DryRun:          req.DryRun,
				RepairExecution: req,
			}
			ui.stepLabel = "Session HOME"
			ui.recordTypedFinding(
				uiFindingFailure,
				sessionHomeAdapterRequiredDiagnosticFinding(),
				"session-local HOME activation is blocked by unsupported harness state",
				"Blocking paths: .codex [harness-state/adapter-required; adapter=codex-state:unsupported]",
			)

			report := ui.diagnosticReport()
			plan := report.RepairPlan
			if plan.Summary.RemainingExecutable != 0 || len(plan.Items) != 0 || len(plan.ManualItems) != 1 {
				t.Fatalf("plan summary = %+v items=%+v manual=%+v, want one unsupported manual item and no executable repairs", plan.Summary, plan.Items, plan.ManualItems)
			}
			if got := plan.ManualItems[0]; got.Status != string(diagnosticRepairUnsupported) || got.RepairAction != "" || got.ExecutableByHazmat {
				t.Fatalf("manual item = %+v, want unsupported non-executable session-home blocker", got)
			}
			if diagnosticReportAdviceMentions(report, "hazmat init") {
				t.Fatalf("session-home adapter blocker loops back to init: %s", diagnosticReportJSON(t, report))
			}
			if diagnosticReportAdviceMentions(report, "hazmat doctor --fix") {
				t.Fatalf("session-home adapter blocker offers mutating fix: %s", diagnosticReportJSON(t, report))
			}
			if len(plan.NextSteps) != 1 || plan.NextSteps[0].ID != "inspect-remaining-items" || plan.NextSteps[0].Command != "" || plan.NextSteps[0].Mutating {
				t.Fatalf("next steps = %+v, want inspect-only guidance", plan.NextSteps)
			}
		})
	}
}

func sessionHomeAdapterRequiredDiagnosticFinding() diagnosticFindingDefinition {
	return mustDiagnosticFinding(diagnosticFindingDefinition{
		ID:             "session-home.adapter-required",
		Resource:       "session-home.activation-blockers",
		Title:          "Resolve session-home adapter blockers",
		Repairability:  diagnosticRepairUnsupported,
		Action:         "Inspect the blocking session-home paths and add typed harness or asset adapters before enabling activation; Hazmat will not copy broad agent-home state.",
		SecurityImpact: "Unsupported session-home adapters can expose durable harness state or credentials if copied generically, so activation remains fail-closed.",
	})
}

func diagnosticReportAdviceMentions(report uiDiagnosticReport, needle string) bool {
	for _, rec := range report.Recommendations {
		if strings.Contains(rec.Title, needle) || strings.Contains(rec.Action, needle) || strings.Contains(strings.Join(rec.Details, "\n"), needle) {
			return true
		}
	}
	for _, item := range append(append(report.RepairPlan.Items, report.RepairPlan.ManualItems...), report.RepairPlan.SkippedItems...) {
		if strings.Contains(item.Title, needle) ||
			strings.Contains(item.Action, needle) ||
			strings.Contains(item.BlockedReason, needle) ||
			strings.Contains(item.Reason, needle) ||
			strings.Contains(strings.Join(item.Details, "\n"), needle) {
			return true
		}
	}
	for _, step := range report.RepairPlan.NextSteps {
		if strings.Contains(step.ID, needle) ||
			strings.Contains(step.Command, needle) ||
			strings.Contains(step.Reason, needle) {
			return true
		}
	}
	for _, example := range report.RepairPlan.Execution.Examples {
		if strings.Contains(example, needle) {
			return true
		}
	}
	return false
}

func diagnosticReportJSON(t *testing.T, report uiDiagnosticReport) string {
	t.Helper()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(report): %v", err)
	}
	return string(data)
}

func assertReportHasFinding(t *testing.T, report uiDiagnosticReport, id diagnosticFindingID) {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.ID == string(id) {
			return
		}
	}
	t.Fatalf("report findings = %+v, want %s", report.Findings, id)
}
