package hazmat

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
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

	doctorUI := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "doctor"}}
	if got := doctorUI.recommendationSectionTitle(); got != "━━━ Repair plan preview ━━━" {
		t.Fatalf("doctor title = %q", got)
	}
	if got := doctorUI.recommendationFooter(); !strings.Contains(got, "hazmat doctor --fix") {
		t.Fatalf("doctor footer = %q, want --fix pointer", got)
	}

	initUI := &UI{RepairExecution: diagnosticRepairExecutionRequest{Command: "init"}}
	if got := initUI.recommendationSectionTitle(); got != "━━━ Post-init repair verification ━━━" {
		t.Fatalf("init title = %q", got)
	}
	if got := initUI.recommendationFooter(); strings.Contains(got, "hazmat init") || !strings.Contains(got, "hazmat doctor --fix") || !strings.Contains(got, "hazmat doctor --dry-run") {
		t.Fatalf("init footer = %q, want doctor fix and dry-run pointers without init loop", got)
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
