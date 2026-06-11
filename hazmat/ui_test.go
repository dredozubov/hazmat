package hazmat

import (
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
		"/Users/agent/.claude/projects/a is not group-writable — resume sync will fail (mode 0755); fix with: sudo chmod 2770 /Users/agent/.claude/projects/a",
		"/Users/agent/.claude/projects/a",
	)
	ui.recordTypedFinding(
		uiFindingWarning,
		diagnosticFinding(findingClaudeProjectPermissions),
		"/Users/agent/.claude/projects/b is not group-writable — resume sync will fail (mode 0700); fix with: sudo chmod 2770 /Users/agent/.claude/projects/b",
		"/Users/agent/.claude/projects/b",
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
	ui.recordFinding(uiFindingWarning, "umask 007 not found in agent's .zshrc — run hazmat init")

	if recommendations := ui.recommendations(); len(recommendations) != 0 {
		t.Fatalf("recommendations = %#v, want none for untyped message", recommendations)
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
