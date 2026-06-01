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
		{input: "hermes", want: "hermes"},
		{input: "HERMES", want: "hermes"},
		{input: "qwen", want: "qwen"},
		{input: "QWEN", want: "qwen"},
		{input: "cursor-agent", want: "cursor-agent"},
		{input: "CURSOR-AGENT", want: "cursor-agent"},
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

// TestOfferHarnessBasicsImportCoversImportableHarnesses asserts that every
// curated-import harness has a dispatch case in offerHarnessBasicsImport.
// Hermes, Qwen, and Cursor Agent are managed but intentionally not importable in Phase 1.
func TestOfferHarnessBasicsImportCoversImportableHarnesses(t *testing.T) {
	for _, id := range []HarnessID{HarnessClaude, HarnessCodex, HarnessOpenCode, HarnessGemini} {
		if !offerHarnessBasicsImportCovers(string(id)) {
			t.Errorf("importable harness %q has no dispatch case in offerHarnessBasicsImport", id)
		}
	}
	if offerHarnessBasicsImportCovers(string(HarnessHermes)) {
		t.Errorf("Hermes must not offer host-profile import in Phase 1")
	}
	if offerHarnessBasicsImportCovers(string(HarnessQwen)) {
		t.Errorf("Qwen must not offer host-profile import in Phase 1")
	}
	if offerHarnessBasicsImportCovers(string(HarnessCursorAgent)) {
		t.Errorf("Cursor Agent must not offer host-profile import in Phase 1")
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
	if len(harnesses) != 7 {
		t.Fatalf("managedHarnesses length = %d, want 7", len(harnesses))
	}

	want := map[HarnessID]string{
		HarnessClaude:      "hazmat claude",
		HarnessCodex:       "hazmat codex",
		HarnessOpenCode:    "hazmat opencode",
		HarnessGemini:      "hazmat gemini",
		HarnessHermes:      "hazmat hermes",
		HarnessQwen:        "hazmat qwen",
		HarnessCursorAgent: "hazmat cursor-agent",
	}

	for _, harness := range harnesses {
		if got := harness.LaunchCommand; got != want[harness.Spec.ID] {
			t.Fatalf("launch command for %s = %q, want %q", harness.Spec.ID, got, want[harness.Spec.ID])
		}
		if harness.BootstrapCommand == "" || harness.Installed == nil || harness.Bootstrap == nil {
			t.Fatalf("managed harness %s has incomplete bootstrap metadata", harness.Spec.ID)
		}
		if harness.Probe == nil || harness.ManagedCodeArtifacts == nil || len(harness.PreservedArtifacts) == 0 {
			t.Fatalf("managed harness %s has incomplete lifecycle metadata", harness.Spec.ID)
		}
	}
}
