package hazmat

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSuggestedIntegrationActionDefaultChoiceInteractiveDefaultsToAlways(t *testing.T) {
	if got := suggestedIntegrationActionDefaultChoice(&UI{}); got != string(suggestedIntegrationActionAlways) {
		t.Fatalf("suggestedIntegrationActionDefaultChoice(interactive) = %q, want %q", got, suggestedIntegrationActionAlways)
	}
}

func TestSuggestedIntegrationActionDefaultChoiceYesAllDefaultsToUseNow(t *testing.T) {
	if got := suggestedIntegrationActionDefaultChoice(&UI{YesAll: true}); got != string(suggestedIntegrationActionUseNow) {
		t.Fatalf("suggestedIntegrationActionDefaultChoice(--yes) = %q, want %q", got, suggestedIntegrationActionUseNow)
	}
}

func TestResolveLaunchIntegrationFlagsUseNowAddsSelectedSuggestions(t *testing.T) {
	isolateConfig(t)

	projectDir := integrationTestProject(t, nil)
	canonicalProject, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	flags, err := resolveLaunchIntegrationFlags(canonicalProject, []string{"python-uv", "node", "python-uv"})
	if err != nil {
		t.Fatalf("resolveLaunchIntegrationFlags: %v", err)
	}
	if !reflect.DeepEqual(flags, []string{"node", "python-uv"}) {
		t.Fatalf("flags = %v, want [node python-uv]", flags)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got := cfg.ProjectPinnedIntegrations(canonicalProject); len(got) != 0 {
		t.Fatalf("pinned = %v, want empty", got)
	}
	if got := cfg.ProjectRejectedIntegrations(canonicalProject); len(got) != 0 {
		t.Fatalf("rejected = %v, want empty", got)
	}
}

func TestResolveLaunchIntegrationFlagsAlwaysPersistsSelectedAndRejectedSuggestions(t *testing.T) {
	isolateConfig(t)

	projectDir := integrationTestProject(t, map[string]string{
		"package.json": "{}\n",
		"uv.lock":      "version = 1\n",
	})
	canonicalProject, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	restorePrompt := stubSuggestedIntegrationPrompt(t, func(_ string, items []suggestedIntegrationPromptItem) (suggestedIntegrationPromptResult, error) {
		t.Fatalf("prompt should not be called for launch integration flags, got %v", promptItemNames(items))
		return suggestedIntegrationPromptResult{}, nil
	})
	defer restorePrompt()
	restoreTTY := stubTerminal(t, true)
	defer restoreTTY()

	flags, err := resolveLaunchIntegrationFlags(canonicalProject, nil)
	if err != nil {
		t.Fatalf("resolveLaunchIntegrationFlags: %v", err)
	}
	if len(flags) != 0 {
		t.Fatalf("flags = %v, want empty", flags)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got := cfg.ProjectPinnedIntegrations(canonicalProject); len(got) != 0 {
		t.Fatalf("pinned = %v, want empty", got)
	}
	if got := cfg.ProjectRejectedIntegrations(canonicalProject); len(got) != 0 {
		t.Fatalf("rejected = %v, want empty", got)
	}
}

func TestSuggestedIntegrationActionPromptIncludesExplicitInputHint(t *testing.T) {
	if got := suggestedIntegrationActionPrompt; got != "How should Hazmat use this selection? [1-3, Enter for default]:" {
		t.Fatalf("suggestedIntegrationActionPrompt = %q", got)
	}
}

func TestPromptSuggestedLaunchIntegrationsLinksIntegrationDocs(t *testing.T) {
	isolateConfig(t)

	savedYesAll := flagYesAll
	flagYesAll = true
	t.Cleanup(func() { flagYesAll = savedYesAll })

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	result, runErr := defaultPromptSuggestedLaunchIntegrations("/tmp/project", []suggestedIntegrationPromptItem{
		{Name: "node", Description: "Node.js project defaults"},
	})
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	os.Stderr = oldStderr
	data, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	if runErr != nil {
		t.Fatalf("defaultPromptSuggestedLaunchIntegrations: %v", runErr)
	}
	if result.Action != suggestedIntegrationActionUseNow {
		t.Fatalf("Action = %q, want %q", result.Action, suggestedIntegrationActionUseNow)
	}
	if !strings.Contains(string(data), integrationDocsURL) {
		t.Fatalf("prompt stderr missing integrations doc link:\n%s", string(data))
	}
}

func TestResolveLaunchIntegrationFlagsSkipsRejectedSuggestions(t *testing.T) {
	isolateConfig(t)

	projectDir := integrationTestProject(t, map[string]string{
		"package.json": "{}\n",
	})
	canonicalProject, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	cfg := defaultConfig()
	cfg.Integrations.Rejected = []IntegrationRejection{{
		ProjectDir:   canonicalProject,
		Integrations: []string{"node"},
	}}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	restorePrompt := stubSuggestedIntegrationPrompt(t, func(_ string, _ []suggestedIntegrationPromptItem) (suggestedIntegrationPromptResult, error) {
		t.Fatal("prompt should not be called when suggestions are already rejected")
		return suggestedIntegrationPromptResult{}, nil
	})
	defer restorePrompt()
	restoreTTY := stubTerminal(t, true)
	defer restoreTTY()

	flags, err := resolveLaunchIntegrationFlags(canonicalProject, nil)
	if err != nil {
		t.Fatalf("resolveLaunchIntegrationFlags: %v", err)
	}
	if len(flags) != 0 {
		t.Fatalf("flags = %v, want empty", flags)
	}
}

func TestSuggestedIntegrationsForProjectIncludesUnapprovedRepoRecommendations(t *testing.T) {
	isolateConfig(t)

	savedApprovals := integrationApprovalsFilePath
	integrationApprovalsFilePath = filepath.Join(t.TempDir(), "integration-approvals.yaml")
	t.Cleanup(func() { integrationApprovalsFilePath = savedApprovals })

	projectDir := integrationTestProject(t, map[string]string{
		".hazmat/integrations.yaml": "integrations:\n  - node\n",
	})
	canonicalProject, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	suggestions := suggestedIntegrationsForProject(canonicalProject, map[string]struct{}{})
	if !reflect.DeepEqual(suggestions, []string{"node"}) {
		t.Fatalf("suggestions = %v, want [node]", suggestions)
	}

	flags, err := resolveLaunchIntegrationFlags(canonicalProject, nil)
	if err != nil {
		t.Fatalf("resolveLaunchIntegrationFlags: %v", err)
	}
	if len(flags) != 0 {
		t.Fatalf("flags = %v, want empty before approval", flags)
	}

	_, fileHash, err := loadRepoRecommendations(canonicalProject)
	if err != nil {
		t.Fatalf("loadRepoRecommendations: %v", err)
	}
	if err := recordApproval(canonicalProject, fileHash); err != nil {
		t.Fatalf("recordApproval: %v", err)
	}

	flags, err = resolveLaunchIntegrationFlags(canonicalProject, nil)
	if err != nil {
		t.Fatalf("resolveLaunchIntegrationFlags after approval: %v", err)
	}
	if !reflect.DeepEqual(flags, []string{"node"}) {
		t.Fatalf("flags after approval = %v, want [node]", flags)
	}
}

func TestResolveLaunchIntegrationFlagsSkipsPromptWithoutTTY(t *testing.T) {
	isolateConfig(t)

	projectDir := integrationTestProject(t, map[string]string{
		"package.json": "{}\n",
	})
	canonicalProject, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	restorePrompt := stubSuggestedIntegrationPrompt(t, func(_ string, _ []suggestedIntegrationPromptItem) (suggestedIntegrationPromptResult, error) {
		t.Fatal("prompt should not be called without a TTY")
		return suggestedIntegrationPromptResult{}, nil
	})
	defer restorePrompt()
	restoreTTY := stubTerminal(t, false)
	defer restoreTTY()

	flags, err := resolveLaunchIntegrationFlags(canonicalProject, nil)
	if err != nil {
		t.Fatalf("resolveLaunchIntegrationFlags: %v", err)
	}
	if len(flags) != 0 {
		t.Fatalf("flags = %v, want empty", flags)
	}
}

func TestApplyIntegrationsFiltersRejectedSuggestionsFromSessionContract(t *testing.T) {
	isolateConfig(t)

	projectDir := integrationTestProject(t, map[string]string{
		"package.json": "{}\n",
	})
	canonicalProject, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	cfg := defaultConfig()
	cfg.Integrations.Rejected = []IntegrationRejection{{
		ProjectDir:   canonicalProject,
		Integrations: []string{"node"},
	}}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	sessionCfg, err := resolveSessionConfig(canonicalProject, nil, nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	if _, err := applyIntegrations(&sessionCfg, nil); err != nil {
		t.Fatalf("applyIntegrations: %v", err)
	}
	if len(sessionCfg.SuggestedIntegrations) != 0 {
		t.Fatalf("SuggestedIntegrations = %v, want empty", sessionCfg.SuggestedIntegrations)
	}
}

func TestPrepareLaunchSessionResolvesActiveIntegrationsOnce(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	projectDir := t.TempDir()
	canonicalProjectDir, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	savedResolver := resolveActiveIntegrationsForSession
	calls := 0
	resolveActiveIntegrationsForSession = func(integrationFlags []string, gotProjectDir string) ([]IntegrationSpec, error) {
		calls++
		if gotProjectDir != canonicalProjectDir {
			t.Fatalf("projectDir = %q, want %q", gotProjectDir, canonicalProjectDir)
		}
		if !reflect.DeepEqual(integrationFlags, []string{"custom"}) {
			t.Fatalf("integrationFlags = %v, want [custom]", integrationFlags)
		}
		return []IntegrationSpec{
			{Meta: IntegrationMeta{Name: "custom"}},
		}, nil
	}
	t.Cleanup(func() {
		resolveActiveIntegrationsForSession = savedResolver
	})

	prepared, err := prepareLaunchSession("shell", harnessSessionOpts{
		project:      projectDir,
		integrations: []string{"custom"},
	}, true)
	if err != nil {
		t.Fatalf("prepareLaunchSession: %v", err)
	}
	if calls != 1 {
		t.Fatalf("resolveActiveIntegrationsForSession call count = %d, want 1", calls)
	}
	if !reflect.DeepEqual(prepared.Config.ActiveIntegrations, []string{"custom"}) {
		t.Fatalf("ActiveIntegrations = %v, want [custom]", prepared.Config.ActiveIntegrations)
	}
}

func BenchmarkSessionPreparationLargeTree(b *testing.B) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(b.TempDir(), "nonexistent.yaml")
	b.Cleanup(func() { configFilePath = savedConfigPath })

	savedRequireInit := requireInit
	requireInit = func() error { return nil }
	b.Cleanup(func() { requireInit = savedRequireInit })

	savedCollectAssets := collectDesiredHarnessAssetsForSync
	collectDesiredHarnessAssetsForSync = func(HarnessID) (map[string]harnessAssetDesiredEntry, []string, error) {
		return nil, nil, nil
	}
	b.Cleanup(func() { collectDesiredHarnessAssetsForSync = savedCollectAssets })

	projectDir := b.TempDir()
	for i := 0; i < 200; i++ {
		nested := filepath.Join(projectDir, fmt.Sprintf("pkg-%03d", i), "internal", "deep")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			b.Fatalf("mkdir %s: %v", nested, err)
		}
		for j := 0; j < 20; j++ {
			path := filepath.Join(nested, fmt.Sprintf("file-%03d.txt", j))
			if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
				b.Fatalf("write %s: %v", path, err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module test"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "Spec.tla"), []byte("---- MODULE Spec ----"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "MC.cfg"), []byte("SPECIFICATION Spec"), 0o644); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolved, err := resolveLaunchIntegrations(projectDir, nil)
		if err != nil {
			b.Fatalf("resolveLaunchIntegrations: %v", err)
		}
		prepared, err := resolvePreparedSessionWithProgress("claude", harnessSessionOpts{
			project:              projectDir,
			resolvedIntegrations: resolved.Integrations,
			integrationsResolved: true,
		}, true, nil)
		if err != nil {
			b.Fatalf("resolvePreparedSessionWithProgress: %v", err)
		}
		if !containsString(prepared.Config.SuggestedIntegrations, "go") || !containsString(prepared.Config.SuggestedIntegrations, "tla-java") {
			b.Fatalf("suggested integrations = %v, want go and tla-java", prepared.Config.SuggestedIntegrations)
		}
	}
}

func integrationTestProject(t *testing.T, files map[string]string) string {
	t.Helper()

	projectDir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(projectDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return projectDir
}

func stubSuggestedIntegrationPrompt(t *testing.T, fn func(string, []suggestedIntegrationPromptItem) (suggestedIntegrationPromptResult, error)) func() {
	t.Helper()
	saved := promptSuggestedLaunchIntegrations
	promptSuggestedLaunchIntegrations = fn
	return func() { promptSuggestedLaunchIntegrations = saved }
}

func stubTerminal(t *testing.T, interactive bool) func() {
	t.Helper()
	saved := uiIsTerminal
	uiIsTerminal = func() bool { return interactive }
	return func() { uiIsTerminal = saved }
}

func promptItemNames(items []suggestedIntegrationPromptItem) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}
