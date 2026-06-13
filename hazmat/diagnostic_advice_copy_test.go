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
			if !strings.Contains(advice, "executable repairs") && !strings.Contains(advice, "supported repairs") {
				t.Fatalf("advice = %q, want doctor --fix path scoped to executable or supported repairs", advice)
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
	if !strings.Contains(joined, "executable typed repairs") {
		t.Fatalf("mode guidance = %q, want fix path scoped to executable typed repairs", joined)
	}
}

func TestStatusIncompleteSetupAdviceUsesDoctorRepairPath(t *testing.T) {
	if strings.Contains(statusIncompleteSetupAdvice, "hazmat init") {
		t.Fatalf("status advice = %q, want no init retry loop in repair advice", statusIncompleteSetupAdvice)
	}
	fixIndex := strings.Index(statusIncompleteSetupAdvice, "hazmat doctor --fix")
	previewIndex := strings.Index(statusIncompleteSetupAdvice, "hazmat doctor --dry-run")
	if fixIndex < 0 || previewIndex < 0 {
		t.Fatalf("status advice = %q, want fix and preview paths", statusIncompleteSetupAdvice)
	}
	if fixIndex > previewIndex {
		t.Fatalf("status advice = %q, want fix path before preview", statusIncompleteSetupAdvice)
	}
}

func TestContainmentStatusActionDistinguishesFreshSetupFromDrift(t *testing.T) {
	if got := containmentStatusAction(false, false, false); got != "hazmat init" {
		t.Fatalf("fresh status action = %q, want hazmat init", got)
	}
	if got := containmentStatusAction(true, true, true); got != "hazmat init" {
		t.Fatalf("complete status action = %q, want hazmat init as completed setup row", got)
	}
	for name, flags := range map[string][3]bool{
		"agent user only": {true, false, false},
		"sudoers only":    {false, true, false},
		"pf only":         {false, false, true},
		"agent sudoers":   {true, true, false},
		"agent pf":        {true, false, true},
		"sudoers pf":      {false, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := containmentStatusAction(flags[0], flags[1], flags[2]); got != "hazmat doctor --fix" {
				t.Fatalf("partial status action = %q, want hazmat doctor --fix", got)
			}
		})
	}
}

func TestStatusFullHelpNamesLiveNetworkProbes(t *testing.T) {
	cmd := newStatusCmd()
	flag := cmd.Flags().Lookup("full")
	if flag == nil {
		t.Fatal("status --full flag missing")
	}
	joined := strings.Join([]string{cmd.Long, flag.Usage}, "\n")
	if !strings.Contains(joined, "hazmat check --full") || !strings.Contains(joined, "live network probes") {
		t.Fatalf("status --full help = %q, want check --full and live network probe wording", joined)
	}
	if strings.Contains(joined, "check --quick") || strings.Contains(joined, "same as 'hazmat check --quick'") {
		t.Fatalf("status --full help = %q, want no quick-mode equivalence", joined)
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
