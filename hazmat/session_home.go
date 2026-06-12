package hazmat

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hazmat/containment"
)

const (
	defaultSessionHomeRoot = "/private/tmp/hazmat-home"
	sessionHomeMarkerFile  = ".hazmat-session-home"
)

type sessionHomeLayout struct {
	SessionID  string
	Root       string
	SessionDir string
	Home       string
	CacheHome  string
	ConfigHome string
	DataHome   string
	MarkerPath string
}

type sessionHomeAssemblyDurability string

const (
	sessionHomeDurableMirror   sessionHomeAssemblyDurability = "durable-mirror"
	sessionHomeDurableExternal sessionHomeAssemblyDurability = "durable-external"
	sessionHomeEphemeralCache  sessionHomeAssemblyDurability = "ephemeral-cache"
)

type sessionHomeAssemblyEntry struct {
	RelPath        string
	Class          containment.AgentHomeStateClass
	Durability     sessionHomeAssemblyDurability
	PersistentPath string
	RuntimePath    string
	Executable     bool
	RequiresBridge bool
}

type sessionHomeLaunchPhase string

const (
	sessionHomePhaseResolveIdentity sessionHomeLaunchPhase = "generate-or-resolve-session-id"
	sessionHomePhaseAssembleHome    sessionHomeLaunchPhase = "assemble-session-home"
	sessionHomePhaseSyncResumeState sessionHomeLaunchPhase = "sync-resume-state"
	sessionHomePhaseLaunchHarness   sessionHomeLaunchPhase = "launch-harness"
)

type sessionHomeLaunchBlockerReason string

const (
	sessionHomeBlockerDurableExternalBridge sessionHomeLaunchBlockerReason = "durable-external-bridge-required"
)

type sessionHomeLaunchBlocker struct {
	RelPath string
	Reason  sessionHomeLaunchBlockerReason
}

type sessionHomeLaunchPlan struct {
	Layout          sessionHomeLayout
	Assembly        []sessionHomeAssemblyEntry
	Phases          []sessionHomeLaunchPhase
	ResumeRequested bool
	Blockers        []sessionHomeLaunchBlocker
}

func newSessionHomeLayout(root, sessionID string) (sessionHomeLayout, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return sessionHomeLayout{}, fmt.Errorf("session home root %q must be absolute", root)
	}
	if err := validateSessionHomeID(sessionID); err != nil {
		return sessionHomeLayout{}, err
	}
	sessionDir := filepath.Join(root, sessionID)
	home := filepath.Join(sessionDir, "home")
	return sessionHomeLayout{
		SessionID:  sessionID,
		Root:       root,
		SessionDir: sessionDir,
		Home:       home,
		CacheHome:  filepath.Join(home, ".cache"),
		ConfigHome: filepath.Join(home, ".config"),
		DataHome:   filepath.Join(home, ".local", "share"),
		MarkerPath: filepath.Join(sessionDir, sessionHomeMarkerFile),
	}, nil
}

func validateSessionHomeID(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session home id is required")
	}
	for _, r := range sessionID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("session home id %q contains unsupported character %q", sessionID, r)
	}
	if sessionID == "." || sessionID == ".." || strings.HasPrefix(sessionID, ".") {
		return fmt.Errorf("session home id %q is reserved", sessionID)
	}
	return nil
}

func createSessionHomeLayout(layout sessionHomeLayout) error {
	if err := os.MkdirAll(layout.CacheHome, 0o700); err != nil {
		return fmt.Errorf("create session XDG cache dir: %w", err)
	}
	if err := os.MkdirAll(layout.ConfigHome, 0o700); err != nil {
		return fmt.Errorf("create session XDG config dir: %w", err)
	}
	if err := os.MkdirAll(layout.DataHome, 0o700); err != nil {
		return fmt.Errorf("create session XDG data dir: %w", err)
	}
	if err := os.WriteFile(layout.MarkerPath, []byte("hazmat session home\n"), 0o600); err != nil {
		return fmt.Errorf("write session home marker: %w", err)
	}
	return nil
}

func newSessionHomeAssemblyPlan(layout sessionHomeLayout, persistentHome string) ([]sessionHomeAssemblyEntry, error) {
	persistentHome = filepath.Clean(persistentHome)
	if !filepath.IsAbs(persistentHome) {
		return nil, fmt.Errorf("persistent agent home %q must be absolute", persistentHome)
	}
	if err := containment.ValidatePersistentAgentHomeManifest(); err != nil {
		return nil, err
	}

	executable := map[string]bool{}
	for _, entry := range containment.PersistentAgentHomeManifest() {
		for _, rel := range entry.ExecutableRelPaths {
			executable[rel] = true
		}
	}

	byRel := map[string]sessionHomeAssemblyEntry{}
	add := func(rel string, class containment.AgentHomeStateClass) {
		durability := sessionHomeDurabilityForClass(class)
		byRel[rel] = sessionHomeAssemblyEntry{
			RelPath:        rel,
			Class:          class,
			Durability:     durability,
			PersistentPath: filepath.Join(persistentHome, rel),
			RuntimePath:    filepath.Join(layout.Home, rel),
			Executable:     executable[rel],
			RequiresBridge: durability == sessionHomeDurableExternal,
		}
	}
	for _, manifestEntry := range containment.PersistentAgentHomeManifest() {
		add(manifestEntry.RelPath, manifestEntry.Class)
		for _, covered := range manifestEntry.CoveredPaths {
			add(covered.RelPath, covered.Class)
		}
		for _, rel := range manifestEntry.ExecutableRelPaths {
			if _, ok := byRel[rel]; !ok {
				add(rel, containment.AgentHomeStateExecutable)
			} else {
				entry := byRel[rel]
				entry.Executable = true
				byRel[rel] = entry
			}
		}
	}

	plan := make([]sessionHomeAssemblyEntry, 0, len(byRel))
	for _, entry := range byRel {
		plan = append(plan, entry)
	}
	sort.Slice(plan, func(i, j int) bool { return plan[i].RelPath < plan[j].RelPath })
	return plan, nil
}

func newSessionHomeLaunchPlan(root, sessionID, persistentHome string, resumeRequested bool) (sessionHomeLaunchPlan, error) {
	layout, err := newSessionHomeLayout(root, sessionID)
	if err != nil {
		return sessionHomeLaunchPlan{}, err
	}
	assembly, err := newSessionHomeAssemblyPlan(layout, persistentHome)
	if err != nil {
		return sessionHomeLaunchPlan{}, err
	}
	phases := []sessionHomeLaunchPhase{
		sessionHomePhaseResolveIdentity,
		sessionHomePhaseAssembleHome,
	}
	if resumeRequested {
		phases = append(phases, sessionHomePhaseSyncResumeState)
	}
	phases = append(phases, sessionHomePhaseLaunchHarness)

	return sessionHomeLaunchPlan{
		Layout:          layout,
		Assembly:        assembly,
		Phases:          phases,
		ResumeRequested: resumeRequested,
		Blockers:        sessionHomeLaunchBlockers(assembly),
	}, nil
}

func (plan sessionHomeLaunchPlan) readyForActivation() bool {
	return len(plan.Blockers) == 0
}

func sessionHomeLaunchBlockers(assembly []sessionHomeAssemblyEntry) []sessionHomeLaunchBlocker {
	blockers := make([]sessionHomeLaunchBlocker, 0)
	for _, entry := range assembly {
		if !entry.RequiresBridge {
			continue
		}
		blockers = append(blockers, sessionHomeLaunchBlocker{
			RelPath: entry.RelPath,
			Reason:  sessionHomeBlockerDurableExternalBridge,
		})
	}
	return blockers
}

func sessionHomeDurabilityForClass(class containment.AgentHomeStateClass) sessionHomeAssemblyDurability {
	switch class {
	case containment.AgentHomeStateTranscript:
		return sessionHomeDurableExternal
	case containment.AgentHomeStateXDGCache:
		return sessionHomeEphemeralCache
	default:
		return sessionHomeDurableMirror
	}
}

func cleanupStaleSessionHomes(root string, now time.Time, maxAge time.Duration) ([]string, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("session home root %q must be absolute", root)
	}
	if maxAge <= 0 {
		return nil, fmt.Errorf("session home cleanup max age must be positive")
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session home root: %w", err)
	}
	var removed []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		sessionDir := filepath.Join(root, entry.Name())
		marker := filepath.Join(sessionDir, sessionHomeMarkerFile)
		info, err := os.Stat(marker)
		if err != nil || info.IsDir() {
			continue
		}
		if now.Sub(info.ModTime()) < maxAge {
			continue
		}
		if err := os.RemoveAll(sessionDir); err != nil {
			return removed, fmt.Errorf("remove stale session home %s: %w", sessionDir, err)
		}
		removed = append(removed, sessionDir)
	}
	return removed, nil
}
