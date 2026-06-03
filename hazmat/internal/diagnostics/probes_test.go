package diagnostics

import (
	"errors"
	"reflect"
	"testing"

	"hazmat/internal/hostexec"
)

func TestAgentTCPConnectUsesHiddenCommandWhenSelfPathIsKnown(t *testing.T) {
	var gotEnv hostexec.Env
	var gotArgs []string
	ok := agentTCPConnect(
		hostexec.Env{AgentUser: "agent"},
		"/usr/local/bin/hazmat",
		"127.0.0.1",
		"8080",
		func(env hostexec.Env, args ...string) error {
			gotEnv = env
			gotArgs = append([]string(nil), args...)
			return nil
		},
		func(hostexec.Env, string) error {
			t.Fatal("fallback shell should not run")
			return nil
		},
	)

	if !ok {
		t.Fatal("agentTCPConnect() = false, want true")
	}
	if gotEnv.AgentUser != "agent" {
		t.Fatalf("env = %+v", gotEnv)
	}
	wantArgs := []string{"/usr/local/bin/hazmat", "_connect", "127.0.0.1", "8080"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestAgentTCPConnectFallsBackToShellWhenSelfPathIsMissing(t *testing.T) {
	var gotScript string
	ok := agentTCPConnect(
		hostexec.Env{},
		"",
		"example.com",
		"443",
		func(hostexec.Env, ...string) error {
			t.Fatal("hidden command should not run")
			return nil
		},
		func(_ hostexec.Env, script string) error {
			gotScript = script
			return errors.New("connection refused")
		},
	)

	if ok {
		t.Fatal("agentTCPConnect() = true, want false")
	}
	wantScript := "timeout 3 bash -c 'echo > /dev/tcp/example.com/443' 2>/dev/null"
	if gotScript != wantScript {
		t.Fatalf("script = %q, want %q", gotScript, wantScript)
	}
}
