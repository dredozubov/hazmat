package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunConfigGitSSHPersistsProjectConfig(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyPath := filepath.Join(t.TempDir(), "repo_ed25519")
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(keyPath, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(knownHostsPath, []byte("github.com ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	if err := runConfigGitSSH(projectDir, keyPath, knownHostsPath, []string{"GitHub.com", "github.com"}, false); err != nil {
		t.Fatalf("runConfigGitSSH enable: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after enable: %v", err)
	}
	canonicalProjectDir, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir project: %v", err)
	}
	got := cfg.ProjectGitSSH(canonicalProjectDir)
	if got == nil {
		t.Fatal("ProjectGitSSH should be configured")
	}
	canonicalKeyPath, err := canonicalizeConfiguredFile(keyPath)
	if err != nil {
		t.Fatalf("canonicalizeConfiguredFile key: %v", err)
	}
	canonicalKnownHostsPath, err := canonicalizeConfiguredFile(knownHostsPath)
	if err != nil {
		t.Fatalf("canonicalizeConfiguredFile known_hosts: %v", err)
	}
	if got.PrivateKeyPath != canonicalKeyPath {
		t.Fatalf("PrivateKeyPath = %q, want %q", got.PrivateKeyPath, canonicalKeyPath)
	}
	if got.KnownHostsPath != canonicalKnownHostsPath {
		t.Fatalf("KnownHostsPath = %q, want %q", got.KnownHostsPath, canonicalKnownHostsPath)
	}
	if len(got.AllowedHosts) != 1 || got.AllowedHosts[0] != "github.com" {
		t.Fatalf("AllowedHosts = %v, want [github.com]", got.AllowedHosts)
	}

	if err := runConfigGitSSH(projectDir, "", "", nil, true); err != nil {
		t.Fatalf("runConfigGitSSH disable: %v", err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after disable: %v", err)
	}
	if got := cfg.ProjectGitSSH(canonicalProjectDir); got != nil {
		t.Fatalf("ProjectGitSSH after disable = %+v, want nil", got)
	}
}

func TestRunConfigGitSSHRejectsPrivateKeyInsideProject(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyPath := filepath.Join(projectDir, "repo_ed25519")
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(keyPath, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(knownHostsPath, []byte("github.com ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	err := runConfigGitSSH(projectDir, keyPath, knownHostsPath, []string{"github.com"}, false)
	if err == nil {
		t.Fatal("expected key inside project to be rejected")
	}
	if !strings.Contains(err.Error(), "outside the project directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveManagedGitSSHRejectsPrivateKeyVisibleViaReadDir(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "repo_ed25519")
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(keyPath, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(knownHostsPath, []byte("github.com ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	if err := runConfigGitSSH(projectDir, keyPath, knownHostsPath, []string{"github.com"}, false); err != nil {
		t.Fatalf("runConfigGitSSH enable: %v", err)
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

func TestNormalizeGitSSHHosts(t *testing.T) {
	got, err := normalizeGitSSHHosts([]string{"GitHub.com", "github.com", "gitlab.com"})
	if err != nil {
		t.Fatalf("normalizeGitSSHHosts: %v", err)
	}
	want := []string{"github.com", "gitlab.com"}
	if len(got) != len(want) {
		t.Fatalf("normalizeGitSSHHosts len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeGitSSHHosts = %v, want %v", got, want)
		}
	}

	if _, err := normalizeGitSSHHosts([]string{"github.com:2222"}); err == nil {
		t.Fatal("expected host with port to be rejected")
	}
}

func TestBuildGitSSHWrapperScriptIncludesConstraints(t *testing.T) {
	script := buildGitSSHWrapperScript("/tmp/agent.sock", "/tmp/known_hosts", []string{"github.com"})
	for _, fragment := range []string{
		"destination host not allowed",
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
	keyPath := filepath.Join(t.TempDir(), "repo_ed25519")
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(keyPath, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(knownHostsPath, []byte("github.com ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	if err := runConfigGitSSH(projectDir, keyPath, knownHostsPath, []string{"github.com"}, false); err != nil {
		t.Fatalf("runConfigGitSSH enable: %v", err)
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
		if strings.Contains(note, "Managed Git SSH enabled") {
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
	keyPath := filepath.Join(t.TempDir(), "repo_ed25519")
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(keyPath, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(knownHostsPath, []byte("github.com ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	if err := runConfigGitSSH(projectDir, keyPath, knownHostsPath, []string{"github.com"}, false); err != nil {
		t.Fatalf("runConfigGitSSH enable: %v", err)
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
