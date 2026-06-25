package hazmat

import "hazmat/credentials"

// This file is the hazmat-package binding of the single credential registry.
// The registry data, types, and constants all live in the pure leaf package
// hazmat/credentials (it cannot import hazmat without a cycle). Package hazmat
// refers to those types/constants directly as credentials.X; this file only
// supplies the runtime paths (credentialRegistryPaths) and the thin
// path-injecting wrappers below, so callers do not have to thread RegistryPaths
// through every query. There is exactly one registry — adding a credential
// means editing credentials.builtinRegistry, nothing here.

func credentialRegistryPaths() credentials.RegistryPaths {
	return credentials.RegistryPaths{
		AgentHome:                    agentHome,
		AgentZshrcPath:               agentZshrcPath,
		AgentLoginKeychainRelPath:    agentLoginKeychainRelPath,
		ConfigFilePath:               configFilePath,
		CloudCredentialPath:          cloudCredentialPath,
		GitHTTPSAgentCredentialsPath: gitHTTPSAgentCredentialsPath,
	}
}

func builtinCredentialDescriptors() []credentials.Descriptor {
	return credentials.BuiltinDescriptors(credentialRegistryPaths())
}

// secretStoreDirForHome binds credentials.SecretStoreDirForHome so the
// host-secret-store root (~/.hazmat/secrets) has a single definition shared by
// the registry and the secret-store IO layer.
func secretStoreDirForHome(home string) string {
	return credentials.SecretStoreDirForHome(home)
}

func summarizeCredentialRegistry(descriptors []credentials.Descriptor) credentials.RegistrySummary {
	return credentials.SummarizeRegistry(descriptors)
}

func findCredentialDescriptor(id credentials.ID) (credentials.Descriptor, bool) {
	return credentials.FindDescriptor(credentialRegistryPaths(), id)
}

func mustCredentialDescriptor(id credentials.ID) credentials.Descriptor {
	return credentials.MustDescriptor(credentialRegistryPaths(), id)
}

func providerCredentialDescriptorForEnvVar(envVar string) (credentials.Descriptor, bool) {
	return credentials.ProviderDescriptorForEnvVar(credentialRegistryPaths(), envVar)
}

func providerCredentialDescriptorForEnvVarAndHarness(envVar string, harness HarnessID) (credentials.Descriptor, bool) {
	return credentials.ProviderDescriptorForEnvVarAndHarness(credentialRegistryPaths(), envVar, harness)
}

func providerCredentialDescriptorsForHarness(harness HarnessID) []credentials.Descriptor {
	return credentials.ProviderDescriptorsForHarness(credentialRegistryPaths(), harness)
}

func credentialDescriptorsForHarnessLifecycle(id HarnessID) []credentials.Descriptor {
	return credentials.DescriptorsForHarnessLifecycle(credentialRegistryPaths(), id)
}

func providerSecretStorePathForDescriptor(home string, descriptor credentials.Descriptor) (string, error) {
	return credentials.ProviderSecretStorePathForDescriptor(home, descriptor)
}

func credentialStorePathForHome(home string, id credentials.ID) (string, error) {
	return credentials.StorePathForHome(credentialRegistryPaths(), home, id)
}

func mustCredentialStorePathForHome(home string, id credentials.ID) string {
	storePath, err := credentialStorePathForHome(home, id)
	if err != nil {
		panic(err)
	}
	return storePath
}

func credentialStorePathForConfig(id credentials.ID) (string, error) {
	return credentials.StorePathForConfig(credentialRegistryPaths(), id)
}

func mustCredentialStorePathForConfig(id credentials.ID) string {
	storePath, err := credentialStorePathForConfig(id)
	if err != nil {
		panic(err)
	}
	return storePath
}

func cleanCredentialStoreRelPath(relPath string) (string, error) {
	return credentials.CleanStoreRelPath(relPath)
}
