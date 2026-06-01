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
	if len(cfg.CredentialEnvGrants) != 1 {
		t.Fatalf("CredentialEnvGrants = %v, want one grant", cfg.CredentialEnvGrants)
	}
	if got := cfg.CredentialEnvGrants[0]; got.EnvVar != "ANTHROPIC_API_KEY" || got.CredentialID != credentialProviderAnthropicAPIKey || got.Source != "host secret store" || got.ConsumerHarness != HarnessClaude {
		t.Fatalf("CredentialEnvGrants[0] = %+v", got)
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

func TestApplyHarnessAPIKeyEnvPlanOnlyDoesNotMigrateLegacyZshrc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tmpZshrc := filepath.Join(t.TempDir(), ".zshrc")
	writeTestFile(t, tmpZshrc, strings.Join([]string{
		`export ANTHROPIC_API_KEY="legacy-value"`,
		`alias ll="ls -la"`,
	}, "\n"))
	withAgentZshrcPath(t, tmpZshrc)

	cfg := sessionConfig{HarnessID: HarnessClaude}
	if err := applyHarnessAPIKeyEnvForSession(&cfg, true); err != nil {
		t.Fatalf("applyHarnessAPIKeyEnvForSession: %v", err)
	}

	if cfg.HarnessEnv["ANTHROPIC_API_KEY"] != "legacy-value" {
		t.Fatalf("HarnessEnv[ANTHROPIC_API_KEY] = %q, want legacy-value", cfg.HarnessEnv["ANTHROPIC_API_KEY"])
	}
	if len(cfg.CredentialEnvGrants) != 1 {
		t.Fatalf("CredentialEnvGrants = %v, want one grant", cfg.CredentialEnvGrants)
	}
	if got := cfg.CredentialEnvGrants[0]; got.Source != "legacy agent zshrc" || got.ConsumerHarness != HarnessClaude {
		t.Fatalf("CredentialEnvGrants[0] = %+v, want legacy agent zshrc for Claude", got)
	}
	if len(cfg.SessionNotes) == 0 || !strings.Contains(cfg.SessionNotes[0], "would be migrated") {
		t.Fatalf("SessionNotes = %v, want plan-only migration note", cfg.SessionNotes)
	}

	secretPath, err := providerSecretStorePathForHome(home, "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatalf("providerSecretStorePathForHome: %v", err)
	}
	if _, err := os.Stat(secretPath); !os.IsNotExist(err) {
		t.Fatalf("plan-only explain should not create host secret store file, stat err=%v", err)
	}
	zshrcRaw, err := os.ReadFile(tmpZshrc)
	if err != nil {
		t.Fatalf("read zshrc: %v", err)
	}
	if !strings.Contains(string(zshrcRaw), "ANTHROPIC_API_KEY") {
		t.Fatalf("plan-only explain should preserve legacy export:\n%s", string(zshrcRaw))
	}
}

func TestApplyHarnessAPIKeyEnvDeliversConfiguredProvidersForAllowedHarness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, spec := range harnessAPIKeyPrompts {
		if err := storeHostAPIKey(spec, "stored-"+spec.EnvVar); err != nil {
			t.Fatalf("storeHostAPIKey(%s): %v", spec.EnvVar, err)
		}
	}

	cases := []struct {
		harness HarnessID
		want    map[string]credentialID
	}{
		{
			harness: HarnessClaude,
			want: map[string]credentialID{
				"ANTHROPIC_API_KEY": credentialProviderAnthropicAPIKey,
			},
		},
		{
			harness: HarnessCodex,
			want: map[string]credentialID{
				"OPENAI_API_KEY": credentialProviderOpenAIAPIKey,
			},
		},
		{
			harness: HarnessGemini,
			want: map[string]credentialID{
				"GEMINI_API_KEY": credentialProviderGeminiAPIKey,
			},
		},
		{
			harness: HarnessHermes,
			want: map[string]credentialID{
				"ANTHROPIC_API_KEY":  credentialProviderAnthropicAPIKey,
				"OPENAI_API_KEY":     credentialProviderOpenAIAPIKey,
				"GEMINI_API_KEY":     credentialProviderGeminiAPIKey,
				"OPENROUTER_API_KEY": credentialProviderOpenRouterAPIKey,
			},
		},
		{
			harness: HarnessOpenCode,
			want:    map[string]credentialID{},
		},
		{
			harness: HarnessQwen,
			want:    map[string]credentialID{},
		},
		{
			harness: HarnessCursorAgent,
			want:    map[string]credentialID{},
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.harness), func(t *testing.T) {
			cfg := sessionConfig{HarnessID: tc.harness}
			if err := applyHarnessAPIKeyEnvForSession(&cfg, false); err != nil {
				t.Fatalf("applyHarnessAPIKeyEnvForSession(%s): %v", tc.harness, err)
			}
			if len(cfg.HarnessEnv) != len(tc.want) {
				t.Fatalf("%s HarnessEnv = %v, want %d provider keys", tc.harness, cfg.HarnessEnv, len(tc.want))
			}
			if len(cfg.CredentialEnvGrants) != len(tc.want) {
				t.Fatalf("%s CredentialEnvGrants = %v, want %d grants", tc.harness, cfg.CredentialEnvGrants, len(tc.want))
			}
			for envVar, credentialID := range tc.want {
				if got := cfg.HarnessEnv[envVar]; got != "stored-"+envVar {
					t.Fatalf("%s HarnessEnv[%s] = %q, want stored value", tc.harness, envVar, got)
				}
				found := false
				for _, grant := range cfg.CredentialEnvGrants {
					if grant.EnvVar == envVar {
						found = true
						if grant.CredentialID != credentialID || grant.Source != "host secret store" || grant.ConsumerHarness != tc.harness {
							t.Fatalf("%s grant for %s = %+v", tc.harness, envVar, grant)
						}
					}
				}
				if !found {
					t.Fatalf("%s missing credential grant for %s in %v", tc.harness, envVar, cfg.CredentialEnvGrants)
				}
			}
		})
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

func TestProviderAPIKeyPromptsCoverManagedProviderDescriptors(t *testing.T) {
	covered := make(map[credentialID]bool, len(harnessAPIKeyPrompts))
	for _, spec := range harnessAPIKeyPrompts {
		descriptor := mustCredentialDescriptor(spec.CredentialID)
		if descriptor.Kind != credentialKindProviderAPIKey {
			t.Fatalf("%s prompt points at %s, want provider API key", spec.CredentialID, descriptor.Kind)
		}
		if spec.EnvVar != descriptor.EnvVar {
			t.Fatalf("%s prompt EnvVar = %q, want descriptor env var %q", spec.CredentialID, spec.EnvVar, descriptor.EnvVar)
		}
		covered[spec.CredentialID] = true
	}
	for _, descriptor := range builtinCredentialDescriptors() {
		if descriptor.Kind != credentialKindProviderAPIKey {
			continue
		}
		if !covered[descriptor.ID] {
			t.Errorf("provider credential %q has no config-agent prompt", descriptor.ID)
		}
	}
}

func TestProviderAPIKeyPromptsAreSelectedFromConsumerHarnesses(t *testing.T) {
	cases := []struct {
		name      string
		harnesses []HarnessID
		want      []string
	}{
		{
			name:      "none installed",
			harnesses: nil,
			want:      nil,
		},
		{
			name:      "opencode only",
			harnesses: []HarnessID{HarnessOpenCode},
			want:      nil,
		},
		{
			name:      "qwen only",
			harnesses: []HarnessID{HarnessQwen},
			want:      nil,
		},
		{
			name:      "cursor-agent only",
			harnesses: []HarnessID{HarnessCursorAgent},
			want:      nil,
		},
		{
			name:      "codex",
			harnesses: []HarnessID{HarnessCodex},
			want:      []string{"OPENAI_API_KEY"},
		},
		{
			name:      "hermes",
			harnesses: []HarnessID{HarnessHermes},
			want:      []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"},
		},
		{
			name:      "codex and hermes deduplicate openai",
			harnesses: []HarnessID{HarnessCodex, HarnessHermes},
			want:      []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompts := providerAPIKeyPromptsForHarnesses(tc.harnesses)
			got := make([]string, 0, len(prompts))
			for _, prompt := range prompts {
				got = append(got, prompt.EnvVar)
			}
			if !sameStrings(got, tc.want) {
				t.Fatalf("providerAPIKeyPromptsForHarnesses(%v) = %v, want %v", tc.harnesses, got, tc.want)
			}
		})
	}
}

func TestProviderAPIKeyPromptConsumerLabelIncludesSharedConsumers(t *testing.T) {
	openai := harnessAPIKeyPromptByEnvVar("OPENAI_API_KEY")
	got := providerAPIKeyPromptConsumerLabel(openai)
	want := "Codex and Hermes"
	if got != want {
		t.Fatalf("OpenAI consumer label = %q, want %q", got, want)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestRemoveINIValuesRemovesOnlyLegacyGitHTTPSHelper(t *testing.T) {
	sections := parseINI(strings.Join([]string{
		"[credential]",
		"\thelper = store --file " + gitHTTPSAgentCredentialsPath,
		"\thelper = osxkeychain",
		"[user]",
		"\tname = Agent",
	}, "\n"))

	got := renderINI(removeINIValues(sections, "credential", "helper", isLegacyGitHTTPSCredentialHelperValue))
	if strings.Contains(got, gitHTTPSAgentCredentialsPath) {
		t.Fatalf("legacy helper still present:\n%s", got)
	}
	if !strings.Contains(got, "helper = osxkeychain") {
		t.Fatalf("non-legacy helper was removed:\n%s", got)
	}
	if !strings.Contains(got, "name = Agent") {
		t.Fatalf("unrelated section was changed:\n%s", got)
	}
}

func TestHasLegacyGitHTTPSCredentialHelper(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitconfig")
	writeTestFile(t, path, strings.Join([]string{
		"[credential]",
		"\thelper = store --file " + gitHTTPSAgentCredentialsPath,
	}, "\n"))
	if !hasLegacyGitHTTPSCredentialHelper(path) {
		t.Fatal("hasLegacyGitHTTPSCredentialHelper = false, want true")
	}

	writeTestFile(t, path, strings.Join([]string{
		"[credential]",
		"\thelper = osxkeychain",
	}, "\n"))
	if hasLegacyGitHTTPSCredentialHelper(path) {
		t.Fatal("hasLegacyGitHTTPSCredentialHelper = true for non-legacy helper")
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
