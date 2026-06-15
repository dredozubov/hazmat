package launchbroker

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceRunsDefaultHelperExecutor(t *testing.T) {
	ctx := context.Background()
	runner := &recordingHelperRunner{result: HelperRunResult{
		ExitCode: 0,
		Stdout:   []byte("ok\n"),
		Stderr:   []byte("{\"kind\":\"hazmat.session\"}\n"),
	}}
	service := startTestService(t, ServiceConfig{
		SocketPath:      newTestSocketPath(t),
		ExpectedPeerUID: 501,
		ResolvePeerUID: func(conn *net.UnixConn) (int, error) {
			return 501, nil
		},
		Helper: HelperExecutorConfig{
			LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
			Runner:           runner,
		},
	})

	req := validDirectRequest()
	req.MetadataJSON = `{"kind":"hazmat.session"}`
	resp, err := Client{SocketPath: service.SocketPath(), Timeout: time.Second}.Launch(ctx, req)
	if err != nil {
		t.Fatalf("Launch through service: %v", err)
	}
	if !resp.OK || resp.MetadataJSON != req.MetadataJSON || resp.Stdout != "ok\n" {
		t.Fatalf("response = %+v", resp)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("runner saw %d commands, want 1", len(runner.commands))
	}
}

func TestServiceReadyAndCloseCleanup(t *testing.T) {
	socketPath := filepath.Join(newShortTempDir(t, "hazmat-lb-svc-*"), "broker.sock")
	service := startTestService(t, ServiceConfig{
		SocketPath:      socketPath,
		ExpectedPeerUID: 501,
		ResolvePeerUID: func(conn *net.UnixConn) (int, error) {
			return 501, nil
		},
		HandleLaunch: func(ctx context.Context, plan ChildPlan) (LaunchResponse, error) {
			return LaunchResponse{OK: true}, nil
		},
	})

	select {
	case <-service.Ready():
	case <-time.After(time.Second):
		t.Fatal("service did not become ready")
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("stat service socket: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still exists after close, stat err=%v", err)
	}
	select {
	case err := <-service.Done():
		if err != nil {
			t.Fatalf("Done error = %v", err)
		}
	default:
		t.Fatal("Done was not signaled after Close")
	}
}

func TestServiceContextCancellationStopsAndCleansSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	socketPath := filepath.Join(newShortTempDir(t, "hazmat-lb-cancel-*"), "broker.sock")
	service, err := StartService(ctx, ServiceConfig{
		SocketPath:      socketPath,
		ExpectedPeerUID: 501,
		ResolvePeerUID: func(conn *net.UnixConn) (int, error) {
			return 501, nil
		},
		HandleLaunch: func(ctx context.Context, plan ChildPlan) (LaunchResponse, error) {
			return LaunchResponse{OK: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	<-service.Ready()
	cancel()

	select {
	case err := <-service.Done():
		if err != nil {
			t.Fatalf("Done error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("service did not stop after context cancellation")
	}
	if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still exists after cancellation, stat err=%v", err)
	}
}

func TestServiceRequiresLaunchHandlerOrHelper(t *testing.T) {
	_, err := StartService(context.Background(), ServiceConfig{
		SocketPath:      filepath.Join(newShortTempDir(t, "hazmat-lb-invalid-*"), "broker.sock"),
		ExpectedPeerUID: 501,
	})
	if err == nil {
		t.Fatal("StartService accepted config without handler or helper")
	}
}

func BenchmarkServiceRoundTripHelperExecutor(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &recordingHelperRunner{result: HelperRunResult{
		ExitCode: 0,
		Stderr:   []byte("{\"kind\":\"hazmat.session\"}\n"),
	}}
	service, err := StartService(ctx, ServiceConfig{
		SocketPath:      newBenchmarkSocketPath(b),
		ExpectedPeerUID: 501,
		ResolvePeerUID: func(conn *net.UnixConn) (int, error) {
			return 501, nil
		},
		Helper: HelperExecutorConfig{
			LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
			Runner:           runner,
		},
	})
	if err != nil {
		b.Fatalf("StartService: %v", err)
	}
	<-service.Ready()
	b.Cleanup(func() {
		if err := service.Close(); err != nil {
			b.Fatalf("close service: %v", err)
		}
	})

	client := Client{SocketPath: service.SocketPath(), Timeout: time.Second}
	req := validDirectRequest()
	req.MetadataJSON = `{"kind":"hazmat.session"}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Launch(ctx, req)
		if err != nil {
			b.Fatalf("Launch: %v", err)
		}
		if !resp.OK || resp.MetadataJSON != req.MetadataJSON {
			b.Fatalf("response = %+v", resp)
		}
	}
}

func startTestService(t *testing.T, cfg ServiceConfig) *Service {
	t.Helper()
	service, err := StartService(context.Background(), cfg)
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	select {
	case <-service.Ready():
	case <-time.After(time.Second):
		t.Fatal("service did not become ready")
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("close service: %v", err)
		}
	})
	return service
}

func newShortTempDir(t *testing.T, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", pattern)
	if err != nil {
		t.Fatalf("create short temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}
