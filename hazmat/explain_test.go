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
				ProofScope:  sessionMutationProofScopeTestsDocs,
			},
		},
		IntegrationExcludes: []string{".venv/"},
		ServiceAccess:       []string{"docker"},
		RoutingReason:       "staying in native containment because docker: none is configured",
		SessionNotes:        []string{"Docker files detected but disabled by config"},
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
}

func TestExplainJSONCommandOutputsStructuredPreview(t *testing.T) {
	isolateConfig(t)

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

func TestRenderSuggestedIntegrations(t *testing.T) {
	got := renderSuggestedIntegrations([]string{"node", "python-uv"})
	want := "hazmat: suggested integrations: node, python-uv (activate with --integration <name>)\n"
	if got != want {
		t.Fatalf("renderSuggestedIntegrations = %q, want %q", got, want)
	}
}
