package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolateCredentialMigrationTest(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	savedConfigPath := configFilePath
	savedCloudCredentialPath := cloudCredentialPath
	savedAgentZshrcPath := agentZshrcPath
	savedGitHTTPSAgentCredentialsPath := gitHTTPSAgentCredentialsPath
	savedGitHTTPSAgentGitConfigPath := gitHTTPSAgentGitConfigPath
	savedHarnesses := credentialMigrationHarnesses
	savedArtifactsForHome := credentialMigrationHarnessAuthArtifactsForHome
	savedConflictNow := harnessAuthConflictNow

	configFilePath = filepath.Join(home, ".hazmat", "config.yaml")
	cloudCredentialPath = filepath.Join(home, ".hazmat", "cloud-credentials")
	agentZshrcPath = filepath.Join(home, "agent", ".zshrc")
	gitHTTPSAgentCredentialsPath = filepath.Join(home, "agent", ".config", "git", "credentials")
	gitHTTPSAgentGitConfigPath = filepath.Join(home, "agent", ".gitconfig")
	credentialMigrationHarnesses = nil
	credentialMigrationHarnessAuthArtifactsForHome = harnessAuthArtifactsForHome
	harnessAuthConflictNow = func() time.Time {
		return time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	}

	t.Cleanup(func() {
		configFilePath = savedConfigPath
		cloudCredentialPath = savedCloudCredentialPath
		agentZshrcPath = savedAgentZshrcPath
		gitHTTPSAgentCredentialsPath = savedGitHTTPSAgentCredentialsPath
		gitHTTPSAgentGitConfigPath = savedGitHTTPSAgentGitConfigPath
		credentialMigrationHarnesses = savedHarnesses
		credentialMigrationHarnessAuthArtifactsForHome = savedArtifactsForHome
		harnessAuthConflictNow = savedConflictNow
	})

	return home
}

func TestMigrateCredentialsMovesLegacyProviderExportWithoutLeakingValue(t *testing.T) {
	home := isolateCredentialMigrationTest(t)
	const secret = "synthetic-provider-secret-do-not-print"

	if err := os.MkdirAll(filepath.Dir(agentZshrcPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentZshrcPath, []byte("export OPENAI_API_KEY="+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runMigrateCredentials(migrateCredentialsOptions{Home: home, Writer: &out}); err != nil {
		t.Fatalf("runMigrateCredentials: %v", err)
	}

	if strings.Contains(out.String(), secret) {
		t.Fatalf("migration output leaked provider secret:\n%s", out.String())
	}
	storePath := mustCredentialStorePathForHome(home, credentialProviderOpenAIAPIKey)
	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read provider store: %v", err)
	}
	if strings.TrimSpace(string(raw)) != secret {
		t.Fatalf("provider store = %q, want migrated secret", strings.TrimSpace(string(raw)))
	}
	zshrc, err := os.ReadFile(agentZshrcPath)
	if err != nil {
		t.Fatalf("read zshrc: %v", err)
	}
	if strings.Contains(string(zshrc), "OPENAI_API_KEY") {
		t.Fatalf("legacy provider export was not removed:\n%s", string(zshrc))
	}
}

func TestMigrateCredentialsDryRunDoesNotModifyLegacyProviderExport(t *testing.T) {
	home := isolateCredentialMigrationTest(t)
	const secret = "synthetic-provider-dry-run-secret"

	if err := os.MkdirAll(filepath.Dir(agentZshrcPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentZshrcPath, []byte("export OPENAI_API_KEY="+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runMigrateCredentials(migrateCredentialsOptions{Home: home, Writer: &out, DryRun: true}); err != nil {
		t.Fatalf("runMigrateCredentials dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "would migrate legacy OPENAI_API_KEY") {
		t.Fatalf("dry-run output missing provider plan:\n%s", out.String())
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("dry-run output leaked provider secret:\n%s", out.String())
	}
	if _, err := os.Stat(mustCredentialStorePathForHome(home, credentialProviderOpenAIAPIKey)); !os.IsNotExist(err) {
		t.Fatalf("provider store was created during dry-run: %v", err)
	}
	zshrc, err := os.ReadFile(agentZshrcPath)
	if err != nil {
		t.Fatalf("read zshrc: %v", err)
	}
	if !strings.Contains(string(zshrc), secret) {
		t.Fatalf("legacy provider export changed during dry-run:\n%s", string(zshrc))
	}
}

func TestMigrateCredentialsArchivesDivergentProviderExport(t *testing.T) {
	home := isolateCredentialMigrationTest(t)
	const hostSecret = "synthetic-provider-host-secret"
	const legacySecret = "synthetic-provider-legacy-secret"

	storePath := mustCredentialStorePathForHome(home, credentialProviderOpenAIAPIKey)
	if err := writeHostStoredSecretFile(storePath, []byte(hostSecret+"\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(agentZshrcPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentZshrcPath, []byte("export OPENAI_API_KEY="+legacySecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runMigrateCredentials(migrateCredentialsOptions{Home: home, Writer: &out}); err != nil {
		t.Fatalf("runMigrateCredentials: %v", err)
	}
	for _, forbidden := range []string{hostSecret, legacySecret} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("migration output leaked %q:\n%s", forbidden, out.String())
		}
	}

	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read provider store: %v", err)
	}
	if strings.TrimSpace(string(raw)) != hostSecret {
		t.Fatalf("provider store = %q, want host secret to remain authoritative", strings.TrimSpace(string(raw)))
	}
	conflictPath := filepath.Join(filepath.Dir(storePath), filepath.Base(storePath)+".conflicts", "20260429T120000.000000000Z")
	conflict, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatalf("read provider conflict archive: %v", err)
	}
	if strings.TrimSpace(string(conflict)) != legacySecret {
		t.Fatalf("provider conflict archive = %q, want legacy secret", strings.TrimSpace(string(conflict)))
	}
}

func TestMigrateCredentialsHarvestsHarnessAuthResidue(t *testing.T) {
	home := isolateCredentialMigrationTest(t)
	const token = `{"token":"harness-secret-do-not-print"}`

	storePath := filepath.Join(home, ".hazmat", "secrets", "codex", "auth.json")
	agentPath := filepath.Join(home, "agent", ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	credentialMigrationHarnesses = []HarnessID{HarnessCodex}
	credentialMigrationHarnessAuthArtifactsForHome = func(id HarnessID, _ string) []harnessAuthArtifact {
		if id != HarnessCodex {
			return nil
		}
		return []harnessAuthArtifact{rawHarnessAuthArtifact("Codex auth file", storePath, agentPath)}
	}

	var out bytes.Buffer
	if err := runMigrateCredentials(migrateCredentialsOptions{Home: home, Writer: &out}); err != nil {
		t.Fatalf("runMigrateCredentials: %v", err)
	}
	if strings.Contains(out.String(), "harness-secret-do-not-print") {
		t.Fatalf("migration output leaked harness auth:\n%s", out.String())
	}
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("agent harness auth still exists: %v", err)
	}
	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read harness store: %v", err)
	}
	if strings.TrimSpace(string(raw)) != token {
		t.Fatalf("harness store = %q, want migrated token", strings.TrimSpace(string(raw)))
	}
}

func TestMigrateCredentialsMigratesGitHTTPSStoreAndHelper(t *testing.T) {
	home := isolateCredentialMigrationTest(t)
	const credential = "https://user:ghp_secret_do_not_print@example.com"

	if err := os.MkdirAll(filepath.Dir(gitHTTPSAgentCredentialsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitHTTPSAgentCredentialsPath, []byte(credential+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(gitHTTPSAgentGitConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	gitConfig := "[credential]\n\thelper = store --file " + gitHTTPSAgentCredentialsPath + "\n\thelper = osxkeychain\n"
	if err := os.WriteFile(gitHTTPSAgentGitConfigPath, []byte(gitConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runMigrateCredentials(migrateCredentialsOptions{Home: home, Writer: &out}); err != nil {
		t.Fatalf("runMigrateCredentials: %v", err)
	}
	if strings.Contains(out.String(), "ghp_secret_do_not_print") {
		t.Fatalf("migration output leaked Git HTTPS credential:\n%s", out.String())
	}
	if _, err := os.Stat(gitHTTPSAgentCredentialsPath); !os.IsNotExist(err) {
		t.Fatalf("legacy Git HTTPS store still exists: %v", err)
	}
	store, err := os.ReadFile(gitHTTPSCredentialStorePathForHome(home))
	if err != nil {
		t.Fatalf("read Git HTTPS host store: %v", err)
	}
	if !strings.Contains(string(store), credential) {
		t.Fatalf("Git HTTPS host store missing migrated credential:\n%s", string(store))
	}
	updatedConfig, err := os.ReadFile(gitHTTPSAgentGitConfigPath)
	if err != nil {
		t.Fatalf("read gitconfig: %v", err)
	}
	if strings.Contains(string(updatedConfig), gitHTTPSAgentCredentialsPath) {
		t.Fatalf("legacy Git HTTPS helper was not removed:\n%s", string(updatedConfig))
	}
	if !strings.Contains(string(updatedConfig), "osxkeychain") {
		t.Fatalf("unrelated Git HTTPS helper was removed:\n%s", string(updatedConfig))
	}
}

func TestMigrateCredentialsMigratesCloudSecretsAndScrubsConfig(t *testing.T) {
	home := isolateCredentialMigrationTest(t)
	const accessKey = "legacy-access-do-not-print"
	const recoveryKey = "legacy-recovery-do-not-print"
	const secretKey = "legacy-secret-do-not-print"

	if err := os.MkdirAll(filepath.Dir(configFilePath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyConfig := []byte(`backup:
  local:
    path: ~/.hazmat/snapshots
  cloud:
    endpoint: s3.example.com
    bucket: hazmat
    access_key: ` + accessKey + `
    password: ` + recoveryKey + `
`)
	if err := os.WriteFile(configFilePath, legacyConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cloudCredentialPath, []byte(secretKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runMigrateCredentials(migrateCredentialsOptions{Home: home, Writer: &out}); err != nil {
		t.Fatalf("runMigrateCredentials: %v", err)
	}
	for _, forbidden := range []string{accessKey, recoveryKey, secretKey} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("migration output leaked %q:\n%s", forbidden, out.String())
		}
	}
	for id, want := range map[credentialID]string{
		credentialCloudS3AccessKeyID: accessKey,
		credentialCloudKopiaRecovery: recoveryKey,
		credentialCloudS3SecretKey:   secretKey,
	} {
		path := mustCredentialStorePathForHome(home, id)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s store: %v", id, err)
		}
		if strings.TrimSpace(string(raw)) != want {
			t.Fatalf("%s store = %q, want migrated secret", id, strings.TrimSpace(string(raw)))
		}
	}
	if _, err := os.Stat(cloudCredentialPath); !os.IsNotExist(err) {
		t.Fatalf("legacy cloud credential file still exists: %v", err)
	}
	updatedConfig, err := os.ReadFile(configFilePath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	for _, forbidden := range []string{accessKey, recoveryKey, "access_key:", "password:"} {
		if strings.Contains(string(updatedConfig), forbidden) {
			t.Fatalf("scrubbed config still contains %q:\n%s", forbidden, string(updatedConfig))
		}
	}
}

func TestMigrateCredentialsMovesLegacyProvisionedGitSSHRoot(t *testing.T) {
	home := isolateCredentialMigrationTest(t)
	const privateKey = "private-key-do-not-print"

	legacyKeyDir := filepath.Join(legacyProvisionedSSHKeysRootDir(), "github")
	if err := os.MkdirAll(legacyKeyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyKeyDir, "id_ed25519"), []byte(privateKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyKeyDir, "known_hosts"), []byte("github.com ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runMigrateCredentials(migrateCredentialsOptions{Home: home, Writer: &out}); err != nil {
		t.Fatalf("runMigrateCredentials: %v", err)
	}
	if strings.Contains(out.String(), privateKey) {
		t.Fatalf("migration output leaked SSH private key:\n%s", out.String())
	}
	newKeyPath := filepath.Join(provisionedSSHKeysRootDir(), "github", "id_ed25519")
	raw, err := os.ReadFile(newKeyPath)
	if err != nil {
		t.Fatalf("read migrated provisioned SSH key: %v", err)
	}
	if strings.TrimSpace(string(raw)) != privateKey {
		t.Fatalf("migrated SSH key = %q, want legacy private key", strings.TrimSpace(string(raw)))
	}
	if _, err := os.Stat(legacyProvisionedSSHKeysRootDir()); !os.IsNotExist(err) {
		t.Fatalf("legacy provisioned SSH root still exists: %v", err)
	}
}
