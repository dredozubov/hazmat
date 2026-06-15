package hazmat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hazmat/internal/agententry"
)

const (
	defaultLaunchBrokerReadyTimeout = 5 * time.Second
	defaultLaunchBrokerPollInterval = 10 * time.Millisecond
)

type launchBrokerStartConfig struct {
	RuntimeDir       string
	SocketName       string
	ExpectedPeerUID  int
	HazmatPath       string
	LaunchHelperPath string
}

type launchBrokerStartPlan struct {
	socketPath string
	command    launchBrokerStartCommand
}

func (p launchBrokerStartPlan) SocketPath() string {
	return p.socketPath
}

func (p launchBrokerStartPlan) Command() launchBrokerStartCommand {
	return p.command
}

type launchBrokerStartCommand struct {
	path string
	args []string
	dir  string
}

func (c launchBrokerStartCommand) Path() string {
	return c.path
}

func (c launchBrokerStartCommand) Args() []string {
	return append([]string(nil), c.args...)
}

func (c launchBrokerStartCommand) Dir() string {
	return c.dir
}

func (c launchBrokerStartCommand) CommandContext(ctx context.Context) (*exec.Cmd, error) {
	if c.path == "" || len(c.args) == 0 {
		return nil, errors.New("launch broker start command is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, c.path, c.args[1:]...)
	cmd.Dir = c.dir
	return cmd, nil
}

func newLaunchBrokerStartPlan(cfg launchBrokerStartConfig) (launchBrokerStartPlan, error) {
	runtimeDir, err := cleanLaunchBrokerAbsolutePath("runtime dir", cfg.RuntimeDir)
	if err != nil {
		return launchBrokerStartPlan{}, err
	}
	hazmatPath, err := cleanLaunchBrokerAbsolutePath("hazmat path", cfg.HazmatPath)
	if err != nil {
		return launchBrokerStartPlan{}, err
	}
	helperPath, err := cleanLaunchBrokerAbsolutePath("launch helper path", cfg.LaunchHelperPath)
	if err != nil {
		return launchBrokerStartPlan{}, err
	}
	if cfg.ExpectedPeerUID <= 0 {
		return launchBrokerStartPlan{}, fmt.Errorf("expected peer uid must be positive, got %d", cfg.ExpectedPeerUID)
	}

	socketName := cfg.SocketName
	if socketName == "" {
		socketName = fmt.Sprintf("launch-broker-%d.sock", cfg.ExpectedPeerUID)
	}
	if err := validateLaunchBrokerSocketName(socketName); err != nil {
		return launchBrokerStartPlan{}, err
	}
	socketPath := filepath.Join(runtimeDir, socketName)

	brokerArgs := []string{
		"-n",
		"-u", agentUser,
		"-H",
		helperPath,
		"exec",
		hazmatPath,
		agententry.LaunchBrokerCommandName,
		socketPath,
		strconv.Itoa(cfg.ExpectedPeerUID),
		helperPath,
	}
	args := append([]string{hostSudoPath}, brokerArgs...)
	return launchBrokerStartPlan{
		socketPath: socketPath,
		command: launchBrokerStartCommand{
			path: hostSudoPath,
			args: args,
			dir:  "/",
		},
	}, nil
}

func cleanLaunchBrokerAbsolutePath(label, path string) (string, error) {
	clean := filepath.Clean(path)
	if path == "" || !filepath.IsAbs(clean) || clean != path {
		return "", fmt.Errorf("%s %q must be absolute and clean", label, path)
	}
	return clean, nil
}

func validateLaunchBrokerSocketName(name string) error {
	if strings.TrimSpace(name) == "" || strings.ContainsRune(name, os.PathSeparator) || name != filepath.Base(name) {
		return fmt.Errorf("launch broker socket name %q is invalid", name)
	}
	return nil
}

type launchBrokerSupervisorConfig struct {
	RuntimeDir        string
	SocketName        string
	ExpectedPeerUID   int
	HazmatPath        string
	LaunchHelperPath  string
	ReadyTimeout      time.Duration
	ReadyPollInterval time.Duration
	ProcessStarter    launchBrokerProcessStarter
}

type launchBrokerProcessStarter interface {
	Start(context.Context, launchBrokerStartPlan) (launchBrokerProcess, error)
}

type launchBrokerProcess interface {
	Stop() error
}

type launchBrokerSupervisor struct {
	socketPath string
	process    launchBrokerProcess

	closeOnce sync.Once
	closeErr  error
}

func startLaunchBrokerSupervisor(ctx context.Context, cfg launchBrokerSupervisorConfig) (*launchBrokerSupervisor, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	starter := cfg.ProcessStarter
	if starter == nil {
		starter = execLaunchBrokerProcessStarter{}
	}

	plan, err := newLaunchBrokerStartPlan(launchBrokerStartConfig{
		RuntimeDir:       cfg.RuntimeDir,
		SocketName:       cfg.SocketName,
		ExpectedPeerUID:  cfg.ExpectedPeerUID,
		HazmatPath:       cfg.HazmatPath,
		LaunchHelperPath: cfg.LaunchHelperPath,
	})
	if err != nil {
		return nil, err
	}

	process, err := starter.Start(ctx, plan)
	if err != nil {
		return nil, err
	}
	if err := waitForLaunchBrokerSocket(ctx, plan.socketPath, cfg.ReadyTimeout, cfg.ReadyPollInterval); err != nil {
		return nil, errors.Join(err, process.Stop())
	}
	return &launchBrokerSupervisor{
		socketPath: plan.socketPath,
		process:    process,
	}, nil
}

func (s *launchBrokerSupervisor) SocketPath() string {
	if s == nil {
		return ""
	}
	return s.socketPath
}

func (s *launchBrokerSupervisor) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		var removeErr error
		if s.socketPath != "" {
			removeErr = os.Remove(s.socketPath)
			if errors.Is(removeErr, os.ErrNotExist) {
				removeErr = nil
			}
		}
		var stopErr error
		if s.process != nil {
			stopErr = s.process.Stop()
		}
		s.closeErr = errors.Join(stopErr, removeErr)
	})
	return s.closeErr
}

type execLaunchBrokerProcessStarter struct{}

func (execLaunchBrokerProcessStarter) Start(ctx context.Context, plan launchBrokerStartPlan) (launchBrokerProcess, error) {
	cmd, err := plan.command.CommandContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	process := &execLaunchBrokerProcess{
		cmd:  cmd,
		done: make(chan error, 1),
	}
	go func() {
		process.done <- cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

type execLaunchBrokerProcess struct {
	cmd  *exec.Cmd
	done chan error

	stopOnce sync.Once
	stopErr  error
}

func (p *execLaunchBrokerProcess) Stop() error {
	if p == nil {
		return nil
	}
	p.stopOnce.Do(func() {
		if p.cmd != nil && p.cmd.Process != nil {
			p.stopErr = p.cmd.Process.Kill()
		}
		if p.done != nil {
			<-p.done
		}
	})
	return p.stopErr
}

func waitForLaunchBrokerSocket(ctx context.Context, socketPath string, timeout, pollInterval time.Duration) error {
	if timeout <= 0 {
		timeout = defaultLaunchBrokerReadyTimeout
	}
	if pollInterval <= 0 {
		pollInterval = defaultLaunchBrokerPollInterval
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		ready, err := launchBrokerSocketReady(socketPath)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for launch broker socket %s: %w", socketPath, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func launchBrokerSocketReady(socketPath string) (bool, error) {
	info, err := os.Stat(socketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return false, fmt.Errorf("launch broker socket path %s exists but is not a socket", socketPath)
	}
	return true, nil
}
