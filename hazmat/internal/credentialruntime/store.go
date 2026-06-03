package credentialruntime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hazmat/credentials"
)

func CloudCredentialStorePath(paths credentials.RegistryPaths, id credentials.ID) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory for Hazmat cloud credentials: %w", err)
	}
	return credentials.StorePathForHome(paths, home, id)
}

func ReadCloudStoredCredential(paths credentials.RegistryPaths, id credentials.ID) (string, bool, error) {
	path, err := CloudCredentialStorePath(paths, id)
	if err != nil {
		return "", false, err
	}
	raw, ok, err := ReadHostStoredSecretFile(path)
	if err != nil || !ok {
		return "", ok, err
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

func SaveCloudStoredCredential(paths credentials.RegistryPaths, id credentials.ID, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s cannot be empty", id)
	}
	path, err := CloudCredentialStorePath(paths, id)
	if err != nil {
		return err
	}
	return WriteHostStoredSecretFile(path, []byte(value+"\n"))
}

func ReadHostStoredSecretFile(path string) ([]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return raw, true, nil
}

func WriteHostStoredSecretFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
