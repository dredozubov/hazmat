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

	integration, err := loadBuiltinIntegrationSpec("go")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(go): %v", err)
	}
	t.Setenv("GOROOT", "")

	resolved, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
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

	resolved, err := resolveRuntimeIntegrations(projectDir, []IntegrationSpec{integration})
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
	if _, err := validatedJavaHome("/usr"); err == nil {
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
	got, err := validatedJavaHome(javaHome)
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
	if got := javaHomeFromPrefix("/opt/homebrew/opt/openjdk"); got == "" {
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
	allowAllIntegrationExecutables(t)
	t.Setenv("JAVA_HOME", "/usr")

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
