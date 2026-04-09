package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestDiscoverSSHKeyCandidatesReportsUsableEntries(t *testing.T) {
	isolateConfig(t)

	keyDir := writeSSHKeyDirectory(t, true)

	keys, err := discoverSSHKeyCandidates(keyDir)
	if err != nil {
		t.Fatalf("discoverSSHKeyCandidates: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("discoverSSHKeyCandidates len = %d, want 1", len(keys))
	}
	if keys[0].DisplayName() != "id_ed25519" || !keys[0].Usable() {
		t.Fatalf("usable key = %+v, want usable id_ed25519", keys[0])
	}
}

func TestDiscoverSSHKeyCandidatesReportsBrokenEntriesWithoutKnownHosts(t *testing.T) {
	isolateConfig(t)

	keyDir := writeSSHKeyDirectory(t, false)

	keys, err := discoverSSHKeyCandidates(keyDir)
	if err != nil {
		t.Fatalf("discoverSSHKeyCandidates: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("discoverSSHKeyCandidates len = %d, want 1", len(keys))
	}
	if keys[0].Status != "missing known_hosts" {
		t.Fatalf("broken key = %+v, want missing known_hosts", keys[0])
	}
}

func TestRunConfigSSHSetPersistsProjectConfigAndUnsetRemovesItWithoutTouchingKeyFile(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeSSHKeyDirectory(t, true)
	privateKeyPath, err := canonicalizeConfiguredFile(filepath.Join(keyDir, "id_ed25519"))
	if err != nil {
		t.Fatalf("canonicalizeConfiguredFile private key: %v", err)
	}
	knownHostsPath, err := canonicalizeConfiguredFile(filepath.Join(keyDir, "known_hosts"))
	if err != nil {
		t.Fatalf("canonicalizeConfiguredFile known_hosts: %v", err)
	}

	if err := runConfigSSHSet(projectDir, filepath.Join(keyDir, "id_ed25519")); err != nil {
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
	if got.Key != "" {
		t.Fatalf("ProjectSSH.Key = %q, want empty", got.Key)
	}
	if got.PrivateKeyPath != privateKeyPath {
		t.Fatalf("ProjectSSH.PrivateKeyPath = %q, want %q", got.PrivateKeyPath, privateKeyPath)
	}
	if got.KnownHostsPath != knownHostsPath {
		t.Fatalf("ProjectSSH.KnownHostsPath = %q, want %q", got.KnownHostsPath, knownHostsPath)
	}

	if err := runConfigSSHUnset(projectDir, ""); err != nil {
		t.Fatalf("runConfigSSHUnset: %v", err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after unset: %v", err)
	}
	if got := cfg.ProjectSSH(canonicalProjectDir); got != nil {
		t.Fatalf("ProjectSSH after unset = %+v, want nil", got)
	}
	if _, err := os.Stat(privateKeyPath); err != nil {
		t.Fatalf("private key should still exist after unset: %v", err)
	}
	if _, err := os.Stat(knownHostsPath); err != nil {
		t.Fatalf("known_hosts should still exist after unset: %v", err)
	}
}

func TestConfigSSHSetCommandUsesPositionalKeyPathInCurrentProject(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeSSHKeyDirectory(t, true)
	t.Chdir(projectDir)

	cmd := newConfigSSHCmd()
	cmd.SetArgs([]string{"set", filepath.Join(keyDir, "id_ed25519")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	canonicalProjectDir, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir project: %v", err)
	}
	projectCfg := cfg.ProjectSSH(canonicalProjectDir)
	if projectCfg == nil {
		t.Fatal("expected project SSH config")
	}
	if filepath.Base(projectCfg.PrivateKeyPath) != "id_ed25519" {
		t.Fatalf("PrivateKeyPath = %q, want id_ed25519", projectCfg.PrivateKeyPath)
	}
}

func TestConfigSSHUnsetCommandRemovesOnlyProjectConfig(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeSSHKeyDirectory(t, true)
	keyPath := filepath.Join(keyDir, "id_ed25519")
	knownHostsPath := filepath.Join(keyDir, "known_hosts")
	t.Chdir(projectDir)

	setCmd := newConfigSSHCmd()
	setCmd.SetArgs([]string{"set", keyPath})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("set cmd.Execute: %v", err)
	}

	unsetCmd := newConfigSSHCmd()
	unsetCmd.SetArgs([]string{"unset"})
	if err := unsetCmd.Execute(); err != nil {
		t.Fatalf("unset cmd.Execute: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	canonicalProjectDir, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir project: %v", err)
	}
	if projectCfg := cfg.ProjectSSH(canonicalProjectDir); projectCfg != nil {
		t.Fatalf("ProjectSSH after unset = %+v, want nil", projectCfg)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("private key should still exist after unset command: %v", err)
	}
	if _, err := os.Stat(knownHostsPath); err != nil {
		t.Fatalf("known_hosts should still exist after unset command: %v", err)
	}
}

func TestRunConfigSSHUnsetRejectsMismatchedKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeSSHKeyDirectory(t, true)
	if err := runConfigSSHSet(projectDir, filepath.Join(keyDir, "id_ed25519")); err != nil {
		t.Fatalf("runConfigSSHSet: %v", err)
	}

	err := runConfigSSHUnset(projectDir, "other_key")
	if err == nil {
		t.Fatal("expected mismatched unset key to be rejected")
	}
	if !strings.Contains(err.Error(), "does not match the current project assignment") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunConfigSSHSetRejectsUnknownKey(t *testing.T) {
	isolateConfig(t)

	keyDir := writeSSHKeyDirectory(t, true)

	err := runConfigSSHSet(t.TempDir(), filepath.Join(keyDir, "missing-key"))
	if err == nil {
		t.Fatal("expected unknown key to be rejected")
	}
	if !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunConfigSSHSetRejectsPublicKeyPath(t *testing.T) {
	isolateConfig(t)

	keyDir := writeSSHKeyDirectory(t, true)

	err := runConfigSSHSet(t.TempDir(), filepath.Join(keyDir, "id_ed25519.pub"))
	if err == nil {
		t.Fatal("expected public key path to be rejected")
	}
	if !strings.Contains(err.Error(), "looks like a public key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunConfigSSHSetAcceptsNonDefaultKeyNames(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "deploy_key", true)

	if err := runConfigSSHSet(projectDir, filepath.Join(keyDir, "deploy_key")); err != nil {
		t.Fatalf("runConfigSSHSet: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	canonicalProjectDir, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir project: %v", err)
	}
	projectCfg := cfg.ProjectSSH(canonicalProjectDir)
	if projectCfg == nil {
		t.Fatal("expected project SSH config")
	}
	if filepath.Base(projectCfg.PrivateKeyPath) != "deploy_key" {
		t.Fatalf("PrivateKeyPath = %q, want deploy_key", projectCfg.PrivateKeyPath)
	}
}

func TestConfigSSHSetCommandRejectsPublicKeyPath(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeSSHKeyDirectory(t, true)
	t.Chdir(projectDir)

	cmd := newConfigSSHCmd()
	cmd.SetArgs([]string{"set", filepath.Join(keyDir, "id_ed25519.pub")})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected public key path to be rejected")
	}
	if !strings.Contains(err.Error(), "looks like a public key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompleteSSHSetKeyArgsSuggestsPrivateKeysFromDefaultSSHDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sshDir, err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "github_rsa"), []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "github_rsa.pub"), []byte("ssh-rsa AAAA"), 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte("github.com ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	got, directive := completeSSHSetKeyArgs(nil, nil, "git")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want %v", directive, cobra.ShellCompDirectiveNoFileComp)
	}
	if !slices.Equal(got, []string{"github_rsa"}) {
		t.Fatalf("completeSSHSetKeyArgs = %v, want [github_rsa]", got)
	}
}

func TestCompleteSSHSetKeyArgsSuggestsPathScopedKeys(t *testing.T) {
	keyDir := writeNamedSSHKeyDirectory(t, "deploy_key", true)

	got, _ := completeSSHSetKeyArgs(nil, nil, filepath.Join(keyDir, "dep"))
	want := []string{filepath.Join(keyDir, "deploy_key")}
	if !slices.Equal(got, want) {
		t.Fatalf("completeSSHSetKeyArgs = %v, want %v", got, want)
	}
}

func TestCompleteSSHUnsetKeyArgsSuggestsCurrentProjectKey(t *testing.T) {
	isolateConfig(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sshDir, err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519.pub"), []byte("ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte("github.com ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	projectDir := t.TempDir()
	if err := runConfigSSHSet(projectDir, filepath.Join(sshDir, "id_ed25519")); err != nil {
		t.Fatalf("runConfigSSHSet: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("project", projectDir, "")
	got, directive := completeSSHUnsetKeyArgs(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want %v", directive, cobra.ShellCompDirectiveNoFileComp)
	}
	if !slices.Equal(got, []string{"id_ed25519"}) {
		t.Fatalf("completeSSHUnsetKeyArgs = %v, want [id_ed25519]", got)
	}
}

func TestCompleteSSHUnsetKeyArgsSuggestsConfiguredPathForPathPrefix(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "deploy_key", true)
	keyPath := filepath.Join(keyDir, "deploy_key")
	canonicalKeyPath, err := canonicalizeConfiguredFile(keyPath)
	if err != nil {
		t.Fatalf("canonicalizeConfiguredFile key: %v", err)
	}
	if err := runConfigSSHSet(projectDir, keyPath); err != nil {
		t.Fatalf("runConfigSSHSet: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("project", projectDir, "")
	got, _ := completeSSHUnsetKeyArgs(cmd, nil, filepath.Join(keyDir, "dep"))
	if !slices.Equal(got, []string{canonicalKeyPath}) {
		t.Fatalf("completeSSHUnsetKeyArgs = %v, want [%s]", got, canonicalKeyPath)
	}
}

func TestResolveManagedGitSSHUsesSelectedConfiguredKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeSSHKeyDirectory(t, true)
	keyPath, err := canonicalizeConfiguredFile(filepath.Join(keyDir, "id_ed25519"))
	if err != nil {
		t.Fatalf("canonicalizeConfiguredFile key: %v", err)
	}
	knownHostsPath, err := canonicalizeConfiguredFile(filepath.Join(keyDir, "known_hosts"))
	if err != nil {
		t.Fatalf("canonicalizeConfiguredFile known_hosts: %v", err)
	}
	if err := runConfigSSHSet(projectDir, filepath.Join(keyDir, "id_ed25519")); err != nil {
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
	if got.DisplayName != "id_ed25519" {
		t.Fatalf("DisplayName = %q, want id_ed25519", got.DisplayName)
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
	if !strings.Contains(got.SessionNote, "selected key") {
		t.Fatalf("SessionNote = %q, want selected key note", got.SessionNote)
	}
}

func TestResolveManagedGitSSHRejectsVisibleSelectedPrivateKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeSSHKeyDirectory(t, true)
	if err := runConfigSSHSet(projectDir, filepath.Join(keyDir, "id_ed25519")); err != nil {
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

	got, err = normalizeGitSSHTestHost("git@github.com:dredozubov/hazmat.git")
	if err != nil {
		t.Fatalf("normalizeGitSSHTestHost scp remote: %v", err)
	}
	if got != "github.com" {
		t.Fatalf("normalizeGitSSHTestHost scp remote = %q, want github.com", got)
	}

	got, err = normalizeGitSSHTestHost("ssh://git@github.com/dredozubov/hazmat.git")
	if err != nil {
		t.Fatalf("normalizeGitSSHTestHost ssh url: %v", err)
	}
	if got != "github.com" {
		t.Fatalf("normalizeGitSSHTestHost ssh url = %q, want github.com", got)
	}

	if _, err := normalizeGitSSHTestHost("bad host value"); err == nil {
		t.Fatal("expected invalid host to be rejected")
	}
}

func TestNewGitSSHProbeCommandRunsAsAgentUser(t *testing.T) {
	cmd := newGitSSHProbeCommand("/tmp/agent.sock", "/tmp/known_hosts", "github.com")

	wantFragments := []string{
		"sudo",
		"-u",
		agentUser,
		"env",
		"HOME=" + agentHome,
		"USER=" + agentUser,
		"LOGNAME=" + agentUser,
		"SSH_ASKPASS_REQUIRE=never",
		"/usr/bin/ssh",
		"-o",
		"IdentityFile=none",
		"-o",
		"IdentityAgent=/tmp/agent.sock",
		"-o",
		"UserKnownHostsFile=/tmp/known_hosts",
		"git@github.com",
	}
	for _, fragment := range wantFragments {
		if !slices.Contains(cmd.Args, fragment) {
			t.Fatalf("probe command args = %v, want fragment %q", cmd.Args, fragment)
		}
	}
	if slices.Contains(cmd.Args, "IdentitiesOnly=yes") {
		t.Fatalf("probe command args = %v, should not force IdentitiesOnly=yes", cmd.Args)
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
		"-o IdentityFile=none",
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
	if strings.Contains(script, "IdentitiesOnly=yes") {
		t.Fatalf("wrapper script should not force IdentitiesOnly=yes:\n%s", script)
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
	keyDir := writeSSHKeyDirectory(t, true)
	if err := runConfigSSHSet(projectDir, filepath.Join(keyDir, "id_ed25519")); err != nil {
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
		if strings.Contains(note, "selected key") && strings.Contains(note, "id_ed25519") {
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
	keyDir := writeSSHKeyDirectory(t, true)
	if err := runConfigSSHSet(projectDir, filepath.Join(keyDir, "id_ed25519")); err != nil {
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

func writeSSHKeyDirectory(t *testing.T, includeKnownHosts bool) string {
	return writeNamedSSHKeyDirectory(t, "id_ed25519", includeKnownHosts)
}

func writeNamedSSHKeyDirectory(t *testing.T, keyName string, includeKnownHosts bool) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, keyName), []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, keyName+".pub"), []byte("ssh-ed25519 AAAA test@hazmat"), 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
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
