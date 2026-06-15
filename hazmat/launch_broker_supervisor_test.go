package hazmat

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hazmat/internal/agententry"
)

func TestNewLaunchBrokerStartPlanUsesHelperExecCleanupBoundary(t *testing.T) {
	runtimeDir := t.TempDir()
	helperPath := "/usr/local/libexec/hazmat-launch"
	hazmatPath := "/usr/local/bin/hazmat"

	plan, err := newLaunchBrokerStartPlan(launchBrokerStartConfig{
		RuntimeDir:       runtimeDir,
		ExpectedPeerUID:  501,
		HazmatPath:       hazmatPath,
		LaunchHelperPath: helperPath,
	})
	if err != nil {
		t.Fatalf("newLaunchBrokerStartPlan: %v", err)
	}

	socketPath := filepath.Join(runtimeDir, "launch-broker-501.sock")
	if plan.SocketPath() != socketPath {
		t.Fatalf("SocketPath() = %q, want %q", plan.SocketPath(), socketPath)
	}

	command := plan.Command()
	if command.Path() != hostSudoPath {
		t.Fatalf("Path() = %q, want %q", command.Path(), hostSudoPath)
	}
	if command.Dir() != "/" {
		t.Fatalf("Dir() = %q, want /", command.Dir())
	}
	wantArgs := []string{
		hostSudoPath,
		"-n",
		"-u", agentUser,
		"-H",
		helperPath,
		"exec",
		hazmatPath,
		agententry.LaunchBrokerCommandName,
		socketPath,
		"501",
		helperPath,
	}
	if !reflect.DeepEqual(command.Args(), wantArgs) {
		t.Fatalf("Args() = %#v, want %#v", command.Args(), wantArgs)
	}
}

func TestNewLaunchBrokerStartPlanRejectsInvalidInputs(t *testing.T) {
	validRuntime := t.TempDir()
	validHazmat := "/usr/local/bin/hazmat"
	validHelper := "/usr/local/libexec/hazmat-launch"

	tests := []struct {
		name string
		cfg  launchBrokerStartConfig
		want string
	}{
		{
			name: "relative runtime dir",
			cfg: launchBrokerStartConfig{
				RuntimeDir:       "tmp",
				ExpectedPeerUID:  501,
				HazmatPath:       validHazmat,
				LaunchHelperPath: validHelper,
			},
			want: "runtime dir",
		},
		{
			name: "relative hazmat path",
			cfg: launchBrokerStartConfig{
				RuntimeDir:       validRuntime,
				ExpectedPeerUID:  501,
				HazmatPath:       "hazmat",
				LaunchHelperPath: validHelper,
			},
			want: "hazmat path",
		},
		{
			name: "relative helper path",
			cfg: launchBrokerStartConfig{
				RuntimeDir:       validRuntime,
				ExpectedPeerUID:  501,
				HazmatPath:       validHazmat,
				LaunchHelperPath: "hazmat-launch",
			},
			want: "launch helper path",
		},
		{
			name: "nonpositive uid",
			cfg: launchBrokerStartConfig{
				RuntimeDir:       validRuntime,
				ExpectedPeerUID:  0,
				HazmatPath:       validHazmat,
				LaunchHelperPath: validHelper,
			},
			want: "expected peer uid",
		},
		{
			name: "socket traversal",
			cfg: launchBrokerStartConfig{
				RuntimeDir:       validRuntime,
				SocketName:       "../broker.sock",
				ExpectedPeerUID:  501,
				HazmatPath:       validHazmat,
				LaunchHelperPath: validHelper,
			},
			want: "socket name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newLaunchBrokerStartPlan(tt.cfg); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("newLaunchBrokerStartPlan() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLaunchBrokerStartCommandArgsAreDefensive(t *testing.T) {
	plan, err := newLaunchBrokerStartPlan(launchBrokerStartConfig{
		RuntimeDir:       t.TempDir(),
		ExpectedPeerUID:  501,
		HazmatPath:       "/usr/local/bin/hazmat",
		LaunchHelperPath: "/usr/local/libexec/hazmat-launch",
	})
	if err != nil {
		t.Fatalf("newLaunchBrokerStartPlan: %v", err)
	}

	args := plan.Command().Args()
	args[0] = "tampered"
	if got := plan.Command().Args()[0]; got != hostSudoPath {
		t.Fatalf("Args() returned aliased storage: first arg = %q", got)
	}
}

func TestStartLaunchBrokerSupervisorWithFakeStarter(t *testing.T) {
	starter := &fakeLaunchBrokerStarter{listen: true}
	supervisor, err := startLaunchBrokerSupervisor(context.Background(), launchBrokerSupervisorConfig{
		RuntimeDir:        newShortLaunchBrokerTempDir(t),
		ExpectedPeerUID:   501,
		HazmatPath:        "/usr/local/bin/hazmat",
		LaunchHelperPath:  "/usr/local/libexec/hazmat-launch",
		ReadyTimeout:      200 * time.Millisecond,
		ReadyPollInterval: time.Millisecond,
		ProcessStarter:    starter,
	})
	if err != nil {
		t.Fatalf("startLaunchBrokerSupervisor: %v", err)
	}

	if supervisor.SocketPath() == "" {
		t.Fatal("SocketPath() is empty")
	}
	if starter.plan.Command().Args()[8] != agententry.LaunchBrokerCommandName {
		t.Fatalf("broker command arg = %q, want %q", starter.plan.Command().Args()[8], agententry.LaunchBrokerCommandName)
	}
	if err := supervisor.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if err := supervisor.Close(); err != nil {
		t.Fatalf("second Close(): %v", err)
	}
	if starter.process.stopCount != 1 {
		t.Fatalf("Stop() called %d times, want 1", starter.process.stopCount)
	}
	if _, err := os.Stat(supervisor.SocketPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still exists after Close(): %v", err)
	}
}

func TestStartLaunchBrokerSupervisorStopsProcessWhenSocketNotReady(t *testing.T) {
	starter := &fakeLaunchBrokerStarter{}
	_, err := startLaunchBrokerSupervisor(context.Background(), launchBrokerSupervisorConfig{
		RuntimeDir:        newShortLaunchBrokerTempDir(t),
		ExpectedPeerUID:   501,
		HazmatPath:        "/usr/local/bin/hazmat",
		LaunchHelperPath:  "/usr/local/libexec/hazmat-launch",
		ReadyTimeout:      5 * time.Millisecond,
		ReadyPollInterval: time.Millisecond,
		ProcessStarter:    starter,
	})
	if err == nil || !strings.Contains(err.Error(), "wait for launch broker socket") {
		t.Fatalf("startLaunchBrokerSupervisor() error = %v, want readiness failure", err)
	}
	if starter.process == nil || starter.process.stopCount != 1 {
		t.Fatalf("Stop() count = %v, want 1", starter.process)
	}
}

type fakeLaunchBrokerStarter struct {
	listen  bool
	plan    launchBrokerStartPlan
	process *fakeLaunchBrokerProcess
}

func (s *fakeLaunchBrokerStarter) Start(_ context.Context, plan launchBrokerStartPlan) (launchBrokerProcess, error) {
	s.plan = plan
	process := &fakeLaunchBrokerProcess{}
	if s.listen {
		listener, err := net.Listen("unix", plan.socketPath)
		if err != nil {
			return nil, err
		}
		process.listener = listener
	}
	s.process = process
	return process, nil
}

type fakeLaunchBrokerProcess struct {
	listener  net.Listener
	stopCount int
}

func (p *fakeLaunchBrokerProcess) Stop() error {
	p.stopCount++
	if p.listener == nil {
		return nil
	}
	if err := p.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

func newShortLaunchBrokerTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hazmat-launch-broker-*")
	if err != nil {
		t.Fatalf("create short temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}
