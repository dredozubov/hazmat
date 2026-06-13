package hazmat

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func TestUIRecommendationsGroupClaudeProjectPermissions(t *testing.T) {
	ui := &UI{}
	ui.stepLabel = "Agent user tools"
	ui.recordTypedFinding(
		uiFindingWarning,
		diagnosticFinding(findingClaudeProjectPermissions),
		"/Users/agent/.claude/projects/a is not group-writable; resume sync will fail (mode 0755)",
		"path: /Users/agent/.claude/projects/a",
	)
	ui.recordTypedFinding(
		uiFindingWarning,
		diagnosticFinding(findingClaudeProjectPermissions),
		"/Users/agent/.claude/projects/b is not group-writable; resume sync will fail (mode 0700)",
		"path: /Users/agent/.claude/projects/b",
	)

	recommendations := ui.recommendations()
	if len(recommendations) != 1 {
		t.Fatalf("recommendations = %d, want 1: %#v", len(recommendations), recommendations)
	}
	rec := recommendations[0]
	if rec.Key != diagnosticFinding(findingClaudeProjectPermissions).RecommendationKey() {
		t.Fatalf("recommendation key = %q, want %q", rec.Key, diagnosticFinding(findingClaudeProjectPermissions).RecommendationKey())
	}
	if strings.Contains(rec.Action, "sudo chmod") {
		t.Fatalf("recommendation action = %q, should come from typed repair action copy instead of shell recipe", rec.Action)
	}
	if len(rec.Details) != 2 {
		t.Fatalf("recommendation details = %v, want two affected paths", rec.Details)
	}
	for _, detail := range rec.Details {
		if strings.Contains(detail, "sudo chmod") || !strings.HasPrefix(detail, "path: ") {
			t.Fatalf("recommendation detail = %q, want structured path evidence without shell recipe", detail)
		}
	}
}

func TestUIRecommendationsUseTypedAction(t *testing.T) {
	ui := &UI{}
	ui.stepLabel = "Credential inventory"
	def := diagnosticFinding(findingCredentialCloudSecretKeyLegacy)
	ui.recordTypedFinding(
		uiFindingWarning,
		def,
		"Credential cloud.s3.secret-key: needs-repair backend=host-secret-store delivery=none host-store=absent",
	)

	recommendations := ui.recommendations()
	if len(recommendations) != 1 {
		t.Fatalf("recommendations = %d, want 1: %#v", len(recommendations), recommendations)
	}
	if recommendations[0].Action != def.Action {
		t.Fatalf("recommendation action = %q, want %q", recommendations[0].Action, def.Action)
	}
}

func TestUIRecommendationsIgnoreUntypedMessages(t *testing.T) {
	ui := &UI{}
	ui.stepLabel = "Hardening gaps"
	ui.recordFinding(uiFindingWarning, "umask 007 not found in agent's .zshrc")

	if recommendations := ui.recommendations(); len(recommendations) != 0 {
		t.Fatalf("recommendations = %#v, want none for untyped message", recommendations)
	}
}

func TestUIRecommendationFramingDistinguishesCheckAndDoctor(t *testing.T) {
	checkUI := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "check"}}
	if got := checkUI.recommendationSectionTitle(); got != "━━━ Repairability report ━━━" {
		t.Fatalf("check title = %q", got)
	}
	if got := checkUI.recommendationFooter(); !strings.Contains(got, "hazmat doctor --fix") || !strings.Contains(got, "Preview only: hazmat doctor --dry-run") {
		t.Fatalf("check footer = %q, want direct fix path and explicit dry-run preview pointer", got)
	}
	if got := checkUI.recommendationFooter(); !strings.Contains(got, "executable planned repairs") {
		t.Fatalf("check footer = %q, want fix path scoped to executable planned repairs", got)
	}

	doctorUI := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "doctor"}}
	if got := doctorUI.recommendationSectionTitle(); got != "━━━ Repair plan preview ━━━" {
		t.Fatalf("doctor title = %q", got)
	}
	if got := doctorUI.recommendationFooter(); !strings.Contains(got, "hazmat doctor --fix") {
		t.Fatalf("doctor footer = %q, want --fix pointer", got)
	}
	if got := doctorUI.recommendationFooter(); !strings.Contains(got, "executable repairs") {
		t.Fatalf("doctor footer = %q, want fix path scoped to executable repairs", got)
	}
	doctorFixUI := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "doctor", Fix: true}}
	if got := doctorFixUI.recommendationFooter(); !strings.Contains(got, "full live validation") || !strings.Contains(got, "hazmat check --full") {
		t.Fatalf("doctor fix footer = %q, want approval-gated full validation pointer", got)
	}

	initUI := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "init"}}
	if got := initUI.recommendationSectionTitle(); got != "━━━ Post-init repair verification ━━━" {
		t.Fatalf("init title = %q", got)
	}
	if got := initUI.recommendationFooter(); strings.Contains(got, "hazmat init") || !strings.Contains(got, "hazmat doctor --fix") || !strings.Contains(got, "hazmat doctor --dry-run") {
		t.Fatalf("init footer = %q, want doctor fix and dry-run pointers without init loop", got)
	}
	if got := initUI.recommendationFooter(); !strings.Contains(got, "executable planned repairs") {
		t.Fatalf("init footer = %q, want fix path scoped to executable planned repairs", got)
	}
}

func TestUIDiagnosticReportIncludesTypedMetadata(t *testing.T) {
	ui := &UI{Quick: true, JSON: true}
	ui.stepLabel = "Agent user tools"
	def := diagnosticFinding(findingClaudeProjectPermissions)
	ui.TestWarnFinding(def, "project dir is not group-writable", "/Users/agent/.claude/projects/a")

	report := ui.diagnosticReport()
	if report.FormatVersion != 1 || report.Kind != "hazmat.diagnostic_report" || !report.Quick {
		t.Fatalf("report header = %+v", report)
	}
	if report.Totals.Warn != 1 || report.Totals.Fail != 0 {
		t.Fatalf("report totals = %+v", report.Totals)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(report.Findings))
	}
	finding := report.Findings[0]
	if !finding.Typed || finding.ID != string(def.ID) || finding.RepairReceipt != string(def.RepairReceipt) {
		t.Fatalf("finding metadata = %+v, want typed %s with receipt %s", finding, def.ID, def.RepairReceipt)
	}
	if finding.Resource == nil || finding.Resource.Owner == "" || finding.Resource.DesiredState == "" {
		t.Fatalf("finding resource = %+v, want owner and desired state", finding.Resource)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal(report): %v", err)
	}
	if !strings.Contains(string(data), `"repair_receipt":"receipt.claude.project-permissions"`) {
		t.Fatalf("json report missing repair receipt: %s", data)
	}
	if !strings.Contains(string(data), `"summary":{"executable":1,"manual":0,"skipped":0`) {
		t.Fatalf("json report missing repair plan summary: %s", data)
	}
	if !strings.Contains(string(data), `"next_steps":[{"id":"apply-approved-repairs","command":"hazmat doctor --fix"`) {
		t.Fatalf("json report missing direct repair next step: %s", data)
	}
}

func TestUIDiagnosticReportRepairPlanBuckets(t *testing.T) {
	ui := &UI{JSON: true}
	ui.stepLabel = "Mixed findings"
	ui.TestWarnFinding(diagnosticFinding(findingAgentUmask), "umask missing")
	ui.TestFailFinding(diagnosticFinding(findingDockerSocketPermissions), "docker socket is too broad")
	ui.TestWarnFinding(diagnosticFinding(findingAgentSSHKey), "ssh key missing")
	ui.TestFail("legacy untyped failure")

	plan := ui.diagnosticReport().RepairPlan
	if plan.Mutating || plan.Mode != "preview" {
		t.Fatalf("plan header = %+v, want non-mutating preview", plan)
	}
	if len(plan.Items) != 1 || plan.Items[0].RepairAction != "repair.agent-shell.umask" {
		t.Fatalf("plan items = %+v, want umask repair item", plan.Items)
	}
	if !plan.Items[0].ExecutableByHazmat || !plan.Items[0].Privileged || plan.Items[0].Authority != string(diagnosticRepairAuthorityRoot) {
		t.Fatalf("plan item governance = %+v, want executable privileged root repair", plan.Items[0])
	}
	if len(plan.Items[0].Preconditions) == 0 || len(plan.Items[0].TestObligations) == 0 {
		t.Fatalf("plan item governance = %+v, want preconditions and test obligations", plan.Items[0])
	}
	if len(plan.ManualItems) != 1 || plan.ManualItems[0].Status != string(diagnosticRepairManualExternal) {
		t.Fatalf("manual items = %+v, want docker manual item", plan.ManualItems)
	}
	if len(plan.SkippedItems) != 2 {
		t.Fatalf("skipped items = %+v, want optional ssh plus untyped finding", plan.SkippedItems)
	}
	if len(plan.AppliedReceipts) != 0 || len(plan.FailedVerifications) != 0 {
		t.Fatalf("plan execution state = receipts %+v failures %+v, want empty preview", plan.AppliedReceipts, plan.FailedVerifications)
	}
	wantSummary := diagnosticRepairPlanSummary{
		Executable:          1,
		Manual:              1,
		Skipped:             2,
		Applied:             0,
		FailedVerifications: 0,
		RemainingExecutable: 1,
		Remaining:           4,
	}
	if plan.Summary != wantSummary {
		t.Fatalf("plan summary = %+v, want %+v", plan.Summary, wantSummary)
	}
	if len(plan.NextSteps) != 2 {
		t.Fatalf("next steps = %+v, want direct fix plus dry-run preview", plan.NextSteps)
	}
	if got := plan.NextSteps[0]; got.ID != "apply-approved-repairs" || got.Command != "hazmat doctor --fix" || !got.Mutating || !got.RequiresApproval {
		t.Fatalf("first next step = %+v, want approved mutating fix path", got)
	}
	if got := plan.NextSteps[1]; got.ID != "preview-repair-plan" || got.Command != "hazmat doctor --dry-run" || got.Mutating {
		t.Fatalf("second next step = %+v, want non-mutating dry-run preview", got)
	}
}

func TestCheckManualOnlyPlanDoesNotPointAtFix(t *testing.T) {
	ui := &UI{JSON: true, RepairExecution: diagnosticRepairExecutionRequest{Command: "check"}}
	ui.stepLabel = "Manual findings"
	ui.TestFailFinding(diagnosticFinding(findingDockerSocketPermissions), "docker socket is too broad")

	plan := ui.diagnosticReport().RepairPlan
	if plan.Summary.RemainingExecutable != 0 || len(plan.Items) != 0 || len(plan.ManualItems) != 1 {
		t.Fatalf("plan summary = %+v items=%+v manual=%+v, want one manual item and no executable repairs", plan.Summary, plan.Items, plan.ManualItems)
	}
	footer := ui.repairPlanFooter(plan)
	if strings.Contains(footer, "hazmat doctor --fix") {
		t.Fatalf("footer = %q, want no fix command for manual-only plan", footer)
	}
	if !strings.Contains(footer, "No executable Hazmat repairs") || !strings.Contains(footer, "manual") {
		t.Fatalf("footer = %q, want manual-only guidance", footer)
	}
	if len(plan.NextSteps) != 1 || plan.NextSteps[0].ID != "inspect-remaining-items" || plan.NextSteps[0].Command != "" || plan.NextSteps[0].Mutating {
		t.Fatalf("next steps = %+v, want non-mutating inspect step without command", plan.NextSteps)
	}
	if containsExampleWithPrefix(plan.Execution.Examples, "hazmat doctor --fix") {
		t.Fatalf("execution examples = %v, want no fix command for manual-only plan", plan.Execution.Examples)
	}
	if !containsPlanString(plan.Execution.Examples, "hazmat doctor --dry-run") {
		t.Fatalf("execution examples = %v, want non-mutating preview retained", plan.Execution.Examples)
	}
}

func TestDoctorDryRunManualOnlyPlanDoesNotPointAtFix(t *testing.T) {
	ui := &UI{
		JSON:   true,
		DryRun: true,
		RepairExecution: diagnosticRepairExecutionRequest{
			Command: "doctor",
			DryRun:  true,
		},
	}
	ui.stepLabel = "Manual findings"
	ui.TestFailFinding(diagnosticFinding(findingDockerSocketPermissions), "docker socket is too broad")

	plan := ui.diagnosticReport().RepairPlan
	if plan.Execution.Mode != "dry-run" || plan.Summary.RemainingExecutable != 0 || len(plan.ManualItems) != 1 {
		t.Fatalf("plan = mode %q summary %+v manual %+v, want manual-only dry-run", plan.Execution.Mode, plan.Summary, plan.ManualItems)
	}
	footer := ui.repairPlanFooter(plan)
	if strings.Contains(footer, "hazmat doctor --fix") {
		t.Fatalf("footer = %q, want no fix command for manual-only doctor preview", footer)
	}
	if len(plan.NextSteps) != 1 || plan.NextSteps[0].ID != "inspect-remaining-items" || plan.NextSteps[0].Command != "" || plan.NextSteps[0].Mutating {
		t.Fatalf("next steps = %+v, want non-mutating inspect step without command", plan.NextSteps)
	}
	if containsExampleWithPrefix(plan.Execution.Examples, "hazmat doctor --fix") {
		t.Fatalf("execution examples = %v, want no fix command for manual-only doctor preview", plan.Execution.Examples)
	}
}

func TestCleanDiagnosticPlanDoesNotSuggestRepairs(t *testing.T) {
	for _, req := range []diagnosticRepairExecutionRequest{
		{Command: "check"},
		{Command: "doctor"},
		{Command: "doctor", DryRun: true},
	} {
		t.Run(req.Command, func(t *testing.T) {
			ui := &UI{
				JSON:            true,
				DryRun:          req.DryRun,
				RepairExecution: req,
			}

			plan := ui.diagnosticReport().RepairPlan
			if plan.Summary.Remaining != 0 || plan.Summary.RemainingExecutable != 0 {
				t.Fatalf("plan summary = %+v, want clean plan", plan.Summary)
			}
			if len(plan.NextSteps) != 0 {
				t.Fatalf("next steps = %+v, want none for clean plan", plan.NextSteps)
			}
			if len(plan.Execution.Examples) != 0 {
				t.Fatalf("execution examples = %v, want no repair commands for clean plan", plan.Execution.Examples)
			}
		})
	}
}

func containsExampleWithPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func TestUIRepairPlanSummaryLineCountsActionableBuckets(t *testing.T) {
	got := diagnosticRepairPlanSummaryLine(diagnosticRepairPlanSummary{
		Executable:          1,
		Manual:              2,
		Skipped:             0,
		Applied:             1,
		FailedVerifications: 1,
		Remaining:           3,
	})
	want := "1 executable repair, 2 manual items, 0 skipped items, 1 applied repair, 1 failed verification, 3 remaining items"
	if got != want {
		t.Fatalf("summary line = %q, want %q", got, want)
	}
}

func TestUIPrintRepairPlanIncludesCompactSummary(t *testing.T) {
	ui := &UI{}
	ui.stepLabel = "Mixed findings"
	ui.TestWarnFinding(diagnosticFinding(findingAgentUmask), "umask missing")
	ui.TestFailFinding(diagnosticFinding(findingDockerSocketPermissions), "docker socket is too broad")

	plan := ui.diagnosticReport().RepairPlan
	out := captureUIOutput(t, func() {
		ui.printRepairPlan(plan)
	})
	if !strings.Contains(out, "Summary: 1 executable repair, 1 manual item, 0 skipped items, 2 remaining items") {
		t.Fatalf("repair plan output missing compact summary:\n%s", out)
	}
	if !strings.Contains(out, "1. [WARN] Restore the agent umask") || !strings.Contains(out, "2. [FAIL] Restrict the Docker socket") {
		t.Fatalf("repair plan output lost detailed items:\n%s", out)
	}
}

func captureUIOutput(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	oldColorOutput := color.Output
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	color.Output = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	os.Stdout = oldStdout
	color.Output = oldColorOutput
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(data)
}

func TestUIDiagnosticReportInitPostVerificationUsesTypedRepairPlan(t *testing.T) {
	ui := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "init"}}
	ui.stepLabel = "Verify setup"
	ui.TestFailFinding(diagnosticFinding(findingSetupSudoers), "Passwordless sudo not working")
	ui.TestWarnFinding(diagnosticFinding(findingDNSBlocklist), "DNS blocklist not installed")

	plan := ui.diagnosticReport().RepairPlan
	if plan.Execution.Mode != "post-init-verify" || plan.Execution.MutationAllowed {
		t.Fatalf("execution policy = %+v, want post-init verification", plan.Execution)
	}
	if len(plan.Items) != 2 {
		t.Fatalf("plan items = %+v, want sudoers and DNS repair blockers", plan.Items)
	}
	if plan.Items[0].RepairAction != "repair.setup.sudoers" || plan.Items[0].Status != "planned" {
		t.Fatalf("first item = %+v, want typed sudoers repair blocker", plan.Items[0])
	}
	if strings.Contains(ui.repairPlanFooter(plan), "hazmat init") {
		t.Fatalf("init footer = %q, want no init retry advice", ui.repairPlanFooter(plan))
	}
	if len(plan.NextSteps) != 2 || plan.NextSteps[0].Command != "hazmat doctor --fix" || plan.NextSteps[1].Command != "hazmat doctor --dry-run" {
		t.Fatalf("post-init next steps = %+v, want doctor repair path without init retry", plan.NextSteps)
	}
	for _, step := range plan.NextSteps {
		if strings.Contains(step.Command, "hazmat init") || strings.Contains(step.Reason, "hazmat init") {
			t.Fatalf("post-init next step = %+v, want no init retry advice", step)
		}
	}
}

func TestUIDiagnosticReportDoctorFixWithoutYesIsBlocked(t *testing.T) {
	backend := &recordingDiagnosticRepairBackend{}
	ui := &UI{
		JSON:          true,
		RepairBackend: backend,
		RepairExecution: diagnosticRepairExecutionRequest{
			Command: "doctor",
			Fix:     true,
		},
	}
	ui.stepLabel = "Hardening gaps"
	ui.TestWarnFinding(diagnosticFinding(findingAgentUmask), "umask missing")

	plan := ui.diagnosticReport().RepairPlan
	if plan.Execution.Mode != "blocked-noninteractive" || plan.Execution.MutationAllowed {
		t.Fatalf("execution policy = %+v, want blocked noninteractive", plan.Execution)
	}
	if plan.Mutating || plan.Mode != "preview" {
		t.Fatalf("plan header = mode %q mutating=%v, want preview", plan.Mode, plan.Mutating)
	}
	if backend.applyCalls != 0 || backend.verifyCalls != 0 {
		t.Fatalf("backend calls = apply %d verify %d, want none", backend.applyCalls, backend.verifyCalls)
	}
	if len(plan.NextSteps) != 1 || plan.NextSteps[0].Command != "hazmat doctor --fix --yes" || !plan.NextSteps[0].RequiresApproval {
		t.Fatalf("blocked next steps = %+v, want explicit --yes fix path", plan.NextSteps)
	}
}

func TestUIDiagnosticReportDryRunOverridesDoctorFix(t *testing.T) {
	backend := &recordingDiagnosticRepairBackend{}
	ui := &UI{
		JSON:          true,
		DryRun:        true,
		YesAll:        true,
		RepairBackend: backend,
		RepairExecution: diagnosticRepairExecutionRequest{
			Command: "doctor",
			Fix:     true,
			YesAll:  true,
		},
	}
	ui.stepLabel = "Hardening gaps"
	ui.TestWarnFinding(diagnosticFinding(findingAgentUmask), "umask missing")

	plan := ui.diagnosticReport().RepairPlan
	if plan.Execution.Mode != "dry-run" || plan.Execution.MutationAllowed || plan.Mutating {
		t.Fatalf("execution policy = %+v mutating=%v, want non-mutating dry-run", plan.Execution, plan.Mutating)
	}
	if plan.Mode != "preview" {
		t.Fatalf("plan mode = %q, want preview", plan.Mode)
	}
	if backend.applyCalls != 0 || backend.verifyCalls != 0 {
		t.Fatalf("backend calls = apply %d verify %d, want none", backend.applyCalls, backend.verifyCalls)
	}
	if !strings.Contains(ui.repairPlanFooter(plan), "hazmat doctor --fix") {
		t.Fatalf("dry-run footer = %q, want fix path", ui.repairPlanFooter(plan))
	}
	if !strings.Contains(ui.repairPlanFooter(plan), "executable repairs") {
		t.Fatalf("dry-run footer = %q, want executable repair scope", ui.repairPlanFooter(plan))
	}
	if len(plan.NextSteps) != 1 || plan.NextSteps[0].Command != "hazmat doctor --fix" || !plan.NextSteps[0].Mutating {
		t.Fatalf("dry-run next steps = %+v, want approved fix path", plan.NextSteps)
	}
}

func TestUIDiagnosticReportDoctorFixYesExecutesSharedPlan(t *testing.T) {
	backend := &recordingDiagnosticRepairBackend{
		applyEvidence:  []string{"applied managed umask block"},
		verifyEvidence: []string{"verified umask 007"},
	}
	ui := &UI{
		JSON:          true,
		YesAll:        true,
		RepairBackend: backend,
		RepairExecution: diagnosticRepairExecutionRequest{
			Command: "doctor",
			Fix:     true,
			YesAll:  true,
		},
	}
	ui.stepLabel = "Hardening gaps"
	ui.TestWarnFinding(diagnosticFinding(findingAgentUmask), "umask missing")

	plan := ui.diagnosticReport().RepairPlan
	if plan.Mode != "executed" || !plan.Mutating || plan.Execution.Mode != "fix-yes" {
		t.Fatalf("plan execution = mode %q mutating=%v policy=%+v, want executed fix-yes", plan.Mode, plan.Mutating, plan.Execution)
	}
	if backend.applyCalls != 1 || backend.verifyCalls != 1 {
		t.Fatalf("backend calls = apply %d verify %d, want 1/1", backend.applyCalls, backend.verifyCalls)
	}
	if len(plan.AppliedReceipts) != 1 || plan.AppliedReceipts[0].RollbackBoundary != "setup.agent-shell" {
		t.Fatalf("receipts = %+v, want verified rollback-aware receipt", plan.AppliedReceipts)
	}
	if len(plan.FailedVerifications) != 0 {
		t.Fatalf("failed verifications = %+v, want none", plan.FailedVerifications)
	}
	if plan.Summary.Applied != 1 || plan.Summary.Remaining != 0 || plan.Summary.RemainingExecutable != 0 {
		t.Fatalf("summary = %+v, want one applied repair and no remaining items", plan.Summary)
	}
	if len(plan.NextSteps) != 1 || plan.NextSteps[0].Command != "hazmat check --full" || plan.NextSteps[0].Mutating || !plan.NextSteps[0].RequiresApproval {
		t.Fatalf("next steps = %+v, want approval-gated non-mutating full verification", plan.NextSteps)
	}
	if !strings.Contains(plan.NextSteps[0].Reason, "helper-backed, backup, and cloud live validation") || !strings.Contains(plan.NextSteps[0].Reason, "ask before running") {
		t.Fatalf("next step reason = %q, want full live-validation approval disclosure", plan.NextSteps[0].Reason)
	}
	if footer := ui.repairPlanFooter(plan); !strings.Contains(footer, "full live validation") || !strings.Contains(footer, "hazmat check --full") {
		t.Fatalf("repair footer = %q, want approval-gated full validation pointer", footer)
	}
}

func TestDoctorFixFooterReportsUnresolvedManualFindings(t *testing.T) {
	backend := &recordingDiagnosticRepairBackend{
		applyEvidence:  []string{"applied managed umask block"},
		verifyEvidence: []string{"verified umask 007"},
	}
	ui := &UI{
		JSON:          true,
		YesAll:        true,
		RepairBackend: backend,
		RepairExecution: diagnosticRepairExecutionRequest{
			Command: "doctor",
			Fix:     true,
			YesAll:  true,
		},
	}
	ui.stepLabel = "Mixed findings"
	ui.TestWarnFinding(diagnosticFinding(findingAgentUmask), "umask missing")
	ui.TestFailFinding(diagnosticFinding(findingDockerSocketPermissions), "docker socket is too broad")
	ui.TestFailFinding(diagnosticFinding(findingCredentialAdapterRequired), "gemini keychain adapter required")
	ui.TestWarnFinding(diagnosticFinding(findingAgentSSHKey), "optional ssh key missing")

	plan := ui.diagnosticReport().RepairPlan
	if len(plan.AppliedReceipts) != 1 || len(plan.ManualItems) != 2 || len(plan.SkippedItems) != 1 {
		t.Fatalf("plan = receipts %+v manual %+v skipped %+v, want repaired item plus manual and skipped findings", plan.AppliedReceipts, plan.ManualItems, plan.SkippedItems)
	}
	footer := ui.repairPlanFooter(plan)
	for _, want := range []string{"Executable repairs verified", "manual", "optional", "unsupported", "informational"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("footer = %q, want %q in unresolved non-executable guidance", footer, want)
		}
	}
	if strings.Contains(footer, "hazmat init") {
		t.Fatalf("footer = %q, want no init retry guidance", footer)
	}
	if len(plan.NextSteps) != 1 || plan.NextSteps[0].ID != "inspect-remaining-items" || plan.NextSteps[0].Command != "" || plan.NextSteps[0].Mutating {
		t.Fatalf("next steps = %+v, want non-mutating remaining-item inspection", plan.NextSteps)
	}
	if !strings.Contains(plan.NextSteps[0].Reason, "unsupported") {
		t.Fatalf("next step reason = %q, want unsupported remaining findings named", plan.NextSteps[0].Reason)
	}
}

func TestUIChooseBlankInputReturnsDefaultChoice(t *testing.T) {
	restoreTTY := stubTerminal(t, true)
	defer restoreTTY()

	restoreStdin := stubStdinFile(t, "\n")
	defer restoreStdin()

	got, err := (&UI{}).Choose(
		"How should Hazmat use this selection?",
		[]UIChoice{
			{Key: "use-now", Label: "Use selected now"},
			{Key: "always", Label: "Always use for this project"},
			{Key: "not-now", Label: "Not now"},
		},
		"always",
	)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got != "always" {
		t.Fatalf("Choose(blank input) = %q, want always", got)
	}
}

func stubStdinFile(t *testing.T, content string) func() {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatalf("create temp stdin: %v", err)
	}
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		t.Fatalf("write temp stdin: %v", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		t.Fatalf("seek temp stdin: %v", err)
	}

	saved := os.Stdin
	os.Stdin = file
	return func() {
		os.Stdin = saved
		if err := file.Close(); err != nil {
			t.Fatalf("close temp stdin: %v", err)
		}
	}
}
