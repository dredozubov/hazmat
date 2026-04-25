package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
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
			addNote(fmt.Sprintf("Host-owned %s differs from the legacy copy in %s; Hazmat will keep using the legacy file for this session and harvest it back into ~/.hazmat/secrets on exit.", artifact.Name, artifact.AgentPath))
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
		if err := materializeHarnessAuthArtifact(artifact); err != nil {
			return preparedSessionRuntime{}, fmt.Errorf("prepare %s: %w", artifact.Name, err)
		}
		artifact := artifact
		cleanups = append(cleanups, func() {
			if err := harvestHarnessAuthArtifact(artifact); err != nil {
				fmt.Fprintf(os.Stderr, "hazmat: warning: could not harvest %s into ~/.hazmat/secrets: %v\n", artifact.Name, err)
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

func materializeHarnessAuthArtifact(artifact harnessAuthArtifact) error {
	if _, ok, err := artifact.ReadAgent(artifact.AgentPath); err != nil {
		return err
	} else if ok {
		return nil
	}

	stored, ok, err := artifact.ReadStore(artifact.StorePath)
	if err != nil || !ok {
		return err
	}
	return artifact.WriteAgent(artifact.AgentPath, stored)
}

func harvestHarnessAuthArtifact(artifact harnessAuthArtifact) error {
	data, ok, err := artifact.ReadAgent(artifact.AgentPath)
	if err != nil || !ok {
		return err
	}
	if err := artifact.WriteStore(artifact.StorePath, data); err != nil {
		return err
	}
	return artifact.RemoveAgent(artifact.AgentPath)
}
