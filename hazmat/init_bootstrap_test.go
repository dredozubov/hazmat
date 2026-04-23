package main

import "testing"

func TestNormalizeInitBootstrapAgent(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "", want: initBootstrapSkip},
		{input: "skip", want: initBootstrapSkip},
		{input: "claude", want: "claude"},
		{input: "CoDeX", want: "codex"},
		{input: " opencode ", want: "opencode"},
		{input: "gemini", want: "gemini"},
		{input: "GEMINI", want: "gemini"},
		{input: "none", wantErr: true},
	}

	for _, tc := range tests {
		got, err := normalizeInitBootstrapAgent(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("normalizeInitBootstrapAgent(%q) = %q, want error", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeInitBootstrapAgent(%q) unexpected error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeInitBootstrapAgent(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestResolveInitBootstrapAgentDefaultsToSkipWithoutTTY(t *testing.T) {
	got, err := resolveInitBootstrapAgent(&UI{}, "")
	if err != nil {
		t.Fatalf("resolveInitBootstrapAgent unexpected error: %v", err)
	}
	if got != initBootstrapSkip {
		t.Fatalf("resolveInitBootstrapAgent = %q, want %q", got, initBootstrapSkip)
	}
}

func TestResolveInitBootstrapAgentHonorsExplicitFlag(t *testing.T) {
	got, err := resolveInitBootstrapAgent(&UI{YesAll: true}, "codex")
	if err != nil {
		t.Fatalf("resolveInitBootstrapAgent unexpected error: %v", err)
	}
	if got != "codex" {
		t.Fatalf("resolveInitBootstrapAgent = %q, want codex", got)
	}
}

// TestOfferHarnessBasicsImportCoversEveryManagedHarness asserts that every
// managed harness has a dispatch case in offerHarnessBasicsImport. Catches
// the case where someone adds a new harness to managedHarnessRegistry but
// forgets to wire the post-bootstrap import offer.
func TestOfferHarnessBasicsImportCoversEveryManagedHarness(t *testing.T) {
	for _, h := range managedHarnessRegistry {
		if !offerHarnessBasicsImportCovers(string(h.Spec.ID)) {
			t.Errorf("managed harness %q has no dispatch case in offerHarnessBasicsImport — 'hazmat init --bootstrap-agent %s' will not offer the basics import", h.Spec.ID, h.Spec.ID)
		}
	}
}

func TestOfferHarnessBasicsImportRejectsUnknownSelections(t *testing.T) {
	for _, sel := range []string{"", initBootstrapSkip, "unknown", "Claude" /* case-sensitive */} {
		if offerHarnessBasicsImportCovers(sel) {
			t.Errorf("offerHarnessBasicsImportCovers(%q) returned true; should only match the four lowercase harness IDs", sel)
		}
	}
}

func TestManagedHarnessRegistryIncludesSupportedLaunchCommands(t *testing.T) {
	harnesses := managedHarnesses()
	if len(harnesses) != 4 {
		t.Fatalf("managedHarnesses length = %d, want 4", len(harnesses))
	}

	want := map[HarnessID]string{
		HarnessClaude:   "hazmat claude",
		HarnessCodex:    "hazmat codex",
		HarnessOpenCode: "hazmat opencode",
		HarnessGemini:   "hazmat gemini",
	}

	for _, harness := range harnesses {
		if got := harness.LaunchCommand; got != want[harness.Spec.ID] {
			t.Fatalf("launch command for %s = %q, want %q", harness.Spec.ID, got, want[harness.Spec.ID])
		}
	}
}
