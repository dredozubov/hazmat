package hazmat

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

func TestPrepareSharedBrokerRuntimeDirUsesStickyRootAndAgentSharedLeaf(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-runtime")
	savedRoot := brokerRuntimeRoot
	brokerRuntimeRoot = root
	t.Cleanup(func() {
		brokerRuntimeRoot = savedRoot
	})

	var ensuredPath string
	var ensuredMode os.FileMode
	savedEnsure := brokerRuntimeAgentEnsureSharedDir
	brokerRuntimeAgentEnsureSharedDir = func(path string, mode os.FileMode) error {
		ensuredPath = path
		ensuredMode = mode
		return nil
	}
	t.Cleanup(func() {
		brokerRuntimeAgentEnsureSharedDir = savedEnsure
	})

	runtimeDir, err := prepareSharedBrokerRuntimeDir("git-https")
	if err != nil {
		t.Fatalf("prepareSharedBrokerRuntimeDir: %v", err)
	}
	if !strings.HasPrefix(runtimeDir, root+string(os.PathSeparator)+"git-https-") {
		t.Fatalf("runtimeDir = %s, want child of %s with git-https prefix", runtimeDir, root)
	}
	if ensuredPath != runtimeDir || ensuredMode != 0o2770 {
		t.Fatalf("agent shared dir ensure = %s %04o, want %s 2770", ensuredPath, ensuredMode, runtimeDir)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat broker root: %v", err)
	}
	if info.Mode().Perm() != 0o733 || info.Mode()&os.ModeSticky == 0 {
		t.Fatalf("broker root mode = %s, want sticky 0733", info.Mode())
	}
}

func TestBuildGitHTTPSCredentialHelperCommandUsesBrokerCommand(t *testing.T) {
	got := buildGitHTTPSCredentialHelperCommand("/Applications/Hazmat App/hazmat", "/tmp/hazmat sock")
	for _, want := range []string{
		"!",
		"'/Applications/Hazmat App/hazmat'",
		"_git_https_credential",
		"'/tmp/hazmat sock'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("helper command missing %q:\n%s", want, got)
		}
	}
	if !strings.HasPrefix(got, "!") {
		t.Fatalf("helper command = %q, want Git shell helper prefix", got)
	}
	if strings.Contains(got, "HAZMAT_GIT_HTTPS_CREDENTIAL_SOCKET") {
		t.Fatalf("helper command unexpectedly uses socket environment:\n%s", got)
	}
}

func TestPrepareGitHTTPSCredentialRuntimeUsesInlineHelperCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	savedLegacyPath := gitHTTPSAgentCredentialsPath
	gitHTTPSAgentCredentialsPath = filepath.Join(home, "agent", ".config", "git", "credentials")
	t.Cleanup(func() {
		gitHTTPSAgentCredentialsPath = savedLegacyPath
	})

	savedRoot := brokerRuntimeRoot
	root, err := os.MkdirTemp("/tmp", "hghrt-*")
	if err != nil {
		t.Fatal(err)
	}
	brokerRuntimeRoot = root
	t.Cleanup(func() {
		_ = os.RemoveAll(root)
		brokerRuntimeRoot = savedRoot
	})

	savedEnsure := brokerRuntimeAgentEnsureSharedDir
	brokerRuntimeAgentEnsureSharedDir = func(path string, mode os.FileMode) error {
		if err := os.MkdirAll(path, mode.Perm()); err != nil {
			return err
		}
		return os.Chmod(path, mode.Perm())
	}
	t.Cleanup(func() {
		brokerRuntimeAgentEnsureSharedDir = savedEnsure
	})

	savedExecutable := gitHTTPSExecutablePath
	gitHTTPSExecutablePath = func() (string, error) {
		return "/usr/local/bin/hazmat", nil
	}
	t.Cleanup(func() {
		gitHTTPSExecutablePath = savedExecutable
	})

	runtime, err := prepareGitHTTPSCredentialRuntime()
	if err != nil {
		t.Fatalf("prepareGitHTTPSCredentialRuntime: %v", err)
	}
	defer runtime.Cleanup()

	env := map[string]string{}
	for _, pair := range runtime.EnvPairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			t.Fatalf("env pair %q is missing =", pair)
		}
		env[key] = value
	}

	if got := env["GIT_CONFIG_COUNT"]; got != "2" {
		t.Fatalf("GIT_CONFIG_COUNT = %q, want 2", got)
	}
	if got := env["GIT_CONFIG_KEY_0"]; got != "credential.helper" {
		t.Fatalf("GIT_CONFIG_KEY_0 = %q, want credential.helper", got)
	}
	if got := env["GIT_CONFIG_VALUE_0"]; got != "" {
		t.Fatalf("GIT_CONFIG_VALUE_0 = %q, want empty reset", got)
	}
	if got := env["GIT_CONFIG_KEY_1"]; got != "credential.helper" {
		t.Fatalf("GIT_CONFIG_KEY_1 = %q, want credential.helper", got)
	}
	helperCommand := env["GIT_CONFIG_VALUE_1"]
	if !strings.HasPrefix(helperCommand, "!/usr/local/bin/hazmat _git_https_credential ") {
		t.Fatalf("GIT_CONFIG_VALUE_1 = %q, want inline Git shell helper", helperCommand)
	}
	if strings.Contains(strings.Join(runtime.EnvPairs, "\n"), "HAZMAT_GIT_HTTPS_CREDENTIAL_SOCKET") {
		t.Fatalf("runtime env unexpectedly exposes socket path:\n%s", strings.Join(runtime.EnvPairs, "\n"))
	}
}
