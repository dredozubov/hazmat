package hazmat

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"hazmat/internal/runtime/launchbroker"
)

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
	if !req.DirectExec || req.WorkingDir != cfg.ProjectDir || req.Script != nativeDirectProjectExecScript {
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
	if got := launchBrokerStdout.(*bytes.Buffer).String(); got != "out\n" {
		t.Fatalf("stdout = %q", got)
	}
	if got := launchBrokerStderr.(*bytes.Buffer).String(); got != "{\"kind\":\"hazmat.session\"}\nerr\n" {
		t.Fatalf("stderr = %q", got)
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
	if got := launchBrokerStdout.(*bytes.Buffer).String(); got != "ok\n" {
		t.Fatalf("stdout = %q", got)
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

func replaceLaunchBrokerTestHooks(t *testing.T, roundTrip func(context.Context, string, launchbroker.LaunchRequest) (launchbroker.LaunchResponse, error)) func() {
	t.Helper()
	oldRoundTrip := launchBrokerRoundTrip
	oldStdout := launchBrokerStdout
	oldStderr := launchBrokerStderr
	launchBrokerRoundTrip = roundTrip
	launchBrokerStdout = &bytes.Buffer{}
	launchBrokerStderr = &bytes.Buffer{}
	return func() {
		launchBrokerRoundTrip = oldRoundTrip
		launchBrokerStdout = oldStdout
		launchBrokerStderr = oldStderr
	}
}
