package hazmat

import "hazmat/internal/credentialruntime"

func cloudCredentialStorePath(id credentialID) (string, error) {
	return credentialruntime.CloudCredentialStorePath(credentialRegistryPaths(), id)
}

func readCloudStoredCredential(id credentialID) (string, bool, error) {
	return credentialruntime.ReadCloudStoredCredential(credentialRegistryPaths(), id)
}

func saveCloudStoredCredential(id credentialID, value string) error {
	return credentialruntime.SaveCloudStoredCredential(credentialRegistryPaths(), id, value)
}

func readHostStoredSecretFile(path string) ([]byte, bool, error) {
	return credentialruntime.ReadHostStoredSecretFile(path)
}

func writeHostStoredSecretFile(path string, raw []byte) error {
	return credentialruntime.WriteHostStoredSecretFile(path, raw)
}
