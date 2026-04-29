package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	os.Stdout = old
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(data), runErr
}

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

func TestConfigSSHUnsetCommandRemovesOnlyProjectConfig(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeSSHKeyDirectory(t, true)
	keyPath := filepath.Join(keyDir, "id_ed25519")
	knownHostsPath := filepath.Join(keyDir, "known_hosts")
	t.Chdir(projectDir)

	if err := runConfigSSHAdd(projectDir, "id_ed25519", []string{"github.com"}, "", "", keyPath); err != nil {
		t.Fatalf("runConfigSSHAdd: %v", err)
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
	if err := runConfigSSHAdd(projectDir, "id_ed25519", []string{"github.com"}, "", "", filepath.Join(keyDir, "id_ed25519")); err != nil {
		t.Fatalf("runConfigSSHAdd: %v", err)
	}

	err := runConfigSSHUnset(projectDir, "other_key")
	if err == nil {
		t.Fatal("expected mismatched unset key to be rejected")
	}
	if !strings.Contains(err.Error(), "does not match the current project assignment") {
		t.Fatalf("unexpected error: %v", err)
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
	if err := runConfigSSHAdd(projectDir, "id_ed25519", []string{"github.com"}, "", "", filepath.Join(sshDir, "id_ed25519")); err != nil {
		t.Fatalf("runConfigSSHAdd: %v", err)
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
	if err := runConfigSSHAdd(projectDir, "deploy_key", []string{"github.com"}, "", "", keyPath); err != nil {
		t.Fatalf("runConfigSSHAdd: %v", err)
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
	if err := runConfigSSHAdd(projectDir, "id_ed25519", []string{"github.com"}, "", "", filepath.Join(keyDir, "id_ed25519")); err != nil {
		t.Fatalf("runConfigSSHAdd: %v", err)
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
	if len(got.Keys) != 1 {
		t.Fatalf("Keys len = %d, want 1", len(got.Keys))
	}
	if got.Keys[0].Name != "id_ed25519" {
		t.Fatalf("Keys[0].Name = %q, want id_ed25519", got.Keys[0].Name)
	}
	if got.Keys[0].Identity.CredentialID != credentialGitSSHExternalIdentity {
		t.Fatalf("Keys[0].Identity.CredentialID = %q, want %q", got.Keys[0].Identity.CredentialID, credentialGitSSHExternalIdentity)
	}
	if got.Keys[0].Identity.Source != gitSSHIdentitySourceExternalFile {
		t.Fatalf("Keys[0].Identity.Source = %q, want %q", got.Keys[0].Identity.Source, gitSSHIdentitySourceExternalFile)
	}
	if got.Keys[0].Identity.PrivateKeyPath != keyPath {
		t.Fatalf("Keys[0].Identity.PrivateKeyPath = %q, want %q", got.Keys[0].Identity.PrivateKeyPath, keyPath)
	}
	if got.Keys[0].Identity.KnownHostsPath != knownHostsPath {
		t.Fatalf("Keys[0].Identity.KnownHostsPath = %q, want %q", got.Keys[0].Identity.KnownHostsPath, knownHostsPath)
	}
	if !slices.Equal(got.Keys[0].AllowedHosts, []string{"github.com"}) {
		t.Fatalf("Keys[0].AllowedHosts = %v, want [github.com]", got.Keys[0].AllowedHosts)
	}
	if !strings.Contains(got.SessionNote, "selected key") {
		t.Fatalf("SessionNote = %q, want selected key note", got.SessionNote)
	}
}

func TestResolveManagedGitSSHUsesProfileIdentityAndInheritsDefaultHosts(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "shared_key", true)
	canonicalKey, _ := canonicalizeConfiguredFile(filepath.Join(keyDir, "shared_key"))

	cfg := HazmatConfig{
		SSHProfiles: map[string]SSHProfile{
			"shared": {
				PrivateKeyPath: filepath.Join(keyDir, "shared_key"),
				KnownHostsPath: filepath.Join(keyDir, "known_hosts"),
				DefaultHosts:   []string{"github.com"},
			},
		},
		Projects: map[string]ProjectConfig{},
	}
	canonicalProjectDir, _ := resolveDir(projectDir, false)
	cfg.Projects[canonicalProjectDir] = ProjectConfig{
		SSH: &ProjectSSHConfig{
			Keys: []ProjectSSHKey{{Name: "via_shared", Profile: "shared"}}, // no declared hosts
		},
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	sess, err := resolveSessionConfig(projectDir, nil, nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	got, err := resolveManagedGitSSH(sess)
	if err != nil {
		t.Fatalf("resolveManagedGitSSH: %v", err)
	}
	if got == nil {
		t.Fatal("expected managed Git SSH config")
	}
	if len(got.Keys) != 1 {
		t.Fatalf("Keys len = %d, want 1", len(got.Keys))
	}
	if got.Keys[0].Identity.CredentialID != credentialGitSSHExternalIdentity {
		t.Fatalf("Keys[0].Identity.CredentialID = %q, want %q", got.Keys[0].Identity.CredentialID, credentialGitSSHExternalIdentity)
	}
	if got.Keys[0].Identity.Source != gitSSHIdentitySourceExternalFile {
		t.Fatalf("Keys[0].Identity.Source = %q, want %q", got.Keys[0].Identity.Source, gitSSHIdentitySourceExternalFile)
	}
	if got.Keys[0].Identity.PrivateKeyPath != canonicalKey {
		t.Fatalf("Keys[0].Identity.PrivateKeyPath = %q, want %q (profile identity)", got.Keys[0].Identity.PrivateKeyPath, canonicalKey)
	}
	if !slices.Equal(got.Keys[0].AllowedHosts, []string{"github.com"}) {
		t.Fatalf("Keys[0].AllowedHosts = %v, want [github.com] (inherited from profile)", got.Keys[0].AllowedHosts)
	}
}

func TestResolveManagedGitSSHUsesProvisionedIdentityReference(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	key := writeProvisionedSSHKeyDirectory(t, "github-bot")
	if err := runConfigSSHAdd(projectDir, "github", []string{"github.com"}, "github-bot", "", ""); err != nil {
		t.Fatalf("runConfigSSHAdd --inventory: %v", err)
	}

	cfg, err := resolveSessionConfig(projectDir, nil, nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	got, err := resolveManagedGitSSH(cfg)
	if err != nil {
		t.Fatalf("resolveManagedGitSSH: %v", err)
	}
	if got == nil || len(got.Keys) != 1 {
		t.Fatalf("managed Git SSH config = %+v, want one key", got)
	}
	identity := got.Keys[0].Identity
	if identity.CredentialID != credentialGitSSHProvisionedIdentity {
		t.Fatalf("CredentialID = %q, want %q", identity.CredentialID, credentialGitSSHProvisionedIdentity)
	}
	if identity.Source != gitSSHIdentitySourceProvisionedKeyRoot {
		t.Fatalf("Source = %q, want %q", identity.Source, gitSSHIdentitySourceProvisionedKeyRoot)
	}
	if identity.PrivateKeyPath != key.PrivateKeyPath || identity.KnownHostsPath != key.KnownHostsPath {
		t.Fatalf("identity = %+v, want provisioned key %+v", identity, key)
	}
	root, err := canonicalizePath(provisionedSSHKeysRootDir())
	if err != nil {
		t.Fatalf("canonicalize provisioned root: %v", err)
	}
	if !isWithinDir(root, identity.PrivateKeyPath) {
		t.Fatalf("provisioned private key %q is outside %q", identity.PrivateKeyPath, root)
	}
}

func TestResolveManagedGitSSHRejectsDanglingProfile(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()

	cfg := HazmatConfig{
		Projects: map[string]ProjectConfig{},
	}
	canonicalProjectDir, _ := resolveDir(projectDir, false)
	cfg.Projects[canonicalProjectDir] = ProjectConfig{
		SSH: &ProjectSSHConfig{
			Keys: []ProjectSSHKey{{Name: "x", Profile: "ghost", Hosts: []string{"github.com"}}},
		},
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	// loadConfig itself should reject the dangling reference.
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "not defined in ssh_profiles") {
		t.Fatalf("loadConfig should reject dangling ref, got %v", err)
	}
}

func TestResolveManagedGitSSHProjectDeclaredHostsOverrideProfileDefaults(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "shared_key", true)

	cfg := HazmatConfig{
		SSHProfiles: map[string]SSHProfile{
			"shared": {
				PrivateKeyPath: filepath.Join(keyDir, "shared_key"),
				KnownHostsPath: filepath.Join(keyDir, "known_hosts"),
				DefaultHosts:   []string{"github.com"},
			},
		},
		Projects: map[string]ProjectConfig{},
	}
	canonicalProjectDir, _ := resolveDir(projectDir, false)
	cfg.Projects[canonicalProjectDir] = ProjectConfig{
		SSH: &ProjectSSHConfig{
			Keys: []ProjectSSHKey{{
				Name:    "scoped",
				Profile: "shared",
				Hosts:   []string{"enterprise.internal"}, // overrides profile's default github.com
			}},
		},
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	sess, err := resolveSessionConfig(projectDir, nil, nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	got, err := resolveManagedGitSSH(sess)
	if err != nil {
		t.Fatalf("resolveManagedGitSSH: %v", err)
	}
	if !slices.Equal(got.Keys[0].AllowedHosts, []string{"enterprise.internal"}) {
		t.Fatalf("Keys[0].AllowedHosts = %v, want [enterprise.internal] (declared override)", got.Keys[0].AllowedHosts)
	}
}

func TestResolveManagedGitSSHRejectsVisibleSelectedPrivateKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeSSHKeyDirectory(t, true)
	if err := runConfigSSHAdd(projectDir, "id_ed25519", []string{"github.com"}, "", "", filepath.Join(keyDir, "id_ed25519")); err != nil {
		t.Fatalf("runConfigSSHAdd: %v", err)
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

func TestNewGitSSHProbeCommandUsesHostUserSSH(t *testing.T) {
	cmd := newGitSSHProbeCommand("/tmp/id_rsa", "/tmp/known_hosts", gitSSHTestTarget{
		RequestedHost: "github.com",
	})

	wantFragments := []string{
		"/usr/bin/ssh",
		"-o",
		"IdentitiesOnly=yes",
		"-o",
		"IdentityAgent=none",
		"-o",
		"PasswordAuthentication=no",
		"-o",
		"NumberOfPasswordPrompts=0",
		"-o",
		"UserKnownHostsFile=/tmp/known_hosts",
		"-i",
		"/tmp/id_rsa",
		"github.com",
	}
	for _, fragment := range wantFragments {
		if !slices.Contains(cmd.Args, fragment) {
			t.Fatalf("probe command args = %v, want fragment %q", cmd.Args, fragment)
		}
	}
	if slices.Contains(cmd.Args, "sudo") {
		t.Fatalf("probe command args = %v, should not run through sudo", cmd.Args)
	}
}

func TestNewGitSSHProbeCommandUsesExplicitUserAndPortFromInput(t *testing.T) {
	cmd := newGitSSHProbeCommand("/tmp/id_rsa", "/tmp/known_hosts", gitSSHTestTarget{
		RequestedHost: "openclaw-1",
		InputUser:     "deploy",
		InputPort:     "2222",
	})

	wantFragments := []string{
		"-l",
		"deploy",
		"-p",
		"2222",
		"openclaw-1",
	}
	for _, fragment := range wantFragments {
		if !slices.Contains(cmd.Args, fragment) {
			t.Fatalf("probe command args = %v, want fragment %q", cmd.Args, fragment)
		}
	}
}

func TestResolveGitSSHTestTargetUsesSSHConfigAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sshDir, err)
	}
	config := "Host openclaw-1\n  HostName bastion.example.com\n  User deploy\n  Port 2222\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	target, err := resolveGitSSHTestTarget("openclaw-1")
	if err != nil {
		t.Fatalf("resolveGitSSHTestTarget: %v", err)
	}
	if target.RequestedHost != "openclaw-1" {
		t.Fatalf("RequestedHost = %q, want openclaw-1", target.RequestedHost)
	}
	if target.Host != "bastion.example.com" {
		t.Fatalf("Host = %q, want bastion.example.com", target.Host)
	}
	if target.User != "deploy" {
		t.Fatalf("User = %q, want deploy", target.User)
	}
	if target.Port != "2222" {
		t.Fatalf("Port = %q, want 2222", target.Port)
	}
	if target.HostKeyAlias != "openclaw-1" {
		t.Fatalf("HostKeyAlias = %q, want openclaw-1", target.HostKeyAlias)
	}
	if !target.ResolvedFromSSHConfig {
		t.Fatal("expected target to be resolved from ssh config")
	}
}

func TestResolveGitSSHTestTargetUsesSSHConfigAliasForScpRemote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sshDir, err)
	}
	config := "Host openclaw-1\n  HostName bastion.example.com\n  User deploy\n  Port 2222\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	target, err := resolveGitSSHTestTarget("git@openclaw-1:owner/repo.git")
	if err != nil {
		t.Fatalf("resolveGitSSHTestTarget: %v", err)
	}
	if target.Host != "bastion.example.com" {
		t.Fatalf("Host = %q, want bastion.example.com", target.Host)
	}
	if target.User != "git" {
		t.Fatalf("User = %q, want git", target.User)
	}
	if target.Port != "2222" {
		t.Fatalf("Port = %q, want 2222", target.Port)
	}
}

func TestResolveGitSSHTestTargetFollowsSSHConfigInclude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	includeDir := filepath.Join(sshDir, "conf.d")
	if err := os.MkdirAll(includeDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", includeDir, err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte("Include conf.d/*.conf\n"), 0o600); err != nil {
		t.Fatalf("write root config: %v", err)
	}
	include := "Host openclaw-1\n  HostName included.example.com\n  User ops\n"
	if err := os.WriteFile(filepath.Join(includeDir, "openclaw.conf"), []byte(include), 0o600); err != nil {
		t.Fatalf("write include config: %v", err)
	}

	target, err := resolveGitSSHTestTarget("openclaw-1")
	if err != nil {
		t.Fatalf("resolveGitSSHTestTarget: %v", err)
	}
	if target.Host != "included.example.com" {
		t.Fatalf("Host = %q, want included.example.com", target.Host)
	}
	if target.User != "ops" {
		t.Fatalf("User = %q, want ops", target.User)
	}
}

func TestResolveGitSSHTestTargetResolvesProxyJumpAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sshDir, err)
	}
	config := "Host openclaw-1\n  HostName bastion.example.com\n  ProxyJump jumpbox\n\nHost jumpbox\n  HostName gateway.example.com\n  User ops\n  Port 2222\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	target, err := resolveGitSSHTestTarget("openclaw-1")
	if err != nil {
		t.Fatalf("resolveGitSSHTestTarget: %v", err)
	}
	if len(target.JumpTargets) != 1 {
		t.Fatalf("JumpTargets = %+v, want one resolved jump target", target.JumpTargets)
	}
	if target.JumpTargets[0].Host != "gateway.example.com" {
		t.Fatalf("JumpTargets[0].Host = %q, want gateway.example.com", target.JumpTargets[0].Host)
	}
	if target.JumpTargets[0].User != "ops" {
		t.Fatalf("JumpTargets[0].User = %q, want ops", target.JumpTargets[0].User)
	}
	if target.JumpTargets[0].Port != "2222" {
		t.Fatalf("JumpTargets[0].Port = %q, want 2222", target.JumpTargets[0].Port)
	}
}

func TestResolveGitSSHTestTargetUsesExplicitHostKeyAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sshDir, err)
	}
	config := "Host openclaw-1\n  HostName bastion.example.com\n  HostKeyAlias cluster-primary\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	target, err := resolveGitSSHTestTarget("openclaw-1")
	if err != nil {
		t.Fatalf("resolveGitSSHTestTarget: %v", err)
	}
	if target.HostKeyAlias != "cluster-primary" {
		t.Fatalf("HostKeyAlias = %q, want cluster-primary", target.HostKeyAlias)
	}
}

func TestResolveGitSSHTestTargetIgnoresUnsupportedProxyCommandForDisplay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sshDir, err)
	}
	config := "Host openclaw-1\n  HostName bastion.example.com\n  ProxyCommand ssh jumpbox nc %h %p\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	target, err := resolveGitSSHTestTarget("openclaw-1")
	if err != nil {
		t.Fatalf("resolveGitSSHTestTarget: %v", err)
	}
	if target.Host != "bastion.example.com" {
		t.Fatalf("Host = %q, want bastion.example.com", target.Host)
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

func TestParseGitSSHTransportInvocationAllowsOnlyGitTransport(t *testing.T) {
	invocation, err := parseGitSSHTransportInvocation([]string{
		"-o", "SendEnv=GIT_PROTOCOL",
		"-p", "2222",
		"git@github.com",
		"git-upload-pack '/owner/repo.git'",
	}, "version=2")
	if err != nil {
		t.Fatalf("parseGitSSHTransportInvocation: %v", err)
	}
	if invocation.RequestedHost != "github.com" || invocation.Port != "2222" || invocation.GitProtocol != "version=2" {
		t.Fatalf("invocation = %+v", invocation)
	}

	for _, args := range [][]string{
		{"github.com"},
		{"github.com", "bash"},
		{"github.com", "git-upload-packevil repo"},
		{"github.com", "git-upload-pack", "; touch /tmp/pwned"},
		{"github.com", "git-upload-pack '/owner/repo.git'; touch /tmp/pwned"},
		{"github.com", "git-upload-pack $(touch /tmp/pwned)"},
		{"-A", "github.com", "git-upload-pack repo"},
		{"-o", "ProxyCommand=sh", "github.com", "git-upload-pack repo"},
	} {
		if _, err := parseGitSSHTransportInvocation(args, ""); err == nil {
			t.Fatalf("parseGitSSHTransportInvocation(%v) succeeded, want rejection", args)
		}
	}
}

func TestBuildGitSSHTransportCommandRoutesHostThroughBrokerPolicy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sshDir, err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte("Host github-alias\n  HostName github.com\n  User git\n  Port 2222\n  HostKeyAlias github.com\n"), 0o600); err != nil {
		t.Fatalf("write ssh config: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(keyPath, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(knownHostsPath, []byte("github.com ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cmd, err := buildGitSSHTransportCommand(sessionGitSSHConfig{
		Keys: []sessionGitSSHKey{{
			Name:         "github",
			Identity:     newExternalGitSSHIdentityRef(keyPath, knownHostsPath),
			AllowedHosts: []string{"github-alias"},
		}},
	}, gitSSHTransportRequest{
		Args:        []string{"git@github-alias", "git-upload-pack '/owner/repo.git'"},
		GitProtocol: "version=2",
	})
	if err != nil {
		t.Fatalf("buildGitSSHTransportCommand: %v", err)
	}
	got := strings.Join(cmd.Args, "\x00")
	for _, fragment := range []string{
		"/usr/bin/ssh",
		"-F\x00none",
		"BatchMode=yes",
		"IdentitiesOnly=yes",
		"IdentityAgent=none",
		"StrictHostKeyChecking=yes",
		"UserKnownHostsFile=" + knownHostsPath,
		"ForwardAgent=no",
		"ClearAllForwardings=yes",
		"PasswordAuthentication=no",
		"NumberOfPasswordPrompts=0",
		"-i\x00" + keyPath,
		"SendEnv=GIT_PROTOCOL",
		"HostKeyAlias=github.com",
		"-l\x00git",
		"-p\x002222",
		"-T\x00github.com",
		"git-upload-pack '/owner/repo.git'",
	} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("brokered ssh command missing %q:\n%q", fragment, cmd.Args)
		}
	}
	if strings.Contains(got, "SSH_AUTH_SOCK") || strings.Contains(got, "agent-") {
		t.Fatalf("brokered ssh command should not expose a session ssh-agent socket: %q", cmd.Args)
	}
}

func TestBuildGitSSHTransportCommandPreservesProxyJump(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", sshDir, err)
	}
	config := "Host openclaw-1\n  HostName bastion.example.com\n  ProxyJump jumpbox\n\nHost jumpbox\n  HostName gateway.example.com\n  User ops\n  Port 2222\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(config), 0o600); err != nil {
		t.Fatalf("write ssh config: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(keyPath, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(knownHostsPath, []byte("bastion.example.com ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cmd, err := buildGitSSHTransportCommand(sessionGitSSHConfig{
		Keys: []sessionGitSSHKey{{
			Name:         "cluster",
			Identity:     newExternalGitSSHIdentityRef(keyPath, knownHostsPath),
			AllowedHosts: []string{"openclaw-1"},
		}},
	}, gitSSHTransportRequest{Args: []string{"git@openclaw-1", "git-upload-pack repo"}})
	if err != nil {
		t.Fatalf("buildGitSSHTransportCommand: %v", err)
	}
	got := strings.Join(cmd.Args, "\x00")
	if !strings.Contains(got, "-J\x00ops@gateway.example.com:2222") {
		t.Fatalf("brokered ssh command should preserve ProxyJump, got %q", cmd.Args)
	}
	if !strings.Contains(got, "-T\x00bastion.example.com") {
		t.Fatalf("brokered ssh command should use resolved HostName, got %q", cmd.Args)
	}
}

func TestGitSSHTransportFramesRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := writeGitSSHTransportFrame(&buf, gitSSHFrameStdout, []byte("hello")); err != nil {
		t.Fatalf("writeGitSSHTransportFrame: %v", err)
	}
	frameType, payload, err := readGitSSHTransportFrame(&buf)
	if err != nil {
		t.Fatalf("readGitSSHTransportFrame: %v", err)
	}
	if frameType != gitSSHFrameStdout || string(payload) != "hello" {
		t.Fatalf("frame = %d %q, want stdout hello", frameType, payload)
	}
}

func TestResolvePreparedSessionAddsManagedGitSSHNotes(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	projectDir := t.TempDir()
	keyDir := writeSSHKeyDirectory(t, true)
	if err := runConfigSSHAdd(projectDir, "id_ed25519", []string{"github.com"}, "", "", filepath.Join(keyDir, "id_ed25519")); err != nil {
		t.Fatalf("runConfigSSHAdd: %v", err)
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
	if err := runConfigSSHAdd(projectDir, "id_ed25519", []string{"github.com"}, "", "", filepath.Join(keyDir, "id_ed25519")); err != nil {
		t.Fatalf("runConfigSSHAdd: %v", err)
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

func writeProvisionedSSHKeyDirectory(t *testing.T, keyName string) provisionedSSHKey {
	t.Helper()

	dir := filepath.Join(provisionedSSHKeysRootDir(), keyName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir provisioned SSH key dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private_key"), []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("write provisioned private key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "public_key"), []byte("ssh-ed25519 AAAA test@hazmat"), 0o600); err != nil {
		t.Fatalf("write provisioned public key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "known_hosts"), []byte("github.com ssh-ed25519 AAAA"), 0o600); err != nil {
		t.Fatalf("write provisioned known_hosts: %v", err)
	}
	key := inspectProvisionedSSHKey(keyName, dir)
	if !key.Usable() {
		t.Fatalf("provisioned key = %+v, want usable", key)
	}
	return key
}

type assertErr struct{}

func (assertErr) Error() string {
	return "probe failed"
}

func TestRunConfigSSHAddAppendsNamedKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "github_key", true)
	if err := runConfigSSHAdd(projectDir, "github", []string{"github.com"}, "", "", filepath.Join(keyDir, "github_key")); err != nil {
		t.Fatalf("runConfigSSHAdd: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	canonical, _ := resolveDir(projectDir, false)
	got := cfg.ProjectSSH(canonical)
	if got == nil {
		t.Fatal("ProjectSSH nil after add")
	}
	if len(got.Keys) != 1 {
		t.Fatalf("Keys len = %d, want 1", len(got.Keys))
	}
	if got.Keys[0].Name != "github" || !slices.Equal(got.Keys[0].Hosts, []string{"github.com"}) {
		t.Fatalf("first key = %+v", got.Keys[0])
	}
}

func TestRunConfigSSHAddRejectsOverlap(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDirA := writeNamedSSHKeyDirectory(t, "key_a", true)
	keyDirB := writeNamedSSHKeyDirectory(t, "key_b", true)

	if err := runConfigSSHAdd(projectDir, "a", []string{"github.com"}, "", "", filepath.Join(keyDirA, "key_a")); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := runConfigSSHAdd(projectDir, "b", []string{"github.com", "gitlab.com"}, "", "", filepath.Join(keyDirB, "key_b"))
	if err == nil || !strings.Contains(err.Error(), "github.com") {
		t.Fatalf("want overlap rejection, got %v", err)
	}
}

func TestRunConfigSSHAddRejectsInlineKeyWithEmptyHosts(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDirA := writeNamedSSHKeyDirectory(t, "key_a", true)

	// An inline key with no declared hosts is rejected regardless of
	// how many other keys are configured.
	err := runConfigSSHAdd(projectDir, "a", nil, "", "", filepath.Join(keyDirA, "key_a"))
	if err == nil || !strings.Contains(err.Error(), "inline key has no declared hosts") {
		t.Fatalf("want inline-empty-hosts rejection, got %v", err)
	}
}

func TestRunConfigSSHRemoveClearsLastKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "only_key", true)
	if err := runConfigSSHAdd(projectDir, "only", []string{"github.com"}, "", "", filepath.Join(keyDir, "only_key")); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := runConfigSSHRemove(projectDir, "only"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	canonical, _ := resolveDir(projectDir, false)
	if got := cfg.ProjectSSH(canonical); got != nil {
		t.Fatalf("ProjectSSH = %+v, want nil after removing last key", got)
	}
}

func TestRunConfigSSHRemoveUnknownName(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "only_key", true)
	if err := runConfigSSHAdd(projectDir, "only", []string{"github.com"}, "", "", filepath.Join(keyDir, "only_key")); err != nil {
		t.Fatalf("add: %v", err)
	}
	err := runConfigSSHRemove(projectDir, "ghost")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("want unknown-name rejection, got %v", err)
	}
}

func TestRunConfigSSHProfileAddAndList(t *testing.T) {
	isolateConfig(t)

	keyDir := writeNamedSSHKeyDirectory(t, "shared_key", true)
	if err := runConfigSSHProfileAdd("github", filepath.Join(keyDir, "shared_key"), "", []string{"github.com"}, "personal"); err != nil {
		t.Fatalf("profile add: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	profile, ok := cfg.SSHProfiles["github"]
	if !ok {
		t.Fatal("SSHProfiles[github] missing after add")
	}
	if !slices.Equal(profile.DefaultHosts, []string{"github.com"}) || profile.Description != "personal" {
		t.Fatalf("profile after add = %+v", profile)
	}

	if err := runConfigSSHProfileList(); err != nil {
		t.Fatalf("profile list: %v", err)
	}
}

func TestRunConfigSSHProfileAddRejectsDuplicate(t *testing.T) {
	isolateConfig(t)
	keyDir := writeNamedSSHKeyDirectory(t, "shared_key", true)
	if err := runConfigSSHProfileAdd("github", filepath.Join(keyDir, "shared_key"), "", nil, ""); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := runConfigSSHProfileAdd("github", filepath.Join(keyDir, "shared_key"), "", nil, "")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want duplicate rejection, got %v", err)
	}
}

func TestRunConfigSSHProfileRemoveRefusesWithReferrers(t *testing.T) {
	isolateConfig(t)
	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "shared_key", true)
	if err := runConfigSSHProfileAdd("github", filepath.Join(keyDir, "shared_key"), "", []string{"github.com"}, ""); err != nil {
		t.Fatalf("profile add: %v", err)
	}
	if err := runConfigSSHAdd(projectDir, "gh", nil, "", "github", ""); err != nil {
		t.Fatalf("project add --profile: %v", err)
	}
	err := runConfigSSHProfileRemove("github", false)
	if err == nil || !strings.Contains(err.Error(), "referenced by") {
		t.Fatalf("want referrer-safe rejection, got %v", err)
	}
}

func TestRunConfigSSHProfileRemoveForceDetaches(t *testing.T) {
	isolateConfig(t)
	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "shared_key", true)
	if err := runConfigSSHProfileAdd("github", filepath.Join(keyDir, "shared_key"), "", []string{"github.com"}, ""); err != nil {
		t.Fatalf("profile add: %v", err)
	}
	if err := runConfigSSHAdd(projectDir, "gh", nil, "", "github", ""); err != nil {
		t.Fatalf("project add --profile: %v", err)
	}
	if err := runConfigSSHProfileRemove("github", true); err != nil {
		t.Fatalf("profile remove --force: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if _, exists := cfg.SSHProfiles["github"]; exists {
		t.Fatal("profile should be removed")
	}
	canonical, _ := resolveDir(projectDir, false)
	if got := cfg.ProjectSSH(canonical); got != nil {
		t.Fatalf("project SSH should be cleared after last key detach, got %+v", got)
	}
}

func TestRunConfigSSHProfileRenameUpdatesReferrers(t *testing.T) {
	isolateConfig(t)
	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "shared_key", true)
	if err := runConfigSSHProfileAdd("github", filepath.Join(keyDir, "shared_key"), "", []string{"github.com"}, ""); err != nil {
		t.Fatalf("profile add: %v", err)
	}
	if err := runConfigSSHAdd(projectDir, "gh", nil, "", "github", ""); err != nil {
		t.Fatalf("project add --profile: %v", err)
	}
	if err := runConfigSSHProfileRename("github", "work_github"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if _, exists := cfg.SSHProfiles["github"]; exists {
		t.Fatal("old name should be gone")
	}
	if _, exists := cfg.SSHProfiles["work_github"]; !exists {
		t.Fatal("new name should exist")
	}
	canonical, _ := resolveDir(projectDir, false)
	projectSSH := cfg.ProjectSSH(canonical)
	if projectSSH == nil || len(projectSSH.Keys) != 1 || projectSSH.Keys[0].Profile != "work_github" {
		t.Fatalf("project reference not updated: %+v", projectSSH)
	}
}

func TestRunConfigSSHAddProfileInheritsDefaultHosts(t *testing.T) {
	isolateConfig(t)
	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "shared_key", true)
	if err := runConfigSSHProfileAdd("github", filepath.Join(keyDir, "shared_key"), "", []string{"github.com"}, ""); err != nil {
		t.Fatalf("profile add: %v", err)
	}
	// --host not passed — should inherit from profile.default_hosts
	if err := runConfigSSHAdd(projectDir, "work", nil, "", "github", ""); err != nil {
		t.Fatalf("project add --profile without --host: %v", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	canonical, _ := resolveDir(projectDir, false)
	got := cfg.ProjectSSH(canonical)
	if got == nil || len(got.Keys) != 1 {
		t.Fatalf("project SSH not set correctly: %+v", got)
	}
	if got.Keys[0].Profile != "github" || len(got.Keys[0].Hosts) != 0 {
		t.Fatalf("project key should reference profile with empty declared Hosts (inherits), got %+v", got.Keys[0])
	}
}

func TestRunConfigSSHShowDisplaysProfileBackedKey(t *testing.T) {
	isolateConfig(t)
	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "shared_key", true)
	keyPath := filepath.Join(keyDir, "shared_key")
	canonicalKeyPath, err := canonicalizeConfiguredFile(keyPath)
	if err != nil {
		t.Fatalf("canonicalize key: %v", err)
	}
	if err := runConfigSSHProfileAdd("github", keyPath, "", []string{"github.com"}, ""); err != nil {
		t.Fatalf("profile add: %v", err)
	}
	if err := runConfigSSHAdd(projectDir, "work", nil, "", "github", ""); err != nil {
		t.Fatalf("project add --profile without --host: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runConfigSSHShow(projectDir)
	})
	if err != nil {
		t.Fatalf("show profile-backed key: %v", err)
	}
	for _, fragment := range []string{
		"Profile:       github",
		"Private key:   " + canonicalKeyPath,
		"Hosts:         github.com (inherited from profile default_hosts)",
	} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("show output missing %q:\n%s", fragment, out)
		}
	}
	if strings.Contains(out, "legacy fallback") {
		t.Fatalf("show output should not mention legacy fallback:\n%s", out)
	}
}

func TestProjectSSHConfigNormalizedKeysFlatLegacyReturnsNil(t *testing.T) {
	// The pre-migration flat shape no longer synthesizes a Keys entry.
	// loadConfig (via detectLegacyFlatSSH) rejects such configs before
	// they reach NormalizedKeys; if one somehow does, the function
	// returns nil rather than fabricating a key.
	cfg := ProjectSSHConfig{
		PrivateKeyPath: "/keys/id_ed25519",
		KnownHostsPath: "/keys/known_hosts",
	}
	if got := cfg.NormalizedKeys(); got != nil {
		t.Fatalf("NormalizedKeys on flat legacy = %+v, want nil", got)
	}
}

func TestDetectLegacyFlatSSHEmitsMigrationSnippet(t *testing.T) {
	cfg := ProjectSSHConfig{
		PrivateKeyPath: "/keys/id_ed25519",
		KnownHostsPath: "/keys/known_hosts",
	}
	err := detectLegacyFlatSSH("/tmp/proj", cfg)
	if err == nil {
		t.Fatal("want rejection for flat legacy shape")
	}
	for _, fragment := range []string{
		"retired single-key SSH shape",
		"/tmp/proj",
		"ssh:",
		"keys:",
		"- name: default",
		"private_key: /keys/id_ed25519",
		"known_hosts: /keys/known_hosts",
		"hosts: [github.com]",
	} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("migration snippet missing %q:\n%s", fragment, err.Error())
		}
	}
}

func TestDetectLegacyFlatSSHPassesWhenKeysListPresent(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "x", PrivateKeyPath: "/p", Hosts: []string{"github.com"}}},
	}
	if err := detectLegacyFlatSSH("/tmp/proj", cfg); err != nil {
		t.Fatalf("multi-key config should not trigger legacy detection: %v", err)
	}
}

func TestResolveManagedGitSSHPropagatesLegacyFlatLoadError(t *testing.T) {
	isolateConfig(t)
	projectDir := t.TempDir()
	cfg := defaultConfig()
	cfg.Projects = map[string]ProjectConfig{
		projectDir: {
			SSH: &ProjectSSHConfig{
				PrivateKeyPath: "/keys/id_ed25519",
				KnownHostsPath: "/keys/known_hosts",
			},
		},
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("save flat legacy config: %v", err)
	}

	_, err := resolveManagedGitSSH(sessionConfig{ProjectDir: projectDir})
	if err == nil || !strings.Contains(err.Error(), "retired single-key SSH shape") {
		t.Fatalf("want flat legacy migration error, got %v", err)
	}
}

func TestProjectSSHConfigNormalizedKeysMultiKey(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{
			{Name: "github", PrivateKeyPath: "/g", KnownHostsPath: "/g.kh", Hosts: []string{"github.com"}},
			{Name: "prod", PrivateKeyPath: "/p", KnownHostsPath: "/p.kh", Hosts: []string{"prod.example.com"}},
		},
	}
	got := cfg.NormalizedKeys()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "github" || got[1].Name != "prod" {
		t.Fatalf("names = %q,%q, want github,prod", got[0].Name, got[1].Name)
	}
	got[0].Hosts[0] = "mutated"
	if cfg.Keys[0].Hosts[0] == "mutated" {
		t.Fatal("NormalizedKeys should deep-copy Hosts slice")
	}
}

func TestProjectSSHConfigNormalizedKeysEmpty(t *testing.T) {
	if got := (ProjectSSHConfig{}).NormalizedKeys(); got != nil {
		t.Fatalf("NormalizedKeys on empty config = %v, want nil", got)
	}
}

func TestValidateProjectSSHConfigRejectsMixedShapes(t *testing.T) {
	cfg := ProjectSSHConfig{
		PrivateKeyPath: "/k",
		Keys:           []ProjectSSHKey{{Name: "github", PrivateKeyPath: "/g", Hosts: []string{"github.com"}}},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "cannot combine") {
		t.Fatalf("want mixed-shape rejection, got %v", err)
	}
}

func TestValidateProjectSSHConfigRejectsOverlap(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{
			{Name: "a", PrivateKeyPath: "/a", Hosts: []string{"github.com"}},
			{Name: "b", PrivateKeyPath: "/b", Hosts: []string{"github.com", "gitlab.com"}},
		},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "github.com") {
		t.Fatalf("want overlap rejection on github.com, got %v", err)
	}
}

func TestValidateProjectSSHConfigRejectsWildcardOverlap(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{
			{Name: "a", PrivateKeyPath: "/a", Hosts: []string{"*.prod.example.com"}},
			{Name: "b", PrivateKeyPath: "/b", Hosts: []string{"api.prod.example.com"}},
		},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "both match host") {
		t.Fatalf("want wildcard overlap rejection, got %v", err)
	}
}

func TestValidateProjectSSHConfigRejectsInlineKeyWithEmptyHostsEvenSingle(t *testing.T) {
	// After the any-host fallback retirement (sandboxing-qq9b), an inline
	// key with no declared hosts is rejected regardless of how many keys
	// are configured.
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "only", PrivateKeyPath: "/k"}},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "inline key has no declared hosts") {
		t.Fatalf("want inline-empty-hosts rejection, got %v", err)
	}
}

func TestValidateProjectSSHConfigAcceptsProfileKeyWithEmptyHosts(t *testing.T) {
	// Profile-referencing keys with empty declared hosts remain valid —
	// they inherit default_hosts from the profile.
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "shared", Profile: "github"}},
	}
	if err := ValidateProjectSSHConfig(cfg); err != nil {
		t.Fatalf("profile-referencing key with empty hosts should still pass format check: %v", err)
	}
}

func TestValidateProjectSSHConfigRejectsDuplicateName(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{
			{Name: "dup", PrivateKeyPath: "/a", Hosts: []string{"a.example"}},
			{Name: "dup", PrivateKeyPath: "/b", Hosts: []string{"b.example"}},
		},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("want duplicate-name rejection, got %v", err)
	}
}

func TestValidateProjectSSHConfigRejectsMissingIdentity(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "x", Hosts: []string{"a.example"}}},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "one of 'profile', 'private_key', or 'key' is required") {
		t.Fatalf("want missing-identity rejection, got %v", err)
	}
}

func TestValidateProjectSSHConfigRejectsBothIdentities(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "x", Key: "inv", PrivateKeyPath: "/p", Hosts: []string{"a.example"}}},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "set exactly one") {
		t.Fatalf("want multiple-identities rejection, got %v", err)
	}
}

func TestValidateProjectSSHConfigRejectsProfilePlusInline(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{
			Name:           "x",
			Profile:        "github",
			PrivateKeyPath: "/p",
			Hosts:          []string{"a.example"},
		}},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "set exactly one") {
		t.Fatalf("want profile+inline rejection, got %v", err)
	}
}

func TestValidateProjectSSHConfigAcceptsProfileOnly(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "x", Profile: "github", Hosts: []string{"github.com"}}},
	}
	if err := ValidateProjectSSHConfig(cfg); err != nil {
		t.Fatalf("profile-only key should validate: %v", err)
	}
}

func TestValidateProjectSSHConfigAllowsProfileKeyWithEmptyHostsAlongsideInline(t *testing.T) {
	// A profile-referencing key with empty hosts is NOT the legacy
	// any-host fallback — it inherits from the profile. So it can coexist
	// with an inline key that has its own declared hosts.
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{
			{Name: "inline", PrivateKeyPath: "/p", Hosts: []string{"github.com"}},
			{Name: "via_profile", Profile: "prod"}, // no declared hosts; inherits
		},
	}
	if err := ValidateProjectSSHConfig(cfg); err != nil {
		t.Fatalf("profile-referencing key with empty hosts should pass format check: %v", err)
	}
}

func TestValidateProjectSSHConfigRejectsInvalidHost(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "x", PrivateKeyPath: "/p", Hosts: []string{"git@evil"}}},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "invalid host") {
		t.Fatalf("want invalid-host rejection, got %v", err)
	}
}

func TestSelectSessionGitSSHKeyRejectsEmptyAllowedHosts(t *testing.T) {
	// After the any-host fallback retirement, a single key with empty
	// AllowedHosts cannot reach selectSessionGitSSHKey (ValidateProjectSSHConfig
	// and detectLegacyFlatSSH reject it at config load). If it somehow
	// did, no host would match and the lookup would reject.
	cfg := &sessionGitSSHConfig{
		Keys: []sessionGitSSHKey{{Name: "only", Identity: newExternalGitSSHIdentityRef("/k", "/kh")}},
	}
	_, err := selectSessionGitSSHKey(cfg, "github.com")
	if err == nil || !strings.Contains(err.Error(), "no SSH key configured for host") {
		t.Fatalf("want rejection for empty AllowedHosts, got %v", err)
	}
}

func TestSelectSessionGitSSHKeyMultiKeyRoutesByHost(t *testing.T) {
	cfg := &sessionGitSSHConfig{
		Keys: []sessionGitSSHKey{
			{Name: "github", AllowedHosts: []string{"github.com"}},
			{Name: "prod", AllowedHosts: []string{"prod.example.com", "*.prod.example.com"}},
		},
	}
	cases := map[string]string{
		"github.com":             "github",
		"prod.example.com":       "prod",
		"api.prod.example.com":   "prod",
		"cache.prod.example.com": "prod",
	}
	for host, want := range cases {
		got, err := selectSessionGitSSHKey(cfg, host)
		if err != nil {
			t.Fatalf("select(%q): %v", host, err)
		}
		if got.Name != want {
			t.Fatalf("select(%q) = %q, want %q", host, got.Name, want)
		}
	}
}

func TestSelectSessionGitSSHKeyRejectsUnknownHost(t *testing.T) {
	cfg := &sessionGitSSHConfig{
		Keys: []sessionGitSSHKey{
			{Name: "github", AllowedHosts: []string{"github.com"}},
		},
	}
	_, err := selectSessionGitSSHKey(cfg, "gitlab.com")
	if err == nil || !strings.Contains(err.Error(), "no SSH key configured for host") {
		t.Fatalf("want unknown-host rejection, got %v", err)
	}
}

func TestValidateSSHProfilesAcceptsWellFormed(t *testing.T) {
	profiles := map[string]SSHProfile{
		"github": {PrivateKeyPath: "/k/github", DefaultHosts: []string{"github.com"}},
		"prod":   {PrivateKeyPath: "/k/prod", KnownHostsPath: "/k/prod.kh", DefaultHosts: []string{"prod.example.com", "*.prod.example.com"}, Description: "prod servers"},
	}
	if err := ValidateSSHProfiles(profiles); err != nil {
		t.Fatalf("well-formed profiles should validate: %v", err)
	}
}

func TestValidateSSHProfilesRejectsMissingPrivateKey(t *testing.T) {
	profiles := map[string]SSHProfile{
		"orphan": {DefaultHosts: []string{"example.com"}},
	}
	err := ValidateSSHProfiles(profiles)
	if err == nil || !strings.Contains(err.Error(), "'private_key' is required") {
		t.Fatalf("want missing private_key rejection, got %v", err)
	}
}

func TestValidateSSHProfilesRejectsInvalidName(t *testing.T) {
	profiles := map[string]SSHProfile{
		"bad name!": {PrivateKeyPath: "/p"},
	}
	err := ValidateSSHProfiles(profiles)
	if err == nil || !strings.Contains(err.Error(), "invalid name") {
		t.Fatalf("want invalid-name rejection, got %v", err)
	}
}

func TestValidateSSHProfilesRejectsInvalidDefaultHost(t *testing.T) {
	profiles := map[string]SSHProfile{
		"x": {PrivateKeyPath: "/p", DefaultHosts: []string{"git@evil"}},
	}
	err := ValidateSSHProfiles(profiles)
	if err == nil || !strings.Contains(err.Error(), "invalid host") {
		t.Fatalf("want invalid default host rejection, got %v", err)
	}
}

func TestValidateProjectSSHProfileRefsRejectsDangling(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "x", Profile: "ghost", Hosts: []string{"github.com"}}},
	}
	profiles := map[string]SSHProfile{
		"other": {PrivateKeyPath: "/p"},
	}
	err := ValidateProjectSSHProfileRefs(cfg, profiles)
	if err == nil || !strings.Contains(err.Error(), "not defined in ssh_profiles") {
		t.Fatalf("want dangling-ref rejection, got %v", err)
	}
}

func TestValidateProjectSSHProfileRefsAcceptsValidRef(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "x", Profile: "github", Hosts: []string{"github.com"}}},
	}
	profiles := map[string]SSHProfile{
		"github": {PrivateKeyPath: "/p"},
	}
	if err := ValidateProjectSSHProfileRefs(cfg, profiles); err != nil {
		t.Fatalf("valid ref should pass: %v", err)
	}
}

func TestValidateProjectSSHProfileRefsRejectsOverlapAfterInheritance(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{
			// Key a declares hosts directly.
			{Name: "a", PrivateKeyPath: "/p1", Hosts: []string{"github.com"}},
			// Key b has no declared hosts; inherits default_hosts from profile.
			{Name: "b", Profile: "github-bot"},
		},
	}
	profiles := map[string]SSHProfile{
		"github-bot": {PrivateKeyPath: "/p2", DefaultHosts: []string{"github.com"}},
	}
	err := ValidateProjectSSHProfileRefs(cfg, profiles)
	if err == nil || !strings.Contains(err.Error(), "github.com") {
		t.Fatalf("want effective-hosts overlap rejection, got %v", err)
	}
}

func TestValidateProjectSSHProfileRefsAcceptsDisjointInheritance(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{
			{Name: "gh", Profile: "gh"},
			{Name: "pr", Profile: "pr"},
		},
	}
	profiles := map[string]SSHProfile{
		"gh": {PrivateKeyPath: "/p1", DefaultHosts: []string{"github.com"}},
		"pr": {PrivateKeyPath: "/p2", DefaultHosts: []string{"prod.example.com"}},
	}
	if err := ValidateProjectSSHProfileRefs(cfg, profiles); err != nil {
		t.Fatalf("disjoint inherited defaults should pass: %v", err)
	}
}

func TestProjectSSHKeyEffectiveHostsDeclaredOverride(t *testing.T) {
	key := ProjectSSHKey{
		Name:    "x",
		Profile: "github",
		Hosts:   []string{"internal.example.com"}, // declared overrides profile defaults
	}
	profiles := map[string]SSHProfile{
		"github": {PrivateKeyPath: "/p", DefaultHosts: []string{"github.com"}},
	}
	got := key.EffectiveHosts(profiles)
	if len(got) != 1 || got[0] != "internal.example.com" {
		t.Fatalf("EffectiveHosts = %v, want [internal.example.com]", got)
	}
}

func TestProjectSSHKeyEffectiveHostsInheritsDefaults(t *testing.T) {
	key := ProjectSSHKey{Name: "x", Profile: "github"}
	profiles := map[string]SSHProfile{
		"github": {PrivateKeyPath: "/p", DefaultHosts: []string{"github.com", "github.enterprise"}},
	}
	got := key.EffectiveHosts(profiles)
	want := []string{"github.com", "github.enterprise"}
	if !slices.Equal(got, want) {
		t.Fatalf("EffectiveHosts = %v, want %v", got, want)
	}
}

func TestValidateProjectSSHConfigRejectsInvalidName(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "bad name", PrivateKeyPath: "/p", Hosts: []string{"a.example"}}},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "invalid name") {
		t.Fatalf("want invalid-name rejection, got %v", err)
	}
}
