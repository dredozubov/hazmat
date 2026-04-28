package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type harnessAuthData any

type harnessAuthArtifact struct {
	Name        string
	StorePath   string
	AgentPath   string
	ReadStore   func(string) (harnessAuthData, bool, error)
	ReadAgent   func(string) (harnessAuthData, bool, error)
	WriteStore  func(string, harnessAuthData) error
	WriteAgent  func(string, harnessAuthData) error
	RemoveAgent func(string) error
	Equal       func(harnessAuthData, harnessAuthData) bool
}

var harnessAuthConflictNow = time.Now

func harnessAuthArtifactsForHome(id HarnessID, home string) []harnessAuthArtifact {
	switch id {
	case HarnessClaude:
		return []harnessAuthArtifact{
			rawHarnessAuthArtifact("Claude credential file",
				claudeCredentialStorePathForHome(home),
				agentHome+"/.claude/.credentials.json",
			),
			claudeStateHarnessAuthArtifact(home),
		}
	case HarnessCodex:
		return []harnessAuthArtifact{
			rawHarnessAuthArtifact("Codex auth file",
				codexAuthStorePathForHome(home),
				agentHome+"/.codex/auth.json",
			),
		}
	case HarnessOpenCode:
		return []harnessAuthArtifact{
			rawHarnessAuthArtifact("OpenCode auth file",
				openCodeAuthStorePathForHome(home),
				agentHome+"/.local/share/opencode/auth.json",
			),
		}
	case HarnessGemini:
		return []harnessAuthArtifact{
			rawHarnessAuthArtifact("Gemini OAuth credentials",
				geminiOAuthStorePathForHome(home),
				agentHome+"/.gemini/oauth_creds.json",
			),
			rawHarnessAuthArtifact("Gemini account index",
				geminiAccountsStorePathForHome(home),
				agentHome+"/.gemini/google_accounts.json",
			),
		}
	default:
		return nil
	}
}

func harnessAuthConflictDir(storePath string) string {
	return filepath.Join(filepath.Dir(storePath), filepath.Base(storePath)+".conflicts")
}

func nextHarnessAuthConflictPath(storePath string) string {
	stamp := harnessAuthConflictNow().UTC().Format("20060102T150405.000000000Z")
	dir := harnessAuthConflictDir(storePath)
	for i := 0; ; i++ {
		name := stamp
		if i > 0 {
			name = fmt.Sprintf("%s-%d", stamp, i)
		}
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			return path
		}
	}
}

func preserveHarnessAuthConflict(artifact harnessAuthArtifact, data harnessAuthData) (string, error) {
	path := nextHarnessAuthConflictPath(artifact.StorePath)
	if err := artifact.WriteStore(path, data); err != nil {
		return "", err
	}
	return path, nil
}

func rawHarnessAuthArtifact(name, storePath, agentPath string) harnessAuthArtifact {
	return harnessAuthArtifact{
		Name:      name,
		StorePath: storePath,
		AgentPath: agentPath,
		ReadStore: func(path string) (harnessAuthData, bool, error) {
			raw, ok, err := readHostStoredSecretFile(path)
			if !ok || err != nil {
				return nil, ok, err
			}
			return raw, true, nil
		},
		ReadAgent: func(path string) (harnessAuthData, bool, error) {
			raw, ok, err := readAgentSecretFile(path)
			if !ok || err != nil {
				return nil, ok, err
			}
			return raw, true, nil
		},
		WriteStore: func(path string, data harnessAuthData) error {
			raw, _ := data.([]byte)
			return writeHostStoredSecretFile(path, raw)
		},
		WriteAgent: func(path string, data harnessAuthData) error {
			raw, _ := data.([]byte)
			return writeAgentSecretFile(path, raw, 0o600)
		},
		RemoveAgent: removeAgentSecretFile,
		Equal: func(a, b harnessAuthData) bool {
			left, _ := a.([]byte)
			right, _ := b.([]byte)
			return bytes.Equal(left, right)
		},
	}
}

func claudeStateHarnessAuthArtifact(home string) harnessAuthArtifact {
	return harnessAuthArtifact{
		Name:      "Claude account state",
		StorePath: claudeStateStorePathForHome(home),
		AgentPath: agentHome + "/.claude.json",
		ReadStore: func(path string) (harnessAuthData, bool, error) {
			payload, ok, err := readJSONMapStoreFile(path)
			if !ok || err != nil {
				return nil, ok, err
			}
			return payload, true, nil
		},
		ReadAgent: func(path string) (harnessAuthData, bool, error) {
			payload, ok, err := readClaudeStateKeysFromAgent(path)
			if !ok || err != nil {
				return nil, ok, err
			}
			return payload, true, nil
		},
		WriteStore: func(path string, data harnessAuthData) error {
			payload, _ := data.(map[string]json.RawMessage)
			return writeJSONMapStoreFile(path, payload)
		},
		WriteAgent: func(path string, data harnessAuthData) error {
			payload, _ := data.(map[string]json.RawMessage)
			return writeClaudeStateKeysToAgent(path, payload)
		},
		RemoveAgent: removeClaudeStateKeysFromAgent,
		Equal: func(a, b harnessAuthData) bool {
			left, _ := a.(map[string]json.RawMessage)
			right, _ := b.(map[string]json.RawMessage)
			return jsonSubsetEqual(left, right)
		},
	}
}

func applyHarnessAuthArtifacts(cfg *sessionConfig) error {
	if cfg.HarnessID == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determine home directory for harness auth: %w", err)
	}
	return migrateHarnessAuthArtifacts(harnessAuthArtifactsForHome(cfg.HarnessID, home), func(note string) {
		cfg.SessionNotes = append(cfg.SessionNotes, note)
	})
}

func migrateHarnessAuthArtifacts(artifacts []harnessAuthArtifact, addNote func(string)) error {
	for _, artifact := range artifacts {
		if err := migrateHarnessAuthArtifact(artifact, addNote); err != nil {
			return err
		}
	}
	return nil
}

func migrateHarnessAuthArtifact(artifact harnessAuthArtifact, addNote func(string)) error {
	stored, storedExists, err := artifact.ReadStore(artifact.StorePath)
	if err != nil {
		return fmt.Errorf("read host-owned %s: %w", artifact.Name, err)
	}
	legacy, legacyExists, err := artifact.ReadAgent(artifact.AgentPath)
	if err != nil {
		return fmt.Errorf("read legacy %s from %s: %w", artifact.Name, artifact.AgentPath, err)
	}

	switch {
	case !storedExists && !legacyExists:
		return nil
	case !storedExists && legacyExists:
		if err := artifact.WriteStore(artifact.StorePath, legacy); err != nil {
			addNote(fmt.Sprintf("Using legacy %s from %s because migration into ~/.hazmat/secrets failed: %v", artifact.Name, artifact.AgentPath, err))
			return nil
		}
		if err := artifact.RemoveAgent(artifact.AgentPath); err != nil {
			addNote(fmt.Sprintf("Migrated legacy %s into ~/.hazmat/secrets, but could not remove the old copy from %s: %v", artifact.Name, artifact.AgentPath, err))
		} else {
			addNote(fmt.Sprintf("Migrated legacy %s from %s into ~/.hazmat/secrets.", artifact.Name, artifact.AgentPath))
		}
	case storedExists && legacyExists:
		if artifact.Equal(stored, legacy) {
			if err := artifact.RemoveAgent(artifact.AgentPath); err != nil {
				addNote(fmt.Sprintf("Host-owned %s already matches %s, but Hazmat could not remove the legacy copy: %v", artifact.Name, artifact.AgentPath, err))
			} else {
				addNote(fmt.Sprintf("Removed legacy %s from %s because the host-owned copy in ~/.hazmat/secrets already matches it.", artifact.Name, artifact.AgentPath))
			}
		} else {
			conflictPath, err := preserveHarnessAuthConflict(artifact, stored)
			if err != nil {
				addNote(fmt.Sprintf("Host-owned %s differs from the legacy copy in %s, but Hazmat could not archive the host-owned copy before recovery: %v", artifact.Name, artifact.AgentPath, err))
				return nil
			}
			if err := artifact.WriteStore(artifact.StorePath, legacy); err != nil {
				addNote(fmt.Sprintf("Archived the previous host-owned %s at %s, but could not promote the legacy copy from %s into ~/.hazmat/secrets: %v", artifact.Name, conflictPath, artifact.AgentPath, err))
				return nil
			}
			if err := artifact.RemoveAgent(artifact.AgentPath); err != nil {
				addNote(fmt.Sprintf("Recovered divergent %s from %s into ~/.hazmat/secrets and preserved the previous host-owned copy at %s, but could not remove the old runtime copy: %v", artifact.Name, artifact.AgentPath, conflictPath, err))
			} else {
				addNote(fmt.Sprintf("Recovered divergent %s from %s into ~/.hazmat/secrets; previous host-owned copy preserved at %s.", artifact.Name, artifact.AgentPath, conflictPath))
			}
		}
	}

	return nil
}

func prepareHarnessAuthRuntime(cfg sessionConfig) (preparedSessionRuntime, error) {
	if cfg.HarnessID == "" {
		return preparedSessionRuntime{Cleanup: func() {}}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return preparedSessionRuntime{}, fmt.Errorf("determine home directory for harness auth: %w", err)
	}
	return prepareHarnessAuthRuntimeForArtifacts(harnessAuthArtifactsForHome(cfg.HarnessID, home))
}

func prepareHarnessAuthRuntimeForArtifacts(artifacts []harnessAuthArtifact) (preparedSessionRuntime, error) {
	runtime := preparedSessionRuntime{Cleanup: func() {}}
	if len(artifacts) == 0 {
		return runtime, nil
	}

	var cleanups []func()
	for _, artifact := range artifacts {
		baseline, baselineExists, err := materializeHarnessAuthArtifact(artifact)
		if err != nil {
			return preparedSessionRuntime{}, fmt.Errorf("prepare %s: %w", artifact.Name, err)
		}
		artifactForCleanup := artifact
		baselineForCleanup := baseline
		baselineExistsForCleanup := baselineExists
		cleanups = append(cleanups, func() {
			if err := harvestHarnessAuthArtifact(artifactForCleanup, baselineForCleanup, baselineExistsForCleanup); err != nil {
				fmt.Fprintf(os.Stderr, "hazmat: warning: could not harvest %s into ~/.hazmat/secrets: %v\n", artifactForCleanup.Name, err)
			}
		})
	}

	runtime.Cleanup = func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	return runtime, nil
}

func materializeHarnessAuthArtifact(artifact harnessAuthArtifact) (harnessAuthData, bool, error) {
	stored, storedExists, err := artifact.ReadStore(artifact.StorePath)
	if err != nil {
		return nil, false, err
	}

	if _, ok, err := artifact.ReadAgent(artifact.AgentPath); err != nil {
		return nil, false, err
	} else if ok {
		return stored, storedExists, nil
	}

	if !storedExists {
		return nil, false, nil
	}
	return stored, true, artifact.WriteAgent(artifact.AgentPath, stored)
}

func harvestHarnessAuthArtifact(artifact harnessAuthArtifact, baseline harnessAuthData, baselineExists bool) error {
	data, ok, err := artifact.ReadAgent(artifact.AgentPath)
	if err != nil || !ok {
		return err
	}
	stored, storedExists, err := artifact.ReadStore(artifact.StorePath)
	if err != nil {
		return err
	}
	if storedExists && !artifact.Equal(stored, data) {
		hostChangedSinceMaterialize := !baselineExists || !artifact.Equal(stored, baseline)
		if hostChangedSinceMaterialize {
			if _, err := preserveHarnessAuthConflict(artifact, stored); err != nil {
				return fmt.Errorf("archive existing host-owned copy before harvest: %w", err)
			}
		}
	}
	if err := artifact.WriteStore(artifact.StorePath, data); err != nil {
		return err
	}
	return artifact.RemoveAgent(artifact.AgentPath)
}
