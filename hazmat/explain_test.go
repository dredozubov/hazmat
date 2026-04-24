package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildExplainJSON(t *testing.T) {
	repoSetup := &repoSetupState{
		AppliedSafe: []repoSetupEffect{
			{
				ID:      "ro:/tmp/toolchain",
				Class:   repoSetupEffectClassSafe,
				Kind:    repoSetupEffectReadOnly,
				Value:   "/tmp/toolchain",
				Sources: []string{"Suggested by project files (node)"},
			},
		},
		PendingExplicit: []repoSetupEffect{
			{
				ID:      "rw:/tmp/cache",
				Class:   repoSetupEffectClassExplicit,
				Kind:    repoSetupEffectWrite,
				Value:   "/tmp/cache",
				Sources: []string{"Learned from previous session denial"},
			},
		},
		record: repoProfileRecord{
			Remembered: repoSetupStoredEffects{ReadOnly: []string{"/tmp/toolchain"}},
		},
	}
	cfg := sessionConfig{
		ProjectDir:            "/tmp/project",
		ReadDirs:              []string{"/tmp/auto", "/tmp/user"},
		WriteDirs:             []string{"/tmp/write"},
		UserReadDirs:          []string{"/tmp/user"},
		AutoReadDirs:          []string{"/tmp/auto"},
		SuggestedIntegrations: []string{"node"},
		ActiveIntegrations:    []string{"python-uv"},
		IntegrationSources:    []string{"uv (uv.lock)"},
		IntegrationDetails:    []string{"python-uv: resolved interpreter via uv"},
		IntegrationWarnings:   []string{"registry redirect active"},
		IntegrationEnv: map[string]string{
			"VIRTUAL_ENV": "/tmp/project/.venv",
			"GOROOT":      "/opt/homebrew/Cellar/go/1.2.3/libexec",
		},
		IntegrationRegistryKeys: []string{"NPM_CONFIG_REGISTRY"},
		PlannedHostMutations: []sessionMutation{
			{
				Summary:     "project ACL repair",
				Detail:      "may add collaborative ACLs under /tmp/project",
				Persistence: "persistent in project",
				ProofScope:  sessionMutationProofScopeTLAModel,
			},
		},
		IntegrationExcludes: []string{".venv/"},
		ServiceAccess:       []string{"docker"},
		GitSSH: &sessionGitSSHConfig{
			DisplayName: "id_ed25519",
		},
		RoutingReason: "staying in native containment because docker: none is configured",
		SessionNotes:  []string{"Docker files detected but disabled by config"},
		RepoSetup:     repoSetup,
	}

	got := buildExplainJSON("shell", cfg, sessionModeNative, true)
	if got.FormatVersion != explainJSONFormatVersion {
		t.Fatalf("FormatVersion = %d", got.FormatVersion)
	}
	if got.Target != "shell" {
		t.Fatalf("Target = %q", got.Target)
	}
	if got.Mode != string(sessionModeNative) {
		t.Fatalf("Mode = %q", got.Mode)
	}
	if !reflect.DeepEqual(got.SuggestedIntegrations, []string{"node"}) {
		t.Fatalf("SuggestedIntegrations = %v", got.SuggestedIntegrations)
	}
	if !reflect.DeepEqual(got.ActiveIntegrations, []string{"python-uv"}) {
		t.Fatalf("ActiveIntegrations = %v", got.ActiveIntegrations)
	}
	if got.RepoSetupSummary != "remembered (1 read-only path); additional approval required (1 write path)" {
		t.Fatalf("RepoSetupSummary = %q", got.RepoSetupSummary)
	}
	if !reflect.DeepEqual(got.RepoSetupApplied, []explainJSONRepoSetupEffect{
		{
			Class:   "safe",
			Kind:    "read_only",
			Value:   "/tmp/toolchain",
			Sources: []string{"Suggested by project files (node)"},
		},
	}) {
		t.Fatalf("RepoSetupApplied = %#v", got.RepoSetupApplied)
	}
	if !reflect.DeepEqual(got.RepoSetupPending, []explainJSONRepoSetupEffect{
		{
			Class:   "explicit",
			Kind:    "write",
			Value:   "/tmp/cache",
			Sources: []string{"Learned from previous session denial"},
		},
	}) {
		t.Fatalf("RepoSetupPending = %#v", got.RepoSetupPending)
	}
	if !reflect.DeepEqual(got.IntegrationEnvKeys, []string{"GOROOT", "VIRTUAL_ENV"}) {
		t.Fatalf("IntegrationEnvKeys = %v", got.IntegrationEnvKeys)
	}
	if got.Snapshot.Enabled {
		t.Fatalf("Snapshot.Enabled = true, want false")
	}
	if !reflect.DeepEqual(got.Snapshot.Excludes, []string{".venv/"}) {
		t.Fatalf("Snapshot.Excludes = %v", got.Snapshot.Excludes)
	}
	if len(got.PlannedHostMutations) != 1 || got.PlannedHostMutations[0].Summary != "project ACL repair" {
		t.Fatalf("PlannedHostMutations = %v", got.PlannedHostMutations)
	}
	if got.GitSSHKey != "id_ed25519" {
		t.Fatalf("GitSSHKey = %q, want id_ed25519", got.GitSSHKey)
	}
}

func TestExplainJSONCommandOutputsStructuredPreview(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	canonicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", dir, err)
	}

	cmd := newExplainCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--json", "-C", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var preview explainJSONPreview
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil {
		t.Fatalf("unmarshal preview: %v\nstdout=%s", err, stdout.String())
	}
	if preview.ProjectDir != canonicalDir {
		t.Fatalf("ProjectDir = %q, want %q", preview.ProjectDir, canonicalDir)
	}
	if !reflect.DeepEqual(preview.SuggestedIntegrations, []string{"node"}) {
		t.Fatalf("SuggestedIntegrations = %v", preview.SuggestedIntegrations)
	}
	if preview.Mode != string(sessionModeNative) {
		t.Fatalf("Mode = %q", preview.Mode)
	}
	if !preview.Snapshot.Enabled {
		t.Fatalf("Snapshot.Enabled = false, want true")
	}
}

func TestRenderRepoSetupDetails(t *testing.T) {
	got := renderRepoSetupDetails(&repoSetupState{
		AppliedSafe: []repoSetupEffect{
			{
				ID:      "ro:/tmp/toolchain",
				Class:   repoSetupEffectClassSafe,
				Kind:    repoSetupEffectReadOnly,
				Value:   "/tmp/toolchain",
				Sources: []string{"Suggested by project files (node)"},
			},
		},
		PendingSafe: []repoSetupEffect{
			{
				ID:      "exclude:build/",
				Class:   repoSetupEffectClassSafe,
				Kind:    repoSetupEffectSnapshotExclude,
				Value:   "build/",
				Sources: []string{"Suggested by project files (node)"},
			},
		},
		record: repoProfileRecord{
			Remembered: repoSetupStoredEffects{ReadOnly: []string{"/tmp/toolchain"}},
		},
	})

	for _, want := range []string{
		"hazmat: repo setup remembered (1 read-only path); additional repo setup available (1 snapshot exclude)",
		"Applied:",
		"- read-only: /tmp/toolchain (Suggested by project files (node))",
		"Available:",
		"- snapshot exclude: build/ (Suggested by project files (node))",
	} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("renderRepoSetupDetails missing %q in:\n%s", want, got)
		}
	}
}
