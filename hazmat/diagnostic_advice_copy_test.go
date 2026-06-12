package hazmat

import (
	"strings"
	"testing"
	"unicode"
)

func TestDiagnosticAdviceNamesExplicitDoctorCommands(t *testing.T) {
	cases := map[string]string{
		"post-init verification": postInitVerificationAdvice,
		"missing agent user":     missingAgentUserRepairAdvice(),
		"rollback residue": strings.Join(rollbackCredentialDetails(credentialInventoryEntry{
			ID: credentialProviderOpenAIAPIKey,
			AgentResidue: []credentialInventoryFinding{{
				Path:   "/Users/agent/.openai",
				Detail: "stale agent credential",
			}},
		}), "\n"),
	}

	for name, advice := range cases {
		t.Run(name, func(t *testing.T) {
			if hasPlainHazmatDoctorAdvice(advice) {
				t.Fatalf("advice = %q, want explicit hazmat doctor command with flags", advice)
			}
			if !strings.Contains(advice, "hazmat doctor --dry-run") || !strings.Contains(advice, "hazmat doctor --fix") {
				t.Fatalf("advice = %q, want preview and fix paths", advice)
			}
		})
	}
}

func TestDiagnosticModeGuidanceShowsFixBeforePreview(t *testing.T) {
	lines := diagnosticModeGuidanceLines()
	joined := strings.Join(lines, "\n")
	fixIndex := strings.Index(joined, "hazmat doctor --fix")
	previewIndex := strings.Index(joined, "hazmat doctor --dry-run")
	if fixIndex < 0 || previewIndex < 0 {
		t.Fatalf("mode guidance = %q, want fix and preview commands", joined)
	}
	if fixIndex > previewIndex {
		t.Fatalf("mode guidance = %q, want fix path before preview path", joined)
	}
}

func TestStatusIncompleteSetupAdvicePrioritizesDoctorFix(t *testing.T) {
	if strings.Contains(statusIncompleteSetupAdvice, "Next step: hazmat init") {
		t.Fatalf("status advice = %q, want no init retry loop", statusIncompleteSetupAdvice)
	}
	fixIndex := strings.Index(statusIncompleteSetupAdvice, "hazmat doctor --fix")
	previewIndex := strings.Index(statusIncompleteSetupAdvice, "hazmat doctor --dry-run")
	initIndex := strings.Index(statusIncompleteSetupAdvice, "hazmat init")
	if fixIndex < 0 || previewIndex < 0 || initIndex < 0 {
		t.Fatalf("status advice = %q, want fix, preview, and first-time init paths", statusIncompleteSetupAdvice)
	}
	if fixIndex > previewIndex || previewIndex > initIndex {
		t.Fatalf("status advice = %q, want fix path before preview before first-time init", statusIncompleteSetupAdvice)
	}
}

func hasPlainHazmatDoctorAdvice(text string) bool {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(".,;:()[]{}", r)
	})
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] != "hazmat" || fields[i+1] != "doctor" {
			continue
		}
		if i+2 >= len(fields) || !strings.HasPrefix(fields[i+2], "--") {
			return true
		}
	}
	return false
}
