package hazmat

import (
	"hazmat/credentials"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinCredentialDescriptorsAreWellFormed(t *testing.T) {
	home := t.TempDir()
	secretRoot := secretStoreDirForHome(home)
	seenIDs := map[credentials.ID]bool{}
	seenStorePaths := map[string]credentials.ID{}
	seenEnvVars := map[string]credentials.ID{}

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
		if descriptor.Kind == credentials.KindProviderAPIKey {
			if descriptor.Harness != "" {
				t.Fatalf("%s provider API-key descriptor must be provider-owned, got harness %q", descriptor.ID, descriptor.Harness)
			}
			if len(descriptor.ConsumerHarnessIDs()) == 0 {
				t.Fatalf("%s provider API-key descriptor has no consumer harnesses", descriptor.ID)
			}
		}

		switch descriptor.Backend {
		case credentials.StorageHostSecretStore:
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
		case credentials.StorageKeychain, credentials.StorageExternalFile, credentials.StorageBroker:
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
		case credentials.DeliveryEnv:
			envVar, err := descriptor.EnvDeliveryVar()
			if err != nil {
				t.Fatalf("%s EnvDeliveryVar: %v", descriptor.ID, err)
			}
			if !isCredentialGrantEnvKey(envVar) {
				t.Fatalf("%s env delivery uses unexpected env var %q", descriptor.ID, envVar)
			}
			if previous, exists := seenEnvVars[envVar]; exists {
				t.Fatalf("%s and %s share env var %q; env-var lookup must stay unambiguous", descriptor.ID, previous, envVar)
			}
			seenEnvVars[envVar] = descriptor.ID
			if descriptor.AgentPath != "" {
				t.Fatalf("%s env delivery must not declare an agent path", descriptor.ID)
			}
		case credentials.DeliveryMaterializedFile:
			agentPath, err := descriptor.AgentMaterializationPath()
			if err != nil {
				t.Fatalf("%s AgentMaterializationPath: %v", descriptor.ID, err)
			}
			if !usesManagedAgentPath(agentPath) {
				t.Fatalf("%s materializes outside managed agent home: %q", descriptor.ID, agentPath)
			}
			if len(descriptor.ConsumerHarnessIDs()) != 1 {
				t.Fatalf("%s materialized credential consumers = %v, want single harness", descriptor.ID, descriptor.ConsumerHarnessIDs())
			}
			if !descriptor.ConflictArchive {
				t.Fatalf("%s materialized credential must preserve conflicts", descriptor.ID)
			}
		case credentials.DeliveryNone, credentials.DeliveryBrokeredHelper, credentials.DeliveryExternalReference:
			if descriptor.Delivery == credentials.DeliveryExternalReference && descriptor.ExternalRef == "" {
				t.Fatalf("%s external-reference delivery must describe the external authority", descriptor.ID)
			}
			if descriptor.AgentPath != "" {
				t.Fatalf("%s non-file delivery must not declare an agent path", descriptor.ID)
			}
		default:
			t.Fatalf("%s has unknown delivery mode %q", descriptor.ID, descriptor.Delivery)
		}

		switch descriptor.Support {
		case credentials.SupportManaged:
			if descriptor.Backend != credentials.StorageHostSecretStore {
				t.Fatalf("%s managed credential uses non-host backend %q", descriptor.ID, descriptor.Backend)
			}
		case credentials.SupportExternal, credentials.SupportAdapterRequired:
			if descriptor.Backend == credentials.StorageHostSecretStore {
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
		id     credentials.ID
	}{
		{"ANTHROPIC_API_KEY", credentials.ProviderAnthropicAPIKey},
		{"OPENAI_API_KEY", credentials.ProviderOpenAIAPIKey},
		{"GEMINI_API_KEY", credentials.ProviderGeminiAPIKey},
		{"OPENROUTER_API_KEY", credentials.ProviderOpenRouterAPIKey},
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

func TestProviderCredentialConsumersAreHarnessAware(t *testing.T) {
	cases := []struct {
		id        credentials.ID
		allowed   []HarnessID
		denied    []HarnessID
		storePath string
	}{
		{
			id:        credentials.ProviderAnthropicAPIKey,
			allowed:   []HarnessID{HarnessClaude, HarnessHermes},
			denied:    []HarnessID{HarnessCodex, HarnessAntigravity, HarnessOpenCode, HarnessQwen, HarnessCursorAgent, HarnessPi},
			storePath: "providers/anthropic-api-key",
		},
		{
			id:        credentials.ProviderOpenAIAPIKey,
			allowed:   []HarnessID{HarnessCodex, HarnessHermes},
			denied:    []HarnessID{HarnessClaude, HarnessAntigravity, HarnessOpenCode, HarnessQwen, HarnessCursorAgent, HarnessPi},
			storePath: "providers/openai-api-key",
		},
		{
			id:        credentials.ProviderAntigravityAPIKey,
			allowed:   []HarnessID{HarnessAntigravity},
			denied:    []HarnessID{HarnessClaude, HarnessCodex, HarnessOpenCode, HarnessHermes, HarnessQwen, HarnessCursorAgent, HarnessPi},
			storePath: "providers/antigravity-api-key",
		},
		{
			id:        credentials.ProviderGeminiAPIKey,
			allowed:   []HarnessID{HarnessAntigravity, HarnessHermes},
			denied:    []HarnessID{HarnessClaude, HarnessCodex, HarnessOpenCode, HarnessQwen, HarnessCursorAgent, HarnessPi},
			storePath: "providers/gemini-api-key",
		},
		{
			id:        credentials.ProviderOpenRouterAPIKey,
			allowed:   []HarnessID{HarnessHermes},
			denied:    []HarnessID{HarnessClaude, HarnessCodex, HarnessAntigravity, HarnessOpenCode, HarnessQwen, HarnessCursorAgent, HarnessPi},
			storePath: "providers/openrouter-api-key",
		},
	}

	for _, tc := range cases {
		descriptor := mustCredentialDescriptor(tc.id)
		if descriptor.StoreRelPath != tc.storePath {
			t.Fatalf("%s StoreRelPath = %q, want %q", tc.id, descriptor.StoreRelPath, tc.storePath)
		}
		if !sameHarnessIDs(descriptor.ConsumerHarnessIDs(), tc.allowed) {
			t.Fatalf("%s consumers = %v, want %v", tc.id, descriptor.ConsumerHarnessIDs(), tc.allowed)
		}
		for _, harness := range tc.allowed {
			if !descriptor.CanDeliverTo(harness) {
				t.Fatalf("%s CanDeliverTo(%s) = false, want true", tc.id, harness)
			}
		}
		for _, harness := range tc.denied {
			if descriptor.CanDeliverTo(harness) {
				t.Fatalf("%s CanDeliverTo(%s) = true, want false", tc.id, harness)
			}
		}
	}
}

func TestProviderCredentialDescriptorLookupRequiresAllowedHarness(t *testing.T) {
	if descriptor, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENAI_API_KEY", HarnessCodex); !ok || descriptor.ID != credentials.ProviderOpenAIAPIKey {
		t.Fatalf("OPENAI_API_KEY for Codex = %+v, %v; want OpenAI descriptor", descriptor, ok)
	}
	if descriptor, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENAI_API_KEY", HarnessHermes); !ok || descriptor.ID != credentials.ProviderOpenAIAPIKey {
		t.Fatalf("OPENAI_API_KEY for Hermes = %+v, %v; want OpenAI descriptor", descriptor, ok)
	}
	if _, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENAI_API_KEY", HarnessClaude); ok {
		t.Fatalf("OPENAI_API_KEY must not resolve for Claude")
	}
	if _, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENAI_API_KEY", HarnessQwen); ok {
		t.Fatalf("OPENAI_API_KEY must not resolve for Qwen in Phase 1")
	}
	if _, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENAI_API_KEY", HarnessCursorAgent); ok {
		t.Fatalf("OPENAI_API_KEY must not resolve for Cursor Agent in Phase 1")
	}
	if _, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENAI_API_KEY", HarnessPi); ok {
		t.Fatalf("OPENAI_API_KEY must not resolve for Pi in Phase 1")
	}
	if descriptor, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENROUTER_API_KEY", HarnessHermes); !ok || descriptor.ID != credentials.ProviderOpenRouterAPIKey {
		t.Fatalf("OPENROUTER_API_KEY for Hermes = %+v, %v; want OpenRouter descriptor", descriptor, ok)
	}
	if _, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENROUTER_API_KEY", HarnessCodex); ok {
		t.Fatalf("OPENROUTER_API_KEY must not resolve for Codex")
	}
	if _, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENROUTER_API_KEY", HarnessQwen); ok {
		t.Fatalf("OPENROUTER_API_KEY must not resolve for Qwen in Phase 1")
	}
	if _, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENROUTER_API_KEY", HarnessCursorAgent); ok {
		t.Fatalf("OPENROUTER_API_KEY must not resolve for Cursor Agent in Phase 1")
	}
	if _, ok := providerCredentialDescriptorForEnvVarAndHarness("OPENROUTER_API_KEY", HarnessPi); ok {
		t.Fatalf("OPENROUTER_API_KEY must not resolve for Pi in Phase 1")
	}
}

func TestProviderCredentialDescriptorsForHarness(t *testing.T) {
	cases := []struct {
		harness HarnessID
		want    []credentials.ID
	}{
		{HarnessClaude, []credentials.ID{credentials.ProviderAnthropicAPIKey}},
		{HarnessCodex, []credentials.ID{credentials.ProviderOpenAIAPIKey}},
		{HarnessAntigravity, []credentials.ID{credentials.ProviderAntigravityAPIKey, credentials.ProviderGeminiAPIKey}},
		{HarnessHermes, []credentials.ID{
			credentials.ProviderAnthropicAPIKey,
			credentials.ProviderOpenAIAPIKey,
			credentials.ProviderGeminiAPIKey,
			credentials.ProviderOpenRouterAPIKey,
		}},
		{HarnessOpenCode, nil},
		{HarnessQwen, nil},
		{HarnessCursorAgent, nil},
		{HarnessPi, nil},
	}

	for _, tc := range cases {
		descriptors := providerCredentialDescriptorsForHarness(tc.harness)
		got := make([]credentials.ID, 0, len(descriptors))
		for _, descriptor := range descriptors {
			got = append(got, descriptor.ID)
		}
		if !sameCredentialIDs(got, tc.want) {
			t.Fatalf("providerCredentialDescriptorsForHarness(%s) = %v, want %v", tc.harness, got, tc.want)
		}
	}
}

func TestCloudCredentialStorePathsUseCredentialRegistry(t *testing.T) {
	home := t.TempDir()
	for _, id := range []credentials.ID{
		credentials.CloudS3AccessKeyID,
		credentials.CloudS3SecretKey,
		credentials.CloudKopiaRecovery,
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
		ids     []credentials.ID
	}{
		{HarnessClaude, []credentials.ID{credentials.HarnessClaudeCredentials, credentials.HarnessClaudeState}},
		{HarnessCodex, []credentials.ID{credentials.HarnessCodexAuth}},
		{HarnessOpenCode, []credentials.ID{credentials.HarnessOpenCodeAuth}},
		{HarnessAntigravity, nil},
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
	envDescriptor := mustCredentialDescriptor(credentials.ProviderOpenAIAPIKey)
	if _, err := envDescriptor.AgentMaterializationPath(); err == nil {
		t.Fatalf("AgentMaterializationPath accepted env-delivered credential")
	}

	fileDescriptor := mustCredentialDescriptor(credentials.HarnessCodexAuth)
	if _, err := fileDescriptor.EnvDeliveryVar(); err == nil {
		t.Fatalf("EnvDeliveryVar accepted materialized-file credential")
	}
}

func TestCredentialRegistrySummaryReportsManagedAndExternal(t *testing.T) {
	summary := summarizeCredentialRegistry(builtinCredentialDescriptors())
	if summary.ManagedHostSecretStore != 15 {
		t.Fatalf("ManagedHostSecretStore = %d, want 15", summary.ManagedHostSecretStore)
	}
	// The Antigravity Keychain adapter is now an active external boundary, so no
	// built-in credential is adapter-required.
	if len(summary.AdapterRequired) != 0 {
		t.Fatalf("AdapterRequired = %v, want none", summary.AdapterRequired)
	}
	wantExternal := []string{"Claude agent Keychain OAuth state", "Antigravity Keychain OAuth state", "Git SSH external identity reference"}
	if len(summary.ExternalBoundaries) != len(wantExternal) {
		t.Fatalf("ExternalBoundaries = %v, want %v", summary.ExternalBoundaries, wantExternal)
	}
	for i, want := range wantExternal {
		if summary.ExternalBoundaries[i] != want {
			t.Fatalf("ExternalBoundaries = %v, want %v", summary.ExternalBoundaries, wantExternal)
		}
	}
}

func sameHarnessIDs(got, want []HarnessID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sameCredentialIDs(got, want []credentials.ID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestGitHubAPITokenUsesCredentialRegistry(t *testing.T) {
	descriptor := mustCredentialDescriptor(credentials.GitHubAPIToken)
	if descriptor.Kind != credentials.KindGitHubToken {
		t.Fatalf("GitHub token kind = %q, want %q", descriptor.Kind, credentials.KindGitHubToken)
	}
	if descriptor.Backend != credentials.StorageHostSecretStore || descriptor.Delivery != credentials.DeliveryEnv || descriptor.Support != credentials.SupportManaged {
		t.Fatalf("GitHub token descriptor = %+v, want managed host-store env delivery", descriptor)
	}
	envVar, err := descriptor.EnvDeliveryVar()
	if err != nil {
		t.Fatalf("GitHub token EnvDeliveryVar: %v", err)
	}
	if envVar != "GH_TOKEN" {
		t.Fatalf("GitHub token env var = %q, want GH_TOKEN", envVar)
	}
	storePath := mustCredentialStorePathForHome(t.TempDir(), credentials.GitHubAPIToken)
	if !strings.Contains(storePath, filepath.Join(".hazmat", "secrets", "github", "token")) {
		t.Fatalf("GitHub token store path = %q, want github/token secret-store path", storePath)
	}
}

func TestGitHTTPSCredentialStoreUsesCredentialRegistry(t *testing.T) {
	descriptor := mustCredentialDescriptor(credentials.GitHTTPSAgentStore)
	if descriptor.Kind != credentials.KindGitHTTPS {
		t.Fatalf("Git HTTPS kind = %q, want %q", descriptor.Kind, credentials.KindGitHTTPS)
	}
	if descriptor.Backend != credentials.StorageHostSecretStore || descriptor.Delivery != credentials.DeliveryBrokeredHelper || descriptor.Support != credentials.SupportManaged {
		t.Fatalf("Git HTTPS descriptor = %+v, want managed host-store brokered helper", descriptor)
	}
	storePath := mustCredentialStorePathForHome(t.TempDir(), credentials.GitHTTPSAgentStore)
	if !strings.Contains(storePath, filepath.Join(".hazmat", "secrets", "git-https", "credentials")) {
		t.Fatalf("Git HTTPS store path = %q, want git-https secret-store path", storePath)
	}
}

func TestGitSSHCredentialDescriptorsModelIdentitySources(t *testing.T) {
	external := mustCredentialDescriptor(credentials.GitSSHExternalIdentity)
	if external.Kind != credentials.KindGitSSHIdentity {
		t.Fatalf("external Git SSH kind = %q, want %q", external.Kind, credentials.KindGitSSHIdentity)
	}
	if external.Backend != credentials.StorageExternalFile || external.Delivery != credentials.DeliveryExternalReference || external.Support != credentials.SupportExternal {
		t.Fatalf("external Git SSH descriptor = %+v, want external file reference", external)
	}
	if _, err := external.StorePathForHome(t.TempDir()); err == nil {
		t.Fatalf("external Git SSH descriptor produced host store path")
	}

	provisioned := mustCredentialDescriptor(credentials.GitSSHProvisionedIdentity)
	if provisioned.Kind != credentials.KindGitSSHIdentity {
		t.Fatalf("provisioned Git SSH kind = %q, want %q", provisioned.Kind, credentials.KindGitSSHIdentity)
	}
	if provisioned.Backend != credentials.StorageHostSecretStore || provisioned.Delivery != credentials.DeliveryBrokeredHelper || provisioned.Support != credentials.SupportManaged {
		t.Fatalf("provisioned Git SSH descriptor = %+v, want managed brokered helper", provisioned)
	}
	storePath := mustCredentialStorePathForHome(t.TempDir(), credentials.GitSSHProvisionedIdentity)
	if !strings.Contains(storePath, filepath.Join(".hazmat", "secrets", "git-ssh", "provisioned")) {
		t.Fatalf("provisioned Git SSH store path = %q, want git-ssh/provisioned secret subtree", storePath)
	}
}

func TestAntigravityKeychainCredentialBoundaryIsExternal(t *testing.T) {
	descriptor := mustCredentialDescriptor(credentials.HarnessAntigravityKeychain)
	if descriptor.Backend != credentials.StorageKeychain {
		t.Fatalf("Antigravity Keychain backend = %q, want %q", descriptor.Backend, credentials.StorageKeychain)
	}
	if descriptor.Delivery != credentials.DeliveryExternalReference {
		t.Fatalf("Antigravity Keychain delivery = %q, want %q", descriptor.Delivery, credentials.DeliveryExternalReference)
	}
	if descriptor.Support != credentials.SupportExternal {
		t.Fatalf("Antigravity Keychain support = %q, want %q", descriptor.Support, credentials.SupportExternal)
	}
	if !descriptor.CanDeliverTo(HarnessAntigravity) || descriptor.CanDeliverTo(HarnessClaude) {
		t.Fatalf("Antigravity Keychain consumers = %v, want Antigravity only", descriptor.ConsumerHarnessIDs())
	}
	if descriptor.ExternalRef != agentLoginKeychainPath() {
		t.Fatalf("Antigravity Keychain external ref = %q, want %q", descriptor.ExternalRef, agentLoginKeychainPath())
	}
	if descriptor.StoreRelPath != "" || descriptor.AgentPath != "" {
		t.Fatalf("Antigravity Keychain descriptor must not declare file paths: %+v", descriptor)
	}
	if _, err := descriptor.StorePathForHome(t.TempDir()); err == nil {
		t.Fatalf("Antigravity Keychain descriptor produced host store path")
	}
	if _, err := descriptor.AgentMaterializationPath(); err == nil {
		t.Fatalf("Antigravity Keychain descriptor produced agent materialization path")
	}
}

func TestClaudeAgentKeychainCredentialBoundaryIsExternal(t *testing.T) {
	descriptor := mustCredentialDescriptor(credentials.HarnessClaudeKeychain)
	if descriptor.Backend != credentials.StorageKeychain {
		t.Fatalf("Claude Keychain backend = %q, want %q", descriptor.Backend, credentials.StorageKeychain)
	}
	if descriptor.Delivery != credentials.DeliveryExternalReference {
		t.Fatalf("Claude Keychain delivery = %q, want %q", descriptor.Delivery, credentials.DeliveryExternalReference)
	}
	if descriptor.Support != credentials.SupportExternal {
		t.Fatalf("Claude Keychain support = %q, want %q", descriptor.Support, credentials.SupportExternal)
	}
	if !descriptor.CanDeliverTo(HarnessClaude) || descriptor.CanDeliverTo(HarnessCodex) {
		t.Fatalf("Claude Keychain consumers = %v, want Claude only", descriptor.ConsumerHarnessIDs())
	}
	if descriptor.ExternalRef != agentLoginKeychainPath() {
		t.Fatalf("Claude Keychain external ref = %q, want %q", descriptor.ExternalRef, agentLoginKeychainPath())
	}
	if descriptor.StoreRelPath != "" || descriptor.AgentPath != "" {
		t.Fatalf("Claude Keychain descriptor must not declare file paths: %+v", descriptor)
	}
	if _, err := descriptor.StorePathForHome(t.TempDir()); err == nil {
		t.Fatalf("Claude Keychain descriptor produced host store path")
	}
	if _, err := descriptor.AgentMaterializationPath(); err == nil {
		t.Fatalf("Claude Keychain descriptor produced agent materialization path")
	}
}
