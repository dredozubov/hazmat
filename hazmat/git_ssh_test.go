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
	if len(got.Keys) != 1 {
		t.Fatalf("Keys len = %d, want 1", len(got.Keys))
	}
	if got.Keys[0].Name != "id_ed25519" {
		t.Fatalf("Keys[0].Name = %q, want id_ed25519", got.Keys[0].Name)
	}
	if got.Keys[0].PrivateKeyPath != keyPath {
		t.Fatalf("Keys[0].PrivateKeyPath = %q, want %q", got.Keys[0].PrivateKeyPath, keyPath)
	}
	if got.Keys[0].KnownHostsPath != knownHostsPath {
		t.Fatalf("Keys[0].KnownHostsPath = %q, want %q", got.Keys[0].KnownHostsPath, knownHostsPath)
	}
	if len(got.Keys[0].AllowedHosts) != 0 {
		t.Fatalf("Keys[0].AllowedHosts = %v, want none (legacy any-host fallback)", got.Keys[0].AllowedHosts)
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
	if got.Keys[0].PrivateKeyPath != canonicalKey {
		t.Fatalf("Keys[0].PrivateKeyPath = %q, want %q (profile identity)", got.Keys[0].PrivateKeyPath, canonicalKey)
	}
	if !slices.Equal(got.Keys[0].AllowedHosts, []string{"github.com"}) {
		t.Fatalf("Keys[0].AllowedHosts = %v, want [github.com] (inherited from profile)", got.Keys[0].AllowedHosts)
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

func TestBuildGitSSHWrapperScriptWithoutAllowlistSkipsHostRestriction(t *testing.T) {
	script := buildGitSSHWrapperScript([]preparedSSHIdentityKey{{
		Name:           "default",
		SocketPath:     "/tmp/agent.sock",
		KnownHostsPath: "/tmp/known_hosts",
	}})
	for _, fragment := range []string{
		"interactive ssh is not allowed",
		"git-upload-pack*|git-receive-pack*|git-upload-archive*",
		"-o IdentityFile=none",
		"sock=/tmp/agent.sock",
		"kh=/tmp/known_hosts",
		"-o UserKnownHostsFile=\"$kh\"",
		"-o IdentityAgent=\"$sock\"",
		"-o StrictHostKeyChecking=yes",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("wrapper script missing %q:\n%s", fragment, script)
		}
	}
	if strings.Contains(script, "destination host not allowed") {
		t.Fatalf("legacy single-key wrapper should not enforce host allowlist:\n%s", script)
	}
	if strings.Contains(script, "IdentitiesOnly=yes") {
		t.Fatalf("wrapper script should not force IdentitiesOnly=yes:\n%s", script)
	}
}

func TestBuildGitSSHWrapperScriptMultiKeyRoutesByHost(t *testing.T) {
	script := buildGitSSHWrapperScript([]preparedSSHIdentityKey{
		{
			Name:           "github",
			SocketPath:     "/tmp/agent-github.sock",
			KnownHostsPath: "/tmp/kh-github",
			AllowedHosts:   []string{"github.com"},
		},
		{
			Name:           "prod",
			SocketPath:     "/tmp/agent-prod.sock",
			KnownHostsPath: "/tmp/kh-prod",
			AllowedHosts:   []string{"prod.example.com", "*.prod.example.com"},
		},
	})
	for _, fragment := range []string{
		"github.com) sock=/tmp/agent-github.sock; kh=/tmp/kh-github ;;",
		"prod.example.com|*.prod.example.com) sock=/tmp/agent-prod.sock; kh=/tmp/kh-prod ;;",
		"*) reject \"destination host not allowed: $normalized_host\" ;;",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("multi-key wrapper missing %q:\n%s", fragment, script)
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

func TestRunConfigSSHAddAppendsNamedKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "github_key", true)
	if err := runConfigSSHAdd(projectDir, "github", []string{"github.com"}, "", filepath.Join(keyDir, "github_key")); err != nil {
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

	if err := runConfigSSHAdd(projectDir, "a", []string{"github.com"}, "", filepath.Join(keyDirA, "key_a")); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := runConfigSSHAdd(projectDir, "b", []string{"github.com", "gitlab.com"}, "", filepath.Join(keyDirB, "key_b"))
	if err == nil || !strings.Contains(err.Error(), "github.com") {
		t.Fatalf("want overlap rejection, got %v", err)
	}
}

func TestRunConfigSSHAddRejectsSecondKeyWithEmptyHosts(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDirA := writeNamedSSHKeyDirectory(t, "key_a", true)
	keyDirB := writeNamedSSHKeyDirectory(t, "key_b", true)

	if err := runConfigSSHAdd(projectDir, "a", []string{"github.com"}, "", filepath.Join(keyDirA, "key_a")); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := runConfigSSHAdd(projectDir, "b", nil, "", filepath.Join(keyDirB, "key_b"))
	if err == nil || !strings.Contains(err.Error(), "legacy any-host fallback") {
		t.Fatalf("want empty-hosts rejection, got %v", err)
	}
}

func TestRunConfigSSHAddRejectsMixingLegacyWithNewKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDirA := writeNamedSSHKeyDirectory(t, "legacy_key", true)
	keyDirB := writeNamedSSHKeyDirectory(t, "second_key", true)

	if err := runConfigSSHSet(projectDir, filepath.Join(keyDirA, "legacy_key")); err != nil {
		t.Fatalf("runConfigSSHSet: %v", err)
	}
	err := runConfigSSHAdd(projectDir, "second", []string{"prod.example.com"}, "", filepath.Join(keyDirB, "second_key"))
	if err == nil || !strings.Contains(err.Error(), "any-host legacy key") {
		t.Fatalf("want legacy-migration hint, got %v", err)
	}
}

func TestRunConfigSSHRemoveClearsLastKey(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	keyDir := writeNamedSSHKeyDirectory(t, "only_key", true)
	if err := runConfigSSHAdd(projectDir, "only", []string{"github.com"}, "", filepath.Join(keyDir, "only_key")); err != nil {
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
	if err := runConfigSSHAdd(projectDir, "only", []string{"github.com"}, "", filepath.Join(keyDir, "only_key")); err != nil {
		t.Fatalf("add: %v", err)
	}
	err := runConfigSSHRemove(projectDir, "ghost")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("want unknown-name rejection, got %v", err)
	}
}

func TestProjectSSHConfigNormalizedKeysLegacySingleKey(t *testing.T) {
	cfg := ProjectSSHConfig{
		PrivateKeyPath: "/keys/id_ed25519",
		KnownHostsPath: "/keys/known_hosts",
	}
	got := cfg.NormalizedKeys()
	if len(got) != 1 {
		t.Fatalf("NormalizedKeys len = %d, want 1", len(got))
	}
	if got[0].Name != "id_ed25519" {
		t.Fatalf("Name = %q, want id_ed25519 (basename of PrivateKeyPath)", got[0].Name)
	}
	if got[0].PrivateKeyPath != "/keys/id_ed25519" || got[0].KnownHostsPath != "/keys/known_hosts" {
		t.Fatalf("legacy paths not preserved: %+v", got[0])
	}
	if len(got[0].Hosts) != 0 {
		t.Fatalf("Hosts = %v, want empty (any-host fallback)", got[0].Hosts)
	}
}

func TestProjectSSHConfigNormalizedKeysLegacyUnparseableBasenameFallsBackToDefault(t *testing.T) {
	cfg := ProjectSSHConfig{
		PrivateKeyPath: "/keys/my key!",
		KnownHostsPath: "/keys/known_hosts",
	}
	got := cfg.NormalizedKeys()
	if len(got) != 1 {
		t.Fatalf("NormalizedKeys len = %d, want 1", len(got))
	}
	if got[0].Name != "default" {
		t.Fatalf("Name = %q, want default (basename has invalid chars)", got[0].Name)
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

func TestValidateProjectSSHConfigAcceptsLegacyFlat(t *testing.T) {
	cfg := ProjectSSHConfig{PrivateKeyPath: "/k", KnownHostsPath: "/k.kh"}
	if err := ValidateProjectSSHConfig(cfg); err != nil {
		t.Fatalf("legacy flat should validate: %v", err)
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

func TestValidateProjectSSHConfigRejectsMultiKeyWithEmptyHosts(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{
			{Name: "a", PrivateKeyPath: "/a", Hosts: []string{"github.com"}},
			{Name: "b", PrivateKeyPath: "/b"},
		},
	}
	err := ValidateProjectSSHConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "legacy any-host fallback") {
		t.Fatalf("want legacy-multi rejection, got %v", err)
	}
}

func TestValidateProjectSSHConfigAcceptsSingleKeyWithEmptyHosts(t *testing.T) {
	cfg := ProjectSSHConfig{
		Keys: []ProjectSSHKey{{Name: "only", PrivateKeyPath: "/k"}},
	}
	if err := ValidateProjectSSHConfig(cfg); err != nil {
		t.Fatalf("single-key any-host should validate: %v", err)
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

func TestSelectSessionGitSSHKeyLegacyAnyHostServesAny(t *testing.T) {
	cfg := &sessionGitSSHConfig{
		Keys: []sessionGitSSHKey{{Name: "default", PrivateKeyPath: "/k", KnownHostsPath: "/kh"}},
	}
	for _, host := range []string{"github.com", "prod.example.com", "anything.else"} {
		got, err := selectSessionGitSSHKey(cfg, host)
		if err != nil {
			t.Fatalf("select(%q): %v", host, err)
		}
		if got.Name != "default" {
			t.Fatalf("select(%q) = %q, want default", host, got.Name)
		}
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
