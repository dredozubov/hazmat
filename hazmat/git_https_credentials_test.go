package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeGitCredentialStoreLinesPreservesUniqueEntries(t *testing.T) {
	got := string(mergeGitCredentialStoreLines(
		[]byte("https://alice:one@example.com\nhttps://bob:two@example.org\n"),
		[]byte("https://alice:one@example.com\nhttps://carol:three@example.net\n"),
	))
	want := strings.Join([]string{
		"https://alice:one@example.com",
		"https://bob:two@example.org",
		"https://carol:three@example.net",
	}, "\n")
	if got != want {
		t.Fatalf("mergeGitCredentialStoreLines = %q, want %q", got, want)
	}
}

func TestMigrateLegacyGitHTTPSCredentialsMovesAgentStoreToHostStore(t *testing.T) {
	home := t.TempDir()
	storePath := filepath.Join(home, ".hazmat", "secrets", "git-https", "credentials")
	legacyPath := filepath.Join(home, "agent", ".config", "git", "credentials")

	savedLegacyPath := gitHTTPSAgentCredentialsPath
	gitHTTPSAgentCredentialsPath = legacyPath
	t.Cleanup(func() {
		gitHTTPSAgentCredentialsPath = savedLegacyPath
	})

	if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, []byte("https://host:kept@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("https://legacy:moved@example.org\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	migrated, err := migrateLegacyGitHTTPSCredentials(storePath)
	if err != nil {
		t.Fatalf("migrateLegacyGitHTTPSCredentials: %v", err)
	}
	if !migrated {
		t.Fatal("migrateLegacyGitHTTPSCredentials did not report migration")
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy credentials still exist or stat failed: %v", err)
	}
	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read host store: %v", err)
	}
	for _, want := range []string{"https://host:kept@example.com", "https://legacy:moved@example.org"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("host store missing %q:\n%s", want, string(raw))
		}
	}
}

func TestGitHTTPSCredentialServiceStoreGetErase(t *testing.T) {
	runtimeDir, err := os.MkdirTemp("/tmp", "hgh-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(runtimeDir)
	})
	storePath := filepath.Join(t.TempDir(), "credentials")
	service, err := startGitHTTPSCredentialService(storePath, runtimeDir)
	if err != nil {
		t.Fatalf("startGitHTTPSCredentialService: %v", err)
	}
	defer service.Close()

	payload := []byte(strings.Join([]string{
		"protocol=https",
		"host=example.com",
		"username=alice",
		"password=example-password",
		"",
	}, "\n"))
	if _, err := requestGitHTTPSCredential(service.socketPath, "store", payload); err != nil {
		t.Fatalf("store credential: %v", err)
	}

	resp, err := requestGitHTTPSCredential(service.socketPath, "get", []byte("protocol=https\nhost=example.com\n\n"))
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	got := string(resp.Stdout)
	if !strings.Contains(got, "username=alice") || !strings.Contains(got, "password=example-password") {
		t.Fatalf("get response = %q, want stored username/password", got)
	}

	if _, err := requestGitHTTPSCredential(service.socketPath, "erase", payload); err != nil {
		t.Fatalf("erase credential: %v", err)
	}
	resp, err = requestGitHTTPSCredential(service.socketPath, "get", []byte("protocol=https\nhost=example.com\n\n"))
	if err != nil {
		t.Fatalf("get after erase: %v", err)
	}
	if strings.TrimSpace(string(resp.Stdout)) != "" {
		t.Fatalf("get after erase stdout = %q, want empty", string(resp.Stdout))
	}
}

func TestBuildGitHTTPSCredentialHelperScriptUsesBrokerCommand(t *testing.T) {
	got := buildGitHTTPSCredentialHelperScript("/usr/local/bin/hazmat", "/tmp/hazmat.sock")
	for _, want := range []string{
		"_git_https_credential",
		gitHTTPSCredentialSocketEnv,
		`operation=${1:-get}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("helper script missing %q:\n%s", want, got)
		}
	}
}
