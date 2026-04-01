package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── canonicalizePath ───────────────────────────────────────────────────────

func TestCanonicalizePathResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")

	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	got, err := canonicalizePath(link)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(real)
	if got != want {
		t.Fatalf("canonicalizePath(%q) = %q, want %q", link, got, want)
	}
}

func TestCanonicalizePathFailsOnNonExistent(t *testing.T) {
	_, err := canonicalizePath("/nonexistent/path/xyz")
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

// ── isCredentialDenyPath ───────────────────────────────────────────────────

func TestIsCredentialDenyPathExactMatch(t *testing.T) {
	for _, sub := range credentialDenySubs {
		path := agentHome + sub
		if !isCredentialDenyPath(path) {
			t.Errorf("isCredentialDenyPath(%q) = false, want true", path)
		}
	}
}

func TestIsCredentialDenyPathParent(t *testing.T) {
	// Agent home itself is a parent of /.ssh, /.aws, etc.
	if !isCredentialDenyPath(agentHome) {
		t.Errorf("isCredentialDenyPath(%q) = false, want true (parent of cred paths)", agentHome)
	}
}

func TestIsCredentialDenyPathInvokerHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	// Invoker's ~/.ssh should also be flagged.
	if !isCredentialDenyPath(filepath.Join(home, ".ssh")) {
		t.Errorf("isCredentialDenyPath(~/.ssh) = false, want true")
	}
	// Invoker's home itself is a parent of credential paths.
	if !isCredentialDenyPath(home) {
		t.Errorf("isCredentialDenyPath(~) = false, want true")
	}
}

func TestIsCredentialDenyPathSiblingAllowed(t *testing.T) {
	// ~/.local/share/pypoetry is a sibling of credential dirs, not a parent.
	safe := agentHome + "/.local/share/pypoetry"
	if isCredentialDenyPath(safe) {
		t.Errorf("isCredentialDenyPath(%q) = true, want false", safe)
	}
}

func TestIsCredentialDenyPathChildOfMavenRepoAllowed(t *testing.T) {
	// ~/.m2/repository is a sibling of ~/.m2/settings.xml — should be allowed.
	safe := agentHome + "/.m2/repository"
	if isCredentialDenyPath(safe) {
		t.Errorf("isCredentialDenyPath(%q) = true, want false", safe)
	}
}

func TestIsCredentialDenyPathConfigSubdirAllowed(t *testing.T) {
	// ~/.config/git is fine — credential denies are /.config/gh and /.config/gcloud.
	safe := agentHome + "/.config/git"
	if isCredentialDenyPath(safe) {
		t.Errorf("isCredentialDenyPath(%q) = true, want false", safe)
	}
}

func TestIsCredentialDenyPathCargoRegistryAllowed(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	safe := filepath.Join(home, ".cargo/registry")
	if isCredentialDenyPath(safe) {
		t.Errorf("isCredentialDenyPath(%q) = true, want false", safe)
	}
}

// ── validatePackSchema ─────────────────────────────────────────────────────

func TestValidatePackSchemaMinimal(t *testing.T) {
	p := Pack{
		PackMeta: PackMeta{Name: "test", Version: 1},
	}
	if err := validatePackSchema(p); err != nil {
		t.Fatalf("minimal valid pack failed: %v", err)
	}
}

func TestValidatePackSchemaMissingName(t *testing.T) {
	p := Pack{PackMeta: PackMeta{Version: 1}}
	if err := validatePackSchema(p); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidatePackSchemaInvalidName(t *testing.T) {
	for _, name := range []string{"Test", "foo_bar", "-start", "has space"} {
		p := Pack{PackMeta: PackMeta{Name: name, Version: 1}}
		if err := validatePackSchema(p); err == nil {
			t.Fatalf("expected error for invalid name %q", name)
		}
	}
}

func TestValidatePackSchemaWrongVersion(t *testing.T) {
	p := Pack{PackMeta: PackMeta{Name: "test", Version: 2}}
	if err := validatePackSchema(p); err == nil {
		t.Fatal("expected error for version != 1")
	}
}

func TestValidatePackSchemaUnsafeEnvKey(t *testing.T) {
	p := Pack{
		PackMeta: PackMeta{Name: "test", Version: 1},
		Session:  PackSession{EnvPassthrough: []string{"NODE_OPTIONS"}},
	}
	if err := validatePackSchema(p); err == nil {
		t.Fatal("expected error for unsafe env key NODE_OPTIONS")
	}
}

func TestValidatePackSchemaSafeEnvKey(t *testing.T) {
	p := Pack{
		PackMeta: PackMeta{Name: "test", Version: 1},
		Session:  PackSession{EnvPassthrough: []string{"GOPATH", "NODE_ENV"}},
	}
	if err := validatePackSchema(p); err != nil {
		t.Fatalf("safe env keys rejected: %v", err)
	}
}

func TestValidatePackSchemaNegationExclude(t *testing.T) {
	p := Pack{
		PackMeta: PackMeta{Name: "test", Version: 1},
		Backup:   PackBackup{Excludes: []string{"!important/"}},
	}
	if err := validatePackSchema(p); err == nil {
		t.Fatal("expected error for negation exclude")
	}
}

func TestValidatePackSchemaDetectFileWithPath(t *testing.T) {
	p := Pack{
		PackMeta: PackMeta{Name: "test", Version: 1},
		Detect:   PackDetect{Files: []string{"src/main.go"}},
	}
	if err := validatePackSchema(p); err == nil {
		t.Fatal("expected error for detect file with path separator")
	}
}

// ── validatePackPaths (V2) ─────────────────────────────────────────────────

func TestValidatePackPathsCredentialDirRejected(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	sshDir := filepath.Join(home, ".ssh")
	if _, err := os.Stat(sshDir); err != nil {
		t.Skipf("%s does not exist", sshDir)
	}

	p := Pack{
		PackMeta: PackMeta{Name: "bad", Version: 1},
		Session:  PackSession{ReadDirs: []string{"~/.ssh"}},
	}
	if _, err := validatePackPaths(p); err == nil {
		t.Fatal("expected error for ~/.ssh in read_dirs")
	}
}

func TestValidatePackPathsSymlinkToCredentialRejected(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	sshDir := filepath.Join(home, ".ssh")
	if _, err := os.Stat(sshDir); err != nil {
		t.Skipf("%s does not exist", sshDir)
	}

	tmpDir := t.TempDir()
	link := filepath.Join(tmpDir, "innocent-looking-dir")
	if err := os.Symlink(sshDir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	p := Pack{
		PackMeta: PackMeta{Name: "bad", Version: 1},
		Session:  PackSession{ReadDirs: []string{link}},
	}
	if _, err := validatePackPaths(p); err == nil {
		t.Fatal("expected error for symlink pointing to ~/.ssh")
	}
}

func TestValidatePackPathsSafeDirAccepted(t *testing.T) {
	dir := t.TempDir()
	p := Pack{
		PackMeta: PackMeta{Name: "ok", Version: 1},
		Session:  PackSession{ReadDirs: []string{dir}},
	}
	canonical, err := validatePackPaths(p)
	if err != nil {
		t.Fatalf("safe dir rejected: %v", err)
	}
	if len(canonical) != 1 {
		t.Fatalf("expected 1 canonical path, got %d", len(canonical))
	}
}

func TestValidatePackPathsNonExistentSkipped(t *testing.T) {
	p := Pack{
		PackMeta: PackMeta{Name: "ok", Version: 1},
		Session:  PackSession{ReadDirs: []string{"/nonexistent/path/xyz"}},
	}
	canonical, err := validatePackPaths(p)
	if err != nil {
		t.Fatalf("non-existent path should be skipped, not error: %v", err)
	}
	if len(canonical) != 0 {
		t.Fatalf("non-existent path should be skipped, got %v", canonical)
	}
}

// ── loadPack ───────────────────────────────────────────────────────────────

func TestLoadPackValid(t *testing.T) {
	data := []byte(`
pack:
  name: test-pack
  version: 1
  description: A test pack
detect:
  files: [go.mod]
session:
  read_dirs: [/tmp]
  env_passthrough: [GOPATH]
backup:
  excludes: [vendor/]
warnings:
  - "This is a test warning."
commands:
  test: go test ./...
`)
	p, err := loadPack(data)
	if err != nil {
		t.Fatalf("loadPack failed: %v", err)
	}
	if p.PackMeta.Name != "test-pack" {
		t.Fatalf("name = %q, want test-pack", p.PackMeta.Name)
	}
	if len(p.Session.ReadDirs) != 1 || p.Session.ReadDirs[0] != "/tmp" {
		t.Fatalf("read_dirs = %v, want [/tmp]", p.Session.ReadDirs)
	}
}

func TestLoadPackTooLarge(t *testing.T) {
	data := make([]byte, packMaxSize+1)
	if _, err := loadPack(data); err == nil {
		t.Fatal("expected error for oversized manifest")
	}
}

func TestLoadPackInvalidYAML(t *testing.T) {
	if _, err := loadPack([]byte("{{{")); err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadPackRejectsUnknownFields(t *testing.T) {
	data := []byte(`
pack:
  name: test
  version: 1
  unknown_field: oops
`)
	if _, err := loadPack(data); err == nil {
		t.Fatal("expected error for unknown field in pack section")
	}
}

func TestLoadPackRejectsUnknownTopLevelKey(t *testing.T) {
	data := []byte(`
pack:
  name: test
  version: 1
network:
  allow_ssh: true
`)
	if _, err := loadPack(data); err == nil {
		t.Fatal("expected error for unknown top-level key")
	}
}

// ── Built-in packs ────────────────────────────────────────────────────────

func TestBuiltinPacksLoad(t *testing.T) {
	names := allBuiltinPackNames()
	if len(names) == 0 {
		t.Fatal("no built-in packs found")
	}
	for _, name := range names {
		p, err := loadBuiltinPack(name)
		if err != nil {
			t.Fatalf("built-in pack %q failed to load: %v", name, err)
		}
		if p.PackMeta.Name != name {
			t.Errorf("built-in pack %q: internal name = %q (mismatch)", name, p.PackMeta.Name)
		}
	}
}

func TestBuiltinPacksSchemaValid(t *testing.T) {
	for _, name := range allBuiltinPackNames() {
		p, err := loadBuiltinPack(name)
		if err != nil {
			t.Fatalf("load %q: %v", name, err)
		}
		if err := validatePackSchema(p); err != nil {
			t.Fatalf("built-in pack %q schema invalid: %v", name, err)
		}
	}
}

// ── mergePacks ─────────────────────────────────────────────────────────────

func TestMergePacksDeduplicates(t *testing.T) {
	dir := t.TempDir()
	packs := []Pack{
		{
			PackMeta: PackMeta{Name: "a", Version: 1},
			Session:  PackSession{ReadDirs: []string{dir}},
			Backup:   PackBackup{Excludes: []string{"vendor/"}},
			Warnings: []string{"warning one"},
		},
		{
			PackMeta: PackMeta{Name: "b", Version: 1},
			Session:  PackSession{ReadDirs: []string{dir}}, // duplicate
			Backup:   PackBackup{Excludes: []string{"vendor/", ".next/"}},
			Warnings: []string{"warning one", "warning two"}, // one duplicate
		},
	}

	result, err := mergePacks(packs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ReadDirs) != 1 {
		t.Errorf("ReadDirs should be deduped to 1, got %d: %v", len(result.ReadDirs), result.ReadDirs)
	}
	if len(result.Excludes) != 2 {
		t.Errorf("Excludes should be deduped to 2, got %d: %v", len(result.Excludes), result.Excludes)
	}
	if len(result.Warnings) != 2 {
		t.Errorf("Warnings should be deduped to 2, got %d: %v", len(result.Warnings), result.Warnings)
	}
}

func TestMergePacksResolvesEnv(t *testing.T) {
	t.Setenv("GOPATH", "/test/gopath")
	packs := []Pack{
		{
			PackMeta: PackMeta{Name: "go", Version: 1},
			Session:  PackSession{EnvPassthrough: []string{"GOPATH", "GOPRIVATE"}},
		},
	}

	result, err := mergePacks(packs)
	if err != nil {
		t.Fatal(err)
	}
	if result.EnvPassthrough["GOPATH"] != "/test/gopath" {
		t.Errorf("GOPATH = %q, want /test/gopath", result.EnvPassthrough["GOPATH"])
	}
	if _, set := result.EnvPassthrough["GOPRIVATE"]; set {
		t.Error("GOPRIVATE should not be set (not in invoker env)")
	}
}

func TestMergePacksSurfacesRegistryKeys(t *testing.T) {
	t.Setenv("GOPROXY", "https://proxy.example.com")
	packs := []Pack{
		{
			PackMeta: PackMeta{Name: "go", Version: 1},
			Session:  PackSession{EnvPassthrough: []string{"GOPROXY", "GOPATH"}},
		},
	}

	result, err := mergePacks(packs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RegistryKeys) != 1 || result.RegistryKeys[0] != "GOPROXY" {
		t.Errorf("RegistryKeys = %v, want [GOPROXY]", result.RegistryKeys)
	}
}

// ── suggestPacks ───────────────────────────────────────────────────────────

func TestSuggestPacksMatchesDetectFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestPacks(dir, nil)
	found := false
	for _, s := range suggestions {
		if s == "go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'go' pack suggestion for dir with go.mod, got %v", suggestions)
	}
}

func TestSuggestPacksSkipsActive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}

	active := map[string]struct{}{"go": {}}
	suggestions := suggestPacks(dir, active)
	for _, s := range suggestions {
		if s == "go" {
			t.Error("'go' pack should not be suggested when already active")
		}
	}
}

func TestSuggestPacksNoMatchReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	suggestions := suggestPacks(dir, nil)
	if len(suggestions) != 0 {
		t.Errorf("expected no suggestions for empty dir, got %v", suggestions)
	}
}

// ── safeEnvKeys coverage ───────────────────────────────────────────────────

func TestSafeEnvKeysExcludesDangerousKeys(t *testing.T) {
	dangerous := []string{
		"NODE_OPTIONS", "PYTHONPATH", "GOFLAGS", "MAVEN_OPTS",
		"CGO_CFLAGS", "CFLAGS", "CXXFLAGS", "LDFLAGS",
		"BUNDLE_PATH", "CC", "CXX", "LD_PRELOAD",
		"DYLD_INSERT_LIBRARIES", "SSH_AUTH_SOCK",
		"AWS_ACCESS_KEY_ID", "GITHUB_TOKEN",
	}
	for _, key := range dangerous {
		if safeEnvKeys[key] {
			t.Errorf("%q should not be in safeEnvKeys", key)
		}
	}
}

func TestSafeEnvKeysIncludesExpected(t *testing.T) {
	expected := []string{
		"GOPATH", "GOROOT", "GOPROXY", "CGO_ENABLED",
		"RUSTUP_HOME", "CARGO_HOME", "NODE_ENV",
		"VIRTUAL_ENV", "JAVA_HOME", "GEM_HOME",
		"EDITOR", "VISUAL",
	}
	for _, key := range expected {
		if !safeEnvKeys[key] {
			t.Errorf("%q should be in safeEnvKeys", key)
		}
	}
}

// ── credentialDenySubs sync check ──────────────────────────────────────────

func TestCredentialDenySubsMatchSBPL(t *testing.T) {
	// Verify that credentialDenySubs matches what generateSBPL uses.
	// If someone updates the seatbelt deny list without updating pack.go,
	// this test catches it.
	cfg := sessionConfig{ProjectDir: "/tmp/test"}
	policy := generateSBPL(cfg)

	for _, sub := range credentialDenySubs {
		want := `(deny file-read* file-write* (subpath "` + agentHome + sub + `"))`
		if !strings.Contains(policy, want) {
			t.Errorf("credentialDenySubs has %q but generateSBPL does not produce deny rule for it", sub)
		}
	}
}
