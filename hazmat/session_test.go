package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestParseHarnessArgsRejectsLegacyPackFlag(t *testing.T) {
	_, _, err := parseHarnessArgs([]string{"--pack", "node"})
	if err == nil {
		t.Fatal("expected legacy --pack flag to be rejected")
	}
	if !strings.Contains(err.Error(), "--pack was removed before v1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunConfigSetRejectsLegacyPackKeys(t *testing.T) {
	isolateConfig(t)

	tests := []struct {
		key   string
		value string
	}{
		{key: "packs.pin", value: "~/workspace/example:node"},
		{key: "packs.unpin", value: "~/workspace/example"},
	}

	for _, tc := range tests {
		err := runConfigSet(tc.key, tc.value)
		if err == nil {
			t.Fatalf("expected %s to be rejected", tc.key)
		}
		if !strings.Contains(err.Error(), "removed before v1") {
			t.Fatalf("unexpected error for %s: %v", tc.key, err)
		}
	}
}

func TestLoadConfigRejectsLegacyPacksSection(t *testing.T) {
	isolateConfig(t)

	if err := os.WriteFile(configFilePath, []byte("packs:\n  pinned: []\n"), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected legacy packs config to be rejected")
	}
	if !strings.Contains(err.Error(), "config key 'packs' was removed before v1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCodexSkipPermissionsArgs(t *testing.T) {
	got := codexSkipPermissionsArgs()
	if len(got) != 1 {
		t.Fatalf("codexSkipPermissionsArgs() returned %d args, want 1: %v", len(got), got)
	}
	if got[0] != "--dangerously-bypass-approvals-and-sandbox" {
		t.Fatalf("codexSkipPermissionsArgs() = %v, want --dangerously-bypass-approvals-and-sandbox", got)
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

	cfg, err := resolveSessionConfig(projectDir, []string{readDir}, nil)
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

	cfg, err := resolveSessionConfig(projectDir, nil, nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	if len(cfg.ReadDirs) != 0 {
		t.Errorf("ReadDirs = %v, want empty", cfg.ReadDirs)
	}
}

func TestResolveSessionConfigUsesConfiguredBackupExcludes(t *testing.T) {
	projectDir := t.TempDir()

	savedCfg := configFilePath
	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	configFilePath = cfgFile
	t.Cleanup(func() { configFilePath = savedCfg })

	cfg := defaultConfig()
	cfg.Backup.Excludes = append(cfg.Backup.Excludes, ".terraform/")
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	sessionCfg, err := resolveSessionConfig(projectDir, nil, nil)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}

	found := false
	for _, pat := range sessionCfg.BackupExcludes {
		if pat == ".terraform/" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("BackupExcludes = %v, want to contain .terraform/", sessionCfg.BackupExcludes)
	}
}

func TestResolveSessionConfigProjectAnywhere(t *testing.T) {
	projectDir := t.TempDir()

	cfg, err := resolveSessionConfig(projectDir, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want, _ := filepath.EvalSymlinks(projectDir)
	if cfg.ProjectDir != want {
		t.Errorf("ProjectDir = %q, want %q", cfg.ProjectDir, want)
	}
}

func TestResolveSessionConfigRejectsProjectCredentialDenyPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}

	_, err = resolveSessionConfig(home, nil, nil)
	if err == nil {
		t.Fatal("expected credential deny project dir to be rejected")
	}
	if !strings.Contains(err.Error(), "credential deny zone") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSessionConfigRejectsReadCredentialDenyPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", projectDir, err)
	}

	_, err = resolveSessionConfig(projectDir, []string{home}, nil)
	if err == nil {
		t.Fatal("expected credential deny read dir to be rejected")
	}
	if !strings.Contains(err.Error(), "credential deny zone") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSessionConfigRejectsWriteCredentialDenyPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", projectDir, err)
	}

	_, err = resolveSessionConfig(projectDir, nil, []string{home})
	if err == nil {
		t.Fatal("expected credential deny write dir to be rejected")
	}
	if !strings.Contains(err.Error(), "credential deny zone") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── defaultReadDirs ─────────────────────────────────────────────────────────

func isolateConfig(t *testing.T) {
	t.Helper()
	savedCfg := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "nonexistent.yaml")
	t.Cleanup(func() { configFilePath = savedCfg })
}

func isolateApprovals(t *testing.T) {
	t.Helper()
	saved := sandboxApprovalsFilePath
	sandboxApprovalsFilePath = filepath.Join(t.TempDir(), "sandbox-approvals.yaml")
	t.Cleanup(func() { sandboxApprovalsFilePath = saved })
}

func autoApprove(t *testing.T) {
	t.Helper()
	saved := flagYesAll
	flagYesAll = true
	t.Cleanup(func() { flagYesAll = saved })
}

func skipInitCheck(t *testing.T) {
	t.Helper()
	saved := requireInit
	requireInit = func() error { return nil }
	t.Cleanup(func() { requireInit = saved })
}

func autoDockerRequest() dockerRoutingRequest {
	return dockerRoutingRequest{Mode: dockerModeAuto, Source: dockerRequestFlag}
}

func defaultDockerRequest() dockerRoutingRequest {
	return dockerRoutingRequest{Mode: dockerModeNone, Source: dockerRequestDefault}
}

func noneDockerRequest(source dockerRequestSource) dockerRoutingRequest {
	return dockerRoutingRequest{Mode: dockerModeNone, Source: source}
}

func sandboxDockerRequest(source dockerRequestSource) dockerRoutingRequest {
	return dockerRoutingRequest{Mode: dockerModeSandbox, Source: source}
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

func TestStatusBarDisabledByDefault(t *testing.T) {
	isolateConfig(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.StatusBar() {
		t.Fatal("StatusBar should default to false")
	}
}

func TestRunConfigSetSessionStatusBar(t *testing.T) {
	isolateConfig(t)

	if err := runConfigSet("session.status_bar", "true"); err != nil {
		t.Fatalf("runConfigSet enable: %v", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after enable: %v", err)
	}
	if !cfg.StatusBar() {
		t.Fatal("StatusBar should be true after enabling")
	}

	if err := runConfigSet("session.status_bar", "false"); err != nil {
		t.Fatalf("runConfigSet disable: %v", err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after disable: %v", err)
	}
	if cfg.StatusBar() {
		t.Fatal("StatusBar should be false after disabling")
	}
}

func TestRunConfigDockerPersistsProjectMode(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	canonicalProjectDir, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}
	if err := runConfigDocker(projectDir, "none"); err != nil {
		t.Fatalf("runConfigDocker none: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after none: %v", err)
	}
	mode, ok := cfg.ProjectDockerMode(canonicalProjectDir)
	if !ok {
		t.Fatal("ProjectDockerMode should be configured")
	}
	if mode != dockerModeNone {
		t.Fatalf("ProjectDockerMode = %q, want %q", mode, dockerModeNone)
	}

	if err := runConfigDocker(projectDir, "auto"); err != nil {
		t.Fatalf("runConfigDocker auto: %v", err)
	}

	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after auto: %v", err)
	}
	mode, ok = cfg.ProjectDockerMode(canonicalProjectDir)
	if !ok {
		t.Fatal("ProjectDockerMode should be configured after auto")
	}
	if mode != dockerModeAuto {
		t.Fatalf("ProjectDockerMode = %q, want %q", mode, dockerModeAuto)
	}
}

func TestRunConfigAccessPersistsProjectDirs(t *testing.T) {
	isolateConfig(t)

	projectDir := t.TempDir()
	readDir := t.TempDir()
	writeDir := t.TempDir()
	canonicalReadDir, err := resolveDir(readDir, false)
	if err != nil {
		t.Fatalf("resolveDir read: %v", err)
	}
	canonicalWriteDir, err := resolveDir(writeDir, false)
	if err != nil {
		t.Fatalf("resolveDir write: %v", err)
	}

	if err := runConfigAccess(projectDir, []string{readDir}, []string{writeDir}, false); err != nil {
		t.Fatalf("runConfigAccess add: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after add: %v", err)
	}
	canonicalProjectDir, err := resolveDir(projectDir, false)
	if err != nil {
		t.Fatalf("resolveDir project: %v", err)
	}
	gotRead := cfg.ProjectReadDirs(canonicalProjectDir)
	gotWrite := cfg.ProjectWriteDirs(canonicalProjectDir)
	if len(gotRead) != 1 || gotRead[0] != canonicalReadDir {
		t.Fatalf("ProjectReadDirs = %v, want [%q]", gotRead, canonicalReadDir)
	}
	if len(gotWrite) != 1 || gotWrite[0] != canonicalWriteDir {
		t.Fatalf("ProjectWriteDirs = %v, want [%q]", gotWrite, canonicalWriteDir)
	}

	if err := runConfigAccess(projectDir, []string{canonicalReadDir}, []string{canonicalWriteDir}, true); err != nil {
		t.Fatalf("runConfigAccess remove: %v", err)
	}

	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after remove: %v", err)
	}
	if got := cfg.ProjectReadDirs(canonicalProjectDir); len(got) != 0 {
		t.Fatalf("ProjectReadDirs after remove = %v, want empty", got)
	}
	if got := cfg.ProjectWriteDirs(canonicalProjectDir); len(got) != 0 {
		t.Fatalf("ProjectWriteDirs after remove = %v, want empty", got)
	}
}

func TestApplyStatusBarConfigDisablesBarAndWatcherByDefault(t *testing.T) {
	ui := applyStatusBarConfig(sessionLaunchUI{
		showStatusBar:    true,
		waitForAltScreen: true,
	}, defaultConfig())
	if ui.showStatusBar {
		t.Fatal("showStatusBar should be false when status bar is disabled in config")
	}
	if ui.waitForAltScreen {
		t.Fatal("waitForAltScreen should be false when status bar is disabled in config")
	}
}

func TestApplyStatusBarConfigPreservesBarWhenEnabled(t *testing.T) {
	enabled := true
	ui := applyStatusBarConfig(sessionLaunchUI{
		showStatusBar:    true,
		waitForAltScreen: true,
	}, HazmatConfig{
		Session: SessionConfig{
			StatusBar: &enabled,
		},
	})
	if !ui.showStatusBar {
		t.Fatal("showStatusBar should stay enabled when config opts in")
	}
	if !ui.waitForAltScreen {
		t.Fatal("waitForAltScreen should stay enabled when config opts in")
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

func TestTerminalCapabilitySupportSynthesizesUserTerminfoDir(t *testing.T) {
	home := t.TempDir()
	terminfoDir := filepath.Join(home, ".terminfo")
	if err := os.MkdirAll(terminfoDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", terminfoDir, err)
	}
	canonicalTerminfoDir, err := filepath.EvalSymlinks(terminfoDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", terminfoDir, err)
	}

	pairs, readDirs := terminalCapabilitySupport(home, func(key string) string {
		switch key {
		case "TERM":
			return "xterm-ghostty"
		case "TERM_PROGRAM":
			return "Ghostty"
		default:
			return ""
		}
	})

	values := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			t.Fatalf("malformed env pair: %q", pair)
		}
		values[key] = value
	}

	if values["TERM"] != "xterm-ghostty" {
		t.Fatalf("TERM = %q, want xterm-ghostty", values["TERM"])
	}
	if values["TERM_PROGRAM"] != "Ghostty" {
		t.Fatalf("TERM_PROGRAM = %q, want Ghostty", values["TERM_PROGRAM"])
	}
	wantDirsValue := terminfoDir + string(os.PathListSeparator)
	if values["TERMINFO_DIRS"] != wantDirsValue {
		t.Fatalf("TERMINFO_DIRS = %q, want %q", values["TERMINFO_DIRS"], wantDirsValue)
	}
	if len(readDirs) != 1 || readDirs[0] != canonicalTerminfoDir {
		t.Fatalf("readDirs = %v, want [%q]", readDirs, canonicalTerminfoDir)
	}
}

func TestTerminalCapabilitySupportPreservesExplicitTerminfoEnv(t *testing.T) {
	home := t.TempDir()
	explicitTerminfo := filepath.Join(t.TempDir(), "terminfo-explicit")
	if err := os.MkdirAll(explicitTerminfo, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", explicitTerminfo, err)
	}
	canonicalExplicitTerminfo, err := filepath.EvalSymlinks(explicitTerminfo)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", explicitTerminfo, err)
	}
	listTerminfoA := filepath.Join(t.TempDir(), "terminfo-a")
	if err := os.MkdirAll(listTerminfoA, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", listTerminfoA, err)
	}
	canonicalListTerminfoA, err := filepath.EvalSymlinks(listTerminfoA)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", listTerminfoA, err)
	}
	listTerminfoB := filepath.Join(t.TempDir(), "terminfo-b")
	if err := os.MkdirAll(listTerminfoB, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", listTerminfoB, err)
	}
	canonicalListTerminfoB, err := filepath.EvalSymlinks(listTerminfoB)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", listTerminfoB, err)
	}

	pairs, readDirs := terminalCapabilitySupport(home, func(key string) string {
		switch key {
		case "TERMINFO":
			return explicitTerminfo
		case "TERMINFO_DIRS":
			return listTerminfoA + string(os.PathListSeparator) + listTerminfoB
		default:
			return ""
		}
	})

	values := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			t.Fatalf("malformed env pair: %q", pair)
		}
		values[key] = value
	}

	if values["TERMINFO"] != explicitTerminfo {
		t.Fatalf("TERMINFO = %q, want %q", values["TERMINFO"], explicitTerminfo)
	}
	wantDirsValue := listTerminfoA + string(os.PathListSeparator) + listTerminfoB
	if values["TERMINFO_DIRS"] != wantDirsValue {
		t.Fatalf("TERMINFO_DIRS = %q, want %q", values["TERMINFO_DIRS"], wantDirsValue)
	}
	if len(readDirs) != 3 {
		t.Fatalf("readDirs = %v, want 3 entries", readDirs)
	}
	for _, want := range []string{canonicalExplicitTerminfo, canonicalListTerminfoA, canonicalListTerminfoB} {
		found := false
		for _, dir := range readDirs {
			if dir == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("readDirs = %v, want to contain %q", readDirs, want)
		}
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
	if !strings.Contains(policy, `(allow process-exec (subpath "/tmp/myproject"))`) {
		t.Error("expected process-exec rule for PROJECT_DIR")
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

	for _, want := range []string{
		`(allow file-read* file-write* (literal "/dev/tty"))`,
		`(allow file-read* file-write* (literal "/dev/ptmx"))`,
		`(allow file-read* file-write* (regex #"/dev/ttys[0-9]+"))`,
	} {
		if !strings.Contains(policy, want) {
			t.Errorf("expected terminal support rule %q", want)
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
		execWant := `(allow process-exec (subpath "` + dir + `"))`
		if !strings.Contains(policy, execWant) {
			t.Errorf("expected process-exec rule for read dir %s", dir)
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

func TestGenerateSBPLWithWriteDirs(t *testing.T) {
	cfg := sessionConfig{
		ProjectDir: "/tmp/myproject",
		WriteDirs:  []string{"/tmp/venvs/project"},
	}
	policy := generateSBPL(cfg)

	want := `(allow file-read* file-write* (subpath "/tmp/venvs/project"))`
	if !strings.Contains(policy, want) {
		t.Fatalf("expected read-write rule for write dir in policy:\n%s", policy)
	}
	if !strings.Contains(policy, `(allow process-exec (subpath "/tmp/venvs/project"))`) {
		t.Fatalf("expected process-exec rule for write dir in policy:\n%s", policy)
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

// codexOnlySBPLRules captures every line that should be emitted ONLY when the
// session's harness uses macOS native TLS (currently codex). Adding a new
// harness with the same need? Update harnessUsesMacOSNativeTLS in
// native_session_policy.go and these tests stay green automatically.
var codexOnlySBPLRules = []string{
	`(allow mach-lookup (global-name "com.apple.SystemConfiguration.configd"))`,
	`(allow mach-lookup (global-name "com.apple.trustd.agent"))`,
	`(allow mach-lookup (global-name "com.apple.SecurityServer"))`,
	`(allow file-read* (subpath "/System/Cryptexes"))`,
	`(allow file-read* (subpath "/Library/Keychains"))`,
	`(allow file-read* (literal "/Library/Preferences/com.apple.security.plist"))`,
	`(allow ipc-posix-shm-read-data (ipc-posix-name "apple.shm.notification_center"))`,
	`(allow system-socket (require-all (socket-domain 32) (socket-protocol 2)))`,
	`(allow file-read-metadata (literal "` + agentHome + `/Library/Keychains"))`,
	`(allow file-read* (literal "` + agentHome + `/Library/Keychains/login.keychain-db"))`,
}

func TestGenerateSBPLCodexHarnessGetsNativeTLSRules(t *testing.T) {
	cfg := sessionConfig{
		ProjectDir: "/tmp/myproject",
		HarnessID:  HarnessCodex,
	}
	policy := generateSBPL(cfg)

	for _, want := range codexOnlySBPLRules {
		if !strings.Contains(policy, want) {
			t.Errorf("codex policy missing macOS-native-TLS rule:\n  %s", want)
		}
	}
}

func TestGenerateSBPLNonRustHarnessesDoNotGetNativeTLSRules(t *testing.T) {
	for _, harness := range []HarnessID{HarnessClaude, HarnessGemini, HarnessOpenCode, ""} {
		cfg := sessionConfig{
			ProjectDir: "/tmp/myproject",
			HarnessID:  harness,
		}
		policy := generateSBPL(cfg)
		for _, leaked := range codexOnlySBPLRules {
			if strings.Contains(policy, leaked) {
				t.Errorf("harness %q policy includes codex-only rule (least-privilege violation):\n  %s", harness, leaked)
			}
		}
	}
}

func TestGenerateSBPLClaudePolicyHasFewerAllowsThanCodex(t *testing.T) {
	// Acceptance criterion for sandboxing-m7f7: a claude session must carry
	// strictly fewer seatbelt allows than a codex session, since claude is a
	// Node app shipping its own CA bundle and doesn't need the macOS Security
	// framework surface codex requires.
	claudeCfg := sessionConfig{ProjectDir: "/tmp/myproject", HarnessID: HarnessClaude}
	codexCfg := sessionConfig{ProjectDir: "/tmp/myproject", HarnessID: HarnessCodex}

	claudeAllows := strings.Count(generateSBPL(claudeCfg), "(allow ")
	codexAllows := strings.Count(generateSBPL(codexCfg), "(allow ")

	if claudeAllows >= codexAllows {
		t.Fatalf("claude should have FEWER allows than codex (least privilege) — got claude=%d, codex=%d", claudeAllows, codexAllows)
	}

	delta := codexAllows - claudeAllows
	// Sanity: this should track len(codexOnlySBPLRules) modulo a small slack
	// for shared rules that get split or duplicated. As of 2026-04-23 the
	// gating adds 10 codex-only allows; assert at least 8 to catch accidental
	// regressions where rules creep back into the base path.
	if delta < 8 {
		t.Fatalf("expected codex policy to carry at least 8 more allows than claude, got delta=%d (claude=%d, codex=%d)", delta, claudeAllows, codexAllows)
	}
}

func TestGenerateSBPLBaseRulesPresentForEveryHarness(t *testing.T) {
	// Rules every harness — including generic shell/exec — must always have.
	baseRules := []string{
		`(allow mach-lookup (global-name "com.apple.trustd"))`,
		`(allow mach-lookup (global-name "com.apple.mDNSResponder"))`,
		`(allow file-read* (subpath "/System/Library"))`,
		`(allow file-read* file-write* (literal "/dev/tty"))`,
		`(allow network-outbound)`,
	}
	for _, harness := range []HarnessID{HarnessClaude, HarnessCodex, HarnessGemini, HarnessOpenCode, ""} {
		cfg := sessionConfig{
			ProjectDir: "/tmp/myproject",
			HarnessID:  harness,
		}
		policy := generateSBPL(cfg)
		for _, base := range baseRules {
			if !strings.Contains(policy, base) {
				t.Errorf("harness %q policy missing base rule:\n  %s", harness, base)
			}
		}
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
	if err := warnDockerProject("claude", dir, autoDockerRequest()); err != nil {
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
			err := warnDockerProject("claude", dir, autoDockerRequest())
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
	if err := warnDockerProject("claude", dir, autoDockerRequest()); err != nil {
		t.Fatalf("expected advisory-only behavior for .devcontainer/, got: %v", err)
	}
}

func TestWarnDockerProjectDevcontainerWithImage(t *testing.T) {
	dir := t.TempDir()
	dc := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(dc, 0o755); err != nil {
		t.Fatalf("mkdir .devcontainer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dc, "devcontainer.json"),
		[]byte(`{"name": "test", "image": "mcr.microsoft.com/devcontainers/go:1"}`), 0o644); err != nil {
		t.Fatalf("write devcontainer.json: %v", err)
	}
	// With image field, .devcontainer/ is a hard marker → blocks without an
	// explicit --docker=none override.
	if err := warnDockerProject("claude", dir, autoDockerRequest()); err == nil {
		t.Fatal("expected hard-marker blocking for .devcontainer/ with image field")
	}
}

func TestDevcontainerJSONNeedsDocker(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{"image field", `{"image": "node:20"}`, true},
		{"dockerFile field", `{"dockerFile": "Dockerfile.dev"}`, true},
		{"dockerComposeFile field", `{"dockerComposeFile": "compose.yaml"}`, true},
		{"build with dockerfile", `{"build": {"dockerfile": "Dockerfile"}}`, true},
		{"build without dockerfile", `{"build": {"context": "."}}`, false},
		{"name only", `{"name": "my-project"}`, false},
		{"empty object", `{}`, false},
		{"features only", `{"name": "x", "features": {"ghcr.io/some/feature:1": {}}}`, false},
		{"invalid json", `not json`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := devcontainerJSONNeedsDocker([]byte(tc.json))
			if got != tc.want {
				t.Errorf("devcontainerJSONNeedsDocker(%s) = %v, want %v", tc.json, got, tc.want)
			}
		})
	}
}

func TestWarnDockerProjectMultipleMarkersAllListed(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Dockerfile", "compose.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	err := warnDockerProject("claude", dir, autoDockerRequest())
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
	// --docker=none should not return an error even when markers are present.
	if err := warnDockerProject("claude", dir, noneDockerRequest(dockerRequestFlag)); err != nil {
		t.Fatalf("expected no error with --docker=none, got: %v", err)
	}
}

func TestWarnDockerProjectErrorMentionsDockerSandboxSupport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}
	err := warnDockerProject("claude", dir, autoDockerRequest())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "hazmat claude --docker=sandbox") {
		t.Errorf("error message should mention Docker Sandbox command, got: %s", err)
	}
	if !strings.Contains(err.Error(), "hazmat config docker none") {
		t.Errorf("error message should mention persistent --docker=none config, got: %s", err)
	}
	if !strings.Contains(err.Error(), "hazmat sandbox doctor") {
		t.Errorf("error message should mention sandbox doctor, got: %s", err)
	}
	if !strings.Contains(err.Error(), "Native containment does not expose Docker commands") {
		t.Errorf("error message should explain the routing decision, got: %s", err)
	}
}

func TestWarnDockerProjectHarnessCommandMentionsSameHarnessCommands(t *testing.T) {
	for _, commandName := range []string{"opencode", "codex", "gemini"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
			t.Fatalf("create Dockerfile: %v", err)
		}
		err := warnDockerProject(commandName, dir, autoDockerRequest())
		if err == nil {
			t.Fatalf("expected error for %s", commandName)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("hazmat %s --docker=sandbox", commandName)) {
			t.Errorf("%s should point to its Docker Sandbox command, got: %s", commandName, err)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("hazmat %s --docker=none", commandName)) {
			t.Errorf("%s should keep a native fallback command, got: %s", commandName, err)
		}
	}
}

func TestWarnDockerProjectSharedDaemonSignals(t *testing.T) {
	dir := t.TempDir()
	compose := `services:
  api:
    labels:
      traefik.enable: "true"
      traefik.docker.network: "proxy"

networks:
  proxy:
    external: true
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatalf("create docker-compose.yml: %v", err)
	}

	err := warnDockerProject("claude", dir, autoDockerRequest())
	if err == nil {
		t.Fatal("expected shared-daemon project to block in auto mode")
	}
	if !strings.Contains(err.Error(), "appears to depend on the host Docker daemon") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "Traefik Docker network") {
		t.Fatalf("shared-daemon signal missing from error: %v", err)
	}
	if !strings.Contains(err.Error(), "hazmat config docker none") {
		t.Fatalf("error should mention persistent code-only mode: %v", err)
	}
}

func TestResolveSessionSandboxModeHardMarkersNeedHealthyBackend(t *testing.T) {
	isolateConfig(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return &fakeSandboxProbe{lookPathErr: os.ErrNotExist} }
	t.Cleanup(func() { sandboxProbeFactory = savedProbeFactory })

	detection := detectDockerProject(dir)
	useSandbox, err := resolveSessionSandboxMode("claude", dir, autoDockerRequest(), detection)
	if err == nil {
		t.Fatal("expected missing healthy backend to fail closed")
	}
	if useSandbox {
		t.Fatal("useSandbox should be false when backend detection fails")
	}
	if !strings.Contains(err.Error(), "hazmat sandbox doctor") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSessionSandboxModeExplicitAutoRoutesHealthyDockerProjectWithoutConfiguredBackend(t *testing.T) {
	isolateConfig(t)
	isolateApprovals(t)
	autoApprove(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return healthySandboxProbe() }
	t.Cleanup(func() { sandboxProbeFactory = savedProbeFactory })

	detection := detectDockerProject(dir)
	useSandbox, err := resolveSessionSandboxMode("claude", dir, autoDockerRequest(), detection)
	if err != nil {
		t.Fatalf("resolveSessionSandboxMode: %v", err)
	}
	if !useSandbox {
		t.Fatal("expected --docker=auto on a healthy Docker project to route into Docker Sandboxes without prior setup")
	}
}

func TestResolveSessionSandboxModeExplicitAutoRoutesConfiguredDockerProject(t *testing.T) {
	savedCfg := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	t.Cleanup(func() { configFilePath = savedCfg })
	isolateApprovals(t)
	autoApprove(t)

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:          sandboxBackendDockerSandboxes,
		PolicyProfile: sandboxPolicyProfileBaseline,
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}

	detection := detectDockerProject(dir)
	useSandbox, err := resolveSessionSandboxMode("shell", dir, autoDockerRequest(), detection)
	if err != nil {
		t.Fatalf("resolveSessionSandboxMode: %v", err)
	}
	if !useSandbox {
		t.Fatal("expected --docker=auto on a configured Docker project to route into Docker Sandboxes")
	}
}

func TestResolveSessionSandboxModeIgnoreDockerKeepsTier2(t *testing.T) {
	savedCfg := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	t.Cleanup(func() { configFilePath = savedCfg })

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:          sandboxBackendDockerSandboxes,
		PolicyProfile: sandboxPolicyProfileBaseline,
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}

	detection := detectDockerProject(dir)
	useSandbox, err := resolveSessionSandboxMode("exec", dir, noneDockerRequest(dockerRequestFlag), detection)
	if err != nil {
		t.Fatalf("resolveSessionSandboxMode: %v", err)
	}
	if useSandbox {
		t.Fatal("expected --docker=none to keep the session out of Docker Sandboxes")
	}
}

func TestResolveSessionSandboxModeDeclinedApprovalFallsBackToNative(t *testing.T) {
	savedCfg := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	t.Cleanup(func() { configFilePath = savedCfg })
	isolateApprovals(t)

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:          sandboxBackendDockerSandboxes,
		PolicyProfile: sandboxPolicyProfileBaseline,
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}

	// Simulate an interactive decline by directly recording a sentinel error path.
	// In real usage, ensureSandboxApproval returns errSandboxApprovalDeclined when
	// the user answers N. In non-interactive test environments, it returns a
	// different error (approval required), so pre-approve to verify the happy path
	// is tested elsewhere, and test the decline sentinel directly.
	err := fmt.Errorf("%w for %s", errSandboxApprovalDeclined, dir)
	if !errors.Is(err, errSandboxApprovalDeclined) {
		t.Fatal("errSandboxApprovalDeclined sentinel should be detectable via errors.Is")
	}

	// Non-interactive without --yes should error (not silently fall back).
	detection := detectDockerProject(dir)
	_, err = resolveSessionSandboxMode("claude", dir, autoDockerRequest(), detection)
	if err == nil {
		t.Fatal("expected error in non-interactive mode without approval")
	}
	if errors.Is(err, errSandboxApprovalDeclined) {
		t.Fatal("non-interactive should not return errSandboxApprovalDeclined")
	}
}

func TestResolveSessionSandboxModeDevcontainerOnlyIsAdvisory(t *testing.T) {
	savedCfg := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	t.Cleanup(func() { configFilePath = savedCfg })

	cfg := defaultConfig()
	cfg.Sandbox.Backend = &SandboxBackendConfig{
		Type:          sandboxBackendDockerSandboxes,
		PolicyProfile: sandboxPolicyProfileBaseline,
	}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".devcontainer"), 0o755); err != nil {
		t.Fatalf("mkdir .devcontainer: %v", err)
	}

	detection := detectDockerProject(dir)
	useSandbox, err := resolveSessionSandboxMode("claude", dir, autoDockerRequest(), detection)
	if err != nil {
		t.Fatalf("resolveSessionSandboxMode: %v", err)
	}
	if useSandbox {
		t.Fatal("expected .devcontainer-only repo to stay out of Docker Sandboxes")
	}
}

func TestResolveSessionSandboxModeSharedDaemonSignalsFailBeforeBackendRouting(t *testing.T) {
	isolateConfig(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("networks:\n  proxy:\n    external: true\n"), 0o644); err != nil {
		t.Fatalf("create docker-compose.yml: %v", err)
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return healthySandboxProbe() }
	t.Cleanup(func() { sandboxProbeFactory = savedProbeFactory })

	detection := detectDockerProject(dir)
	useSandbox, err := resolveSessionSandboxMode("claude", dir, autoDockerRequest(), detection)
	if err == nil {
		t.Fatal("expected shared-daemon project to fail in auto mode")
	}
	if useSandbox {
		t.Fatal("shared-daemon project should not route into Docker Sandboxes in auto mode")
	}
	if !strings.Contains(err.Error(), "shared-daemon Docker access") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── agentEnvPairs ──────────────────────────────────────────────────────────────

// ── parseClaudeArgs tests ────────────────────────────────────────────────────

func TestParseClaudeArgsEmpty(t *testing.T) {
	opts, fwd, err := parseClaudeArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.project != "" || opts.noBackup || opts.useSandbox || opts.allowDocker || opts.dockerMode != "" || opts.dockerModeExplicit || len(opts.readDirs) != 0 {
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

func TestParseClaudeArgsDockerFlag(t *testing.T) {
	args := []string{"--no-backup", "--docker=none", "-C", "/myproject", "-p", "hello"}
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
	if opts.dockerMode != "none" {
		t.Fatalf("dockerMode = %q, want none", opts.dockerMode)
	}
	if !opts.dockerModeExplicit {
		t.Fatal("dockerModeExplicit should be true")
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

func TestParseClaudeArgsLegacyDockerFlags(t *testing.T) {
	args := []string{"--sandbox", "--ignore-docker"}
	opts, _, err := parseClaudeArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.useSandbox {
		t.Fatal("useSandbox should be true for legacy --sandbox")
	}
	if !opts.allowDocker {
		t.Fatal("allowDocker should be true for legacy --ignore-docker")
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

func TestClaudeLaunchUIBareResumeClearsScreenAndSkipsStatusBar(t *testing.T) {
	ui := claudeLaunchUI([]string{"--resume"})
	if !ui.clearScreen {
		t.Fatal("clearScreen should be true for bare --resume")
	}
	if ui.showStatusBar {
		t.Fatal("showStatusBar should be false for bare --resume")
	}
	if !ui.waitForAltScreen {
		t.Fatal("waitForAltScreen should be true for bare --resume")
	}
}

func TestClaudeLaunchUITargetedResumeKeepsStatusBar(t *testing.T) {
	ui := claudeLaunchUI([]string{"--resume", "a1b2c3"})
	if ui.clearScreen {
		t.Fatal("clearScreen should be false for targeted --resume")
	}
	if !ui.showStatusBar {
		t.Fatal("showStatusBar should stay enabled for targeted --resume")
	}
	if ui.waitForAltScreen {
		t.Fatal("waitForAltScreen should be false for targeted --resume")
	}
}

func TestClaudeLaunchUIContinueKeepsStatusBar(t *testing.T) {
	ui := claudeLaunchUI([]string{"--continue"})
	if ui.clearScreen {
		t.Fatal("clearScreen should be false for --continue")
	}
	if !ui.showStatusBar {
		t.Fatal("showStatusBar should stay enabled for --continue")
	}
	if ui.waitForAltScreen {
		t.Fatal("waitForAltScreen should be false for --continue")
	}
}

func TestTranscriptHasAltScreenEnter(t *testing.T) {
	if !transcriptHasAltScreenEnter([]byte("prefix\x1b[?1049hsuffix")) {
		t.Fatal("expected alt-screen enter sequence to be detected")
	}
	if transcriptHasAltScreenEnter([]byte("plain output only")) {
		t.Fatal("unexpected alt-screen detection for plain output")
	}
}

func TestWatchTranscriptForAltScreenActivatesAcrossChunkBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	activated := make(chan struct{})
	stop := make(chan struct{})
	go watchTranscriptForAltScreen(path, func() { close(activated) }, stop)
	defer close(stop)

	appendChunk := func(data string) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(data); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	appendChunk("prefix\x1b[?10")
	time.Sleep(150 * time.Millisecond)
	select {
	case <-activated:
		t.Fatal("watcher activated before full alt-screen sequence arrived")
	default:
	}

	appendChunk("49hmain")
	select {
	case <-activated:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not activate after alt-screen sequence arrived")
	}
}

func TestAgentEnvPairsExposeSessionConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	terminfoDir := filepath.Join(home, ".terminfo")
	if err := os.MkdirAll(terminfoDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", terminfoDir, err)
	}
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("TERMINFO_DIRS", "")

	cfg := sessionConfig{
		ProjectDir: "/Users/dr/workspace/project",
		ReadDirs: []string{
			"/Users/dr/workspace/ref-a",
			"/Users/dr/workspace/ref-b",
		},
		WriteDirs: []string{
			"/Users/dr/.venvs/project",
		},
		IntegrationEnv: map[string]string{
			"GOPATH": "/Users/dr/go",
		},
		HarnessEnv: map[string]string{
			"ANTHROPIC_API_KEY": "stored-claude-key",
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
	if err := json.Unmarshal([]byte(values["SANDBOX_WRITE_DIRS_JSON"]), &dirs); err != nil {
		t.Fatalf("unmarshal SANDBOX_WRITE_DIRS_JSON: %v", err)
	}
	if len(dirs) != len(cfg.WriteDirs) {
		t.Fatalf("write dir count = %d, want %d", len(dirs), len(cfg.WriteDirs))
	}
	if values["GOPATH"] != "/Users/dr/go" {
		t.Fatalf("GOPATH = %q, want /Users/dr/go", values["GOPATH"])
	}
	if values["ANTHROPIC_API_KEY"] != "stored-claude-key" {
		t.Fatalf("ANTHROPIC_API_KEY = %q, want stored-claude-key", values["ANTHROPIC_API_KEY"])
	}
	if values["TERM"] != "xterm-ghostty" {
		t.Fatalf("TERM = %q, want xterm-ghostty", values["TERM"])
	}
	wantTerminfoDirs := terminfoDir + string(os.PathListSeparator)
	if values["TERMINFO_DIRS"] != wantTerminfoDirs {
		t.Fatalf("TERMINFO_DIRS = %q, want %q", values["TERMINFO_DIRS"], wantTerminfoDirs)
	}
}

// Acceptance check for "session integrations should be implemented for
// supported harnesses": all four harness IDs (and the generic shell/exec
// targets) flow through the same applyIntegrations code path with identical
// effect. The HarnessID field on sessionConfig is metadata only — it does
// not gate which integrations apply.
func TestApplyIntegrationsRunsUniformlyForAllHarnesses(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GOPROXY", "https://proxy.example")

	type result struct {
		active  []string
		envKeys []string
		excl    []string
	}
	got := make(map[HarnessID]result)
	for _, harness := range []HarnessID{HarnessClaude, HarnessCodex, HarnessOpenCode, HarnessGemini} {
		cfg := sessionConfig{
			ProjectDir:     t.TempDir(),
			BackupExcludes: snapshotIgnoreRules(nil),
			HarnessID:      harness,
		}
		if _, err := applyIntegrations(&cfg, []string{"go"}); err != nil {
			t.Fatalf("applyIntegrations(%s): %v", harness, err)
		}
		got[harness] = result{
			active:  append([]string{}, cfg.ActiveIntegrations...),
			envKeys: append([]string{}, cfg.IntegrationRegistryKeys...),
			excl:    append([]string{}, cfg.IntegrationExcludes...),
		}
	}

	// All four harnesses must end up with identical integration effects:
	// same active list, same env passthrough keys, same excludes.
	first := got[HarnessClaude]
	for _, harness := range []HarnessID{HarnessCodex, HarnessOpenCode, HarnessGemini} {
		if !slicesEqualString(first.active, got[harness].active) {
			t.Errorf("ActiveIntegrations diverged for %s: %v vs %v (claude)", harness, got[harness].active, first.active)
		}
		if !slicesEqualString(first.envKeys, got[harness].envKeys) {
			t.Errorf("IntegrationRegistryKeys diverged for %s: %v vs %v (claude)", harness, got[harness].envKeys, first.envKeys)
		}
		if !slicesEqualString(first.excl, got[harness].excl) {
			t.Errorf("IntegrationExcludes diverged for %s: %v vs %v (claude)", harness, got[harness].excl, first.excl)
		}
	}
}

func slicesEqualString(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestApplyIntegrationsMergesSnapshotExcludes(t *testing.T) {
	isolateConfig(t)

	cfg := sessionConfig{
		ProjectDir:     t.TempDir(),
		BackupExcludes: []string{"node_modules/"},
	}

	if _, err := applyIntegrations(&cfg, []string{"node"}); err != nil {
		t.Fatalf("applyIntegrations: %v", err)
	}

	countNodeModules := 0
	foundNext := false
	for _, pat := range cfg.BackupExcludes {
		if pat == "node_modules/" {
			countNodeModules++
		}
		if pat == ".next/" {
			foundNext = true
		}
	}
	if countNodeModules != 1 {
		t.Fatalf("node_modules/ count = %d, want 1 in %v", countNodeModules, cfg.BackupExcludes)
	}
	if !foundNext {
		t.Fatalf("BackupExcludes = %v, want to contain .next/", cfg.BackupExcludes)
	}
}

func TestApplyIntegrationsPopulatesSessionContractFields(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GOPROXY", "https://proxy.example")

	cfg := sessionConfig{
		ProjectDir:     t.TempDir(),
		BackupExcludes: snapshotIgnoreRules(nil),
	}

	if _, err := applyIntegrations(&cfg, []string{"go"}); err != nil {
		t.Fatalf("applyIntegrations: %v", err)
	}

	if len(cfg.ActiveIntegrations) != 1 || cfg.ActiveIntegrations[0] != "go" {
		t.Fatalf("ActiveIntegrations = %v, want [go]", cfg.ActiveIntegrations)
	}
	if len(cfg.IntegrationRegistryKeys) != 1 || cfg.IntegrationRegistryKeys[0] != "GOPROXY" {
		t.Fatalf("IntegrationRegistryKeys = %v, want [GOPROXY]", cfg.IntegrationRegistryKeys)
	}
	foundVendor := false
	for _, pat := range cfg.IntegrationExcludes {
		if pat == "vendor/" {
			foundVendor = true
			break
		}
	}
	if !foundVendor {
		t.Fatalf("IntegrationExcludes = %v, want to contain vendor/", cfg.IntegrationExcludes)
	}
}

func TestRenderSessionContractShowsComputedSessionState(t *testing.T) {
	cfg := sessionConfig{
		ProjectDir:              "/tmp/project",
		AutoReadDirs:            []string{"/opt/homebrew/lib/node_modules", "/Users/dr/go/pkg/mod"},
		UserReadDirs:            []string{"/Users/dr/workspace/shared-ref"},
		WriteDirs:               []string{"/Users/dr/.venvs/project"},
		IntegrationSources:      []string{"go (go env GOROOT)", "node (active runtime)"},
		IntegrationRegistryKeys: []string{"GOPROXY"},
		IntegrationExcludes:     []string{"vendor/", ".next/"},
		IntegrationWarnings:     []string{"Using SDKMAN Java. Ensure JAVA_HOME points to the correct version."},
		ActiveIntegrations:      []string{"go", "node"},
		ServiceAccess:           []string{"github"},
		GitSSH: &sessionGitSSHConfig{
			DisplayName: "id_rsa",
		},
		RoutingReason: "using Docker Sandbox because --docker=auto detected a private-daemon Docker fit (Dockerfile)",
		SessionNotes:  []string{"If this session needs Docker, use: hazmat claude --docker=sandbox -C /tmp/project"},
	}

	got := renderSessionContract(cfg, sessionModeDockerSandbox, false)

	for _, want := range []string{
		"hazmat: session",
		"Mode:                 Docker Sandbox",
		"Why this mode:        using Docker Sandbox because --docker=auto detected a private-daemon Docker fit (Dockerfile)",
		"Project (read-write): /tmp/project",
		"Integrations:         go, node",
		"Integration sources: go (go env GOROOT), node (active runtime)",
		"Auto read-only:       /opt/homebrew/lib/node_modules, /Users/dr/go/pkg/mod",
		"Read-only extensions: /Users/dr/workspace/shared-ref",
		"Read-write extensions: /Users/dr/.venvs/project",
		"Service access:       github",
		"Git SSH key:          id_rsa",
		"Pre-session snapshot: on",
		"Snapshot excludes:    vendor/, .next/",
		"Invoker env passthrough: registry URLs via GOPROXY",
		"Notes:",
		"If this session needs Docker, use: hazmat claude --docker=sandbox -C /tmp/project",
		"Warnings:",
		"Using SDKMAN Java. Ensure JAVA_HOME points to the correct version.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderSessionContract missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderSessionContractShowsPlannedHostPermissionChanges(t *testing.T) {
	cfg := sessionConfig{
		ProjectDir: "/tmp/project",
		PlannedHostMutations: []sessionMutation{
			{Summary: "project ACL repair"},
			{Summary: "git metadata ACL repair"},
		},
	}

	got := renderSessionContract(cfg, sessionModeNative, false)
	if !strings.Contains(got, "Host changes:          project ACL repair, git metadata ACL repair") {
		t.Fatalf("renderSessionContract missing host changes in:\n%s", got)
	}
}

func TestRenderSessionMutationDetails(t *testing.T) {
	got := renderSessionMutationDetails([]sessionMutation{
		{
			Summary:     "project ACL repair",
			Detail:      "may add collaborative ACLs under /tmp/project",
			Persistence: "persistent in project",
			ProofScope:  "TLA+ model + tests/docs",
		},
	})

	for _, want := range []string{
		"hazmat: planned host changes",
		"project ACL repair: may add collaborative ACLs under /tmp/project (persistent in project; proof scope: TLA+ model + tests/docs)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderSessionMutationDetails missing %q in:\n%s", want, got)
		}
	}
}

func TestBuildNativeSessionMutationPlanIncludesProjectRepair(t *testing.T) {
	projectDir := t.TempDir()
	plan := buildNativeSessionMutationPlan(sessionConfig{ProjectDir: projectDir})

	found := false
	for _, mutation := range plan.Describe() {
		if mutation.Summary == "project ACL repair" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Describe() = %+v, want project ACL repair", plan.Describe())
	}
}

func TestBuildNativeSessionMutationPlanIncludesGitSafeDirectoryTrust(t *testing.T) {
	savedDetect := detectGitRepoTopLevel
	savedSystem := readSystemGitSafeDirectoryEntries
	savedAgent := readAgentGlobalGitSafeDirectoryEntries
	t.Cleanup(func() {
		detectGitRepoTopLevel = savedDetect
		readSystemGitSafeDirectoryEntries = savedSystem
		readAgentGlobalGitSafeDirectoryEntries = savedAgent
	})

	repoDir := "/Users/dr/workspace/stack-matrix/pydantic-ai"
	detectGitRepoTopLevel = func(projectDir string) (string, bool) {
		return repoDir, true
	}
	readSystemGitSafeDirectoryEntries = func() ([]string, error) {
		return nil, nil
	}
	readAgentGlobalGitSafeDirectoryEntries = func() ([]string, error) {
		return nil, nil
	}

	plan := buildNativeSessionMutationPlan(sessionConfig{ProjectDir: t.TempDir()})

	found := false
	for _, mutation := range plan.Describe() {
		if mutation.Summary != "git safe.directory trust" {
			continue
		}
		found = true
		if !strings.Contains(mutation.Detail, repoDir) {
			t.Fatalf("mutation.Detail = %q, want repo path %q", mutation.Detail, repoDir)
		}
		if mutation.ProofScope != sessionMutationProofScopeTestsDocs {
			t.Fatalf("mutation.ProofScope = %q, want %q", mutation.ProofScope, sessionMutationProofScopeTestsDocs)
		}
	}
	if !found {
		t.Fatalf("Describe() = %+v, want git safe.directory trust", plan.Describe())
	}
}

func TestBuildNativeSessionMutationPlanIncludesLaunchHelperTraverseRepair(t *testing.T) {
	savedExecutable := currentExecutablePath
	savedUserHomeDir := currentUserHomeDir
	savedPathAllows := pathAllowsAgentTraverse
	t.Cleanup(func() {
		currentExecutablePath = savedExecutable
		currentUserHomeDir = savedUserHomeDir
		pathAllowsAgentTraverse = savedPathAllows
	})

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	currentExecutablePath = func() (string, error) {
		return filepath.Join(homeDir, ".local", "bin", "hazmat"), nil
	}
	currentUserHomeDir = func() (string, error) {
		return homeDir, nil
	}
	pathAllowsAgentTraverse = func(path string) bool {
		return false
	}

	plan := buildNativeSessionMutationPlan(sessionConfig{ProjectDir: t.TempDir()})
	helperPath := filepath.Join(homeDir, ".local", "libexec", "hazmat-launch")

	found := false
	for _, mutation := range plan.Describe() {
		if mutation.Summary != "launch-helper traverse ACL repair" {
			continue
		}
		found = true
		if !strings.Contains(mutation.Detail, helperPath) {
			t.Fatalf("mutation.Detail = %q, want helper path %q", mutation.Detail, helperPath)
		}
		if mutation.ProofScope != sessionMutationProofScopeTLAModel {
			t.Fatalf("mutation.ProofScope = %q, want %q", mutation.ProofScope, sessionMutationProofScopeTLAModel)
		}
	}
	if !found {
		t.Fatalf("Describe() = %+v, want launch-helper traverse ACL repair", plan.Describe())
	}
}

func TestRenderSessionContractShowsNoneAndSkippedSnapshot(t *testing.T) {
	got := renderSessionContract(sessionConfig{ProjectDir: "/tmp/project"}, sessionModeNative, true)

	for _, want := range []string{
		"Mode:                 Native containment",
		"Integrations:         none",
		"Integration help:     " + integrationContributorFlowDocURL,
		"Auto read-only:       none",
		"Read-only extensions: none",
		"Read-write extensions: none",
		"Service access:       none",
		"Pre-session snapshot: skipped (--no-backup)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderSessionContract missing %q in:\n%s", want, got)
		}
	}
}

func TestSessionRoutingExplanationRequestedSandbox(t *testing.T) {
	dir := t.TempDir()
	reason, notes := sessionRoutingExplanation("claude", dir, sandboxDockerRequest(dockerRequestFlag), detectDockerProject(dir), sessionModeDockerSandbox)
	if reason != "using Docker Sandbox because --docker=sandbox was requested" {
		t.Fatalf("reason = %q", reason)
	}
	if len(notes) != 0 {
		t.Fatalf("notes = %v, want empty", notes)
	}
}

func TestSessionRoutingExplanationDockerNone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}
	detection := detectDockerProject(dir)
	reason, notes := sessionRoutingExplanation("claude", dir, noneDockerRequest(dockerRequestFlag), detection, sessionModeNative)
	if reason != "staying in native containment because --docker=none was requested" {
		t.Fatalf("reason = %q", reason)
	}
	if len(notes) != 2 {
		t.Fatalf("notes = %v, want 2 entries", notes)
	}
	if !strings.Contains(notes[0], "Docker files detected: Dockerfile") {
		t.Fatalf("notes[0] = %q", notes[0])
	}
	if !strings.Contains(notes[1], "hazmat claude --docker=sandbox") {
		t.Fatalf("notes[1] = %q", notes[1])
	}
}

func TestSessionRoutingExplanationDefaultDockerNone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}
	detection := detectDockerProject(dir)
	reason, notes := sessionRoutingExplanation("claude", dir, defaultDockerRequest(), detection, sessionModeNative)
	if reason != "using native containment by default (Docker routing: none)" {
		t.Fatalf("reason = %q", reason)
	}
	if len(notes) != 2 {
		t.Fatalf("notes = %v, want 2 entries", notes)
	}
	if !strings.Contains(notes[0], "Docker files detected: Dockerfile") {
		t.Fatalf("notes[0] = %q", notes[0])
	}
	if !strings.Contains(notes[1], "hazmat claude --docker=sandbox") {
		t.Fatalf("notes[1] = %q", notes[1])
	}
}

func TestSessionRoutingExplanationDevcontainerOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".devcontainer"), 0o755); err != nil {
		t.Fatalf("mkdir .devcontainer: %v", err)
	}
	detection := detectDockerProject(dir)
	reason, notes := sessionRoutingExplanation("claude", dir, autoDockerRequest(), detection, sessionModeNative)
	if reason != "staying in native containment because .devcontainer/ alone does not require Docker mode" {
		t.Fatalf("reason = %q", reason)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "hazmat claude --docker=sandbox") {
		t.Fatalf("notes = %v", notes)
	}
}

func TestDockerSessionExampleUsesSameHarnessForSandboxMode(t *testing.T) {
	projectDir := "/tmp/project"
	for _, commandName := range []string{"claude", "opencode", "codex", "gemini"} {
		got := dockerSessionExample(commandName, projectDir, dockerModeSandbox)
		want := fmt.Sprintf("hazmat %s --docker=sandbox -C %s", commandName, projectDir)
		if got != want {
			t.Fatalf("dockerSessionExample(%q, sandbox) = %q, want %q", commandName, got, want)
		}
	}
}

func TestResolveExplainSessionDefaultsToNativeForDockerProject(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}

	cfg, mode, err := resolveExplainSession("claude", harnessSessionOpts{project: dir})
	if err != nil {
		t.Fatalf("resolveExplainSession: %v", err)
	}
	if mode != sessionModeNative {
		t.Fatalf("mode = %q, want Native containment", mode)
	}
	if cfg.RoutingReason != "using native containment by default (Docker routing: none)" {
		t.Fatalf("RoutingReason = %q", cfg.RoutingReason)
	}
	if len(cfg.SessionNotes) == 0 || !strings.Contains(cfg.SessionNotes[0], "Docker files detected") {
		t.Fatalf("SessionNotes = %v", cfg.SessionNotes)
	}
}

func TestResolveExplainSessionAutoFlagRoutesDockerProject(t *testing.T) {
	isolateConfig(t)
	isolateApprovals(t)
	autoApprove(t)
	skipInitCheck(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return healthySandboxProbe() }
	t.Cleanup(func() { sandboxProbeFactory = savedProbeFactory })

	cfg, mode, err := resolveExplainSession("claude", harnessSessionOpts{
		project:            dir,
		dockerMode:         "auto",
		dockerModeExplicit: true,
	})
	if err != nil {
		t.Fatalf("resolveExplainSession: %v", err)
	}
	if mode != sessionModeDockerSandbox {
		t.Fatalf("mode = %q, want Docker Sandbox", mode)
	}
	if !strings.Contains(cfg.RoutingReason, "--docker=auto") {
		t.Fatalf("RoutingReason = %q", cfg.RoutingReason)
	}
}

func TestResolveExplainSessionUsesProjectDockerModeNone(t *testing.T) {
	isolateConfig(t)
	skipInitCheck(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}
	if err := runConfigDocker(dir, "none"); err != nil {
		t.Fatalf("runConfigDocker: %v", err)
	}

	cfg, mode, err := resolveExplainSession("claude", harnessSessionOpts{project: dir})
	if err != nil {
		t.Fatalf("resolveExplainSession: %v", err)
	}
	if mode != sessionModeNative {
		t.Fatalf("mode = %q, want Native containment", mode)
	}
	if cfg.RoutingReason != "staying in native containment because this project is configured with docker: none" {
		t.Fatalf("RoutingReason = %q", cfg.RoutingReason)
	}
	if len(cfg.SessionNotes) == 0 || !strings.Contains(cfg.SessionNotes[0], "Docker files detected") {
		t.Fatalf("SessionNotes = %v", cfg.SessionNotes)
	}
}

func TestResolveExplainSessionUsesProjectDockerModeAuto(t *testing.T) {
	isolateConfig(t)
	isolateApprovals(t)
	autoApprove(t)
	skipInitCheck(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte{}, 0o644); err != nil {
		t.Fatalf("create Dockerfile: %v", err)
	}
	if err := runConfigDocker(dir, "auto"); err != nil {
		t.Fatalf("runConfigDocker: %v", err)
	}

	savedProbeFactory := sandboxProbeFactory
	sandboxProbeFactory = func() sandboxProbe { return healthySandboxProbe() }
	t.Cleanup(func() { sandboxProbeFactory = savedProbeFactory })

	cfg, mode, err := resolveExplainSession("claude", harnessSessionOpts{project: dir})
	if err != nil {
		t.Fatalf("resolveExplainSession: %v", err)
	}
	if mode != sessionModeDockerSandbox {
		t.Fatalf("mode = %q, want Docker Sandbox", mode)
	}
	if !strings.Contains(cfg.RoutingReason, "configured with docker: auto") {
		t.Fatalf("RoutingReason = %q", cfg.RoutingReason)
	}
}

func TestResolvePreparedSessionSupportsHarnessSandboxTarget(t *testing.T) {
	skipInitCheck(t)
	for _, commandName := range []string{"opencode", "codex", "gemini"} {
		dir := t.TempDir()
		prepared, err := resolvePreparedSession(commandName, harnessSessionOpts{
			project:            dir,
			dockerMode:         "sandbox",
			dockerModeExplicit: true,
		}, true)
		if err != nil {
			t.Fatalf("resolvePreparedSession(%s): %v", commandName, err)
		}
		if prepared.Mode != sessionModeDockerSandbox {
			t.Fatalf("%s mode = %q, want Docker Sandbox", commandName, prepared.Mode)
		}
		if prepared.Config.RoutingReason != "using Docker Sandbox because --docker=sandbox was requested" {
			t.Fatalf("%s RoutingReason = %q", commandName, prepared.Config.RoutingReason)
		}
		wantHarness, ok := harnessIDForCommand(commandName)
		if !ok {
			t.Fatalf("missing harness ID for %s", commandName)
		}
		if prepared.Config.HarnessID != wantHarness {
			t.Fatalf("%s HarnessID = %q, want %q", commandName, prepared.Config.HarnessID, wantHarness)
		}
	}
}

func TestResolveExplainSessionSupportsHarnessSandboxTarget(t *testing.T) {
	skipInitCheck(t)
	for _, commandName := range []string{"opencode", "codex", "gemini"} {
		dir := t.TempDir()
		cfg, mode, err := resolveExplainSession(commandName, harnessSessionOpts{
			project:            dir,
			dockerMode:         "sandbox",
			dockerModeExplicit: true,
		})
		if err != nil {
			t.Fatalf("resolveExplainSession(%s): %v", commandName, err)
		}
		if mode != sessionModeDockerSandbox {
			t.Fatalf("%s mode = %q, want Docker Sandbox", commandName, mode)
		}
		if cfg.RoutingReason != "using Docker Sandbox because --docker=sandbox was requested" {
			t.Fatalf("%s RoutingReason = %q", commandName, cfg.RoutingReason)
		}
		wantHarness, ok := harnessIDForCommand(commandName)
		if !ok {
			t.Fatalf("missing harness ID for %s", commandName)
		}
		if cfg.HarnessID != wantHarness {
			t.Fatalf("%s HarnessID = %q, want %q", commandName, cfg.HarnessID, wantHarness)
		}
	}
}
