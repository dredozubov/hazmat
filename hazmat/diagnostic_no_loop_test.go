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

	checkUI := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "check"}}
	checkUI.stepLabel = "Hardening gaps"
	checkUI.TestWarnFinding(diagnosticFinding(findingAgentUmask), "umask still missing")
	checkReport := checkUI.diagnosticReport()
	if diagnosticReportAdviceMentions(checkReport, "hazmat init") {
		t.Fatalf("check report after doctor fix loops back to init: %s", diagnosticReportJSON(t, checkReport))
	}
	assertReportHasFinding(t, checkReport, findingAgentUmask)
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
