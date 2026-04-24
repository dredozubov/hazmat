package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveLaunchIntegrationFlagsUseNowAddsSelectedSuggestions(t *testing.T) {
	isolateConfig(t)

	projectDir := integrationTestProject(t, map[string]string{
		"package.json": "{}\n",
		"uv.lock":      "version = 1\n",
	})
	canonicalProject, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	restorePrompt := stubSuggestedIntegrationPrompt(t, func(projectDir string, items []suggestedIntegrationPromptItem) (suggestedIntegrationPromptResult, error) {
		if projectDir != canonicalProject {
			t.Fatalf("projectDir = %q, want %q", projectDir, canonicalProject)
		}
		if !sameStringSet(promptItemNames(items), []string{"node", "python-uv"}) {
			t.Fatalf("prompt items = %v", promptItemNames(items))
		}
		return suggestedIntegrationPromptResult{
			Action:   suggestedIntegrationActionUseNow,
			Selected: []string{"python-uv"},
		}, nil
	})
	defer restorePrompt()
	restoreTTY := stubTerminal(t, true)
	defer restoreTTY()

	flags, err := resolveLaunchIntegrationFlags(canonicalProject, nil)
	if err != nil {
		t.Fatalf("resolveLaunchIntegrationFlags: %v", err)
	}
	if !reflect.DeepEqual(flags, []string{"python-uv"}) {
		t.Fatalf("flags = %v, want [python-uv]", flags)
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
		if !sameStringSet(promptItemNames(items), []string{"node", "python-uv"}) {
			t.Fatalf("prompt items = %v", promptItemNames(items))
		}
		return suggestedIntegrationPromptResult{
			Action:   suggestedIntegrationActionAlways,
			Selected: []string{"python-uv"},
		}, nil
	})
	defer restorePrompt()
	restoreTTY := stubTerminal(t, true)
	defer restoreTTY()

	flags, err := resolveLaunchIntegrationFlags(canonicalProject, nil)
	if err != nil {
		t.Fatalf("resolveLaunchIntegrationFlags: %v", err)
	}
	if !reflect.DeepEqual(flags, []string{"python-uv"}) {
		t.Fatalf("flags = %v, want [python-uv]", flags)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got := cfg.ProjectPinnedIntegrations(canonicalProject); !reflect.DeepEqual(got, []string{"python-uv"}) {
		t.Fatalf("pinned = %v, want [python-uv]", got)
	}
	if got := cfg.ProjectRejectedIntegrations(canonicalProject); !reflect.DeepEqual(got, []string{"node"}) {
		t.Fatalf("rejected = %v, want [node]", got)
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

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := make(map[string]struct{}, len(a))
	for _, value := range a {
		as[value] = struct{}{}
	}
	for _, value := range b {
		if _, ok := as[value]; !ok {
			return false
		}
	}
	return true
}
