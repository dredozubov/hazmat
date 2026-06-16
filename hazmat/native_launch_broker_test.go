package hazmat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"hazmat/internal/runtime/launchbroker"
)

func launchBrokerOutputBuffer(t *testing.T, value any) *bytes.Buffer {
	t.Helper()
	buf, ok := value.(*bytes.Buffer)
	if !ok {
		t.Fatalf("launch broker output writer = %T, want *bytes.Buffer", value)
	}
	return buf
}

func TestConfiguredLaunchBrokerSocketPath(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "broker.sock")
	uid := strconv.Itoa(os.Getuid())
	tests := []struct {
		name         string
		env          map[string]string
		wantPath     string
		wantExplicit bool
		wantErr      string
	}{
		{
			name: "disabled",
		},
		{
			name: "explicit socket",
			env: map[string]string{
				launchBrokerSocketEnv: explicit,
			},
			wantPath:     explicit,
			wantExplicit: true,
		},
		{
			name: "relative explicit socket rejected",
			env: map[string]string{
				launchBrokerSocketEnv: "broker.sock",
			},
			wantExplicit: true,
			wantErr:      "absolute and clean",
		},
		{
			name: "experimental default socket",
			env: map[string]string{
				launchBrokerExperimentalEnv: "1",
			},
			wantPath: filepath.Join(defaultBrokerRuntimeRoot, "launch-"+uid, "launch-broker-"+uid+".sock"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotExplicit, err := configuredLaunchBrokerSocketPath(func(key string) string {
				return tt.env[key]
			})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				if gotExplicit != tt.wantExplicit {
					t.Fatalf("explicit = %v, want %v", gotExplicit, tt.wantExplicit)
				}
				return
			}
			if err != nil {
				t.Fatalf("configuredLaunchBrokerSocketPath: %v", err)
			}
			if gotPath != tt.wantPath || gotExplicit != tt.wantExplicit {
				t.Fatalf("got (%q, %v), want (%q, %v)", gotPath, gotExplicit, tt.wantPath, tt.wantExplicit)
			}
		})
	}
}

func TestNativeLaunchBrokerRequestMirrorsNativeLaunchShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range terminalEnvPassthroughKeys {
		t.Setenv(key, "")
	}
	t.Setenv("TERMINFO", "")
	t.Setenv("TERMINFO_DIRS", "")

	savedSupportsDirectExec := launchHelperSupportsDirectExec
	launchHelperSupportsDirectExec = func(string) bool { return true }
	t.Cleanup(func() { launchHelperSupportsDirectExec = savedSupportsDirectExec })

	cfg := sessionConfig{
		ProjectDir: "/Users/dr/workspace/project",
		ReadDirs:   []string{"/Users/dr/workspace/reference"},
		WriteDirs:  []string{"/Users/dr/.cache/project"},
	}
	policy := nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-123.sb"}
	req := nativeLaunchBrokerRequestWithMetadataPlanAndRuntime(
		cfg,
		nativeLaunchPlanForConfig(cfg),
		policy,
		[]string{"RUNTIME_ENV=1"},
		`{"kind":"hazmat.session"}`,
		agentHome+"/.cache/hazmat/tmp/123-456",
		nativeDirectProjectExecScript,
		"/usr/bin/true",
	)

	if req.PolicyPath != policy.Path || req.MetadataJSON != `{"kind":"hazmat.session"}` {
		t.Fatalf("policy/metadata = %q/%q", req.PolicyPath, req.MetadataJSON)
	}
	if !req.DirectExec || req.WorkingDir != cfg.ProjectDir || req.Script != "" {
		t.Fatalf("direct exec fields = direct=%v working=%q script=%q", req.DirectExec, req.WorkingDir, req.Script)
	}
	if !reflect.DeepEqual(req.RuntimeEnvPairs, []string{"RUNTIME_ENV=1"}) {
		t.Fatalf("RuntimeEnvPairs = %#v", req.RuntimeEnvPairs)
	}
	if !reflect.DeepEqual(req.Args, []string{"/usr/bin/true"}) {
		t.Fatalf("Args = %#v", req.Args)
	}
	if !containsString(req.EnvPairs, "SANDBOX_PROJECT_DIR="+cfg.ProjectDir) {
		t.Fatalf("EnvPairs missing project dir: %#v", req.EnvPairs)
	}
}

func TestTryRunNativeLaunchViaBrokerUsesExplicitSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "broker.sock")
	t.Setenv(launchBrokerSocketEnv, socketPath)

	var gotReq launchbroker.LaunchRequest
	restore := replaceLaunchBrokerTestHooks(t,
		func(_ context.Context, gotSocket string, req launchbroker.LaunchRequest) (launchbroker.LaunchResponse, error) {
			if gotSocket != socketPath {
				t.Fatalf("socket path = %q, want %q", gotSocket, socketPath)
			}
			gotReq = req
			return launchbroker.LaunchResponse{
				OK:           true,
				MetadataJSON: req.MetadataJSON,
				Stdout:       "out\n",
				Stderr:       "err\n",
			}, nil
		})
	defer restore()

	used, err := tryRunNativeLaunchViaBroker(
		sessionConfig{ProjectDir: "/Users/dr/workspace/project"},
		sessionBackendPlan{},
		sessionLaunchUI{},
		nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-123.sb"},
		nil,
		`{"kind":"hazmat.session"}`,
		"",
		`echo ok`,
		"arg1",
	)
	if !used || err != nil {
		t.Fatalf("tryRunNativeLaunchViaBroker used=%v err=%v", used, err)
	}
	if gotReq.PolicyPath != "/private/tmp/hazmat-123.sb" || !reflect.DeepEqual(gotReq.Args, []string{"arg1"}) {
		t.Fatalf("request = %+v", gotReq)
	}
	if got := launchBrokerOutputBuffer(t, launchBrokerStdout).String(); got != "out\n" {
		t.Fatalf("stdout = %q", got)
	}
	if got := launchBrokerOutputBuffer(t, launchBrokerStderr).String(); got != "{\"kind\":\"hazmat.session\"}\nerr\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestTryRunNativeLaunchViaBrokerDirectFallbackWhenColdDefaultUnavailable(t *testing.T) {
	t.Setenv(launchBrokerExperimentalEnv, "1")
	var calls int
	restore := replaceLaunchBrokerTestHooks(t,
		func(context.Context, string, launchbroker.LaunchRequest) (launchbroker.LaunchResponse, error) {
			calls++
			return launchbroker.LaunchResponse{}, fmt.Errorf("connect launch broker: %w", os.ErrNotExist)
		},
		func(context.Context) error {
			t.Fatal("default broker should not be started for cold direct exec fallback")
			return nil
		})
	defer restore()

	used, err := tryRunNativeLaunchViaBroker(
		sessionConfig{ProjectDir: "/Users/dr/workspace/project"},
		sessionBackendPlan{},
		sessionLaunchUI{},
		nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-123.sb"},
		nil,
		"",
		"",
		nativeDirectProjectExecScript,
		"/usr/bin/true",
	)
	if used || err != nil {
		t.Fatalf("tryRunNativeLaunchViaBroker used=%v err=%v, want direct sudo fallback", used, err)
	}
	if calls != 1 {
		t.Fatalf("broker calls = %d, want one cold probe", calls)
	}
}

func TestTryRunNativeLaunchViaBrokerExplicitSocketDoesNotUseDirectFallback(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "broker.sock")
	t.Setenv(launchBrokerSocketEnv, socketPath)
	restore := replaceLaunchBrokerTestHooks(t,
		func(context.Context, string, launchbroker.LaunchRequest) (launchbroker.LaunchResponse, error) {
			return launchbroker.LaunchResponse{}, fmt.Errorf("connect launch broker: %w", os.ErrNotExist)
		},
		func(context.Context) error {
			t.Fatal("explicit broker socket should not start default broker")
			return nil
		})
	defer restore()

	used, err := tryRunNativeLaunchViaBroker(
		sessionConfig{ProjectDir: "/Users/dr/workspace/project"},
		sessionBackendPlan{},
		sessionLaunchUI{},
		nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-123.sb"},
		nil,
		"",
		"",
		nativeDirectProjectExecScript,
		"/usr/bin/true",
	)
	if !used || err == nil {
		t.Fatalf("tryRunNativeLaunchViaBroker used=%v err=%v, want explicit broker error", used, err)
	}
}

func TestTryRunNativeLaunchViaBrokerFallsBackWhenExperimentalSocketUnavailable(t *testing.T) {
	t.Setenv(launchBrokerExperimentalEnv, "1")
	restore := replaceLaunchBrokerTestHooks(t,
		func(context.Context, string, launchbroker.LaunchRequest) (launchbroker.LaunchResponse, error) {
			return launchbroker.LaunchResponse{}, errors.New("connect launch broker: no such file")
		})
	defer restore()

	used, err := tryRunNativeLaunchViaBroker(
		sessionConfig{ProjectDir: "/Users/dr/workspace/project"},
		sessionBackendPlan{},
		sessionLaunchUI{},
		nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-123.sb"},
		nil,
		"",
		"",
		`echo ok`,
	)
	if used || err != nil {
		t.Fatalf("tryRunNativeLaunchViaBroker used=%v err=%v, want sudo fallback", used, err)
	}
}

func TestTryRunNativeLaunchViaBrokerStartsDefaultBrokerAndRetries(t *testing.T) {
	t.Setenv(launchBrokerExperimentalEnv, "1")
	var calls int
	var ensured bool
	restore := replaceLaunchBrokerTestHooks(t,
		func(context.Context, string, launchbroker.LaunchRequest) (launchbroker.LaunchResponse, error) {
			calls++
			if calls == 1 {
				return launchbroker.LaunchResponse{}, errors.New("connect launch broker: no such file")
			}
			return launchbroker.LaunchResponse{OK: true, Stdout: "ok\n"}, nil
		},
		func(context.Context) error {
			ensured = true
			return nil
		})
	defer restore()

	used, err := tryRunNativeLaunchViaBroker(
		sessionConfig{ProjectDir: "/Users/dr/workspace/project"},
		sessionBackendPlan{},
		sessionLaunchUI{},
		nativeLaunchPolicyArtifact{Path: "/private/tmp/hazmat-123.sb"},
		nil,
		"",
		"",
		`echo ok`,
	)
	if !used || err != nil {
		t.Fatalf("tryRunNativeLaunchViaBroker used=%v err=%v, want broker retry success", used, err)
	}
	if !ensured || calls != 2 {
		t.Fatalf("ensured=%v calls=%d, want ensure and two round trips", ensured, calls)
	}
	if got := launchBrokerOutputBuffer(t, launchBrokerStdout).String(); got != "ok\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestDefaultEnsureLaunchBrokerCachesStartedSupervisor(t *testing.T) {
	root := newShortLaunchBrokerTempDir(t)
	restore := replaceDefaultEnsureLaunchBrokerDependencies(t, root)
	defer restore()

	var starts int
	launchBrokerStartSupervisor = func(_ context.Context, cfg launchBrokerSupervisorConfig) (*launchBrokerSupervisor, error) {
		starts++
		return startCachedSupervisorListener(t, cfg)
	}

	if err := defaultEnsureLaunchBroker(context.Background()); err != nil {
		t.Fatalf("first defaultEnsureLaunchBroker: %v", err)
	}
	if err := defaultEnsureLaunchBroker(context.Background()); err != nil {
		t.Fatalf("second defaultEnsureLaunchBroker: %v", err)
	}
	if starts != 1 {
		t.Fatalf("broker starts = %d, want 1", starts)
	}
}

func TestDefaultEnsureLaunchBrokerRestartsWhenCachedSocketGone(t *testing.T) {
	root := newShortLaunchBrokerTempDir(t)
	restore := replaceDefaultEnsureLaunchBrokerDependencies(t, root)
	defer restore()

	var starts int
	launchBrokerStartSupervisor = func(_ context.Context, cfg launchBrokerSupervisorConfig) (*launchBrokerSupervisor, error) {
		starts++
		return startCachedSupervisorListener(t, cfg)
	}

	if err := defaultEnsureLaunchBroker(context.Background()); err != nil {
		t.Fatalf("first defaultEnsureLaunchBroker: %v", err)
	}
	defaultLaunchBrokerSupervisor.mu.Lock()
	if defaultLaunchBrokerSupervisor.supervisor == nil {
		t.Fatal("missing cached supervisor")
	}
	if err := defaultLaunchBrokerSupervisor.supervisor.Close(); err != nil {
		t.Fatalf("close cached supervisor: %v", err)
	}
	defaultLaunchBrokerSupervisor.mu.Unlock()

	if err := defaultEnsureLaunchBroker(context.Background()); err != nil {
		t.Fatalf("restart defaultEnsureLaunchBroker: %v", err)
	}
	if starts != 2 {
		t.Fatalf("broker starts = %d, want 2", starts)
	}
}

func TestPrepareDefaultLaunchBrokerRuntimeDirRepairsExistingDir(t *testing.T) {
	root := newShortLaunchBrokerTempDir(t)
	const uid = 501
	runtimeDir := filepath.Join(root, "launch-501")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatalf("create existing runtime dir: %v", err)
	}

	oldRoot := brokerRuntimeRoot
	oldEnsureSharedDir := brokerRuntimeAgentEnsureSharedDir
	brokerRuntimeRoot = root
	var ensureCalls int
	brokerRuntimeAgentEnsureSharedDir = func(path string, mode os.FileMode) error {
		ensureCalls++
		if path != runtimeDir {
			t.Fatalf("ensure path = %q, want %q", path, runtimeDir)
		}
		if mode != 0o2770 {
			t.Fatalf("ensure mode = %v, want 02770", mode)
		}
		if err := os.MkdirAll(path, mode.Perm()); err != nil {
			return err
		}
		return os.Chmod(path, mode)
	}
	t.Cleanup(func() {
		brokerRuntimeRoot = oldRoot
		brokerRuntimeAgentEnsureSharedDir = oldEnsureSharedDir
	})

	gotDir, err := prepareDefaultLaunchBrokerRuntimeDir(uid)
	if err != nil {
		t.Fatalf("prepareDefaultLaunchBrokerRuntimeDir: %v", err)
	}
	if gotDir != runtimeDir {
		t.Fatalf("runtime dir = %q, want %q", gotDir, runtimeDir)
	}
	if ensureCalls != 1 {
		t.Fatalf("ensure calls = %d, want 1", ensureCalls)
	}
	info, err := os.Stat(runtimeDir)
	if err != nil {
		t.Fatalf("stat runtime dir: %v", err)
	}
	if info.Mode().Perm() != 0o770 {
		t.Fatalf("runtime dir mode = %v, want permissions 0770", info.Mode())
	}
}

func TestDefaultRunAgentSeatbeltScriptWithPlanUsesConfiguredBroker(t *testing.T) {
	projectDir := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "broker.sock")
	t.Setenv(launchBrokerSocketEnv, socketPath)

	savedPrepareRuntime := prepareSessionRuntime
	prepareSessionRuntime = func(sessionConfig) (preparedSessionRuntime, error) {
		return preparedSessionRuntime{
			TempDir:             agentHome + "/.cache/hazmat/tmp/123-456",
			LaunchHelperTempDir: agentHome + "/.cache/hazmat/tmp/123-456",
			Cleanup:             func() {},
		}, nil
	}
	t.Cleanup(func() { prepareSessionRuntime = savedPrepareRuntime })

	savedSupportsDirectExec := launchHelperSupportsDirectExec
	launchHelperSupportsDirectExec = func(string) bool { return true }
	t.Cleanup(func() { launchHelperSupportsDirectExec = savedSupportsDirectExec })

	var gotReq launchbroker.LaunchRequest
	restore := replaceLaunchBrokerTestHooks(t,
		func(_ context.Context, gotSocket string, req launchbroker.LaunchRequest) (launchbroker.LaunchResponse, error) {
			if gotSocket != socketPath {
				t.Fatalf("socket path = %q, want %q", gotSocket, socketPath)
			}
			gotReq = req
			return launchbroker.LaunchResponse{
				OK:           true,
				MetadataJSON: req.MetadataJSON,
				Stdout:       "ok\n",
			}, nil
		})
	defer restore()

	err := defaultRunAgentSeatbeltScriptWithPlan(
		sessionConfig{
			ProjectDir:              projectDir,
			EmitSessionMetadataJSON: true,
			SkipGitHTTPSRuntime:     true,
		},
		sessionBackendPlan{},
		sessionLaunchUI{},
		nativeDirectProjectExecScript,
		"/usr/bin/true",
	)
	if err != nil {
		t.Fatalf("defaultRunAgentSeatbeltScriptWithPlan: %v", err)
	}
	if !gotReq.DirectExec || gotReq.WorkingDir != projectDir {
		t.Fatalf("broker request direct exec fields = direct=%v working=%q", gotReq.DirectExec, gotReq.WorkingDir)
	}
	if gotReq.SessionTempDir != agentHome+"/.cache/hazmat/tmp/123-456" {
		t.Fatalf("SessionTempDir = %q", gotReq.SessionTempDir)
	}
	if gotReq.MetadataJSON == "" {
		t.Fatal("broker request missing containment metadata JSON")
	}
	if got := launchBrokerOutputBuffer(t, launchBrokerStdout).String(); got != "ok\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestPrepareFallbackAgentTempRuntimeCreatesDirForOlderSudoHelper(t *testing.T) {
	tempDir := agentHome + "/.cache/hazmat/tmp/123-456"
	runtime := preparedSessionRuntime{
		TempDir:             tempDir,
		LaunchHelperTempDir: tempDir,
	}

	savedSupportsSessionTemp := launchHelperSupportsSessionTemp
	launchHelperSupportsSessionTemp = func(string) bool { return false }
	t.Cleanup(func() { launchHelperSupportsSessionTemp = savedSupportsSessionTemp })

	var gotPath string
	var gotMode os.FileMode
	savedEnsureDir := sessionRuntimeAgentEnsureDir
	sessionRuntimeAgentEnsureDir = func(path string, mode os.FileMode) error {
		gotPath = path
		gotMode = mode
		return nil
	}
	t.Cleanup(func() { sessionRuntimeAgentEnsureDir = savedEnsureDir })

	prepared, err := prepareFallbackAgentTempRuntime(&runtime)
	if err != nil {
		t.Fatalf("prepareFallbackAgentTempRuntime: %v", err)
	}
	if !prepared {
		t.Fatal("prepareFallbackAgentTempRuntime did not report fallback preparation")
	}
	if gotPath != tempDir || gotMode != 0o700 {
		t.Fatalf("agent ensure dir = (%q, %v), want (%q, 0700)", gotPath, gotMode, tempDir)
	}
	if runtime.LaunchHelperTempDir != "" {
		t.Fatalf("LaunchHelperTempDir = %q, want cleared for sudo fallback", runtime.LaunchHelperTempDir)
	}
}

func TestWriteLaunchBrokerResponsePreservesExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := writeLaunchBrokerResponse(launchbroker.LaunchResponse{
		OK:           true,
		ExitCode:     7,
		MetadataJSON: `{"kind":"hazmat.session"}`,
		Stdout:       "out\n",
		Stderr:       "err\n",
	}, `{"kind":"hazmat.session"}`, &stdout, &stderr)
	if err == nil || err.Error() != "exit status 7" {
		t.Fatalf("error = %v, want exit status 7", err)
	}
	var exitErr interface{ ExitCode() int }
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("error does not expose ExitCode 7: %T %v", err, err)
	}
	if stdout.String() != "out\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "{\"kind\":\"hazmat.session\"}\nerr\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func replaceLaunchBrokerTestHooks(t *testing.T, roundTrip func(context.Context, string, launchbroker.LaunchRequest) (launchbroker.LaunchResponse, error), ensure ...func(context.Context) error) func() {
	t.Helper()
	oldRoundTrip := launchBrokerRoundTrip
	oldEnsure := launchBrokerEnsureDefault
	oldStdout := launchBrokerStdout
	oldStderr := launchBrokerStderr
	ensureDefault := func(context.Context) error {
		return errors.New("test launch broker not available")
	}
	if len(ensure) > 0 {
		ensureDefault = ensure[0]
	}
	launchBrokerRoundTrip = roundTrip
	launchBrokerEnsureDefault = ensureDefault
	launchBrokerStdout = &bytes.Buffer{}
	launchBrokerStderr = &bytes.Buffer{}
	return func() {
		launchBrokerRoundTrip = oldRoundTrip
		launchBrokerEnsureDefault = oldEnsure
		launchBrokerStdout = oldStdout
		launchBrokerStderr = oldStderr
	}
}

func replaceDefaultEnsureLaunchBrokerDependencies(t *testing.T, root string) func() {
	t.Helper()
	oldRoot := brokerRuntimeRoot
	oldEnsureSharedDir := brokerRuntimeAgentEnsureSharedDir
	oldExecutablePath := currentExecutablePath
	oldStartSupervisor := launchBrokerStartSupervisor
	oldHelper, hadHelper := os.LookupEnv("HAZMAT_LAUNCH_HELPER")
	clearDefaultLaunchBrokerSupervisorForTest(t)

	brokerRuntimeRoot = root
	brokerRuntimeAgentEnsureSharedDir = func(path string, mode os.FileMode) error {
		if err := os.MkdirAll(path, mode.Perm()); err != nil {
			return err
		}
		return os.Chmod(path, mode)
	}
	currentExecutablePath = func() (string, error) {
		return "/usr/local/bin/hazmat", nil
	}
	if err := os.Setenv("HAZMAT_LAUNCH_HELPER", "/usr/local/libexec/hazmat-launch"); err != nil {
		t.Fatalf("set HAZMAT_LAUNCH_HELPER: %v", err)
	}

	return func() {
		clearDefaultLaunchBrokerSupervisorForTest(t)
		brokerRuntimeRoot = oldRoot
		brokerRuntimeAgentEnsureSharedDir = oldEnsureSharedDir
		currentExecutablePath = oldExecutablePath
		launchBrokerStartSupervisor = oldStartSupervisor
		if hadHelper {
			if err := os.Setenv("HAZMAT_LAUNCH_HELPER", oldHelper); err != nil {
				t.Fatalf("restore HAZMAT_LAUNCH_HELPER: %v", err)
			}
		} else if err := os.Unsetenv("HAZMAT_LAUNCH_HELPER"); err != nil {
			t.Fatalf("unset HAZMAT_LAUNCH_HELPER: %v", err)
		}
	}
}

func clearDefaultLaunchBrokerSupervisorForTest(t *testing.T) {
	t.Helper()
	defaultLaunchBrokerSupervisor.mu.Lock()
	defer defaultLaunchBrokerSupervisor.mu.Unlock()
	if defaultLaunchBrokerSupervisor.supervisor != nil {
		if err := defaultLaunchBrokerSupervisor.supervisor.Close(); err != nil {
			t.Fatalf("close cached launch broker supervisor: %v", err)
		}
		defaultLaunchBrokerSupervisor.supervisor = nil
	}
}

func startCachedSupervisorListener(t *testing.T, cfg launchBrokerSupervisorConfig) (*launchBrokerSupervisor, error) {
	t.Helper()
	socketPath := filepath.Join(cfg.RuntimeDir, cfg.SocketName)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	return &launchBrokerSupervisor{
		socketPath: socketPath,
		process:    &fakeLaunchBrokerProcess{listener: listener},
	}, nil
}
