package hazmat

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hazmat/containment"
)

func TestNewSessionHomeLayoutBuildsXDGPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	layout, err := newSessionHomeLayout(root, "session-123")
	if err != nil {
		t.Fatalf("newSessionHomeLayout: %v", err)
	}

	want := sessionHomeLayout{
		SessionID:  "session-123",
		Root:       root,
		SessionDir: filepath.Join(root, "session-123"),
		Home:       filepath.Join(root, "session-123", "home"),
		CacheHome:  filepath.Join(root, "session-123", "home", ".cache"),
		ConfigHome: filepath.Join(root, "session-123", "home", ".config"),
		DataHome:   filepath.Join(root, "session-123", "home", ".local", "share"),
		MarkerPath: filepath.Join(root, "session-123", sessionHomeMarkerFile),
	}
	if !reflect.DeepEqual(layout, want) {
		t.Fatalf("layout = %+v, want %+v", layout, want)
	}
}

func TestNewSessionHomeLayoutRejectsUnsafeInputs(t *testing.T) {
	for _, tc := range []struct {
		root      string
		sessionID string
	}{
		{"relative", "session-123"},
		{"/tmp/hazmat-home", ""},
		{"/tmp/hazmat-home", "."},
		{"/tmp/hazmat-home", ".."},
		{"/tmp/hazmat-home", ".hidden"},
		{"/tmp/hazmat-home", "../escape"},
		{"/tmp/hazmat-home", "has/slash"},
		{"/tmp/hazmat-home", "has space"},
		{"/tmp/hazmat-home", "snowman-\u2603"},
	} {
		if _, err := newSessionHomeLayout(tc.root, tc.sessionID); err == nil {
			t.Fatalf("newSessionHomeLayout(%q, %q) succeeded, want error", tc.root, tc.sessionID)
		}
	}
}

func TestApplyExperimentalSessionHomePlanDisabledByDefault(t *testing.T) {
	t.Setenv(experimentalSessionHomeEnv, "")
	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/hazmat"}

	if err := applyExperimentalSessionHomePlan(&cfg, sessionModeNative, harnessSessionOpts{planOnly: true}); err != nil {
		t.Fatalf("applyExperimentalSessionHomePlan: %v", err)
	}
	if cfg.SessionHome != nil {
		t.Fatalf("SessionHome = %+v, want nil when gate is disabled", cfg.SessionHome)
	}
}

func TestApplyExperimentalSessionHomePlanRequiresPlanOnlyNativeSession(t *testing.T) {
	t.Setenv(experimentalSessionHomeEnv, "1")
	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/hazmat"}

	if err := applyExperimentalSessionHomePlan(&cfg, sessionModeNative, harnessSessionOpts{}); err == nil {
		t.Fatal("applyExperimentalSessionHomePlan accepted executable native launch")
	}
	if err := applyExperimentalSessionHomePlan(&cfg, sessionModeDockerSandbox, harnessSessionOpts{planOnly: true}); err == nil {
		t.Fatal("applyExperimentalSessionHomePlan accepted non-native mode")
	}
}

func TestApplyExperimentalSessionHomePlanBuildsRuntimePreview(t *testing.T) {
	t.Setenv(experimentalSessionHomeEnv, "1")
	savedNewSessionHomeID := newSessionHomeID
	newSessionHomeID = func() string { return "session-123" }
	t.Cleanup(func() { newSessionHomeID = savedNewSessionHomeID })
	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/hazmat"}

	if err := applyExperimentalSessionHomePlan(&cfg, sessionModeNative, harnessSessionOpts{planOnly: true}); err != nil {
		t.Fatalf("applyExperimentalSessionHomePlan: %v", err)
	}
	if cfg.SessionHome == nil {
		t.Fatal("SessionHome = nil, want runtime plan")
	}
	if got, want := cfg.SessionHome.Launch.Layout.Home, filepath.Join(defaultSessionHomeRoot, "session-123", "home"); got != want {
		t.Fatalf("SessionHome layout = %s, want %s", got, want)
	}
	if cfg.SessionHome.AgentHomePolicy.Mode != containment.AgentHomeModeSessionLocal {
		t.Fatalf("AgentHomePolicy mode = %s", cfg.SessionHome.AgentHomePolicy.Mode)
	}
	if len(cfg.SessionNotes) < 2 || !strings.Contains(cfg.SessionNotes[0], "Experimental session-local HOME preview") {
		t.Fatalf("SessionNotes = %v", cfg.SessionNotes)
	}
}

func TestCreateSessionHomeLayoutCreatesMarkerAndXDGDirs(t *testing.T) {
	layout, err := newSessionHomeLayout(filepath.Join(t.TempDir(), "hazmat-home"), "session-123")
	if err != nil {
		t.Fatal(err)
	}
	if err := createSessionHomeLayout(layout); err != nil {
		t.Fatalf("createSessionHomeLayout: %v", err)
	}

	for _, dir := range []string{layout.Home, layout.CacheHome, layout.ConfigHome, layout.DataHome} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
	}
	marker, err := os.ReadFile(layout.MarkerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(marker), "hazmat session home") {
		t.Fatalf("marker = %q", marker)
	}
}

func TestNewSessionHomeAssemblyPlanClassifiesDurability(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	layout, err := newSessionHomeLayout(root, "session-123")
	if err != nil {
		t.Fatal(err)
	}
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeAssemblyPlan(layout, persistentHome)
	if err != nil {
		t.Fatalf("newSessionHomeAssemblyPlan: %v", err)
	}

	byRel := map[string]sessionHomeAssemblyEntry{}
	for _, entry := range plan {
		byRel[entry.RelPath] = entry
		if !strings.HasPrefix(entry.PersistentPath, persistentHome+string(os.PathSeparator)) {
			t.Fatalf("%s persistent path = %s, want under %s", entry.RelPath, entry.PersistentPath, persistentHome)
		}
		if !strings.HasPrefix(entry.RuntimePath, layout.Home+string(os.PathSeparator)) {
			t.Fatalf("%s runtime path = %s, want under %s", entry.RelPath, entry.RuntimePath, layout.Home)
		}
	}

	for _, tc := range []struct {
		rel         string
		class       containment.AgentHomeStateClass
		durability  sessionHomeAssemblyDurability
		executable  bool
		bridge      bool
		persistent  string
		runtimePath string
	}{
		{
			rel:         ".claude/projects",
			class:       containment.AgentHomeStateTranscript,
			durability:  sessionHomeDurableExternal,
			bridge:      true,
			persistent:  filepath.Join(persistentHome, ".claude", "projects"),
			runtimePath: filepath.Join(layout.Home, ".claude", "projects"),
		},
		{
			rel:         ".hazmat/hermes/projects",
			class:       containment.AgentHomeStateTranscript,
			durability:  sessionHomeDurableExternal,
			bridge:      true,
			persistent:  filepath.Join(persistentHome, ".hazmat", "hermes", "projects"),
			runtimePath: filepath.Join(layout.Home, ".hazmat", "hermes", "projects"),
		},
		{
			rel:        ".local/bin",
			class:      containment.AgentHomeStateExecutable,
			durability: sessionHomeDurableMirror,
			executable: true,
		},
		{
			rel:        ".gitconfig",
			class:      containment.AgentHomeStateGitConfig,
			durability: sessionHomeDurableMirror,
		},
		{
			rel:        ".cache",
			class:      containment.AgentHomeStateXDGCache,
			durability: sessionHomeEphemeralCache,
		},
	} {
		entry, ok := byRel[tc.rel]
		if !ok {
			t.Fatalf("assembly plan missing %s", tc.rel)
		}
		if entry.Class != tc.class || entry.Durability != tc.durability || entry.Executable != tc.executable || entry.RequiresBridge != tc.bridge {
			t.Fatalf("%s = %+v, want class=%s durability=%s executable=%v bridge=%v", tc.rel, entry, tc.class, tc.durability, tc.executable, tc.bridge)
		}
		if tc.persistent != "" && entry.PersistentPath != tc.persistent {
			t.Fatalf("%s persistent path = %s, want %s", tc.rel, entry.PersistentPath, tc.persistent)
		}
		if tc.runtimePath != "" && entry.RuntimePath != tc.runtimePath {
			t.Fatalf("%s runtime path = %s, want %s", tc.rel, entry.RuntimePath, tc.runtimePath)
		}
	}
}

func TestNewSessionHomeAssemblyPlanRejectsRelativePersistentHome(t *testing.T) {
	layout, err := newSessionHomeLayout(filepath.Join(t.TempDir(), "hazmat-home"), "session-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newSessionHomeAssemblyPlan(layout, "relative-agent-home"); err == nil {
		t.Fatal("newSessionHomeAssemblyPlan accepted relative persistent home")
	}
}

func TestNewSessionHomeLaunchPlanOrdersResumeAfterAssemblyBeforeLaunch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")

	plan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	want := []sessionHomeLaunchPhase{
		sessionHomePhaseCleanupStaleHomes,
		sessionHomePhaseResolveIdentity,
		sessionHomePhaseAssembleHome,
		sessionHomePhaseSyncResumeState,
		sessionHomePhaseLaunchHarness,
	}
	if !reflect.DeepEqual(plan.Phases, want) {
		t.Fatalf("phases = %#v, want %#v", plan.Phases, want)
	}
	if !plan.ResumeRequested {
		t.Fatal("ResumeRequested = false, want true")
	}
	if plan.Layout.Home != filepath.Join(root, "session-123", "home") {
		t.Fatalf("layout home = %s", plan.Layout.Home)
	}
	if plan.Cleanup.Root != root || plan.Cleanup.MaxAge != defaultSessionHomeCleanupMaxAge {
		t.Fatalf("cleanup = %+v, want root %s age %s", plan.Cleanup, root, defaultSessionHomeCleanupMaxAge)
	}
	if len(plan.Assembly) == 0 {
		t.Fatal("launch plan has no assembly entries")
	}
}

func TestNewSessionHomeLaunchPlanOmitsResumeSyncWhenNotRequested(t *testing.T) {
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", filepath.Join(t.TempDir(), "agent"), false)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	want := []sessionHomeLaunchPhase{
		sessionHomePhaseCleanupStaleHomes,
		sessionHomePhaseResolveIdentity,
		sessionHomePhaseAssembleHome,
		sessionHomePhaseLaunchHarness,
	}
	if !reflect.DeepEqual(plan.Phases, want) {
		t.Fatalf("phases = %#v, want %#v", plan.Phases, want)
	}
	if plan.ResumeRequested {
		t.Fatal("ResumeRequested = true, want false")
	}
}

func TestNewSessionHomeLaunchPlanReadyAfterBridgeRequirementsAreModeled(t *testing.T) {
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", filepath.Join(t.TempDir(), "agent"), true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	if !plan.readyForActivation() {
		t.Fatalf("plan has activation blockers: %+v", plan.Blockers)
	}
}

func TestNativeLaunchEnvironmentWithSessionHomeOverridesHomeAndXDG(t *testing.T) {
	layout, err := newSessionHomeLayout(filepath.Join(t.TempDir(), "hazmat-home"), "session-123")
	if err != nil {
		t.Fatal(err)
	}
	base := nativeLaunchEnvironment{
		Shell:      "/bin/zsh",
		Path:       "/usr/bin:/bin",
		Home:       agentHome,
		TmpDir:     "/tmp/agent",
		CacheHome:  defaultAgentCacheHome,
		ConfigHome: defaultAgentConfigHome,
		DataHome:   defaultAgentDataHome,
	}

	env := nativeLaunchEnvironmentWithSessionHome(base, layout)
	pairs := nativeLaunchBaseEnvPairs(sessionConfig{ProjectDir: "/Users/dr/workspace/hazmat"}, env)
	values := map[string]string{}
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			values[key] = value
		}
	}

	for key, want := range map[string]string{
		"HOME":            layout.Home,
		"XDG_CACHE_HOME":  layout.CacheHome,
		"XDG_CONFIG_HOME": layout.ConfigHome,
		"XDG_DATA_HOME":   layout.DataHome,
		"PATH":            base.Path,
		"TMPDIR":          base.TmpDir,
	} {
		if values[key] != want {
			t.Fatalf("%s = %q, want %q", key, values[key], want)
		}
	}
}

func TestNativeLaunchBaseEnvPairsUsesSessionHomeRuntimePlan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	launchPlan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, false)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	runtimePlan, err := newSessionHomeRuntimePlan(launchPlan, persistentHome)
	if err != nil {
		t.Fatalf("newSessionHomeRuntimePlan: %v", err)
	}
	env := nativeLaunchEnvironment{
		Shell:      "/bin/zsh",
		Path:       "/usr/bin:/bin",
		Home:       agentHome,
		TmpDir:     "/tmp/agent",
		CacheHome:  defaultAgentCacheHome,
		ConfigHome: defaultAgentConfigHome,
		DataHome:   defaultAgentDataHome,
	}

	pairs := nativeLaunchBaseEnvPairs(sessionConfig{ProjectDir: "/Users/dr/workspace/hazmat", SessionHome: &runtimePlan}, env)
	values := map[string]string{}
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			values[key] = value
		}
	}

	for key, want := range map[string]string{
		"HOME":            launchPlan.Layout.Home,
		"XDG_CACHE_HOME":  launchPlan.Layout.CacheHome,
		"XDG_CONFIG_HOME": launchPlan.Layout.ConfigHome,
		"XDG_DATA_HOME":   launchPlan.Layout.DataHome,
	} {
		if values[key] != want {
			t.Fatalf("%s = %q, want %q", key, values[key], want)
		}
	}
}

func TestBuildNativeSessionPolicyUsesSessionHomeRuntimePlan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	launchPlan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, false)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	runtimePlan, err := newSessionHomeRuntimePlan(launchPlan, persistentHome)
	if err != nil {
		t.Fatalf("newSessionHomeRuntimePlan: %v", err)
	}

	policy, err := buildNativeSessionPolicy(sessionConfig{
		ProjectDir:    "/Users/dr/workspace/hazmat",
		NetworkMode:   sessionNetworkDefault,
		SessionHome:   &runtimePlan,
		HarnessID:     HarnessClaude,
		TempDir:       filepath.Join(root, "session-123", "tmp"),
		ReadDirs:      []string{"/opt/sdk"},
		WriteDirs:     []string{"/tmp/cache"},
		HarnessEnv:    map[string]string{},
		RepoSetup:     nil,
		ServiceAccess: nil,
	})
	if err != nil {
		t.Fatalf("buildNativeSessionPolicy: %v", err)
	}
	agentHomePolicy := policy.Contract.AgentHome
	if agentHomePolicy.Mode != containment.AgentHomeModeSessionLocal {
		t.Fatalf("AgentHome mode = %s", agentHomePolicy.Mode)
	}
	if agentHomePolicy.Path != launchPlan.Layout.Home || agentHomePolicy.PersistentPath != persistentHome {
		t.Fatalf("AgentHome = %+v", agentHomePolicy)
	}
	if !reflect.DeepEqual(agentHomePolicy.DurableBridgeRoots, runtimePlan.AgentHomePolicy.DurableBridgeRoots) {
		t.Fatalf("DurableBridgeRoots = %#v, want %#v", agentHomePolicy.DurableBridgeRoots, runtimePlan.AgentHomePolicy.DurableBridgeRoots)
	}
}

func TestRenderSessionContractShowsExperimentalSessionHome(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	launchPlan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, false)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	runtimePlan, err := newSessionHomeRuntimePlan(launchPlan, persistentHome)
	if err != nil {
		t.Fatalf("newSessionHomeRuntimePlan: %v", err)
	}

	got := renderSessionContract(sessionConfig{
		ProjectDir:   "/Users/dr/workspace/hazmat",
		SessionHome:  &runtimePlan,
		NetworkMode:  sessionNetworkDefault,
		SessionNotes: []string{"session-home preview"},
	}, sessionModeNative, true)
	for _, want := range []string{
		"Session HOME:         " + launchPlan.Layout.Home + " (experimental preview)",
		"Persistent HOME:      " + persistentHome,
		"session-home preview",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderSessionContract missing %q in:\n%s", want, got)
		}
	}
}

func TestNewSessionHomeLaunchPlanIncludesDurableBridgeRequirements(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	byRel := map[string]sessionHomeBridgeRequirement{}
	for _, requirement := range plan.BridgeRequirements {
		byRel[requirement.RelPath] = requirement
	}

	claude := byRel[".claude/projects"]
	if claude.Kind != sessionHomeBridgeHomeRelativeRoot {
		t.Fatalf("Claude bridge kind = %s", claude.Kind)
	}
	if claude.PersistentRoot != filepath.Join(persistentHome, ".claude", "projects") {
		t.Fatalf("Claude persistent root = %s", claude.PersistentRoot)
	}
	if claude.RuntimeRoot != filepath.Join(root, "session-123", "home", ".claude", "projects") {
		t.Fatalf("Claude runtime root = %s", claude.RuntimeRoot)
	}
	if claude.EnvVar != "" || claude.ProjectScoped {
		t.Fatalf("Claude bridge = %+v, want no env var and not project scoped", claude)
	}

	hermes := byRel[".hazmat/hermes/projects"]
	if hermes.Kind != sessionHomeBridgeHarnessEnvRoot {
		t.Fatalf("Hermes bridge kind = %s", hermes.Kind)
	}
	if hermes.EnvVar != "HERMES_HOME" || !hermes.ProjectScoped {
		t.Fatalf("Hermes bridge = %+v, want HERMES_HOME project-scoped env root", hermes)
	}
	if hermes.PersistentRoot != filepath.Join(persistentHome, ".hazmat", "hermes", "projects") {
		t.Fatalf("Hermes persistent root = %s", hermes.PersistentRoot)
	}
	if hermes.RuntimeRoot != filepath.Join(root, "session-123", "home", ".hazmat", "hermes", "projects") {
		t.Fatalf("Hermes runtime root = %s", hermes.RuntimeRoot)
	}
}

func TestMaterializeSessionHomeBridgesLinksClaudeAndEnsuresHermesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	if err := materializeSessionHomeBridges(plan.Layout, plan.BridgeRequirements); err != nil {
		t.Fatalf("materializeSessionHomeBridges: %v", err)
	}

	claudeRuntime := filepath.Join(plan.Layout.Home, ".claude", "projects")
	claudePersistent := filepath.Join(persistentHome, ".claude", "projects")
	target, err := os.Readlink(claudeRuntime)
	if err != nil {
		t.Fatalf("read Claude bridge: %v", err)
	}
	if target != claudePersistent {
		t.Fatalf("Claude bridge target = %s, want %s", target, claudePersistent)
	}

	hermesPersistent := filepath.Join(persistentHome, ".hazmat", "hermes", "projects")
	info, err := os.Stat(hermesPersistent)
	if err != nil {
		t.Fatalf("stat Hermes persistent root: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("Hermes persistent root is not a directory: %s", hermesPersistent)
	}
	if _, err := os.Lstat(filepath.Join(plan.Layout.Home, ".hazmat", "hermes", "projects")); !os.IsNotExist(err) {
		t.Fatalf("Hermes env bridge should not create a runtime HOME path, err=%v", err)
	}
}

func TestMaterializeSessionHomeBridgesRejectsRuntimeEscape(t *testing.T) {
	layout, err := newSessionHomeLayout(filepath.Join(t.TempDir(), "hazmat-home"), "session-123")
	if err != nil {
		t.Fatal(err)
	}
	req := sessionHomeBridgeRequirement{
		RelPath:        ".claude/projects",
		Kind:           sessionHomeBridgeHomeRelativeRoot,
		PersistentRoot: filepath.Join(t.TempDir(), "agent", ".claude", "projects"),
		RuntimeRoot:    filepath.Join(layout.SessionDir, "outside"),
	}
	if err := materializeSessionHomeBridges(layout, []sessionHomeBridgeRequirement{req}); err == nil {
		t.Fatal("materializeSessionHomeBridges accepted a runtime bridge outside the session home")
	}
}

func TestMaterializeSessionHomeBridgesRejectsPersistentRootInsideSessionHome(t *testing.T) {
	layout, err := newSessionHomeLayout(filepath.Join(t.TempDir(), "hazmat-home"), "session-123")
	if err != nil {
		t.Fatal(err)
	}
	req := sessionHomeBridgeRequirement{
		RelPath:        ".claude/projects",
		Kind:           sessionHomeBridgeHomeRelativeRoot,
		PersistentRoot: filepath.Join(layout.Home, ".claude", "projects"),
		RuntimeRoot:    filepath.Join(layout.Home, ".claude", "projects"),
	}
	if err := materializeSessionHomeBridges(layout, []sessionHomeBridgeRequirement{req}); err == nil {
		t.Fatal("materializeSessionHomeBridges accepted a persistent root inside the session home")
	}
}

func TestSessionHomeAgentHomePolicyUsesSessionHomeAndBridgeRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	policy, err := sessionHomeAgentHomePolicy(plan, persistentHome)
	if err != nil {
		t.Fatalf("sessionHomeAgentHomePolicy: %v", err)
	}

	wantRoots := []string{
		filepath.Join(persistentHome, ".claude", "projects"),
		filepath.Join(persistentHome, ".hazmat", "hermes", "projects"),
	}
	if policy.Path != plan.Layout.Home || policy.Mode != containment.AgentHomeModeSessionLocal || policy.PersistentPath != persistentHome {
		t.Fatalf("policy = %+v", policy)
	}
	if !reflect.DeepEqual(policy.DurableBridgeRoots, wantRoots) {
		t.Fatalf("DurableBridgeRoots = %#v, want %#v", policy.DurableBridgeRoots, wantRoots)
	}
}

func TestSessionHomeAgentHomePolicyRejectsBridgeOutsidePersistentHome(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	plan.BridgeRequirements[0].PersistentRoot = filepath.Join(t.TempDir(), "outside")

	if _, err := sessionHomeAgentHomePolicy(plan, persistentHome); err == nil {
		t.Fatal("sessionHomeAgentHomePolicy accepted a bridge root outside the persistent home")
	}
}

func TestSessionHomeBridgeRequirementsRejectUnknownExternalDurablePath(t *testing.T) {
	_, err := sessionHomeBridgeRequirements([]sessionHomeAssemblyEntry{
		{
			RelPath:        ".unknown/transcripts",
			Durability:     sessionHomeDurableExternal,
			PersistentPath: "/Users/agent/.unknown/transcripts",
			RuntimePath:    "/private/tmp/hazmat-home/session-123/home/.unknown/transcripts",
			RequiresBridge: true,
		},
	})
	if err == nil {
		t.Fatal("sessionHomeBridgeRequirements accepted an unknown durable external bridge")
	}
}

func TestCleanupStaleSessionHomesRemovesOnlyMarkedOldHomes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

	oldLayout, err := newSessionHomeLayout(root, "old-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := createSessionHomeLayout(oldLayout); err != nil {
		t.Fatal(err)
	}
	oldTime := now.Add(-48 * time.Hour)
	if err := os.Chtimes(oldLayout.MarkerPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	freshLayout, err := newSessionHomeLayout(root, "fresh-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := createSessionHomeLayout(freshLayout); err != nil {
		t.Fatal(err)
	}

	unmarked := filepath.Join(root, "unmarked")
	if err := os.MkdirAll(filepath.Join(unmarked, "home"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(unmarked, filepath.Join(root, "linked-session")); err != nil {
		t.Fatal(err)
	}

	removed, err := cleanupStaleSessionHomes(root, now, 24*time.Hour)
	if err != nil {
		t.Fatalf("cleanupStaleSessionHomes: %v", err)
	}
	if !reflect.DeepEqual(removed, []string{oldLayout.SessionDir}) {
		t.Fatalf("removed = %#v, want %#v", removed, []string{oldLayout.SessionDir})
	}
	for _, path := range []string{freshLayout.SessionDir, unmarked, filepath.Join(root, "linked-session")} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("expected %s to remain: %v", path, err)
		}
	}
	if _, err := os.Stat(oldLayout.SessionDir); !os.IsNotExist(err) {
		t.Fatalf("old session dir still exists or unexpected error: %v", err)
	}
}

func TestCleanupStaleSessionHomesRejectsUnsafeInputs(t *testing.T) {
	if _, err := cleanupStaleSessionHomes("relative", time.Now(), time.Hour); err == nil {
		t.Fatal("cleanupStaleSessionHomes accepted relative root")
	}
	if _, err := cleanupStaleSessionHomes(filepath.Join(t.TempDir(), "root"), time.Now(), 0); err == nil {
		t.Fatal("cleanupStaleSessionHomes accepted zero max age")
	}
}
