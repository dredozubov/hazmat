package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareHarnessAuthRuntimeMaterializesAndHarvestsRawAuth(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store", "codex", "auth.json")
	agentPath := filepath.Join(root, "agent", ".codex", "auth.json")
	initial := []byte(`{"token":"stored"}`)
	updated := []byte(`{"token":"updated"}`)

	if err := writeHostStoredSecretFile(storePath, initial); err != nil {
		t.Fatalf("writeHostStoredSecretFile: %v", err)
	}

	runtime, err := prepareHarnessAuthRuntimeForArtifacts([]harnessAuthArtifact{
		rawHarnessAuthArtifact("Codex auth file", storePath, agentPath),
	})
	if err != nil {
		t.Fatalf("prepareHarnessAuthRuntimeForArtifacts: %v", err)
	}

	agentRaw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read materialized agent auth: %v", err)
	}
	if string(agentRaw) != string(initial) {
		t.Fatalf("materialized agent auth = %q, want %q", agentRaw, initial)
	}

	if err := os.WriteFile(agentPath, updated, 0o600); err != nil {
		t.Fatalf("overwrite agent auth: %v", err)
	}

	runtime.Cleanup()

	storeRaw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read harvested store auth: %v", err)
	}
	if string(storeRaw) != string(updated) {
		t.Fatalf("harvested store auth = %q, want %q", storeRaw, updated)
	}
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("agent auth should be removed after cleanup, got err=%v", err)
	}
}

func TestMigrateHarnessAuthArtifactsMovesLegacyRawAuthIntoStore(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store", "opencode", "auth.json")
	agentPath := filepath.Join(root, "agent", ".local", "share", "opencode", "auth.json")
	legacy := []byte(`{"provider":"anthropic","token":"legacy"}`)

	if err := os.MkdirAll(filepath.Dir(agentPath), 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(agentPath, legacy, 0o600); err != nil {
		t.Fatalf("write legacy auth: %v", err)
	}

	var notes []string
	if err := migrateHarnessAuthArtifacts([]harnessAuthArtifact{
		rawHarnessAuthArtifact("OpenCode auth file", storePath, agentPath),
	}, func(note string) {
		notes = append(notes, note)
	}); err != nil {
		t.Fatalf("migrateHarnessAuthArtifacts: %v", err)
	}

	storeRaw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read migrated store auth: %v", err)
	}
	if string(storeRaw) != string(legacy) {
		t.Fatalf("migrated store auth = %q, want %q", storeRaw, legacy)
	}
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("legacy agent auth should be removed after migration, got err=%v", err)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "Migrated legacy OpenCode auth file") {
		t.Fatalf("migration notes = %v, want migration note", notes)
	}
}

func TestPrepareHarnessAuthRuntimeClaudeStateHarvestKeepsNonAuthKeys(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	agentPath := filepath.Join(root, "agent", ".claude.json")

	artifact := claudeStateHarnessAuthArtifact(home)
	artifact.AgentPath = agentPath

	initialStore := map[string]json.RawMessage{
		"oauthAccount": json.RawMessage(`{"emailAddress":"stored@example.com"}`),
		"userID":       json.RawMessage(`"u-stored"`),
	}
	if err := writeJSONMapStoreFile(artifact.StorePath, initialStore); err != nil {
		t.Fatalf("writeJSONMapStoreFile: %v", err)
	}

	runtime, err := prepareHarnessAuthRuntimeForArtifacts([]harnessAuthArtifact{artifact})
	if err != nil {
		t.Fatalf("prepareHarnessAuthRuntimeForArtifacts: %v", err)
	}

	agentRaw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read materialized Claude state: %v", err)
	}
	if !strings.Contains(string(agentRaw), `"oauthAccount"`) || !strings.Contains(string(agentRaw), `"userID"`) {
		t.Fatalf("materialized Claude state missing auth keys:\n%s", string(agentRaw))
	}

	updatedAgentState := `{
  "oauthAccount": {"emailAddress": "updated@example.com"},
  "userID": "u-updated",
  "projects": {"hazmat": true}
}`
	if err := os.WriteFile(agentPath, []byte(updatedAgentState), 0o600); err != nil {
		t.Fatalf("overwrite agent Claude state: %v", err)
	}

	runtime.Cleanup()

	storeRaw, err := os.ReadFile(artifact.StorePath)
	if err != nil {
		t.Fatalf("read harvested Claude store state: %v", err)
	}
	if !strings.Contains(string(storeRaw), `"updated@example.com"`) || !strings.Contains(string(storeRaw), `"u-updated"`) {
		t.Fatalf("harvested Claude store state missing updated auth:\n%s", string(storeRaw))
	}
	if strings.Contains(string(storeRaw), `"projects"`) {
		t.Fatalf("harvested Claude store state should not contain non-auth keys:\n%s", string(storeRaw))
	}

	remainingAgentRaw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read cleaned Claude agent state: %v", err)
	}
	if !strings.Contains(string(remainingAgentRaw), `"projects"`) {
		t.Fatalf("cleaned Claude agent state missing non-auth keys:\n%s", string(remainingAgentRaw))
	}
	if strings.Contains(string(remainingAgentRaw), `"oauthAccount"`) || strings.Contains(string(remainingAgentRaw), `"userID"`) {
		t.Fatalf("cleaned Claude agent state still contains auth keys:\n%s", string(remainingAgentRaw))
	}
}
