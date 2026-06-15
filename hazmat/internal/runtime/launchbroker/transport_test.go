package launchbroker

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTransportBuildsAuthenticatedChildPlan(t *testing.T) {
	ctx := context.Background()
	var sawPlan bool
	var gotUID int
	var gotReq LaunchRequest
	var requiresCleanup bool

	server := newTestServer(t, 501, func(ctx context.Context, plan ChildPlan) (LaunchResponse, error) {
		sawPlan = true
		gotUID = plan.Request.PeerUID()
		gotReq = plan.Request.Request()
		requiresCleanup = plan.RequiresInheritedFDCleanup()
		return LaunchResponse{OK: true, ExitCode: 7, MetadataJSON: `{"containment":"confirmed"}`}, nil
	})

	errCh := serveOnce(t, ctx, server)
	resp, err := Client{SocketPath: server.SocketPath(), Timeout: time.Second}.Launch(ctx, validDirectRequest())
	if err != nil {
		t.Fatalf("Launch valid request: %v", err)
	}
	if resp.ExitCode != 7 || resp.MetadataJSON == "" {
		t.Fatalf("unexpected launch response: %+v", resp)
	}
	assertServeOnce(t, errCh)

	if !sawPlan {
		t.Fatal("launch handler was not called")
	}
	if gotUID != 501 {
		t.Fatalf("handler saw peer uid %d, want 501", gotUID)
	}
	if gotReq.Args[0] != "/usr/bin/true" {
		t.Fatalf("handler saw request args %v", gotReq.Args)
	}
	if !requiresCleanup {
		t.Fatal("child plan does not require inherited fd cleanup")
	}
}

func TestTransportRejectsMismatchedPeerBeforeHandler(t *testing.T) {
	ctx := context.Background()
	var handlerCalled bool
	server := newTestServerWithResolver(t, 501, func(conn *net.UnixConn) (int, error) {
		return 502, nil
	}, func(ctx context.Context, plan ChildPlan) (LaunchResponse, error) {
		handlerCalled = true
		return LaunchResponse{OK: true}, nil
	})

	errCh := serveOnce(t, ctx, server)
	resp, err := Client{SocketPath: server.SocketPath(), Timeout: time.Second}.Launch(ctx, validDirectRequest())
	if err == nil {
		t.Fatal("Launch accepted mismatched peer uid")
	}
	if resp.OK {
		t.Fatalf("mismatched peer response marked OK: %+v", resp)
	}
	if !strings.Contains(err.Error(), "does not match expected uid") {
		t.Fatalf("unexpected mismatch error: %v", err)
	}
	assertServeOnce(t, errCh)

	if handlerCalled {
		t.Fatal("handler called for mismatched peer")
	}
}

func TestTransportRejectsInvalidLaunchRequestBeforeHandler(t *testing.T) {
	ctx := context.Background()
	var handlerCalled bool
	server := newTestServer(t, 501, func(ctx context.Context, plan ChildPlan) (LaunchResponse, error) {
		handlerCalled = true
		return LaunchResponse{OK: true}, nil
	})

	req := validDirectRequest()
	req.WorkingDir = ""
	errCh := serveOnce(t, ctx, server)
	resp, err := Client{SocketPath: server.SocketPath(), Timeout: time.Second}.Launch(ctx, req)
	if err == nil {
		t.Fatal("Launch accepted invalid direct request")
	}
	if resp.OK {
		t.Fatalf("invalid request response marked OK: %+v", resp)
	}
	if !strings.Contains(err.Error(), "direct exec requires a working directory") {
		t.Fatalf("unexpected invalid request error: %v", err)
	}
	assertServeOnce(t, errCh)

	if handlerCalled {
		t.Fatal("handler called for invalid request")
	}
}

func TestTransportRejectsUnknownRequestFields(t *testing.T) {
	ctx := context.Background()
	var handlerCalled bool
	server := newTestServer(t, 501, func(ctx context.Context, plan ChildPlan) (LaunchResponse, error) {
		handlerCalled = true
		return LaunchResponse{OK: true}, nil
	})

	conn, err := net.DialTimeout("unix", server.SocketPath(), time.Second)
	if err != nil {
		t.Fatalf("dial broker socket: %v", err)
	}
	defer conn.Close()

	errCh := serveOnce(t, ctx, server)
	if err := json.NewEncoder(conn).Encode(map[string]any{
		"PolicyPath": "/private/tmp/hazmat-123.sb",
		"DirectExec": true,
		"WorkingDir": "/Users/dr/workspace/project",
		"Args":       []string{"/usr/bin/true"},
		"Surprise":   true,
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var resp LaunchResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.OK {
		t.Fatalf("unknown field response marked OK: %+v", resp)
	}
	if !strings.Contains(resp.Error, "unknown field") {
		t.Fatalf("unexpected unknown field error: %+v", resp)
	}
	assertServeOnce(t, errCh)

	if handlerCalled {
		t.Fatal("handler called for unknown request field")
	}
}

func TestTransportAuthenticatesDefaultDarwinPeer(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("default Unix peer uid resolver is currently implemented on Darwin")
	}

	ctx := context.Background()
	var sawPlan bool
	var gotUID int
	var requiresCleanup bool
	server, err := Listen(ServerConfig{
		SocketPath:      newTestSocketPath(t),
		ExpectedPeerUID: os.Getuid(),
		HandleLaunch: func(ctx context.Context, plan ChildPlan) (LaunchResponse, error) {
			sawPlan = true
			gotUID = plan.Request.PeerUID()
			requiresCleanup = plan.RequiresInheritedFDCleanup()
			return LaunchResponse{OK: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("Listen default peer broker: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("close default peer broker: %v", err)
		}
	})

	errCh := serveOnce(t, ctx, server)
	if _, err := (Client{SocketPath: server.SocketPath(), Timeout: time.Second}).Launch(ctx, validDirectRequest()); err != nil {
		t.Fatalf("Launch with default peer resolver: %v", err)
	}
	assertServeOnce(t, errCh)

	if !sawPlan {
		t.Fatal("launch handler was not called")
	}
	if gotUID != os.Getuid() {
		t.Fatalf("peer uid = %d, want %d", gotUID, os.Getuid())
	}
	if !requiresCleanup {
		t.Fatal("child plan does not require inherited fd cleanup")
	}
}

func TestTransportRunsHelperExecutorHandler(t *testing.T) {
	ctx := context.Background()
	runner := &recordingHelperRunner{result: HelperRunResult{
		ExitCode: 9,
		Stdout:   []byte("stdout\n"),
		Stderr:   []byte("{\"kind\":\"hazmat.session\"}\nstderr\n"),
	}}
	handler, err := NewHelperLaunchHandler(HelperExecutorConfig{
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
		Runner:           runner,
	})
	if err != nil {
		t.Fatalf("NewHelperLaunchHandler: %v", err)
	}
	server := newTestServer(t, 501, handler)

	req := validDirectRequest()
	req.MetadataJSON = `{"kind":"hazmat.session"}`
	errCh := serveOnce(t, ctx, server)
	resp, err := Client{SocketPath: server.SocketPath(), Timeout: time.Second}.Launch(ctx, req)
	if err != nil {
		t.Fatalf("Launch via helper executor handler: %v", err)
	}
	assertServeOnce(t, errCh)

	if !resp.OK || resp.ExitCode != 9 {
		t.Fatalf("response = %+v", resp)
	}
	if resp.MetadataJSON != req.MetadataJSON {
		t.Fatalf("MetadataJSON = %q", resp.MetadataJSON)
	}
	if resp.Stdout != "stdout\n" || resp.Stderr != "stderr\n" {
		t.Fatalf("stdout/stderr = %q/%q", resp.Stdout, resp.Stderr)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("runner saw %d commands, want 1", len(runner.commands))
	}
}

func BenchmarkRoundTripPlanOnly(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := newBenchmarkServer(b)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ctx)
	}()
	client := Client{SocketPath: server.SocketPath(), Timeout: time.Second}
	req := validDirectRequest()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Launch(ctx, req)
		if err != nil {
			b.Fatalf("Launch benchmark request: %v", err)
		}
		if !resp.OK {
			b.Fatalf("benchmark response was not OK: %+v", resp)
		}
	}
	b.StopTimer()

	cancel()
	if err := server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		b.Fatalf("close server: %v", err)
	}
	if err := <-errCh; err != nil {
		b.Fatalf("serve broker: %v", err)
	}
}

func newTestServer(t *testing.T, expectedUID int, handler LaunchHandler) *Server {
	t.Helper()
	return newTestServerWithResolver(t, expectedUID, func(conn *net.UnixConn) (int, error) {
		return expectedUID, nil
	}, handler)
}

func newTestServerWithResolver(t *testing.T, expectedUID int, resolver PeerUIDResolver, handler LaunchHandler) *Server {
	t.Helper()
	socketPath := newTestSocketPath(t)
	server, err := Listen(ServerConfig{
		SocketPath:      socketPath,
		ExpectedPeerUID: expectedUID,
		ResolvePeerUID:  resolver,
		HandleLaunch:    handler,
	})
	if err != nil {
		t.Fatalf("Listen test broker: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("close test broker: %v", err)
		}
	})
	return server
}

func newBenchmarkServer(b *testing.B) *Server {
	b.Helper()
	socketPath := newBenchmarkSocketPath(b)
	server, err := Listen(ServerConfig{
		SocketPath:      socketPath,
		ExpectedPeerUID: 501,
		ResolvePeerUID: func(conn *net.UnixConn) (int, error) {
			return 501, nil
		},
		HandleLaunch: func(ctx context.Context, plan ChildPlan) (LaunchResponse, error) {
			if !plan.RequiresInheritedFDCleanup() {
				return LaunchResponse{}, errors.New("child plan missing inherited fd cleanup")
			}
			return LaunchResponse{OK: true}, nil
		},
	})
	if err != nil {
		b.Fatalf("Listen benchmark broker: %v", err)
	}
	b.Cleanup(func() {
		_ = server.Close()
	})
	return server
}

func newTestSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hazmat-lb-test-*")
	if err != nil {
		t.Fatalf("create socket temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return filepath.Join(dir, "broker.sock")
}

func newBenchmarkSocketPath(b *testing.B) string {
	b.Helper()
	dir, err := os.MkdirTemp("/tmp", "hazmat-lb-bench-*")
	if err != nil {
		b.Fatalf("create socket temp dir: %v", err)
	}
	b.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return filepath.Join(dir, "broker.sock")
}

func serveOnce(t *testing.T, ctx context.Context, server *Server) <-chan error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ServeOnce(ctx)
	}()
	return errCh
}

func assertServeOnce(t *testing.T, errCh <-chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ServeOnce: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeOnce did not finish")
	}
}
