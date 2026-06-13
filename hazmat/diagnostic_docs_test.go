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
	text := string(data)
	required := []string{
		"no helper-backed agent probes in the default quick mode",
		"hazmat check --full",
		"helper-backed live validation",
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
