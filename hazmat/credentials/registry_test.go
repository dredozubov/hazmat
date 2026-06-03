package credentials

import (
	"path/filepath"
	"testing"
)

func TestBuiltinDescriptorsUseSuppliedPaths(t *testing.T) {
	paths := testRegistryPaths(t)

	openAI := MustDescriptor(paths, ProviderOpenAIAPIKey)
	if len(openAI.LegacyPaths) != 1 || openAI.LegacyPaths[0] != paths.AgentZshrcPath {
		t.Fatalf("OpenAI legacy paths = %v, want %s", openAI.LegacyPaths, paths.AgentZshrcPath)
	}

	claudeKeychain := MustDescriptor(paths, HarnessClaudeKeychain)
	wantKeychainRef := filepath.Join(paths.AgentHome, paths.AgentLoginKeychainRelPath)
	if claudeKeychain.ExternalRef != wantKeychainRef {
		t.Fatalf("Claude keychain external ref = %q, want %q", claudeKeychain.ExternalRef, wantKeychainRef)
	}

	gitHTTPS := MustDescriptor(paths, GitHTTPSAgentStore)
	if len(gitHTTPS.LegacyPaths) != 1 || gitHTTPS.LegacyPaths[0] != paths.GitHTTPSAgentCredentialsPath {
		t.Fatalf("Git HTTPS legacy paths = %v, want %s", gitHTTPS.LegacyPaths, paths.GitHTTPSAgentCredentialsPath)
	}

	cloudSecret := MustDescriptor(paths, CloudS3SecretKey)
	if len(cloudSecret.LegacyPaths) != 1 || cloudSecret.LegacyPaths[0] != paths.CloudCredentialPath {
		t.Fatalf("cloud secret legacy paths = %v, want %s", cloudSecret.LegacyPaths, paths.CloudCredentialPath)
	}
}

func TestBuiltinDescriptorsReturnIndependentSlices(t *testing.T) {
	paths := testRegistryPaths(t)
	descriptors := BuiltinDescriptors(paths)
	descriptors[0].ConsumerHarnesses[0] = HarnessQwen
	descriptors[0].LegacyPaths[0] = "mutated"

	fresh := MustDescriptor(paths, ProviderAnthropicAPIKey)
	if got := fresh.ConsumerHarnessIDs()[0]; got != HarnessClaude {
		t.Fatalf("fresh Anthropic first consumer = %q, want %q", got, HarnessClaude)
	}
	if got := fresh.LegacyPaths[0]; got != paths.AgentZshrcPath {
		t.Fatalf("fresh Anthropic legacy path = %q, want %q", got, paths.AgentZshrcPath)
	}
}

func TestMaterializedPathMustStayUnderAgentHome(t *testing.T) {
	descriptor := Descriptor{
		ID:        HarnessCodexAuth,
		Delivery:  DeliveryMaterializedFile,
		AgentPath: filepath.Join(t.TempDir(), "outside", "auth.json"),
		agentHome: filepath.Join(t.TempDir(), "agent"),
	}

	if _, err := descriptor.AgentMaterializationPath(); err == nil {
		t.Fatal("AgentMaterializationPath accepted path outside agent home")
	}
}

func TestStorePathForConfigUsesConfigDirectory(t *testing.T) {
	paths := testRegistryPaths(t)
	got, err := StorePathForConfig(paths, GitSSHProvisionedIdentity)
	if err != nil {
		t.Fatalf("StorePathForConfig: %v", err)
	}

	want := filepath.Join(filepath.Dir(paths.ConfigFilePath), "secrets", "git-ssh", "provisioned")
	if got != want {
		t.Fatalf("StorePathForConfig = %q, want %q", got, want)
	}
}

func testRegistryPaths(t *testing.T) RegistryPaths {
	t.Helper()

	root := t.TempDir()
	agentHome := filepath.Join(root, "agent")
	hostHome := filepath.Join(root, "host")
	return RegistryPaths{
		AgentHome:                    agentHome,
		AgentZshrcPath:               filepath.Join(agentHome, ".zshrc.custom"),
		AgentLoginKeychainRelPath:    filepath.Join("Library", "Keychains", "login.keychain-db"),
		ConfigFilePath:               filepath.Join(hostHome, ".hazmat", "config.yaml"),
		CloudCredentialPath:          filepath.Join(hostHome, ".hazmat", "cloud-credentials"),
		GitHTTPSAgentCredentialsPath: filepath.Join(agentHome, ".config", "git", "credentials"),
	}
}
