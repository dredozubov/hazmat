package main

import (
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

	configFilePath = filepath.Join(home, ".hazmat", "config.yaml")
	cloudCredentialPath = filepath.Join(home, ".hazmat", "cloud-credentials")
	agentZshrcPath = filepath.Join(home, "agent", ".zshrc")
	gitHTTPSAgentGitConfigPath = filepath.Join(home, "agent", ".gitconfig")
	credentialInventoryPathExists = credentialInventoryPathExistsOnDisk
	credentialInventoryReadFile = os.ReadFile

	t.Cleanup(func() {
		configFilePath = savedConfigPath
		cloudCredentialPath = savedCloudCredentialPath
		agentZshrcPath = savedAgentZshrcPath
		gitHTTPSAgentGitConfigPath = savedGitHTTPSAgentGitConfigPath
		credentialInventoryPathExists = savedPathExists
		credentialInventoryReadFile = savedReadFile
	})
	return home
}

func TestCredentialInventoryReportsLegacyProviderExportWithoutSecretValue(t *testing.T) {
	home := isolateCredentialInventoryTest(t)
	const secretValue = "example-secret-do-not-print"

	storePath := mustCredentialStorePathForHome(home, credentialProviderOpenAIAPIKey)
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
	entry := findInventoryEntryForTest(t, entries, credentialProviderOpenAIAPIKey)
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
	for _, want := range []string{"provider.openai.api-key", "host-store=present", "legacy agent-home provider API-key export", "hazmat config agent"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("inventory output missing %q:\n%s", want, rendered)
		}
	}
}

func TestCredentialInventoryReportsMaterializedAndGitHTTPSResidue(t *testing.T) {
	home := isolateCredentialInventoryTest(t)
	codexStorePath := mustCredentialStorePathForHome(home, credentialHarnessCodexAuth)
	if err := os.MkdirAll(filepath.Dir(codexStorePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexStorePath, []byte(`{"token":"redacted"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	realPathExists := credentialInventoryPathExists
	credentialInventoryPathExists = func(path string) (bool, error) {
		switch path {
		case agentHome + "/.codex/auth.json", gitHTTPSAgentCredentialsPath:
			return true, nil
		default:
			return realPathExists(path)
		}
	}

	entries, err := inspectCredentialInventory(home)
	if err != nil {
		t.Fatalf("inspectCredentialInventory: %v", err)
	}

	codex := findInventoryEntryForTest(t, entries, credentialHarnessCodexAuth)
	if got := codex.Status(); got != credentialInventoryNeedsRepair {
		t.Fatalf("Codex status = %s, want %s", got, credentialInventoryNeedsRepair)
	}
	if len(codex.AgentResidue) != 1 || !strings.Contains(codex.AgentResidue[0].Detail, "stale agent-home") {
		t.Fatalf("Codex agent residue = %v, want stale materialized file finding", codex.AgentResidue)
	}

	gitHTTPS := findInventoryEntryForTest(t, entries, credentialGitHTTPSAgentStore)
	if got := gitHTTPS.Status(); got != credentialInventoryNeedsRepair {
		t.Fatalf("Git HTTPS status = %s, want %s", got, credentialInventoryNeedsRepair)
	}
	if len(gitHTTPS.AgentResidue) != 1 || !strings.Contains(gitHTTPS.AgentResidue[0].Repair, "migrate the Git HTTPS credentials") {
		t.Fatalf("Git HTTPS residue = %v, want broker repair guidance", gitHTTPS.AgentResidue)
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
	for _, id := range []credentialID{
		credentialCloudS3AccessKeyID,
		credentialCloudS3SecretKey,
		credentialCloudKopiaRecovery,
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
	}
}

func findInventoryEntryForTest(t *testing.T, entries []credentialInventoryEntry, id credentialID) credentialInventoryEntry {
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
