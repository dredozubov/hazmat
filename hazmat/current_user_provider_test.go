package hazmat

import (
	"bytes"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"

	"hazmat/containment"
	darwinruntime "hazmat/internal/runtime/darwin"
	"hazmat/runtimeprovider"
)

func TestExplainExecPreviewsMacOSCurrentUserProvider(t *testing.T) {
	isolateConfig(t)
	t.Setenv(darwinruntime.EnvExperimentalCurrentUser, "")
	savedSupportsCurrentUser := launchHelperSupportsCurrentUser
	launchHelperSupportsCurrentUser = func(string) bool { return false }
	t.Cleanup(func() { launchHelperSupportsCurrentUser = savedSupportsCurrentUser })

	cmd := newExplainCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--json", "--for", "exec", "--provider=macos-current-user", "-C", t.TempDir()})

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
	if preview.Provider == nil || preview.Provider.Provider != runtimeprovider.KindMacOSCurrentUser {
		t.Fatalf("Provider = %+v, want macos-current-user", preview.Provider)
	}
	if runtime.GOOS == "darwin" && preview.Provider.Status != runtimeprovider.StatusPlanOnly {
		t.Fatalf("Provider.Status = %s, want plan-only without gate/helper", preview.Provider.Status)
	}
	if len(preview.ProviderCapabilityGaps) == 0 {
		t.Fatalf("ProviderCapabilityGaps empty; provider=%+v", preview.Provider)
	}
}

func TestExecMacOSCurrentUserRequiresExperimentalGate(t *testing.T) {
	isolateConfig(t)
	t.Setenv(darwinruntime.EnvExperimentalCurrentUser, "")
	savedSupportsCurrentUser := launchHelperSupportsCurrentUser
	launchHelperSupportsCurrentUser = func(string) bool { return true }
	t.Cleanup(func() { launchHelperSupportsCurrentUser = savedSupportsCurrentUser })

	cmd := newExecCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--provider=macos-current-user", "-C", t.TempDir(), "--", "true"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("exec launched without current-user experimental gate")
	}
	if runtime.GOOS == "darwin" && !strings.Contains(err.Error(), darwinruntime.EnvExperimentalCurrentUser) {
		t.Fatalf("err = %v, want experimental gate guidance", err)
	}
}

func TestExecMacOSCurrentUserRejectsGitHubCredentialGrant(t *testing.T) {
	isolateConfig(t)
	cmd := newExecCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--provider=macos-current-user", "--github", "-C", t.TempDir(), "--", "true"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--github is not supported") {
		t.Fatalf("err = %v, want current-user GitHub credential refusal", err)
	}
}

func TestHarnessProviderFlagFailsClosedForCurrentUser(t *testing.T) {
	isolateConfig(t)
	cmd := newClaudeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--provider=macos-current-user", "-C", t.TempDir()})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "supported only for hazmat exec") {
		t.Fatalf("err = %v, want current-user harness refusal", err)
	}
}

func TestExecProviderCannotCombineWithBackend(t *testing.T) {
	isolateConfig(t)
	cmd := newExecCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--backend=apple-container", "--provider=macos-current-user", "--image", "alpine:latest", "-C", t.TempDir(), "--", "true"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--provider cannot be combined with --backend") {
		t.Fatalf("err = %v, want provider/backend refusal", err)
	}
}

func TestBuildCurrentUserNativeSessionPolicyUsesSessionHomeAndCredentialFloors(t *testing.T) {
	hostHome := t.TempDir()
	t.Setenv("HOME", hostHome)
	sessionRoot := t.TempDir()
	dirs := currentUserSessionDirs{
		Root:       sessionRoot,
		Home:       sessionRoot + "/home",
		CacheHome:  sessionRoot + "/home/.cache",
		ConfigHome: sessionRoot + "/home/.config",
		DataHome:   sessionRoot + "/home/.local/share",
		TempDir:    sessionRoot + "/tmp",
	}
	policy, err := buildNativeSessionPolicy(sessionConfig{
		ProjectDir:          t.TempDir(),
		NetworkMode:         sessionNetworkDefault,
		RuntimeProvider:     runtimeprovider.KindMacOSCurrentUser,
		CurrentUserSession:  &dirs,
		SkipGoModCacheEnv:   true,
		HarnessEnv:          map[string]string{},
		IntegrationEnv:      map[string]string{},
		CredentialEnvGrants: nil,
	})
	if err != nil {
		t.Fatalf("buildNativeSessionPolicy: %v", err)
	}
	if policy.AgentHome.Mode != containment.AgentHomeModeSessionLocal || policy.AgentHome.Path != dirs.Home {
		t.Fatalf("AgentHome = %+v, want session-local %s", policy.AgentHome, dirs.Home)
	}
	denies := strings.Join(policy.CredentialDenyPaths(), "\n")
	for _, want := range []string{
		dirs.Home + "/.ssh",
		hostHome + "/.ssh",
		agentHome + "/.ssh",
	} {
		if !strings.Contains(denies, want) {
			t.Fatalf("credential deny floor missing %s in:\n%s", want, denies)
		}
	}
	if policy.MacOSAgentKeychainAccess {
		t.Fatal("current-user policy must not re-allow agent login keychain access")
	}
}

func TestCurrentUserEnvUsesSessionLocalRootsAndNoAmbientCredentialEnv(t *testing.T) {
	t.Setenv("USER", "dr")
	t.Setenv("LOGNAME", "dr")
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient-secret")
	t.Setenv("GH_TOKEN", "ambient-token")
	dirs := currentUserSessionDirs{
		Root:       "/private/tmp/hazmat-current-user-123",
		Home:       "/private/tmp/hazmat-current-user-123/home",
		CacheHome:  "/private/tmp/hazmat-current-user-123/home/.cache",
		ConfigHome: "/private/tmp/hazmat-current-user-123/home/.config",
		DataHome:   "/private/tmp/hazmat-current-user-123/home/.local/share",
		TempDir:    "/private/tmp/hazmat-current-user-123/tmp",
	}
	cfg := sessionConfig{
		ProjectDir:         t.TempDir(),
		RuntimeProvider:    runtimeprovider.KindMacOSCurrentUser,
		CurrentUserSession: &dirs,
		TempDir:            dirs.TempDir,
		SkipGoModCacheEnv:  true,
	}
	env := envPairsMap(currentUserEnvPairsWithPlan(cfg, nativeLaunchPlanForConfig(cfg)))
	for key, want := range map[string]string{
		"HOME":            dirs.Home,
		"USER":            "dr",
		"LOGNAME":         "dr",
		"TMPDIR":          dirs.TempDir,
		"XDG_CACHE_HOME":  dirs.CacheHome,
		"XDG_CONFIG_HOME": dirs.ConfigHome,
		"XDG_DATA_HOME":   dirs.DataHome,
	} {
		if env[key] != want {
			t.Fatalf("%s = %q, want %q", key, env[key], want)
		}
	}
	for _, key := range []string{"AWS_SECRET_ACCESS_KEY", "GH_TOKEN"} {
		if _, ok := env[key]; ok {
			t.Fatalf("current-user env included ambient credential %s", key)
		}
	}
}

func TestPrepareCurrentUserSessionDirsCleanupRemovesRoot(t *testing.T) {
	dirs, cleanup, err := prepareCurrentUserSessionDirs()
	if err != nil {
		t.Fatalf("prepareCurrentUserSessionDirs: %v", err)
	}
	if _, err := os.Stat(dirs.Home); err != nil {
		cleanup()
		t.Fatalf("session home missing before cleanup: %v", err)
	}
	cleanup()
	if _, err := os.Stat(dirs.Root); !os.IsNotExist(err) {
		t.Fatalf("session root after cleanup stat err = %v, want not exist", err)
	}
}
