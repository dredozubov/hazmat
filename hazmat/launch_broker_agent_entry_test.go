package hazmat

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hazmat/internal/agententry"
	"hazmat/internal/runtime/launchbroker"
)

func TestRunLaunchBrokerAgentEntryRejectsInvalidHelperPath(t *testing.T) {
	err := runLaunchBrokerAgentEntry(context.Background(), agententry.LaunchBrokerRequest{
		SocketPath:      filepath.Join(t.TempDir(), "broker.sock"),
		ExpectedPeerUID: os.Getuid(),
		LaunchHelper:    "hazmat-launch",
	})
	if err == nil {
		t.Fatal("runLaunchBrokerAgentEntry accepted relative helper path")
	}
}

func TestRootCommandRegistersLaunchBrokerAgentEntry(t *testing.T) {
	if cmd := findCommandByName(NewRootCommand(), "_launch_broker"); cmd == nil {
		t.Fatal("root command missing _launch_broker hidden command")
	}
}

func TestLaunchBrokerAgentEntryServiceConfigPropagatesHelperProfile(t *testing.T) {
	t.Setenv("HAZMAT_SESSION_PREP_PROFILE", "yes")

	cfg := launchBrokerAgentEntryServiceConfig(agententry.LaunchBrokerRequest{
		SocketPath:      filepath.Join(t.TempDir(), "broker.sock"),
		ExpectedPeerUID: os.Getuid(),
		LaunchHelper:    "/usr/local/libexec/hazmat-launch",
	})
	if !cfg.Helper.Profile {
		t.Fatal("Helper.Profile = false, want true when session preparation profiling is enabled")
	}
}

func TestLaunchBrokerServiceCommandPathWithFakeRunner(t *testing.T) {
	socketPath := filepath.Join(newShortBrokerTestDir(t), "broker.sock")
	service := startBrokerServiceWithFakeRunner(t, socketPath, os.Getuid())

	req := launchbroker.LaunchRequest{
		PolicyPath:     "/private/tmp/hazmat-123.sb",
		MetadataJSON:   `{"kind":"hazmat.session"}`,
		DirectExec:     true,
		WorkingDir:     "/Users/dr/workspace/project",
		SessionTempDir: "/Users/agent/.cache/hazmat/tmp/123-456",
		EnvPairs:       []string{"HOME=/Users/agent", "PATH=/usr/bin"},
		Args:           []string{"/usr/bin/true"},
	}
	resp, err := launchbroker.Client{SocketPath: service.SocketPath(), Timeout: time.Second}.Launch(context.Background(), req)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !resp.OK || resp.MetadataJSON != req.MetadataJSON {
		t.Fatalf("response = %+v", resp)
	}
}

func startBrokerServiceWithFakeRunner(t *testing.T, socketPath string, expectedUID int) *launchbroker.Service {
	t.Helper()
	service, err := launchbroker.StartService(context.Background(), launchbroker.ServiceConfig{
		SocketPath:      socketPath,
		ExpectedPeerUID: expectedUID,
		ResolvePeerUID: func(conn *net.UnixConn) (int, error) {
			return expectedUID, nil
		},
		Helper: launchbroker.HelperExecutorConfig{
			LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
			Runner: launchbrokerFakeRunner{
				result: launchbroker.HelperRunResult{
					ExitCode: 0,
					Stderr:   []byte("{\"kind\":\"hazmat.session\"}\n"),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	<-service.Ready()
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("close service: %v", err)
		}
	})
	return service
}

type launchbrokerFakeRunner struct {
	result launchbroker.HelperRunResult
	err    error
}

func (r launchbrokerFakeRunner) Run(ctx context.Context, command launchbroker.HelperCommand) (launchbroker.HelperRunResult, error) {
	return r.result, r.err
}

func newShortBrokerTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hazmat-agent-lb-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

func TestLaunchBrokerServiceCommandCancellationCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	socketPath := filepath.Join(newShortBrokerTestDir(t), "broker.sock")
	done := make(chan error, 1)
	go func() {
		done <- runLaunchBrokerAgentEntry(ctx, agententry.LaunchBrokerRequest{
			SocketPath:      socketPath,
			ExpectedPeerUID: os.Getuid(),
			LaunchHelper:    "/usr/local/libexec/hazmat-launch",
		})
	}()

	deadline := time.After(time.Second)
	for {
		if info, err := os.Stat(socketPath); err == nil {
			if got := info.Mode().Perm(); got != 0o660 {
				t.Fatalf("broker socket mode = %04o, want 0660", got)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("broker socket was not created")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runLaunchBrokerAgentEntry returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runLaunchBrokerAgentEntry did not stop after cancellation")
	}
	if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still exists after cancellation, stat err=%v", err)
	}
}
