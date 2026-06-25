package hazmat

import (
	"hazmat/credentials"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolateCredentialInventoryTest(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	savedConfigPath := configFilePath
	savedCloudCredentialPath := cloudCredentialPath
	savedAgentZshrcPath := agentZshrcPath
	savedGitHTTPSAgentGitConfigPath := gitHTTPSAgentGitConfigPath
	savedPathExists := credentialInventoryPathExists
	savedReadFile := credentialInventoryReadFile
	savedAgentPathExists := credentialInventoryAgentPathExists
	savedAgentReadFile := credentialInventoryAgentReadFile

	configFilePath = filepath.Join(home, ".hazmat", "config.yaml")
	cloudCredentialPath = filepath.Join(home, ".hazmat", "cloud-credentials")
	agentZshrcPath = filepath.Join(home, "agent", ".zshrc")
	gitHTTPSAgentGitConfigPath = filepath.Join(home, "agent", ".gitconfig")
	credentialInventoryPathExists = credentialInventoryPathExistsOnDisk
	credentialInventoryReadFile = os.ReadFile
	credentialInventoryAgentPathExists = credentialInventoryPathExistsOnDisk
	credentialInventoryAgentReadFile = os.ReadFile

	t.Cleanup(func() {
		configFilePath = savedConfigPath
		cloudCredentialPath = savedCloudCredentialPath
		agentZshrcPath = savedAgentZshrcPath
		gitHTTPSAgentGitConfigPath = savedGitHTTPSAgentGitConfigPath
		credentialInventoryPathExists = savedPathExists
		credentialInventoryReadFile = savedReadFile
		credentialInventoryAgentPathExists = savedAgentPathExists
		credentialInventoryAgentReadFile = savedAgentReadFile
	})
	return home
}

func TestCredentialInventoryReportsLegacyProviderExportWithoutSecretValue(t *testing.T) {
	home := isolateCredentialInventoryTest(t)
	const secretValue = "example-secret-do-not-print"

	storePath := mustCredentialStorePathForHome(home, credentials.ProviderOpenAIAPIKey)
	if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, []byte(secretValue+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(agentZshrcPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentZshrcPath, []byte("export OPENAI_API_KEY="+secretValue+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := inspectCredentialInventory(home)
	if err != nil {
		t.Fatalf("inspectCredentialInventory: %v", err)
	}
	entry := findInventoryEntryForTest(t, entries, credentials.ProviderOpenAIAPIKey)
	if !entry.HostStorePresent {
		t.Fatal("OpenAI host-store credential was not reported present")
	}
	if got := entry.Status(); got != credentialInventoryNeedsRepair {
		t.Fatalf("OpenAI status = %s, want %s", got, credentialInventoryNeedsRepair)
	}
	if len(entry.LegacyResidue) != 1 {
		t.Fatalf("OpenAI legacy residue = %v, want one finding", entry.LegacyResidue)
	}

	rendered := renderInventoryEntryForTest(entry)
	if strings.Contains(rendered, secretValue) {
		t.Fatalf("inventory output leaked secret value: %s", rendered)
	}
	for _, want := range []string{"provider.openai.api-key", "host-store=present", "legacy agent-home provider API-key export", "credential repair"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("inventory output missing %q:\n%s", want, rendered)
		}
	}
	assertNoCredentialInventoryCommandRecipe(t, rendered)
}

func TestCredentialInventoryReportsMaterializedAndGitHTTPSResidue(t *testing.T) {
	home := isolateCredentialInventoryTest(t)
	codexStorePath := mustCredentialStorePathForHome(home, credentials.HarnessCodexAuth)
	if err := os.MkdirAll(filepath.Dir(codexStorePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexStorePath, []byte(`{"token":"redacted"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	realAgentPathExists := credentialInventoryAgentPathExists
	credentialInventoryAgentPathExists = func(path string) (bool, error) {
		switch path {
		case agentHome + "/.codex/auth.json", gitHTTPSAgentCredentialsPath:
			return true, nil
		default:
			return realAgentPathExists(path)
		}
	}

	entries, err := inspectCredentialInventory(home)
	if err != nil {
		t.Fatalf("inspectCredentialInventory: %v", err)
	}

	codex := findInventoryEntryForTest(t, entries, credentials.HarnessCodexAuth)
	if got := codex.Status(); got != credentialInventoryNeedsRepair {
		t.Fatalf("Codex status = %s, want %s", got, credentialInventoryNeedsRepair)
	}
	if len(codex.AgentResidue) != 1 || !strings.Contains(codex.AgentResidue[0].Detail, "stale agent-home") {
		t.Fatalf("Codex agent residue = %v, want stale materialized file finding", codex.AgentResidue)
	}

	gitHTTPS := findInventoryEntryForTest(t, entries, credentials.GitHTTPSAgentStore)
	if got := gitHTTPS.Status(); got != credentialInventoryNeedsRepair {
		t.Fatalf("Git HTTPS status = %s, want %s", got, credentialInventoryNeedsRepair)
	}
	if len(gitHTTPS.AgentResidue) != 1 || !strings.Contains(gitHTTPS.AgentResidue[0].Repair, "credential repair") {
		t.Fatalf("Git HTTPS residue = %v, want broker repair guidance", gitHTTPS.AgentResidue)
	}
}

func TestCredentialInventoryHostOnlySkipsAgentStateProbes(t *testing.T) {
	home := isolateCredentialInventoryTest(t)
	openAIStorePath := mustCredentialStorePathForHome(home, credentials.ProviderOpenAIAPIKey)
	if err := os.MkdirAll(filepath.Dir(openAIStorePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(openAIStorePath, []byte("host-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(configFilePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFilePath, []byte("backup:\n  cloud:\n    access_key: legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	credentialInventoryAgentPathExists = func(path string) (bool, error) {
		t.Fatalf("host-only credential inventory touched agent path %s", path)
		return false, nil
	}
	credentialInventoryAgentReadFile = func(path string) ([]byte, error) {
		t.Fatalf("host-only credential inventory read agent path %s", path)
		return nil, os.ErrNotExist
	}

	entries, err := inspectCredentialInventoryHostOnly(home)
	if err != nil {
		t.Fatalf("inspectCredentialInventoryHostOnly: %v", err)
	}
	openAI := findInventoryEntryForTest(t, entries, credentials.ProviderOpenAIAPIKey)
	if !openAI.HostStorePresent {
		t.Fatal("host-only inventory did not report host OpenAI store")
	}
	if len(openAI.AgentResidue) != 0 || len(openAI.LegacyResidue) != 0 {
		t.Fatalf("OpenAI host-only residue = agent:%v legacy:%v, want none without agent probes", openAI.AgentResidue, openAI.LegacyResidue)
	}
	cloud := findInventoryEntryForTest(t, entries, credentials.CloudS3AccessKeyID)
	if len(cloud.LegacyResidue) != 1 {
		t.Fatalf("cloud host legacy residue = %v, want host config residue still reported", cloud.LegacyResidue)
	}
}

func TestRunStatusUsesHostOnlyCredentialInventory(t *testing.T) {
	isolateCredentialInventoryTest(t)
	credentialInventoryAgentPathExists = func(path string) (bool, error) {
		t.Fatalf("default status touched agent credential path %s", path)
		return false, nil
	}
	credentialInventoryAgentReadFile = func(path string) ([]byte, error) {
		t.Fatalf("default status read agent credential path %s", path)
		return nil, os.ErrNotExist
	}

	if err := runStatus(false); err != nil {
		t.Fatalf("runStatus(false): %v", err)
	}
}

func TestCredentialInventoryIgnoresClaudeSettingsOnlyAgentState(t *testing.T) {
	home := isolateCredentialInventoryTest(t)
	claudeStorePath := mustCredentialStorePathForHome(home, credentials.HarnessClaudeState)
	if err := os.MkdirAll(filepath.Dir(claudeStorePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeStorePath, []byte(`{"userID":"host-user"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	agentPath := mustCredentialDescriptor(credentials.HarnessClaudeState).AgentPath
	credentialInventoryAgentPathExists = func(path string) (bool, error) {
		return path == agentPath, nil
	}
	credentialInventoryAgentReadFile = func(path string) ([]byte, error) {
		if path != agentPath {
			return nil, os.ErrNotExist
		}
		return []byte(`{"theme":"dark","projects":{}}` + "\n"), nil
	}

	entries, err := inspectCredentialInventory(home)
	if err != nil {
		t.Fatalf("inspectCredentialInventory: %v", err)
	}

	entry := findInventoryEntryForTest(t, entries, credentials.HarnessClaudeState)
	if got := entry.Status(); got != credentialInventoryConfigured {
		t.Fatalf("Claude state status = %s, want %s; residue=%v errors=%v", got, credentialInventoryConfigured, entry.AgentResidue, entry.Errors)
	}
	if len(entry.AgentResidue) != 0 {
		t.Fatalf("Claude state agent residue = %v, want none for settings-only state file", entry.AgentResidue)
	}
}

func TestCredentialInventoryReportsClaudePortableAuthStateResidue(t *testing.T) {
	home := isolateCredentialInventoryTest(t)
	const secretValue = "legacy-user-id"
	claudeStorePath := mustCredentialStorePathForHome(home, credentials.HarnessClaudeState)
	if err := os.MkdirAll(filepath.Dir(claudeStorePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeStorePath, []byte(`{"userID":"host-user"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	agentPath := mustCredentialDescriptor(credentials.HarnessClaudeState).AgentPath
	credentialInventoryAgentPathExists = func(path string) (bool, error) {
		return path == agentPath, nil
	}
	credentialInventoryAgentReadFile = func(path string) ([]byte, error) {
		if path != agentPath {
			return nil, os.ErrNotExist
		}
		return []byte(`{"theme":"dark","userID":"` + secretValue + `"}` + "\n"), nil
	}

	entries, err := inspectCredentialInventory(home)
	if err != nil {
		t.Fatalf("inspectCredentialInventory: %v", err)
	}

	entry := findInventoryEntryForTest(t, entries, credentials.HarnessClaudeState)
	if got := entry.Status(); got != credentialInventoryNeedsRepair {
		t.Fatalf("Claude state status = %s, want %s", got, credentialInventoryNeedsRepair)
	}
	if len(entry.AgentResidue) != 1 || !strings.Contains(entry.AgentResidue[0].Detail, "Claude portable auth state") {
		t.Fatalf("Claude state residue = %v, want portable auth state finding", entry.AgentResidue)
	}

	rendered := renderInventoryEntryForTest(entry)
	if strings.Contains(rendered, secretValue) {
		t.Fatalf("inventory output leaked Claude state value: %s", rendered)
	}
	assertNoCredentialInventoryCommandRecipe(t, rendered)
}

func TestCredentialInventoryUsesAgentProbesForPrivateAgentPaths(t *testing.T) {
	home := isolateCredentialInventoryTest(t)
	hostStorePath := mustCredentialStorePathForHome(home, credentials.HarnessCodexAuth)
	if err := os.MkdirAll(filepath.Dir(hostStorePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostStorePath, []byte(`{"token":"redacted"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	credentialInventoryPathExists = func(path string) (bool, error) {
		if strings.Contains(path, string(filepath.Separator)+"agent"+string(filepath.Separator)) ||
			strings.HasPrefix(path, agentHome+"/") {
			return false, os.ErrPermission
		}
		return credentialInventoryPathExistsOnDisk(path)
	}
	credentialInventoryAgentPathExists = func(string) (bool, error) {
		return false, nil
	}
	credentialInventoryAgentReadFile = func(path string) ([]byte, error) {
		return nil, os.ErrNotExist
	}

	entries, err := inspectCredentialInventory(home)
	if err != nil {
		t.Fatalf("inspectCredentialInventory: %v", err)
	}

	codex := findInventoryEntryForTest(t, entries, credentials.HarnessCodexAuth)
	if got := codex.Status(); got != credentialInventoryConfigured {
		t.Fatalf("Codex status = %s, want %s; errors=%v", got, credentialInventoryConfigured, codex.Errors)
	}
	openCode := findInventoryEntryForTest(t, entries, credentials.HarnessOpenCodeAuth)
	if got := openCode.Status(); got != credentialInventoryNotConfigured {
		t.Fatalf("OpenCode status = %s, want %s; errors=%v", got, credentialInventoryNotConfigured, openCode.Errors)
	}
	provider := findInventoryEntryForTest(t, entries, credentials.ProviderOpenAIAPIKey)
	if got := provider.Status(); got != credentialInventoryNotConfigured {
		t.Fatalf("OpenAI provider status = %s, want %s; errors=%v", got, credentialInventoryNotConfigured, provider.Errors)
	}
}

func TestCredentialInventoryReportsLegacyCloudCredentialsWithoutSecretValues(t *testing.T) {
	home := isolateCredentialInventoryTest(t)
	const accessKey = "legacy-access"
	const recoveryKey = "legacy-recovery"
	const secretKey = "legacy-secret"

	if err := os.MkdirAll(filepath.Dir(configFilePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFilePath, []byte(`backup:
  cloud:
    endpoint: s3.example.com
    bucket: hazmat
    access_key: `+accessKey+`
    password: `+recoveryKey+`
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cloudCredentialPath, []byte(secretKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := inspectCredentialInventory(home)
	if err != nil {
		t.Fatalf("inspectCredentialInventory: %v", err)
	}
	for _, id := range []credentials.ID{
		credentials.CloudS3AccessKeyID,
		credentials.CloudS3SecretKey,
		credentials.CloudKopiaRecovery,
	} {
		entry := findInventoryEntryForTest(t, entries, id)
		if got := entry.Status(); got != credentialInventoryNeedsRepair {
			t.Fatalf("%s status = %s, want %s", id, got, credentialInventoryNeedsRepair)
		}
		rendered := renderInventoryEntryForTest(entry)
		for _, forbidden := range []string{accessKey, recoveryKey, secretKey} {
			if strings.Contains(rendered, forbidden) {
				t.Fatalf("%s inventory output leaked %q:\n%s", id, forbidden, rendered)
			}
		}
		assertNoCredentialInventoryCommandRecipe(t, rendered)
	}
}

func assertNoCredentialInventoryCommandRecipe(t *testing.T, rendered string) {
	t.Helper()

	for _, forbidden := range []string{
		"hazmat config",
		"launch the matching harness",
		"run `",
		"sudo ",
		"chmod ",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("inventory output contains command recipe %q:\n%s", forbidden, rendered)
		}
	}
}

func findInventoryEntryForTest(t *testing.T, entries []credentialInventoryEntry, id credentials.ID) credentialInventoryEntry {
	t.Helper()

	for _, entry := range entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("missing inventory entry %s", id)
	return credentialInventoryEntry{}
}

func renderInventoryEntryForTest(entry credentialInventoryEntry) string {
	var b strings.Builder
	b.WriteString(formatCredentialInventoryEntry(entry))
	for _, finding := range entry.AgentResidue {
		b.WriteString("\n")
		b.WriteString(formatCredentialInventoryFinding("agent-home residue", finding))
	}
	for _, finding := range entry.LegacyResidue {
		b.WriteString("\n")
		b.WriteString(formatCredentialInventoryFinding("legacy residue", finding))
	}
	for _, hint := range entry.RepairHints() {
		b.WriteString("\n")
		b.WriteString(hint)
	}
	for _, errText := range entry.Errors {
		b.WriteString("\n")
		b.WriteString(errText)
	}
	return b.String()
}
