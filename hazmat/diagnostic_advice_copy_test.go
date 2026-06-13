package hazmat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"hazmat/internal/diagnostics"
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
			fixIndex := strings.Index(advice, "hazmat doctor --fix")
			previewIndex := strings.Index(advice, "hazmat doctor --dry-run")
			if fixIndex > previewIndex {
				t.Fatalf("advice = %q, want fix path before preview path", advice)
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
	if !strings.Contains(joined, "no helper-backed probes, backup smokes, or external traffic") {
		t.Fatalf("mode guidance = %q, want quick check helper/backup/external traffic boundary", joined)
	}
	if !strings.Contains(joined, "Helper-backed, backup, and cloud live probes") || !strings.Contains(joined, "sudo-adjacent") {
		t.Fatalf("mode guidance = %q, want full check live-probe disclosure", joined)
	}
}

func TestInitHelpUsesDoctorForPostInitRepairPath(t *testing.T) {
	cmd := newInitCmd()
	text := strings.Join([]string{cmd.Short, cmd.Long}, "\n")
	required := []string{
		"hazmat doctor --fix                         # Apply executable post-init repairs",
		"hazmat doctor --dry-run                     # Preview the typed repair plan",
		"hazmat check                                # Read-only health report",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("init help missing %q:\n%s", phrase, text)
		}
	}
	if strings.Contains(text, "hazmat check                                # Verify the setup") {
		t.Fatalf("init help still presents check as setup verification:\n%s", text)
	}
	fixIndex := strings.Index(text, "hazmat doctor --fix")
	checkIndex := strings.Index(text, "hazmat check")
	if fixIndex < 0 || checkIndex < 0 || fixIndex > checkIndex {
		t.Fatalf("init help should present post-init fix path before read-only check:\n%s", text)
	}
}

func TestBootstrapClaudeHelpPrefersHarnessLifecycleAndDriftRepair(t *testing.T) {
	cmd := newBootstrapClaudeCmd()
	text := strings.Join(strings.Fields(strings.Join([]string{cmd.Short, cmd.Long}, "\n")), " ")
	required := []string{
		"hazmat harness update claude",
		"On a fresh host, run 'hazmat init' first",
		"If setup already ran but helper artifacts drifted, run",
		"hazmat doctor --fix",
		"hazmat doctor --dry-run",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Fatalf("bootstrap claude help missing %q:\n%s", phrase, text)
		}
	}
	for _, stale := range []string{
		"Run once after 'hazmat init'",
		"run 'hazmat init' first if not",
	} {
		if strings.Contains(text, stale) {
			t.Fatalf("bootstrap claude help still uses stale phrase %q:\n%s", stale, text)
		}
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
	if strings.Contains(statusIncompleteSetupAdvice, "Preview first") {
		t.Fatalf("status advice = %q, want preview wording that does not precede the fix path", statusIncompleteSetupAdvice)
	}
}

func TestStatusCredentialInventoryAdviceUsesDoctorRepairPath(t *testing.T) {
	isolateCredentialInventoryTest(t)
	if err := os.MkdirAll(filepath.Dir(configFilePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFilePath, []byte("backup:\n  cloud:\n    access_key: legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureUIOutput(t, func() {
		runErr = runStatus(false)
	})
	if runErr != nil {
		t.Fatalf("runStatus(false): %v", runErr)
	}
	if strings.Contains(out, "run hazmat check") {
		t.Fatalf("status output routes credential drift through check:\n%s", out)
	}
	for _, want := range []string{"Credential inventory", "legacy host credential items", "hazmat doctor --fix", "hazmat doctor --dry-run"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
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

func TestStatusFullHelpNamesLiveProbes(t *testing.T) {
	cmd := newStatusCmd()
	flag := cmd.Flags().Lookup("full")
	if flag == nil {
		t.Fatal("status --full flag missing")
	}
	joined := strings.Join([]string{cmd.Long, flag.Usage}, "\n")
	if !strings.Contains(cmd.Short, "setup progress") || !strings.Contains(cmd.Short, "next action") {
		t.Fatalf("status short help = %q, want progress checklist wording", cmd.Short)
	}
	if strings.Contains(cmd.Short, "health check") {
		t.Fatalf("status short help = %q, want default status distinct from health check", cmd.Short)
	}
	if !strings.Contains(joined, "hazmat check --full") || !strings.Contains(joined, "helper-backed, backup, and cloud live validation") || !strings.Contains(joined, "sudo-adjacent") {
		t.Fatalf("status --full help = %q, want check --full and helper-backed sudo-adjacent wording", joined)
	}
	if strings.Contains(joined, "check --quick") || strings.Contains(joined, "same as 'hazmat check --quick'") {
		t.Fatalf("status --full help = %q, want no quick-mode equivalence", joined)
	}
}

func TestStatusFullDelegatesToFullDiagnosticScheduler(t *testing.T) {
	saved := runStatusFullDiagnostics
	defer func() { runStatusFullDiagnostics = saved }()

	var called int
	var got diagnostics.CheckOptions
	runStatusFullDiagnostics = func(options diagnostics.CheckOptions) error {
		called++
		got = options
		return nil
	}

	if err := runStatus(true); err != nil {
		t.Fatalf("runStatus(true): %v", err)
	}
	if called != 1 {
		t.Fatalf("full diagnostics calls = %d, want 1", called)
	}
	if got.Command != "status" || got.Quick || got.JSON || got.Fix {
		t.Fatalf("full diagnostics options = %+v, want status full non-json non-fix", got)
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
