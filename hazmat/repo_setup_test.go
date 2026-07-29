package hazmat

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFinalizePreparedRepoSetupRememberPersistsEffects(t *testing.T) {
	isolateConfig(t)
	t.Setenv("JAVA_HOME", "/tmp/jdk")

	projectDir, err := resolveDir(t.TempDir(), false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	safeEffects := []repoSetupEffect{
		{
			ID:      "ro:/tmp/toolchain",
			Class:   repoSetupEffectClassSafe,
			Kind:    repoSetupEffectReadOnly,
			Value:   "/tmp/toolchain",
			Sources: []string{"Suggested by project files (node)"},
		},
		{
			ID:      "exclude:build/",
			Class:   repoSetupEffectClassSafe,
			Kind:    repoSetupEffectSnapshotExclude,
			Value:   "build/",
			Sources: []string{"Suggested by project files (node)"},
		},
		{
			ID:            "env:JAVA_HOME",
			Class:         repoSetupEffectClassSafe,
			Kind:          repoSetupEffectEnvSelector,
			Value:         "JAVA_HOME",
			ResolvedValue: "/tmp/jdk",
			Sources:       []string{"Suggested by project files (node)"},
		},
	}

	restorePrompt := stubRepoSetupSafePrompt(t, func(state repoSetupState) (repoSetupPromptAction, error) {
		if repoSetupEffectKindsSummary(state.PendingSafe) != "1 read-only path, 1 snapshot exclude, 1 env selector" {
			t.Fatalf("pending safe summary = %q", repoSetupEffectKindsSummary(state.PendingSafe))
		}
		return repoSetupPromptRemember, nil
	})
	defer restorePrompt()
	restoreExplicit := stubRepoSetupExplicitPrompt(t, func(repoSetupState) (repoSetupPromptAction, error) {
		t.Fatal("explicit prompt should not be called")
		return "", nil
	})
	defer restoreExplicit()
	restoreTTY := stubTerminal(t, true)
	defer restoreTTY()

	prepared, err := finalizePreparedRepoSetup(preparedSession{
		Config: sessionConfig{
			ProjectDir: projectDir,
			RepoSetup: &repoSetupState{
				CandidateHash:      "sha256:candidate",
				PendingSafe:        safeEffects,
				currentSafe:        repoSetupStoredEffectsFromEffects(safeEffects),
				currentSafeEffects: safeEffects,
				record:             repoProfileRecord{ProjectDir: projectDir},
			},
		},
	}, true, true)
	if err != nil {
		t.Fatalf("finalizePreparedRepoSetup: %v", err)
	}

	if !reflect.DeepEqual(prepared.Config.ReadDirs, []string{"/tmp/toolchain"}) {
		t.Fatalf("ReadDirs = %v, want [/tmp/toolchain]", prepared.Config.ReadDirs)
	}
	if !reflect.DeepEqual(prepared.Config.AutoReadDirs, []string{"/tmp/toolchain"}) {
		t.Fatalf("AutoReadDirs = %v, want [/tmp/toolchain]", prepared.Config.AutoReadDirs)
	}
	if !reflect.DeepEqual(prepared.Config.BackupExcludes, []string{"build/"}) {
		t.Fatalf("BackupExcludes = %v, want [build/]", prepared.Config.BackupExcludes)
	}
	if got := prepared.Config.IntegrationEnv["JAVA_HOME"]; got != "/tmp/jdk" {
		t.Fatalf("IntegrationEnv[JAVA_HOME] = %q, want /tmp/jdk", got)
	}
	if summary := repoSetupSummary(prepared.Config.RepoSetup); summary != "remembered (1 read-only path, 1 snapshot exclude, 1 env selector)" {
		t.Fatalf("repoSetupSummary = %q", summary)
	}

	record, err := loadRepoProfileRecord(projectDir)
	if err != nil {
		t.Fatalf("loadRepoProfileRecord: %v", err)
	}
	if record.LastSeenHash != "sha256:candidate" {
		t.Fatalf("LastSeenHash = %q, want sha256:candidate", record.LastSeenHash)
	}
	if !reflect.DeepEqual(record.Remembered, repoSetupStoredEffects{
		ReadOnly:         []string{"/tmp/toolchain"},
		SnapshotExcludes: []string{"build/"},
		EnvSelectors:     []string{"JAVA_HOME"},
	}) {
		t.Fatalf("Remembered = %#v", record.Remembered)
	}
	if record.ApprovalHash == "" {
		t.Fatal("ApprovalHash = empty, want hash")
	}
}

func TestDefaultPromptRepoSetupSafeRequiresChoiceAndShowsEffects(t *testing.T) {
	restoreTTY := stubTerminal(t, true)
	defer restoreTTY()
	restoreStdin := stubStdinFile(t, "1\n")
	defer restoreStdin()

	var action repoSetupPromptAction
	var err error
	stderr := captureStderr(t, func() {
		action, err = defaultPromptRepoSetupSafe(repoSetupState{
			PendingSafe: []repoSetupEffect{
				{
					ID:      "ro:/tmp/toolchain",
					Class:   repoSetupEffectClassSafe,
					Kind:    repoSetupEffectReadOnly,
					Value:   "/tmp/toolchain",
					Sources: []string{"Generic repo heuristic (Makefile references \"tool\")"},
				},
			},
		})
	})
	if err != nil {
		t.Fatalf("defaultPromptRepoSetupSafe: %v", err)
	}
	if action != repoSetupPromptRemember {
		t.Fatalf("action = %q, want remember", action)
	}
	for _, want := range []string{"additional repo setup available", "read-only: /tmp/toolchain", "Generic repo heuristic"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want %q", stderr, want)
		}
	}
}

func TestDefaultPromptRepoSetupSafeDefaultsToKeepCurrent(t *testing.T) {
	restoreTTY := stubTerminal(t, true)
	defer restoreTTY()
	restoreStdin := stubStdinFile(t, "\n")
	defer restoreStdin()

	action, err := defaultPromptRepoSetupSafe(repoSetupState{
		PendingSafe: []repoSetupEffect{{
			ID: "ro:/tmp/toolchain", Class: repoSetupEffectClassSafe,
			Kind: repoSetupEffectReadOnly, Value: "/tmp/toolchain",
		}},
	})
	if err != nil {
		t.Fatalf("defaultPromptRepoSetupSafe: %v", err)
	}
	if action != repoSetupPromptKeepCurrent {
		t.Fatalf("action = %q, want keep-current", action)
	}
}

func TestRepoSetupStateDoesNotRepromptRememberedSafeEffects(t *testing.T) {
	isolateConfig(t)
	allowAllIntegrationExecutables(t)

	projectDir, err := resolveDir(t.TempDir(), false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "Makefile"), []byte("build:\n\tcustomtool build\n"), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}

	toolRoot := filepath.Join(t.TempDir(), "custom-tool-root")
	toolPath := writeExecutable(t, toolRoot, "customtool")
	canonicalToolRoot, err := canonicalizePath(toolRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(toolRoot): %v", err)
	}
	if err := saveRepoProfileRecord(repoProfileRecord{
		ProjectDir: projectDir,
		Remembered: repoSetupStoredEffects{ReadOnly: []string{
			canonicalToolRoot,
		}},
	}); err != nil {
		t.Fatalf("saveRepoProfileRecord: %v", err)
	}

	savedProbeFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{lookPaths: map[string]string{"customtool": toolPath}}
	}
	t.Cleanup(func() { integrationProbeFactory = savedProbeFactory })

	state, err := repoSetupStateForSession(sessionConfig{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("repoSetupStateForSession: %v", err)
	}
	if len(state.PendingSafe) != 0 {
		t.Fatalf("PendingSafe = %#v, want none for remembered safe effect", state.PendingSafe)
	}
	if len(state.AppliedSafe) != 1 || state.AppliedSafe[0].Value != canonicalToolRoot {
		t.Fatalf("AppliedSafe = %#v, want remembered %q", state.AppliedSafe, canonicalToolRoot)
	}
}

func TestApplyRepoSetupEffectsRejectsCredentialEnvSelector(t *testing.T) {
	var cfg sessionConfig
	err := applyRepoSetupEffects(&cfg, repoSetupStoredEffects{
		EnvSelectors: []string{"GH_TOKEN", "GITHUB_TOKEN"},
	}, repoSetupState{})
	if err == nil {
		t.Fatal("expected credential env selector to be rejected")
	}
	if !strings.Contains(err.Error(), "credential/capability-shaped") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.IntegrationEnv) != 0 {
		t.Fatalf("IntegrationEnv = %v, want empty", cfg.IntegrationEnv)
	}
}

func TestFinalizePreparedRepoSetupUseOnceDoesNotPersistRemembered(t *testing.T) {
	isolateConfig(t)

	projectDir, err := resolveDir(t.TempDir(), false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	safeEffects := []repoSetupEffect{
		{
			ID:      "ro:/tmp/toolchain",
			Class:   repoSetupEffectClassSafe,
			Kind:    repoSetupEffectReadOnly,
			Value:   "/tmp/toolchain",
			Sources: []string{"Suggested by project files (node)"},
		},
	}

	restorePrompt := stubRepoSetupSafePrompt(t, func(repoSetupState) (repoSetupPromptAction, error) {
		return repoSetupPromptUseOnce, nil
	})
	defer restorePrompt()
	restoreTTY := stubTerminal(t, true)
	defer restoreTTY()

	prepared, err := finalizePreparedRepoSetup(preparedSession{
		Config: sessionConfig{
			ProjectDir: projectDir,
			RepoSetup: &repoSetupState{
				PendingSafe:        safeEffects,
				currentSafe:        repoSetupStoredEffectsFromEffects(safeEffects),
				currentSafeEffects: safeEffects,
				record:             repoProfileRecord{ProjectDir: projectDir},
			},
		},
	}, true, true)
	if err != nil {
		t.Fatalf("finalizePreparedRepoSetup: %v", err)
	}

	if summary := repoSetupSummary(prepared.Config.RepoSetup); summary != "active for this launch (1 read-only path)" {
		t.Fatalf("repoSetupSummary = %q", summary)
	}

	record, err := loadRepoProfileRecord(projectDir)
	if err != nil {
		t.Fatalf("loadRepoProfileRecord: %v", err)
	}
	if !record.Remembered.empty() {
		t.Fatalf("Remembered = %#v, want empty", record.Remembered)
	}
}

func TestFinalizePreparedRepoSetupFailsForExplicitStepUpWithoutPrompt(t *testing.T) {
	isolateConfig(t)

	projectDir, err := resolveDir(t.TempDir(), false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	restoreTTY := stubTerminal(t, false)
	defer restoreTTY()

	_, err = finalizePreparedRepoSetup(preparedSession{
		Config: sessionConfig{
			ProjectDir: projectDir,
			RepoSetup: &repoSetupState{
				PendingExplicit: []repoSetupEffect{
					{
						ID:      "rw:/tmp/cache",
						Class:   repoSetupEffectClassExplicit,
						Kind:    repoSetupEffectWrite,
						Value:   "/tmp/cache",
						Sources: []string{"Learned from previous session denial"},
					},
				},
				currentExplicit: repoSetupStoredEffects{Write: []string{"/tmp/cache"}},
				record:          repoProfileRecord{ProjectDir: projectDir},
			},
		},
	}, true, true)
	if err == nil {
		t.Fatal("expected explicit step-up to fail in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "additional repo setup approval required") || !strings.Contains(err.Error(), "write path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRememberRepoSetupDenialsFeedsNextRunSuggestions(t *testing.T) {
	isolateConfig(t)

	projectDir, err := resolveDir(t.TempDir(), false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	savedLogShow := repoSetupLogShow
	repoSetupLogShow = func(start, end time.Time) (string, error) {
		return "Sandbox: deny(1) file-read-data " + filepath.Join(homeDir, ".gradle", "caches", "modules-2", "metadata.bin"), nil
	}
	t.Cleanup(func() { repoSetupLogShow = savedLogShow })

	start := time.Now().UTC().Add(-time.Minute)
	end := start.Add(2 * time.Second)
	if err := rememberRepoSetupDenials(sessionConfig{ProjectDir: projectDir}, start, end); err != nil {
		t.Fatalf("rememberRepoSetupDenials: %v", err)
	}

	record, err := loadRepoProfileRecord(projectDir)
	if err != nil {
		t.Fatalf("loadRepoProfileRecord: %v", err)
	}
	wantEvidence := repoSetupStoredEvidence{
		ID:          "ro:" + filepath.Join(homeDir, ".gradle"),
		Class:       repoSetupEffectClassSafe,
		Kind:        repoSetupEffectReadOnly,
		Value:       filepath.Join(homeDir, ".gradle"),
		Source:      "Learned from previous session denial",
		FirstSeenAt: end.Add(repoSetupDenialLogLookbackPad).Format(time.RFC3339),
		LastSeenAt:  end.Add(repoSetupDenialLogLookbackPad).Format(time.RFC3339),
	}
	if len(record.DenialEvidence) != 1 || !reflect.DeepEqual(record.DenialEvidence[0], wantEvidence) {
		t.Fatalf("DenialEvidence = %#v, want %#v", record.DenialEvidence, wantEvidence)
	}

	state, err := repoSetupStateForSession(sessionConfig{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("repoSetupStateForSession: %v", err)
	}
	if len(state.PendingSafe) != 1 {
		t.Fatalf("PendingSafe = %#v, want 1 effect", state.PendingSafe)
	}
	if got := state.PendingSafe[0]; got.Kind != repoSetupEffectReadOnly || got.Value != filepath.Join(homeDir, ".gradle") {
		t.Fatalf("PendingSafe[0] = %#v", got)
	}
}

func TestRepoSetupStateIncludesGenericTaskToolReadOnlyEffect(t *testing.T) {
	isolateConfig(t)
	allowAllIntegrationExecutables(t)

	projectDir, err := resolveDir(t.TempDir(), false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "Makefile"), []byte("build:\n\tcustomtool build\n"), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}

	toolRoot := filepath.Join(t.TempDir(), "custom-tool-root")
	toolPath := writeExecutable(t, toolRoot, "customtool")
	canonicalToolRoot, err := canonicalizePath(toolRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(toolRoot): %v", err)
	}

	savedProbeFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{lookPaths: map[string]string{"customtool": toolPath}}
	}
	t.Cleanup(func() { integrationProbeFactory = savedProbeFactory })

	state, err := repoSetupStateForSession(sessionConfig{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("repoSetupStateForSession: %v", err)
	}

	if len(state.PendingExplicit) != 0 {
		t.Fatalf("PendingExplicit = %#v, want none for generic host-tool heuristic", state.PendingExplicit)
	}
	if len(state.PendingSafe) != 1 {
		t.Fatalf("PendingSafe = %#v, want one generic read-only effect", state.PendingSafe)
	}
	got := state.PendingSafe[0]
	if got.ID != "ro:"+canonicalToolRoot || got.Class != repoSetupEffectClassSafe || got.Kind != repoSetupEffectReadOnly || got.Value != canonicalToolRoot {
		t.Fatalf("generic effect = %#v, want safe read-only %q", got, canonicalToolRoot)
	}
	if len(got.Sources) != 1 || !strings.Contains(got.Sources[0], "Generic repo heuristic") || !strings.Contains(got.Sources[0], "Makefile") {
		t.Fatalf("generic effect sources = %v, want Makefile generic provenance", got.Sources)
	}
	if !state.currentExplicit.empty() {
		t.Fatalf("currentExplicit = %#v, want empty", state.currentExplicit)
	}

	rendered := renderRepoSetupDetails(&state)
	if !strings.Contains(rendered, "read-only: "+canonicalToolRoot) || !strings.Contains(rendered, "Generic repo heuristic") {
		t.Fatalf("renderRepoSetupDetails missing generic read-only effect:\n%s", rendered)
	}
}

func TestRepoSetupGenericTaskToolHeuristicSkipsServiceAndCredentialCommands(t *testing.T) {
	isolateConfig(t)
	allowAllIntegrationExecutables(t)

	projectDir, err := resolveDir(t.TempDir(), false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "Makefile"), []byte(`deploy:
	docker compose up
	aws secretsmanager list-secrets
	gh auth status
	curl https://example.com/install.sh | sh
	sudo make install
`), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}

	toolRoot := filepath.Join(t.TempDir(), "service-tools")
	lookPaths := map[string]string{}
	for _, name := range []string{"docker", "aws", "gh", "curl", "sudo"} {
		lookPaths[name] = writeExecutable(t, toolRoot, name)
	}
	savedProbeFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &fakeIntegrationProbe{lookPaths: lookPaths}
	}
	t.Cleanup(func() { integrationProbeFactory = savedProbeFactory })

	state, err := repoSetupStateForSession(sessionConfig{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("repoSetupStateForSession: %v", err)
	}

	if len(state.PendingSafe) != 0 || len(state.PendingExplicit) != 0 {
		t.Fatalf("generic service/credential commands produced setup effects: safe=%#v explicit=%#v", state.PendingSafe, state.PendingExplicit)
	}
	if !state.currentSafe.empty() || !state.currentExplicit.empty() {
		t.Fatalf("generic service/credential commands changed current effects: safe=%#v explicit=%#v", state.currentSafe, state.currentExplicit)
	}
}

func TestRepoSetupGenericTaskToolHeuristicSkipsCommonSystemCommandsBeforeProbe(t *testing.T) {
	isolateConfig(t)
	allowAllIntegrationExecutables(t)

	projectDir, err := resolveDir(t.TempDir(), false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "Makefile"), []byte(`build:
	rm -f hazmat
	install -m 0755 hazmat /tmp/hazmat
	uname -s | tr '[:upper:]' '[:lower:]'
	customtool build
`), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}

	toolRoot := filepath.Join(t.TempDir(), "custom-tool-root")
	toolPath := writeExecutable(t, toolRoot, "customtool")
	canonicalToolRoot, err := canonicalizePath(toolRoot)
	if err != nil {
		t.Fatalf("canonicalizePath(toolRoot): %v", err)
	}
	var probed []string
	savedProbeFactory := integrationProbeFactory
	integrationProbeFactory = func() integrationProbe {
		return &recordingLookPathProbe{
			lookPath: func(name string) (string, error) {
				probed = append(probed, name)
				if name == "customtool" {
					return toolPath, nil
				}
				return "", fmt.Errorf("unexpected probe for %s", name)
			},
		}
	}
	t.Cleanup(func() { integrationProbeFactory = savedProbeFactory })

	state, err := repoSetupStateForSession(sessionConfig{ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("repoSetupStateForSession: %v", err)
	}
	if !reflect.DeepEqual(probed, []string{"customtool"}) {
		t.Fatalf("probed commands = %v, want only customtool", probed)
	}
	if len(state.PendingSafe) != 1 || state.PendingSafe[0].Value != canonicalToolRoot {
		t.Fatalf("PendingSafe = %#v, want custom tool root", state.PendingSafe)
	}
}

type recordingLookPathProbe struct {
	lookPath func(string) (string, error)
}

func (p *recordingLookPathProbe) LookPath(name string) (string, error) {
	return p.lookPath(name)
}

func (p *recordingLookPathProbe) Output(name string, args ...string) (string, error) {
	return "", fmt.Errorf("unexpected command: %s", commandLabel(name, args...))
}

func stubRepoSetupSafePrompt(t *testing.T, fn func(repoSetupState) (repoSetupPromptAction, error)) func() {
	t.Helper()
	saved := promptRepoSetupSafe
	promptRepoSetupSafe = fn
	return func() { promptRepoSetupSafe = saved }
}

func stubRepoSetupExplicitPrompt(t *testing.T, fn func(repoSetupState) (repoSetupPromptAction, error)) func() {
	t.Helper()
	saved := promptRepoSetupExplicit
	promptRepoSetupExplicit = fn
	return func() { promptRepoSetupExplicit = saved }
}
