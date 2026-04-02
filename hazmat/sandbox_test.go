package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeSandboxProbe struct {
	lookPathErr error
	outputs     map[string]fakeSandboxResult
	outputSeq   map[string][]fakeSandboxResult
	runResults  map[string]fakeSandboxResult
	runSeq      map[string][]fakeSandboxResult
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
	if seq := f.outputSeq[key]; len(seq) > 0 {
		result := seq[0]
		f.outputSeq[key] = seq[1:]
		return result.output, result.err
	}
	if result, ok := f.outputs[key]; ok {
		return result.output, result.err
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func (f *fakeSandboxProbe) Run(name string, args ...string) (string, error) {
	key := sandboxProbeKey(name, args...)
	f.calls = append(f.calls, "run:"+key)
	if seq := f.runSeq[key]; len(seq) > 0 {
		result := seq[0]
		f.runSeq[key] = seq[1:]
		return result.output, result.err
	}
	if result, ok := f.runResults[key]; ok {
		return result.output, result.err
	}
	return "", nil
}

func sandboxProbeKey(name string, args ...string) string {
	return name + "\x00" + strings.Join(args, "\x00")
}

func healthySandboxProbe() *fakeSandboxProbe {
	return &fakeSandboxProbe{
		outputs: map[string]fakeSandboxResult{
			sandboxProbeKey("docker", "version", "--format", "{{json .Server}}"): {
				output: `{"Platform":{"Name":"Docker Desktop 4.58.1 (123456)"}}`,
			},
			sandboxProbeKey("docker", "compose", "version"): {
				output: "Docker Compose version v2.40.3",
			},
			sandboxProbeKey("docker", "sandbox", "ls", "--json"): {
				output: `{"sandboxes":[]}`,
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

func countSandboxCall(calls []string, want string) int {
	count := 0
	for _, call := range calls {
		if call == want {
			count++
		}
	}
	return count
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
	count, err := validateSandboxListJSON(`{"sandboxes":[{"name":"claude-demo"}]}`)
	if err != nil {
		t.Fatalf("validateSandboxListJSON: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestValidateSandboxListJSONLegacyVMs(t *testing.T) {
	count, err := validateSandboxListJSON(`{"vms":[{"name":"claude-demo"}]}`)
	if err != nil {
		t.Fatalf("validateSandboxListJSON legacy: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy count = %d, want 1", count)
	}
}

func TestSandboxStatusFromJSON(t *testing.T) {
	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "sandbox", "ls", "--json")] = fakeSandboxResult{
		output: `{"sandboxes":[{"name":"hazmat-demo","status":"stopped"}]}`,
	}

	status, exists, err := sandboxStatus(probe, "hazmat-demo")
	if err != nil {
		t.Fatalf("sandboxStatus: %v", err)
	}
	if !exists {
		t.Fatal("expected sandbox to exist")
	}
	if status != "stopped" {
		t.Fatalf("status = %q, want stopped", status)
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
	probe.outputs[sandboxProbeKey("docker", "version", "--format", "{{json .Server}}")] = fakeSandboxResult{
		output: `{"Platform":{"Name":"Docker Desktop 4.57.0 (123456)"}}`,
	}

	report := collectSandboxDoctorReport(probe)
	if report.Healthy() {
		t.Fatal("expected report to fail when Docker Desktop is too old")
	}
}

func TestCollectSandboxDoctorReportFallsBackToDockerAppVersion(t *testing.T) {
	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "version", "--format", "{{json .Server}}")] = fakeSandboxResult{
		output: "null\nError response from daemon: Docker Desktop is unable to start",
		err:    errors.New("daemon unavailable"),
	}
	probe.outputs[sandboxProbeKey("plutil", "-extract", "CFBundleShortVersionString", "raw", "/Applications/Docker.app/Contents/Info.plist")] = fakeSandboxResult{
		output: "4.67.0",
	}

	report := collectSandboxDoctorReport(probe)
	if !report.Healthy() {
		t.Fatalf("expected fallback version probe to keep report healthy, got %+v", report.Checks)
	}
	if report.DesktopVersion != "4.67.0" {
		t.Fatalf("DesktopVersion = %q, want 4.67.0", report.DesktopVersion)
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
	if len(cfg.ManagedSandboxes()) != 0 {
		t.Fatalf("expected managed sandboxes to be cleared, got %d", len(cfg.ManagedSandboxes()))
	}
}

func TestSandboxApprovalRoundTrip(t *testing.T) {
	saved := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	t.Cleanup(func() { sandboxApprovalsFilePath = saved })

	if sandboxApprovalGranted("/test/project", sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline) {
		t.Fatal("should not be approved before recording")
	}
	if err := recordSandboxApproval("/test/project", sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatal(err)
	}
	if !sandboxApprovalGranted("/test/project", sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline) {
		t.Fatal("should be approved after recording")
	}
}

func TestSandboxApprovalInvalidatedOnPolicyChange(t *testing.T) {
	saved := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	t.Cleanup(func() { sandboxApprovalsFilePath = saved })

	if err := recordSandboxApproval("/test/project", sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatal(err)
	}
	if !sandboxApprovalGranted("/test/project", sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline) {
		t.Fatal("should be approved with original tuple")
	}
	if sandboxApprovalGranted("/test/project", sandboxBackendDockerSandboxes, "stricter") {
		t.Fatal("approval should not survive policy change")
	}
	if sandboxApprovalGranted("/test/project", "colima", sandboxPolicyProfileBaseline) {
		t.Fatal("approval should not survive backend change")
	}
}

func TestEnsureSandboxApprovalYesAllRecordsApproval(t *testing.T) {
	savedPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedPath }()

	savedYesAll := flagYesAll
	flagYesAll = true
	defer func() { flagYesAll = savedYesAll }()

	savedDryRun := flagDryRun
	flagDryRun = false
	defer func() { flagDryRun = savedDryRun }()

	if err := ensureSandboxApproval("/test/project", sandboxBackendDockerSandboxes, defaultSandboxPolicyProfile()); err != nil {
		t.Fatalf("ensureSandboxApproval: %v", err)
	}
	if !sandboxApprovalGranted("/test/project", sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline) {
		t.Fatal("approval should be recorded when --yes is active")
	}
}

func TestBuildSandboxLaunchSpecRejectsCredentialProjectDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	_, err = buildSandboxLaunchSpec("claude", sessionConfig{ProjectDir: home}, defaultSandboxPolicyProfile())
	if err == nil {
		t.Fatal("expected credential-parent project dir to be rejected")
	}
	if !strings.Contains(err.Error(), "credential deny zone") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildSandboxLaunchSpecRejectsCredentialReadDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	_, err = buildSandboxLaunchSpec("claude", sessionConfig{
		ProjectDir: t.TempDir(),
		ReadDirs:   []string{filepath.Join(home, ".ssh")},
	}, defaultSandboxPolicyProfile())
	if err == nil {
		t.Fatal("expected credential read dir to be rejected")
	}
	if !strings.Contains(err.Error(), "credential deny zone") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildSandboxLaunchSpecFiltersCoveredReadDirs(t *testing.T) {
	projectDir := t.TempDir()
	projectChild := filepath.Join(projectDir, "child")
	if err := os.MkdirAll(projectChild, 0o755); err != nil {
		t.Fatalf("mkdir projectChild: %v", err)
	}

	refDir := filepath.Join(t.TempDir(), "ref")
	refChild := filepath.Join(refDir, "nested")
	for _, dir := range []string{refDir, refChild} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	spec, err := buildSandboxLaunchSpec("claude", sessionConfig{
		ProjectDir: projectDir,
		ReadDirs:   []string{projectChild, refDir, refChild},
	}, defaultSandboxPolicyProfile())
	if err != nil {
		t.Fatalf("buildSandboxLaunchSpec: %v", err)
	}
	if len(spec.MountReadDirs) != 1 || spec.MountReadDirs[0] != refDir {
		t.Fatalf("MountReadDirs = %v, want [%q]", spec.MountReadDirs, refDir)
	}
	wantName := sandboxName("claude", projectDir, []string{refDir}, sandboxPolicyProfileBaseline)
	if spec.Name != wantName {
		t.Fatalf("spec.Name = %q, want %q", spec.Name, wantName)
	}
}

func TestBuildSandboxLaunchSpecExpandsAncestorReadDirsNoSiblings(t *testing.T) {
	workspaceDir := t.TempDir()
	projectDir := filepath.Join(workspaceDir, "niche-sieve")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir projectDir: %v", err)
	}
	cacheDir := filepath.Join(t.TempDir(), "pkgmod")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cacheDir: %v", err)
	}

	// workspaceDir has only one child (niche-sieve = project),
	// so ancestor expansion yields no siblings.
	spec, err := buildSandboxLaunchSpec("claude", sessionConfig{
		ProjectDir: projectDir,
		ReadDirs:   []string{workspaceDir, cacheDir},
	}, defaultSandboxPolicyProfile())
	if err != nil {
		t.Fatalf("buildSandboxLaunchSpec: %v", err)
	}
	if len(spec.MountReadDirs) != 1 || spec.MountReadDirs[0] != cacheDir {
		t.Fatalf("MountReadDirs = %v, want [%q]", spec.MountReadDirs, cacheDir)
	}
	wantName := sandboxName("claude", projectDir, []string{cacheDir}, sandboxPolicyProfileBaseline)
	if spec.Name != wantName {
		t.Fatalf("spec.Name = %q, want %q", spec.Name, wantName)
	}
}

func TestBuildSandboxLaunchSpecExpandsAncestorReadDirsWithSiblings(t *testing.T) {
	// Resolve symlinks on TempDir (macOS: /var → /private/var) so test
	// paths match the EvalSymlinks output from expandAncestorReadDir.
	workspaceDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	projectDir := filepath.Join(workspaceDir, "project")
	siblingA := filepath.Join(workspaceDir, "sibling-a")
	siblingB := filepath.Join(workspaceDir, "sibling-b")
	for _, dir := range []string{projectDir, siblingA, siblingB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	// Also create a regular file — should not appear in mounts.
	if err := os.WriteFile(filepath.Join(workspaceDir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	spec, err := buildSandboxLaunchSpec("claude", sessionConfig{
		ProjectDir: projectDir,
		ReadDirs:   []string{workspaceDir},
	}, defaultSandboxPolicyProfile())
	if err != nil {
		t.Fatalf("buildSandboxLaunchSpec: %v", err)
	}

	got := make(map[string]struct{})
	for _, d := range spec.MountReadDirs {
		got[d] = struct{}{}
	}
	if _, ok := got[siblingA]; !ok {
		t.Errorf("MountReadDirs missing sibling %q; got %v", siblingA, spec.MountReadDirs)
	}
	if _, ok := got[siblingB]; !ok {
		t.Errorf("MountReadDirs missing sibling %q; got %v", siblingB, spec.MountReadDirs)
	}
	if _, ok := got[projectDir]; ok {
		t.Errorf("MountReadDirs should not contain projectDir %q", projectDir)
	}
	if _, ok := got[workspaceDir]; ok {
		t.Errorf("MountReadDirs should not contain ancestor %q", workspaceDir)
	}
	if len(spec.MountReadDirs) != 2 {
		t.Errorf("MountReadDirs = %v, want exactly 2 siblings", spec.MountReadDirs)
	}
}

func TestExpandAncestorReadDirDeepNesting(t *testing.T) {
	// Resolve symlinks on TempDir (macOS: /var → /private/var).
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	// root/team/project is the project dir;
	// root/docs and root/team/sibling should appear as siblings.
	teamDir := filepath.Join(root, "team")
	projectDir := filepath.Join(teamDir, "project")
	sibling := filepath.Join(teamDir, "sibling")
	topSibling := filepath.Join(root, "docs")
	for _, dir := range []string{projectDir, sibling, topSibling} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	result := expandAncestorReadDir(root, projectDir)
	got := make(map[string]struct{})
	for _, d := range result {
		got[d] = struct{}{}
	}
	if _, ok := got[topSibling]; !ok {
		t.Errorf("missing top-level sibling %q; got %v", topSibling, result)
	}
	if _, ok := got[sibling]; !ok {
		t.Errorf("missing nested sibling %q; got %v", sibling, result)
	}
	if _, ok := got[teamDir]; ok {
		t.Errorf("should not include intermediate dir %q on path to project", teamDir)
	}
}

func TestRunSandboxClaudeSessionCreatesPolicyAndRuns(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()
	savedApprovalsPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedApprovalsPath }()

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
	name := sandboxName("claude", projectDir, nil, sandboxPolicyProfileBaseline)
	if err := recordSandboxApproval(projectDir, sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatalf("recordSandboxApproval: %v", err)
	}

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "version", "--format", "{{json .Server}}")] = fakeSandboxResult{
		output: `{"Platform":{"Name":"Docker Desktop 4.61.1 (123456)"}}`,
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	if err := runSandboxClaudeSession(sessionCfg, []string{"-p", "hi"}); err != nil {
		t.Fatalf("runSandboxClaudeSession: %v", err)
	}

	if !containsSandboxCall(probe.calls, "run:"+sandboxProbeKey("docker", "sandbox", "create", "--name", name, "claude", projectDir)) {
		t.Fatal("expected sandbox create command to be issued")
	}
	if !containsSandboxCall(probe.calls, "run:"+sandboxProbeKey("docker", "sandbox", "network", "proxy", name, "--policy", "deny",
		"--allow-host", "api.anthropic.com",
		"--allow-host", "claude.ai",
		"--allow-host", "platform.claude.com",
		"--allow-host", "statsig.anthropic.com",
		"--allow-host", "*.sentry.io",
		"--allow-host", "github.com",
		"--allow-host", "registry.npmjs.org")) {
		t.Fatal("expected sandbox policy command to be issued")
	}
	if !containsSandboxCall(probe.calls, "run:"+sandboxProbeKey("docker", "sandbox", "run", name, "--",
		"--dangerously-skip-permissions", "-p", "hi")) {
		t.Fatal("expected sandbox run command with forwarded Claude args")
	}

	updatedCfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after launch: %v", err)
	}
	if len(updatedCfg.ManagedSandboxes()) != 1 {
		t.Fatalf("expected 1 managed sandbox, got %d", len(updatedCfg.ManagedSandboxes()))
	}
	if updatedCfg.ManagedSandboxes()[0].Name != name {
		t.Fatalf("managed sandbox name = %q, want %q", updatedCfg.ManagedSandboxes()[0].Name, name)
	}
}

func TestRunSandboxClaudeSessionAutoDetectsAndRecordsBackend(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()
	savedApprovalsPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedApprovalsPath }()

	projectDir := t.TempDir()
	sessionCfg := sessionConfig{ProjectDir: projectDir}
	name := sandboxName("claude", projectDir, nil, sandboxPolicyProfileBaseline)
	if err := recordSandboxApproval(projectDir, sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatalf("recordSandboxApproval: %v", err)
	}

	probe := healthySandboxProbe()

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	if err := runSandboxClaudeSession(sessionCfg, []string{"-p", "hi"}); err != nil {
		t.Fatalf("runSandboxClaudeSession: %v", err)
	}

	updatedCfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after auto-detect launch: %v", err)
	}
	backend := updatedCfg.SandboxBackend()
	if backend == nil {
		t.Fatal("expected backend to be auto-recorded")
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
	if len(updatedCfg.ManagedSandboxes()) != 1 {
		t.Fatalf("expected 1 managed sandbox, got %d", len(updatedCfg.ManagedSandboxes()))
	}
	if updatedCfg.ManagedSandboxes()[0].Name != name {
		t.Fatalf("managed sandbox name = %q, want %q", updatedCfg.ManagedSandboxes()[0].Name, name)
	}
}

func TestRunSandboxExecSessionUsesShellSandbox(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()
	savedApprovalsPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedApprovalsPath }()

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
	name := sandboxName("shell", projectDir, nil, sandboxPolicyProfileBaseline)
	if err := recordSandboxApproval(projectDir, sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatalf("recordSandboxApproval: %v", err)
	}

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "version", "--format", "{{json .Server}}")] = fakeSandboxResult{
		output: `{"Platform":{"Name":"Docker Desktop 4.61.1 (123456)"}}`,
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

func TestRunSandboxClaudeSessionReadDirsWithinProjectSkipExtraWorkspaceGate(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()
	savedApprovalsPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedApprovalsPath }()

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

	projectDir := t.TempDir()
	projectChild := filepath.Join(projectDir, "child")
	if err := os.MkdirAll(projectChild, 0o755); err != nil {
		t.Fatalf("mkdir projectChild: %v", err)
	}
	sessionCfg := sessionConfig{
		ProjectDir: projectDir,
		ReadDirs:   []string{projectChild},
	}
	name := sandboxName("claude", projectDir, nil, sandboxPolicyProfileBaseline)
	if err := recordSandboxApproval(projectDir, sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatalf("recordSandboxApproval: %v", err)
	}

	probe := healthySandboxProbe()

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	if err := runSandboxClaudeSession(sessionCfg, nil); err != nil {
		t.Fatalf("runSandboxClaudeSession: %v", err)
	}

	if !containsSandboxCall(probe.calls, "run:"+sandboxProbeKey("docker", "sandbox", "create", "--name", name, "claude", projectDir)) {
		t.Fatal("expected sandbox create command without extra read-only mount")
	}
	if containsSandboxCall(probe.calls, "run:"+sandboxProbeKey("docker", "sandbox", "create", "--name", name, "claude", projectDir, projectChild+":ro")) {
		t.Fatal("did not expect project child to be mounted read-only")
	}
}

func TestRunSandboxClaudeSessionDoesNotRecordManagedSandboxWhenCreateFails(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()
	savedApprovalsPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedApprovalsPath }()

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
	name := sandboxName("claude", projectDir, nil, sandboxPolicyProfileBaseline)
	if err := recordSandboxApproval(projectDir, sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatalf("recordSandboxApproval: %v", err)
	}

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "version", "--format", "{{json .Server}}")] = fakeSandboxResult{
		output: `{"Platform":{"Name":"Docker Desktop 4.61.1 (123456)"}}`,
	}
	probe.runResults = map[string]fakeSandboxResult{
		sandboxProbeKey("docker", "sandbox", "create", "--name", name, "claude", projectDir): {
			output: "create failed",
			err:    errors.New("create failed"),
		},
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	err := runSandboxClaudeSession(sessionConfig{ProjectDir: projectDir}, nil)
	if err == nil {
		t.Fatal("expected launch to fail when sandbox create fails")
	}

	updatedCfg, cfgErr := loadConfig()
	if cfgErr != nil {
		t.Fatalf("loadConfig after failed launch: %v", cfgErr)
	}
	if len(updatedCfg.ManagedSandboxes()) != 0 {
		t.Fatalf("expected no managed sandboxes after failed create, got %d", len(updatedCfg.ManagedSandboxes()))
	}
}

func TestRunSandboxClaudeSessionRecreatesStoppedSandbox(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()
	savedApprovalsPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedApprovalsPath }()

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
	name := sandboxName("claude", projectDir, nil, sandboxPolicyProfileBaseline)
	if err := recordSandboxApproval(projectDir, sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatalf("recordSandboxApproval: %v", err)
	}

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "version", "--format", "{{json .Server}}")] = fakeSandboxResult{
		output: `{"Platform":{"Name":"Docker Desktop 4.61.1 (123456)"}}`,
	}
	probe.outputs[sandboxProbeKey("docker", "sandbox", "ls", "--json")] = fakeSandboxResult{
		output: fmt.Sprintf(`{"sandboxes":[{"name":%q,"status":"stopped"}]}`, name),
	}
	probe.outputs[sandboxProbeKey("docker", "sandbox", "rm", name)] = fakeSandboxResult{
		output: "removed",
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	if err := runSandboxClaudeSession(sessionConfig{ProjectDir: projectDir}, []string{"-p", "hi"}); err != nil {
		t.Fatalf("runSandboxClaudeSession: %v", err)
	}

	if !containsSandboxCall(probe.calls, "output:"+sandboxProbeKey("docker", "sandbox", "rm", name)) {
		t.Fatal("expected stopped sandbox to be removed before recreate")
	}
	if !containsSandboxCall(probe.calls, "run:"+sandboxProbeKey("docker", "sandbox", "create", "--name", name, "claude", projectDir)) {
		t.Fatal("expected sandbox create command after removing stopped sandbox")
	}
}

func TestRunSandboxClaudeSessionRecreatesStaleSandboxAfterRunNotFound(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()
	savedApprovalsPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedApprovalsPath }()

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
	name := sandboxName("claude", projectDir, nil, sandboxPolicyProfileBaseline)
	if err := recordSandboxApproval(projectDir, sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatalf("recordSandboxApproval: %v", err)
	}

	runKey := sandboxProbeKey("docker", "sandbox", "run", name, "--", "--dangerously-skip-permissions", "-p", "hi")

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "version", "--format", "{{json .Server}}")] = fakeSandboxResult{
		output: `{"Platform":{"Name":"Docker Desktop 4.61.1 (123456)"}}`,
	}
	probe.outputSeq = map[string][]fakeSandboxResult{
		sandboxProbeKey("docker", "sandbox", "ls", "--json"): {
			{output: fmt.Sprintf(`{"sandboxes":[{"name":%q,"status":"running"}]}`, name)},
			{output: `{"sandboxes":[]}`},
		},
	}
	probe.outputs[sandboxProbeKey("docker", "sandbox", "rm", name)] = fakeSandboxResult{
		output: "removed",
	}
	probe.runSeq = map[string][]fakeSandboxResult{
		runKey: {
			{output: fmt.Sprintf("no sandbox found in '%s'", name), err: errors.New("missing")},
			{},
		},
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	if err := runSandboxClaudeSession(sessionConfig{ProjectDir: projectDir}, []string{"-p", "hi"}); err != nil {
		t.Fatalf("runSandboxClaudeSession: %v", err)
	}

	if !containsSandboxCall(probe.calls, "output:"+sandboxProbeKey("docker", "sandbox", "rm", name)) {
		t.Fatal("expected stale sandbox to be removed before retry")
	}
	if !containsSandboxCall(probe.calls, "run:"+sandboxProbeKey("docker", "sandbox", "create", "--name", name, "claude", projectDir)) {
		t.Fatal("expected sandbox create command when retrying stale sandbox")
	}
	if got := countSandboxCall(probe.calls, "run:"+runKey); got != 2 {
		t.Fatalf("sandbox run call count = %d, want 2", got)
	}
}

func TestRunSandboxClaudeSessionCreateFailureHintsWhenDesktopStopped(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()
	savedApprovalsPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedApprovalsPath }()

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
	name := sandboxName("claude", projectDir, nil, sandboxPolicyProfileBaseline)
	if err := recordSandboxApproval(projectDir, sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatalf("recordSandboxApproval: %v", err)
	}

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "version", "--format", "{{json .Server}}")] = fakeSandboxResult{
		output: `{"Platform":{"Name":"Docker Desktop 4.61.1 (123456)"}}`,
	}
	probe.outputs[sandboxProbeKey("docker", "desktop", "status")] = fakeSandboxResult{
		output: "Name                Value\nStatus              stopped\n",
	}
	probe.runResults = map[string]fakeSandboxResult{
		sandboxProbeKey("docker", "sandbox", "create", "--name", name, "claude", projectDir): {
			output: "create failed",
			err:    errors.New("create failed"),
		},
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	err := runSandboxClaudeSession(sessionConfig{ProjectDir: projectDir}, nil)
	if err == nil {
		t.Fatal("expected launch to fail when sandbox create fails")
	}
	if !strings.Contains(err.Error(), "Docker Desktop stopped unexpectedly") {
		t.Fatalf("expected desktop stopped hint, got: %v", err)
	}
}

func TestRunSandboxClaudeSessionCreateFailureHintsClosedPipePrompt(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()
	savedApprovalsPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedApprovalsPath }()

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
	name := sandboxName("claude", projectDir, nil, sandboxPolicyProfileBaseline)
	if err := recordSandboxApproval(projectDir, sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatalf("recordSandboxApproval: %v", err)
	}

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "version", "--format", "{{json .Server}}")] = fakeSandboxResult{
		output: `{"Platform":{"Name":"Docker Desktop 4.61.1 (123456)"}}`,
	}
	probe.runResults = map[string]fakeSandboxResult{
		sandboxProbeKey("docker", "sandbox", "create", "--name", name, "claude", projectDir): {
			output: "service command exited with code 1: command exited with code 1: io: read/write on closed pipe",
			err:    errors.New("create failed"),
		},
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	err := runSandboxClaudeSession(sessionConfig{ProjectDir: projectDir}, nil)
	if err == nil {
		t.Fatal("expected launch to fail when sandbox create hits a closed-pipe engine failure")
	}
	if !strings.Contains(err.Error(), "click Allow and retry") {
		t.Fatalf("expected Docker Desktop closed-pipe hint, got: %v", err)
	}
}

func TestRunSandboxClaudeSessionNotLoggedInHintsSandboxAuthSetup(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()
	savedApprovalsPath := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	defer func() { sandboxApprovalsFilePath = savedApprovalsPath }()

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
	name := sandboxName("claude", projectDir, nil, sandboxPolicyProfileBaseline)
	if err := recordSandboxApproval(projectDir, sandboxBackendDockerSandboxes, sandboxPolicyProfileBaseline); err != nil {
		t.Fatalf("recordSandboxApproval: %v", err)
	}

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "version", "--format", "{{json .Server}}")] = fakeSandboxResult{
		output: `{"Platform":{"Name":"Docker Desktop 4.61.1 (123456)"}}`,
	}
	probe.outputs[sandboxProbeKey("docker", "sandbox", "ls", "--json")] = fakeSandboxResult{
		output: fmt.Sprintf(`{"sandboxes":[{"name":%q,"status":"running"}]}`, name),
	}
	probe.runResults = map[string]fakeSandboxResult{
		sandboxProbeKey("docker", "sandbox", "network", "proxy", name, "--policy", "deny",
			"--allow-host", "api.anthropic.com",
			"--allow-host", "claude.ai",
			"--allow-host", "platform.claude.com",
			"--allow-host", "statsig.anthropic.com",
			"--allow-host", "*.sentry.io",
			"--allow-host", "github.com",
			"--allow-host", "registry.npmjs.org"): {},
		sandboxProbeKey("docker", "sandbox", "run", name): {
			output: "Not logged in · Please run /login",
			err:    errors.New("agent exited"),
		},
		sandboxProbeKey("docker", "sandbox", "run", name, "--", "--dangerously-skip-permissions"): {
			output: "Not logged in · Please run /login",
			err:    errors.New("agent exited"),
		},
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	err := runSandboxClaudeSession(sessionConfig{ProjectDir: projectDir}, nil)
	if err == nil {
		t.Fatal("expected launch to fail when Claude is not logged in inside sandbox")
	}
	if !strings.Contains(err.Error(), "Claude is not authenticated in Docker Sandboxes") {
		t.Fatalf("expected sandbox auth hint, got: %v", err)
	}
}

func TestRunSandboxResetRemovesManagedSandboxes(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:          sandboxBackendDockerSandboxes,
		PolicyProfile: sandboxPolicyProfileBaseline,
	}
	cfg.Sandbox.Managed = []ManagedSandboxConfig{
		{
			Name:          "hazmat-claude-demo-123",
			BackendType:   sandboxBackendDockerSandboxes,
			Agent:         "claude",
			ProjectDir:    "/tmp/project",
			PolicyProfile: sandboxPolicyProfileBaseline,
		},
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "sandbox", "rm", "hazmat-claude-demo-123")] = fakeSandboxResult{
		output: "removed",
	}
	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	savedDryRun := flagDryRun
	flagDryRun = false
	defer func() { flagDryRun = savedDryRun }()

	savedYesAll := flagYesAll
	flagYesAll = true
	defer func() { flagYesAll = savedYesAll }()

	if err := runSandboxReset(); err != nil {
		t.Fatalf("runSandboxReset: %v", err)
	}
	if !containsSandboxCall(probe.calls, "output:"+sandboxProbeKey("docker", "sandbox", "rm", "hazmat-claude-demo-123")) {
		t.Fatal("expected sandbox rm command to be issued")
	}

	updatedCfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after reset: %v", err)
	}
	if updatedCfg.SandboxBackend() != nil {
		t.Fatal("expected backend to be cleared")
	}
	if len(updatedCfg.ManagedSandboxes()) != 0 {
		t.Fatalf("expected managed sandboxes to be cleared, got %d", len(updatedCfg.ManagedSandboxes()))
	}
}

func TestRunSandboxResetIgnoresMissingManagedSandbox(t *testing.T) {
	savedConfigPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	defer func() { configFilePath = savedConfigPath }()

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:          sandboxBackendDockerSandboxes,
		PolicyProfile: sandboxPolicyProfileBaseline,
	}
	cfg.Sandbox.Managed = []ManagedSandboxConfig{
		{
			Name:          "hazmat-claude-demo-123",
			BackendType:   sandboxBackendDockerSandboxes,
			Agent:         "claude",
			ProjectDir:    "/tmp/project",
			PolicyProfile: sandboxPolicyProfileBaseline,
		},
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	probe := healthySandboxProbe()
	probe.outputs[sandboxProbeKey("docker", "sandbox", "rm", "hazmat-claude-demo-123")] = fakeSandboxResult{
		output: "Error: sandbox not found",
		err:    fmt.Errorf("missing"),
	}
	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return probe }
	defer func() { sandboxProbeFactory = savedProbeFactory }()

	savedDryRun := flagDryRun
	flagDryRun = false
	defer func() { flagDryRun = savedDryRun }()

	savedYesAll := flagYesAll
	flagYesAll = true
	defer func() { flagYesAll = savedYesAll }()

	if err := runSandboxReset(); err != nil {
		t.Fatalf("runSandboxReset should ignore missing sandboxes: %v", err)
	}
}
