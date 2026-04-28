package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinCredentialDescriptorsAreWellFormed(t *testing.T) {
	home := t.TempDir()
	secretRoot := secretStoreDirForHome(home)
	seenIDs := map[credentialID]bool{}
	seenStorePaths := map[string]credentialID{}

	for _, descriptor := range builtinCredentialDescriptors() {
		if descriptor.ID == "" {
			t.Fatalf("descriptor has empty ID: %+v", descriptor)
		}
		if seenIDs[descriptor.ID] {
			t.Fatalf("duplicate credential ID %q", descriptor.ID)
		}
		seenIDs[descriptor.ID] = true
		if descriptor.DisplayName == "" {
			t.Fatalf("%s has empty display name", descriptor.ID)
		}
		if descriptor.Kind == "" {
			t.Fatalf("%s has empty kind", descriptor.ID)
		}
		if descriptor.Backend == "" {
			t.Fatalf("%s has empty backend", descriptor.ID)
		}
		if descriptor.Delivery == "" {
			t.Fatalf("%s has empty delivery mode", descriptor.ID)
		}
		if descriptor.Support == "" {
			t.Fatalf("%s has empty support status", descriptor.ID)
		}
		if !descriptor.Redacted {
			t.Fatalf("%s must be redacted", descriptor.ID)
		}

		switch descriptor.Backend {
		case credentialStorageHostSecretStore:
			storePath, err := descriptor.StorePathForHome(home)
			if err != nil {
				t.Fatalf("%s StorePathForHome: %v", descriptor.ID, err)
			}
			rel, err := filepath.Rel(secretRoot, storePath)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
				t.Fatalf("%s store path %q is outside secret root %q", descriptor.ID, storePath, secretRoot)
			}
			if previous, exists := seenStorePaths[storePath]; exists {
				t.Fatalf("%s and %s share store path %q", descriptor.ID, previous, storePath)
			}
			seenStorePaths[storePath] = descriptor.ID
		case credentialStorageKeychain, credentialStorageExternalFile, credentialStorageBroker:
			if descriptor.StoreRelPath != "" {
				t.Fatalf("%s non-host backend must not declare host store path %q", descriptor.ID, descriptor.StoreRelPath)
			}
			if _, err := descriptor.StorePathForHome(home); err == nil {
				t.Fatalf("%s non-host backend produced a host store path", descriptor.ID)
			}
		default:
			t.Fatalf("%s has unknown backend %q", descriptor.ID, descriptor.Backend)
		}

		switch descriptor.Delivery {
		case credentialDeliveryEnv:
			envVar, err := descriptor.EnvDeliveryVar()
			if err != nil {
				t.Fatalf("%s EnvDeliveryVar: %v", descriptor.ID, err)
			}
			if !strings.HasSuffix(envVar, "_API_KEY") {
				t.Fatalf("%s env delivery uses unexpected env var %q", descriptor.ID, envVar)
			}
			if descriptor.AgentPath != "" {
				t.Fatalf("%s env delivery must not declare an agent path", descriptor.ID)
			}
		case credentialDeliveryMaterializedFile:
			agentPath, err := descriptor.AgentMaterializationPath()
			if err != nil {
				t.Fatalf("%s AgentMaterializationPath: %v", descriptor.ID, err)
			}
			if !usesManagedAgentPath(agentPath) {
				t.Fatalf("%s materializes outside managed agent home: %q", descriptor.ID, agentPath)
			}
			if !descriptor.ConflictArchive {
				t.Fatalf("%s materialized credential must preserve conflicts", descriptor.ID)
			}
		case credentialDeliveryNone, credentialDeliveryBrokeredHelper, credentialDeliveryExternalReference:
			if descriptor.Delivery == credentialDeliveryExternalReference && descriptor.ExternalRef == "" {
				t.Fatalf("%s external-reference delivery must describe the external authority", descriptor.ID)
			}
			if descriptor.AgentPath != "" {
				t.Fatalf("%s non-file delivery must not declare an agent path", descriptor.ID)
			}
		default:
			t.Fatalf("%s has unknown delivery mode %q", descriptor.ID, descriptor.Delivery)
		}

		switch descriptor.Support {
		case credentialSupportManaged:
			if descriptor.Backend != credentialStorageHostSecretStore {
				t.Fatalf("%s managed credential uses non-host backend %q", descriptor.ID, descriptor.Backend)
			}
		case credentialSupportExternal, credentialSupportAdapterRequired:
			if descriptor.Backend == credentialStorageHostSecretStore {
				t.Fatalf("%s external credential unexpectedly uses host secret store", descriptor.ID)
			}
		default:
			t.Fatalf("%s has unknown support status %q", descriptor.ID, descriptor.Support)
		}
	}
}

func TestProviderSecretStorePathForHomeUsesCredentialRegistry(t *testing.T) {
	home := t.TempDir()
	cases := []struct {
		envVar string
		id     credentialID
	}{
		{"ANTHROPIC_API_KEY", credentialProviderAnthropicAPIKey},
		{"OPENAI_API_KEY", credentialProviderOpenAIAPIKey},
		{"GEMINI_API_KEY", credentialProviderGeminiAPIKey},
	}

	for _, tc := range cases {
		got, err := providerSecretStorePathForHome(home, tc.envVar)
		if err != nil {
			t.Fatalf("providerSecretStorePathForHome(%q): %v", tc.envVar, err)
		}
		want := mustCredentialStorePathForHome(home, tc.id)
		if got != want {
			t.Fatalf("providerSecretStorePathForHome(%q) = %q, want %q", tc.envVar, got, want)
		}
	}

	if _, err := providerSecretStorePathForHome(home, "UNREGISTERED_API_KEY"); err == nil {
		t.Fatalf("providerSecretStorePathForHome accepted unregistered env var")
	}
}

func TestCloudCredentialStorePathsUseCredentialRegistry(t *testing.T) {
	home := t.TempDir()
	for _, id := range []credentialID{
		credentialCloudS3AccessKeyID,
		credentialCloudS3SecretKey,
		credentialCloudKopiaRecovery,
	} {
		got, err := credentialStorePathForHome(home, id)
		if err != nil {
			t.Fatalf("credentialStorePathForHome(%s): %v", id, err)
		}
		want := mustCredentialStorePathForHome(home, id)
		if got != want {
			t.Fatalf("credentialStorePathForHome(%s) = %q, want %q", id, got, want)
		}
		if !strings.Contains(got, filepath.Join(".hazmat", "secrets", "cloud")) {
			t.Fatalf("cloud credential %s stored outside cloud secret-store subtree: %q", id, got)
		}
	}
}

func TestHarnessAuthArtifactsUseCredentialRegistry(t *testing.T) {
	home := t.TempDir()
	cases := []struct {
		harness HarnessID
		ids     []credentialID
	}{
		{HarnessClaude, []credentialID{credentialHarnessClaudeCredentials, credentialHarnessClaudeState}},
		{HarnessCodex, []credentialID{credentialHarnessCodexAuth}},
		{HarnessOpenCode, []credentialID{credentialHarnessOpenCodeAuth}},
		{HarnessGemini, []credentialID{credentialHarnessGeminiOAuth, credentialHarnessGeminiAccounts}},
	}

	for _, tc := range cases {
		artifacts := harnessAuthArtifactsForHome(tc.harness, home)
		if len(artifacts) != len(tc.ids) {
			t.Fatalf("%s artifacts length = %d, want %d", tc.harness, len(artifacts), len(tc.ids))
		}
		for i, id := range tc.ids {
			descriptor := mustCredentialDescriptor(id)
			wantAgentPath, err := descriptor.AgentMaterializationPath()
			if err != nil {
				t.Fatalf("%s AgentMaterializationPath: %v", id, err)
			}
			wantStorePath := mustCredentialStorePathForHome(home, id)
			if artifacts[i].Name != descriptor.DisplayName {
				t.Fatalf("%s artifact name = %q, want %q", id, artifacts[i].Name, descriptor.DisplayName)
			}
			if artifacts[i].StorePath != wantStorePath {
				t.Fatalf("%s artifact StorePath = %q, want %q", id, artifacts[i].StorePath, wantStorePath)
			}
			if artifacts[i].AgentPath != wantAgentPath {
				t.Fatalf("%s artifact AgentPath = %q, want %q", id, artifacts[i].AgentPath, wantAgentPath)
			}
		}
	}
}

func TestCredentialStoreRelPathRejectsUnsafePaths(t *testing.T) {
	for _, relPath := range []string{"", "/absolute", "../secret", "secret/../other", "secret//other", "./secret"} {
		if _, err := cleanCredentialStoreRelPath(relPath); err == nil {
			t.Fatalf("cleanCredentialStoreRelPath(%q) succeeded, want error", relPath)
		}
	}

	got, err := cleanCredentialStoreRelPath("providers/openai-api-key")
	if err != nil {
		t.Fatalf("cleanCredentialStoreRelPath valid path: %v", err)
	}
	if got != "providers/openai-api-key" {
		t.Fatalf("cleanCredentialStoreRelPath valid path = %q", got)
	}
}

func TestCredentialDescriptorRejectsInvalidDeliveryAccess(t *testing.T) {
	envDescriptor := mustCredentialDescriptor(credentialProviderOpenAIAPIKey)
	if _, err := envDescriptor.AgentMaterializationPath(); err == nil {
		t.Fatalf("AgentMaterializationPath accepted env-delivered credential")
	}

	fileDescriptor := mustCredentialDescriptor(credentialHarnessCodexAuth)
	if _, err := fileDescriptor.EnvDeliveryVar(); err == nil {
		t.Fatalf("EnvDeliveryVar accepted materialized-file credential")
	}
}

func TestCredentialRegistrySummaryReportsManagedAndAdapterRequired(t *testing.T) {
	summary := summarizeCredentialRegistry(builtinCredentialDescriptors())
	if summary.ManagedHostSecretStore != 13 {
		t.Fatalf("ManagedHostSecretStore = %d, want 13", summary.ManagedHostSecretStore)
	}
	if len(summary.AdapterRequired) != 1 || summary.AdapterRequired[0] != "Gemini Keychain OAuth state" {
		t.Fatalf("AdapterRequired = %v, want Gemini Keychain OAuth state", summary.AdapterRequired)
	}
	if len(summary.ExternalBoundaries) != 1 || summary.ExternalBoundaries[0] != "Git SSH external identity reference" {
		t.Fatalf("ExternalBoundaries = %v, want Git SSH external identity reference", summary.ExternalBoundaries)
	}
}

func TestGitSSHCredentialDescriptorsModelIdentitySources(t *testing.T) {
	external := mustCredentialDescriptor(credentialGitSSHExternalIdentity)
	if external.Kind != credentialKindGitSSHIdentity {
		t.Fatalf("external Git SSH kind = %q, want %q", external.Kind, credentialKindGitSSHIdentity)
	}
	if external.Backend != credentialStorageExternalFile || external.Delivery != credentialDeliveryExternalReference || external.Support != credentialSupportExternal {
		t.Fatalf("external Git SSH descriptor = %+v, want external file reference", external)
	}
	if _, err := external.StorePathForHome(t.TempDir()); err == nil {
		t.Fatalf("external Git SSH descriptor produced host store path")
	}

	provisioned := mustCredentialDescriptor(credentialGitSSHProvisionedIdentity)
	if provisioned.Kind != credentialKindGitSSHIdentity {
		t.Fatalf("provisioned Git SSH kind = %q, want %q", provisioned.Kind, credentialKindGitSSHIdentity)
	}
	if provisioned.Backend != credentialStorageHostSecretStore || provisioned.Delivery != credentialDeliveryBrokeredHelper || provisioned.Support != credentialSupportManaged {
		t.Fatalf("provisioned Git SSH descriptor = %+v, want managed brokered helper", provisioned)
	}
	storePath := mustCredentialStorePathForHome(t.TempDir(), credentialGitSSHProvisionedIdentity)
	if !strings.Contains(storePath, filepath.Join(".hazmat", "secrets", "git-ssh", "provisioned")) {
		t.Fatalf("provisioned Git SSH store path = %q, want git-ssh/provisioned secret subtree", storePath)
	}
}

func TestGeminiKeychainCredentialBoundaryIsExternal(t *testing.T) {
	descriptor := mustCredentialDescriptor(credentialHarnessGeminiKeychain)
	if descriptor.Backend != credentialStorageKeychain {
		t.Fatalf("Gemini Keychain backend = %q, want %q", descriptor.Backend, credentialStorageKeychain)
	}
	if descriptor.Delivery != credentialDeliveryExternalReference {
		t.Fatalf("Gemini Keychain delivery = %q, want %q", descriptor.Delivery, credentialDeliveryExternalReference)
	}
	if descriptor.Support != credentialSupportAdapterRequired {
		t.Fatalf("Gemini Keychain support = %q, want %q", descriptor.Support, credentialSupportAdapterRequired)
	}
	if descriptor.StoreRelPath != "" || descriptor.AgentPath != "" {
		t.Fatalf("Gemini Keychain descriptor must not declare file paths: %+v", descriptor)
	}
	if _, err := descriptor.StorePathForHome(t.TempDir()); err == nil {
		t.Fatalf("Gemini Keychain descriptor produced host store path")
	}
	if _, err := descriptor.AgentMaterializationPath(); err == nil {
		t.Fatalf("Gemini Keychain descriptor produced agent materialization path")
	}
}
