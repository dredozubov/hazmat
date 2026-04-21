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

// ── validateIntegrationSchema ─────────────────────────────────────────────────────

func TestValidateIntegrationSchemaMinimal(t *testing.T) {
	p := IntegrationSpec{
		Meta: IntegrationMeta{Name: "test", Version: 1},
	}
	if err := validateIntegrationSchema(p); err != nil {
		t.Fatalf("minimal valid integration failed: %v", err)
	}
}

func TestValidateIntegrationSchemaMissingName(t *testing.T) {
	p := IntegrationSpec{Meta: IntegrationMeta{Version: 1}}
	if err := validateIntegrationSchema(p); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateIntegrationSchemaInvalidName(t *testing.T) {
	for _, name := range []string{"Test", "foo_bar", "-start", "has space"} {
		p := IntegrationSpec{Meta: IntegrationMeta{Name: name, Version: 1}}
		if err := validateIntegrationSchema(p); err == nil {
			t.Fatalf("expected error for invalid name %q", name)
		}
	}
}

func TestValidateIntegrationSchemaWrongVersion(t *testing.T) {
	p := IntegrationSpec{Meta: IntegrationMeta{Name: "test", Version: 2}}
	if err := validateIntegrationSchema(p); err == nil {
		t.Fatal("expected error for version != 1")
	}
}

func TestValidateIntegrationSchemaUnsafeEnvKey(t *testing.T) {
	p := IntegrationSpec{
		Meta:    IntegrationMeta{Name: "test", Version: 1},
		Session: IntegrationSession{EnvPassthrough: []string{"NODE_OPTIONS"}},
	}
	if err := validateIntegrationSchema(p); err == nil {
		t.Fatal("expected error for unsafe env key NODE_OPTIONS")
	}
}

func TestValidateIntegrationSchemaSafeEnvKey(t *testing.T) {
	p := IntegrationSpec{
		Meta:    IntegrationMeta{Name: "test", Version: 1},
		Session: IntegrationSession{EnvPassthrough: []string{"GOPATH", "NODE_ENV"}},
	}
	if err := validateIntegrationSchema(p); err != nil {
		t.Fatalf("safe env keys rejected: %v", err)
	}
}

func TestValidateIntegrationSchemaNegationExclude(t *testing.T) {
	p := IntegrationSpec{
		Meta:   IntegrationMeta{Name: "test", Version: 1},
		Backup: IntegrationBackup{Excludes: []string{"!important/"}},
	}
	if err := validateIntegrationSchema(p); err == nil {
		t.Fatal("expected error for negation exclude")
	}
}

func TestValidateIntegrationSchemaDetectFileWithPath(t *testing.T) {
	p := IntegrationSpec{
		Meta:   IntegrationMeta{Name: "test", Version: 1},
		Detect: IntegrationDetect{Files: []string{"src/main.go"}},
	}
	if err := validateIntegrationSchema(p); err == nil {
		t.Fatal("expected error for detect file with path separator")
	}
}

// ── validateIntegrationPaths (V2) ─────────────────────────────────────────────────

func TestValidateIntegrationPathsCredentialDirRejected(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	sshDir := filepath.Join(home, ".ssh")
	if _, err := os.Stat(sshDir); err != nil {
		t.Skipf("%s does not exist", sshDir)
	}

	p := IntegrationSpec{
		Meta:    IntegrationMeta{Name: "bad", Version: 1},
		Session: IntegrationSession{ReadDirs: []string{"~/.ssh"}},
	}
	if _, err := validateIntegrationPaths(p); err == nil {
		t.Fatal("expected error for ~/.ssh in read_dirs")
	}
}

func TestValidateIntegrationPathsSymlinkToCredentialRejected(t *testing.T) {
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

	p := IntegrationSpec{
		Meta:    IntegrationMeta{Name: "bad", Version: 1},
		Session: IntegrationSession{ReadDirs: []string{link}},
	}
	if _, err := validateIntegrationPaths(p); err == nil {
		t.Fatal("expected error for symlink pointing to ~/.ssh")
	}
}

func TestValidateIntegrationPathsSafeDirAccepted(t *testing.T) {
	dir := t.TempDir()
	p := IntegrationSpec{
		Meta:    IntegrationMeta{Name: "ok", Version: 1},
		Session: IntegrationSession{ReadDirs: []string{dir}},
	}
	canonical, err := validateIntegrationPaths(p)
	if err != nil {
		t.Fatalf("safe dir rejected: %v", err)
	}
	if len(canonical) != 1 {
		t.Fatalf("expected 1 canonical path, got %d", len(canonical))
	}
}

func TestValidateIntegrationPathsNonExistentSkipped(t *testing.T) {
	p := IntegrationSpec{
		Meta:    IntegrationMeta{Name: "ok", Version: 1},
		Session: IntegrationSession{ReadDirs: []string{"/nonexistent/path/xyz"}},
	}
	canonical, err := validateIntegrationPaths(p)
	if err != nil {
		t.Fatalf("non-existent path should be skipped, not error: %v", err)
	}
	if len(canonical) != 0 {
		t.Fatalf("non-existent path should be skipped, got %v", canonical)
	}
}

// ── loadIntegrationSpec ───────────────────────────────────────────────────────────────

func TestLoadIntegrationSpecValid(t *testing.T) {
	data := []byte(`
integration:
  name: test-integration
  version: 1
  description: A test integration
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
	p, err := loadIntegrationSpec(data)
	if err != nil {
		t.Fatalf("loadIntegrationSpec failed: %v", err)
	}
	if p.Meta.Name != "test-integration" {
		t.Fatalf("name = %q, want test-integration", p.Meta.Name)
	}
	if len(p.Session.ReadDirs) != 1 || p.Session.ReadDirs[0] != "/tmp" {
		t.Fatalf("read_dirs = %v, want [/tmp]", p.Session.ReadDirs)
	}
}

func TestLoadIntegrationSpecTooLarge(t *testing.T) {
	data := make([]byte, integrationMaxSize+1)
	if _, err := loadIntegrationSpec(data); err == nil {
		t.Fatal("expected error for oversized manifest")
	}
}

func TestLoadIntegrationSpecInvalidYAML(t *testing.T) {
	if _, err := loadIntegrationSpec([]byte("{{{")); err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadIntegrationSpecRejectsUnknownFields(t *testing.T) {
	data := []byte(`
integration:
  name: test
  version: 1
  unknown_field: oops
`)
	if _, err := loadIntegrationSpec(data); err == nil {
		t.Fatal("expected error for unknown field in integration section")
	}
}

func TestLoadIntegrationSpecRejectsUnknownTopLevelKey(t *testing.T) {
	data := []byte(`
integration:
  name: test
  version: 1
network:
  allow_ssh: true
`)
	if _, err := loadIntegrationSpec(data); err == nil {
		t.Fatal("expected error for unknown top-level key")
	}
}

// ── Built-in integrations ────────────────────────────────────────────────

func TestBuiltinIntegrationsLoad(t *testing.T) {
	names := allBuiltinIntegrationNames()
	if len(names) == 0 {
		t.Fatal("no built-in integrations found")
	}
	for _, name := range names {
		p, err := loadBuiltinIntegrationSpec(name)
		if err != nil {
			t.Fatalf("built-in integration %q failed to load: %v", name, err)
		}
		if p.Meta.Name != name {
			t.Errorf("built-in integration %q: internal name = %q (mismatch)", name, p.Meta.Name)
		}
	}
}

func TestBuiltinIntegrationsSchemaValid(t *testing.T) {
	for _, name := range allBuiltinIntegrationNames() {
		p, err := loadBuiltinIntegrationSpec(name)
		if err != nil {
			t.Fatalf("load %q: %v", name, err)
		}
		if err := validateIntegrationSchema(p); err != nil {
			t.Fatalf("built-in integration %q schema invalid: %v", name, err)
		}
	}
}

// ── mergeIntegrations ─────────────────────────────────────────────────────────────

func TestMergeIntegrationsDeduplicates(t *testing.T) {
	dir := t.TempDir()
	integrations := []IntegrationSpec{
		{
			Meta:     IntegrationMeta{Name: "a", Version: 1},
			Session:  IntegrationSession{ReadDirs: []string{dir}},
			Backup:   IntegrationBackup{Excludes: []string{"vendor/"}},
			Warnings: []string{"warning one"},
		},
		{
			Meta:     IntegrationMeta{Name: "b", Version: 1},
			Session:  IntegrationSession{ReadDirs: []string{dir}}, // duplicate
			Backup:   IntegrationBackup{Excludes: []string{"vendor/", ".next/"}},
			Warnings: []string{"warning one", "warning two"}, // one duplicate
		},
	}

	result, err := mergeIntegrations(integrations)
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

func TestMergeIntegrationsResolvesEnv(t *testing.T) {
	t.Setenv("GOPATH", "/test/gopath")
	integrations := []IntegrationSpec{
		{
			Meta:    IntegrationMeta{Name: "go", Version: 1},
			Session: IntegrationSession{EnvPassthrough: []string{"GOPATH", "GOPRIVATE"}},
		},
	}

	result, err := mergeIntegrations(integrations)
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

func TestMergeIntegrationsSurfacesRegistryKeys(t *testing.T) {
	t.Setenv("GOPROXY", "https://proxy.example.com")
	integrations := []IntegrationSpec{
		{
			Meta:    IntegrationMeta{Name: "go", Version: 1},
			Session: IntegrationSession{EnvPassthrough: []string{"GOPROXY", "GOPATH"}},
		},
	}

	result, err := mergeIntegrations(integrations)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RegistryKeys) != 1 || result.RegistryKeys[0] != "GOPROXY" {
		t.Errorf("RegistryKeys = %v, want [GOPROXY]", result.RegistryKeys)
	}
}

// ── suggestIntegrations ───────────────────────────────────────────────────────────

func TestSuggestIntegrationsMatchesDetectFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestIntegrations(dir, nil)
	found := false
	for _, s := range suggestions {
		if s == "go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'go' integration suggestion for dir with go.mod, got %v", suggestions)
	}
}

func TestSuggestIntegrationsMatchesNestedDetectFiles(t *testing.T) {
	dir := t.TempDir()
	frontendDir := filepath.Join(dir, "frontend")
	if err := os.MkdirAll(frontendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(frontendDir, "package.json"), []byte(`{"name":"ui"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestIntegrations(dir, nil)
	found := false
	for _, s := range suggestions {
		if s == "node" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'node' integration suggestion for nested frontend/package.json, got %v", suggestions)
	}
}

func TestSuggestIntegrationsSkipsAuxiliaryDocsSiteMarkers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), []byte("version = 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	docsSiteDir := filepath.Join(dir, "docs-site")
	if err := os.MkdirAll(docsSiteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsSiteDir, "package.json"), []byte(`{"name":"docs-site"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestIntegrations(dir, nil)
	for _, s := range suggestions {
		if s == "node" {
			t.Fatalf("did not expect node suggestion from auxiliary docs-site/package.json, got %v", suggestions)
		}
	}
	found := false
	for _, s := range suggestions {
		if s == "python-uv" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected python-uv suggestion from root uv.lock, got %v", suggestions)
	}
}

func TestSuggestIntegrationsSkipsAuxiliaryPlaygroundAndScriptsMarkers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[workspace]"), 0o644); err != nil {
		t.Fatal(err)
	}
	playgroundDir := filepath.Join(dir, "playground")
	if err := os.MkdirAll(playgroundDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(playgroundDir, "package.json"), []byte(`{"name":"playground"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "uv.lock"), []byte("version = 1"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestIntegrations(dir, nil)
	for _, s := range suggestions {
		if s == "node" || s == "python-uv" {
			t.Fatalf("did not expect auxiliary suggestions from playground/scripts markers, got %v", suggestions)
		}
	}
	found := false
	for _, s := range suggestions {
		if s == "rust" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected rust suggestion from root Cargo.toml, got %v", suggestions)
	}
}

func TestSuggestIntegrationsMatchesPreferredNestedRustDirs(t *testing.T) {
	dir := t.TempDir()
	cratesDir := filepath.Join(dir, "crates", "core")
	if err := os.MkdirAll(cratesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cratesDir, "Cargo.toml"), []byte("[package]\nname = \"core\""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"root"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestIntegrations(dir, nil)
	foundNode := false
	foundRust := false
	for _, s := range suggestions {
		if s == "node" {
			foundNode = true
		}
		if s == "rust" {
			foundRust = true
		}
	}
	if !foundNode || !foundRust {
		t.Fatalf("expected node and rust suggestions for root package.json plus crates/* Cargo.toml, got %v", suggestions)
	}
}

func TestSuggestIntegrationsMatchesWildcardDetectFiles(t *testing.T) {
	dir := t.TempDir()
	tlaDir := filepath.Join(dir, "tla")
	if err := os.MkdirAll(tlaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tlaDir, "ClusterAggregation.cfg"), []byte("SPECIFICATION Spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tlaDir, "ClusterAggregation.tla"), []byte("---- MODULE ClusterAggregation ----"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestIntegrations(dir, nil)
	found := false
	for _, s := range suggestions {
		if s == "tla-java" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'tla-java' integration suggestion for nested .cfg/.tla pair, got %v", suggestions)
	}
}

func TestSuggestIntegrationsJavaBuildMarkersMustBeAtProjectRoot(t *testing.T) {
	dir := t.TempDir()
	fixtureDir := filepath.Join(dir, "subprojects", "fixtures")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "build.gradle.kts"), []byte("plugins {}"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestIntegrations(dir, nil)
	for _, s := range suggestions {
		if s == "java-gradle" || s == "java-maven" {
			t.Fatalf("did not expect Java build integration suggestion from nested fixture files, got %v", suggestions)
		}
	}
}

func TestSuggestIntegrationsJavaBuildMarkersMatchAtProjectRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.gradle.kts"), []byte("rootProject.name = \"demo\""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestIntegrations(dir, nil)
	foundGradle := false
	foundMaven := false
	for _, s := range suggestions {
		if s == "java-gradle" {
			foundGradle = true
		}
		if s == "java-maven" {
			foundMaven = true
		}
	}
	if !foundGradle || !foundMaven {
		t.Fatalf("expected both Java build integrations from project-root markers, got %v", suggestions)
	}
}

func TestSuggestIntegrationsSkipsGenericCfgWithoutTLA(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "app.cfg"), []byte("debug=true"), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestIntegrations(dir, nil)
	for _, s := range suggestions {
		if s == "tla-java" {
			t.Fatalf("did not expect tla-java suggestion from generic cfg without sibling TLA spec, got %v", suggestions)
		}
	}
}

func TestSuggestIntegrationsSkipsIgnoredDirectories(t *testing.T) {
	dir := t.TempDir()
	nodeModulesDir := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(nodeModulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeModulesDir, "package.json"), []byte(`{"name":"ignored"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	suggestions := suggestIntegrations(dir, nil)
	for _, s := range suggestions {
		if s == "node" {
			t.Fatalf("did not expect node suggestion from ignored node_modules dir, got %v", suggestions)
		}
	}
}

func TestSuggestIntegrationsSkipsActive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}

	active := map[string]struct{}{"go": {}}
	suggestions := suggestIntegrations(dir, active)
	for _, s := range suggestions {
		if s == "go" {
			t.Error("'go' integration should not be suggested when already active")
		}
	}
}

func TestSuggestIntegrationsNoMatchReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	suggestions := suggestIntegrations(dir, nil)
	if len(suggestions) != 0 {
		t.Errorf("expected no suggestions for empty dir, got %v", suggestions)
	}
}

// ── repo recommendations ───────────────────────────────────────────────────

func TestLoadRepoRecommendationsValid(t *testing.T) {
	dir := t.TempDir()
	recDir := filepath.Join(dir, ".hazmat")
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recDir, "integrations.yaml"),
		[]byte("integrations:\n  - go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	names, hash, err := loadRepoRecommendations(dir)
	if err != nil {
		t.Fatalf("loadRepoRecommendations: %v", err)
	}
	if len(names) != 1 || names[0] != "go" {
		t.Fatalf("names = %v, want [go]", names)
	}
	if hash == "" {
		t.Fatal("hash should not be empty")
	}
}

func TestLoadRepoRecommendationsNoFile(t *testing.T) {
	dir := t.TempDir()
	names, hash, err := loadRepoRecommendations(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if names != nil || hash != "" {
		t.Fatalf("expected nil/empty for missing file, got %v / %q", names, hash)
	}
}

func TestLoadRepoRecommendationsRejectsLegacyRepoFile(t *testing.T) {
	dir := t.TempDir()
	recDir := filepath.Join(dir, ".hazmat")
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recDir, "packs.yaml"),
		[]byte("packs:\n  - go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := loadRepoRecommendations(dir)
	if err == nil {
		t.Fatal("expected legacy repo recommendations file to be rejected")
	}
	if !strings.Contains(err.Error(), "rename .hazmat/packs.yaml to .hazmat/integrations.yaml") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRepoRecommendationsUnknownIntegration(t *testing.T) {
	dir := t.TempDir()
	recDir := filepath.Join(dir, ".hazmat")
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recDir, "integrations.yaml"),
		[]byte("integrations:\n  - nonexistent-integration-xyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := loadRepoRecommendations(dir)
	if err == nil {
		t.Fatal("expected error for unknown integration name")
	}
}

func TestLoadRepoRecommendationsRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	recDir := filepath.Join(dir, ".hazmat")
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recDir, "integrations.yaml"),
		[]byte("integrations:\n  - go\nread_dirs:\n  - /tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := loadRepoRecommendations(dir)
	if err == nil {
		t.Fatal("expected error for unknown field in recommendations file")
	}
}

func TestLoadRepoRecommendationsRejectsInlineDefinitions(t *testing.T) {
	dir := t.TempDir()
	recDir := filepath.Join(dir, ".hazmat")
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Even if the YAML is structurally valid, unknown keys are rejected.
	if err := os.WriteFile(filepath.Join(recDir, "integrations.yaml"),
		[]byte("integrations:\n  - go\nsession:\n  env_passthrough: [SSH_AUTH_SOCK]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := loadRepoRecommendations(dir)
	if err == nil {
		t.Fatal("expected error for inline session config in recommendations file")
	}
}

// ── approval records ──────────────────────────────────────────────────────

func TestApprovalRoundTrip(t *testing.T) {
	saved := integrationApprovalsFilePath
	integrationApprovalsFilePath = filepath.Join(t.TempDir(), "approvals.yaml")
	t.Cleanup(func() { integrationApprovalsFilePath = saved })

	if isApproved("/test/project", "abc123") {
		t.Fatal("should not be approved before recording")
	}

	if err := recordApproval("/test/project", "abc123"); err != nil {
		t.Fatal(err)
	}

	if !isApproved("/test/project", "abc123") {
		t.Fatal("should be approved after recording")
	}
}

func TestApprovalInvalidatedOnHashChange(t *testing.T) {
	saved := integrationApprovalsFilePath
	integrationApprovalsFilePath = filepath.Join(t.TempDir(), "approvals.yaml")
	t.Cleanup(func() { integrationApprovalsFilePath = saved })

	if err := recordApproval("/test/project", "hash1"); err != nil {
		t.Fatal(err)
	}
	if !isApproved("/test/project", "hash1") {
		t.Fatal("should be approved with original hash")
	}
	if isApproved("/test/project", "hash2") {
		t.Fatal("should NOT be approved with different hash")
	}
}

func TestApprovalReplacesStaleEntry(t *testing.T) {
	saved := integrationApprovalsFilePath
	integrationApprovalsFilePath = filepath.Join(t.TempDir(), "approvals.yaml")
	t.Cleanup(func() { integrationApprovalsFilePath = saved })

	if err := recordApproval("/test/project", "hash1"); err != nil {
		t.Fatal(err)
	}
	if err := recordApproval("/test/project", "hash2"); err != nil {
		t.Fatal(err)
	}

	// Old hash should no longer be approved.
	if isApproved("/test/project", "hash1") {
		t.Fatal("old hash should be replaced")
	}
	// New hash should be approved.
	if !isApproved("/test/project", "hash2") {
		t.Fatal("new hash should be approved")
	}

	// Should have exactly one entry for this project.
	af := loadIntegrationApprovals()
	count := 0
	for _, rec := range af.Approvals {
		if rec.ProjectDir == "/test/project" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 approval entry, got %d", count)
	}
}

func TestApprovalDifferentProjectsIndependent(t *testing.T) {
	saved := integrationApprovalsFilePath
	integrationApprovalsFilePath = filepath.Join(t.TempDir(), "approvals.yaml")
	t.Cleanup(func() { integrationApprovalsFilePath = saved })

	if err := recordApproval("/project/a", "hashA"); err != nil {
		t.Fatal(err)
	}
	if err := recordApproval("/project/b", "hashB"); err != nil {
		t.Fatal(err)
	}

	if !isApproved("/project/a", "hashA") {
		t.Fatal("project a should be approved")
	}
	if !isApproved("/project/b", "hashB") {
		t.Fatal("project b should be approved")
	}
	if isApproved("/project/a", "hashB") {
		t.Fatal("project a with project b's hash should not be approved")
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
		"VIRTUAL_ENV", "JAVA_HOME", "TLA2TOOLS_JAR", "GEM_HOME",
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
	// Verify that credentialDenySubs matches the deny rules emitted by the
	// current Darwin compiler. If someone updates the policy model without
	// updating integration_manifest.go, this test catches it.
	cfg := sessionConfig{ProjectDir: "/tmp/test"}
	policy := generateSBPL(cfg)

	for _, sub := range credentialDenySubs {
		want := `(deny file-read* file-write* (subpath "` + agentHome + sub + `"))`
		if !strings.Contains(policy, want) {
			t.Errorf("credentialDenySubs has %q but generateSBPL does not produce deny rule for it", sub)
		}
	}
}
