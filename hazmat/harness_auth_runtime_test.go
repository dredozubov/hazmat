package hazmat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if _, err := os.Stat(harnessAuthConflictDir(storePath)); !os.IsNotExist(err) {
		t.Fatalf("normal harvest should not archive the superseded baseline, got err=%v", err)
	}
}

func TestHarnessAuthArtifactsForRuntimeHomeRemapMaterializedPaths(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host")
	runtimeHome := filepath.Join(defaultSessionHomeRoot, "session-123", "home")

	artifacts := harnessAuthArtifactsForRuntimeHome(HarnessCodex, home, runtimeHome)
	if len(artifacts) != 1 {
		t.Fatalf("Codex artifacts = %d, want 1", len(artifacts))
	}
	if got, want := artifacts[0].AgentPath, filepath.Join(runtimeHome, ".codex", "auth.json"); got != want {
		t.Fatalf("AgentPath = %s, want %s", got, want)
	}
	if got, want := artifacts[0].StorePath, filepath.Join(home, ".hazmat", "secrets", "codex", "auth.json"); got != want {
		t.Fatalf("StorePath = %s, want %s", got, want)
	}

	persistent := harnessAuthArtifactsForHome(HarnessCodex, home)
	if len(persistent) != 1 {
		t.Fatalf("persistent Codex artifacts = %d, want 1", len(persistent))
	}
	if got, want := persistent[0].AgentPath, filepath.Join(agentHome, ".codex", "auth.json"); got != want {
		t.Fatalf("persistent AgentPath = %s, want %s", got, want)
	}
}

func TestHarnessAuthArtifactsForRuntimeHomeRemapAllManagedFileHarnesses(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host")
	runtimeHome := filepath.Join(defaultSessionHomeRoot, "session-123", "home")
	want := map[HarnessID][]string{
		HarnessClaude: {
			filepath.Join(runtimeHome, ".claude", ".credentials.json"),
			filepath.Join(runtimeHome, ".claude.json"),
		},
		HarnessCodex: {
			filepath.Join(runtimeHome, ".codex", "auth.json"),
		},
		HarnessOpenCode: {
			filepath.Join(runtimeHome, ".local", "share", "opencode", "auth.json"),
		},
		HarnessGemini: {
			filepath.Join(runtimeHome, ".gemini", "oauth_creds.json"),
			filepath.Join(runtimeHome, ".gemini", "google_accounts.json"),
		},
	}

	for harness, wantPaths := range want {
		t.Run(string(harness), func(t *testing.T) {
			artifacts := harnessAuthArtifactsForRuntimeHome(harness, home, runtimeHome)
			if len(artifacts) != len(wantPaths) {
				t.Fatalf("%s artifacts = %d, want %d", harness, len(artifacts), len(wantPaths))
			}
			for i, wantPath := range wantPaths {
				if artifacts[i].AgentPath != wantPath {
					t.Fatalf("%s artifact %d AgentPath = %s, want %s", harness, i, artifacts[i].AgentPath, wantPath)
				}
				if !strings.HasPrefix(artifacts[i].StorePath, filepath.Join(home, ".hazmat", "secrets")+string(os.PathSeparator)) {
					t.Fatalf("%s artifact %d StorePath = %s, want host secret store under %s", harness, i, artifacts[i].StorePath, home)
				}
			}
		})
	}
}

func TestHarnessAuthArtifactsDeclareRegisteredHostAuthPaths(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host")
	runtimeHome := filepath.Join(t.TempDir(), "runtime-home")
	want := map[HarnessID][]string{
		HarnessCodex: {
			filepath.Join(home, ".codex", "auth.json"),
		},
		HarnessOpenCode: {
			filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		},
		HarnessGemini: {
			filepath.Join(home, ".gemini", "oauth_creds.json"),
			filepath.Join(home, ".gemini", "google_accounts.json"),
		},
	}

	for harness, wantPaths := range want {
		t.Run(string(harness), func(t *testing.T) {
			artifacts := harnessAuthArtifactsForRuntimeHome(harness, home, runtimeHome)
			if len(artifacts) != len(wantPaths) {
				t.Fatalf("%s artifacts = %d, want %d", harness, len(artifacts), len(wantPaths))
			}
			for i, wantPath := range wantPaths {
				if artifacts[i].HostPath != wantPath {
					t.Fatalf("%s artifact %d HostPath = %s, want %s", harness, i, artifacts[i].HostPath, wantPath)
				}
			}
		})
	}
}

func TestPrepareHarnessAuthRuntimeImportsNewerHostFileBackedAuth(t *testing.T) {
	for _, tc := range fileBackedHarnessAuthCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			storeUpdatedAt := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
			hostUpdatedAt := storeUpdatedAt.Add(time.Minute)
			stored := []byte(`{"token":"stored"}`)
			host := []byte(`{"token":"host-rotated"}`)

			if err := writeHostStoredSecretFile(tc.artifact.StorePath, stored); err != nil {
				t.Fatalf("write store: %v", err)
			}
			if err := writeHostStoredSecretFile(tc.artifact.HostPath, host); err != nil {
				t.Fatalf("write host auth: %v", err)
			}
			mustChtimes(t, tc.artifact.StorePath, storeUpdatedAt)
			mustChtimes(t, tc.artifact.HostPath, hostUpdatedAt)

			if _, _, err := materializeHarnessAuthArtifact(tc.artifact); err != nil {
				t.Fatalf("materializeHarnessAuthArtifact: %v", err)
			}

			assertFileBytes(t, tc.artifact.StorePath, host, "host-owned auth store")
			assertFileBytes(t, tc.artifact.AgentPath, host, "agent auth file")
		})
	}
}

func TestPrepareHarnessAuthRuntimePublishesFileBackedRotationToHost(t *testing.T) {
	for _, tc := range fileBackedHarnessAuthCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			stored := []byte(`{"token":"stored"}`)
			rotated := []byte(`{"token":"agent-rotated"}`)

			if err := writeHostStoredSecretFile(tc.artifact.StorePath, stored); err != nil {
				t.Fatalf("write store: %v", err)
			}
			runtime, err := prepareHarnessAuthRuntimeForArtifacts([]harnessAuthArtifact{tc.artifact})
			if err != nil {
				t.Fatalf("prepareHarnessAuthRuntimeForArtifacts: %v", err)
			}
			if err := os.WriteFile(tc.artifact.AgentPath, rotated, 0o600); err != nil {
				t.Fatalf("write rotated agent auth: %v", err)
			}

			runtime.Cleanup()

			assertFileBytes(t, tc.artifact.StorePath, rotated, "host-owned auth store")
			assertFileBytes(t, tc.artifact.HostPath, rotated, "plain host auth file")
			if _, err := os.Stat(tc.artifact.AgentPath); !os.IsNotExist(err) {
				t.Fatalf("agent auth residue should be removed, got err=%v", err)
			}
		})
	}
}

func TestPrepareHarnessAuthRuntimeRejectsEqualTimeHostFileConflict(t *testing.T) {
	tc := fileBackedHarnessAuthCases(t)[0]
	updatedAt := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	stored := []byte(`{"token":"stored-secret-should-not-leak"}`)
	host := []byte(`{"token":"host-secret-should-not-leak"}`)

	if err := writeHostStoredSecretFile(tc.artifact.StorePath, stored); err != nil {
		t.Fatalf("write store: %v", err)
	}
	if err := writeHostStoredSecretFile(tc.artifact.HostPath, host); err != nil {
		t.Fatalf("write host auth: %v", err)
	}
	mustChtimes(t, tc.artifact.StorePath, updatedAt)
	mustChtimes(t, tc.artifact.HostPath, updatedAt)

	_, _, err := materializeHarnessAuthArtifact(tc.artifact)
	if err == nil {
		t.Fatal("materializeHarnessAuthArtifact succeeded, want conflict")
	}
	msg := err.Error()
	for _, leaked := range []string{"stored-secret-should-not-leak", "host-secret-should-not-leak"} {
		if strings.Contains(msg, leaked) {
			t.Fatalf("conflict error leaked secret %q: %s", leaked, msg)
		}
	}
}

func TestPrepareHarnessAuthRuntimePreservesClaudeCredentialsOnLoggedOutRewrite(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	agentPath := filepath.Join(root, "agent", ".claude", ".credentials.json")
	stored := []byte(`{"sessionKey":"stored-token","refreshToken":"stored-refresh"}`)

	artifact := claudeCredentialsHarnessAuthArtifact(home)
	artifact.AgentPath = agentPath
	if err := writeHostStoredSecretFile(artifact.StorePath, stored); err != nil {
		t.Fatalf("writeHostStoredSecretFile: %v", err)
	}

	runtime, err := prepareHarnessAuthRuntimeForArtifacts([]harnessAuthArtifact{artifact})
	if err != nil {
		t.Fatalf("prepareHarnessAuthRuntimeForArtifacts: %v", err)
	}

	if err := os.WriteFile(agentPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("simulate Claude logged-out credential rewrite: %v", err)
	}

	runtime.Cleanup()

	storeRaw, err := os.ReadFile(artifact.StorePath)
	if err != nil {
		t.Fatalf("read preserved Claude credentials: %v", err)
	}
	if string(storeRaw) != string(stored) {
		t.Fatalf("Claude credentials store = %q, want preserved %q", storeRaw, stored)
	}
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("logged-out agent credential residue should be removed, got err=%v", err)
	}
}

func TestPrepareHarnessAuthRuntimeImportsNewerHostClaudeKeychain(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store", "claude", "credentials.json")
	agentPath := filepath.Join(root, "agent", ".claude", ".credentials.json")
	storeUpdatedAt := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	hostUpdatedAt := storeUpdatedAt.Add(time.Minute)
	stored := []byte(`{"refreshToken":"stored-refresh"}`)
	host := []byte(`{"refreshToken":"host-rotated-refresh"}`)

	if err := writeHostStoredSecretFile(storePath, stored); err != nil {
		t.Fatalf("write host store: %v", err)
	}
	mustChtimes(t, storePath, storeUpdatedAt)

	artifact := testClaudeKeychainSyncArtifact(storePath, agentPath, host, hostUpdatedAt)
	if _, _, err := materializeHarnessAuthArtifact(artifact); err != nil {
		t.Fatalf("materializeHarnessAuthArtifact: %v", err)
	}

	assertFileBytes(t, storePath, host, "host-owned Claude credentials")
	assertFileBytes(t, agentPath, host, "agent Claude credentials")
}

func TestPrepareHarnessAuthRuntimePublishesNewerStoreToHostClaudeKeychain(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store", "claude", "credentials.json")
	agentPath := filepath.Join(root, "agent", ".claude", ".credentials.json")
	hostUpdatedAt := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	storeUpdatedAt := hostUpdatedAt.Add(time.Minute)
	stored := []byte(`{"refreshToken":"store-rotated-refresh"}`)
	host := []byte(`{"refreshToken":"old-host-refresh"}`)

	if err := writeHostStoredSecretFile(storePath, stored); err != nil {
		t.Fatalf("write host store: %v", err)
	}
	mustChtimes(t, storePath, storeUpdatedAt)

	artifact := testClaudeKeychainSyncArtifact(storePath, agentPath, host, hostUpdatedAt)
	var hostWrites [][]byte
	artifact.WriteHostKeychain = func(data harnessAuthData) error {
		raw, _ := data.([]byte)
		hostWrites = append(hostWrites, append([]byte(nil), raw...))
		return nil
	}
	if _, _, err := materializeHarnessAuthArtifact(artifact); err != nil {
		t.Fatalf("materializeHarnessAuthArtifact: %v", err)
	}

	assertLastHostKeychainWrite(t, hostWrites, stored)
	assertFileBytes(t, agentPath, stored, "agent Claude credentials")
}

func TestHarvestHarnessAuthArtifactPublishesAgentKeychainRotationToHost(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store", "claude", "credentials.json")
	agentPath := filepath.Join(root, "agent", ".claude", ".credentials.json")
	updatedAt := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	stored := []byte(`{"refreshToken":"stored-refresh"}`)
	rotated := []byte(`{"refreshToken":"agent-rotated-refresh"}`)

	if err := writeHostStoredSecretFile(storePath, stored); err != nil {
		t.Fatalf("write host store: %v", err)
	}
	mustChtimes(t, storePath, updatedAt)

	artifact := testClaudeKeychainSyncArtifact(storePath, agentPath, stored, updatedAt)
	var agentKeychain []byte
	agentCleared := false
	artifact.ReadAgentKeychain = func() (harnessAuthData, bool, error) {
		if agentKeychain == nil {
			return nil, false, nil
		}
		return agentKeychain, true, nil
	}
	artifact.ClearAgentKeychain = func() error {
		agentCleared = true
		agentKeychain = nil
		return nil
	}
	var hostWrites [][]byte
	artifact.WriteHostKeychain = func(data harnessAuthData) error {
		raw, _ := data.([]byte)
		hostWrites = append(hostWrites, append([]byte(nil), raw...))
		return nil
	}

	runtime, err := prepareHarnessAuthRuntimeForArtifacts([]harnessAuthArtifact{artifact})
	if err != nil {
		t.Fatalf("prepareHarnessAuthRuntimeForArtifacts: %v", err)
	}
	agentKeychain = rotated
	if err := os.WriteFile(agentPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write logged-out agent file: %v", err)
	}

	runtime.Cleanup()

	assertFileBytes(t, storePath, rotated, "host-owned Claude credentials")
	assertLastHostKeychainWrite(t, hostWrites, rotated)
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("agent credential residue should be removed, got err=%v", err)
	}
	if !agentCleared {
		t.Fatal("agent keychain residue was not cleared")
	}
}

func TestPrepareHarnessAuthRuntimeRejectsEqualTimeHostKeychainConflict(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store", "claude", "credentials.json")
	agentPath := filepath.Join(root, "agent", ".claude", ".credentials.json")
	updatedAt := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	stored := []byte(`{"refreshToken":"stored-secret-should-not-leak"}`)
	host := []byte(`{"refreshToken":"host-secret-should-not-leak"}`)

	if err := writeHostStoredSecretFile(storePath, stored); err != nil {
		t.Fatalf("write host store: %v", err)
	}
	mustChtimes(t, storePath, updatedAt)

	artifact := testClaudeKeychainSyncArtifact(storePath, agentPath, host, updatedAt)
	_, _, err := materializeHarnessAuthArtifact(artifact)
	if err == nil {
		t.Fatal("materializeHarnessAuthArtifact succeeded, want conflict")
	}
	msg := err.Error()
	for _, leaked := range []string{"stored-secret-should-not-leak", "host-secret-should-not-leak"} {
		if strings.Contains(msg, leaked) {
			t.Fatalf("conflict error leaked secret %q: %s", leaked, msg)
		}
	}
}

func TestMigrateHarnessAuthArtifactsDropsNonHarvestableClaudeCredentials(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	agentPath := filepath.Join(root, "agent", ".claude", ".credentials.json")
	stored := []byte(`{"sessionKey":"stored-token"}`)

	artifact := claudeCredentialsHarnessAuthArtifact(home)
	artifact.AgentPath = agentPath
	if err := writeHostStoredSecretFile(artifact.StorePath, stored); err != nil {
		t.Fatalf("writeHostStoredSecretFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o700); err != nil {
		t.Fatalf("mkdir agent credentials dir: %v", err)
	}
	if err := os.WriteFile(agentPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write logged-out agent credentials: %v", err)
	}

	var notes []string
	if err := migrateHarnessAuthArtifacts([]harnessAuthArtifact{artifact}, func(note string) {
		notes = append(notes, note)
	}); err != nil {
		t.Fatalf("migrateHarnessAuthArtifacts: %v", err)
	}

	storeRaw, err := os.ReadFile(artifact.StorePath)
	if err != nil {
		t.Fatalf("read preserved Claude credentials: %v", err)
	}
	if string(storeRaw) != string(stored) {
		t.Fatalf("Claude credentials store = %q, want preserved %q", storeRaw, stored)
	}
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("logged-out legacy credential residue should be removed, got err=%v", err)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "Ignored non-harvestable legacy Claude credential file") {
		t.Fatalf("migration notes = %v, want non-harvestable note", notes)
	}
}

func testClaudeKeychainSyncArtifact(storePath, agentPath string, host []byte, hostUpdatedAt time.Time) harnessAuthArtifact {
	artifact := rawHarnessAuthArtifact("Claude credential file", storePath, agentPath)
	artifact.Harvestable = isHarvestableClaudeCredentialData
	artifact.ReadHostKeychain = func() (harnessAuthKeychainData, bool, error) {
		return harnessAuthKeychainData{Data: host, UpdatedAt: hostUpdatedAt}, true, nil
	}
	artifact.WriteHostKeychain = func(harnessAuthData) error { return nil }
	return artifact
}

type fileBackedHarnessAuthCase struct {
	name     string
	artifact harnessAuthArtifact
}

func fileBackedHarnessAuthCases(t *testing.T) []fileBackedHarnessAuthCase {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "host")
	runtimeHome := filepath.Join(root, "runtime-home")
	var cases []fileBackedHarnessAuthCase
	for _, harness := range []HarnessID{HarnessCodex, HarnessOpenCode, HarnessGemini} {
		for _, artifact := range harnessAuthArtifactsForRuntimeHome(harness, home, runtimeHome) {
			if strings.TrimSpace(artifact.HostPath) == "" {
				t.Fatalf("%s artifact %s missing host auth path", harness, artifact.Name)
			}
			cases = append(cases, fileBackedHarnessAuthCase{
				name:     string(harness) + "/" + artifact.Name,
				artifact: artifact,
			})
		}
	}
	return cases
}

func mustChtimes(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte, label string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s at %s: %v", label, path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s = %q, want %q", label, got, want)
	}
}

func assertLastHostKeychainWrite(t *testing.T, writes [][]byte, want []byte) {
	t.Helper()
	if len(writes) == 0 {
		t.Fatal("host Keychain was not written")
	}
	if got := writes[len(writes)-1]; string(got) != string(want) {
		t.Fatalf("host Keychain write = %q, want %q", got, want)
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

func TestMigrateHarnessAuthArtifactsArchivesDivergentHostBeforePromotingAgent(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store", "codex", "auth.json")
	agentPath := filepath.Join(root, "agent", ".codex", "auth.json")
	hostAuth := []byte(`{"token":"host"}`)
	agentAuth := []byte(`{"token":"agent"}`)

	if err := writeHostStoredSecretFile(storePath, hostAuth); err != nil {
		t.Fatalf("write host auth: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o700); err != nil {
		t.Fatalf("mkdir agent auth dir: %v", err)
	}
	if err := os.WriteFile(agentPath, agentAuth, 0o600); err != nil {
		t.Fatalf("write agent auth: %v", err)
	}

	restoreClock := harnessAuthConflictNow
	harnessAuthConflictNow = func() time.Time {
		return time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() {
		harnessAuthConflictNow = restoreClock
	})

	var notes []string
	if err := migrateHarnessAuthArtifacts([]harnessAuthArtifact{
		rawHarnessAuthArtifact("Codex auth file", storePath, agentPath),
	}, func(note string) {
		notes = append(notes, note)
	}); err != nil {
		t.Fatalf("migrateHarnessAuthArtifacts: %v", err)
	}

	storeRaw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read recovered store auth: %v", err)
	}
	if string(storeRaw) != string(agentAuth) {
		t.Fatalf("recovered store auth = %q, want %q", storeRaw, agentAuth)
	}
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("agent auth should be removed after recovery, got err=%v", err)
	}

	conflictPath := filepath.Join(harnessAuthConflictDir(storePath), "20260428T120000.000000000Z")
	conflictRaw, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatalf("read archived host auth: %v", err)
	}
	if string(conflictRaw) != string(hostAuth) {
		t.Fatalf("archived host auth = %q, want %q", conflictRaw, hostAuth)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "Recovered divergent Codex auth file") {
		t.Fatalf("migration notes = %v, want divergent recovery note", notes)
	}
}

func TestHarvestHarnessAuthArtifactArchivesDivergentStoreBeforeOverwrite(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store", "opencode", "auth.json")
	agentPath := filepath.Join(root, "agent", ".local", "share", "opencode", "auth.json")
	baselineAuth := []byte(`{"token":"baseline"}`)
	hostAuth := []byte(`{"token":"host"}`)
	agentAuth := []byte(`{"token":"agent"}`)

	if err := writeHostStoredSecretFile(storePath, hostAuth); err != nil {
		t.Fatalf("write host auth: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o700); err != nil {
		t.Fatalf("mkdir agent auth dir: %v", err)
	}
	if err := os.WriteFile(agentPath, agentAuth, 0o600); err != nil {
		t.Fatalf("write agent auth: %v", err)
	}

	restoreClock := harnessAuthConflictNow
	harnessAuthConflictNow = func() time.Time {
		return time.Date(2026, 4, 28, 12, 1, 0, 0, time.UTC)
	}
	t.Cleanup(func() {
		harnessAuthConflictNow = restoreClock
	})

	if err := harvestHarnessAuthArtifact(rawHarnessAuthArtifact("OpenCode auth file", storePath, agentPath), baselineAuth, true); err != nil {
		t.Fatalf("harvestHarnessAuthArtifact: %v", err)
	}

	storeRaw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read harvested store auth: %v", err)
	}
	if string(storeRaw) != string(agentAuth) {
		t.Fatalf("harvested store auth = %q, want %q", storeRaw, agentAuth)
	}
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Fatalf("agent auth should be removed after harvest, got err=%v", err)
	}

	conflictPath := filepath.Join(harnessAuthConflictDir(storePath), "20260428T120100.000000000Z")
	conflictRaw, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatalf("read archived host auth: %v", err)
	}
	if string(conflictRaw) != string(hostAuth) {
		t.Fatalf("archived host auth = %q, want %q", conflictRaw, hostAuth)
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
