package main

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

type credentialID string

type credentialKind string

const (
	credentialKindProviderAPIKey credentialKind = "provider-api-key"
	credentialKindHarnessAuth    credentialKind = "harness-auth"
	credentialKindGitHTTPS       credentialKind = "git-https"
	credentialKindGitSSHIdentity credentialKind = "git-ssh-identity"
	credentialKindCloudBackup    credentialKind = "cloud-backup"
	credentialKindGitHubToken    credentialKind = "github-token"
	credentialKindIntegrationEnv credentialKind = "integration-env"
	credentialKindExternalAuth   credentialKind = "external-auth"
)

type credentialStorageBackend string

const (
	credentialStorageHostSecretStore credentialStorageBackend = "host-secret-store"
	credentialStorageExternalFile    credentialStorageBackend = "external-file"
	credentialStorageKeychain        credentialStorageBackend = "keychain"
	credentialStorageBroker          credentialStorageBackend = "broker"
)

type credentialDeliveryMode string

const (
	credentialDeliveryNone              credentialDeliveryMode = "none"
	credentialDeliveryEnv               credentialDeliveryMode = "env"
	credentialDeliveryMaterializedFile  credentialDeliveryMode = "materialized-file"
	credentialDeliveryBrokeredHelper    credentialDeliveryMode = "brokered-helper"
	credentialDeliveryExternalReference credentialDeliveryMode = "external-reference"
)

const (
	credentialProviderAnthropicAPIKey credentialID = "provider.anthropic.api-key"
	credentialProviderOpenAIAPIKey    credentialID = "provider.openai.api-key"
	credentialProviderGeminiAPIKey    credentialID = "provider.gemini.api-key"

	credentialHarnessClaudeCredentials credentialID = "harness.claude.credentials"
	credentialHarnessClaudeState       credentialID = "harness.claude.state"
	credentialHarnessCodexAuth         credentialID = "harness.codex.auth"
	credentialHarnessOpenCodeAuth      credentialID = "harness.opencode.auth"
	credentialHarnessGeminiOAuth       credentialID = "harness.gemini.oauth"
	credentialHarnessGeminiAccounts    credentialID = "harness.gemini.accounts"
)

// credentialDescriptor is the only place a durable credential surface should
// name both its host-owned storage and its session delivery mode.
type credentialDescriptor struct {
	ID              credentialID
	DisplayName     string
	Kind            credentialKind
	Backend         credentialStorageBackend
	Delivery        credentialDeliveryMode
	StoreRelPath    string
	Harness         HarnessID
	EnvVar          string
	AgentPath       string
	LegacyPaths     []string
	Redacted        bool
	ConflictArchive bool
}

var builtinCredentialRegistry = []credentialDescriptor{
	{
		ID:           credentialProviderAnthropicAPIKey,
		DisplayName:  "Anthropic API key",
		Kind:         credentialKindProviderAPIKey,
		Backend:      credentialStorageHostSecretStore,
		Delivery:     credentialDeliveryEnv,
		StoreRelPath: "providers/anthropic-api-key",
		Harness:      HarnessClaude,
		EnvVar:       "ANTHROPIC_API_KEY",
		LegacyPaths:  []string{agentZshrcPath},
		Redacted:     true,
	},
	{
		ID:           credentialProviderOpenAIAPIKey,
		DisplayName:  "OpenAI API key",
		Kind:         credentialKindProviderAPIKey,
		Backend:      credentialStorageHostSecretStore,
		Delivery:     credentialDeliveryEnv,
		StoreRelPath: "providers/openai-api-key",
		Harness:      HarnessCodex,
		EnvVar:       "OPENAI_API_KEY",
		LegacyPaths:  []string{agentZshrcPath},
		Redacted:     true,
	},
	{
		ID:           credentialProviderGeminiAPIKey,
		DisplayName:  "Gemini API key",
		Kind:         credentialKindProviderAPIKey,
		Backend:      credentialStorageHostSecretStore,
		Delivery:     credentialDeliveryEnv,
		StoreRelPath: "providers/gemini-api-key",
		Harness:      HarnessGemini,
		EnvVar:       "GEMINI_API_KEY",
		LegacyPaths:  []string{agentZshrcPath},
		Redacted:     true,
	},
	{
		ID:              credentialHarnessClaudeCredentials,
		DisplayName:     "Claude credential file",
		Kind:            credentialKindHarnessAuth,
		Backend:         credentialStorageHostSecretStore,
		Delivery:        credentialDeliveryMaterializedFile,
		StoreRelPath:    "claude/credentials.json",
		Harness:         HarnessClaude,
		AgentPath:       agentHome + "/.claude/.credentials.json",
		LegacyPaths:     []string{agentHome + "/.claude/.credentials.json"},
		Redacted:        true,
		ConflictArchive: true,
	},
	{
		ID:              credentialHarnessClaudeState,
		DisplayName:     "Claude account state",
		Kind:            credentialKindHarnessAuth,
		Backend:         credentialStorageHostSecretStore,
		Delivery:        credentialDeliveryMaterializedFile,
		StoreRelPath:    "claude/state.json",
		Harness:         HarnessClaude,
		AgentPath:       agentHome + "/.claude.json",
		LegacyPaths:     []string{agentHome + "/.claude.json"},
		Redacted:        true,
		ConflictArchive: true,
	},
	{
		ID:              credentialHarnessCodexAuth,
		DisplayName:     "Codex auth file",
		Kind:            credentialKindHarnessAuth,
		Backend:         credentialStorageHostSecretStore,
		Delivery:        credentialDeliveryMaterializedFile,
		StoreRelPath:    "codex/auth.json",
		Harness:         HarnessCodex,
		AgentPath:       agentHome + "/.codex/auth.json",
		LegacyPaths:     []string{agentHome + "/.codex/auth.json"},
		Redacted:        true,
		ConflictArchive: true,
	},
	{
		ID:              credentialHarnessOpenCodeAuth,
		DisplayName:     "OpenCode auth file",
		Kind:            credentialKindHarnessAuth,
		Backend:         credentialStorageHostSecretStore,
		Delivery:        credentialDeliveryMaterializedFile,
		StoreRelPath:    "opencode/auth.json",
		Harness:         HarnessOpenCode,
		AgentPath:       agentHome + "/.local/share/opencode/auth.json",
		LegacyPaths:     []string{agentHome + "/.local/share/opencode/auth.json"},
		Redacted:        true,
		ConflictArchive: true,
	},
	{
		ID:              credentialHarnessGeminiOAuth,
		DisplayName:     "Gemini OAuth credentials",
		Kind:            credentialKindHarnessAuth,
		Backend:         credentialStorageHostSecretStore,
		Delivery:        credentialDeliveryMaterializedFile,
		StoreRelPath:    "gemini/oauth_creds.json",
		Harness:         HarnessGemini,
		AgentPath:       agentHome + "/.gemini/oauth_creds.json",
		LegacyPaths:     []string{agentHome + "/.gemini/oauth_creds.json"},
		Redacted:        true,
		ConflictArchive: true,
	},
	{
		ID:              credentialHarnessGeminiAccounts,
		DisplayName:     "Gemini account index",
		Kind:            credentialKindHarnessAuth,
		Backend:         credentialStorageHostSecretStore,
		Delivery:        credentialDeliveryMaterializedFile,
		StoreRelPath:    "gemini/google_accounts.json",
		Harness:         HarnessGemini,
		AgentPath:       agentHome + "/.gemini/google_accounts.json",
		LegacyPaths:     []string{agentHome + "/.gemini/google_accounts.json"},
		Redacted:        true,
		ConflictArchive: true,
	},
}

func builtinCredentialDescriptors() []credentialDescriptor {
	descriptors := make([]credentialDescriptor, len(builtinCredentialRegistry))
	copy(descriptors, builtinCredentialRegistry)
	return descriptors
}

func findCredentialDescriptor(id credentialID) (credentialDescriptor, bool) {
	for _, descriptor := range builtinCredentialRegistry {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return credentialDescriptor{}, false
}

func mustCredentialDescriptor(id credentialID) credentialDescriptor {
	descriptor, ok := findCredentialDescriptor(id)
	if !ok {
		panic(fmt.Sprintf("missing credential descriptor %q", id))
	}
	return descriptor
}

func providerCredentialDescriptorForEnvVar(envVar string) (credentialDescriptor, bool) {
	for _, descriptor := range builtinCredentialRegistry {
		if descriptor.Kind == credentialKindProviderAPIKey && descriptor.EnvVar == envVar {
			return descriptor, true
		}
	}
	return credentialDescriptor{}, false
}

func credentialStorePathForHome(home string, id credentialID) (string, error) {
	descriptor, ok := findCredentialDescriptor(id)
	if !ok {
		return "", fmt.Errorf("no credential descriptor for %s", id)
	}
	return descriptor.StorePathForHome(home)
}

func mustCredentialStorePathForHome(home string, id credentialID) string {
	storePath, err := credentialStorePathForHome(home, id)
	if err != nil {
		panic(err)
	}
	return storePath
}

func (descriptor credentialDescriptor) StorePathForHome(home string) (string, error) {
	if descriptor.Backend != credentialStorageHostSecretStore {
		return "", fmt.Errorf("%s uses %s, not host secret store", descriptor.ID, descriptor.Backend)
	}
	cleanRelPath, err := cleanCredentialStoreRelPath(descriptor.StoreRelPath)
	if err != nil {
		return "", fmt.Errorf("%s store path: %w", descriptor.ID, err)
	}
	return filepath.Join(secretStoreDirForHome(home), filepath.FromSlash(cleanRelPath)), nil
}

func (descriptor credentialDescriptor) AgentMaterializationPath() (string, error) {
	if descriptor.Delivery != credentialDeliveryMaterializedFile {
		return "", fmt.Errorf("%s uses %s delivery, not materialized file delivery", descriptor.ID, descriptor.Delivery)
	}
	if descriptor.AgentPath == "" {
		return "", fmt.Errorf("%s materialized file delivery has no agent path", descriptor.ID)
	}
	if !usesManagedAgentPath(descriptor.AgentPath) {
		return "", fmt.Errorf("%s materializes outside managed agent home: %s", descriptor.ID, descriptor.AgentPath)
	}
	return descriptor.AgentPath, nil
}

func (descriptor credentialDescriptor) EnvDeliveryVar() (string, error) {
	if descriptor.Delivery != credentialDeliveryEnv {
		return "", fmt.Errorf("%s uses %s delivery, not env delivery", descriptor.ID, descriptor.Delivery)
	}
	if descriptor.EnvVar == "" {
		return "", fmt.Errorf("%s env delivery has no env var", descriptor.ID)
	}
	return descriptor.EnvVar, nil
}

func cleanCredentialStoreRelPath(relPath string) (string, error) {
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
