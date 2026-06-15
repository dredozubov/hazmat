package launchbroker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	darwinruntime "hazmat/internal/runtime/darwin"
)

type HelperCommandConfig struct {
	LaunchHelperPath string
	Profile          bool
}

type HelperCommand struct {
	path string
	args []string
	env  []string
}

func NewHelperCommand(plan ChildPlan, cfg HelperCommandConfig) (HelperCommand, error) {
	if !plan.RequiresInheritedFDCleanup() {
		return HelperCommand{}, errors.New("child plan must require inherited fd cleanup")
	}
	req := plan.Request.Request()
	if req.PolicyPath == "" {
		return HelperCommand{}, errors.New("verified launch request is required")
	}
	helperPath := filepath.Clean(cfg.LaunchHelperPath)
	if cfg.LaunchHelperPath == "" || !filepath.IsAbs(helperPath) || helperPath != cfg.LaunchHelperPath {
		return HelperCommand{}, fmt.Errorf("launch helper path %q must be absolute and clean", cfg.LaunchHelperPath)
	}

	args := darwinruntime.CommandLaunchHelperArgs(darwinruntime.CommandRequest{
		LaunchHelperPath: helperPath,
		PolicyPath:       req.PolicyPath,
		MetadataJSON:     req.MetadataJSON,
		Profile:          cfg.Profile,
		DirectExec:       req.DirectExec,
		WorkingDir:       req.WorkingDir,
		SessionTempDir:   req.SessionTempDir,
		EnvPairs:         req.EnvPairs,
		RuntimeEnvPairs:  req.RuntimeEnvPairs,
		Script:           req.Script,
		Args:             req.Args,
	})
	return HelperCommand{
		path: helperPath,
		args: args,
		env:  []string{fmt.Sprintf("SUDO_UID=%d", plan.Request.PeerUID())},
	}, nil
}

func (c HelperCommand) Path() string {
	return c.path
}

func (c HelperCommand) Args() []string {
	return append([]string(nil), c.args...)
}

func (c HelperCommand) Env() []string {
	return append([]string(nil), c.env...)
}

func (c HelperCommand) CommandContext(ctx context.Context) (*exec.Cmd, error) {
	if c.path == "" || len(c.args) == 0 {
		return nil, errors.New("helper command is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, c.path, c.args[1:]...)
	cmd.Env = c.Env()
	return cmd, nil
}
