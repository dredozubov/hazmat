package main

import (
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

	start := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
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
