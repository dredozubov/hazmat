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

func TestResolveReadDirsDeduplicates(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	for _, dir := range []string{dirA, dirB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	dirAResolved, err := filepath.EvalSymlinks(dirA)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", dirA, err)
	}
	dirBResolved, err := filepath.EvalSymlinks(dirB)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", dirB, err)
	}

	got, err := resolveReadDirs([]string{dirA, dirA, dirB})
	if err != nil {
		t.Fatalf("resolveReadDirs returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 unique dirs, got %d (%v)", len(got), got)
	}
	if got[0] != dirAResolved || got[1] != dirBResolved {
		t.Fatalf("unexpected order/content: %v", got)
	}
}

func TestResolveReadDirsAcceptsAnyPath(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveReadDirs([]string{dir})
	if err != nil {
		t.Fatalf("expected path to be accepted, got error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 dir, got %d", len(got))
	}
}

func TestResolveSessionConfigWithReadDirs(t *testing.T) {
	projectDir := t.TempDir()
	readDir := t.TempDir()

	cfg, err := resolveSessionConfig(projectDir, []string{readDir})
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}

	wantProject, _ := filepath.EvalSymlinks(projectDir)
	wantRead, _ := filepath.EvalSymlinks(readDir)

	if cfg.ProjectDir != wantProject {
		t.Errorf("ProjectDir = %q, want %q", cfg.ProjectDir, wantProject)
	}
	if len(cfg.ReadDirs) != 1 || cfg.ReadDirs[0] != wantRead {
		t.Errorf("ReadDirs = %v, want [%q]", cfg.ReadDirs, wantRead)
	}
}

func TestResolveSessionConfigNoReadDirs(t *testing.T) {
	projectDir := t.TempDir()

	cfg, err := resolveSessionConfig(projectDir, nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	if len(cfg.ReadDirs) != 0 {
		t.Errorf("ReadDirs = %v, want empty", cfg.ReadDirs)
	}
}

func TestResolveSessionConfigProjectAnywhere(t *testing.T) {
	projectDir := t.TempDir()

	cfg, err := resolveSessionConfig(projectDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want, _ := filepath.EvalSymlinks(projectDir)
	if cfg.ProjectDir != want {
		t.Errorf("ProjectDir = %q, want %q", cfg.ProjectDir, want)
	}
}

// ── defaultReadDirs ─────────────────────────────────────────────────────────

func isolateConfig(t *testing.T) {
	t.Helper()
	savedCfg := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "nonexistent.yaml")
	t.Cleanup(func() { configFilePath = savedCfg })
}

func TestDefaultReadDirsNoConfiguredDirsByDefault(t *testing.T) {
	isolateConfig(t)
	// With no config, defaultReadDirs returns only implicit toolchain dirs
	// (e.g. go/pkg/mod if it exists). No configured session.read_dirs.
	got := defaultReadDirs(nil)
	for _, d := range got {
		// Implicit toolchain dirs are fine — they're auto-detected, not configured.
		if !isImplicitToolchainDir(d) {
			t.Errorf("unexpected non-toolchain read dir: %s", d)
		}
	}
}

func isImplicitToolchainDir(d string) bool {
	implicit := implicitReadDirs()
	for _, i := range implicit {
		if d == i {
			return true
		}
	}
	return false
}

func TestDefaultReadDirsUsesConfigReadDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	savedCfg := configFilePath
	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	configFilePath = cfgFile
	t.Cleanup(func() { configFilePath = savedCfg })

	cfg := defaultConfig()
	dirs := []string{dir1, dir2}
	cfg.Session.ReadDirs = &dirs
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	got := defaultReadDirs(nil)
	found := 0
	for _, d := range got {
		if d == dir1 || d == dir2 {
			found++
		}
	}
	if found != 2 {
		t.Errorf("defaultReadDirs(nil) = %v, want to contain [%q, %q]", got, dir1, dir2)
	}
}

func TestDefaultReadDirsExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}

	savedCfg := configFilePath
	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	configFilePath = cfgFile
	t.Cleanup(func() { configFilePath = savedCfg })

	cfg := defaultConfig()
	dirs := []string{"~/workspace"}
	cfg.Session.ReadDirs = &dirs
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	got := defaultReadDirs(nil)
	want := filepath.Join(home, "workspace")
	if _, err := os.Stat(want); err != nil {
		t.Skipf("%s does not exist, skipping", want)
	}
	found := false
	for _, d := range got {
		if d == want {
			found = true
		}
	}
	if !found {
		t.Errorf("defaultReadDirs(nil) = %v, want to contain %q", got, want)
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

	// No read-only section when ReadDirs is empty.
	if strings.Contains(policy, "Read-only directories") {
		t.Error("policy should not have read-only section when ReadDirs is empty")
	}

	// Credential dirs must be denied.
	for _, cred := range []string{"/.ssh", "/.aws", "/.gnupg"} {
		want := `(deny file-read* file-write* (subpath "` + agentHome + cred + `"))`
		if !strings.Contains(policy, want) {
			t.Errorf("expected credential deny rule for %s", cred)
		}
	}
}

func TestGenerateSBPLWithReadDirs(t *testing.T) {
	cfg := sessionConfig{
		ProjectDir: "/tmp/myproject",
		ReadDirs:   []string{"/tmp/ref1", "/tmp/ref2"},
	}
	policy := generateSBPL(cfg)

	// Each read dir must have a read rule.
	for _, dir := range cfg.ReadDirs {
		want := `(allow file-read* (subpath "` + dir + `"))`
		if !strings.Contains(policy, want) {
			t.Errorf("expected file-read* rule for read dir %s", dir)
		}
	}

	// Read dirs must NOT have a write rule.
	for _, dir := range cfg.ReadDirs {
		bad := `(allow file-write* (subpath "` + dir + `"))`
		if strings.Contains(policy, bad) {
			t.Errorf("read dir %s must not have file-write* rule", dir)
		}
	}
}

func TestGenerateSBPLReadDirEqualToProjectOmitted(t *testing.T) {
	// A read dir that equals ProjectDir is redundant (project already has
	// read+write) and should not emit a separate read-only rule.
	cfg := sessionConfig{
		ProjectDir: "/tmp/myproject",
		ReadDirs:   []string{"/tmp/myproject"},
	}
	policy := generateSBPL(cfg)

	count := strings.Count(policy, `(allow file-read* (subpath "/tmp/myproject"))`)
	if count != 1 {
		t.Errorf("expected exactly 1 file-read* rule for path, got %d", count)
	}
}

func TestGenerateSBPLReadDirCoveredByBroaderReadDirSkipped(t *testing.T) {
	// A narrow read dir inside a broader one should not emit a redundant rule.
	cfg := sessionConfig{
		ProjectDir: "/tmp/myproject",
		ReadDirs:   []string{"/Users/Shared/code", "/Users/Shared/code/lib"},
	}
	policy := generateSBPL(cfg)

	// The broad rule covers lib already; no separate rule needed.
	redundant := `(allow file-read* (subpath "/Users/Shared/code/lib"))`
	if strings.Contains(policy, redundant) {
		t.Error("redundant rule emitted for read dir inside a broader read dir")
	}
	// The broad rule itself must be present.
	if !strings.Contains(policy, `(allow file-read* (subpath "/Users/Shared/code"))`) {
		t.Error("expected read rule for broader read dir")
	}
}

func TestGenerateSBPLReadDirInsideProjectSkipped(t *testing.T) {
	// A read dir inside the project is redundant — project has read+write.
	cfg := sessionConfig{
		ProjectDir: "/Users/Shared/code/myproject",
		ReadDirs:   []string{"/Users/Shared/code/myproject/subdir"},
	}
	policy := generateSBPL(cfg)

	redundant := `(allow file-read* (subpath "/Users/Shared/code/myproject/subdir"))`
	if strings.Contains(policy, redundant) {
		t.Error("redundant rule emitted for read dir inside project dir")
	}
}

func TestGenerateSBPLDoesNotGrantHostResumeDirAccess(t *testing.T) {
	cfg := sessionConfig{
		ProjectDir: "/tmp/myproject",
	}
	policy := generateSBPL(cfg)

	if strings.Contains(policy, "Resume session directory") {
		t.Error("resume section should not appear in the seatbelt policy")
	}
	if strings.Contains(policy, "/Users/dr/.claude/projects") {
		t.Error("seatbelt should not reference host Claude transcript directories")
	}
}

func TestGenerateSBPLAllowsAncestorMetadata(t *testing.T) {
	cfg := sessionConfig{
		ProjectDir: "/Users/dr/workspace/project",
		ReadDirs:   []string{"/Users/dr/workspace/references", "/opt/tools"},
	}
	policy := generateSBPL(cfg)

	for _, want := range []string{
		`(allow file-read-metadata (literal "/Users"))`,
		`(allow file-read-metadata (literal "/Users/dr"))`,
		`(allow file-read-metadata (literal "/Users/dr/workspace"))`,
		`(allow file-read-metadata (literal "/opt"))`,
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("expected ancestor metadata rule %q in policy:\n%s", want, policy)
		}
	}
	if strings.Count(policy, `(allow file-read-metadata (literal "/Users/dr/workspace"))`) != 1 {
		t.Fatal("shared ancestor should only appear once")
	}
}

func TestGenerateSBPLProjectWriteReasserted(t *testing.T) {
	// When a read dir is a parent of the project dir, the project's write
	// access must be re-asserted as the last allow before credential denies.
	cfg := sessionConfig{
		ProjectDir: "/Users/dr/workspace/sandboxing",
		ReadDirs:   []string{"/Users/dr/workspace"},
	}
	policy := generateSBPL(cfg)

	// The project write re-assertion must appear after the read-only section
	// and before the credential deny section.
	reassert := `(allow file-read* file-write* (subpath "/Users/dr/workspace/sandboxing"))`
	denySection := ";; ── DENY sensitive credential directories"

	reassertIdx := strings.LastIndex(policy, reassert)
	denyIdx := strings.Index(policy, denySection)
	if reassertIdx < 0 {
		t.Fatal("project write re-assertion not found in policy")
	}
	if denyIdx < 0 {
		t.Fatal("credential deny section not found in policy")
	}
	if reassertIdx > denyIdx {
		t.Error("project write re-assertion must appear before credential denies")
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
	if !strings.Contains(err.Error(), "docker hazmat run") {
		t.Errorf("error message should mention Tier 3 command, got: %s", err)
	}
	if !strings.Contains(err.Error(), "--ignore-docker") {
		t.Errorf("error message should mention --ignore-docker override, got: %s", err)
	}
}

// ── agentEnvPairs ──────────────────────────────────────────────────────────────

// ── parseClaudeArgs tests ────────────────────────────────────────────────────

func TestParseClaudeArgsEmpty(t *testing.T) {
	opts, fwd, err := parseClaudeArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.project != "" || opts.noBackup || opts.allowDocker || len(opts.readDirs) != 0 {
		t.Fatalf("expected zero opts, got %+v", opts)
	}
	if len(fwd) != 0 {
		t.Fatalf("expected no forwarded args, got %v", fwd)
	}
}

func TestParseClaudeArgsForwardsUnknownFlags(t *testing.T) {
	args := []string{"--print", "explain this code", "--model", "sonnet"}
	opts, fwd, err := parseClaudeArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if opts.project != "" || opts.noBackup {
		t.Fatalf("hazmat flags should be empty, got %+v", opts)
	}
	want := []string{"--print", "explain this code", "--model", "sonnet"}
	if len(fwd) != len(want) {
		t.Fatalf("forwarded = %v, want %v", fwd, want)
	}
	for i := range want {
		if fwd[i] != want[i] {
			t.Fatalf("forwarded[%d] = %q, want %q", i, fwd[i], want[i])
		}
	}
}

func TestParseClaudeArgsMixedFlags(t *testing.T) {
	args := []string{"--no-backup", "-C", "/myproject", "-p", "hello", "--ignore-docker"}
	opts, fwd, err := parseClaudeArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if opts.project != "/myproject" {
		t.Fatalf("project = %q, want /myproject", opts.project)
	}
	if !opts.noBackup {
		t.Fatal("noBackup should be true")
	}
	if !opts.allowDocker {
		t.Fatal("allowDocker should be true")
	}
	want := []string{"-p", "hello"}
	if len(fwd) != len(want) {
		t.Fatalf("forwarded = %v, want %v", fwd, want)
	}
	for i := range want {
		if fwd[i] != want[i] {
			t.Fatalf("forwarded[%d] = %q, want %q", i, fwd[i], want[i])
		}
	}
}

func TestParseClaudeArgsDoubleDash(t *testing.T) {
	args := []string{"--no-backup", "--", "--help", "--project", "/sneaky"}
	opts, fwd, err := parseClaudeArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.noBackup {
		t.Fatal("noBackup should be true")
	}
	// Everything after -- is forwarded, even things that look like hazmat flags.
	want := []string{"--help", "--project", "/sneaky"}
	if len(fwd) != len(want) {
		t.Fatalf("forwarded = %v, want %v", fwd, want)
	}
	for i := range want {
		if fwd[i] != want[i] {
			t.Fatalf("forwarded[%d] = %q, want %q", i, fwd[i], want[i])
		}
	}
}

func TestParseClaudeArgsEqualsForm(t *testing.T) {
	args := []string{"--project=/foo", "--read=/bar", "--read=/baz", "-p", "hi"}
	opts, fwd, err := parseClaudeArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if opts.project != "/foo" {
		t.Fatalf("project = %q, want /foo", opts.project)
	}
	if len(opts.readDirs) != 2 || opts.readDirs[0] != "/bar" || opts.readDirs[1] != "/baz" {
		t.Fatalf("readDirs = %v, want [/bar /baz]", opts.readDirs)
	}
	if len(fwd) != 2 || fwd[0] != "-p" || fwd[1] != "hi" {
		t.Fatalf("forwarded = %v, want [-p hi]", fwd)
	}
}

func TestParseClaudeArgsReadRepeat(t *testing.T) {
	args := []string{"-R", "/a", "-R", "/b", "myarg"}
	opts, fwd, err := parseClaudeArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.readDirs) != 2 || opts.readDirs[0] != "/a" || opts.readDirs[1] != "/b" {
		t.Fatalf("readDirs = %v, want [/a /b]", opts.readDirs)
	}
	if len(fwd) != 1 || fwd[0] != "myarg" {
		t.Fatalf("forwarded = %v, want [myarg]", fwd)
	}
}

func TestParseClaudeArgsForwardsLeadingDirectory(t *testing.T) {
	args := []string{"/tmp/project", "-p", "hi"}
	opts, fwd, err := parseClaudeArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if opts.project != "" {
		t.Fatalf("project = %q, want empty", opts.project)
	}
	want := []string{"/tmp/project", "-p", "hi"}
	if len(fwd) != len(want) {
		t.Fatalf("forwarded = %v, want %v", fwd, want)
	}
	for i := range want {
		if fwd[i] != want[i] {
			t.Fatalf("forwarded[%d] = %q, want %q", i, fwd[i], want[i])
		}
	}
}

func TestParseClaudeArgsMissingValue(t *testing.T) {
	for _, flag := range []string{"--project", "-C", "--read", "-R"} {
		_, _, err := parseClaudeArgs([]string{flag})
		if err == nil {
			t.Fatalf("%s without value should error", flag)
		}
	}
}

func TestParseClaudeArgsHelp(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		_, _, err := parseClaudeArgs([]string{flag})
		if err != errClaudeHelp {
			t.Fatalf("parseClaudeArgs(%q) error = %v, want errClaudeHelp", flag, err)
		}
	}
}

func TestAgentEnvPairsExposeSessionConfig(t *testing.T) {
	cfg := sessionConfig{
		ProjectDir: "/Users/dr/workspace/project",
		ReadDirs: []string{
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

	if values["SANDBOX_PROJECT_DIR"] != cfg.ProjectDir {
		t.Fatalf("SANDBOX_PROJECT_DIR = %q, want %q", values["SANDBOX_PROJECT_DIR"], cfg.ProjectDir)
	}

	var dirs []string
	if err := json.Unmarshal([]byte(values["SANDBOX_READ_DIRS_JSON"]), &dirs); err != nil {
		t.Fatalf("unmarshal SANDBOX_READ_DIRS_JSON: %v", err)
	}
	if len(dirs) != len(cfg.ReadDirs) {
		t.Fatalf("read dir count = %d, want %d", len(dirs), len(cfg.ReadDirs))
	}
}
