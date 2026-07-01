package proxyruntime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestProcessRunnerRunsFakeChildWithSeparatedStreams(t *testing.T) {
	stdin := &bufferWriteCloser{}
	handle := &fakeProcessHandle{
		stdin:  stdin,
		stdout: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0"}`)),
		stderr: io.NopCloser(strings.NewReader("debug log\n")),
	}
	starter := &fakeProcessStarter{handle: handle}
	var cleanupCalls int

	result, err := (ProcessRunner{Starter: starter}).Run(context.Background(), ProcessRequest{
		Spec: ProcessSpec{
			Command: "fake-server",
			Args:    []string{"--stdio"},
			Env:     []string{"A=1"},
		},
		Event: EventInput{
			SessionID:  "session-1",
			ProxyKind:  ProxyKindMCPStdio,
			AttachKind: AttachKindStdio,
		},
		Cleanup: func() { cleanupCalls++ },
	}, func(_ context.Context, streams ProcessStreams) error {
		if _, err := streams.Stdin.Write([]byte("request")); err != nil {
			return err
		}
		stdout, err := io.ReadAll(streams.Stdout)
		if err != nil {
			return err
		}
		stderr, err := io.ReadAll(streams.Stderr)
		if err != nil {
			return err
		}
		if string(stdout) != `{"jsonrpc":"2.0"}` {
			return errors.New("stdout was not protocol stream")
		}
		if string(stderr) != "debug log\n" {
			return errors.New("stderr was not separate log stream")
		}
		if strings.Contains(string(stdout), "debug log") {
			return errors.New("stderr corrupted protocol stdout")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Started || cleanupCalls != 1 || !handle.waited || handle.killed {
		t.Fatalf("result started=%v cleanup=%d waited=%v killed=%v", result.Started, cleanupCalls, handle.waited, handle.killed)
	}
	if got := stdin.String(); got != "request" {
		t.Fatalf("stdin = %q, want request", got)
	}
	if starter.spec.Command != "fake-server" || !slices.Equal(starter.spec.Args, []string{"--stdio"}) ||
		!slices.Equal(starter.spec.Env, []string{"A=1"}) {
		t.Fatalf("starter spec = %+v", starter.spec)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Operation != "process:start" ||
		result.Events[1].Operation != "process:exit" {
		t.Fatalf("events = %+v", result.Events)
	}
}

func TestProcessRunnerStartFailureCleansUp(t *testing.T) {
	errStart := errors.New("start failed")
	var cleanupCalls int
	var events []Event

	result, err := (ProcessRunner{
		Starter: &fakeProcessStarter{err: errStart},
		Events:  func(event Event) { events = append(events, event) },
	}).Run(context.Background(), ProcessRequest{
		Spec:    ProcessSpec{Command: "missing"},
		Cleanup: func() { cleanupCalls++ },
	}, func(context.Context, ProcessStreams) error {
		t.Fatal("loop should not run after start failure")
		return nil
	})
	if !errors.Is(err, errStart) {
		t.Fatalf("Run error = %v, want %v", err, errStart)
	}
	if result.Started || cleanupCalls != 1 {
		t.Fatalf("started=%v cleanup=%d, want not started and one cleanup", result.Started, cleanupCalls)
	}
	if len(events) != 2 || events[1].Decision != DecisionError || events[1].Operation != "process:start" {
		t.Fatalf("events = %+v, want start error event", events)
	}
}

func TestProcessRunnerLoopFailureKillsWaitsAndCleansUp(t *testing.T) {
	errLoop := errors.New("protocol loop failed")
	var cleanupCalls int
	handle := &fakeProcessHandle{
		stdin:  &bufferWriteCloser{},
		stdout: io.NopCloser(strings.NewReader("")),
		stderr: io.NopCloser(strings.NewReader("")),
	}

	result, err := (ProcessRunner{Starter: &fakeProcessStarter{handle: handle}}).Run(context.Background(), ProcessRequest{
		Spec:    ProcessSpec{Command: "fake"},
		Cleanup: func() { cleanupCalls++ },
	}, func(context.Context, ProcessStreams) error {
		return errLoop
	})
	if !errors.Is(err, errLoop) {
		t.Fatalf("Run error = %v, want %v", err, errLoop)
	}
	if !result.Started || cleanupCalls != 1 || !handle.killed || !handle.waited {
		t.Fatalf("started=%v cleanup=%d killed=%v waited=%v", result.Started, cleanupCalls, handle.killed, handle.waited)
	}
	if len(result.Events) != 2 ||
		result.Events[1].Operation != "process:loop" ||
		result.Events[1].Decision != DecisionError {
		t.Fatalf("events = %+v, want loop error event", result.Events)
	}
}

type fakeProcessStarter struct {
	handle *fakeProcessHandle
	err    error
	spec   ProcessSpec
}

func (s *fakeProcessStarter) Start(_ context.Context, spec ProcessSpec) (ProcessHandle, error) {
	s.spec = spec
	if s.err != nil {
		return nil, s.err
	}
	return s.handle, nil
}

type fakeProcessHandle struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	killed bool
	waited bool
}

func (h *fakeProcessHandle) Stdin() io.WriteCloser {
	return h.stdin
}

func (h *fakeProcessHandle) Stdout() io.ReadCloser {
	return h.stdout
}

func (h *fakeProcessHandle) Stderr() io.ReadCloser {
	return h.stderr
}

func (h *fakeProcessHandle) Wait() error {
	h.waited = true
	return nil
}

func (h *fakeProcessHandle) Kill() error {
	h.killed = true
	return nil
}

type bufferWriteCloser struct {
	bytes.Buffer
	closed bool
}

func (w *bufferWriteCloser) Close() error {
	w.closed = true
	return nil
}
