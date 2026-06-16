package hazmat

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

func TestExperimentalSessionHomeModeFromEnv(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  experimentalSessionHomeMode
	}{
		{"", experimentalSessionHomeDisabled},
		{"1", experimentalSessionHomePreview},
		{"preview", experimentalSessionHomePreview},
		{"plan-only", experimentalSessionHomePreview},
		{"activate", experimentalSessionHomeActivate},
		{"launch", experimentalSessionHomeActivate},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv(experimentalSessionHomeEnv, tc.value)
			if got := experimentalSessionHomeModeFromEnv(); got != tc.want {
				t.Fatalf("experimentalSessionHomeModeFromEnv() = %q, want %q", got, tc.want)
			}
		})
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
	if cfg.SessionHome.Launch.readyForActivation() {
		t.Fatal("experimental session-home preview is activation-ready before durable mirror sync exists")
	}
	if len(cfg.SessionNotes) < 2 || !strings.Contains(cfg.SessionNotes[0], "Experimental session-local HOME preview") {
		t.Fatalf("SessionNotes = %v", cfg.SessionNotes)
	}
}

func TestApplyExperimentalSessionHomePlanActivateMaterializesWhenReady(t *testing.T) {
	t.Setenv(experimentalSessionHomeEnv, "activate")
	savedNewSessionHomeID := newSessionHomeID
	newSessionHomeID = func() string { return "session-123" }
	t.Cleanup(func() { newSessionHomeID = savedNewSessionHomeID })
	savedExists := sessionHomeActivationPersistentPathExists
	sessionHomeActivationPersistentPathExists = func(string) (bool, error) { return false, nil }
	t.Cleanup(func() { sessionHomeActivationPersistentPathExists = savedExists })
	materialized := false
	savedMaterialize := materializeSessionHomeLaunchPlanForActivation
	materializeSessionHomeLaunchPlanForActivation = func(plan sessionHomeLaunchPlan) (sessionHomeMaterializationResult, error) {
		materialized = true
		if plan.readyForActivation() {
			return sessionHomeMaterializationResult{}, nil
		}
		t.Fatalf("activation materializer received blockers: %+v", plan.Blockers)
		return sessionHomeMaterializationResult{}, nil
	}
	t.Cleanup(func() { materializeSessionHomeLaunchPlanForActivation = savedMaterialize })
	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/hazmat"}

	if err := applyExperimentalSessionHomePlan(&cfg, sessionModeNative, harnessSessionOpts{}); err != nil {
		t.Fatalf("applyExperimentalSessionHomePlan: %v", err)
	}
	if !materialized {
		t.Fatal("activation materializer was not called")
	}
	if cfg.SessionHome == nil || cfg.SessionHome.Launch.Layout.Home != filepath.Join(defaultSessionHomeRoot, "session-123", "home") {
		t.Fatalf("SessionHome = %+v", cfg.SessionHome)
	}
	if !cfg.SessionHome.Launch.readyForActivation() {
		t.Fatalf("activation plan still has blockers: %+v", cfg.SessionHome.Launch.Blockers)
	}
	if len(cfg.SessionNotes) < 1 || !strings.Contains(cfg.SessionNotes[0], "validation activation") {
		t.Fatalf("SessionNotes = %v", cfg.SessionNotes)
	}
}

func TestApplyExperimentalSessionHomePlanActivatePlanOnlyReportsReadinessWithoutMaterializing(t *testing.T) {
	t.Setenv(experimentalSessionHomeEnv, "activate")
	savedNewSessionHomeID := newSessionHomeID
	newSessionHomeID = func() string { return "session-123" }
	t.Cleanup(func() { newSessionHomeID = savedNewSessionHomeID })
	savedExists := sessionHomeActivationPersistentPathExists
	sessionHomeActivationPersistentPathExists = func(string) (bool, error) { return false, nil }
	t.Cleanup(func() { sessionHomeActivationPersistentPathExists = savedExists })
	savedMaterialize := materializeSessionHomeLaunchPlanForActivation
	materializeSessionHomeLaunchPlanForActivation = func(sessionHomeLaunchPlan) (sessionHomeMaterializationResult, error) {
		t.Fatal("plan-only activation explain should not materialize session home")
		return sessionHomeMaterializationResult{}, nil
	}
	t.Cleanup(func() { materializeSessionHomeLaunchPlanForActivation = savedMaterialize })
	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/hazmat"}

	if err := applyExperimentalSessionHomePlan(&cfg, sessionModeNative, harnessSessionOpts{planOnly: true}); err != nil {
		t.Fatalf("applyExperimentalSessionHomePlan: %v", err)
	}
	if cfg.SessionHome == nil {
		t.Fatal("SessionHome = nil, want activate-mode plan")
	}
	if !cfg.SessionHome.Launch.readyForActivation() {
		t.Fatalf("activate plan-only explain should report real readiness, blockers: %+v", cfg.SessionHome.Launch.Blockers)
	}
	if sessionHomeHasBlockerReason(cfg.SessionHome.Launch.Blockers, sessionHomeBlockerActivationGate) {
		t.Fatalf("activate plan-only explain should not include activation gate blocker: %+v", cfg.SessionHome.Launch.Blockers)
	}
	if len(cfg.SessionNotes) < 2 ||
		!strings.Contains(cfg.SessionNotes[0], "validation preview") ||
		!strings.Contains(cfg.SessionNotes[1], "without materializing") {
		t.Fatalf("SessionNotes = %v", cfg.SessionNotes)
	}
}

func TestApplyExperimentalSessionHomePlanActivatePlanOnlyReportsRealBlockers(t *testing.T) {
	t.Setenv(experimentalSessionHomeEnv, "activate")
	savedExists := sessionHomeActivationPersistentPathExists
	sessionHomeActivationPersistentPathExists = func(path string) (bool, error) {
		return strings.HasSuffix(path, filepath.Join(".config", "mcp")), nil
	}
	t.Cleanup(func() { sessionHomeActivationPersistentPathExists = savedExists })
	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/hazmat"}

	if err := applyExperimentalSessionHomePlan(&cfg, sessionModeNative, harnessSessionOpts{planOnly: true}); err != nil {
		t.Fatalf("applyExperimentalSessionHomePlan: %v", err)
	}
	if cfg.SessionHome == nil {
		t.Fatal("SessionHome = nil, want activate-mode plan")
	}
	if !sessionHomeHasBlockerReason(cfg.SessionHome.Launch.Blockers, sessionHomeBlockerAdapterRequired) {
		t.Fatalf("activate plan-only explain blockers = %+v, want adapter-required blocker", cfg.SessionHome.Launch.Blockers)
	}
	if sessionHomeHasBlockerReason(cfg.SessionHome.Launch.Blockers, sessionHomeBlockerActivationGate) {
		t.Fatalf("activate plan-only explain should not include activation gate blocker: %+v", cfg.SessionHome.Launch.Blockers)
	}
}

func TestApplyExperimentalSessionHomePlanActivateFailsClosedOnBlockers(t *testing.T) {
	t.Setenv(experimentalSessionHomeEnv, "activate")
	savedExists := sessionHomeActivationPersistentPathExists
	sessionHomeActivationPersistentPathExists = func(path string) (bool, error) {
		return strings.HasSuffix(path, filepath.Join(".config", "mcp")), nil
	}
	t.Cleanup(func() { sessionHomeActivationPersistentPathExists = savedExists })
	savedMaterialize := materializeSessionHomeLaunchPlanForActivation
	materializeSessionHomeLaunchPlanForActivation = func(sessionHomeLaunchPlan) (sessionHomeMaterializationResult, error) {
		t.Fatal("activation materializer should not run when blockers remain")
		return sessionHomeMaterializationResult{}, nil
	}
	t.Cleanup(func() { materializeSessionHomeLaunchPlanForActivation = savedMaterialize })
	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/hazmat"}

	err := applyExperimentalSessionHomePlan(&cfg, sessionModeNative, harnessSessionOpts{})
	if err == nil || !strings.Contains(err.Error(), "adapter required") {
		t.Fatalf("applyExperimentalSessionHomePlan err = %v, want adapter blocker", err)
	}
	if !strings.Contains(err.Error(), "Blocking paths: adapter required: .config/mcp [harness-state/adapter-required; adapter=mcp-state:manual-only]") {
		t.Fatalf("applyExperimentalSessionHomePlan err = %v, want actionable blocker path", err)
	}
	if !strings.Contains(err.Error(), "not hazmat init") || !strings.Contains(err.Error(), "hazmat explain --json") {
		t.Fatalf("applyExperimentalSessionHomePlan err = %v, want actionable non-init guidance", err)
	}
	if cfg.SessionHome != nil {
		t.Fatalf("SessionHome = %+v, want nil on activation failure", cfg.SessionHome)
	}
}

func sessionHomeHasBlockerReason(blockers []sessionHomeLaunchBlocker, reason sessionHomeLaunchBlockerReason) bool {
	for _, blocker := range blockers {
		if blocker.Reason == reason {
			return true
		}
	}
	return false
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
	runtimeMarker, err := os.ReadFile(filepath.Join(layout.Home, sessionHomeMarkerFile))
	if err != nil {
		t.Fatalf("read runtime marker: %v", err)
	}
	if !strings.Contains(string(runtimeMarker), "hazmat session home") {
		t.Fatalf("runtime marker = %q", runtimeMarker)
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
		rel            string
		kind           containment.AgentHomeStateKind
		class          containment.AgentHomeStateClass
		durability     sessionHomeAssemblyDurability
		policy         sessionHomeRuntimePolicy
		adapter        sessionHomeAdapterName
		adapterOutcome sessionHomeAdapterOutcome
		executable     bool
		bridge         bool
		persistent     string
		runtimePath    string
	}{
		{
			rel:         ".claude/projects",
			kind:        containment.AgentHomeStateDir,
			class:       containment.AgentHomeStateTranscript,
			durability:  sessionHomeDurableExternal,
			policy:      sessionHomePolicyDurableExternal,
			bridge:      true,
			persistent:  filepath.Join(persistentHome, ".claude", "projects"),
			runtimePath: filepath.Join(layout.Home, ".claude", "projects"),
		},
		{
			rel:         ".hazmat/hermes/projects",
			kind:        containment.AgentHomeStateDir,
			class:       containment.AgentHomeStateTranscript,
			durability:  sessionHomeDurableExternal,
			policy:      sessionHomePolicyDurableExternal,
			bridge:      true,
			persistent:  filepath.Join(persistentHome, ".hazmat", "hermes", "projects"),
			runtimePath: filepath.Join(layout.Home, ".hazmat", "hermes", "projects"),
		},
		{
			rel:            ".cargo",
			kind:           containment.AgentHomeStateDir,
			class:          containment.AgentHomeStateToolchainState,
			durability:     sessionHomeDurableMirror,
			policy:         sessionHomePolicyAdapterRequired,
			adapter:        sessionHomeAdapterToolchainCache,
			adapterOutcome: sessionHomeAdapterIgnoredEphemeral,
		},
		{
			rel:            ".local/bin",
			kind:           containment.AgentHomeStateDir,
			class:          containment.AgentHomeStateExecutable,
			durability:     sessionHomeDurableMirror,
			policy:         sessionHomePolicyAdapterRequired,
			adapter:        sessionHomeAdapterExecutableTooling,
			adapterOutcome: sessionHomeAdapterIgnoredEphemeral,
			executable:     true,
		},
		{
			rel:        ".gitconfig",
			kind:       containment.AgentHomeStateFile,
			class:      containment.AgentHomeStateGitConfig,
			durability: sessionHomeDurableMirror,
			policy:     sessionHomePolicySeedOnly,
		},
		{
			rel:        ".cache",
			kind:       containment.AgentHomeStateDir,
			class:      containment.AgentHomeStateXDGCache,
			durability: sessionHomeEphemeralCache,
			policy:     sessionHomePolicyEphemeralCache,
		},
		{
			rel:            ".config",
			kind:           containment.AgentHomeStateDir,
			class:          containment.AgentHomeStateXDGConfig,
			durability:     sessionHomeDurableMirror,
			policy:         sessionHomePolicyAdapterRequired,
			adapter:        sessionHomeAdapterXDGState,
			adapterOutcome: sessionHomeAdapterIgnoredEphemeral,
		},
		{
			rel:            ".codex",
			kind:           containment.AgentHomeStateDir,
			class:          containment.AgentHomeStateHarnessState,
			durability:     sessionHomeDurableMirror,
			policy:         sessionHomePolicyAdapterRequired,
			adapter:        sessionHomeAdapterCodexState,
			adapterOutcome: sessionHomeAdapterIgnoredEphemeral,
		},
		{
			rel:            ".config/mcp",
			kind:           containment.AgentHomeStateDir,
			class:          containment.AgentHomeStateHarnessState,
			durability:     sessionHomeDurableMirror,
			policy:         sessionHomePolicyAdapterRequired,
			adapter:        sessionHomeAdapterMCPState,
			adapterOutcome: sessionHomeAdapterManualOnly,
		},
		{
			rel:            ".config/opencode",
			kind:           containment.AgentHomeStateDir,
			class:          containment.AgentHomeStateHarnessState,
			durability:     sessionHomeDurableMirror,
			policy:         sessionHomePolicyAdapterRequired,
			adapter:        sessionHomeAdapterOpenCodeState,
			adapterOutcome: sessionHomeAdapterIgnoredEphemeral,
		},
		{
			rel:            ".local/share",
			kind:           containment.AgentHomeStateDir,
			class:          containment.AgentHomeStateXDGData,
			durability:     sessionHomeDurableMirror,
			policy:         sessionHomePolicyAdapterRequired,
			adapter:        sessionHomeAdapterXDGState,
			adapterOutcome: sessionHomeAdapterIgnoredEphemeral,
		},
		{
			rel:            ".npmrc",
			kind:           containment.AgentHomeStateFile,
			class:          containment.AgentHomeStateToolchainState,
			durability:     sessionHomeDurableMirror,
			policy:         sessionHomePolicyAdapterRequired,
			adapter:        sessionHomeAdapterToolchainCache,
			adapterOutcome: sessionHomeAdapterIgnoredEphemeral,
		},
	} {
		entry, ok := byRel[tc.rel]
		if !ok {
			t.Fatalf("assembly plan missing %s", tc.rel)
		}
		if entry.Kind != tc.kind ||
			entry.Class != tc.class ||
			entry.Durability != tc.durability ||
			entry.RuntimePolicy != tc.policy ||
			entry.AdapterName != tc.adapter ||
			entry.AdapterOutcome != tc.adapterOutcome ||
			entry.Executable != tc.executable ||
			entry.RequiresBridge != tc.bridge {
			t.Fatalf("%s = %+v, want kind=%s class=%s durability=%s policy=%s adapter=%s outcome=%s executable=%v bridge=%v", tc.rel, entry, tc.kind, tc.class, tc.durability, tc.policy, tc.adapter, tc.adapterOutcome, tc.executable, tc.bridge)
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

func TestNewSessionHomeLaunchPlanBlocksActivationOnGateAndExistingAdapterState(t *testing.T) {
	persistentHome := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(filepath.Join(persistentHome, ".config", "mcp"), 0o700); err != nil {
		t.Fatal(err)
	}
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	if plan.readyForActivation() {
		t.Fatal("plan is activation-ready before session-home activation gate is lifted")
	}
	foundGate := false
	foundAdapter := false
	for _, blocker := range plan.Blockers {
		switch {
		case blocker.RelPath == "session-home" && blocker.Reason == sessionHomeBlockerActivationGate:
			foundGate = true
		case blocker.RelPath == ".config/mcp" && blocker.Reason == sessionHomeBlockerAdapterRequired:
			foundAdapter = true
		case blocker.Reason == sessionHomeBlockerAdapterRequired || blocker.Reason == sessionHomeBlockerActivationGate:
		default:
			t.Fatalf("unexpected activation blocker: %+v", blocker)
		}
	}
	if !foundGate || !foundAdapter {
		t.Fatalf("activation blockers = %+v, want activation gate and adapter blocker", plan.Blockers)
	}
	if got := sessionHomeActivationBlockerSummary(plan.Blockers); strings.Contains(got, "seed materialization") || !strings.Contains(got, "activation gate") || !strings.Contains(got, "adapter required") {
		t.Fatalf("blocker summary = %q", got)
	}
}

func TestNewSessionHomeLaunchPlanIgnoresEphemeralExecutableTooling(t *testing.T) {
	persistentHome := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(filepath.Join(persistentHome, ".local", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	for _, blocker := range plan.Blockers {
		if blocker.RelPath == ".local/bin" {
			t.Fatalf("activation blockers = %+v, want executable tooling ignored until an explicit adapter connects it", plan.Blockers)
		}
	}
}

func TestNewSessionHomeLaunchPlanIgnoresEphemeralToolchainCacheBlockers(t *testing.T) {
	persistentHome := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(filepath.Join(persistentHome, ".cargo"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".npmrc", ".pypirc"} {
		if err := os.WriteFile(filepath.Join(persistentHome, rel), []byte("registry config\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	for _, blocker := range plan.Blockers {
		switch blocker.RelPath {
		case ".cargo", ".npmrc", ".pypirc":
			t.Fatalf("activation blockers = %+v, want %s ignored as ephemeral toolchain state", plan.Blockers, blocker.RelPath)
		}
	}
}

func TestNewSessionHomeLaunchPlanIgnoresEphemeralXDGRootBlockers(t *testing.T) {
	persistentHome := filepath.Join(t.TempDir(), "agent")
	for _, rel := range []string{".config", filepath.Join(".local", "share")} {
		if err := os.MkdirAll(filepath.Join(persistentHome, rel), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	for _, blocker := range plan.Blockers {
		if blocker.RelPath == ".config" || blocker.RelPath == ".local" || blocker.RelPath == ".local/share" {
			t.Fatalf("activation blockers = %+v, want broad XDG roots ignored as ephemeral session state", plan.Blockers)
		}
	}
}

func TestNewSessionHomeLaunchPlanKeepsCoveredHarnessXDGStateBlocked(t *testing.T) {
	persistentHome := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(filepath.Join(persistentHome, ".config", "mcp"), 0o700); err != nil {
		t.Fatal(err)
	}
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	for _, blocker := range plan.Blockers {
		if blocker.RelPath == ".config/mcp" &&
			blocker.AdapterName == sessionHomeAdapterMCPState &&
			blocker.AdapterOutcome == sessionHomeAdapterManualOnly {
			return
		}
	}
	t.Fatalf("activation blockers = %+v, want covered harness XDG state blocked", plan.Blockers)
}

func TestNewSessionHomeLaunchPlanIgnoresSupportedBroadHarnessRoots(t *testing.T) {
	persistentHome := filepath.Join(t.TempDir(), "agent")
	for _, rel := range []string{".agents", ".claude", ".codex", ".cursor", ".gemini", ".hazmat", ".hazmat/hermes", ".opencode", ".pi", ".qwen"} {
		if err := os.MkdirAll(filepath.Join(persistentHome, filepath.FromSlash(rel)), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	for _, blocker := range plan.Blockers {
		switch blocker.RelPath {
		case ".agents", ".claude", ".codex", ".cursor", ".gemini", ".hazmat", ".hazmat/hermes", ".opencode", ".pi", ".qwen":
			t.Fatalf("activation blockers = %+v, want supported broad harness root %s ignored as empty session-local state", plan.Blockers, blocker.RelPath)
		}
	}
}

func TestNewSessionHomeLaunchPlanKeepsMCPStateBlocked(t *testing.T) {
	persistentHome := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(filepath.Join(persistentHome, ".config", "mcp"), 0o700); err != nil {
		t.Fatalf("mkdir .config/mcp: %v", err)
	}
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	for _, blocker := range plan.Blockers {
		if blocker.RelPath == ".config/mcp" &&
			blocker.AdapterName == sessionHomeAdapterMCPState &&
			blocker.AdapterOutcome == sessionHomeAdapterManualOnly {
			return
		}
	}
	t.Fatalf("activation blockers = %+v, missing MCP manual-only blocker", plan.Blockers)
}

func TestNewSessionHomeLaunchPlanIgnoresOpenCodeConfigState(t *testing.T) {
	persistentHome := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(filepath.Join(persistentHome, ".config", "opencode"), 0o700); err != nil {
		t.Fatalf("mkdir .config/opencode: %v", err)
	}
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}

	for _, blocker := range plan.Blockers {
		if blocker.RelPath == ".config/opencode" {
			t.Fatalf("activation blockers = %+v, want OpenCode config ignored as empty session-local state", plan.Blockers)
		}
	}
}

func TestNewSessionHomeLaunchPlanDoesNotReportMissingAdapterState(t *testing.T) {
	plan, err := newSessionHomeLaunchPlan(filepath.Join(t.TempDir(), "hazmat-home"), "session-123", filepath.Join(t.TempDir(), "agent"), true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	if plan.readyForActivation() {
		t.Fatal("plan is activation-ready before session-home activation gate is lifted")
	}
	if len(plan.Blockers) != 1 || plan.Blockers[0].Reason != sessionHomeBlockerActivationGate {
		t.Fatalf("activation blockers = %+v, want only activation gate for absent adapter state", plan.Blockers)
	}
	if got := sessionHomeActivationBlockerSummary(plan.Blockers); got != "activation gate" {
		t.Fatalf("blocker summary = %q", got)
	}
}

func TestSessionHomeActivationBlockerDetailsGroupsPaths(t *testing.T) {
	blockers := []sessionHomeLaunchBlocker{
		{
			RelPath:        ".npm",
			Reason:         sessionHomeBlockerAdapterRequired,
			Class:          containment.AgentHomeStateToolchainState,
			RuntimePolicy:  sessionHomePolicyAdapterRequired,
			AdapterName:    sessionHomeAdapterToolchainCache,
			AdapterOutcome: sessionHomeAdapterUnsupported,
		},
		{
			RelPath:        ".cargo",
			Reason:         sessionHomeBlockerAdapterRequired,
			Class:          containment.AgentHomeStateToolchainState,
			RuntimePolicy:  sessionHomePolicyAdapterRequired,
			AdapterName:    sessionHomeAdapterToolchainCache,
			AdapterOutcome: sessionHomeAdapterUnsupported,
		},
		{RelPath: "session-home", Reason: sessionHomeBlockerActivationGate},
	}

	got := sessionHomeActivationBlockerDetails(blockers)
	for _, want := range []string{
		"Blocking paths:",
		"activation gate: session-home",
		"adapter required (2 paths): .cargo [toolchain-state/adapter-required; adapter=toolchain-cache:unsupported], .npm [toolchain-state/adapter-required; adapter=toolchain-cache:unsupported]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("details = %q, want %q", got, want)
		}
	}
}

func TestSessionHomeActivationBlockerGuidanceIsReasonAware(t *testing.T) {
	tests := []struct {
		name     string
		blockers []sessionHomeLaunchBlocker
		want     []string
		notWant  []string
	}{
		{
			name: "activation gate only",
			blockers: []sessionHomeLaunchBlocker{
				{RelPath: "session-home", Reason: sessionHomeBlockerActivationGate},
			},
			want: []string{
				"activation-gate blockers mean the current mode is preview-only",
				"use activate mode only after the structured plan is ready",
				"not hazmat init",
			},
			notWant: []string{
				"adapter-required paths need typed bridge",
			},
		},
		{
			name: "adapter required",
			blockers: []sessionHomeLaunchBlocker{
				{RelPath: ".codex", Reason: sessionHomeBlockerAdapterRequired},
			},
			want: []string{
				"adapter-required paths need typed bridge/materializer support",
				"not hazmat init",
			},
		},
		{
			name: "mixed mirror and writeback",
			blockers: []sessionHomeLaunchBlocker{
				{RelPath: ".gitconfig", Reason: sessionHomeBlockerSeedMaterialize},
				{RelPath: ".cache", Reason: sessionHomeBlockerDurableMirrorSync},
				{RelPath: ".state", Reason: sessionHomeBlockerCheckedWriteback},
			},
			want: []string{
				"seed materialization blockers need implemented seed-copy rules",
				"durable mirror sync blockers need mirror materialization and cleanup semantics",
				"checked writeback blockers need a verified adapter writeback plan",
				"not hazmat init",
			},
			notWant: []string{
				"adapter-required paths need typed bridge",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sessionHomeActivationBlockerGuidance(tt.blockers)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("guidance = %q, want %q", got, want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Fatalf("guidance = %q, did not want %q", got, notWant)
				}
			}
		})
	}
}

func TestSessionHomePersistentPathExistsDoesNotProbeAgentHome(t *testing.T) {
	exists, err := sessionHomePersistentPathExists(filepath.Join(agentHome, ".definitely-private-for-session-home-test"))
	if err != nil {
		t.Fatalf("sessionHomePersistentPathExists returned error for agent-home path: %v", err)
	}
	if exists {
		t.Fatal("sessionHomePersistentPathExists reported an unprobed agent-home path as existing")
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

func TestNativeLaunchBaseEnvPairsUsesSessionHomeForEveryManagedHarness(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	launchPlan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	runtimePlan, err := newSessionHomeRuntimePlan(launchPlan, persistentHome)
	if err != nil {
		t.Fatalf("newSessionHomeRuntimePlan: %v", err)
	}

	for _, harness := range managedHarnessRegistry {
		t.Run(string(harness.Spec.ID), func(t *testing.T) {
			pairs := agentEnvPairsWithPlan(sessionConfig{
				ProjectDir:  "/Users/dr/workspace/hazmat",
				HarnessID:   harness.Spec.ID,
				SessionHome: &runtimePlan,
			}, sessionBackendPlan{})
			values := envPairsMap(pairs)

			for key, want := range map[string]string{
				"HOME":            launchPlan.Layout.Home,
				"XDG_CACHE_HOME":  launchPlan.Layout.CacheHome,
				"XDG_CONFIG_HOME": launchPlan.Layout.ConfigHome,
				"XDG_DATA_HOME":   launchPlan.Layout.DataHome,
				"USER":            agentUser,
				"LOGNAME":         agentUser,
			} {
				if values[key] != want {
					t.Fatalf("%s = %q, want %q in env pairs %#v", key, values[key], want, pairs)
				}
			}
		})
	}
}

func envPairsMap(pairs []string) map[string]string {
	values := map[string]string{}
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			values[key] = value
		}
	}
	return values
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
	agentHomePolicy := policy.AgentHome
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
		"Session HOME status:  blocked: activation gate",
		"Session HOME blockers: activation gate: session-home",
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

func TestMaterializeSessionHomeLaunchPlanAssemblesSupportedRuntimePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	writePersistent := func(rel, content string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(persistentHome, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	writePersistent(".zshrc", "export HAZMAT_TEST=1\n", 0o640)
	writePersistent(".local/bin/tool", "#!/bin/sh\n", 0o700)

	result, err := materializeSessionHomeLaunchPlan(plan)
	if err != nil {
		t.Fatalf("materializeSessionHomeLaunchPlan: %v", err)
	}
	if len(result.CheckedWritebackReceipts) != 0 {
		t.Fatalf("CheckedWritebackReceipts = %+v, want none for current manifest", result.CheckedWritebackReceipts)
	}
	if _, err := os.Stat(plan.Layout.MarkerPath); err != nil {
		t.Fatalf("stat marker: %v", err)
	}
	copiedShell, err := os.ReadFile(filepath.Join(plan.Layout.Home, ".zshrc"))
	if err != nil {
		t.Fatalf("read copied shell config: %v", err)
	}
	if string(copiedShell) != "export HAZMAT_TEST=1\n" {
		t.Fatalf("copied shell config = %q", copiedShell)
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
	if _, err := os.Stat(filepath.Join(persistentHome, ".hazmat", "hermes", "projects")); err != nil {
		t.Fatalf("stat Hermes persistent root: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(plan.Layout.Home, ".local", "bin", "tool")); !os.IsNotExist(err) {
		t.Fatalf(".local/bin/tool should not be seed-copied without an explicit executable adapter, err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(plan.Layout.Home, ".cache", "package-cache")); !os.IsNotExist(err) {
		t.Fatalf(".cache/package-cache should not be seed-copied, err=%v", err)
	}
}

func TestMaterializeSessionHomeLaunchPlanReturnsCheckedWritebackReceipts(t *testing.T) {
	layout, err := newSessionHomeLayout(filepath.Join(t.TempDir(), "hazmat-home"), "session-123")
	if err != nil {
		t.Fatal(err)
	}
	persistentHome := filepath.Join(t.TempDir(), "agent")
	checked := sessionHomeAssemblyEntry{
		RelPath:        ".config/tool/state.json",
		RuntimePolicy:  sessionHomePolicyCheckedWriteback,
		PersistentPath: filepath.Join(persistentHome, ".config", "tool", "state.json"),
		RuntimePath:    filepath.Join(layout.Home, ".config", "tool", "state.json"),
	}
	if err := os.MkdirAll(filepath.Dir(checked.PersistentPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checked.PersistentPath, []byte("state\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := materializeSessionHomeLaunchPlan(sessionHomeLaunchPlan{
		Layout:   layout,
		Assembly: []sessionHomeAssemblyEntry{checked},
	})
	if err != nil {
		t.Fatalf("materializeSessionHomeLaunchPlan: %v", err)
	}
	if len(result.CheckedWritebackReceipts) != 1 || result.CheckedWritebackReceipts[0].Outcome != sessionHomeWritebackCopiedIn {
		t.Fatalf("CheckedWritebackReceipts = %+v", result.CheckedWritebackReceipts)
	}
	got, err := os.ReadFile(checked.RuntimePath)
	if err != nil {
		t.Fatalf("read checked runtime path: %v", err)
	}
	if string(got) != "state\n" {
		t.Fatalf("checked runtime path = %q", got)
	}
}

func TestMaterializeSessionHomeLaunchPlanForNativeActivationUsesAgentHooks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeLaunchPlanWithBlockerInspector(root, "session-123", persistentHome, true, func(string) (bool, error) {
		return false, nil
	}, false)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlanWithBlockerInspector: %v", err)
	}

	var ensured []string
	var written [][3]string
	var copied [][2]string
	var linked [][2]string
	savedEnsure := sessionHomeAgentEnsureDir
	sessionHomeAgentEnsureDir = func(path string, mode os.FileMode) error {
		ensured = append(ensured, fmt.Sprintf("%s:%04o", path, mode))
		return nil
	}
	t.Cleanup(func() { sessionHomeAgentEnsureDir = savedEnsure })
	savedWrite := sessionHomeAgentWriteFile
	sessionHomeAgentWriteFile = func(path string, content []byte, mode os.FileMode) error {
		written = append(written, [3]string{path, string(content), fmt.Sprintf("%04o", mode)})
		return nil
	}
	t.Cleanup(func() { sessionHomeAgentWriteFile = savedWrite })
	savedCopy := sessionHomeAgentCopyPath
	sessionHomeAgentCopyPath = func(src, dest string) error {
		copied = append(copied, [2]string{src, dest})
		return nil
	}
	t.Cleanup(func() { sessionHomeAgentCopyPath = savedCopy })
	savedSymlink := sessionHomeAgentSymlink
	sessionHomeAgentSymlink = func(target, link string) error {
		linked = append(linked, [2]string{target, link})
		return nil
	}
	t.Cleanup(func() { sessionHomeAgentSymlink = savedSymlink })

	result, err := materializeSessionHomeLaunchPlanForNativeActivation(plan)
	if err != nil {
		t.Fatalf("materializeSessionHomeLaunchPlanForNativeActivation: %v", err)
	}
	if len(result.CheckedWritebackReceipts) != 0 {
		t.Fatalf("CheckedWritebackReceipts = %+v, want none", result.CheckedWritebackReceipts)
	}
	rootInfo, err := os.Stat(plan.Layout.Root)
	if err != nil {
		t.Fatalf("stat root: %v", err)
	}
	if rootInfo.Mode().Perm() != 0o733 || rootInfo.Mode()&os.ModeSticky == 0 {
		t.Fatalf("root mode = %s, want sticky 0733", rootInfo.Mode())
	}
	if !slices.Contains(written, [3]string{
		plan.Layout.MarkerPath,
		"hazmat session home\n",
		"0600",
	}) {
		t.Fatalf("written files = %#v, missing agent-owned marker", written)
	}
	if !slices.Contains(written, [3]string{
		filepath.Join(plan.Layout.Home, sessionHomeMarkerFile),
		"hazmat session home\n",
		"0600",
	}) {
		t.Fatalf("written files = %#v, missing runtime HOME marker", written)
	}
	for _, want := range []string{
		fmt.Sprintf("%s:%04o", plan.Layout.SessionDir, 0o711),
		fmt.Sprintf("%s:%04o", plan.Layout.Home, 0o700),
		fmt.Sprintf("%s:%04o", plan.Layout.CacheHome, 0o700),
		fmt.Sprintf("%s:%04o", filepath.Join(persistentHome, ".claude", "projects"), 0o700),
		fmt.Sprintf("%s:%04o", filepath.Join(persistentHome, ".hazmat", "hermes", "projects"), 0o700),
	} {
		if !slices.Contains(ensured, want) {
			t.Fatalf("ensured dirs = %#v, missing %s", ensured, want)
		}
	}
	if !slices.Contains(copied, [2]string{
		filepath.Join(persistentHome, ".zshrc"),
		filepath.Join(plan.Layout.Home, ".zshrc"),
	}) {
		t.Fatalf("copied paths = %#v, missing .zshrc seed copy", copied)
	}
	if !slices.Contains(linked, [2]string{
		filepath.Join(persistentHome, ".claude", "projects"),
		filepath.Join(plan.Layout.Home, ".claude", "projects"),
	}) {
		t.Fatalf("linked paths = %#v, missing Claude bridge", linked)
	}
}

func TestMaterializeSessionHomeLaunchPlanForNativeActivationRejectsCheckedWriteback(t *testing.T) {
	layout, err := newSessionHomeLayout(filepath.Join(t.TempDir(), "hazmat-home"), "session-123")
	if err != nil {
		t.Fatal(err)
	}
	savedEnsure := sessionHomeAgentEnsureDir
	sessionHomeAgentEnsureDir = func(string, os.FileMode) error { return nil }
	t.Cleanup(func() { sessionHomeAgentEnsureDir = savedEnsure })
	savedWrite := sessionHomeAgentWriteFile
	sessionHomeAgentWriteFile = func(string, []byte, os.FileMode) error { return nil }
	t.Cleanup(func() { sessionHomeAgentWriteFile = savedWrite })
	checked := sessionHomeAssemblyEntry{
		RelPath:        ".config/tool/state.json",
		RuntimePolicy:  sessionHomePolicyCheckedWriteback,
		PersistentPath: filepath.Join(t.TempDir(), "agent", ".config", "tool", "state.json"),
		RuntimePath:    filepath.Join(layout.Home, ".config", "tool", "state.json"),
	}
	_, err = materializeSessionHomeLaunchPlanForNativeActivation(sessionHomeLaunchPlan{
		Layout:   layout,
		Assembly: []sessionHomeAssemblyEntry{checked},
	})
	if err == nil || !strings.Contains(err.Error(), "agent-backed receipt materializer") {
		t.Fatalf("materializeSessionHomeLaunchPlanForNativeActivation err = %v, want checked-writeback rejection", err)
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

func TestMaterializeSessionHomeSeedEntriesCopiesOnlySeedPolicyPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	if err := createSessionHomeLayout(plan.Layout); err != nil {
		t.Fatalf("createSessionHomeLayout: %v", err)
	}

	writeFile := func(rel, content string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(persistentHome, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	writeFile(".zshrc", "export HAZMAT_TEST=1\n", 0o640)
	writeFile(".gitconfig", "[user]\n\tname = Agent\n", 0o600)
	writeFile(".config/git/config", "[safe]\n\tdirectory = /workspace/*\n", 0o600)
	writeFile(".claude/commands/review.md", "review command\n", 0o644)
	if err := os.Chmod(filepath.Join(persistentHome, ".claude", "commands"), 0o755); err != nil {
		t.Fatalf("chmod persistent commands dir: %v", err)
	}
	writeFile(".cache/package-cache", "cache\n", 0o600)

	if err := materializeSessionHomeSeedEntries(plan.Layout, plan.Assembly); err != nil {
		t.Fatalf("materializeSessionHomeSeedEntries: %v", err)
	}

	for rel, want := range map[string]string{
		".zshrc":                     "export HAZMAT_TEST=1\n",
		".gitconfig":                 "[user]\n\tname = Agent\n",
		".config/git/config":         "[safe]\n\tdirectory = /workspace/*\n",
		".claude/commands/review.md": "review command\n",
	} {
		got, err := os.ReadFile(filepath.Join(plan.Layout.Home, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read copied %s: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
	}
	if info, err := os.Stat(filepath.Join(plan.Layout.Home, ".zshrc")); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("copied .zshrc mode err=%v info=%v", err, info)
	}
	if info, err := os.Stat(filepath.Join(plan.Layout.Home, ".claude")); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("created .claude parent mode err=%v info=%v", err, info)
	}
	if info, err := os.Stat(filepath.Join(plan.Layout.Home, ".claude", "commands")); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("copied commands dir mode err=%v info=%v", err, info)
	}
	for _, rel := range []string{".cache/package-cache"} {
		if _, err := os.Lstat(filepath.Join(plan.Layout.Home, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("%s should not be seed-copied, err=%v", rel, err)
		}
	}
}

func TestMaterializeSessionHomeSeedEntriesRejectsSeedSymlinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	if err := createSessionHomeLayout(plan.Layout); err != nil {
		t.Fatalf("createSessionHomeLayout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(persistentHome, ".claude", "commands"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(persistentHome, ".zshrc.real"), []byte("source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(persistentHome, ".zshrc.real"), filepath.Join(persistentHome, ".zshrc")); err != nil {
		t.Fatal(err)
	}

	err = materializeSessionHomeSeedEntries(plan.Layout, plan.Assembly)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("materializeSessionHomeSeedEntries err = %v, want symlink rejection", err)
	}
}

func TestMaterializeSessionHomeSeedEntriesIgnoresExecutableSymlinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	if err := createSessionHomeLayout(plan.Layout); err != nil {
		t.Fatalf("createSessionHomeLayout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(persistentHome, ".local"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(persistentHome, "tool-real"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(persistentHome, "tool-real"), filepath.Join(persistentHome, ".local", "bin")); err != nil {
		t.Fatal(err)
	}

	if err := materializeSessionHomeSeedEntries(plan.Layout, plan.Assembly); err != nil {
		t.Fatalf("materializeSessionHomeSeedEntries: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(plan.Layout.Home, ".local", "bin")); !os.IsNotExist(err) {
		t.Fatalf(".local/bin symlink should not be imported without an explicit executable adapter, err=%v", err)
	}
}

func TestMaterializeSessionHomeSeedEntriesRejectsNestedSeedSymlinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hazmat-home")
	persistentHome := filepath.Join(t.TempDir(), "agent")
	plan, err := newSessionHomeLaunchPlan(root, "session-123", persistentHome, true)
	if err != nil {
		t.Fatalf("newSessionHomeLaunchPlan: %v", err)
	}
	if err := createSessionHomeLayout(plan.Layout); err != nil {
		t.Fatalf("createSessionHomeLayout: %v", err)
	}
	commands := filepath.Join(persistentHome, ".claude", "commands")
	if err := os.MkdirAll(commands, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(persistentHome, "outside.md"), []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(persistentHome, "outside.md"), filepath.Join(commands, "link.md")); err != nil {
		t.Fatal(err)
	}

	err = materializeSessionHomeSeedEntries(plan.Layout, plan.Assembly)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("materializeSessionHomeSeedEntries err = %v, want nested symlink rejection", err)
	}
	if _, err := os.Lstat(filepath.Join(plan.Layout.Home, ".claude", "commands", "link.md")); !os.IsNotExist(err) {
		t.Fatalf("nested symlink destination should not exist, err=%v", err)
	}
}

func TestMaterializeSessionHomeSeedEntriesRejectsRuntimeEscape(t *testing.T) {
	layout, err := newSessionHomeLayout(filepath.Join(t.TempDir(), "hazmat-home"), "session-123")
	if err != nil {
		t.Fatal(err)
	}
	entry := sessionHomeAssemblyEntry{
		RelPath:        ".zshrc",
		RuntimePolicy:  sessionHomePolicySeedOnly,
		PersistentPath: filepath.Join(t.TempDir(), "agent", ".zshrc"),
		RuntimePath:    filepath.Join(layout.SessionDir, "outside", ".zshrc"),
	}
	if err := materializeSessionHomeSeedEntries(layout, []sessionHomeAssemblyEntry{entry}); err == nil {
		t.Fatal("materializeSessionHomeSeedEntries accepted runtime path outside session home")
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
	persistentBridge := filepath.Join(t.TempDir(), "agent", ".claude", "projects")
	if err := os.MkdirAll(persistentBridge, 0o700); err != nil {
		t.Fatal(err)
	}
	persistentBridgeFile := filepath.Join(persistentBridge, "session.jsonl")
	if err := os.WriteFile(persistentBridgeFile, []byte("durable transcript\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(oldLayout.Home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(persistentBridge, filepath.Join(oldLayout.Home, ".claude", "projects")); err != nil {
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
	if got, err := os.ReadFile(persistentBridgeFile); err != nil || string(got) != "durable transcript\n" {
		t.Fatalf("persistent bridge target was removed or changed: %q err=%v", got, err)
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
