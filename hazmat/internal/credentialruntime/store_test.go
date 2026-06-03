package credentialruntime

import (
	"os"
	"path/filepath"
	"testing"

	"hazmat/credentials"
)

func TestCloudStoredCredentialRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := testRegistryPaths(home)

	if err := SaveCloudStoredCredential(paths, credentials.CloudS3SecretKey, " secret-value \n"); err != nil {
		t.Fatalf("SaveCloudStoredCredential: %v", err)
	}

	got, ok, err := ReadCloudStoredCredential(paths, credentials.CloudS3SecretKey)
	if err != nil {
		t.Fatalf("ReadCloudStoredCredential: %v", err)
	}
	if !ok || got != "secret-value" {
		t.Fatalf("ReadCloudStoredCredential = %q, %v; want secret-value, true", got, ok)
	}

	storePath, err := CloudCredentialStorePath(paths, credentials.CloudS3SecretKey)
	if err != nil {
		t.Fatalf("CloudCredentialStorePath: %v", err)
	}
	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read store path: %v", err)
	}
	if string(raw) != "secret-value\n" {
		t.Fatalf("stored raw value = %q, want trimmed line", raw)
	}
}

func TestSaveCloudStoredCredentialRejectsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := SaveCloudStoredCredential(testRegistryPaths(home), credentials.CloudS3SecretKey, " \n\t"); err == nil {
		t.Fatal("SaveCloudStoredCredential accepted empty value")
	}
}

func TestReadCloudStoredCredentialTreatsBlankFileAsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := testRegistryPaths(home)
	storePath, err := CloudCredentialStorePath(paths, credentials.CloudS3SecretKey)
	if err != nil {
		t.Fatalf("CloudCredentialStorePath: %v", err)
	}
	if err := WriteHostStoredSecretFile(storePath, []byte(" \n")); err != nil {
		t.Fatalf("WriteHostStoredSecretFile: %v", err)
	}

	got, ok, err := ReadCloudStoredCredential(paths, credentials.CloudS3SecretKey)
	if err != nil {
		t.Fatalf("ReadCloudStoredCredential: %v", err)
	}
	if ok || got != "" {
		t.Fatalf("ReadCloudStoredCredential = %q, %v; want missing blank credential", got, ok)
	}
}

func testRegistryPaths(home string) credentials.RegistryPaths {
	return credentials.RegistryPaths{
		AgentHome:                    filepath.Join(home, "agent"),
		AgentZshrcPath:               filepath.Join(home, "agent", ".zshrc"),
		AgentLoginKeychainRelPath:    filepath.Join("Library", "Keychains", "login.keychain-db"),
		ConfigFilePath:               filepath.Join(home, ".hazmat", "config.yaml"),
		CloudCredentialPath:          filepath.Join(home, ".hazmat", "cloud-credentials"),
		GitHTTPSAgentCredentialsPath: filepath.Join(home, "agent", ".config", "git", "credentials"),
	}
}
