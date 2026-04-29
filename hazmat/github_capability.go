package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	githubPrimaryTokenEnvVar  = "GH_TOKEN"
	githubFallbackTokenEnvVar = "GITHUB_TOKEN"
	githubServiceAccess       = "github-api"
)

func githubTokenStorePathForHome(home string) string {
	return mustCredentialStorePathForHome(home, credentialGitHubAPIToken)
}

func githubTokenStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory for GitHub token: %w", err)
	}
	return githubTokenStorePathForHome(home), nil
}

func readGitHubStoredToken() (string, bool, error) {
	storePath, err := githubTokenStorePath()
	if err != nil {
		return "", false, err
	}
	raw, ok, err := readHostStoredSecretFile(storePath)
	if err != nil || !ok {
		return "", ok, err
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", false, nil
	}
	return token, true, nil
}

func saveGitHubStoredToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("GitHub token cannot be empty")
	}
	storePath, err := githubTokenStorePath()
	if err != nil {
		return err
	}
	return writeHostStoredSecretFile(storePath, []byte(token+"\n"))
}

func removeGitHubStoredToken() error {
	storePath, err := githubTokenStorePath()
	if err != nil {
		return err
	}
	if err := os.Remove(storePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove GitHub token from host secret store: %w", err)
	}
	return nil
}

func githubTokenFromEnv() (string, string) {
	if token := strings.TrimSpace(os.Getenv(githubPrimaryTokenEnvVar)); token != "" {
		return githubPrimaryTokenEnvVar, token
	}
	if token := strings.TrimSpace(os.Getenv(githubFallbackTokenEnvVar)); token != "" {
		return githubFallbackTokenEnvVar, token
	}
	return "", ""
}

func applyGitHubSessionCapability(cfg *sessionConfig, mode sessionMode) error {
	if mode != sessionModeNative {
		return fmt.Errorf("GitHub API credential capability is not supported for Docker Sandbox sessions yet\nuse --docker=none for a native contained session, or omit --github")
	}

	token, ok, err := readGitHubStoredToken()
	if err != nil {
		return fmt.Errorf("read GitHub token from host secret store: %w", err)
	}
	if !ok {
		return fmt.Errorf("GitHub API token is not configured\nRun: hazmat config github --token-from-env")
	}

	if cfg.HarnessEnv == nil {
		cfg.HarnessEnv = make(map[string]string, 1)
	}
	descriptor := mustCredentialDescriptor(credentialGitHubAPIToken)
	envVar, err := descriptor.EnvDeliveryVar()
	if err != nil {
		return err
	}
	cfg.HarnessEnv[envVar] = token
	cfg.CredentialEnvGrants = appendSessionCredentialEnvGrant(cfg.CredentialEnvGrants, sessionCredentialEnvGrant{
		EnvVar:       envVar,
		CredentialID: credentialGitHubAPIToken,
		Source:       "host secret store",
	})
	cfg.ServiceAccess = dedupeStrings(append(cfg.ServiceAccess, githubServiceAccess))
	cfg.SessionNotes = append(cfg.SessionNotes, "GitHub API access is enabled for this session via GH_TOKEN.")
	return nil
}
