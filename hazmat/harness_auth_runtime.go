package hazmat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hazmat/credentials"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type harnessAuthData any

type harnessAuthKeychainData struct {
	Data      harnessAuthData
	UpdatedAt time.Time
}

type harnessAuthArtifact struct {
	CredentialID credentials.ID
	Name         string
	StorePath    string
	HostPath     string
	AgentPath    string
	ReadStore    func(string) (harnessAuthData, bool, error)
	ReadAgent    func(string) (harnessAuthData, bool, error)
	WriteStore   func(string, harnessAuthData) error
	WriteAgent   func(string, harnessAuthData) error
	RemoveAgent  func(string) error
	Equal        func(harnessAuthData, harnessAuthData) bool
	Harvestable  func(harnessAuthData) bool

	// ReadAgentKeychain reads a credential that the harness rotated into a
	// second runtime sink (the agent login keychain) rather than the
	// materialized file. ClearAgentKeychain removes that residue. Both are
	// optional and set only for keychain-backed harnesses (Claude on macOS).
	// See MC_SecretStoreRecovery: a keychain-backed OAuth refresh empties the
	// file copy, so harvest/recovery must promote the keychain value or the
	// host store strands a server-invalidated token and the next session is
	// logged out.
	ReadAgentKeychain   func() (harnessAuthData, bool, error)
	WriteAgentKeychain  func(harnessAuthData) error
	ClearAgentKeychain  func() error
	PreferAgentKeychain bool

	// ReadHostKeychain/WriteHostKeychain bridge a host-user Keychain item into
	// the host-owned store. They are intentionally separate from the agent
	// keychain hooks: plain host Claude and Hazmat Claude run as different
	// users and rotate different login keychains.
	ReadHostKeychain  func() (harnessAuthKeychainData, bool, error)
	WriteHostKeychain func(harnessAuthData) error
}

var harnessAuthConflictNow = time.Now

func harnessAuthArtifactsForHome(id HarnessID, home string) []harnessAuthArtifact {
	return harnessAuthArtifactsForRuntimeHome(id, home, agentHome)
}

func harnessAuthArtifactsForRuntimeHome(id HarnessID, home, runtimeHome string) []harnessAuthArtifact {
	switch id {
	case HarnessClaude:
		return []harnessAuthArtifact{
			claudeCredentialsHarnessAuthArtifactForRuntimeHome(home, runtimeHome),
			claudeStateHarnessAuthArtifactForRuntimeHome(home, runtimeHome),
		}
	case HarnessCodex:
		return []harnessAuthArtifact{
			rawHarnessAuthArtifactForCredentialRuntimeHome(home, credentials.HarnessCodexAuth, runtimeHome),
		}
	case HarnessOpenCode:
		return []harnessAuthArtifact{
			rawHarnessAuthArtifactForCredentialRuntimeHome(home, credentials.HarnessOpenCodeAuth, runtimeHome),
		}
	default:
		return nil
	}
}

func (a harnessAuthArtifact) isHarvestable(data harnessAuthData) bool {
	return a.Harvestable == nil || a.Harvestable(data)
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

func rawHarnessAuthArtifactForCredentialRuntimeHome(home string, id credentials.ID, runtimeHome string) harnessAuthArtifact {
	descriptor := mustCredentialDescriptor(id)
	storePath := mustCredentialStorePathForHome(home, id)
	agentPath, err := agentMaterializationPathForRuntimeHome(descriptor, runtimeHome)
	if err != nil {
		panic(err)
	}
	artifact := rawHarnessAuthArtifact(descriptor.DisplayName, storePath, agentPath)
	artifact.CredentialID = id
	artifact.HostPath = hostFileBackedAuthPath(home, id)
	return artifact
}

func claudeCredentialsHarnessAuthArtifact(home string) harnessAuthArtifact {
	return claudeCredentialsHarnessAuthArtifactForRuntimeHome(home, agentHome)
}

func claudeCredentialsHarnessAuthArtifactForRuntimeHome(home, runtimeHome string) harnessAuthArtifact {
	artifact := rawHarnessAuthArtifactForCredentialRuntimeHome(home, credentials.HarnessClaudeCredentials, runtimeHome)
	// Claude updates can rewrite the runtime credential file to an empty
	// logged-out object. Do not promote that shape over the host-owned store.
	artifact.Harvestable = isHarvestableClaudeCredentialData
	// On keychain-preferring releases the rotated token lands in the agent
	// login keychain instead of the file; teach harvest/recovery to capture it.
	return withClaudeKeychainHarvest(artifact)
}

func isHarvestableClaudeCredentialData(data harnessAuthData) bool {
	raw, _ := data.([]byte)
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return true
	}
	return jsonValueHasMeaningfulCredential(decoded)
}

func jsonValueHasMeaningfulCredential(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case bool:
		return v
	case float64:
		return v != 0
	case []any:
		for _, item := range v {
			if jsonValueHasMeaningfulCredential(item) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, item := range v {
			if jsonValueHasMeaningfulCredential(item) {
				return true
			}
		}
		return false
	default:
		return true
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
	return claudeStateHarnessAuthArtifactForRuntimeHome(home, agentHome)
}

func claudeStateHarnessAuthArtifactForRuntimeHome(home, runtimeHome string) harnessAuthArtifact {
	descriptor := mustCredentialDescriptor(credentials.HarnessClaudeState)
	agentPath, err := agentMaterializationPathForRuntimeHome(descriptor, runtimeHome)
	if err != nil {
		panic(err)
	}
	storePath := mustCredentialStorePathForHome(home, credentials.HarnessClaudeState)
	hostStatePath := filepath.Join(home, ".claude.json")
	return harnessAuthArtifact{
		CredentialID: credentials.HarnessClaudeState,
		Name:         descriptor.DisplayName,
		StorePath:    storePath,
		AgentPath:    agentPath,
		ReadStore: func(path string) (harnessAuthData, bool, error) {
			payload, ok, err := readJSONMapStoreFile(path)
			if err != nil {
				return nil, false, err
			}
			if path == storePath {
				hostState, hostOK, hostErr := readClaudePortableStateFromHost(hostStatePath)
				if hostErr != nil {
					return nil, false, hostErr
				}
				if hostOK {
					payload = mergeClaudeStateMaps(payload, hostState)
					ok = true
				}
			}
			if !ok {
				return nil, false, nil
			}
			return payload, true, nil
		},
		ReadAgent: func(path string) (harnessAuthData, bool, error) {
			payload, ok, err := readClaudeStateKeysFromAgent(path)
			if !ok || err != nil {
				return nil, ok, err
			}
			stored, storedOK, storeErr := readJSONMapStoreFile(storePath)
			if storeErr != nil {
				return nil, false, storeErr
			}
			if storedOK {
				payload = mergeClaudeStateMaps(stored, payload)
			}
			hostState, hostOK, hostErr := readClaudePortableStateFromHost(hostStatePath)
			if hostErr != nil {
				return nil, false, hostErr
			}
			if hostOK {
				payload = mergeClaudeStateMaps(hostState, payload)
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

func readClaudePortableStateFromHost(path string) (map[string]json.RawMessage, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	payload, err := selectClaudePortableStateKeys(raw)
	if err != nil {
		return nil, false, err
	}
	if len(payload) == 0 {
		return nil, false, nil
	}
	return payload, true, nil
}

func mergeClaudeStateMaps(base, overlay map[string]json.RawMessage) map[string]json.RawMessage {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	merged := make(map[string]json.RawMessage, len(base)+len(overlay))
	for key, value := range base {
		merged[key] = slices.Clone(value)
	}
	for key, value := range overlay {
		merged[key] = slices.Clone(value)
	}
	return merged
}

func agentMaterializationPathForRuntimeHome(descriptor credentials.Descriptor, runtimeHome string) (string, error) {
	agentPath, err := descriptor.AgentMaterializationPath()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(runtimeHome) == "" {
		return agentPath, nil
	}
	runtimeHome = filepath.Clean(runtimeHome)
	if runtimeHome == filepath.Clean(agentHome) {
		return agentPath, nil
	}
	if !filepath.IsAbs(runtimeHome) {
		return "", fmt.Errorf("%s runtime home %q must be absolute", descriptor.ID, runtimeHome)
	}
	rel, err := filepath.Rel(filepath.Clean(agentHome), filepath.Clean(agentPath))
	if err != nil {
		return "", fmt.Errorf("%s compute runtime materialization path: %w", descriptor.ID, err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s materialization path %s is outside %s", descriptor.ID, agentPath, agentHome)
	}
	return filepath.Join(runtimeHome, rel), nil
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

// reconcileKeychainResidueIntoAgentFile folds a keychain-backed credential into
// the materialized file copy so the file-based reconciliation in harvest and
// recovery can promote it uniformly. The file copy is authoritative when it
// holds a harvestable value (MC_SecretStoreRecovery: AgentEffective prefers the
// file); otherwise a keychain-backed OAuth refresh may have rotated the live
// token into the agent login keychain while emptying the file. In either case
// the keychain residue is cleared so the file and keychain are never both live
// (AgentKeychainNeverBothLive). Best-effort: a keychain that cannot be read
// without interaction leaves the file untouched and degrades to file-only
// behavior.
func reconcileKeychainResidueIntoAgentFile(artifact harnessAuthArtifact) {
	if artifact.ReadAgentKeychain == nil {
		return
	}
	fileData, fileExists, err := artifact.ReadAgent(artifact.AgentPath)
	if err == nil && fileExists && artifact.isHarvestable(fileData) {
		// The file copy wins; drop any stale keychain residue.
		clearAgentAuthKeychain(artifact)
		return
	}
	data, ok, err := artifact.ReadAgentKeychain()
	if err != nil || !ok || !artifact.isHarvestable(data) {
		return
	}
	if err := artifact.WriteAgent(artifact.AgentPath, data); err != nil {
		// Leave the keychain intact so the next launch can retry recovery.
		return
	}
	clearAgentAuthKeychain(artifact)
}

func clearAgentAuthKeychain(artifact harnessAuthArtifact) {
	if artifact.ClearAgentKeychain == nil {
		return
	}
	_ = artifact.ClearAgentKeychain()
}

func migrateHarnessAuthArtifact(artifact harnessAuthArtifact, addNote func(string)) error {
	reconcileKeychainResidueIntoAgentFile(artifact)
	stored, storedExists, err := artifact.ReadStore(artifact.StorePath)
	if err != nil {
		return fmt.Errorf("read host-owned %s: %w", artifact.Name, err)
	}
	legacy, legacyExists, err := artifact.ReadAgent(artifact.AgentPath)
	if err != nil {
		return fmt.Errorf("read legacy %s from %s: %w", artifact.Name, artifact.AgentPath, err)
	}
	if legacyExists && !artifact.isHarvestable(legacy) {
		if err := artifact.RemoveAgent(artifact.AgentPath); err != nil {
			addNote(fmt.Sprintf("Ignored non-harvestable legacy %s at %s to preserve the host-owned copy, but could not remove the runtime residue: %v", artifact.Name, artifact.AgentPath, err))
		} else {
			addNote(fmt.Sprintf("Ignored non-harvestable legacy %s at %s to preserve the host-owned copy.", artifact.Name, artifact.AgentPath))
		}
		legacy = nil
		legacyExists = false
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
	runtimeHome := agentHome
	if cfg.SessionHome != nil {
		runtimeHome = cfg.SessionHome.Launch.Layout.Home
	}
	artifacts := harnessAuthArtifactsForRuntimeHome(cfg.HarnessID, home, runtimeHome)
	// Only Claude harvests/round-trips its OAuth through the agent login keychain.
	// Antigravity also gets AgentLoginKeychainAccess, but it is a non-syncable
	// external reference (no harvest adapter), so keychain delivery preference
	// stays Claude-scoped.
	if cfg.AgentLoginKeychainAccess && cfg.HarnessID == HarnessClaude {
		artifacts = preferClaudeAgentKeychainDelivery(artifacts)
	}
	return prepareHarnessAuthRuntimeForArtifacts(artifacts)
}

func preferClaudeAgentKeychainDelivery(artifacts []harnessAuthArtifact) []harnessAuthArtifact {
	for i := range artifacts {
		if artifacts[i].CredentialID == credentials.HarnessClaudeCredentials && artifacts[i].WriteAgentKeychain != nil {
			artifacts[i].PreferAgentKeychain = true
		}
	}
	return artifacts
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
	if err := syncHostFileBeforeLaunch(artifact); err != nil {
		return nil, false, err
	}
	if err := syncHostKeychainBeforeLaunch(artifact); err != nil {
		return nil, false, err
	}
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
	if artifact.PreferAgentKeychain && artifact.WriteAgentKeychain != nil {
		if artifact.ReadAgentKeychain != nil {
			if _, ok, err := artifact.ReadAgentKeychain(); err != nil {
				return nil, false, err
			} else if ok {
				return stored, true, nil
			}
		}
		return stored, true, artifact.WriteAgentKeychain(stored)
	}
	return stored, true, artifact.WriteAgent(artifact.AgentPath, stored)
}

func harvestHarnessAuthArtifact(artifact harnessAuthArtifact, baseline harnessAuthData, baselineExists bool) error {
	reconcileKeychainResidueIntoAgentFile(artifact)
	data, ok, err := artifact.ReadAgent(artifact.AgentPath)
	if err != nil || !ok {
		return err
	}
	if !artifact.isHarvestable(data) {
		return artifact.RemoveAgent(artifact.AgentPath)
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
	removeErr := artifact.RemoveAgent(artifact.AgentPath)
	hostFileErr := publishHostFileAfterHarvest(artifact, data)
	hostErr := publishHostKeychainAfterHarvest(artifact, data)
	return errors.Join(removeErr, hostFileErr, hostErr)
}

func hostFileBackedAuthPath(home string, id credentials.ID) string {
	switch id { //nolint:exhaustive // only file-backed harness auth IDs have a host path; default returns empty
	case credentials.HarnessCodexAuth:
		return filepath.Join(home, ".codex", "auth.json")
	case credentials.HarnessOpenCodeAuth:
		return filepath.Join(home, ".local", "share", "opencode", "auth.json")
	default:
		return ""
	}
}

func syncHostFileBeforeLaunch(artifact harnessAuthArtifact) error {
	if strings.TrimSpace(artifact.HostPath) == "" {
		return nil
	}
	host, hostExists, err := artifact.ReadStore(artifact.HostPath)
	if err != nil {
		return fmt.Errorf("read host %s from %s: %w", artifact.Name, artifact.HostPath, err)
	}
	if hostExists && !artifact.isHarvestable(host) {
		hostExists = false
	}
	stored, storedExists, err := artifact.ReadStore(artifact.StorePath)
	if err != nil {
		return fmt.Errorf("read host-owned %s: %w", artifact.Name, err)
	}

	switch {
	case !storedExists && !hostExists:
		return nil
	case !storedExists && hostExists:
		return artifact.WriteStore(artifact.StorePath, host)
	case storedExists && !hostExists:
		return artifact.WriteStore(artifact.HostPath, stored)
	case artifact.Equal(stored, host):
		return nil
	}

	storeUpdatedAt, err := hostStoredSecretFileModTime(artifact.StorePath)
	if err != nil {
		return err
	}
	hostUpdatedAt, err := hostStoredSecretFileModTime(artifact.HostPath)
	if err != nil {
		return err
	}
	switch {
	case hostUpdatedAt.After(storeUpdatedAt):
		return artifact.WriteStore(artifact.StorePath, host)
	case storeUpdatedAt.After(hostUpdatedAt):
		return artifact.WriteStore(artifact.HostPath, stored)
	default:
		return fmt.Errorf("%s host file sync conflict: host store and host file changed at the same time", artifact.Name)
	}
}

func publishHostFileAfterHarvest(artifact harnessAuthArtifact, data harnessAuthData) error {
	if strings.TrimSpace(artifact.HostPath) == "" || !artifact.isHarvestable(data) {
		return nil
	}
	if err := artifact.WriteStore(artifact.HostPath, data); err != nil {
		return fmt.Errorf("write host %s to %s: %w", artifact.Name, artifact.HostPath, err)
	}
	return nil
}

func syncHostKeychainBeforeLaunch(artifact harnessAuthArtifact) error {
	if artifact.ReadHostKeychain == nil {
		return nil
	}
	host, hostExists, err := artifact.ReadHostKeychain()
	if err != nil {
		return fmt.Errorf("read host Keychain %s: %w", artifact.Name, err)
	}
	if hostExists && !artifact.isHarvestable(host.Data) {
		hostExists = false
	}
	stored, storedExists, err := artifact.ReadStore(artifact.StorePath)
	if err != nil {
		return fmt.Errorf("read host-owned %s: %w", artifact.Name, err)
	}

	switch {
	case !storedExists && !hostExists:
		return nil
	case !storedExists && hostExists:
		return artifact.WriteStore(artifact.StorePath, host.Data)
	case storedExists && !hostExists:
		return publishHostKeychainAfterHarvest(artifact, stored)
	case artifact.Equal(stored, host.Data):
		return nil
	}

	storeUpdatedAt, err := hostStoredSecretFileModTime(artifact.StorePath)
	if err != nil {
		return err
	}
	// Both copies exist and differ, so one must win on recency. If either
	// timestamp is unknown — e.g. parseSecurityKeychainModifiedTime could not
	// read the Keychain item's mdat — we cannot prove which is newer. Fail
	// closed rather than assuming the timestamped side wins: a zero time must
	// not let a stale value silently overwrite a possibly-newer one. This keeps
	// the no-silent-loss property at the host-store<->host-Keychain mtime
	// boundary, which MC_SecretStoreRecovery leaves out of model (it resolves
	// divergence by archiving conflicts, never by inferring freshness).
	if host.UpdatedAt.IsZero() || storeUpdatedAt.IsZero() {
		return fmt.Errorf("%s host Keychain sync conflict: cannot determine which of host store and host Keychain is newer (missing modification time); resolve manually", artifact.Name)
	}
	switch {
	case host.UpdatedAt.After(storeUpdatedAt):
		return artifact.WriteStore(artifact.StorePath, host.Data)
	case storeUpdatedAt.After(host.UpdatedAt):
		return publishHostKeychainAfterHarvest(artifact, stored)
	default:
		return fmt.Errorf("%s host Keychain sync conflict: host store and host Keychain changed at the same time", artifact.Name)
	}
}

func publishHostKeychainAfterHarvest(artifact harnessAuthArtifact, data harnessAuthData) error {
	if artifact.WriteHostKeychain == nil || !artifact.isHarvestable(data) {
		return nil
	}
	if err := artifact.WriteHostKeychain(data); err != nil {
		return fmt.Errorf("write host Keychain %s: %w", artifact.Name, err)
	}
	return nil
}

func hostStoredSecretFileModTime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("stat host-owned credential %s: %w", path, err)
	}
	return info.ModTime(), nil
}
