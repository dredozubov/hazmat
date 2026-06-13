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
		"no sudo-adjacent launch-helper validation",
		"no helper-backed agent probes in the default quick mode",
		"skip local snapshot, cloud backup, and cloud restore live validation",
		"does not run backup smoke tests or send external traffic",
		"hazmat check --full",
		"hazmat status --full",
		"sudo-adjacent launch-helper validation plus helper-backed, backup, and cloud live validation",
		"same full validation afterward",
		"require explicit exact-command approval",
		"For setup prerequisites, the message distinguishes a fresh host",
		"from drift in an already-initialized host (`hazmat doctor --fix`, with `hazmat doctor --dry-run` as preview)",
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

func TestTestingDocsMarkdownCodeFencesAreBalanced(t *testing.T) {
	data, err := os.ReadFile("../docs/testing.md")
	if err != nil {
		t.Fatalf("read testing docs: %v", err)
	}
	if got := strings.Count(string(data), "```"); got%2 != 0 {
		t.Fatalf("docs/testing.md has %d Markdown code fences, want an even count", got)
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
		"hazmat check --full # sudo-adjacent launch-helper, helper-backed, backup, and cloud live validation",
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

func TestHarnessDocsUseLifecycleUpdateCommands(t *testing.T) {
	data, err := os.ReadFile("../docs/harnesses.md")
	if err != nil {
		t.Fatalf("read harness docs: %v", err)
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
		"compatible aliases",
		"After install/update + auth",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("docs/harnesses.md missing %q", phrase)
		}
	}
	for _, stale := range []string{
		"| **Claude Code** | 2.1.118 | `hazmat bootstrap claude`",
		"| **Codex** | 0.118.0 | `hazmat bootstrap codex`",
		"| **OpenCode** | 1.14.20 | `hazmat bootstrap opencode`",
		"| **Gemini** | 0.38.2 | `hazmat bootstrap gemini`",
		"| **Hermes (experimental)** | manual install | `hazmat bootstrap hermes`",
		"| **Qwen Code** | npm latest | `hazmat bootstrap qwen`",
		"| **Cursor Agent** | manual install | `hazmat bootstrap cursor-agent`",
		"**Install / update:** `hazmat bootstrap claude`",
		"**Install / update:** `hazmat bootstrap codex`",
		"**Install / update:** `hazmat bootstrap opencode`",
		"**Install / update:** `hazmat bootstrap gemini`",
		"**Install / update:** `hazmat bootstrap hermes`",
		"**Install / update:** `hazmat bootstrap qwen`",
		"**Install / update:** `hazmat bootstrap cursor-agent`",
		"After bootstrap/update + auth",
	} {
		if strings.Contains(text, stale) {
			t.Fatalf("docs/harnesses.md still uses bootstrap-first phrase %q", stale)
		}
	}
}

func TestUsageDocsUseHarnessLifecycleQuickstarts(t *testing.T) {
	data, err := os.ReadFile("../docs/usage.md")
	if err != nil {
		t.Fatalf("read usage docs: %v", err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	required := []string{
		"hazmat harness update opencode",
		"hazmat harness update codex",
		"hazmat harness update gemini",
		"hazmat harness update hermes",
		"hazmat harness update qwen",
		"hazmat harness update cursor-agent",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("docs/usage.md missing %q", phrase)
		}
	}
	for _, stale := range []string{
		"## Running OpenCode ```bash hazmat bootstrap opencode",
		"## Running Codex ```bash hazmat bootstrap codex",
		"## Running Gemini ```bash hazmat bootstrap gemini",
		"## Running Hermes ```bash hazmat bootstrap hermes",
		"## Running Qwen Code ```bash hazmat bootstrap qwen",
		"## Running Cursor Agent ```bash hazmat bootstrap cursor-agent",
		"`hazmat bootstrap hermes` verifies",
		"`hazmat bootstrap cursor-agent` verifies",
	} {
		if strings.Contains(text, stale) {
			t.Fatalf("docs/usage.md still uses bootstrap-first quickstart phrase %q", stale)
		}
	}
}

func TestUsageDocsDescribeClaudeExportWorkflowSidecarPolicy(t *testing.T) {
	data, err := os.ReadFile("../docs/usage.md")
	if err != nil {
		t.Fatalf("read usage docs: %v", err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	required := []string{
		"`hazmat export claude session`",
		"Copies the transcript and session sidecar directory from the agent user's `~/.claude/projects/...`",
		"Rewrites portable JSON/JSONL metadata so references to the agent project/session directory point at the installed host-side copy",
		"Omits opaque Workflow/subagent sidecar files that still contain agent-only paths after export",
		"Workflow/subagent caches are best-effort",
		"host-side resume should not try to read inaccessible agent-home Workflow artifacts",
		"Claude may rerun volatile Workflow steps whose cache files were not portable",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("docs/usage.md missing %q", phrase)
		}
	}
	for _, stale := range []string{
		"Workflow/subagent caches are preserved across host resume",
		"host Claude can reuse all contained Workflow cache files",
		"opaque Workflow caches remain usable after export",
	} {
		if strings.Contains(text, stale) {
			t.Fatalf("docs/usage.md overstates Claude export Workflow cache support with %q", stale)
		}
	}
}
