package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverProvisionedSSHKeysReportsUsableAndBrokenEntries(t *testing.T) {
	isolateConfig(t)

	writeProvisionedSSHKey(t, "github-work", true)
	writeProvisionedSSHKey(t, "broken", false)

	keys, err := discoverProvisionedSSHKeys()
	if err != nil {
		t.Fatalf("discoverProvisionedSSHKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("discoverProvisionedSSHKeys len = %d, want 2", len(keys))
	}
	if keys[0].Name != "broken" || keys[0].Status != "missing known_hosts" {
		t.Fatalf("broken key = %+v, want missing known_hosts", keys[0])
	}
	if keys[1].Name != "github-work" || !keys[1].Usable() {
		t.Fatalf("usable key = %+v, want usable github-work", keys[1])
	}
}

func TestRunConfigSSHSetPersistsProjectConfigAndClearRemovesIt(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	writeProvisionedSSHKey(t, "github-work", true)

	if err := runConfigSSHSet(projectDir, "github-work"); err != nil {
		t.Fatalf("runConfigSSHSet: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after set: %v", err)
	}
	canonicalProjectDir, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir project: %v", err)
	}
	got := cfg.ProjectSSH(canonicalProjectDir)
	if got == nil {
		t.Fatal("ProjectSSH should be configured")
	}
	if got.Key != "github-work" {
		t.Fatalf("ProjectSSH.Key = %q, want github-work", got.Key)
	}

	if err := runConfigSSHClear(projectDir); err != nil {
		t.Fatalf("runConfigSSHClear: %v", err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after clear: %v", err)
	}
	if got := cfg.ProjectSSH(canonicalProjectDir); got != nil {
		t.Fatalf("ProjectSSH after clear = %+v, want nil", got)
	}
}

func TestRunConfigSSHSetRejectsUnknownKey(t *testing.T) {
	isolateConfig(t)

	writeProvisionedSSHKey(t, "github-work", true)

	err := runConfigSSHSet(t.TempDir(), "missing-key")
	if err == nil {
		t.Fatal("expected unknown key to be rejected")
	}
	if !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveManagedGitSSHUsesSelectedProvisionedKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeProvisionedSSHKey(t, "github-work", true)
	keyPath, err := canonicalizeConfiguredFile(filepath.Join(keyDir, "id_ed25519"))
	if err != nil {
		t.Fatalf("canonicalizeConfiguredFile key: %v", err)
	}
	knownHostsPath, err := canonicalizeConfiguredFile(filepath.Join(keyDir, "known_hosts"))
	if err != nil {
		t.Fatalf("canonicalizeConfiguredFile known_hosts: %v", err)
	}
	if err := runConfigSSHSet(projectDir, "github-work"); err != nil {
		t.Fatalf("runConfigSSHSet: %v", err)
	}

	cfg, err := resolveSessionConfig(projectDir, nil, nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	got, err := resolveManagedGitSSH(cfg)
	if err != nil {
		t.Fatalf("resolveManagedGitSSH: %v", err)
	}
	if got == nil {
		t.Fatal("expected managed Git SSH config")
	}
	if got.DisplayName != "github-work" {
		t.Fatalf("DisplayName = %q, want github-work", got.DisplayName)
	}
	if got.PrivateKeyPath != keyPath {
		t.Fatalf("PrivateKeyPath = %q, want %q", got.PrivateKeyPath, keyPath)
	}
	if got.KnownHostsPath != knownHostsPath {
		t.Fatalf("KnownHostsPath = %q, want %q", got.KnownHostsPath, knownHostsPath)
	}
	if len(got.AllowedHosts) != 0 {
		t.Fatalf("AllowedHosts = %v, want none", got.AllowedHosts)
	}
	if !strings.Contains(got.SessionNote, "provisioned key") {
		t.Fatalf("SessionNote = %q, want provisioned key note", got.SessionNote)
	}
}

func TestResolveManagedGitSSHRejectsVisibleSelectedPrivateKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeProvisionedSSHKey(t, "github-work", true)
	if err := runConfigSSHSet(projectDir, "github-work"); err != nil {
		t.Fatalf("runConfigSSHSet: %v", err)
	}

	cfg, err := resolveSessionConfig(projectDir, []string{keyDir}, nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	_, err = resolveManagedGitSSH(cfg)
	if err == nil {
		t.Fatal("expected visible private key to be rejected")
	}
	if !strings.Contains(err.Error(), "visible inside the session contract") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeGitSSHTestHost(t *testing.T) {
	got, err := normalizeGitSSHTestHost("git@GitHub.com")
	if err != nil {
		t.Fatalf("normalizeGitSSHTestHost: %v", err)
	}
	if got != "github.com" {
		t.Fatalf("normalizeGitSSHTestHost = %q, want github.com", got)
	}

	if _, err := normalizeGitSSHTestHost("github.com:2222"); err == nil {
		t.Fatal("expected host with port to be rejected")
	}
}

func TestInterpretGitSSHProbeResultRecognizesAuthenticatedGitErrors(t *testing.T) {
	if err := interpretGitSSHProbeResult("github.com", "ERROR: Repository not found.", assertErr{}); err != nil {
		t.Fatalf("authenticated git error should count as success: %v", err)
	}
	err := interpretGitSSHProbeResult("github.com", "Permission denied (publickey).", assertErr{})
	if err == nil {
		t.Fatal("expected permission denied to fail")
	}
}

func TestBuildGitSSHWrapperScriptWithoutAllowlistSkipsHostRestriction(t *testing.T) {
	script := buildGitSSHWrapperScript("/tmp/agent.sock", "/tmp/known_hosts", nil)
	for _, fragment := range []string{
		"interactive ssh is not allowed",
		"git-upload-pack*|git-receive-pack*|git-upload-archive*",
		"-o IdentityAgent=/tmp/agent.sock",
		"-o UserKnownHostsFile=/tmp/known_hosts",
		"-o StrictHostKeyChecking=yes",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("wrapper script missing %q:\n%s", fragment, script)
		}
	}
	if strings.Contains(script, "destination host not allowed") {
		t.Fatalf("wrapper script should not enforce host allowlist:\n%s", script)
	}
}

func TestParseSSHAgentPID(t *testing.T) {
	pid, err := parseSSHAgentPID("SSH_AUTH_SOCK=/tmp/agent.sock; export SSH_AUTH_SOCK;\nSSH_AGENT_PID=4242; export SSH_AGENT_PID;\n")
	if err != nil {
		t.Fatalf("parseSSHAgentPID: %v", err)
	}
	if pid != "4242" {
		t.Fatalf("parseSSHAgentPID = %q, want 4242", pid)
	}
}

func TestResolvePreparedSessionAddsManagedGitSSHNotes(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	projectDir := t.TempDir()
	writeProvisionedSSHKey(t, "github-work", true)
	if err := runConfigSSHSet(projectDir, "github-work"); err != nil {
		t.Fatalf("runConfigSSHSet: %v", err)
	}

	prepared, err := resolvePreparedSession("shell", harnessSessionOpts{project: projectDir}, true)
	if err != nil {
		t.Fatalf("resolvePreparedSession: %v", err)
	}
	if prepared.Config.GitSSH == nil {
		t.Fatal("prepared session missing managed Git SSH config")
	}
	if len(prepared.Config.ServiceAccess) != 1 || prepared.Config.ServiceAccess[0] != "git+ssh" {
		t.Fatalf("ServiceAccess = %v, want [git+ssh]", prepared.Config.ServiceAccess)
	}
	found := false
	for _, note := range prepared.Config.SessionNotes {
		if strings.Contains(note, "provisioned key") && strings.Contains(note, "github-work") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("SessionNotes = %v, want managed Git SSH note", prepared.Config.SessionNotes)
	}
}

func TestResolvePreparedSessionRejectsManagedGitSSHForSandboxMode(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	projectDir := t.TempDir()
	writeProvisionedSSHKey(t, "github-work", true)
	if err := runConfigSSHSet(projectDir, "github-work"); err != nil {
		t.Fatalf("runConfigSSHSet: %v", err)
	}

	_, err := resolvePreparedSession("claude", harnessSessionOpts{
		project:            projectDir,
		dockerMode:         string(dockerModeSandbox),
		dockerModeExplicit: true,
	}, true)
	if err == nil {
		t.Fatal("expected sandbox mode to reject managed Git SSH")
	}
	if !strings.Contains(err.Error(), "managed Git SSH is not supported for Docker Sandbox sessions yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeProvisionedSSHKey(t *testing.T, name string, includeKnownHosts bool) string {
	t.Helper()

	dir := filepath.Join(provisionedSSHKeysRootDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if includeKnownHosts {
		if err := os.WriteFile(filepath.Join(dir, "known_hosts"), []byte("github.com ssh-ed25519 AAAA"), 0o600); err != nil {
			t.Fatalf("write known_hosts: %v", err)
		}
	}
	return dir
}

type assertErr struct{}

func (assertErr) Error() string {
	return "probe failed"
}
