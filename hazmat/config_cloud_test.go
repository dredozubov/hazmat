package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolateCloudCredentialConfig(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	savedConfigPath := configFilePath
	savedCloudCredentialPath := cloudCredentialPath
	configFilePath = filepath.Join(home, ".hazmat", "config.yaml")
	cloudCredentialPath = filepath.Join(home, ".hazmat", "cloud-credentials")
	t.Cleanup(func() {
		configFilePath = savedConfigPath
		cloudCredentialPath = savedCloudCredentialPath
	})
	return home
}

func readStoredCloudCredentialForTest(t *testing.T, home string, id credentialID) string {
	t.Helper()

	raw, err := os.ReadFile(mustCredentialStorePathForHome(home, id))
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return strings.TrimSpace(string(raw))
}

func TestRunConfigCloudStoresSecretsOutsideConfig(t *testing.T) {
	home := isolateCloudCredentialConfig(t)
	t.Setenv("HAZMAT_CLOUD_SECRET_KEY", "cloud-secret-key")
	t.Setenv("HAZMAT_CLOUD_PASSWORD", "cloud-recovery-key")

	if err := runConfigCloud("s3.example.com", "hazmat-backups", "cloud-access-key", true); err != nil {
		t.Fatalf("runConfigCloud: %v", err)
	}

	raw, err := os.ReadFile(configFilePath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	configText := string(raw)
	for _, forbidden := range []string{"cloud-access-key", "cloud-secret-key", "cloud-recovery-key", "access_key:", "recovery_key:", "password:"} {
		if strings.Contains(configText, forbidden) {
			t.Fatalf("config contains %q after cloud setup:\n%s", forbidden, configText)
		}
	}

	if got := readStoredCloudCredentialForTest(t, home, credentialCloudS3AccessKeyID); got != "cloud-access-key" {
		t.Fatalf("stored access key = %q", got)
	}
	if got := readStoredCloudCredentialForTest(t, home, credentialCloudS3SecretKey); got != "cloud-secret-key" {
		t.Fatalf("stored secret key = %q", got)
	}
	if got := readStoredCloudCredentialForTest(t, home, credentialCloudKopiaRecovery); got != "cloud-recovery-key" {
		t.Fatalf("stored recovery key = %q", got)
	}

	t.Setenv("HAZMAT_CLOUD_PASSWORD", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Backup.Cloud == nil {
		t.Fatal("cloud config missing")
	}
	if cfg.Backup.Cloud.AccessKey != "cloud-access-key" {
		t.Fatalf("loaded access key = %q", cfg.Backup.Cloud.AccessKey)
	}
	if cfg.Backup.Cloud.RecoveryKey != "cloud-recovery-key" {
		t.Fatalf("loaded recovery key = %q", cfg.Backup.Cloud.RecoveryKey)
	}
}

func TestLoadConfigMigratesLegacyCloudSecrets(t *testing.T) {
	home := isolateCloudCredentialConfig(t)
	if err := os.MkdirAll(filepath.Dir(configFilePath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyConfig := []byte(`backup:
  cloud:
    endpoint: s3.example.com
    bucket: hazmat-backups
    access_key: legacy-access-key
    password: legacy-recovery-key
`)
	if err := os.WriteFile(configFilePath, legacyConfig, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Backup.Cloud == nil {
		t.Fatal("cloud config missing")
	}
	if cfg.Backup.Cloud.AccessKey != "legacy-access-key" {
		t.Fatalf("loaded access key = %q", cfg.Backup.Cloud.AccessKey)
	}
	if cfg.Backup.Cloud.RecoveryKey != "legacy-recovery-key" {
		t.Fatalf("loaded recovery key = %q", cfg.Backup.Cloud.RecoveryKey)
	}
	if got := readStoredCloudCredentialForTest(t, home, credentialCloudS3AccessKeyID); got != "legacy-access-key" {
		t.Fatalf("stored access key = %q", got)
	}
	if got := readStoredCloudCredentialForTest(t, home, credentialCloudKopiaRecovery); got != "legacy-recovery-key" {
		t.Fatalf("stored recovery key = %q", got)
	}

	raw, err := os.ReadFile(configFilePath)
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	configText := string(raw)
	for _, forbidden := range []string{"legacy-access-key", "legacy-recovery-key", "access_key:", "recovery_key:", "password:"} {
		if strings.Contains(configText, forbidden) {
			t.Fatalf("migrated config still contains %q:\n%s", forbidden, configText)
		}
	}
}

func TestLoadCloudSecretKeyMigratesLegacyCredentialFile(t *testing.T) {
	home := isolateCloudCredentialConfig(t)
	if err := os.MkdirAll(filepath.Dir(cloudCredentialPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cloudCredentialPath, []byte("legacy-secret-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadCloudSecretKey()
	if err != nil {
		t.Fatalf("loadCloudSecretKey: %v", err)
	}
	if got != "legacy-secret-key" {
		t.Fatalf("loadCloudSecretKey = %q", got)
	}
	if got := readStoredCloudCredentialForTest(t, home, credentialCloudS3SecretKey); got != "legacy-secret-key" {
		t.Fatalf("stored secret key = %q", got)
	}
	if _, err := os.Stat(cloudCredentialPath); !os.IsNotExist(err) {
		t.Fatalf("legacy cloud credential still exists or stat failed: %v", err)
	}
}
