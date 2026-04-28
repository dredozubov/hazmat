package main

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestValidateIntegrationSchemaRejectsCredentialEnvKey(t *testing.T) {
	for _, key := range []string{"GITHUB_TOKEN", "OPENAI_API_KEY", "AWS_ACCESS_KEY_ID", "SSH_AUTH_SOCK"} {
		p := IntegrationSpec{
			Meta:    IntegrationMeta{Name: "test", Version: 1},
			Session: IntegrationSession{EnvPassthrough: []string{key}},
		}
		err := validateIntegrationSchema(p)
		if err == nil {
			t.Fatalf("expected error for credential env key %s", key)
		}
		if !strings.Contains(err.Error(), "credential/capability-shaped") {
			t.Fatalf("unexpected error for %s: %v", key, err)
		}
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

func TestValidateIntegrationSchemaPlatformOverlay(t *testing.T) {
	p := IntegrationSpec{
		Meta: IntegrationMeta{Name: "test", Version: 1},
		Session: IntegrationSession{
			EnvPassthrough: []string{"JAVA_HOME"},
			Platforms: map[string]IntegrationPlatformSession{
				"darwin": {
					ReadDirs:       []string{"/Library/Java"},
					EnvPassthrough: []string{"TLA2TOOLS_JAR"},
				},
				"linux": {
					ReadDirs: []string{"/usr/lib/jvm"},
				},
			},
		},
	}
	if err := validateIntegrationSchema(p); err != nil {
		t.Fatalf("platform overlay rejected: %v", err)
	}
}

func TestValidateIntegrationSchemaRejectsUnsupportedPlatform(t *testing.T) {
	p := IntegrationSpec{
		Meta: IntegrationMeta{Name: "test", Version: 1},
		Session: IntegrationSession{
			Platforms: map[string]IntegrationPlatformSession{
				"windows": {ReadDirs: []string{`C:\Tools`}},
			},
		},
	}
	if err := validateIntegrationSchema(p); err == nil {
		t.Fatal("expected unsupported platform overlay to be rejected")
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

func TestValidateIntegrationPathsUsesPlatformOverlay(t *testing.T) {
	commonDir := filepath.Join(t.TempDir(), "common")
	darwinDir := filepath.Join(t.TempDir(), "darwin")
	linuxDir := filepath.Join(t.TempDir(), "linux")
	for _, dir := range []string{commonDir, darwinDir, linuxDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	commonCanonical, err := canonicalizePath(commonDir)
	if err != nil {
		t.Fatalf("canonicalize common: %v", err)
	}
	darwinCanonical, err := canonicalizePath(darwinDir)
	if err != nil {
		t.Fatalf("canonicalize darwin: %v", err)
	}
	linuxCanonical, err := canonicalizePath(linuxDir)
	if err != nil {
		t.Fatalf("canonicalize linux: %v", err)
	}

	p := IntegrationSpec{
		Meta: IntegrationMeta{Name: "ok", Version: 1},
		Session: IntegrationSession{
			ReadDirs: []string{commonDir},
			Platforms: map[string]IntegrationPlatformSession{
				"darwin": {ReadDirs: []string{darwinDir}},
				"linux":  {ReadDirs: []string{linuxDir}},
			},
		},
	}

	darwinPaths, err := validateIntegrationPathsForPlatform(p, "darwin")
	if err != nil {
		t.Fatalf("validate darwin paths: %v", err)
	}
	if len(darwinPaths) != 2 || darwinPaths[0] != commonCanonical || darwinPaths[1] != darwinCanonical {
		t.Fatalf("darwin paths = %v, want [%q %q]", darwinPaths, commonCanonical, darwinCanonical)
	}

	linuxPaths, err := validateIntegrationPathsForPlatform(p, "linux")
	if err != nil {
		t.Fatalf("validate linux paths: %v", err)
	}
	if len(linuxPaths) != 2 || linuxPaths[0] != commonCanonical || linuxPaths[1] != linuxCanonical {
		t.Fatalf("linux paths = %v, want [%q %q]", linuxPaths, commonCanonical, linuxCanonical)
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
  platforms:
    darwin:
      read_dirs: [/Library/Java]
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
	if got := p.Session.Platforms["darwin"].ReadDirs; len(got) != 1 || got[0] != "/Library/Java" {
		t.Fatalf("darwin read_dirs = %v, want [/Library/Java]", got)
	}
}

func TestLoadIntegrationSpecAuthorKitTemplate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "docs", "examples", "integration-template.yaml"))
	if err != nil {
		t.Fatalf("read author-kit template: %v", err)
	}

	p, err := loadIntegrationSpec(data)
	if err != nil {
		t.Fatalf("loadIntegrationSpec failed for author-kit template: %v", err)
	}

	if p.Meta.Name != "example-stack" {
		t.Fatalf("name = %q, want example-stack", p.Meta.Name)
	}
	if got := p.Commands["test"]; got != "example-tool test" {
		t.Fatalf("commands.test = %q, want example-tool test", got)
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

func TestMergeResolvedIntegrationsUsesPlatformEnvOverlay(t *testing.T) {
	t.Setenv("JAVA_HOME", "/tmp/jdk")
	t.Setenv("TLA2TOOLS_JAR", "/tmp/tla2tools.jar")

	result, err := mergeResolvedIntegrationsForPlatform([]resolvedIntegration{
		{
			Spec: IntegrationSpec{
				Meta: IntegrationMeta{Name: "tla-java", Version: 1},
				Session: IntegrationSession{
					EnvPassthrough: []string{"JAVA_HOME"},
					Platforms: map[string]IntegrationPlatformSession{
						"darwin": {EnvPassthrough: []string{"TLA2TOOLS_JAR"}},
					},
				},
			},
		},
	}, "darwin")
	if err != nil {
		t.Fatalf("mergeResolvedIntegrationsForPlatform: %v", err)
	}
	if result.EnvPassthrough["JAVA_HOME"] != "/tmp/jdk" {
		t.Fatalf("JAVA_HOME = %q", result.EnvPassthrough["JAVA_HOME"])
	}
	if result.EnvPassthrough["TLA2TOOLS_JAR"] != "/tmp/tla2tools.jar" {
		t.Fatalf("TLA2TOOLS_JAR = %q", result.EnvPassthrough["TLA2TOOLS_JAR"])
	}

	result, err = mergeResolvedIntegrationsForPlatform([]resolvedIntegration{
		{
			Spec: IntegrationSpec{
				Meta: IntegrationMeta{Name: "tla-java", Version: 1},
				Session: IntegrationSession{
					EnvPassthrough: []string{"JAVA_HOME"},
					Platforms: map[string]IntegrationPlatformSession{
						"darwin": {EnvPassthrough: []string{"TLA2TOOLS_JAR"}},
					},
				},
			},
		},
	}, "linux")
	if err != nil {
		t.Fatalf("mergeResolvedIntegrationsForPlatform linux: %v", err)
	}
	if _, ok := result.EnvPassthrough["TLA2TOOLS_JAR"]; ok {
		t.Fatalf("linux env should not include darwin overlay: %v", result.EnvPassthrough)
	}
}

func TestMergeResolvedIntegrationsRejectsCredentialResolvedEnv(t *testing.T) {
	_, err := mergeResolvedIntegrationsForPlatform([]resolvedIntegration{
		{
			Spec:        IntegrationSpec{Meta: IntegrationMeta{Name: "bad-resolver", Version: 1}},
			ResolvedEnv: map[string]string{"GITHUB_TOKEN": "example-token"},
		},
	}, "darwin")
	if err == nil {
		t.Fatal("expected error for credential-shaped resolved env")
	}
	if !strings.Contains(err.Error(), "credential/capability-shaped") {
		t.Fatalf("unexpected error: %v", err)
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

func TestSuggestIntegrationsMatchesRootDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	suggestions := suggestIntegrations(dir, nil)
	found := false
	for _, s := range suggestions {
		if s == "beads" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'beads' suggestion for dir with .beads/, got %v", suggestions)
	}
}

func TestSuggestIntegrationsRootDirRequiresDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".beads"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	suggestions := suggestIntegrations(dir, nil)
	for _, s := range suggestions {
		if s == "beads" {
			t.Fatalf("did not expect beads suggestion when .beads is a regular file, got %v", suggestions)
		}
	}
}

func TestSuggestIntegrationsRootDirSkippedWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	suggestions := suggestIntegrations(dir, nil)
	for _, s := range suggestions {
		if s == "beads" {
			t.Fatalf("did not expect beads suggestion without .beads/, got %v", suggestions)
		}
	}
}

func TestLoadIntegrationSpecAcceptsRootDir(t *testing.T) {
	data := []byte(`
integration:
  name: test
  version: 1
detect:
  root_dirs: [.beads]
`)
	p, err := loadIntegrationSpec(data)
	if err != nil {
		t.Fatalf("loadIntegrationSpec: %v", err)
	}
	if len(p.Detect.RootDirs) != 1 || p.Detect.RootDirs[0] != ".beads" {
		t.Fatalf("Detect.RootDirs = %v, want [.beads]", p.Detect.RootDirs)
	}
}

func TestLoadIntegrationSpecRejectsRootDirWithSlash(t *testing.T) {
	cases := []string{".beads/sub", "sub/.beads", "/.beads", "foo\\bar"}
	for _, v := range cases {
		data := []byte(`
integration:
  name: test
  version: 1
detect:
  root_dirs: ["` + v + `"]
`)
		if _, err := loadIntegrationSpec(data); err == nil {
			t.Errorf("expected error for root_dir %q", v)
		}
	}
}

func TestLoadIntegrationSpecRejectsRootDirDotOrDotDot(t *testing.T) {
	for _, v := range []string{".", ".."} {
		data := []byte(`
integration:
  name: test
  version: 1
detect:
  root_dirs: ["` + v + `"]
`)
		if _, err := loadIntegrationSpec(data); err == nil {
			t.Errorf("expected error for root_dir %q", v)
		}
	}
}

func TestLoadIntegrationSpecRejectsEmptyRootDir(t *testing.T) {
	data := []byte(`
integration:
  name: test
  version: 1
detect:
  root_dirs: [""]
`)
	if _, err := loadIntegrationSpec(data); err == nil {
		t.Fatal("expected error for empty root_dir")
	}
}

func TestLoadIntegrationSpecRejectsRootDirWithWhitespace(t *testing.T) {
	for _, v := range []string{"hello world", "has\ttab", "trail "} {
		data := []byte(`
integration:
  name: test
  version: 1
detect:
  root_dirs: ["` + v + `"]
`)
		if _, err := loadIntegrationSpec(data); err == nil {
			t.Errorf("expected error for root_dir %q", v)
		}
	}
}

func TestActiveBeadsContributesIntegrationExcludes(t *testing.T) {
	spec, err := loadBuiltinIntegrationSpec("beads")
	if err != nil {
		t.Fatalf("loadBuiltinIntegrationSpec(beads): %v", err)
	}
	merged, err := mergeIntegrations([]IntegrationSpec{spec})
	if err != nil {
		t.Fatalf("mergeIntegrations: %v", err)
	}
	wantPatterns := []string{
		".beads/dolt/",
		".beads/backup/",
		".beads/dolt-server.log",
		".beads/interactions.jsonl",
		".beads/.beads-credential-key",
	}
	have := make(map[string]struct{}, len(merged.Excludes))
	for _, pat := range merged.Excludes {
		have[pat] = struct{}{}
	}
	for _, want := range wantPatterns {
		if _, ok := have[want]; !ok {
			t.Errorf("merged.Excludes missing %q; got %v", want, merged.Excludes)
		}
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
	if !strings.Contains(err.Error(), integrationContributorFlowDocURL) {
		t.Fatalf("error missing contributor flow link: %v", err)
	}
}

func TestRunIntegrationListShowsContributorFlowLink(t *testing.T) {
	isolateConfig(t)

	out, err := captureStdout(t, runIntegrationList)
	if err != nil {
		t.Fatalf("runIntegrationList: %v", err)
	}
	if !strings.Contains(out, "Contribute: missing your stack? "+integrationContributorFlowDocURL) {
		t.Fatalf("output missing contributor flow link:\n%s", out)
	}
	if !strings.Contains(out, "Learn:    "+integrationDocsURL) {
		t.Fatalf("output missing integrations docs link:\n%s", out)
	}
}

func TestRunIntegrationSetupShowsDoorway(t *testing.T) {
	isolateConfig(t)

	projectDir := integrationTestProject(t, map[string]string{
		"package.json": "{}\n",
	})
	out, err := captureStdout(t, func() error {
		return runIntegrationSetup(integrationSetupOptions{Project: projectDir})
	})
	if err != nil {
		t.Fatalf("runIntegrationSetup: %v", err)
	}
	for _, want := range []string{
		"hazmat: integration setup",
		"Suggested built-ins:  node",
		"Recommend in repo:    hazmat integration setup --recommend <name[,name]>",
		"Create draft:         hazmat integration scaffold <name> --from-current-project",
		integrationDocsURL,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("setup output missing %q:\n%s", want, out)
		}
	}
}

func TestRunIntegrationSetupRecommendWritesRepoRecommendations(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	out, err := captureStdout(t, func() error {
		return runIntegrationSetup(integrationSetupOptions{Project: projectDir, Recommend: "node, go node"})
	})
	if err != nil {
		t.Fatalf("runIntegrationSetup recommend: %v", err)
	}
	if !strings.Contains(out, "Recommended integrations: node, go") {
		t.Fatalf("unexpected output:\n%s", out)
	}

	names, _, err := loadRepoRecommendations(projectDir)
	if err != nil {
		t.Fatalf("loadRepoRecommendations: %v", err)
	}
	if !reflect.DeepEqual(names, []string{"node", "go"}) {
		t.Fatalf("names = %v, want [node go]", names)
	}
}

func TestRunIntegrationScaffoldCreatesDraftFromProjectEvidence(t *testing.T) {
	isolateConfig(t)

	projectDir := integrationTestProject(t, map[string]string{
		"bun.lockb":    "lock\n",
		"package.json": "{}\n",
		".gitignore":   "node_modules/\n.env\n.next/\n!keep\n",
	})
	output := filepath.Join(t.TempDir(), "bun.yaml")

	out, err := captureStdout(t, func() error {
		return runIntegrationScaffold("bun", integrationScaffoldOptions{
			Project:            projectDir,
			Output:             output,
			FromCurrentProject: true,
		})
	})
	if err != nil {
		t.Fatalf("runIntegrationScaffold: %v", err)
	}
	if !strings.Contains(out, "Created integration draft: "+output) {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if !strings.Contains(out, "Contributor flow: "+integrationContributorFlowDocURL) {
		t.Fatalf("scaffold output missing contributor flow link:\n%s", out)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read scaffold: %v", err)
	}
	spec, err := loadIntegrationSpec(data)
	if err != nil {
		t.Fatalf("load scaffold: %v\n%s", err, data)
	}
	if spec.Meta.Name != "bun" {
		t.Fatalf("name = %q, want bun", spec.Meta.Name)
	}
	if !reflect.DeepEqual(spec.Detect.Files, []string{"bun.lockb", "package.json"}) {
		t.Fatalf("detect files = %v, want [bun.lockb package.json]", spec.Detect.Files)
	}
	if !reflect.DeepEqual(spec.Backup.Excludes, []string{"node_modules/", ".next/"}) {
		t.Fatalf("excludes = %v, want [node_modules/ .next/]", spec.Backup.Excludes)
	}
}

func TestRunIntegrationScaffoldRejectsExistingOutputWithoutForce(t *testing.T) {
	isolateConfig(t)

	output := filepath.Join(t.TempDir(), "demo.yaml")
	if err := os.WriteFile(output, []byte("existing\n"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	err := runIntegrationScaffold("demo", integrationScaffoldOptions{
		Output:             output,
		FromCurrentProject: false,
	})
	if err == nil {
		t.Fatal("expected existing output to be rejected")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunIntegrationValidateAcceptsFile(t *testing.T) {
	isolateConfig(t)

	output := filepath.Join(t.TempDir(), "demo.yaml")
	if err := runIntegrationScaffold("demo", integrationScaffoldOptions{
		Output:             output,
		FromCurrentProject: false,
	}); err != nil {
		t.Fatalf("runIntegrationScaffold: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runIntegrationValidate(output, integrationValidateOptions{})
	})
	if err != nil {
		t.Fatalf("runIntegrationValidate: %v", err)
	}
	if !strings.Contains(out, "Integration manifest valid: demo") {
		t.Fatalf("validate output missing success:\n%s", out)
	}
}

func TestRunIntegrationValidateRejectsUnsafeEnv(t *testing.T) {
	isolateConfig(t)

	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte(`integration:
  name: bad
  version: 1
session:
  env_passthrough: [NODE_OPTIONS]
`), 0o644); err != nil {
		t.Fatalf("write bad manifest: %v", err)
	}

	err := runIntegrationValidate(path, integrationValidateOptions{})
	if err == nil {
		t.Fatal("expected unsafe env passthrough to be rejected")
	}
	if !strings.Contains(err.Error(), "NODE_OPTIONS") {
		t.Fatalf("unexpected error: %v", err)
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

func TestRunIntegrationRejectionsListShowsAllConfiguredProjects(t *testing.T) {
	isolateConfig(t)

	projectA := t.TempDir()
	projectB := t.TempDir()
	canonicalA, err := resolveDir(projectA, false)
	if err != nil {
		t.Fatalf("resolveDir(projectA): %v", err)
	}
	canonicalB, err := resolveDir(projectB, false)
	if err != nil {
		t.Fatalf("resolveDir(projectB): %v", err)
	}

	cfg := defaultConfig()
	cfg.Integrations.Rejected = []IntegrationRejection{
		{ProjectDir: canonicalB, Integrations: []string{"python-uv"}},
		{ProjectDir: canonicalA, Integrations: []string{"node", "tla-java"}},
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runIntegrationRejectionsList("")
	})
	if err != nil {
		t.Fatalf("runIntegrationRejectionsList: %v", err)
	}
	if !strings.Contains(out, "Rejected suggested integrations:\n") {
		t.Fatalf("output missing header:\n%s", out)
	}
	if !strings.Contains(out, canonicalA+": node, tla-java") {
		t.Fatalf("output missing project A:\n%s", out)
	}
	if !strings.Contains(out, canonicalB+": python-uv") {
		t.Fatalf("output missing project B:\n%s", out)
	}
}

func TestRunIntegrationRejectionsListShowsSingleProject(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	canonicalProject, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	cfg := defaultConfig()
	cfg.Integrations.Rejected = []IntegrationRejection{
		{ProjectDir: canonicalProject, Integrations: []string{"node", "python-uv"}},
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runIntegrationRejectionsList(projectDir)
	})
	if err != nil {
		t.Fatalf("runIntegrationRejectionsList: %v", err)
	}
	if !strings.Contains(out, "Rejected suggested integrations for "+canonicalProject+":\n") {
		t.Fatalf("output missing project header:\n%s", out)
	}
	if !strings.Contains(out, "  - node\n") || !strings.Contains(out, "  - python-uv\n") {
		t.Fatalf("output missing rejected integrations:\n%s", out)
	}
}

func TestRunIntegrationRejectionsClearAll(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	canonicalProject, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	cfg := defaultConfig()
	cfg.Integrations.Rejected = []IntegrationRejection{
		{ProjectDir: canonicalProject, Integrations: []string{"node", "python-uv"}},
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runIntegrationRejectionsClear(projectDir, nil)
	})
	if err != nil {
		t.Fatalf("runIntegrationRejectionsClear: %v", err)
	}
	if !strings.Contains(out, "Cleared all rejected suggested integrations for "+canonicalProject) {
		t.Fatalf("unexpected output:\n%s", out)
	}

	updated, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got := updated.ProjectRejectedIntegrations(canonicalProject); len(got) != 0 {
		t.Fatalf("rejected = %v, want empty", got)
	}
}

func TestRunIntegrationRejectionsClearSubset(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	canonicalProject, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}

	cfg := defaultConfig()
	cfg.Integrations.Rejected = []IntegrationRejection{
		{ProjectDir: canonicalProject, Integrations: []string{"node", "python-uv", "tla-java"}},
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runIntegrationRejectionsClear(projectDir, []string{"python-uv", "node"})
	})
	if err != nil {
		t.Fatalf("runIntegrationRejectionsClear: %v", err)
	}
	if !strings.Contains(out, "Cleared rejected suggested integrations for "+canonicalProject+": python-uv, node") &&
		!strings.Contains(out, "Cleared rejected suggested integrations for "+canonicalProject+": node, python-uv") {
		t.Fatalf("unexpected output:\n%s", out)
	}

	updated, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got := updated.ProjectRejectedIntegrations(canonicalProject); !reflect.DeepEqual(got, []string{"tla-java"}) {
		t.Fatalf("rejected = %v, want [tla-java]", got)
	}
}

func TestRunIntegrationRejectionsClearRequiresProject(t *testing.T) {
	isolateConfig(t)

	err := runIntegrationRejectionsClear("", nil)
	if err == nil {
		t.Fatal("expected error when project is missing")
	}
	if !strings.Contains(err.Error(), "project directory is required") {
		t.Fatalf("unexpected error: %v", err)
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
