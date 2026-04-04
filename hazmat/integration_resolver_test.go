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

func stubIntegrationEnv(t *testing.T, values map[string]string) {
	t.Helper()
	saved := integrationGetenv
	integrationGetenv = func(key string) string {
		if value, ok := values[key]; ok {
			return value
		}
		return os.Getenv(key)
	}
	t.Cleanup(func() { integrationGetenv = saved })
}

type fakeIntegrationProbe struct {
	outputs      map[string]string
	lookPaths    map[string]string
	lookPathErrs map[string]error
	outputErrs   map[string]error
}

func (p *fakeIntegrationProbe) LookPath(name string) (string, error) {
	if err := p.lookPathErrs[name]; err != nil {
		return "", err
	}
	if path, ok := p.lookPaths[name]; ok {
		return path, nil
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

func writeExecutable(t *testing.T, root, name string) string {
	t.Helper()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", binDir, err)
	}
	path := filepath.Join(binDir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
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

	integration, err := loadBuiltinIntegrationSpec("go")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(go): %v", err)
	}
	t.Setenv("GOROOT", "")

	resolved, _, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
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

func TestResolveRuntimeIntegrationsHaskellCabalUsesRuntimeProbe(t *testing.T) {
	allowAllIntegrationExecutables(t)
	projectDir := t.TempDir()
	ghcRoot := filepath.Join(t.TempDir(), "ghc-root")
	cabalRoot := filepath.Join(t.TempDir(), "cabal-root")
	ghcPath := writeExecutable(t, ghcRoot, "ghc")
	cabalPath := writeExecutable(t, cabalRoot, "cabal")
	canonicalGHC, err := canonicalizePath(ghcRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(ghcRoot): %v", err)
	}
	canonicalCabal, err := canonicalizePath(cabalRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(cabalRoot): %v", err)
	}

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			lookPaths: map[string]string{
				"ghc":   ghcPath,
				"cabal": cabalPath,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	integration, err := loadBuiltinIntegrationSpec("haskell-cabal")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(haskell-cabal): %v", err)
	}
	resolved, _, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
	if err != nil {
		t.Fatalf("resolveRuntimeIntegrations: %v", err)
	}
	if got := resolved[0].AdditionalReadDirs; len(got) != 2 || got[0] != canonicalGHC || got[1] != canonicalCabal {
		t.Fatalf("AdditionalReadDirs = %v, want [%q %q]", got, canonicalGHC, canonicalCabal)
	}
	if resolved[0].Source != "haskell-cabal (ghc runtime, cabal runtime)" {
		t.Fatalf("Source = %q", resolved[0].Source)
	}
}

func TestResolveRuntimeIntegrationsPythonUVUsesRuntimeProbe(t *testing.T) {
	allowAllIntegrationExecutables(t)
	projectDir := t.TempDir()
	pythonRoot := filepath.Join(t.TempDir(), "python-root")
	pythonPath := writeExecutable(t, pythonRoot, "python3")
	canonicalPythonRoot, err := canonicalizePath(pythonRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(pythonRoot): %v", err)
	}
	canonicalPythonPath, err := canonicalizePath(pythonPath)
	if err != nil {
		t.Fatalf("canonicalizePath(pythonPath): %v", err)
	}

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			outputs: map[string]string{
				"python3 -c import os, sys; print(os.path.realpath(sys.executable))": pythonPath,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	integration, err := loadBuiltinIntegrationSpec("python-uv")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(python-uv): %v", err)
	}
	resolved, _, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
	if err != nil {
		t.Fatalf("resolveRuntimeIntegrations: %v", err)
	}
	if got := resolved[0].AdditionalReadDirs; len(got) != 1 || got[0] != canonicalPythonRoot {
		t.Fatalf("AdditionalReadDirs = %v, want [%q]", got, canonicalPythonRoot)
	}
	if resolved[0].ResolvedEnv["UV_PYTHON"] != canonicalPythonPath {
		t.Fatalf("ResolvedEnv[UV_PYTHON] = %q, want %q", resolved[0].ResolvedEnv["UV_PYTHON"], canonicalPythonPath)
	}
	wantPath := filepath.Join(canonicalPythonRoot, "bin") + string(os.PathListSeparator) + defaultAgentPath
	if resolved[0].ResolvedEnv["PATH"] != wantPath {
		t.Fatalf("ResolvedEnv[PATH] = %q, want %q", resolved[0].ResolvedEnv["PATH"], wantPath)
	}
	if resolved[0].Source != "python-uv (python runtime)" {
		t.Fatalf("Source = %q", resolved[0].Source)
	}
}

func TestResolveRuntimeIntegrationsPythonPoetryFallsBackToHomebrewPython(t *testing.T) {
	isolateConfig(t)
	allowAllIntegrationExecutables(t)
	if err := runConfigSet("integrations.homebrew", "enabled"); err != nil {
		t.Fatalf("runConfigSet enabled: %v", err)
	}

	projectDir := t.TempDir()
	pythonRoot := filepath.Join(t.TempDir(), "python-root")
	pythonPath := writeExecutable(t, pythonRoot, "python3")
	canonicalPythonRoot, err := canonicalizePath(pythonRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(pythonRoot): %v", err)
	}
	canonicalPythonPath, err := canonicalizePath(pythonPath)
	if err != nil {
		t.Fatalf("canonicalizePath(pythonPath): %v", err)
	}

	savedCandidates := integrationBrewCandidates
	integrationBrewCandidates = []string{"/bin/echo"}
	t.Cleanup(func() { integrationBrewCandidates = savedCandidates })

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			outputErrs: map[string]error{
				"python3 -c import os, sys; print(os.path.realpath(sys.executable))": fmt.Errorf("missing python3"),
				"python -c import os, sys; print(os.path.realpath(sys.executable))":  fmt.Errorf("missing python"),
			},
			outputs: map[string]string{
				"/bin/echo --prefix --installed python@3.14": pythonRoot,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	integration, err := loadBuiltinIntegrationSpec("python-poetry")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(python-poetry): %v", err)
	}
	resolved, _, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
	if err != nil {
		t.Fatalf("resolveRuntimeIntegrations: %v", err)
	}
	if got := resolved[0].AdditionalReadDirs; len(got) != 1 || got[0] != canonicalPythonRoot {
		t.Fatalf("AdditionalReadDirs = %v, want [%q]", got, canonicalPythonRoot)
	}
	if got := resolved[0].ResolvedEnv["UV_PYTHON"]; got != "" {
		t.Fatalf("ResolvedEnv[UV_PYTHON] = %q, want empty for python-poetry", got)
	}
	wantPath := filepath.Join(canonicalPythonRoot, "bin") + string(os.PathListSeparator) + defaultAgentPath
	if resolved[0].ResolvedEnv["PATH"] != wantPath {
		t.Fatalf("ResolvedEnv[PATH] = %q, want %q", resolved[0].ResolvedEnv["PATH"], wantPath)
	}
	if resolved[0].Source != "python-poetry (Homebrew python@3.14)" {
		t.Fatalf("Source = %q", resolved[0].Source)
	}
	if details := strings.Join(resolved[0].Details, "\n"); !strings.Contains(details, canonicalPythonPath) {
		t.Fatalf("Details = %v, want entry containing %q", resolved[0].Details, canonicalPythonPath)
	}
}

func TestResolveRuntimeIntegrationsJavaGradleUsesJavaAndGradleRuntime(t *testing.T) {
	allowAllIntegrationExecutables(t)
	projectDir := t.TempDir()
	javaHome := filepath.Join(t.TempDir(), "jdk")
	gradleRoot := filepath.Join(t.TempDir(), "gradle-root")
	gradlePath := writeExecutable(t, gradleRoot, "gradle")
	if err := os.MkdirAll(filepath.Join(javaHome, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir javaHome/bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(javaHome, "bin", "java"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write java: %v", err)
	}
	canonicalJavaHome, err := canonicalizePath(javaHome)
	if err != nil {
		t.Fatalf("canonicalizePath(javaHome): %v", err)
	}
	canonicalGradleRoot, err := canonicalizePath(gradleRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(gradleRoot): %v", err)
	}

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			lookPaths: map[string]string{
				"gradle": gradlePath,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })
	stubIntegrationEnv(t, map[string]string{"JAVA_HOME": javaHome})

	integration, err := loadBuiltinIntegrationSpec("java-gradle")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(java-gradle): %v", err)
	}
	resolved, _, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
	if err != nil {
		t.Fatalf("resolveRuntimeIntegrations: %v", err)
	}
	if got := resolved[0].AdditionalReadDirs; len(got) != 2 || got[0] != canonicalJavaHome || got[1] != canonicalGradleRoot {
		t.Fatalf("AdditionalReadDirs = %v, want [%q %q]", got, canonicalJavaHome, canonicalGradleRoot)
	}
	if resolved[0].ResolvedEnv["JAVA_HOME"] != "" {
		t.Fatalf("ResolvedEnv[JAVA_HOME] = %q, want empty because valid JAVA_HOME should pass through unchanged", resolved[0].ResolvedEnv["JAVA_HOME"])
	}
}

func TestResolveRuntimeIntegrationsRubyBundlerUsesHomebrewFallback(t *testing.T) {
	isolateConfig(t)
	allowAllIntegrationExecutables(t)
	projectDir := t.TempDir()
	rubyRoot := filepath.Join(t.TempDir(), "ruby-root")
	writeExecutable(t, rubyRoot, "ruby")
	canonicalRubyRoot, err := canonicalizePath(rubyRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(rubyRoot): %v", err)
	}

	brewBin := filepath.Join(t.TempDir(), "brew")
	if err := os.WriteFile(brewBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write brewBin: %v", err)
	}
	savedCandidates := integrationBrewCandidates
	integrationBrewCandidates = []string{brewBin}
	t.Cleanup(func() { integrationBrewCandidates = savedCandidates })
	if err := runConfigSet("integrations.homebrew", "enabled"); err != nil {
		t.Fatalf("runConfigSet enabled: %v", err)
	}

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			lookPathErrs: map[string]error{
				"ruby": fmt.Errorf("missing ruby in PATH"),
			},
			outputs: map[string]string{
				commandLabel(brewBin, "--prefix", "--installed", "ruby"): rubyRoot,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	integration, err := loadBuiltinIntegrationSpec("ruby-bundler")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(ruby-bundler): %v", err)
	}
	resolved, _, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
	if err != nil {
		t.Fatalf("resolveRuntimeIntegrations: %v", err)
	}
	if got := resolved[0].AdditionalReadDirs; len(got) != 1 || got[0] != canonicalRubyRoot {
		t.Fatalf("AdditionalReadDirs = %v, want [%q]", got, canonicalRubyRoot)
	}
	if resolved[0].Source != "ruby-bundler (Homebrew ruby)" {
		t.Fatalf("Source = %q", resolved[0].Source)
	}
}

func TestResolveRuntimeIntegrationsElixirMixUsesRuntimeProbe(t *testing.T) {
	allowAllIntegrationExecutables(t)
	projectDir := t.TempDir()
	elixirRoot := filepath.Join(t.TempDir(), "elixir-root")
	erlangRoot := filepath.Join(t.TempDir(), "erlang-root")
	elixirPath := writeExecutable(t, elixirRoot, "elixir")
	erlPath := writeExecutable(t, erlangRoot, "erl")
	canonicalElixirRoot, err := canonicalizePath(elixirRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(elixirRoot): %v", err)
	}
	canonicalErlangRoot, err := canonicalizePath(erlangRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(erlangRoot): %v", err)
	}

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			lookPaths: map[string]string{
				"elixir": elixirPath,
				"erl":    erlPath,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	integration, err := loadBuiltinIntegrationSpec("elixir-mix")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(elixir-mix): %v", err)
	}
	resolved, _, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
	if err != nil {
		t.Fatalf("resolveRuntimeIntegrations: %v", err)
	}
	if got := resolved[0].AdditionalReadDirs; len(got) != 2 || got[0] != canonicalElixirRoot || got[1] != canonicalErlangRoot {
		t.Fatalf("AdditionalReadDirs = %v, want [%q %q]", got, canonicalElixirRoot, canonicalErlangRoot)
	}
}

func TestResolveRuntimeIntegrationsOpenTofuUsesHomebrewFallback(t *testing.T) {
	isolateConfig(t)
	allowAllIntegrationExecutables(t)
	projectDir := t.TempDir()
	tofuRoot := filepath.Join(t.TempDir(), "tofu-root")
	writeExecutable(t, tofuRoot, "tofu")
	canonicalTofuRoot, err := canonicalizePath(tofuRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(tofuRoot): %v", err)
	}

	brewBin := filepath.Join(t.TempDir(), "brew")
	if err := os.WriteFile(brewBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write brewBin: %v", err)
	}
	savedCandidates := integrationBrewCandidates
	integrationBrewCandidates = []string{brewBin}
	t.Cleanup(func() { integrationBrewCandidates = savedCandidates })
	if err := runConfigSet("integrations.homebrew", "enabled"); err != nil {
		t.Fatalf("runConfigSet enabled: %v", err)
	}

	savedFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{
			lookPathErrs: map[string]error{
				"tofu": fmt.Errorf("missing tofu in PATH"),
			},
			outputs: map[string]string{
				commandLabel(brewBin, "--prefix", "--installed", "opentofu"): tofuRoot,
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedFactory })

	integration, err := loadBuiltinIntegrationSpec("opentofu-plan")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(opentofu-plan): %v", err)
	}
	resolved, _, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
	if err != nil {
		t.Fatalf("resolveRuntimeIntegrations: %v", err)
	}
	if got := resolved[0].AdditionalReadDirs; len(got) != 1 || got[0] != canonicalTofuRoot {
		t.Fatalf("AdditionalReadDirs = %v, want [%q]", got, canonicalTofuRoot)
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
			Spec: IntegrationSpec{
				Meta: IntegrationMeta{Name: "node", Version: 1},
				Session: IntegrationSession{
					ReadDirs: []string{declaredDir},
				},
			},
			ReplaceDeclaredReadDirs: true,
			AdditionalReadDirs:      []string{resolvedDir},
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

	if _, err := applyIntegrations(&cfg, []string{"go"}); err != nil {
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

func TestCommandPathFromEnvPrefersProvidedPath(t *testing.T) {
	hostDir := filepath.Join(t.TempDir(), "host-bin")
	envDir := filepath.Join(t.TempDir(), "env-bin")
	for _, dir := range []string{hostDir, envDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	hostTool := filepath.Join(hostDir, "demo-probe")
	envTool := filepath.Join(envDir, "demo-probe")
	if err := os.WriteFile(hostTool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write host tool: %v", err)
	}
	if err := os.WriteFile(envTool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write env tool: %v", err)
	}

	t.Setenv("PATH", hostDir)

	resolved, err := commandPathFromEnv("demo-probe", []string{"PATH=" + envDir})
	if err != nil {
		t.Fatalf("commandPathFromEnv: %v", err)
	}
	if resolved != envTool {
		t.Fatalf("resolved = %q, want %q", resolved, envTool)
	}
}

func TestCommandPathFromEnvRespectsAbsolutePath(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "abs-probe")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	resolved, err := commandPathFromEnv(tool, nil)
	if err != nil {
		t.Fatalf("commandPathFromEnv absolute path: %v", err)
	}
	if resolved != tool {
		t.Fatalf("resolved = %q, want %q", resolved, tool)
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

	integration, err := loadBuiltinIntegrationSpec("go")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(go): %v", err)
	}
	t.Setenv("GOROOT", "")

	resolved, _, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
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

func TestValidatedJavaHomeRejectsLauncherStub(t *testing.T) {
	if _, err := os.Stat("/usr/bin/java"); err != nil {
		t.Skip("/usr/bin/java not present on this platform")
	}
	if _, err := validatedJavaHome(nil, "/usr"); err == nil {
		t.Fatal("validatedJavaHome(/usr) should reject the macOS launcher stub")
	}
}

func TestValidatedJavaHomeAcceptsRealHome(t *testing.T) {
	allowAllIntegrationExecutables(t)
	javaHome := filepath.Join(t.TempDir(), "jdk")
	if err := os.MkdirAll(filepath.Join(javaHome, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir javaHome: %v", err)
	}
	if err := os.WriteFile(filepath.Join(javaHome, "bin", "java"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write java binary: %v", err)
	}
	got, err := validatedJavaHome(nil, javaHome)
	if err != nil {
		t.Fatalf("validatedJavaHome(real home): %v", err)
	}
	want, err := canonicalizePath(javaHome)
	if err != nil {
		t.Fatalf("canonicalizePath(javaHome): %v", err)
	}
	if got != want {
		t.Fatalf("validatedJavaHome(real home) = %q, want %q", got, want)
	}
}

func TestJavaHomeFromInstalledOpenJDKPrefix(t *testing.T) {
	allowAllIntegrationExecutables(t)
	if _, err := os.Stat("/opt/homebrew/opt/openjdk"); err != nil {
		t.Skip("installed openjdk prefix not present")
	}
	if got := javaHomeFromPrefix(nil, "/opt/homebrew/opt/openjdk"); got == "" {
		t.Fatal("javaHomeFromPrefix(/opt/homebrew/opt/openjdk) returned empty")
	}
}

func TestBrewPrefixFindsInstalledOpenJDK(t *testing.T) {
	isolateConfig(t)
	if _, err := os.Stat("/opt/homebrew/opt/openjdk"); err != nil {
		t.Skip("installed openjdk prefix not present")
	}
	if err := runConfigSet("integrations.homebrew", "enabled"); err != nil {
		t.Fatalf("runConfigSet enabled: %v", err)
	}

	ctx := &integrationResolveContext{
		ProjectDir: t.TempDir(),
		Probe:      hostIntegrationProbe{},
	}
	result := ctx.brewPrefix("openjdk")
	if result.Prefix == "" {
		t.Fatalf("brewPrefix(openjdk) returned no prefix: %+v", result)
	}
}

func TestIntegrationTimeoutForCommandUsesLongerTimeoutForBrew(t *testing.T) {
	if got := integrationTimeoutForCommand("/opt/homebrew/bin/brew"); got != integrationHomebrewTimeout {
		t.Fatalf("integrationTimeoutForCommand(brew) = %s, want %s", got, integrationHomebrewTimeout)
	}
	if got := integrationTimeoutForCommand("go"); got != integrationProbeTimeout {
		t.Fatalf("integrationTimeoutForCommand(go) = %s, want %s", got, integrationProbeTimeout)
	}
}

func TestBrewPrefixSurfacesProbeError(t *testing.T) {
	isolateConfig(t)
	if err := runConfigSet("integrations.homebrew", "enabled"); err != nil {
		t.Fatalf("runConfigSet enabled: %v", err)
	}

	savedCandidates := integrationBrewCandidates
	integrationBrewCandidates = []string{"/bin/echo"}
	t.Cleanup(func() { integrationBrewCandidates = savedCandidates })

	ctx := &integrationResolveContext{
		ProjectDir: t.TempDir(),
		Probe: &fakeIntegrationProbe{
			outputErrs: map[string]error{
				"/bin/echo --prefix --installed openjdk": fmt.Errorf("brew timed out after 10s"),
			},
		},
	}
	result := ctx.brewPrefix("openjdk")
	if !strings.Contains(result.Detail, "timed out") {
		t.Fatalf("brewPrefix probe error detail = %q, want timeout note", result.Detail)
	}
}

func TestBrewPrefixUsesOptPrefixBeforeProbe(t *testing.T) {
	isolateConfig(t)
	if err := runConfigSet("integrations.homebrew", "enabled"); err != nil {
		t.Fatalf("runConfigSet enabled: %v", err)
	}

	brewRoot := filepath.Join(t.TempDir(), "homebrew")
	brewBin := filepath.Join(brewRoot, "bin", "brew")
	optPrefix := filepath.Join(brewRoot, "opt", "openjdk")
	if err := os.MkdirAll(filepath.Dir(brewBin), 0o755); err != nil {
		t.Fatalf("mkdir brew bin: %v", err)
	}
	if err := os.WriteFile(brewBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write brew bin: %v", err)
	}
	if err := os.MkdirAll(optPrefix, 0o755); err != nil {
		t.Fatalf("mkdir opt prefix: %v", err)
	}

	savedCandidates := integrationBrewCandidates
	integrationBrewCandidates = []string{brewBin}
	t.Cleanup(func() { integrationBrewCandidates = savedCandidates })

	ctx := &integrationResolveContext{
		ProjectDir: t.TempDir(),
		Probe: &fakeIntegrationProbe{
			outputErrs: map[string]error{
				brewBin + " --prefix --installed openjdk": fmt.Errorf("probe should not run when opt prefix exists"),
			},
		},
	}
	result := ctx.brewPrefix("openjdk")
	want, err := canonicalizePath(optPrefix)
	if err != nil {
		t.Fatalf("canonicalizePath(optPrefix): %v", err)
	}
	if result.Prefix != want {
		t.Fatalf("brewPrefix(openjdk) Prefix = %q, want %q", result.Prefix, want)
	}
	if result.Formula != "openjdk" {
		t.Fatalf("brewPrefix(openjdk) Formula = %q, want openjdk", result.Formula)
	}
}

func TestResolveTLAJavaIntegrationOverridesInvalidJavaHome(t *testing.T) {
	isolateConfig(t)
	allowAllIntegrationExecutables(t)
	invalidJavaHome := filepath.Join(t.TempDir(), "invalid-java-home")
	if err := os.MkdirAll(invalidJavaHome, 0o755); err != nil {
		t.Fatalf("mkdir invalidJavaHome: %v", err)
	}
	stubIntegrationEnv(t, map[string]string{"JAVA_HOME": invalidJavaHome})
	if got := integrationGetenv("JAVA_HOME"); got != invalidJavaHome {
		t.Fatalf("integrationGetenv(JAVA_HOME) = %q, want %q", got, invalidJavaHome)
	}
	if _, err := validatedJavaHome(nil, invalidJavaHome); err == nil {
		t.Fatalf("validatedJavaHome(%q) unexpectedly succeeded", invalidJavaHome)
	}
	if !shouldSetResolvedJavaHomeEnv() {
		t.Fatal("shouldSetResolvedJavaHomeEnv() = false, want true for invalid JAVA_HOME")
	}
	if err := runConfigSet("integrations.homebrew", "enabled"); err != nil {
		t.Fatalf("runConfigSet enabled: %v", err)
	}

	prefix := filepath.Join(t.TempDir(), "openjdk-prefix")
	javaHome := filepath.Join(prefix, "libexec", "openjdk.jdk", "Contents", "Home")
	if err := os.MkdirAll(filepath.Join(javaHome, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir javaHome: %v", err)
	}
	if err := os.WriteFile(filepath.Join(javaHome, "bin", "java"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write java binary: %v", err)
	}

	savedCandidates := integrationBrewCandidates
	integrationBrewCandidates = []string{"/bin/echo"}
	t.Cleanup(func() { integrationBrewCandidates = savedCandidates })

	ctx := &integrationResolveContext{
		ProjectDir: t.TempDir(),
		Probe: &fakeIntegrationProbe{
			outputs: map[string]string{
				"/bin/echo --prefix --installed openjdk": prefix,
			},
		},
	}
	integration, err := loadBuiltinIntegrationSpec("tla-java")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(tla-java): %v", err)
	}

	resolved, err := resolveTLAJavaIntegration(ctx, integration)
	if err != nil {
		t.Fatalf("resolveTLAJavaIntegration: %v", err)
	}
	want, err := canonicalizePath(javaHome)
	if err != nil {
		t.Fatalf("canonicalizePath(javaHome): %v", err)
	}
	if resolved.ResolvedEnv["JAVA_HOME"] != want {
		t.Fatalf("ResolvedEnv[JAVA_HOME] = %q, want %q", resolved.ResolvedEnv["JAVA_HOME"], want)
	}
}

func TestHomebrewCellarRoot(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/opt/homebrew/Cellar/go/1.26.1/libexec", "/opt/homebrew/Cellar/go/1.26.1"},
		{"/opt/homebrew/Cellar/go/1.26.1", "/opt/homebrew/Cellar/go/1.26.1"},
		{"/opt/homebrew/Cellar/golangci-lint/2.11.4/bin", "/opt/homebrew/Cellar/golangci-lint/2.11.4"},
		{"/usr/local/Cellar/node/22.0.0/lib", "/usr/local/Cellar/node/22.0.0"},
		{"/opt/homebrew/opt/go", ""},    // not a Cellar path
		{"/usr/local/bin/go", ""},       // not a Cellar path
		{"/opt/homebrew/Cellar/go", ""}, // incomplete — no version
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// homebrewCellarRoot also stats the dir; for tests with non-existent paths
			// it returns "" which is correct.
			got := homebrewCellarRoot(tt.input)
			if tt.want == "" {
				if got != "" {
					t.Errorf("homebrewCellarRoot(%q) = %q, want empty", tt.input, got)
				}
				return
			}
			// For paths that exist on this machine, check the result
			if _, err := os.Stat(tt.want); err != nil {
				t.Skipf("test path %q does not exist on this machine", tt.want)
			}
			if got != tt.want {
				t.Errorf("homebrewCellarRoot(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRepairHomebrewToolAccessSkipsNonHomebrew(t *testing.T) {
	// Ensure repair does nothing for non-Homebrew paths.
	saved := repairHomebrewToolAccess
	called := false
	repairHomebrewToolAccess = func(dir string) bool {
		// The impl calls homebrewCellarRoot which returns "" for non-Cellar paths.
		called = true
		return repairHomebrewToolAccessImpl(dir)
	}
	t.Cleanup(func() { repairHomebrewToolAccess = saved })

	// A non-Homebrew path should not be repaired.
	result := repairHomebrewToolAccess("/tmp/some-tool")
	if result {
		t.Error("repairHomebrewToolAccess should return false for non-Homebrew paths")
	}
	if !called {
		t.Error("expected repairHomebrewToolAccess to be called")
	}
}
