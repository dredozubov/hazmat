package main

import (
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

func hostSecretStoreDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory for Hazmat secrets: %w", err)
	}
	return filepath.Join(home, ".hazmat", "secrets"), nil
}

func providerSecretStorePath(envVar string) (string, error) {
	root, err := hostSecretStoreDir()
	if err != nil {
		return "", err
	}

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
	return filepath.Join(root, "providers", filename), nil
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
