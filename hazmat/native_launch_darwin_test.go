//go:build darwin

package hazmat

import (
	"reflect"
	"strings"
	"testing"

	"hazmat/runtimeprovider"
)

func TestDarwinNativeLaunchSudoArgsShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range terminalEnvPassthroughKeys {
		t.Setenv(key, "")
	}
	t.Setenv("TERMINFO", "")
	t.Setenv("TERMINFO_DIRS", "")

	cfg := sessionConfig{
		ProjectDir:        "/Users/dr/workspace/project",
		ReadDirs:          []string{"/Users/dr/workspace/reference"},
		WriteDirs:         []string{"/Users/dr/.cache/project"},
		SkipGoModCacheEnv: true,
	}
	policy := nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-test.sb"}
	script := `echo "$1"`

	got := nativeLaunchSudoArgs(cfg, policy, []string{"RUNTIME_ENV=1"}, script, "arg1")
	want := []string{
		"-u", agentUser,
		launchHelperPath(), policy.Path,
		"/usr/bin/env", "-i",
	}
	want = append(want, agentEnvPairs(cfg)...)
	want = append(want, "RUNTIME_ENV=1", "/bin/zsh", "-lc", script, "zsh", "arg1")

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native launch sudo args = %#v, want %#v", got, want)
	}
}

func TestDarwinNativeLaunchSudoArgsIncludeMetadataBeforeEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range terminalEnvPassthroughKeys {
		t.Setenv(key, "")
	}
	t.Setenv("TERMINFO", "")
	t.Setenv("TERMINFO_DIRS", "")

	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/project", SkipGoModCacheEnv: true}
	policy := nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-test.sb"}
	metadata := `{"kind":"hazmat.session"}`

	got := nativeLaunchSudoArgsWithMetadata(cfg, policy, nil, metadata, `exec "$@"`, "arg1")

	wantPrefix := []string{
		"-u", agentUser,
		launchHelperPath(), policy.Path,
		"--hazmat-metadata-json", metadata,
		"/usr/bin/env", "-i",
	}
	if len(got) < len(wantPrefix) || !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("native launch sudo args prefix = %#v, want %#v", got, wantPrefix)
	}
}

func TestDarwinNativeLaunchSudoArgsUsesDirectExecForProjectExecScript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range terminalEnvPassthroughKeys {
		t.Setenv(key, "")
	}
	t.Setenv("TERMINFO", "")
	t.Setenv("TERMINFO_DIRS", "")

	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/project", SkipGoModCacheEnv: true}
	policy := nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-test.sb"}
	savedSupportsDirectExec := launchHelperSupportsDirectExec
	launchHelperSupportsDirectExec = func(string) bool { return true }
	t.Cleanup(func() { launchHelperSupportsDirectExec = savedSupportsDirectExec })

	got := nativeLaunchSudoArgsWithMetadata(cfg, policy, []string{"RUNTIME_ENV=1"}, `{"kind":"hazmat.session"}`, nativeDirectProjectExecScript, "/usr/bin/true")

	wantPrefix := []string{
		"-u", agentUser,
		launchHelperPath(), policy.Path,
		"--hazmat-metadata-json", `{"kind":"hazmat.session"}`,
		"--hazmat-direct-exec",
		"--hazmat-working-dir", cfg.ProjectDir,
	}
	if len(got) < len(wantPrefix) || !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("native launch sudo args prefix = %#v, want %#v", got[:len(wantPrefix)], wantPrefix)
	}
	if containsString(got, "/bin/zsh") || containsString(got, "/usr/bin/env") {
		t.Fatalf("direct exec args should not include shell/env helper: %#v", got)
	}
	if !reflect.DeepEqual(got[len(got)-2:], []string{"--", "/usr/bin/true"}) {
		t.Fatalf("direct exec args suffix = %#v, want delimiter and target", got[len(got)-2:])
	}
}

func TestDarwinNativeLaunchSudoArgsPassesHelperManagedTempDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range terminalEnvPassthroughKeys {
		t.Setenv(key, "")
	}
	t.Setenv("TERMINFO", "")
	t.Setenv("TERMINFO_DIRS", "")

	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/project", SkipGoModCacheEnv: true}
	policy := nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-test.sb"}
	tempDir := agentHome + "/.cache/hazmat/tmp/123-456"
	savedSupportsDirectExec := launchHelperSupportsDirectExec
	launchHelperSupportsDirectExec = func(string) bool { return true }
	t.Cleanup(func() { launchHelperSupportsDirectExec = savedSupportsDirectExec })

	got := nativeLaunchSudoArgsWithMetadataPlanAndRuntime(cfg, nativeLaunchPlanForConfig(cfg), policy, nil, `{"kind":"hazmat.session"}`, tempDir, nativeDirectProjectExecScript, "/usr/bin/true")

	wantPrefix := []string{
		"-u", agentUser,
		launchHelperPath(), policy.Path,
		"--hazmat-metadata-json", `{"kind":"hazmat.session"}`,
		"--hazmat-session-temp", tempDir,
		"--hazmat-direct-exec",
	}
	if len(got) < len(wantPrefix) || !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("native launch sudo args prefix = %#v, want %#v", got[:len(wantPrefix)], wantPrefix)
	}
}

func TestDarwinNativeLaunchCurrentUserArgsShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "dr")
	t.Setenv("PATH", agentHome+"/.local/bin:/usr/bin:/bin")
	for _, key := range terminalEnvPassthroughKeys {
		t.Setenv(key, "")
	}
	t.Setenv("TERMINFO", "")
	t.Setenv("TERMINFO_DIRS", "")

	dirs := currentUserSessionDirs{
		Root:       "/private/tmp/hazmat-current-user-123",
		Home:       "/private/tmp/hazmat-current-user-123/home",
		CacheHome:  "/private/tmp/hazmat-current-user-123/home/.cache",
		ConfigHome: "/private/tmp/hazmat-current-user-123/home/.config",
		DataHome:   "/private/tmp/hazmat-current-user-123/home/.local/share",
		TempDir:    "/private/tmp/hazmat-current-user-123/tmp",
	}
	cfg := sessionConfig{
		ProjectDir:         "/Users/dr/workspace/project",
		RuntimeProvider:    runtimeprovider.KindMacOSCurrentUser,
		CurrentUserSession: &dirs,
		TempDir:            dirs.TempDir,
		SkipGoModCacheEnv:  true,
	}
	policy := nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-test.sb"}
	savedSupportsDirectExec := launchHelperSupportsDirectExec
	launchHelperSupportsDirectExec = func(string) bool { return true }
	t.Cleanup(func() { launchHelperSupportsDirectExec = savedSupportsDirectExec })

	got := nativeLaunchCurrentUserArgsWithMetadataPlanAndRuntime(cfg, nativeLaunchPlanForConfig(cfg), policy, nil, `{"kind":"hazmat.session"}`, nativeDirectProjectExecScript, "/usr/bin/true")

	wantPrefix := []string{
		launchHelperPath(),
		"--hazmat-current-user",
		policy.Path,
		"--hazmat-metadata-json", `{"kind":"hazmat.session"}`,
		"--hazmat-direct-exec",
		"--hazmat-working-dir", cfg.ProjectDir,
	}
	if len(got) < len(wantPrefix) || !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("current-user args prefix = %#v, want %#v", got[:len(wantPrefix)], wantPrefix)
	}
	if containsString(got, "-u") || containsString(got, agentUser) {
		t.Fatalf("current-user args must not contain sudo agent identity: %#v", got)
	}
	env := envPairsMap(got)
	if env["HOME"] != dirs.Home || env["USER"] != "dr" || env["LOGNAME"] != "dr" {
		t.Fatalf("current-user env HOME/USER/LOGNAME = %q/%q/%q", env["HOME"], env["USER"], env["LOGNAME"])
	}
	if strings.Contains(env["PATH"], agentHome) {
		t.Fatalf("current-user PATH contains agent home: %q", env["PATH"])
	}
	if !reflect.DeepEqual(got[len(got)-2:], []string{"--", "/usr/bin/true"}) {
		t.Fatalf("direct exec args suffix = %#v, want delimiter and target", got[len(got)-2:])
	}
}

func TestDarwinNativeLaunchSudoArgsKeepsShellPathForOlderHelper(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range terminalEnvPassthroughKeys {
		t.Setenv(key, "")
	}
	t.Setenv("TERMINFO", "")
	t.Setenv("TERMINFO_DIRS", "")

	cfg := sessionConfig{ProjectDir: "/Users/dr/workspace/project", SkipGoModCacheEnv: true}
	policy := nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-test.sb"}
	savedSupportsDirectExec := launchHelperSupportsDirectExec
	launchHelperSupportsDirectExec = func(string) bool { return false }
	t.Cleanup(func() { launchHelperSupportsDirectExec = savedSupportsDirectExec })

	got := nativeLaunchSudoArgsWithMetadata(cfg, policy, nil, `{"kind":"hazmat.session"}`, nativeDirectProjectExecScript, "/usr/bin/true")

	if containsString(got, "--hazmat-direct-exec") {
		t.Fatalf("older helper should keep shell launch args, got %#v", got)
	}
	if !containsString(got, "/bin/zsh") || !containsString(got, "/usr/bin/env") {
		t.Fatalf("older helper fallback should include shell/env path, got %#v", got)
	}
}
