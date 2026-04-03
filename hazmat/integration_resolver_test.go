package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func allowAllIntegrationExecutables(t *testing.T) {
	t.Helper()
	saved := integrationAgentExecCheck
	integrationAgentExecCheck = func(string) bool { return true }
	t.Cleanup(func() { integrationAgentExecCheck = saved })
}

type fakeIntegrationProbe struct {
	outputs      map[string]string
	lookPathErrs map[string]error
	outputErrs   map[string]error
}

func (p *fakeIntegrationProbe) LookPath(name string) (string, error) {
	if err := p.lookPathErrs[name]; err != nil {
		return "", err
	}
	return "/usr/bin/" + name, nil
}

func (p *fakeIntegrationProbe) Output(name string, args ...string) (string, error) {
	key := commandLabel(name, args...)
	if err := p.outputErrs[key]; err != nil {
		return "", err
	}
	if value, ok := p.outputs[key]; ok {
		return value, nil
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func TestRunConfigSetIntegrationsHomebrew(t *testing.T) {
	isolateConfig(t)

	if err := runConfigSet("integrations.homebrew", "enabled"); err != nil {
		t.Fatalf("runConfigSet enabled: %v", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after enabled: %v", err)
	}
	if allowed, configured := cfg.HomebrewIntegrationConsent(); !configured || !allowed {
		t.Fatalf("HomebrewIntegrationConsent after enabled = (%v, %v), want (true, true)", allowed, configured)
	}

	if err := runConfigSet("integrations.homebrew", "disabled"); err != nil {
		t.Fatalf("runConfigSet disabled: %v", err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after disabled: %v", err)
	}
	if allowed, configured := cfg.HomebrewIntegrationConsent(); !configured || allowed {
		t.Fatalf("HomebrewIntegrationConsent after disabled = (%v, %v), want (false, true)", allowed, configured)
	}

	if err := runConfigSet("integrations.homebrew", "ask"); err != nil {
		t.Fatalf("runConfigSet ask: %v", err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after ask: %v", err)
	}
	if _, configured := cfg.HomebrewIntegrationConsent(); configured {
		t.Fatal("HomebrewIntegrationConsent should be unset after ask")
	}
}

func TestResolveRuntimeIntegrationsGoUsesRuntimeProbe(t *testing.T) {
	allowAllIntegrationExecutables(t)
	projectDir := t.TempDir()
	goRoot := filepath.Join(t.TempDir(), "go-root")
	if err := os.MkdirAll(filepath.Join(goRoot, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir goRoot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goRoot, "bin", "go"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write go binary: %v", err)
	}
	canonicalGoRoot, err := canonicalizePath(goRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(goRoot): %v", err)
	}

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			outputs: map[string]string{
				"go env GOROOT": goRoot,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	pack, err := loadBuiltinPack("go")
	if err != nil {
		t.Fatalf("loadBuiltinPack(go): %v", err)
	}
	t.Setenv("GOROOT", "")

	resolved, err := resolveRuntimeIntegrations(projectDir, []Pack{pack})
	if err != nil {
		t.Fatalf("resolveRuntimeIntegrations: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(resolved))
	}
	if len(resolved[0].AdditionalReadDirs) != 1 || resolved[0].AdditionalReadDirs[0] != canonicalGoRoot {
		t.Fatalf("AdditionalReadDirs = %v, want [%q]", resolved[0].AdditionalReadDirs, canonicalGoRoot)
	}
	if resolved[0].ResolvedEnv["GOROOT"] != canonicalGoRoot {
		t.Fatalf("ResolvedEnv[GOROOT] = %q, want %q", resolved[0].ResolvedEnv["GOROOT"], canonicalGoRoot)
	}
	if resolved[0].Source != "go (go env GOROOT)" {
		t.Fatalf("Source = %q", resolved[0].Source)
	}
	if len(resolved[0].Details) == 0 || !strings.Contains(resolved[0].Details[0], canonicalGoRoot) {
		t.Fatalf("Details = %v, want entry containing %q", resolved[0].Details, canonicalGoRoot)
	}
}

func TestMergeResolvedIntegrationsReplacesPackReadDirs(t *testing.T) {
	declaredDir := filepath.Join(t.TempDir(), "declared")
	resolvedDir := filepath.Join(t.TempDir(), "resolved")
	for _, dir := range []string{declaredDir, resolvedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	result, err := mergeResolvedIntegrations([]resolvedIntegration{
		{
			Pack: Pack{
				PackMeta: PackMeta{Name: "node", Version: 1},
				Session: PackSession{
					ReadDirs: []string{declaredDir},
				},
			},
			ReplacePackReadDirs: true,
			AdditionalReadDirs:  []string{resolvedDir},
		},
	})
	if err != nil {
		t.Fatalf("mergeResolvedIntegrations: %v", err)
	}
	if len(result.ReadDirs) != 1 || result.ReadDirs[0] != resolvedDir {
		t.Fatalf("ReadDirs = %v, want [%q]", result.ReadDirs, resolvedDir)
	}
}

func TestApplyIntegrationsPopulatesSourcesAndDetails(t *testing.T) {
	isolateConfig(t)
	allowAllIntegrationExecutables(t)
	projectDir := t.TempDir()
	goRoot := filepath.Join(t.TempDir(), "go-root")
	if err := os.MkdirAll(filepath.Join(goRoot, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir goRoot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goRoot, "bin", "go"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write go binary: %v", err)
	}
	canonicalGoRoot, err := canonicalizePath(goRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(goRoot): %v", err)
	}

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			outputs: map[string]string{
				"go env GOROOT": goRoot,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	cfg := sessionConfig{
		ProjectDir:     projectDir,
		BackupExcludes: snapshotIgnoreRules(nil),
	}
	t.Setenv("GOROOT", "")

	if err := applyIntegrations(&cfg, []string{"go"}); err != nil {
		t.Fatalf("applyIntegrations: %v", err)
	}

	if len(cfg.IntegrationSources) != 1 || cfg.IntegrationSources[0] != "go (go env GOROOT)" {
		t.Fatalf("IntegrationSources = %v", cfg.IntegrationSources)
	}
	if len(cfg.IntegrationDetails) == 0 || !strings.Contains(strings.Join(cfg.IntegrationDetails, "\n"), canonicalGoRoot) {
		t.Fatalf("IntegrationDetails = %v, want path %q", cfg.IntegrationDetails, canonicalGoRoot)
	}
	foundAuto := false
	for _, dir := range cfg.AutoReadDirs {
		if dir == canonicalGoRoot {
			foundAuto = true
			break
		}
	}
	if !foundAuto {
		t.Fatalf("AutoReadDirs = %v, want %q", cfg.AutoReadDirs, canonicalGoRoot)
	}
}

func TestRenderIntegrationDetails(t *testing.T) {
	got := renderIntegrationDetails([]string{
		"go: resolved GOROOT via go env -> /tmp/go",
		"node: Homebrew fallback skipped: consent not configured",
	})

	for _, want := range []string{
		"hazmat: integration resolution",
		"go: resolved GOROOT via go env -> /tmp/go",
		"node: Homebrew fallback skipped: consent not configured",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderIntegrationDetails missing %q in:\n%s", want, got)
		}
	}
}

func TestIntegrationProbeEnvUsesDefaultAgentPath(t *testing.T) {
	t.Setenv("PATH", "/tmp/not-the-agent-path")

	env := integrationProbeEnv()
	found := false
	for _, entry := range env {
		if entry == "PATH="+defaultAgentPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("integrationProbeEnv() did not use defaultAgentPath: %v", env)
	}
}

func TestResolveRuntimeIntegrationsGoSkipsInaccessibleRuntime(t *testing.T) {
	projectDir := t.TempDir()
	goRoot := filepath.Join(t.TempDir(), "go-root")
	if err := os.MkdirAll(filepath.Join(goRoot, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir goRoot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goRoot, "bin", "go"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write go binary: %v", err)
	}

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			outputs: map[string]string{
				"go env GOROOT": goRoot,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	savedExecCheck := integrationAgentExecCheck
	integrationAgentExecCheck = func(path string) bool {
		return !strings.HasSuffix(path, filepath.Join("bin", "go"))
	}
	t.Cleanup(func() { integrationAgentExecCheck = savedExecCheck })

	savedCandidates := integrationBrewCandidates
	integrationBrewCandidates = nil
	t.Cleanup(func() { integrationBrewCandidates = savedCandidates })

	pack, err := loadBuiltinPack("go")
	if err != nil {
		t.Fatalf("loadBuiltinPack(go): %v", err)
	}
	t.Setenv("GOROOT", "")

	resolved, err := resolveRuntimeIntegrations(projectDir, []Pack{pack})
	if err != nil {
		t.Fatalf("resolveRuntimeIntegrations: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(resolved))
	}
	if len(resolved[0].AdditionalReadDirs) != 0 {
		t.Fatalf("AdditionalReadDirs = %v, want none", resolved[0].AdditionalReadDirs)
	}
	if resolved[0].Source != "" {
		t.Fatalf("Source = %q, want empty", resolved[0].Source)
	}
	details := strings.Join(resolved[0].Details, "\n")
	if !strings.Contains(details, "cannot execute") {
		t.Fatalf("Details = %v, want cannot execute note", resolved[0].Details)
	}
}
