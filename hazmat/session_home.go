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
	defaultSessionHomeRoot          = "/private/tmp/hazmat-home"
	defaultSessionHomeCleanupMaxAge = 24 * time.Hour
	experimentalSessionHomeEnv      = "HAZMAT_EXPERIMENTAL_SESSION_HOME"
	sessionHomeMarkerFile           = ".hazmat-session-home"
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

type sessionHomeRuntimePolicy string

const (
	sessionHomePolicyEphemeralCache   sessionHomeRuntimePolicy = "ephemeral-cache"
	sessionHomePolicyDurableExternal  sessionHomeRuntimePolicy = "durable-external"
	sessionHomePolicySeedOnly         sessionHomeRuntimePolicy = "seed-only"
	sessionHomePolicyCheckedWriteback sessionHomeRuntimePolicy = "checked-writeback"
	sessionHomePolicyAdapterRequired  sessionHomeRuntimePolicy = "adapter-required"
)

type sessionHomeAssemblyEntry struct {
	RelPath        string
	Class          containment.AgentHomeStateClass
	Durability     sessionHomeAssemblyDurability
	RuntimePolicy  sessionHomeRuntimePolicy
	PersistentPath string
	RuntimePath    string
	Executable     bool
	RequiresBridge bool
}

type sessionHomeLaunchPhase string

const (
	sessionHomePhaseCleanupStaleHomes sessionHomeLaunchPhase = "cleanup-stale-session-homes"
	sessionHomePhaseResolveIdentity   sessionHomeLaunchPhase = "generate-or-resolve-session-id"
	sessionHomePhaseAssembleHome      sessionHomeLaunchPhase = "assemble-session-home"
	sessionHomePhaseSyncResumeState   sessionHomeLaunchPhase = "sync-resume-state"
	sessionHomePhaseLaunchHarness     sessionHomeLaunchPhase = "launch-harness"
)

type sessionHomeLaunchBlockerReason string

const (
	sessionHomeBlockerActivationGate    sessionHomeLaunchBlockerReason = "activation-gate"
	sessionHomeBlockerDurableMirrorSync sessionHomeLaunchBlockerReason = "durable-mirror-sync"
	sessionHomeBlockerSeedMaterialize   sessionHomeLaunchBlockerReason = "seed-materialization"
	sessionHomeBlockerAdapterRequired   sessionHomeLaunchBlockerReason = "adapter-required"
	sessionHomeBlockerCheckedWriteback  sessionHomeLaunchBlockerReason = "checked-writeback"
)

type sessionHomeLaunchBlocker struct {
	RelPath string
	Reason  sessionHomeLaunchBlockerReason
}

type sessionHomeBridgeKind string

const (
	sessionHomeBridgeHomeRelativeRoot sessionHomeBridgeKind = "home-relative-root"
	sessionHomeBridgeHarnessEnvRoot   sessionHomeBridgeKind = "harness-env-root"
)

type sessionHomeBridgeRequirement struct {
	RelPath        string
	Kind           sessionHomeBridgeKind
	PersistentRoot string
	RuntimeRoot    string
	EnvVar         string
	ProjectScoped  bool
}

type sessionHomeCleanupPlan struct {
	Root   string
	MaxAge time.Duration
}

type sessionHomeLaunchPlan struct {
	Layout             sessionHomeLayout
	Assembly           []sessionHomeAssemblyEntry
	BridgeRequirements []sessionHomeBridgeRequirement
	Cleanup            sessionHomeCleanupPlan
	Phases             []sessionHomeLaunchPhase
	ResumeRequested    bool
	Blockers           []sessionHomeLaunchBlocker
}

type sessionHomeRuntimePlan struct {
	Launch          sessionHomeLaunchPlan
	AgentHomePolicy containment.AgentHomePolicy
}

type sessionHomeMaterializationResult struct {
	CheckedWritebackReceipts []sessionHomeCheckedWritebackReceipt
}

var newSessionHomeID = defaultSessionHomeID

func defaultSessionHomeID() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}

func experimentalSessionHomeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(experimentalSessionHomeEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func applyExperimentalSessionHomePlan(cfg *sessionConfig, mode sessionMode, opts harnessSessionOpts) error {
	if !experimentalSessionHomeEnabled() {
		return nil
	}
	if mode != sessionModeNative {
		return fmt.Errorf("%s=1 supports native sessions only", experimentalSessionHomeEnv)
	}
	if !opts.planOnly {
		return fmt.Errorf("%s=1 is currently plan-only; use hazmat explain to inspect the session-local HOME plan", experimentalSessionHomeEnv)
	}
	launchPlan, err := newSessionHomeLaunchPlan(defaultSessionHomeRoot, newSessionHomeID(), agentHome, true)
	if err != nil {
		return err
	}
	runtimePlan, err := newSessionHomeRuntimePlan(launchPlan, agentHome)
	if err != nil {
		return err
	}
	cfg.SessionHome = &runtimePlan
	cfg.SessionNotes = append(cfg.SessionNotes,
		fmt.Sprintf("Experimental session-local HOME preview: HOME=%s with durable transcript bridges under %s.", launchPlan.Layout.Home, agentHome),
		"Session-local HOME launch remains disabled until activation coverage and live validation land.",
	)
	return nil
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
			RuntimePolicy:  sessionHomeRuntimePolicyFor(rel, class, durability),
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
	bridgeRequirements, err := sessionHomeBridgeRequirements(assembly)
	if err != nil {
		return sessionHomeLaunchPlan{}, err
	}
	blockers, err := sessionHomeActivationBlockers(assembly, sessionHomePersistentPathExists)
	if err != nil {
		return sessionHomeLaunchPlan{}, err
	}
	phases := []sessionHomeLaunchPhase{
		sessionHomePhaseCleanupStaleHomes,
		sessionHomePhaseResolveIdentity,
		sessionHomePhaseAssembleHome,
	}
	if resumeRequested {
		phases = append(phases, sessionHomePhaseSyncResumeState)
	}
	phases = append(phases, sessionHomePhaseLaunchHarness)

	return sessionHomeLaunchPlan{
		Layout:             layout,
		Assembly:           assembly,
		BridgeRequirements: bridgeRequirements,
		Cleanup: sessionHomeCleanupPlan{
			Root:   layout.Root,
			MaxAge: defaultSessionHomeCleanupMaxAge,
		},
		Phases:          phases,
		ResumeRequested: resumeRequested,
		Blockers:        blockers,
	}, nil
}

func (plan sessionHomeLaunchPlan) readyForActivation() bool {
	return len(plan.Blockers) == 0
}

func sessionHomeActivationBlockerSummary(blockers []sessionHomeLaunchBlocker) string {
	if len(blockers) == 0 {
		return "ready"
	}
	counts := map[sessionHomeLaunchBlockerReason]int{}
	for _, blocker := range blockers {
		counts[blocker.Reason]++
	}
	var parts []string
	for _, reason := range []sessionHomeLaunchBlockerReason{
		sessionHomeBlockerSeedMaterialize,
		sessionHomeBlockerAdapterRequired,
		sessionHomeBlockerCheckedWriteback,
		sessionHomeBlockerActivationGate,
		sessionHomeBlockerDurableMirrorSync,
	} {
		count := counts[reason]
		if count == 0 {
			continue
		}
		delete(counts, reason)
		parts = append(parts, sessionHomeActivationBlockerReasonLabel(reason, count))
	}
	for reason, count := range counts {
		parts = append(parts, sessionHomeActivationBlockerReasonLabel(reason, count))
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

func sessionHomeActivationBlockerReasonLabel(reason sessionHomeLaunchBlockerReason, count int) string {
	label := string(reason)
	switch reason {
	case sessionHomeBlockerSeedMaterialize:
		label = "seed materialization"
	case sessionHomeBlockerAdapterRequired:
		label = "adapter required"
	case sessionHomeBlockerCheckedWriteback:
		label = "checked writeback"
	case sessionHomeBlockerDurableMirrorSync:
		label = "durable mirror sync"
	case sessionHomeBlockerActivationGate:
		label = "activation gate"
	}
	if count == 1 {
		return label
	}
	return fmt.Sprintf("%s (%d paths)", label, count)
}

func sessionHomeBridgeRequirements(assembly []sessionHomeAssemblyEntry) ([]sessionHomeBridgeRequirement, error) {
	requirements := make([]sessionHomeBridgeRequirement, 0)
	for _, entry := range assembly {
		if !entry.RequiresBridge {
			continue
		}
		requirement, err := sessionHomeBridgeRequirementForEntry(entry)
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, requirement)
	}
	return requirements, nil
}

func sessionHomeBridgeRequirementForEntry(entry sessionHomeAssemblyEntry) (sessionHomeBridgeRequirement, error) {
	switch entry.RelPath {
	case ".claude/projects":
		return sessionHomeBridgeRequirement{
			RelPath:        entry.RelPath,
			Kind:           sessionHomeBridgeHomeRelativeRoot,
			PersistentRoot: entry.PersistentPath,
			RuntimeRoot:    entry.RuntimePath,
		}, nil
	case ".hazmat/hermes/projects":
		return sessionHomeBridgeRequirement{
			RelPath:        entry.RelPath,
			Kind:           sessionHomeBridgeHarnessEnvRoot,
			PersistentRoot: entry.PersistentPath,
			RuntimeRoot:    entry.RuntimePath,
			EnvVar:         "HERMES_HOME",
			ProjectScoped:  true,
		}, nil
	default:
		return sessionHomeBridgeRequirement{}, fmt.Errorf("%s requires a session-home bridge but has no contract", entry.RelPath)
	}
}

func materializeSessionHomeBridges(layout sessionHomeLayout, requirements []sessionHomeBridgeRequirement) error {
	for _, requirement := range requirements {
		if err := validateSessionHomeBridgeRequirement(layout, requirement); err != nil {
			return err
		}
		switch requirement.Kind {
		case sessionHomeBridgeHomeRelativeRoot:
			if err := materializeSessionHomeSymlinkBridge(requirement); err != nil {
				return err
			}
		case sessionHomeBridgeHarnessEnvRoot:
			if err := os.MkdirAll(requirement.PersistentRoot, 0o700); err != nil {
				return fmt.Errorf("create persistent bridge root %s: %w", requirement.PersistentRoot, err)
			}
		default:
			return fmt.Errorf("%s: unsupported session-home bridge kind %q", requirement.RelPath, requirement.Kind)
		}
	}
	return nil
}

func materializeSessionHomeLaunchPlan(plan sessionHomeLaunchPlan) (sessionHomeMaterializationResult, error) {
	if err := createSessionHomeLayout(plan.Layout); err != nil {
		return sessionHomeMaterializationResult{}, err
	}
	if err := materializeSessionHomeBridges(plan.Layout, plan.BridgeRequirements); err != nil {
		return sessionHomeMaterializationResult{}, err
	}
	if err := materializeSessionHomeSeedEntries(plan.Layout, plan.Assembly); err != nil {
		return sessionHomeMaterializationResult{}, err
	}
	receipts, err := materializeSessionHomeCheckedWritebackEntries(plan.Layout, plan.Assembly)
	if err != nil {
		return sessionHomeMaterializationResult{}, err
	}
	return sessionHomeMaterializationResult{CheckedWritebackReceipts: receipts}, nil
}

func materializeSessionHomeSeedEntries(layout sessionHomeLayout, assembly []sessionHomeAssemblyEntry) error {
	for _, entry := range assembly {
		if entry.RuntimePolicy != sessionHomePolicySeedOnly {
			continue
		}
		if err := validateSessionHomeAssemblyEntry(layout, entry); err != nil {
			return err
		}
		if err := materializeSessionHomeSeedEntry(layout, entry); err != nil {
			return err
		}
	}
	return nil
}

func validateSessionHomeAssemblyEntry(layout sessionHomeLayout, entry sessionHomeAssemblyEntry) error {
	if strings.TrimSpace(entry.RelPath) == "" {
		return fmt.Errorf("session-home assembly rel path is required")
	}
	if entry.PersistentPath == "" || !filepath.IsAbs(entry.PersistentPath) {
		return fmt.Errorf("%s: persistent path %q must be absolute", entry.RelPath, entry.PersistentPath)
	}
	if entry.RuntimePath == "" || !filepath.IsAbs(entry.RuntimePath) {
		return fmt.Errorf("%s: runtime path %q must be absolute", entry.RelPath, entry.RuntimePath)
	}
	if !isWithinDir(layout.Home, filepath.Clean(entry.RuntimePath)) {
		return fmt.Errorf("%s: runtime path %s escapes session home %s", entry.RelPath, entry.RuntimePath, layout.Home)
	}
	if isWithinDir(layout.Home, filepath.Clean(entry.PersistentPath)) {
		return fmt.Errorf("%s: persistent path %s must stay outside session home %s", entry.RelPath, entry.PersistentPath, layout.Home)
	}
	return nil
}

func materializeSessionHomeSeedEntry(layout sessionHomeLayout, entry sessionHomeAssemblyEntry) error {
	info, err := os.Lstat(entry.PersistentPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: inspect seed source: %w", entry.RelPath, err)
	}
	return copySessionHomeSeedPath(layout, entry.PersistentPath, entry.RuntimePath, info)
}

func copySessionHomeSeedPath(layout sessionHomeLayout, src, dest string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: seed source symlinks are not supported", src)
	}
	switch {
	case info.Mode().IsRegular():
		return copySessionHomeSeedFile(layout, src, dest, info)
	case info.IsDir():
		return copySessionHomeSeedDir(layout, src, dest, info)
	default:
		return fmt.Errorf("%s: unsupported seed source type %s", src, info.Mode().String())
	}
}

func copySessionHomeSeedFile(layout sessionHomeLayout, src, dest string, info os.FileInfo) error {
	if err := ensureSessionHomeParentDir(layout, dest); err != nil {
		return err
	}
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%s: seed destination already exists", dest)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("%s: inspect seed destination: %w", dest, err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("%s: read seed source: %w", src, err)
	}
	mode := info.Mode().Perm()
	if err := os.WriteFile(dest, data, mode); err != nil {
		return fmt.Errorf("%s: write seed destination: %w", dest, err)
	}
	if err := os.Chtimes(dest, info.ModTime(), info.ModTime()); err != nil {
		return fmt.Errorf("%s: set seed destination timestamp: %w", dest, err)
	}
	return nil
}

func copySessionHomeSeedDir(layout sessionHomeLayout, src, dest string, info os.FileInfo) error {
	if err := ensureSessionHomeDir(layout, dest); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("%s: read seed source dir: %w", src, err)
	}
	for _, child := range entries {
		childSrc := filepath.Join(src, child.Name())
		childDest := filepath.Join(dest, child.Name())
		childInfo, err := os.Lstat(childSrc)
		if err != nil {
			return fmt.Errorf("%s: inspect seed child: %w", childSrc, err)
		}
		if err := copySessionHomeSeedPath(layout, childSrc, childDest, childInfo); err != nil {
			return err
		}
	}
	if err := os.Chmod(dest, info.Mode().Perm()); err != nil {
		return fmt.Errorf("%s: set seed destination mode: %w", dest, err)
	}
	if err := os.Chtimes(dest, info.ModTime(), info.ModTime()); err != nil {
		return fmt.Errorf("%s: set seed destination timestamp: %w", dest, err)
	}
	return nil
}

func ensureSessionHomeParentDir(layout sessionHomeLayout, path string) error {
	return ensureSessionHomeDir(layout, filepath.Dir(path))
}

func ensureSessionHomeDir(layout sessionHomeLayout, dir string) error {
	home := filepath.Clean(layout.Home)
	if home == "" || !filepath.IsAbs(home) {
		return fmt.Errorf("session home path %q must be absolute", layout.Home)
	}
	info, err := os.Lstat(home)
	if err != nil {
		return fmt.Errorf("%s: inspect session home root: %w", home, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: session home root is a symlink", home)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: session home root is not a directory", home)
	}
	dir = filepath.Clean(dir)
	if dir == home {
		return nil
	}
	if !isWithinDir(home, dir) {
		return fmt.Errorf("%s: directory escapes session home %s", dir, home)
	}
	rel, err := filepath.Rel(home, dir)
	if err != nil {
		return fmt.Errorf("%s: compute session-home relative path: %w", dir, err)
	}
	current := home
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return fmt.Errorf("%s: create session-home dir: %w", current, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: inspect session-home dir: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: session-home dir is a symlink", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s: session-home path is not a directory", current)
		}
	}
	return nil
}

func sessionHomeAgentHomePolicy(plan sessionHomeLaunchPlan, persistentHome string) (containment.AgentHomePolicy, error) {
	persistentHome = filepath.Clean(persistentHome)
	if !filepath.IsAbs(persistentHome) {
		return containment.AgentHomePolicy{}, fmt.Errorf("persistent agent home %q must be absolute", persistentHome)
	}
	if plan.Layout.Home == "" || !filepath.IsAbs(plan.Layout.Home) {
		return containment.AgentHomePolicy{}, fmt.Errorf("session home path %q must be absolute", plan.Layout.Home)
	}
	roots := make([]string, 0, len(plan.BridgeRequirements))
	seen := map[string]struct{}{}
	for _, requirement := range plan.BridgeRequirements {
		root := filepath.Clean(requirement.PersistentRoot)
		if !filepath.IsAbs(root) {
			return containment.AgentHomePolicy{}, fmt.Errorf("%s: persistent bridge root %q must be absolute", requirement.RelPath, requirement.PersistentRoot)
		}
		if !isWithinDir(persistentHome, root) {
			return containment.AgentHomePolicy{}, fmt.Errorf("%s: persistent bridge root %s is outside %s", requirement.RelPath, root, persistentHome)
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return containment.AgentHomePolicy{
		Path:               plan.Layout.Home,
		Mode:               containment.AgentHomeModeSessionLocal,
		PersistentPath:     persistentHome,
		DurableBridgeRoots: roots,
	}, nil
}

func newSessionHomeRuntimePlan(plan sessionHomeLaunchPlan, persistentHome string) (sessionHomeRuntimePlan, error) {
	policy, err := sessionHomeAgentHomePolicy(plan, persistentHome)
	if err != nil {
		return sessionHomeRuntimePlan{}, err
	}
	return sessionHomeRuntimePlan{
		Launch:          plan,
		AgentHomePolicy: policy,
	}, nil
}

type sessionHomePersistentPathExistsFunc func(path string) (bool, error)

func sessionHomePersistentPathExists(path string) (bool, error) {
	clean := filepath.Clean(path)
	if clean == filepath.Clean(agentHome) || isWithinDir(filepath.Clean(agentHome), clean) {
		return false, nil
	}
	if _, err := os.Lstat(clean); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func sessionHomeActivationBlockers(assembly []sessionHomeAssemblyEntry, persistentPathExists sessionHomePersistentPathExistsFunc) ([]sessionHomeLaunchBlocker, error) {
	blockers := []sessionHomeLaunchBlocker{{
		RelPath: "session-home",
		Reason:  sessionHomeBlockerActivationGate,
	}}
	for _, entry := range assembly {
		reason, blocked := sessionHomeActivationBlockerReasonForPolicy(entry.RuntimePolicy)
		if !blocked {
			continue
		}
		exists, err := persistentPathExists(entry.PersistentPath)
		if err != nil {
			return nil, fmt.Errorf("%s: inspect persistent path for activation blockers: %w", entry.RelPath, err)
		}
		if !exists {
			continue
		}
		blockers = append(blockers, sessionHomeLaunchBlocker{
			RelPath: entry.RelPath,
			Reason:  reason,
		})
	}
	return blockers, nil
}

func sessionHomeActivationBlockerReasonForPolicy(policy sessionHomeRuntimePolicy) (sessionHomeLaunchBlockerReason, bool) {
	switch policy {
	case sessionHomePolicyAdapterRequired:
		return sessionHomeBlockerAdapterRequired, true
	default:
		return "", false
	}
}

func validateSessionHomeBridgeRequirement(layout sessionHomeLayout, requirement sessionHomeBridgeRequirement) error {
	if strings.TrimSpace(requirement.RelPath) == "" {
		return fmt.Errorf("session-home bridge rel path is required")
	}
	if requirement.PersistentRoot == "" || !filepath.IsAbs(requirement.PersistentRoot) {
		return fmt.Errorf("%s: persistent bridge root %q must be absolute", requirement.RelPath, requirement.PersistentRoot)
	}
	if isWithinDir(layout.Home, filepath.Clean(requirement.PersistentRoot)) {
		return fmt.Errorf("%s: persistent bridge root %s must stay outside session home %s", requirement.RelPath, requirement.PersistentRoot, layout.Home)
	}
	if requirement.RuntimeRoot != "" {
		runtimeRoot := filepath.Clean(requirement.RuntimeRoot)
		if !filepath.IsAbs(runtimeRoot) {
			return fmt.Errorf("%s: runtime bridge root %q must be absolute", requirement.RelPath, requirement.RuntimeRoot)
		}
		if !isWithinDir(layout.Home, runtimeRoot) {
			return fmt.Errorf("%s: runtime bridge root %s escapes session home %s", requirement.RelPath, runtimeRoot, layout.Home)
		}
	}
	if requirement.Kind == sessionHomeBridgeHarnessEnvRoot && requirement.EnvVar == "" {
		return fmt.Errorf("%s: harness env bridge requires an env var", requirement.RelPath)
	}
	return nil
}

func materializeSessionHomeSymlinkBridge(requirement sessionHomeBridgeRequirement) error {
	if err := os.MkdirAll(requirement.PersistentRoot, 0o700); err != nil {
		return fmt.Errorf("create persistent bridge root %s: %w", requirement.PersistentRoot, err)
	}
	if err := os.MkdirAll(filepath.Dir(requirement.RuntimeRoot), 0o700); err != nil {
		return fmt.Errorf("create bridge parent for %s: %w", requirement.RuntimeRoot, err)
	}
	existing, err := os.Readlink(requirement.RuntimeRoot)
	if err == nil {
		if existing == requirement.PersistentRoot {
			return nil
		}
		return fmt.Errorf("%s: existing symlink points to %s, want %s", requirement.RuntimeRoot, existing, requirement.PersistentRoot)
	}
	if !os.IsNotExist(err) {
		if _, statErr := os.Lstat(requirement.RuntimeRoot); statErr == nil {
			return fmt.Errorf("%s already exists and is not the expected symlink", requirement.RuntimeRoot)
		}
		return fmt.Errorf("inspect bridge root %s: %w", requirement.RuntimeRoot, err)
	}
	if err := os.Symlink(requirement.PersistentRoot, requirement.RuntimeRoot); err != nil {
		return fmt.Errorf("link %s -> %s: %w", requirement.RuntimeRoot, requirement.PersistentRoot, err)
	}
	return nil
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

func sessionHomeRuntimePolicyFor(rel string, class containment.AgentHomeStateClass, durability sessionHomeAssemblyDurability) sessionHomeRuntimePolicy {
	switch durability {
	case sessionHomeDurableExternal:
		return sessionHomePolicyDurableExternal
	case sessionHomeEphemeralCache:
		return sessionHomePolicyEphemeralCache
	}
	switch class {
	case containment.AgentHomeStateShellConfig, containment.AgentHomeStateGitConfig:
		return sessionHomePolicySeedOnly
	}
	switch rel {
	case ".claude/commands", ".claude/skills":
		return sessionHomePolicySeedOnly
	default:
		return sessionHomePolicyAdapterRequired
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
