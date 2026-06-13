package hazmat

import (
	"os"
	"strings"
	"testing"
)

func TestTestingDocsMatchQuickHelperProbeBoundary(t *testing.T) {
	data, err := os.ReadFile("../docs/testing.md")
	if err != nil {
		t.Fatalf("read testing docs: %v", err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	required := []string{
		"no helper-backed agent probes in the default quick mode",
		"skip local snapshot, cloud backup, and cloud restore live validation",
		"does not run backup smoke tests or send external traffic",
		"hazmat check --full",
		"hazmat status --full",
		"helper-backed, backup, and cloud live validation",
		"same full validation afterward",
		"require explicit exact-command approval",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("docs/testing.md missing %q", phrase)
		}
	}
	if strings.Contains(text, "no helper-backed agent probes until setup readiness") {
		t.Fatal("docs/testing.md still describes the old setup-gated helper probe boundary")
	}
}

func TestUsageDocsExplainNonMutatingApprovalGatedNextSteps(t *testing.T) {
	data, err := os.ReadFile("../docs/usage.md")
	if err != nil {
		t.Fatalf("read usage docs: %v", err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	required := []string{
		"`requires_approval` is not limited to mutating repairs",
		"hazmat check --full",
		"non-mutating and still require exact-command approval",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("docs/usage.md missing %q", phrase)
		}
	}
}

func TestUsageDocsDistinguishStatusFromCheck(t *testing.T) {
	data, err := os.ReadFile("../docs/usage.md")
	if err != nil {
		t.Fatalf("read usage docs: %v", err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	required := []string{
		"hazmat status # setup progress checklist",
		"hazmat check # read-only health and repairability report",
		"hazmat status --full # setup progress plus full live validation",
		"`hazmat status` is the lightweight setup progress checklist",
		"`hazmat status --full` keeps that progress checklist and then runs the same approval-gated full validation as `hazmat check --full`",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("docs/usage.md missing %q", phrase)
		}
	}
	if strings.Contains(text, "hazmat status # same thing") {
		t.Fatal("docs/usage.md still equates status with another command")
	}
}

func TestUsageDocsUseDirectPostInitRepairPath(t *testing.T) {
	data, err := os.ReadFile("../docs/usage.md")
	if err != nil {
		t.Fatalf("read usage docs: %v", err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	required := []string{
		"After init, use `hazmat doctor --fix` to apply executable post-init repairs",
		"`hazmat doctor --dry-run` to preview the typed plan",
		"`hazmat check` when you only want a read-only health report",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("docs/usage.md missing %q", phrase)
		}
	}
	if strings.Contains(text, "After init, use `hazmat check` or `hazmat doctor --dry-run` to inspect remaining drift") {
		t.Fatal("docs/usage.md still routes post-init drift through check/dry-run only")
	}
}

func TestManualTestingCredentialFlowExercisesDoctorRepair(t *testing.T) {
	data, err := os.ReadFile("../docs/manual-testing.md")
	if err != nil {
		t.Fatalf("read manual testing docs: %v", err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	required := []string{
		"Credential inventory and legacy residue",
		"run `hazmat doctor --dry-run`",
		"typed executable repairs",
		"run `hazmat doctor --fix` and approve the credential repair plan",
		"Use `hazmat migrate credentials --dry-run` only when validating the scoped lower-level migration command directly",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("docs/manual-testing.md missing %q", phrase)
		}
	}
	if strings.Contains(text, "Steps: run `hazmat migrate credentials --dry-run`; run `hazmat migrate credentials`; run `hazmat check`") {
		t.Fatal("docs/manual-testing.md still makes migration command the primary credential repair path")
	}
}

func TestManualTestingHarnessFlowsUseLifecycleUpdateCommands(t *testing.T) {
	data, err := os.ReadFile("../docs/manual-testing.md")
	if err != nil {
		t.Fatalf("read manual testing docs: %v", err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	required := []string{
		"hazmat harness update claude",
		"hazmat harness update codex",
		"hazmat harness update opencode",
		"hazmat harness update gemini",
		"hazmat harness update hermes",
		"hazmat harness update qwen",
		"hazmat harness update cursor-agent",
		"list status shows all seven harnesses",
		"bootstrap compatibility aliases",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("docs/manual-testing.md missing %q", phrase)
		}
	}
	for _, stale := range []string{
		"Steps: `hazmat bootstrap claude`",
		"Steps: `hazmat bootstrap codex`",
		"Steps: `hazmat bootstrap opencode`",
		"Steps: `hazmat bootstrap gemini`",
		"run `hazmat bootstrap hermes`",
		"Steps: `hazmat bootstrap qwen`",
		"Steps: `hazmat bootstrap cursor-agent`",
		"list status shows all six harnesses",
	} {
		if strings.Contains(text, stale) {
			t.Fatalf("docs/manual-testing.md still uses bootstrap-first checklist phrase %q", stale)
		}
	}
}
