package main

import (
	"strings"
	"testing"
)

func TestRewriteZshrcAPIKeysReplacesExistingExport(t *testing.T) {
	original := strings.Join([]string{
		"# user shell config",
		`export PATH="/usr/local/bin:$PATH"`,
		`export ANTHROPIC_API_KEY="old-claude"`,
		`alias ll="ls -la"`,
	}, "\n")

	updates := []pendingAPIKeyUpdate{
		{EnvVar: "ANTHROPIC_API_KEY", Value: "new-claude"},
	}

	got := rewriteZshrcAPIKeys(original, updates)

	if strings.Contains(got, "old-claude") {
		t.Fatalf("rewrite kept stale ANTHROPIC_API_KEY value:\n%s", got)
	}
	if !strings.Contains(got, `export ANTHROPIC_API_KEY="new-claude"`) {
		t.Fatalf("rewrite missing new ANTHROPIC_API_KEY value:\n%s", got)
	}
	// Other lines preserved.
	for _, want := range []string{`# user shell config`, `export PATH="/usr/local/bin:$PATH"`, `alias ll="ls -la"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rewrite dropped unrelated line: %q\n---\n%s", want, got)
		}
	}
}

func TestRewriteZshrcAPIKeysAddsMultipleHarnessKeys(t *testing.T) {
	original := `# nothing here yet`

	updates := []pendingAPIKeyUpdate{
		{EnvVar: "ANTHROPIC_API_KEY", Value: "ant-key"},
		{EnvVar: "OPENAI_API_KEY", Value: "oai-key"},
		{EnvVar: "GEMINI_API_KEY", Value: "gem-key"},
	}

	got := rewriteZshrcAPIKeys(original, updates)

	wantLines := []string{
		`export ANTHROPIC_API_KEY="ant-key"`,
		`export OPENAI_API_KEY="oai-key"`,
		`export GEMINI_API_KEY="gem-key"`,
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in rewrite:\n%s", want, got)
		}
	}
	if !strings.Contains(got, `# nothing here yet`) {
		t.Errorf("preamble dropped:\n%s", got)
	}
}

func TestRewriteZshrcAPIKeysOnlyTouchesNamedVars(t *testing.T) {
	original := strings.Join([]string{
		`export ANTHROPIC_API_KEY="ant-old"`,
		`export OPENAI_API_KEY="oai-keep"`,
		`export GEMINI_API_KEY="gem-keep"`,
	}, "\n")

	updates := []pendingAPIKeyUpdate{
		{EnvVar: "ANTHROPIC_API_KEY", Value: "ant-new"},
	}

	got := rewriteZshrcAPIKeys(original, updates)

	for _, want := range []string{`export OPENAI_API_KEY="oai-keep"`, `export GEMINI_API_KEY="gem-keep"`, `export ANTHROPIC_API_KEY="ant-new"`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in result:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ant-old") {
		t.Fatalf("rewrite kept stale ANTHROPIC value:\n%s", got)
	}
}

func TestReadZshrcEnvLineMatchesExactName(t *testing.T) {
	tmp := t.TempDir() + "/.zshrc"
	content := strings.Join([]string{
		`export FOO_API_KEY="foo"`,
		`export ANTHROPIC_API_KEY="real-claude"`,
		`# ANTHROPIC_API_KEY_BACKUP="should-not-match"`,
	}, "\n")
	writeTestFile(t, tmp, content)

	got := readZshrcEnvLine(tmp, "ANTHROPIC_API_KEY")
	if got != `export ANTHROPIC_API_KEY="real-claude"` {
		t.Fatalf("readZshrcEnvLine returned %q, want exact export line", got)
	}

	if absent := readZshrcEnvLine(tmp, "OPENAI_API_KEY"); absent != "" {
		t.Fatalf("readZshrcEnvLine should return empty for absent var, got %q", absent)
	}
}

func TestMaskKeyWithKnownPrefix(t *testing.T) {
	line := `export ANTHROPIC_API_KEY="sk-ant-abcdefghijklmnopqrstuvwxyz1234"`
	got := maskKey(line, "sk-ant-")
	// Must show the sk-ant- prefix and end of the key for recognition.
	if !strings.HasPrefix(got, "sk-ant-") {
		t.Errorf("masked key should start with sk-ant-, got %q", got)
	}
	if !strings.HasSuffix(got, "1234") {
		t.Errorf("masked key should retain last 4 chars, got %q", got)
	}
	if strings.Contains(got, "abcdefgh") {
		t.Errorf("masked key should hide middle, got %q", got)
	}
}

func TestMaskKeyWithoutKnownPrefixFallsBackToValueMask(t *testing.T) {
	// Deliberately fake fixture: long enough to exercise masking, but not
	// shaped like a provider-issued key that secret scanners should flag.
	line := `export GEMINI_API_KEY="example-gemini-key-abcdefghijklmnopqrstuvwxyz1234"`
	got := maskKey(line, "")
	// No prefix anchor — should still mask middle.
	if strings.Contains(got, "gemini-key-abcdefghijklmnop") {
		t.Errorf("masked key should hide middle, got %q", got)
	}
	if got == "" || got == "(set)" {
		t.Errorf("expected at least a partial mask, got %q", got)
	}
}

func TestMaskHostKeyShortKeyAllStars(t *testing.T) {
	got := maskHostKey("short")
	if got != "*****" {
		t.Errorf("short key should be fully masked, got %q", got)
	}
}

func TestMaskHostKeyLongKeyShowsHeadAndTail(t *testing.T) {
	got := maskHostKey("sk-abcdefghijklmnopqrstuvwxyz1234")
	if !strings.HasPrefix(got, "sk-abcde") {
		t.Errorf("expected first 8 chars preserved, got %q", got)
	}
	if !strings.HasSuffix(got, "1234") {
		t.Errorf("expected last 4 chars preserved, got %q", got)
	}
}

// harnessAPIKeyPrompts must cover every managed harness that has a
// single-env-var auth path. OpenCode is exempt because it auths per-provider
// via 'opencode auth login' (no single env var); future per-provider config
// agent flows can revisit. Catches the case where someone adds a new harness
// to managedHarnessRegistry but forgets to wire its API-key prompt here.
func TestHarnessAPIKeyPromptsCoverAllSingleEnvVarHarnesses(t *testing.T) {
	exempt := map[HarnessID]bool{
		HarnessOpenCode: true, // multi-provider auth, no single env var
	}
	covered := make(map[HarnessID]bool, len(harnessAPIKeyPrompts))
	for _, spec := range harnessAPIKeyPrompts {
		covered[spec.Harness] = true
	}
	for _, h := range managedHarnessRegistry {
		if exempt[h.Spec.ID] {
			continue
		}
		if !covered[h.Spec.ID] {
			t.Errorf("managed harness %q has no entry in harnessAPIKeyPrompts — config agent will not prompt for its API key", h.Spec.ID)
		}
	}
}
