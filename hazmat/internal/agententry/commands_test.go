package agententry

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestGitSSHTransportCommandRequiresSocketAndSSHArgs(t *testing.T) {
	cmd := NewGitSSHTransportCommand(func(string, []string) int {
		t.Fatal("runner should not be called for invalid args")
		return 1
	})
	cmd.SetArgs([]string{"socket-only"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() succeeded, want argument error")
	}
}

func TestGitSSHTransportCommandRunsTransportAndExits(t *testing.T) {
	var gotSocket string
	var gotArgs []string
	var gotExit int
	cmd := newGitSSHTransportCommand(
		func(socketPath string, args []string) int {
			gotSocket = socketPath
			gotArgs = append([]string(nil), args...)
			return 23
		},
		func(code int) {
			gotExit = code
		},
	)
	cmd.SetArgs([]string{"broker.sock", "ssh", "-W", "example.com:22"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if gotSocket != "broker.sock" {
		t.Fatalf("socket = %q, want broker.sock", gotSocket)
	}
	wantArgs := []string{"ssh", "-W", "example.com:22"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
	if gotExit != 23 {
		t.Fatalf("exit = %d, want 23", gotExit)
	}
}

func TestGitHTTPSCredentialCommandRoutesPayloadAndStreamsResponse(t *testing.T) {
	var gotSocket string
	var gotOperation string
	var gotPayload []byte
	cmd := NewGitHTTPSCredentialCommand(
		func(socketPath, operation string, payload []byte) (GitHTTPSCredentialResponse, error) {
			gotSocket = socketPath
			gotOperation = operation
			gotPayload = append([]byte(nil), payload...)
			return GitHTTPSCredentialResponse{
				Stdout: []byte("username=alice\n"),
				Stderr: []byte("trace\n"),
			}, nil
		},
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetArgs([]string{"credential.sock", "get"})
	cmd.SetIn(strings.NewReader("protocol=https\nhost=example.com\n\n"))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if gotSocket != "credential.sock" {
		t.Fatalf("socket = %q, want credential.sock", gotSocket)
	}
	if gotOperation != "get" {
		t.Fatalf("operation = %q, want get", gotOperation)
	}
	if string(gotPayload) != "protocol=https\nhost=example.com\n\n" {
		t.Fatalf("payload = %q", string(gotPayload))
	}
	if stdout.String() != "username=alice\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "trace\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestLaunchBrokerCommandValidatesPeerUID(t *testing.T) {
	cmd := NewLaunchBrokerCommand(func(context.Context, LaunchBrokerRequest) error {
		t.Fatal("runner should not be called for invalid uid")
		return nil
	})
	cmd.SetArgs([]string{"broker.sock", "not-a-uid", "/usr/local/libexec/hazmat-launch"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() succeeded, want uid validation error")
	}
}

func TestLaunchBrokerCommandRunsServiceRunner(t *testing.T) {
	var got LaunchBrokerRequest
	cmd := NewLaunchBrokerCommand(func(ctx context.Context, req LaunchBrokerRequest) error {
		got = req
		return nil
	})
	cmd.SetArgs([]string{"broker.sock", "501", "/usr/local/libexec/hazmat-launch"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	want := LaunchBrokerRequest{
		SocketPath:      "broker.sock",
		ExpectedPeerUID: 501,
		LaunchHelper:    "/usr/local/libexec/hazmat-launch",
	}
	if got != want {
		t.Fatalf("request = %+v, want %+v", got, want)
	}
}
