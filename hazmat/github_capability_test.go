package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withGitHubTokenHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestConfigGitHubStoresTokenFromEnv(t *testing.T) {
	home := withGitHubTokenHome(t)
	t.Setenv("GH_TOKEN", "example-github-token")
	t.Setenv("GITHUB_TOKEN", "fallback-should-not-win")

	if err := runConfigGitHub(true, false); err != nil {
		t.Fatalf("runConfigGitHub: %v", err)
	}

	token, ok, err := readGitHubStoredToken()
	if err != nil {
		t.Fatalf("readGitHubStoredToken: %v", err)
	}
	if !ok || token != "example-github-token" {
		t.Fatalf("stored token = %q, %v; want example-github-token, true", token, ok)
	}
	info, err := os.Stat(githubTokenStorePathForHome(home))
	if err != nil {
		t.Fatalf("stat stored token: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("stored token mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestConfigGitHubUsesGitHubTokenFallbackEnv(t *testing.T) {
	withGitHubTokenHome(t)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "example-fallback-github-token")

	if err := runConfigGitHub(true, false); err != nil {
		t.Fatalf("runConfigGitHub: %v", err)
	}
	token, ok, err := readGitHubStoredToken()
	if err != nil {
		t.Fatalf("readGitHubStoredToken: %v", err)
	}
	if !ok || token != "example-fallback-github-token" {
		t.Fatalf("stored token = %q, %v; want fallback token, true", token, ok)
	}
}

func TestConfigGitHubClearRemovesToken(t *testing.T) {
	home := withGitHubTokenHome(t)
	if err := saveGitHubStoredToken("example-github-token"); err != nil {
		t.Fatalf("saveGitHubStoredToken: %v", err)
	}

	if err := runConfigGitHub(false, true); err != nil {
		t.Fatalf("runConfigGitHub clear: %v", err)
	}

	if _, err := os.Stat(githubTokenStorePathForHome(home)); !os.IsNotExist(err) {
		t.Fatalf("stored token still exists or stat failed unexpectedly: %v", err)
	}
}

func TestApplyGitHubSessionCapabilityGrantsRedactedGHToken(t *testing.T) {
	withGitHubTokenHome(t)
	if err := saveGitHubStoredToken("example-github-token"); err != nil {
		t.Fatalf("saveGitHubStoredToken: %v", err)
	}

	cfg := sessionConfig{ProjectDir: filepath.Join(t.TempDir(), "project")}
	if err := applyGitHubSessionCapability(&cfg, sessionModeNative); err != nil {
		t.Fatalf("applyGitHubSessionCapability: %v", err)
	}

	if cfg.HarnessEnv["GH_TOKEN"] != "example-github-token" {
		t.Fatalf("GH_TOKEN grant = %q, want stored token", cfg.HarnessEnv["GH_TOKEN"])
	}
	if _, ok := cfg.HarnessEnv["GITHUB_TOKEN"]; ok {
		t.Fatalf("GITHUB_TOKEN should not be injected by the explicit GitHub capability")
	}
	if len(cfg.CredentialEnvGrants) != 1 {
		t.Fatalf("CredentialEnvGrants = %v, want one grant", cfg.CredentialEnvGrants)
	}
	grant := cfg.CredentialEnvGrants[0]
	if grant.EnvVar != "GH_TOKEN" || grant.CredentialID != credentialGitHubAPIToken || grant.Source != "host secret store" {
		t.Fatalf("CredentialEnvGrants[0] = %+v", grant)
	}
	if len(cfg.ServiceAccess) != 1 || cfg.ServiceAccess[0] != githubServiceAccess {
		t.Fatalf("ServiceAccess = %v, want [%s]", cfg.ServiceAccess, githubServiceAccess)
	}
	if got := renderSessionContract(cfg, sessionModeNative, true); !strings.Contains(got, "GH_TOKEN=<redacted> (github.api-token, host secret store)") {
		t.Fatalf("session contract missing redacted GitHub grant:\n%s", got)
	}
	preview := buildExplainJSON("shell", cfg, sessionModeNative, true)
	if len(preview.CredentialEnvGrants) != 1 ||
		preview.CredentialEnvGrants[0].EnvVar != "GH_TOKEN" ||
		preview.CredentialEnvGrants[0].CredentialID != string(credentialGitHubAPIToken) ||
		!preview.CredentialEnvGrants[0].Redacted {
		t.Fatalf("explain JSON credential grants = %#v", preview.CredentialEnvGrants)
	}
}

func TestApplyGitHubSessionCapabilityFailsClosedWhenUnavailable(t *testing.T) {
	withGitHubTokenHome(t)
	cfg := sessionConfig{ProjectDir: filepath.Join(t.TempDir(), "project")}
	err := applyGitHubSessionCapability(&cfg, sessionModeNative)
	if err == nil {
		t.Fatal("applyGitHubSessionCapability succeeded without a configured token")
	}
	if !strings.Contains(err.Error(), "hazmat config github --token-from-env") {
		t.Fatalf("error should point to configuration command, got: %v", err)
	}
	if len(cfg.HarnessEnv) != 0 || len(cfg.CredentialEnvGrants) != 0 {
		t.Fatalf("capability should not partially mutate config on missing token: %+v", cfg)
	}
}

func TestApplyGitHubSessionCapabilityRejectsDockerSandbox(t *testing.T) {
	withGitHubTokenHome(t)
	if err := saveGitHubStoredToken("example-github-token"); err != nil {
		t.Fatalf("saveGitHubStoredToken: %v", err)
	}

	cfg := sessionConfig{ProjectDir: filepath.Join(t.TempDir(), "project")}
	err := applyGitHubSessionCapability(&cfg, sessionModeDockerSandbox)
	if err == nil {
		t.Fatal("applyGitHubSessionCapability succeeded for Docker Sandbox")
	}
	if !strings.Contains(err.Error(), "not supported for Docker Sandbox sessions") {
		t.Fatalf("unexpected Docker Sandbox error: %v", err)
	}
	if len(cfg.HarnessEnv) != 0 || len(cfg.CredentialEnvGrants) != 0 {
		t.Fatalf("capability should not mutate config for unsupported mode: %+v", cfg)
	}
}
