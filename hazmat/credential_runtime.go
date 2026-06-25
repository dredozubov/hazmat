package hazmat

import (
	"hazmat/credentials"
	"hazmat/internal/credentialruntime"
)

func cloudCredentialStorePath(id credentials.ID) (string, error) {
	return credentialruntime.CloudCredentialStorePath(credentialRegistryPaths(), id)
}

func readCloudStoredCredential(id credentials.ID) (string, bool, error) {
	return credentialruntime.ReadCloudStoredCredential(credentialRegistryPaths(), id)
}

func saveCloudStoredCredential(id credentials.ID, value string) error {
	return credentialruntime.SaveCloudStoredCredential(credentialRegistryPaths(), id, value)
}

func readHostStoredSecretFile(path string) ([]byte, bool, error) {
	return credentialruntime.ReadHostStoredSecretFile(path)
}

func writeHostStoredSecretFile(path string, raw []byte) error {
	return credentialruntime.WriteHostStoredSecretFile(path, raw)
}
