package credentials

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"hazmat/harnesses"
)

type HarnessID = harnesses.ID

const (
	HarnessClaude      HarnessID = harnesses.Claude
	HarnessCodex       HarnessID = harnesses.Codex
	HarnessOpenCode    HarnessID = harnesses.OpenCode
	HarnessGemini      HarnessID = harnesses.Gemini
	HarnessHermes      HarnessID = harnesses.Hermes
	HarnessQwen        HarnessID = harnesses.Qwen
	HarnessCursorAgent HarnessID = harnesses.CursorAgent
)

type ID string

type Kind string

const (
	KindProviderAPIKey Kind = "provider-api-key"
	KindHarnessAuth    Kind = "harness-auth"
	KindGitHTTPS       Kind = "git-https"
	KindGitSSHIdentity Kind = "git-ssh-identity"
	KindCloudBackup    Kind = "cloud-backup"
	KindGitHubToken    Kind = "github-token"
	KindIntegrationEnv Kind = "integration-env"
	KindExternalAuth   Kind = "external-auth"
)

type StorageBackend string

const (
	StorageHostSecretStore StorageBackend = "host-secret-store"
	StorageExternalFile    StorageBackend = "external-file"
	StorageKeychain        StorageBackend = "keychain"
	StorageBroker          StorageBackend = "broker"
)

type DeliveryMode string

const (
	DeliveryNone              DeliveryMode = "none"
	DeliveryEnv               DeliveryMode = "env"
	DeliveryMaterializedFile  DeliveryMode = "materialized-file"
	DeliveryBrokeredHelper    DeliveryMode = "brokered-helper"
	DeliveryExternalReference DeliveryMode = "external-reference"
)

type SupportStatus string

const (
	SupportManaged         SupportStatus = "managed"
	SupportExternal        SupportStatus = "external"
	SupportAdapterRequired SupportStatus = "adapter-required"
)

const (
	ProviderAnthropicAPIKey  ID = "provider.anthropic.api-key"
	ProviderOpenAIAPIKey     ID = "provider.openai.api-key"
	ProviderGeminiAPIKey     ID = "provider.gemini.api-key"
	ProviderOpenRouterAPIKey ID = "provider.openrouter.api-key"

	HarnessClaudeCredentials ID = "harness.claude.credentials"
	HarnessClaudeState       ID = "harness.claude.state"
	HarnessClaudeKeychain    ID = "harness.claude.agent-keychain-oauth"
	HarnessCodexAuth         ID = "harness.codex.auth"
	HarnessOpenCodeAuth      ID = "harness.opencode.auth"
	HarnessGeminiOAuth       ID = "harness.gemini.oauth"
	HarnessGeminiAccounts    ID = "harness.gemini.accounts"
	HarnessGeminiKeychain    ID = "harness.gemini.keychain-oauth"

	GitSSHExternalIdentity    ID = "git-ssh.external-identity"
	GitSSHProvisionedIdentity ID = "git-ssh.provisioned-identity"
	GitHTTPSAgentStore        ID = "git-https.agent-store"
	GitHubAPIToken            ID = "github.api-token"

	CloudS3AccessKeyID ID = "cloud.s3.access-key-id"
	CloudS3SecretKey   ID = "cloud.s3.secret-key"
	CloudKopiaRecovery ID = "cloud.kopia.recovery-key"
)

// Descriptor is the durable credential surface registry entry. It names the
// host-owned storage boundary and the corresponding session delivery mode.
type Descriptor struct {
	ID                ID
	DisplayName       string
	Kind              Kind
	Backend           StorageBackend
	Delivery          DeliveryMode
	Support           SupportStatus
	StoreRelPath      string
	Harness           HarnessID
	ConsumerHarnesses []HarnessID
	EnvVar            string
	AgentPath         string
	ExternalRef       string
	LegacyPaths       []string
	Redacted          bool
	ConflictArchive   bool

	agentHome string
}

type RegistrySummary struct {
	ManagedHostSecretStore int
	ExternalBoundaries     []string
	AdapterRequired        []string
}

type RegistryPaths struct {
	AgentHome                    string
	AgentZshrcPath               string
	AgentLoginKeychainRelPath    string
	ConfigFilePath               string
	CloudCredentialPath          string
	GitHTTPSAgentCredentialsPath string
}

func BuiltinDescriptors(paths RegistryPaths) []Descriptor {
	descriptors := builtinRegistry(paths.normalized())
	for i := range descriptors {
		descriptors[i].ConsumerHarnesses = cloneHarnessIDs(descriptors[i].ConsumerHarnesses)
		descriptors[i].LegacyPaths = cloneStrings(descriptors[i].LegacyPaths)
	}
	return descriptors
}

func SummarizeRegistry(descriptors []Descriptor) RegistrySummary {
	var summary RegistrySummary
	for _, descriptor := range descriptors {
		switch descriptor.Support {
		case SupportManaged:
			if descriptor.Backend == StorageHostSecretStore {
				summary.ManagedHostSecretStore++
			}
		case SupportExternal:
			summary.ExternalBoundaries = append(summary.ExternalBoundaries, descriptor.DisplayName)
		case SupportAdapterRequired:
			summary.AdapterRequired = append(summary.AdapterRequired, descriptor.DisplayName)
		}
	}
	return summary
}

func FindDescriptor(paths RegistryPaths, id ID) (Descriptor, bool) {
	for _, descriptor := range BuiltinDescriptors(paths) {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}

func MustDescriptor(paths RegistryPaths, id ID) Descriptor {
	descriptor, ok := FindDescriptor(paths, id)
	if !ok {
		panic(fmt.Sprintf("missing credential descriptor %q", id))
	}
	return descriptor
}

func ProviderDescriptorForEnvVar(paths RegistryPaths, envVar string) (Descriptor, bool) {
	for _, descriptor := range BuiltinDescriptors(paths) {
		if descriptor.Kind == KindProviderAPIKey && descriptor.EnvVar == envVar {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}

func ProviderDescriptorForEnvVarAndHarness(paths RegistryPaths, envVar string, harness HarnessID) (Descriptor, bool) {
	descriptor, ok := ProviderDescriptorForEnvVar(paths, envVar)
	if !ok || !descriptor.CanDeliverTo(harness) {
		return Descriptor{}, false
	}
	return descriptor, true
}

func ProviderDescriptorsForHarness(paths RegistryPaths, harness HarnessID) []Descriptor {
	var descriptors []Descriptor
	for _, descriptor := range BuiltinDescriptors(paths) {
		if descriptor.Kind != KindProviderAPIKey {
			continue
		}
		if descriptor.CanDeliverTo(harness) {
			descriptors = append(descriptors, descriptor)
		}
	}
	return descriptors
}

func DescriptorsForHarnessLifecycle(paths RegistryPaths, id HarnessID) []Descriptor {
	var descriptors []Descriptor
	for _, descriptor := range BuiltinDescriptors(paths) {
		if descriptor.Harness == id || descriptor.CanDeliverTo(id) {
			descriptors = append(descriptors, descriptor)
		}
	}
	return descriptors
}

func ProviderSecretStorePathForDescriptor(home string, descriptor Descriptor) (string, error) {
	if descriptor.Kind != KindProviderAPIKey {
		return "", fmt.Errorf("%s is %s, not %s", descriptor.ID, descriptor.Kind, KindProviderAPIKey)
	}
	if descriptor.Backend != StorageHostSecretStore {
		return "", fmt.Errorf("%s uses %s, not host secret store", descriptor.ID, descriptor.Backend)
	}
	return descriptor.StorePathForHome(home)
}

func StorePathForHome(paths RegistryPaths, home string, id ID) (string, error) {
	descriptor, ok := FindDescriptor(paths, id)
	if !ok {
		return "", fmt.Errorf("no credential descriptor for %s", id)
	}
	return descriptor.StorePathForHome(home)
}

func StorePathForConfig(paths RegistryPaths, id ID) (string, error) {
	paths = paths.normalized()
	descriptor, ok := FindDescriptor(paths, id)
	if !ok {
		return "", fmt.Errorf("no credential descriptor for %s", id)
	}
	if descriptor.Backend != StorageHostSecretStore {
		return "", fmt.Errorf("%s uses %s, not host secret store", descriptor.ID, descriptor.Backend)
	}
	cleanRelPath, err := CleanStoreRelPath(descriptor.StoreRelPath)
	if err != nil {
		return "", fmt.Errorf("%s store path: %w", descriptor.ID, err)
	}
	return filepath.Join(filepath.Dir(paths.ConfigFilePath), "secrets", filepath.FromSlash(cleanRelPath)), nil
}

func SecretStoreDirForHome(home string) string {
	return filepath.Join(home, ".hazmat", "secrets")
}

func (descriptor Descriptor) StorePathForHome(home string) (string, error) {
	if descriptor.Backend != StorageHostSecretStore {
		return "", fmt.Errorf("%s uses %s, not host secret store", descriptor.ID, descriptor.Backend)
	}
	cleanRelPath, err := CleanStoreRelPath(descriptor.StoreRelPath)
	if err != nil {
		return "", fmt.Errorf("%s store path: %w", descriptor.ID, err)
	}
	return filepath.Join(SecretStoreDirForHome(home), filepath.FromSlash(cleanRelPath)), nil
}

func (descriptor Descriptor) AgentMaterializationPath() (string, error) {
	if descriptor.Delivery != DeliveryMaterializedFile {
		return "", fmt.Errorf("%s uses %s delivery, not materialized file delivery", descriptor.ID, descriptor.Delivery)
	}
	if descriptor.AgentPath == "" {
		return "", fmt.Errorf("%s materialized file delivery has no agent path", descriptor.ID)
	}
	if descriptor.agentHome != "" && !usesManagedAgentPath(descriptor.agentHome, descriptor.AgentPath) {
		return "", fmt.Errorf("%s materializes outside managed agent home: %s", descriptor.ID, descriptor.AgentPath)
	}
	return descriptor.AgentPath, nil
}

func (descriptor Descriptor) EnvDeliveryVar() (string, error) {
	if descriptor.Delivery != DeliveryEnv {
		return "", fmt.Errorf("%s uses %s delivery, not env delivery", descriptor.ID, descriptor.Delivery)
	}
	if descriptor.EnvVar == "" {
		return "", fmt.Errorf("%s env delivery has no env var", descriptor.ID)
	}
	return descriptor.EnvVar, nil
}

func (descriptor Descriptor) ConsumerHarnessIDs() []HarnessID {
	if len(descriptor.ConsumerHarnesses) > 0 {
		return cloneHarnessIDs(descriptor.ConsumerHarnesses)
	}
	if descriptor.Harness != "" {
		return []HarnessID{descriptor.Harness}
	}
	return nil
}

func (descriptor Descriptor) CanDeliverTo(harness HarnessID) bool {
	if harness == "" {
		return false
	}
	for _, consumer := range descriptor.ConsumerHarnessIDs() {
		if consumer == harness {
			return true
		}
	}
	return false
}

func CleanStoreRelPath(relPath string) (string, error) {
	if relPath == "" {
		return "", fmt.Errorf("path is empty")
	}
	slashPath := filepath.ToSlash(relPath)
	if path.IsAbs(slashPath) {
		return "", fmt.Errorf("path must be relative: %s", relPath)
	}
	parts := strings.Split(slashPath, "/")
	for _, part := range parts {
		switch part {
		case "", ".", "..":
			return "", fmt.Errorf("path contains invalid component %q: %s", part, relPath)
		}
	}
	clean := path.Clean(slashPath)
	if clean == "." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path escapes secret store: %s", relPath)
	}
	return clean, nil
}

func builtinRegistry(paths RegistryPaths) []Descriptor {
	return []Descriptor{
		{
			ID:                ProviderAnthropicAPIKey,
			DisplayName:       "Anthropic API key",
			Kind:              KindProviderAPIKey,
			Backend:           StorageHostSecretStore,
			Delivery:          DeliveryEnv,
			Support:           SupportManaged,
			StoreRelPath:      "providers/anthropic-api-key",
			ConsumerHarnesses: []HarnessID{HarnessClaude, HarnessHermes},
			EnvVar:            "ANTHROPIC_API_KEY",
			LegacyPaths:       []string{paths.AgentZshrcPath},
			Redacted:          true,
			agentHome:         paths.AgentHome,
		},
		{
			ID:                ProviderOpenAIAPIKey,
			DisplayName:       "OpenAI API key",
			Kind:              KindProviderAPIKey,
			Backend:           StorageHostSecretStore,
			Delivery:          DeliveryEnv,
			Support:           SupportManaged,
			StoreRelPath:      "providers/openai-api-key",
			ConsumerHarnesses: []HarnessID{HarnessCodex, HarnessHermes},
			EnvVar:            "OPENAI_API_KEY",
			LegacyPaths:       []string{paths.AgentZshrcPath},
			Redacted:          true,
			agentHome:         paths.AgentHome,
		},
		{
			ID:                ProviderGeminiAPIKey,
			DisplayName:       "Gemini API key",
			Kind:              KindProviderAPIKey,
			Backend:           StorageHostSecretStore,
			Delivery:          DeliveryEnv,
			Support:           SupportManaged,
			StoreRelPath:      "providers/gemini-api-key",
			ConsumerHarnesses: []HarnessID{HarnessGemini, HarnessHermes},
			EnvVar:            "GEMINI_API_KEY",
			LegacyPaths:       []string{paths.AgentZshrcPath},
			Redacted:          true,
			agentHome:         paths.AgentHome,
		},
		{
			ID:                ProviderOpenRouterAPIKey,
			DisplayName:       "OpenRouter API key",
			Kind:              KindProviderAPIKey,
			Backend:           StorageHostSecretStore,
			Delivery:          DeliveryEnv,
			Support:           SupportManaged,
			StoreRelPath:      "providers/openrouter-api-key",
			ConsumerHarnesses: []HarnessID{HarnessHermes},
			EnvVar:            "OPENROUTER_API_KEY",
			LegacyPaths:       []string{paths.AgentZshrcPath},
			Redacted:          true,
			agentHome:         paths.AgentHome,
		},
		{
			ID:              HarnessClaudeCredentials,
			DisplayName:     "Claude credential file",
			Kind:            KindHarnessAuth,
			Backend:         StorageHostSecretStore,
			Delivery:        DeliveryMaterializedFile,
			Support:         SupportManaged,
			StoreRelPath:    "claude/credentials.json",
			Harness:         HarnessClaude,
			AgentPath:       paths.agentPath("/.claude/.credentials.json"),
			LegacyPaths:     []string{paths.agentPath("/.claude/.credentials.json")},
			Redacted:        true,
			ConflictArchive: true,
			agentHome:       paths.AgentHome,
		},
		{
			ID:              HarnessClaudeState,
			DisplayName:     "Claude account state",
			Kind:            KindHarnessAuth,
			Backend:         StorageHostSecretStore,
			Delivery:        DeliveryMaterializedFile,
			Support:         SupportManaged,
			StoreRelPath:    "claude/state.json",
			Harness:         HarnessClaude,
			AgentPath:       paths.agentPath("/.claude.json"),
			LegacyPaths:     []string{paths.agentPath("/.claude.json")},
			Redacted:        true,
			ConflictArchive: true,
			agentHome:       paths.AgentHome,
		},
		{
			ID:          HarnessClaudeKeychain,
			DisplayName: "Claude agent Keychain OAuth state",
			Kind:        KindExternalAuth,
			Backend:     StorageKeychain,
			Delivery:    DeliveryExternalReference,
			Support:     SupportExternal,
			Harness:     HarnessClaude,
			ExternalRef: paths.agentPath("/" + paths.AgentLoginKeychainRelPath),
			Redacted:    true,
			agentHome:   paths.AgentHome,
		},
		{
			ID:              HarnessCodexAuth,
			DisplayName:     "Codex auth file",
			Kind:            KindHarnessAuth,
			Backend:         StorageHostSecretStore,
			Delivery:        DeliveryMaterializedFile,
			Support:         SupportManaged,
			StoreRelPath:    "codex/auth.json",
			Harness:         HarnessCodex,
			AgentPath:       paths.agentPath("/.codex/auth.json"),
			LegacyPaths:     []string{paths.agentPath("/.codex/auth.json")},
			Redacted:        true,
			ConflictArchive: true,
			agentHome:       paths.AgentHome,
		},
		{
			ID:              HarnessOpenCodeAuth,
			DisplayName:     "OpenCode auth file",
			Kind:            KindHarnessAuth,
			Backend:         StorageHostSecretStore,
			Delivery:        DeliveryMaterializedFile,
			Support:         SupportManaged,
			StoreRelPath:    "opencode/auth.json",
			Harness:         HarnessOpenCode,
			AgentPath:       paths.agentPath("/.local/share/opencode/auth.json"),
			LegacyPaths:     []string{paths.agentPath("/.local/share/opencode/auth.json")},
			Redacted:        true,
			ConflictArchive: true,
			agentHome:       paths.AgentHome,
		},
		{
			ID:              HarnessGeminiOAuth,
			DisplayName:     "Gemini OAuth credentials",
			Kind:            KindHarnessAuth,
			Backend:         StorageHostSecretStore,
			Delivery:        DeliveryMaterializedFile,
			Support:         SupportManaged,
			StoreRelPath:    "gemini/oauth_creds.json",
			Harness:         HarnessGemini,
			AgentPath:       paths.agentPath("/.gemini/oauth_creds.json"),
			LegacyPaths:     []string{paths.agentPath("/.gemini/oauth_creds.json")},
			Redacted:        true,
			ConflictArchive: true,
			agentHome:       paths.AgentHome,
		},
		{
			ID:              HarnessGeminiAccounts,
			DisplayName:     "Gemini account index",
			Kind:            KindHarnessAuth,
			Backend:         StorageHostSecretStore,
			Delivery:        DeliveryMaterializedFile,
			Support:         SupportManaged,
			StoreRelPath:    "gemini/google_accounts.json",
			Harness:         HarnessGemini,
			AgentPath:       paths.agentPath("/.gemini/google_accounts.json"),
			LegacyPaths:     []string{paths.agentPath("/.gemini/google_accounts.json")},
			Redacted:        true,
			ConflictArchive: true,
			agentHome:       paths.AgentHome,
		},
		{
			ID:          HarnessGeminiKeychain,
			DisplayName: "Gemini Keychain OAuth state",
			Kind:        KindExternalAuth,
			Backend:     StorageKeychain,
			Delivery:    DeliveryExternalReference,
			Support:     SupportAdapterRequired,
			Harness:     HarnessGemini,
			ExternalRef: "macOS Keychain item owned by Gemini CLI",
			Redacted:    true,
			agentHome:   paths.AgentHome,
		},
		{
			ID:          GitSSHExternalIdentity,
			DisplayName: "Git SSH external identity reference",
			Kind:        KindGitSSHIdentity,
			Backend:     StorageExternalFile,
			Delivery:    DeliveryExternalReference,
			Support:     SupportExternal,
			ExternalRef: "host-owned private key path selected by project SSH config or ssh_profiles",
			Redacted:    true,
			agentHome:   paths.AgentHome,
		},
		{
			ID:           GitSSHProvisionedIdentity,
			DisplayName:  "Git SSH provisioned identity root",
			Kind:         KindGitSSHIdentity,
			Backend:      StorageHostSecretStore,
			Delivery:     DeliveryBrokeredHelper,
			Support:      SupportManaged,
			StoreRelPath: "git-ssh/provisioned",
			LegacyPaths:  []string{filepath.Join(filepath.Dir(paths.ConfigFilePath), "ssh", "keys")},
			Redacted:     true,
			agentHome:    paths.AgentHome,
		},
		{
			ID:           GitHTTPSAgentStore,
			DisplayName:  "Git HTTPS credential store",
			Kind:         KindGitHTTPS,
			Backend:      StorageHostSecretStore,
			Delivery:     DeliveryBrokeredHelper,
			Support:      SupportManaged,
			StoreRelPath: "git-https/credentials",
			LegacyPaths:  []string{paths.GitHTTPSAgentCredentialsPath},
			Redacted:     true,
			agentHome:    paths.AgentHome,
		},
		{
			ID:           GitHubAPIToken,
			DisplayName:  "GitHub API token",
			Kind:         KindGitHubToken,
			Backend:      StorageHostSecretStore,
			Delivery:     DeliveryEnv,
			Support:      SupportManaged,
			StoreRelPath: "github/token",
			EnvVar:       "GH_TOKEN",
			Redacted:     true,
			agentHome:    paths.AgentHome,
		},
		{
			ID:           CloudS3AccessKeyID,
			DisplayName:  "Cloud backup S3 access key ID",
			Kind:         KindCloudBackup,
			Backend:      StorageHostSecretStore,
			Delivery:     DeliveryNone,
			Support:      SupportManaged,
			StoreRelPath: "cloud/s3-access-key-id",
			LegacyPaths:  []string{paths.ConfigFilePath},
			Redacted:     true,
			agentHome:    paths.AgentHome,
		},
		{
			ID:           CloudS3SecretKey,
			DisplayName:  "Cloud backup S3 secret key",
			Kind:         KindCloudBackup,
			Backend:      StorageHostSecretStore,
			Delivery:     DeliveryNone,
			Support:      SupportManaged,
			StoreRelPath: "cloud/s3-secret-key",
			LegacyPaths:  []string{paths.CloudCredentialPath},
			Redacted:     true,
			agentHome:    paths.AgentHome,
		},
		{
			ID:           CloudKopiaRecovery,
			DisplayName:  "Cloud backup recovery key",
			Kind:         KindCloudBackup,
			Backend:      StorageHostSecretStore,
			Delivery:     DeliveryNone,
			Support:      SupportManaged,
			StoreRelPath: "cloud/kopia-recovery-key",
			LegacyPaths:  []string{paths.ConfigFilePath},
			Redacted:     true,
			agentHome:    paths.AgentHome,
		},
	}
}

func (paths RegistryPaths) normalized() RegistryPaths {
	if paths.AgentZshrcPath == "" && paths.AgentHome != "" {
		paths.AgentZshrcPath = filepath.Join(paths.AgentHome, ".zshrc")
	}
	if paths.AgentLoginKeychainRelPath == "" {
		paths.AgentLoginKeychainRelPath = "Library/Keychains/login.keychain-db"
	}
	if paths.GitHTTPSAgentCredentialsPath == "" && paths.AgentHome != "" {
		paths.GitHTTPSAgentCredentialsPath = filepath.Join(paths.AgentHome, ".config", "git", "credentials")
	}
	return paths
}

func (paths RegistryPaths) agentPath(rel string) string {
	return paths.AgentHome + rel
}

func usesManagedAgentPath(home, target string) bool {
	rel, err := filepath.Rel(home, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func cloneHarnessIDs(in []HarnessID) []HarnessID {
	if len(in) == 0 {
		return nil
	}
	out := make([]HarnessID, len(in))
	copy(out, in)
	return out
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
