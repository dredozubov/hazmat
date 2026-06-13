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
		"helper-backed, backup, and cloud live validation",
		"requires explicit exact-command approval",
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
		"`hazmat status` is the lightweight setup progress checklist",
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
