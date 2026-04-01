package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeSandboxProbe struct {
	lookPathErr error
	outputs     map[string]fakeSandboxResult
	runErrs     map[string]error
	calls       []string
}

type fakeSandboxResult struct {
	output string
	err    error
}

func (f *fakeSandboxProbe) LookPath(name string) (string, error) {
	if f.lookPathErr != nil {
		return "", f.lookPathErr
	}
	return "/usr/local/bin/" + name, nil
}

func (f *fakeSandboxProbe) Output(name string, args ...string) (string, error) {
	key := sandboxProbeKey(name, args...)
	f.calls = append(f.calls, "output:"+key)
	if result, ok := f.outputs[key]; ok {
		return result.output, result.err
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func (f *fakeSandboxProbe) Run(name string, args ...string) error {
	key := sandboxProbeKey(name, args...)
	f.calls = append(f.calls, "run:"+key)
	if err, ok := f.runErrs[key]; ok {
		return err
	}
	return nil
}

func sandboxProbeKey(name string, args ...string) string {
	return name + "\x00" + strings.Join(args, "\x00")
}

func healthySandboxProbe() *fakeSandboxProbe {
	return &fakeSandboxProbe{
		outputs: map[string]fakeSandboxResult{
			sandboxProbeKey("docker", "desktop", "version", "--short"): {
				output: "4.58.1",
			},
			sandboxProbeKey("docker", "compose", "version"): {
				output: "Docker Compose version v2.40.3",
			},
			sandboxProbeKey("docker", "sandbox", "ls", "--json"): {
				output: `{"vms":[]}`,
			},
			sandboxProbeKey("docker", "sandbox", "network", "proxy", "--help"): {
				output: "Usage: docker sandbox network proxy [OPTIONS]\n      --policy string\n      --allow-host strings\n",
			},
		},
	}
}

func containsSandboxCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func TestExtractToolSemver(t *testing.T) {
	tests := []struct {
		raw  string
		want semver
	}{
		{"4.58.1", semver{major: 4, minor: 58, patch: 1}},
		{"Docker Compose version v2.40.3-desktop.1", semver{major: 2, minor: 40, patch: 3}},
		{"Docker Desktop 4.59.0 (123456)", semver{major: 4, minor: 59, patch: 0}},
	}

	for _, tt := range tests {
		got, err := extractToolSemver(tt.raw)
		if err != nil {
			t.Fatalf("extractToolSemver(%q): %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("extractToolSemver(%q) = %+v, want %+v", tt.raw, got, tt.want)
		}
	}
}

func TestValidateSandboxListJSON(t *testing.T) {
	count, err := validateSandboxListJSON(`{"vms":[{"name":"claude-demo"}]}`)
	if err != nil {
		t.Fatalf("validateSandboxListJSON: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestValidateSandboxPolicyProfileRejectsDuplicateHost(t *testing.T) {
	err := validateSandboxPolicyProfile(sandboxPolicyProfile{
		Name:       "baseline",
		Policy:     "deny",
		AllowHosts: []string{"github.com", "github.com"},
	})
	if err == nil {
		t.Fatal("expected duplicate allow-host to be rejected")
	}
}

func TestCollectSandboxDoctorReportHealthy(t *testing.T) {
	report := collectSandboxDoctorReport(healthySandboxProbe())
	if !report.Healthy() {
		t.Fatalf("expected healthy report, got %+v", report.Checks)
	}
	if report.DesktopVersion != "4.58.1" {
		t.Fatalf("DesktopVersion = %q, want 4.58.1", report.DesktopVersion)
	}
	if report.ComposeVersion != "2.40.3" {
		t.Fatalf("ComposeVersion = %q, want 2.40.3", report.ComposeVersion)
	}
}

func TestCollectSandboxDoctorReportOldDesktopVersionFails(t *testing.T) {
	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "desktop", "version", "--short")] = fakeSandboxResult{
		output: "4.57.0",
	}

	report := collectSandboxDoctorReport(probe)
	if report.Healthy() {
		t.Fatal("expected report to fail when Docker Desktop is too old")
	}
}

func TestRunSandboxSetupPersistsBackendConfig(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return healthySandboxProbe() }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	savedNow := sandboxNow
	sandboxNow = func() time.Time { return time.Date(2026, 4, 1, 16, 30, 0, 0, time.UTC) }
	defer func() { sandboxNow = savedNow }()

	savedDryRun := flagDryRun
	flagDryRun = false
	defer func() { flagDryRun = savedDryRun }()

	savedYesAll := flagYesAll
	flagYesAll = false
	defer func() { flagYesAll = savedYesAll }()

	if err := runSandboxSetup(); err != nil {
		t.Fatalf("runSandboxSetup: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	backend := cfg.SandboxBackend()
	if backend == nil {
		t.Fatal("expected sandbox backend to be saved")
	}
	if backend.Type != sandboxBackendDockerSandboxes {
		t.Fatalf("backend.Type = %q, want %q", backend.Type, sandboxBackendDockerSandboxes)
	}
	if backend.PolicyProfile != sandboxPolicyProfileBaseline {
		t.Fatalf("backend.PolicyProfile = %q, want %q", backend.PolicyProfile, sandboxPolicyProfileBaseline)
	}
	if backend.DesktopVersion != "4.58.1" {
		t.Fatalf("backend.DesktopVersion = %q, want 4.58.1", backend.DesktopVersion)
	}
	if backend.ComposeVersion != "2.40.3" {
		t.Fatalf("backend.ComposeVersion = %q, want 2.40.3", backend.ComposeVersion)
	}
}

func TestRunSandboxResetClearsBackendConfig(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:          sandboxBackendDockerSandboxes,
		PolicyProfile: sandboxPolicyProfileBaseline,
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	savedDryRun := flagDryRun
	flagDryRun = false
	defer func() { flagDryRun = savedDryRun }()

	savedYesAll := flagYesAll
	flagYesAll = true
	defer func() { flagYesAll = savedYesAll }()

	if err := runSandboxReset(); err != nil {
		t.Fatalf("runSandboxReset: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.SandboxBackend() != nil {
		t.Fatal("expected sandbox backend to be cleared")
	}
}

func TestRunSandboxClaudeSessionCreatesPolicyAndRuns(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:           sandboxBackendDockerSandboxes,
		PolicyProfile:  sandboxPolicyProfileBaseline,
		DesktopVersion: "4.61.0",
		ComposeVersion: "2.40.3",
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	projectDir := t.TempDir()
	sessionCfg := sessionConfig{ProjectDir: projectDir}
	name := sandboxName("claude", sessionCfg, sandboxPolicyProfileBaseline)

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "desktop", "version", "--short")] = fakeSandboxResult{
		output: "4.61.1",
	}
	probe.outputs[sandboxProbeKey("docker", "sandbox", "create", "--name", name, "claude", projectDir)] = fakeSandboxResult{
		output: "created",
	}
	probe.outputs[sandboxProbeKey("docker", "sandbox", "network", "proxy", name, "--policy", "deny",
		"--allow-host", "api.anthropic.com",
		"--allow-host", "github.com",
		"--allow-host", "registry.npmjs.org")] = fakeSandboxResult{
		output: "configured",
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	if err := runSandboxClaudeSession(sessionCfg, []string{"-p", "hi"}); err != nil {
		t.Fatalf("runSandboxClaudeSession: %v", err)
	}

	if !containsSandboxCall(probe.calls, "output:"+sandboxProbeKey("docker", "sandbox", "create", "--name", name, "claude", projectDir)) {
		t.Fatal("expected sandbox create command to be issued")
	}
	if !containsSandboxCall(probe.calls, "output:"+sandboxProbeKey("docker", "sandbox", "network", "proxy", name, "--policy", "deny",
		"--allow-host", "api.anthropic.com",
		"--allow-host", "github.com",
		"--allow-host", "registry.npmjs.org")) {
		t.Fatal("expected sandbox policy command to be issued")
	}
	if !containsSandboxCall(probe.calls, "run:"+sandboxProbeKey("docker", "sandbox", "run", name, "--",
		"--dangerously-skip-permissions", "-p", "hi")) {
		t.Fatal("expected sandbox run command with forwarded Claude args")
	}
}

func TestRunSandboxExecSessionUsesShellSandbox(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:           sandboxBackendDockerSandboxes,
		PolicyProfile:  sandboxPolicyProfileBaseline,
		DesktopVersion: "4.61.0",
		ComposeVersion: "2.40.3",
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	projectDir := t.TempDir()
	sessionCfg := sessionConfig{ProjectDir: projectDir}
	name := sandboxName("shell", sessionCfg, sandboxPolicyProfileBaseline)

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "desktop", "version", "--short")] = fakeSandboxResult{
		output: "4.61.1",
	}
	probe.outputs[sandboxProbeKey("docker", "sandbox", "create", "--name", name, "shell", projectDir)] = fakeSandboxResult{
		output: "created",
	}
	probe.outputs[sandboxProbeKey("docker", "sandbox", "network", "proxy", name, "--policy", "deny",
		"--allow-host", "api.anthropic.com",
		"--allow-host", "github.com",
		"--allow-host", "registry.npmjs.org")] = fakeSandboxResult{
		output: "configured",
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	if err := runSandboxExecSession(sessionCfg, []string{"make", "test"}); err != nil {
		t.Fatalf("runSandboxExecSession: %v", err)
	}

	if !containsSandboxCall(probe.calls, "run:"+sandboxProbeKey("docker", "sandbox", "run", name, "--",
		"-lc", `cd "$1" && shift && exec "$@"`, "bash", projectDir, "make", "test")) {
		t.Fatal("expected sandbox exec run command")
	}
}

func TestRunSandboxShellSessionRequiresDesktop461(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:           sandboxBackendDockerSandboxes,
		PolicyProfile:  sandboxPolicyProfileBaseline,
		DesktopVersion: "4.58.1",
		ComposeVersion: "2.40.3",
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	probe := healthySandboxProbe()
	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	err := runSandboxShellSession(sessionConfig{ProjectDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected shell sandbox launch to require newer Docker Desktop")
	}
	if !strings.Contains(err.Error(), "require >= 4.61.0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSandboxClaudeSessionReadDirsRequireDesktop461(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:           sandboxBackendDockerSandboxes,
		PolicyProfile:  sandboxPolicyProfileBaseline,
		DesktopVersion: "4.58.1",
		ComposeVersion: "2.40.3",
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	probe := healthySandboxProbe()
	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	err := runSandboxClaudeSession(sessionConfig{
		ProjectDir: t.TempDir(),
		ReadDirs:   []string{t.TempDir()},
	}, nil)
	if err == nil {
		t.Fatal("expected extra workspace launch to require newer Docker Desktop")
	}
	if !strings.Contains(err.Error(), "additional read-only workspaces") {
		t.Fatalf("unexpected error: %v", err)
	}
}
