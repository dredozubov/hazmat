package main

import (
	"os"
	"path/filepath"
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

func TestRewriteZshrcAPIKeysCanRemoveManagedExports(t *testing.T) {
	original := strings.Join([]string{
		`export ANTHROPIC_API_KEY="ant-old"`,
		`export OPENAI_API_KEY="oai-old"`,
		`alias ll="ls -la"`,
	}, "\n")

	got := rewriteZshrcAPIKeys(original, []pendingAPIKeyUpdate{
		{EnvVar: "ANTHROPIC_API_KEY"},
		{EnvVar: "OPENAI_API_KEY"},
	})

	if strings.Contains(got, "ANTHROPIC_API_KEY") || strings.Contains(got, "OPENAI_API_KEY") {
		t.Fatalf("rewrite should remove managed exports, got:\n%s", got)
	}
	if !strings.Contains(got, `alias ll="ls -la"`) {
		t.Fatalf("rewrite dropped unrelated content:\n%s", got)
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

func TestParseExportedEnvLineValue(t *testing.T) {
	got, ok := parseExportedEnvLineValue(`export ANTHROPIC_API_KEY="real-claude"`, "ANTHROPIC_API_KEY")
	if !ok || got != "real-claude" {
		t.Fatalf("parseExportedEnvLineValue = %q, %v", got, ok)
	}

	got, ok = parseExportedEnvLineValue(`export OPENAI_API_KEY=sk-openai-value`, "OPENAI_API_KEY")
	if !ok || got != "sk-openai-value" {
		t.Fatalf("parseExportedEnvLineValue without quotes = %q, %v", got, ok)
	}
}

func TestLookupConfiguredAPIKeyPrefersHostSecretStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tmpZshrc := filepath.Join(t.TempDir(), ".zshrc")
	writeTestFile(t, tmpZshrc, `export ANTHROPIC_API_KEY="legacy-value"`)
	withAgentZshrcPath(t, tmpZshrc)

	spec := harnessAPIKeyPrompts[0]
	if err := storeHostAPIKey(spec, "stored-value"); err != nil {
		t.Fatalf("storeHostAPIKey: %v", err)
	}

	got, source, err := lookupConfiguredAPIKey(spec)
	if err != nil {
		t.Fatalf("lookupConfiguredAPIKey: %v", err)
	}
	if got != "stored-value" {
		t.Fatalf("lookupConfiguredAPIKey value = %q, want stored-value", got)
	}
	if source != configuredAPIKeySourceStore {
		t.Fatalf("lookupConfiguredAPIKey source = %q, want %q", source, configuredAPIKeySourceStore)
	}
}

func TestLookupConfiguredAPIKeyFallsBackToLegacyAgentZshrc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tmpZshrc := filepath.Join(t.TempDir(), ".zshrc")
	writeTestFile(t, tmpZshrc, `export ANTHROPIC_API_KEY="legacy-value"`)
	withAgentZshrcPath(t, tmpZshrc)

	got, source, err := lookupConfiguredAPIKey(harnessAPIKeyPrompts[0])
	if err != nil {
		t.Fatalf("lookupConfiguredAPIKey: %v", err)
	}
	if got != "legacy-value" {
		t.Fatalf("lookupConfiguredAPIKey value = %q, want legacy-value", got)
	}
	if source != configuredAPIKeySourceLegacy {
		t.Fatalf("lookupConfiguredAPIKey source = %q, want %q", source, configuredAPIKeySourceLegacy)
	}
}

func TestApplyHarnessAPIKeyEnvMigratesLegacyZshrc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tmpZshrc := filepath.Join(t.TempDir(), ".zshrc")
	writeTestFile(t, tmpZshrc, strings.Join([]string{
		`export ANTHROPIC_API_KEY="legacy-value"`,
		`alias ll="ls -la"`,
	}, "\n"))
	withAgentZshrcPath(t, tmpZshrc)

	cfg := sessionConfig{HarnessID: HarnessClaude}
	if err := applyHarnessAPIKeyEnv(&cfg); err != nil {
		t.Fatalf("applyHarnessAPIKeyEnv: %v", err)
	}

	if cfg.HarnessEnv["ANTHROPIC_API_KEY"] != "legacy-value" {
		t.Fatalf("HarnessEnv[ANTHROPIC_API_KEY] = %q, want legacy-value", cfg.HarnessEnv["ANTHROPIC_API_KEY"])
	}
	if len(cfg.SessionNotes) == 0 || !strings.Contains(cfg.SessionNotes[0], "Migrated legacy ANTHROPIC_API_KEY") {
		t.Fatalf("SessionNotes = %v, want migration note", cfg.SessionNotes)
	}

	secretPath, err := providerSecretStorePath("ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatalf("providerSecretStorePath: %v", err)
	}
	raw, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("read migrated secret: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "legacy-value" {
		t.Fatalf("stored secret = %q, want legacy-value", strings.TrimSpace(string(raw)))
	}

	zshrcRaw, err := os.ReadFile(tmpZshrc)
	if err != nil {
		t.Fatalf("read migrated zshrc: %v", err)
	}
	if strings.Contains(string(zshrcRaw), "ANTHROPIC_API_KEY") {
		t.Fatalf("legacy export still present after migration:\n%s", string(zshrcRaw))
	}
	if !strings.Contains(string(zshrcRaw), `alias ll="ls -la"`) {
		t.Fatalf("zshrc rewrite dropped unrelated lines:\n%s", string(zshrcRaw))
	}
}

func TestMaskKeyWithKnownPrefix(t *testing.T) {
	// Keep the real prefix so prefix-anchored masking is exercised, but avoid
	// a full provider-shaped fixture that the repo secret scanner should catch.
	line := `export ANTHROPIC_API_KEY="sk-ant-example1234"`
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

func withAgentZshrcPath(t *testing.T, path string) {
	t.Helper()
	prev := agentZshrcPath
	agentZshrcPath = path
	t.Cleanup(func() {
		agentZshrcPath = prev
	})
}
