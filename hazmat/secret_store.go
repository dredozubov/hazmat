package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type configuredAPIKeySource string

const (
	configuredAPIKeySourceNone   configuredAPIKeySource = ""
	configuredAPIKeySourceStore  configuredAPIKeySource = "host-secret-store"
	configuredAPIKeySourceLegacy configuredAPIKeySource = "legacy-agent-zshrc"
)

var agentZshrcPath = filepath.Join(agentHome, ".zshrc")

func secretStoreDirForHome(home string) string {
	return filepath.Join(home, ".hazmat", "secrets")
}

func providerSecretStorePath(envVar string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory for Hazmat secrets: %w", err)
	}
	return providerSecretStorePathForHome(home, envVar)
}

func providerSecretStorePathForHome(home, envVar string) (string, error) {
	var filename string
	switch envVar {
	case "ANTHROPIC_API_KEY":
		filename = "anthropic-api-key"
	case "OPENAI_API_KEY":
		filename = "openai-api-key"
	case "GEMINI_API_KEY":
		filename = "gemini-api-key"
	default:
		return "", fmt.Errorf("no host secret-store mapping for %s", envVar)
	}
	return filepath.Join(secretStoreDirForHome(home), "providers", filename), nil
}

func claudeCredentialStorePathForHome(home string) string {
	return filepath.Join(secretStoreDirForHome(home), "claude", "credentials.json")
}

func claudeStateStorePathForHome(home string) string {
	return filepath.Join(secretStoreDirForHome(home), "claude", "state.json")
}

func codexAuthStorePathForHome(home string) string {
	return filepath.Join(secretStoreDirForHome(home), "codex", "auth.json")
}

func openCodeAuthStorePathForHome(home string) string {
	return filepath.Join(secretStoreDirForHome(home), "opencode", "auth.json")
}

func geminiOAuthStorePathForHome(home string) string {
	return filepath.Join(secretStoreDirForHome(home), "gemini", "oauth_creds.json")
}

func geminiAccountsStorePathForHome(home string) string {
	return filepath.Join(secretStoreDirForHome(home), "gemini", "google_accounts.json")
}

func readHostStoredSecretFile(path string) ([]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return raw, true, nil
}

func writeHostStoredSecretFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func usesManagedAgentPath(path string) bool {
	return path == agentHome || isWithinDir(agentHome, path)
}

func readAgentSecretFile(path string) ([]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		return raw, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if !usesManagedAgentPath(path) {
		return nil, false, err
	}

	out, agentErr := newAgentCommand("cat", path).Output()
	if agentErr != nil {
		return nil, false, agentErr
	}
	return out, true, nil
}

func writeAgentSecretFile(path string, raw []byte, mode os.FileMode) error {
	if !usesManagedAgentPath(path) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, raw, mode); err != nil {
			return err
		}
		return os.Chmod(path, mode)
	}
	if err := agentMkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	return agentWriteFile(path, raw, mode)
}

func removeAgentSecretFile(path string) error {
	if !usesManagedAgentPath(path) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := asAgentQuiet("rm", "-f", path); err != nil {
		return err
	}
	return nil
}

func writeJSONMapStoreFile(path string, payload map[string]json.RawMessage) error {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeHostStoredSecretFile(path, raw)
}

func readJSONMapStoreFile(path string) (map[string]json.RawMessage, bool, error) {
	raw, ok, err := readHostStoredSecretFile(path)
	if err != nil || !ok {
		return nil, ok, err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false, err
	}
	if len(payload) == 0 {
		return nil, false, nil
	}
	return payload, true, nil
}

func readClaudeStateKeysFromAgent(path string) (map[string]json.RawMessage, bool, error) {
	raw, ok, err := readAgentSecretFile(path)
	if err != nil || !ok {
		return nil, ok, err
	}
	payload, err := selectClaudeAuthKeys(raw)
	if err != nil {
		return nil, false, err
	}
	if len(payload) == 0 {
		return nil, false, nil
	}
	return payload, true, nil
}

func writeClaudeStateKeysToAgent(path string, payload map[string]json.RawMessage) error {
	currentRaw, ok, err := readAgentSecretFile(path)
	if err != nil {
		return err
	}

	current := map[string]json.RawMessage{}
	if ok && len(bytes.TrimSpace(currentRaw)) > 0 {
		if err := json.Unmarshal(currentRaw, &current); err != nil {
			return err
		}
	}
	for key, value := range payload {
		current[key] = value
	}

	raw, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAgentSecretFile(path, raw, 0o600)
}

func removeClaudeStateKeysFromAgent(path string) error {
	raw, ok, err := readAgentSecretFile(path)
	if err != nil || !ok {
		return err
	}

	var current map[string]json.RawMessage
	if err := json.Unmarshal(raw, &current); err != nil {
		return err
	}
	for _, key := range claudePortableAuthKeys {
		delete(current, key)
	}
	if len(current) == 0 {
		return removeAgentSecretFile(path)
	}

	updated, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	return writeAgentSecretFile(path, updated, 0o600)
}

func readHostStoredAPIKey(spec harnessAPIKeySpec) (string, error) {
	path, err := providerSecretStorePath(spec.EnvVar)
	if err != nil {
		return "", err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s from host secret store: %w", spec.EnvVar, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func storeHostAPIKey(spec harnessAPIKeySpec, value string) error {
	path, err := providerSecretStorePath(spec.EnvVar)
	if err != nil {
		return err
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s cannot be empty", spec.EnvVar)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create host secret-store directory for %s: %w", spec.EnvVar, err)
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s to host secret store: %w", spec.EnvVar, err)
	}
	return nil
}

func lookupConfiguredAPIKey(spec harnessAPIKeySpec) (string, configuredAPIKeySource, error) {
	value, err := readHostStoredAPIKey(spec)
	if err != nil {
		return "", configuredAPIKeySourceNone, err
	}
	if value != "" {
		return value, configuredAPIKeySourceStore, nil
	}

	line := readZshrcEnvLine(agentZshrcPath, spec.EnvVar)
	if line == "" {
		return "", configuredAPIKeySourceNone, nil
	}

	value, ok := parseExportedEnvLineValue(line, spec.EnvVar)
	if !ok || strings.TrimSpace(value) == "" {
		return "", configuredAPIKeySourceNone, nil
	}
	return value, configuredAPIKeySourceLegacy, nil
}

func parseExportedEnvLineValue(line, envVar string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	prefix := "export " + envVar + "="
	if !strings.HasPrefix(trimmed, prefix) {
		return "", false
	}

	raw := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	if raw == "" {
		return "", false
	}
	if value, err := strconv.Unquote(raw); err == nil {
		return value, true
	}
	return strings.Trim(raw, "\"'"), true
}

func harnessAPIKeySpecForHarness(id HarnessID) (harnessAPIKeySpec, bool) {
	for _, spec := range harnessAPIKeyPrompts {
		if spec.Harness == id {
			return spec, true
		}
	}
	return harnessAPIKeySpec{}, false
}

func harnessAPIKeyPromptByEnvVar(envVar string) harnessAPIKeySpec {
	for _, spec := range harnessAPIKeyPrompts {
		if spec.EnvVar == envVar {
			return spec
		}
	}
	return harnessAPIKeySpec{EnvVar: envVar}
}

func removableLegacyAPIKeyEnvVars(pending []pendingAPIKeyUpdate) []string {
	seen := make(map[string]struct{}, len(harnessAPIKeyPrompts)+len(pending))
	var envVars []string

	for _, update := range pending {
		if _, dup := seen[update.EnvVar]; dup {
			continue
		}
		seen[update.EnvVar] = struct{}{}
		envVars = append(envVars, update.EnvVar)
	}

	for _, spec := range harnessAPIKeyPrompts {
		if _, dup := seen[spec.EnvVar]; dup {
			continue
		}
		value, err := readHostStoredAPIKey(spec)
		if err != nil || value == "" {
			continue
		}
		seen[spec.EnvVar] = struct{}{}
		envVars = append(envVars, spec.EnvVar)
	}

	return envVars
}

func apiKeyRemovalUpdates(envVars []string) []pendingAPIKeyUpdate {
	updates := make([]pendingAPIKeyUpdate, 0, len(envVars))
	for _, envVar := range envVars {
		updates = append(updates, pendingAPIKeyUpdate{EnvVar: envVar})
	}
	return updates
}

func hasLegacyAPIKeyExports(envVars []string) bool {
	for _, envVar := range envVars {
		if readZshrcEnvLine(agentZshrcPath, envVar) != "" {
			return true
		}
	}
	return false
}

func removeLegacyAPIKeyExports(envVars []string) error {
	return updateAgentFile(
		agentZshrcPath,
		func(content string) string {
			return rewriteZshrcAPIKeys(content, apiKeyRemovalUpdates(envVars))
		},
		0o600,
	)
}

func applyHarnessAPIKeyEnv(cfg *sessionConfig) error {
	spec, ok := harnessAPIKeySpecForHarness(cfg.HarnessID)
	if !ok {
		return nil
	}

	value, source, err := lookupConfiguredAPIKey(spec)
	if err != nil {
		return err
	}
	if value == "" {
		return nil
	}

	switch source {
	case configuredAPIKeySourceStore:
	case configuredAPIKeySourceLegacy:
		if err := storeHostAPIKey(spec, value); err != nil {
			cfg.SessionNotes = append(cfg.SessionNotes,
				fmt.Sprintf("Using legacy %s from %s because migration into ~/.hazmat/secrets failed: %v", spec.EnvVar, agentZshrcPath, err))
			break
		}
		if err := removeLegacyAPIKeyExports([]string{spec.EnvVar}); err != nil {
			cfg.SessionNotes = append(cfg.SessionNotes,
				fmt.Sprintf("Migrated legacy %s into ~/.hazmat/secrets, but could not remove the old export from %s: %v", spec.EnvVar, agentZshrcPath, err))
		} else {
			cfg.SessionNotes = append(cfg.SessionNotes,
				fmt.Sprintf("Migrated legacy %s from %s into ~/.hazmat/secrets.", spec.EnvVar, agentZshrcPath))
		}
	default:
		return nil
	}

	if cfg.HarnessEnv == nil {
		cfg.HarnessEnv = make(map[string]string, 1)
	}
	cfg.HarnessEnv[spec.EnvVar] = value
	return nil
}
