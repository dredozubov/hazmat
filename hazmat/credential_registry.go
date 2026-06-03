package hazmat

import "hazmat/credentials"

type credentialID = credentials.ID
type credentialKind = credentials.Kind
type credentialStorageBackend = credentials.StorageBackend
type credentialDeliveryMode = credentials.DeliveryMode
type credentialSupportStatus = credentials.SupportStatus
type credentialDescriptor = credentials.Descriptor
type credentialRegistrySummary = credentials.RegistrySummary

const (
	credentialKindProviderAPIKey credentialKind = credentials.KindProviderAPIKey
	credentialKindHarnessAuth    credentialKind = credentials.KindHarnessAuth
	credentialKindGitHTTPS       credentialKind = credentials.KindGitHTTPS
	credentialKindGitSSHIdentity credentialKind = credentials.KindGitSSHIdentity
	credentialKindCloudBackup    credentialKind = credentials.KindCloudBackup
	credentialKindGitHubToken    credentialKind = credentials.KindGitHubToken
	credentialKindIntegrationEnv credentialKind = credentials.KindIntegrationEnv
	credentialKindExternalAuth   credentialKind = credentials.KindExternalAuth
)

const (
	credentialStorageHostSecretStore credentialStorageBackend = credentials.StorageHostSecretStore
	credentialStorageExternalFile    credentialStorageBackend = credentials.StorageExternalFile
	credentialStorageKeychain        credentialStorageBackend = credentials.StorageKeychain
	credentialStorageBroker          credentialStorageBackend = credentials.StorageBroker
)

const (
	credentialDeliveryNone              credentialDeliveryMode = credentials.DeliveryNone
	credentialDeliveryEnv               credentialDeliveryMode = credentials.DeliveryEnv
	credentialDeliveryMaterializedFile  credentialDeliveryMode = credentials.DeliveryMaterializedFile
	credentialDeliveryBrokeredHelper    credentialDeliveryMode = credentials.DeliveryBrokeredHelper
	credentialDeliveryExternalReference credentialDeliveryMode = credentials.DeliveryExternalReference
)

const (
	credentialSupportManaged         credentialSupportStatus = credentials.SupportManaged
	credentialSupportExternal        credentialSupportStatus = credentials.SupportExternal
	credentialSupportAdapterRequired credentialSupportStatus = credentials.SupportAdapterRequired
)

const (
	credentialProviderAnthropicAPIKey  credentialID = credentials.ProviderAnthropicAPIKey
	credentialProviderOpenAIAPIKey     credentialID = credentials.ProviderOpenAIAPIKey
	credentialProviderGeminiAPIKey     credentialID = credentials.ProviderGeminiAPIKey
	credentialProviderOpenRouterAPIKey credentialID = credentials.ProviderOpenRouterAPIKey

	credentialHarnessClaudeCredentials credentialID = credentials.HarnessClaudeCredentials
	credentialHarnessClaudeState       credentialID = credentials.HarnessClaudeState
	credentialHarnessClaudeKeychain    credentialID = credentials.HarnessClaudeKeychain
	credentialHarnessCodexAuth         credentialID = credentials.HarnessCodexAuth
	credentialHarnessOpenCodeAuth      credentialID = credentials.HarnessOpenCodeAuth
	credentialHarnessGeminiOAuth       credentialID = credentials.HarnessGeminiOAuth
	credentialHarnessGeminiAccounts    credentialID = credentials.HarnessGeminiAccounts
	credentialHarnessGeminiKeychain    credentialID = credentials.HarnessGeminiKeychain

	credentialGitSSHExternalIdentity    credentialID = credentials.GitSSHExternalIdentity
	credentialGitSSHProvisionedIdentity credentialID = credentials.GitSSHProvisionedIdentity
	credentialGitHTTPSAgentStore        credentialID = credentials.GitHTTPSAgentStore
	credentialGitHubAPIToken            credentialID = credentials.GitHubAPIToken

	credentialCloudS3AccessKeyID credentialID = credentials.CloudS3AccessKeyID
	credentialCloudS3SecretKey   credentialID = credentials.CloudS3SecretKey
	credentialCloudKopiaRecovery credentialID = credentials.CloudKopiaRecovery
)

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

func builtinCredentialDescriptors() []credentialDescriptor {
	return credentials.BuiltinDescriptors(credentialRegistryPaths())
}

func summarizeCredentialRegistry(descriptors []credentialDescriptor) credentialRegistrySummary {
	return credentials.SummarizeRegistry(descriptors)
}

func findCredentialDescriptor(id credentialID) (credentialDescriptor, bool) {
	return credentials.FindDescriptor(credentialRegistryPaths(), id)
}

func mustCredentialDescriptor(id credentialID) credentialDescriptor {
	return credentials.MustDescriptor(credentialRegistryPaths(), id)
}

func providerCredentialDescriptorForEnvVar(envVar string) (credentialDescriptor, bool) {
	return credentials.ProviderDescriptorForEnvVar(credentialRegistryPaths(), envVar)
}

func providerCredentialDescriptorForEnvVarAndHarness(envVar string, harness HarnessID) (credentialDescriptor, bool) {
	return credentials.ProviderDescriptorForEnvVarAndHarness(credentialRegistryPaths(), envVar, harness)
}

func providerCredentialDescriptorsForHarness(harness HarnessID) []credentialDescriptor {
	return credentials.ProviderDescriptorsForHarness(credentialRegistryPaths(), harness)
}

func credentialDescriptorsForHarnessLifecycle(id HarnessID) []credentialDescriptor {
	return credentials.DescriptorsForHarnessLifecycle(credentialRegistryPaths(), id)
}

func providerSecretStorePathForDescriptor(home string, descriptor credentialDescriptor) (string, error) {
	return credentials.ProviderSecretStorePathForDescriptor(home, descriptor)
}

func credentialStorePathForHome(home string, id credentialID) (string, error) {
	return credentials.StorePathForHome(credentialRegistryPaths(), home, id)
}

func mustCredentialStorePathForHome(home string, id credentialID) string {
	storePath, err := credentialStorePathForHome(home, id)
	if err != nil {
		panic(err)
	}
	return storePath
}

func credentialStorePathForConfig(id credentialID) (string, error) {
	return credentials.StorePathForConfig(credentialRegistryPaths(), id)
}

func mustCredentialStorePathForConfig(id credentialID) string {
	storePath, err := credentialStorePathForConfig(id)
	if err != nil {
		panic(err)
	}
	return storePath
}

func cleanCredentialStoreRelPath(relPath string) (string, error) {
	return credentials.CleanStoreRelPath(relPath)
}
