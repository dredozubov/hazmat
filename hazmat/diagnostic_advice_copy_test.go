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
