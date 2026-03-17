package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDirAcceptsAnyExistingDirectory(t *testing.T) {
	// Any existing directory should resolve regardless of its location —
	// there is no workspace-containment requirement.
	dir := t.TempDir()
	got, err := resolveDir(dir, false)
	if err != nil {
		t.Fatalf("resolveDir returned error for existing dir: %v", err)
	}
	// EvalSymlinks may change the path on macOS (/var → /private/var etc.)
	want, _ := filepath.EvalSymlinks(dir)
	if got != want {
		t.Fatalf("resolveDir = %q, want %q", got, want)
	}
}

func TestResolveDirRejectsNonExistentPath(t *testing.T) {
	if _, err := resolveDir("/nonexistent/path/that/does/not/exist", false); err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestResolveDirRejectsFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "notadir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := resolveDir(f.Name(), false); err == nil {
		t.Fatal("expected error for file path")
	}
}

func TestResolveReferenceDirsDeduplicates(t *testing.T) {
	refA := filepath.Join(t.TempDir(), "ref-a")
	refB := filepath.Join(t.TempDir(), "ref-b")
	for _, dir := range []string{refA, refB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	refAResolved, err := filepath.EvalSymlinks(refA)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", refA, err)
	}
	refBResolved, err := filepath.EvalSymlinks(refB)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", refB, err)
	}

	got, err := resolveReferenceDirs([]string{refA, refA, refB})
	if err != nil {
		t.Fatalf("resolveReferenceDirs returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 unique references, got %d (%v)", len(got), got)
	}
	if got[0] != refAResolved || got[1] != refBResolved {
		t.Fatalf("unexpected reference order/content: %v", got)
	}
}

func TestResolveReferenceDirsAcceptsPathsOutsideWorkspace(t *testing.T) {
	// References are no longer required to be inside ~/workspace.
	outside := t.TempDir()
	got, err := resolveReferenceDirs([]string{outside})
	if err != nil {
		t.Fatalf("expected outside reference path to be accepted, got error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 reference, got %d", len(got))
	}
}

func TestResolveSessionConfigExplicitWorkspace(t *testing.T) {
	projectDir := t.TempDir()
	workspaceDir := t.TempDir()

	cfg, err := resolveSessionConfig(projectDir, workspaceDir, nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}

	wantProject, _ := filepath.EvalSymlinks(projectDir)
	wantWorkspace, _ := filepath.EvalSymlinks(workspaceDir)

	if cfg.ProjectDir != wantProject {
		t.Errorf("ProjectDir = %q, want %q", cfg.ProjectDir, wantProject)
	}
	if cfg.WorkspaceRoot != wantWorkspace {
		t.Errorf("WorkspaceRoot = %q, want %q", cfg.WorkspaceRoot, wantWorkspace)
	}
}

func TestResolveSessionConfigNoWorkspaceLeaveWorkspaceRootEmpty(t *testing.T) {
	projectDir := t.TempDir()

	cfg, err := resolveSessionConfig(projectDir, "", nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	if cfg.WorkspaceRoot != "" {
		t.Errorf("WorkspaceRoot = %q, want empty string when no --workspace given", cfg.WorkspaceRoot)
	}
}

func TestResolveSessionConfigProjectOutsideWorkspace(t *testing.T) {
	// A project directory that is not under ~/workspace must be accepted.
	projectDir := t.TempDir()

	cfg, err := resolveSessionConfig(projectDir, "", nil)
	if err != nil {
		t.Fatalf("project outside ~/workspace was rejected: %v", err)
	}
	want, _ := filepath.EvalSymlinks(projectDir)
	if cfg.ProjectDir != want {
		t.Errorf("ProjectDir = %q, want %q", cfg.ProjectDir, want)
	}
}

// ── generateSBPL ──────────────────────────────────────────────────────────────

func TestGenerateSBPLProjectOnly(t *testing.T) {
	cfg := sessionConfig{
		ProjectDir: "/tmp/myproject",
	}
	policy := generateSBPL(cfg)

	// Project dir must have both read and write.
	if !strings.Contains(policy, `(allow file-read* (subpath "/tmp/myproject"))`) {
		t.Error("expected file-read* rule for PROJECT_DIR")
	}
	if !strings.Contains(policy, `(allow file-write* (subpath "/tmp/myproject"))`) {
		t.Error("expected file-write* rule for PROJECT_DIR")
	}

	// No broad workspace read when WorkspaceRoot is empty.
	if strings.Contains(policy, "WORKSPACE_ROOT") {
		t.Error("policy should not reference WORKSPACE_ROOT param when WorkspaceRoot is empty")
	}

	// Credential dirs must be denied.
	for _, cred := range []string{"/.ssh", "/.aws", "/.gnupg"} {
		want := `(deny file-read* (subpath "` + agentHome + cred + `"))`
		if !strings.Contains(policy, want) {
			t.Errorf("expected credential deny rule for %s", cred)
		}
	}
}

func TestGenerateSBPLWithReferenceDirs(t *testing.T) {
	cfg := sessionConfig{
		ProjectDir:    "/tmp/myproject",
		ReferenceDirs: []string{"/tmp/ref1", "/tmp/ref2"},
	}
	policy := generateSBPL(cfg)

	// Each reference dir must have a read rule.
	for _, ref := range cfg.ReferenceDirs {
		want := `(allow file-read* (subpath "` + ref + `"))`
		if !strings.Contains(policy, want) {
			t.Errorf("expected file-read* rule for reference dir %s", ref)
		}
	}

	// Reference dirs must NOT have a write rule.
	for _, ref := range cfg.ReferenceDirs {
		bad := `(allow file-write* (subpath "` + ref + `"))`
		if strings.Contains(policy, bad) {
			t.Errorf("reference dir %s must not have file-write* rule", ref)
		}
	}
}

func TestGenerateSBPLWithWorkspaceRoot(t *testing.T) {
	cfg := sessionConfig{
		WorkspaceRoot: "/Users/Shared/workspace",
		ProjectDir:    "/Users/Shared/workspace/myproject",
	}
	policy := generateSBPL(cfg)

	// Workspace root gets a broad read rule.
	if !strings.Contains(policy, `(allow file-read* (subpath "/Users/Shared/workspace"))`) {
		t.Error("expected broad file-read* rule for WorkspaceRoot")
	}

	// Workspace root must NOT get a write rule.
	if strings.Contains(policy, `(allow file-write* (subpath "/Users/Shared/workspace"))`) {
		t.Error("workspace root must not have file-write* rule")
	}

	// Project dir still gets read+write.
	if !strings.Contains(policy, `(allow file-write* (subpath "/Users/Shared/workspace/myproject"))`) {
		t.Error("expected file-write* rule for PROJECT_DIR even when WorkspaceRoot is set")
	}
}

func TestGenerateSBPLWorkspaceEqualToProjectOmitsBroadRead(t *testing.T) {
	// When WorkspaceRoot == ProjectDir, the broad workspace read rule should
	// be omitted (it would be redundant and emit a confusingly wide allow).
	cfg := sessionConfig{
		WorkspaceRoot: "/tmp/myproject",
		ProjectDir:    "/tmp/myproject",
	}
	policy := generateSBPL(cfg)

	// Only one file-read* rule for the path, not two.
	count := strings.Count(policy, `(allow file-read* (subpath "/tmp/myproject"))`)
	if count != 1 {
		t.Errorf("expected exactly 1 file-read* rule for path, got %d", count)
	}
}

func TestGenerateSBPLReferenceCoveredByWorkspaceRootSkipped(t *testing.T) {
	// A reference dir that falls under WorkspaceRoot should not generate a
	// redundant rule — the workspace read allow already covers it.
	cfg := sessionConfig{
		WorkspaceRoot: "/Users/Shared/workspace",
		ProjectDir:    "/Users/Shared/workspace/myproject",
		ReferenceDirs: []string{"/Users/Shared/workspace/lib"},
	}
	policy := generateSBPL(cfg)

	// The broad workspace root rule covers lib already; no separate rule needed.
	redundant := `(allow file-read* (subpath "/Users/Shared/workspace/lib"))`
	if strings.Contains(policy, redundant) {
		t.Error("redundant per-reference rule emitted when reference is inside WorkspaceRoot")
	}
}

// ── warnDockerProject ─────────────────────────────────────────────────────────

func TestWarnDockerProjectCleanDir(t *testing.T) {
	dir := t.TempDir()
	if err := warnDockerProject(dir, false); err != nil {
		t.Fatalf("expected no error for clean dir, got: %v", err)
	}
}

func TestWarnDockerProjectRootArtifacts(t *testing.T) {
	markers := []string{
		"Dockerfile",
		"Containerfile",
		"compose.yaml",
		"compose.yml",
		"docker-compose.yml",
		"docker-compose.yaml",
	}
	for _, name := range markers {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
				t.Fatalf("create %s: %v", name, err)
			}
			err := warnDockerProject(dir, false)
			if err == nil {
				t.Fatalf("expected error when %s is present, got nil", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error message should name %q, got: %s", name, err)
			}
		})
	}
}

func TestWarnDockerProjectDevcontainerDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".devcontainer"), 0o755); err != nil {
		t.Fatalf("mkdir .devcontainer: %v", err)
	}
	err := warnDockerProject(dir, false)
	if err == nil {
		t.Fatal("expected error when .devcontainer/ is present, got nil")
	}
	if !strings.Contains(err.Error(), ".devcontainer/") {
		t.Errorf("error message should name .devcontainer/, got: %s", err)
	}
}

func TestWarnDockerProjectMultipleMarkersAllListed(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Dockerfile", "compose.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	err := warnDockerProject(dir, false)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, name := range []string{"Dockerfile", "compose.yaml"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("expected %q in error message, got: %s", name, err)
		}
	}
}

func TestWarnDockerProjectAllowFlagContinues(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}
	// allow=true should not return an error even when markers are present.
	if err := warnDockerProject(dir, true); err != nil {
		t.Fatalf("expected no error with allow=true, got: %v", err)
	}
}

func TestWarnDockerProjectErrorMentionsTier3(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}
	err := warnDockerProject(dir, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "docker sandbox run") {
		t.Errorf("error message should mention Tier 3 command, got: %s", err)
	}
	if !strings.Contains(err.Error(), "--allow-docker") {
		t.Errorf("error message should mention --allow-docker override, got: %s", err)
	}
}

// ── agentEnvPairs ──────────────────────────────────────────────────────────────

func TestAgentEnvPairsExposeWorkspaceSession(t *testing.T) {
	cfg := sessionConfig{
		WorkspaceRoot: "/Users/dr/workspace",
		ProjectDir:    "/Users/dr/workspace/project",
		ReferenceDirs: []string{
			"/Users/dr/workspace/ref-a",
			"/Users/dr/workspace/ref-b",
		},
	}

	pairs := agentEnvPairs(cfg)
	values := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			t.Fatalf("malformed env pair: %q", pair)
		}
		values[key] = value
	}

	if values["SANDBOX_WORKSPACE_ROOT"] != cfg.WorkspaceRoot {
		t.Fatalf("SANDBOX_WORKSPACE_ROOT = %q, want %q", values["SANDBOX_WORKSPACE_ROOT"], cfg.WorkspaceRoot)
	}
	if values["SANDBOX_PROJECT_DIR"] != cfg.ProjectDir {
		t.Fatalf("SANDBOX_PROJECT_DIR = %q, want %q", values["SANDBOX_PROJECT_DIR"], cfg.ProjectDir)
	}

	var refs []string
	if err := json.Unmarshal([]byte(values["SANDBOX_REFERENCE_DIRS_JSON"]), &refs); err != nil {
		t.Fatalf("unmarshal SANDBOX_REFERENCE_DIRS_JSON: %v", err)
	}
	if len(refs) != len(cfg.ReferenceDirs) {
		t.Fatalf("reference count = %d, want %d", len(refs), len(cfg.ReferenceDirs))
	}
}
