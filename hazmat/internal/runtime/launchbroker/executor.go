package launchbroker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type HelperRunner interface {
	Run(context.Context, HelperCommand) (HelperRunResult, error)
}

type HelperRunResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

type HelperExecutorConfig struct {
	LaunchHelperPath string
	Profile          bool
	Runner           HelperRunner
}

type HelperExecutor struct {
	cfg    HelperCommandConfig
	runner HelperRunner
}

func NewHelperExecutor(cfg HelperExecutorConfig) (HelperExecutor, error) {
	runner := cfg.Runner
	if runner == nil {
		runner = BufferedHelperRunner{}
	}
	executor := HelperExecutor{
		cfg: HelperCommandConfig{
			LaunchHelperPath: cfg.LaunchHelperPath,
			Profile:          cfg.Profile,
		},
		runner: runner,
	}
	if executor.cfg.LaunchHelperPath == "" {
		return HelperExecutor{}, errors.New("launch helper path is required")
	}
	if _, err := cleanAbsolutePath("launch helper path", executor.cfg.LaunchHelperPath); err != nil {
		return HelperExecutor{}, err
	}
	return executor, nil
}

func NewHelperLaunchHandler(cfg HelperExecutorConfig) (LaunchHandler, error) {
	executor, err := NewHelperExecutor(cfg)
	if err != nil {
		return nil, err
	}
	return executor.HandleLaunch, nil
}

func (e HelperExecutor) HandleLaunch(ctx context.Context, plan ChildPlan) (LaunchResponse, error) {
	if e.runner == nil {
		return LaunchResponse{}, errors.New("helper runner is required")
	}
	command, err := NewHelperCommand(plan, e.cfg)
	if err != nil {
		return LaunchResponse{}, err
	}

	result, err := e.runner.Run(ctx, command)
	if err != nil {
		return LaunchResponse{}, err
	}

	req := plan.Request.Request()
	metadata, stderrWithoutMetadata := splitConfirmedMetadata(result.Stderr, req.MetadataJSON)
	resp := LaunchResponse{
		OK:           true,
		ExitCode:     result.ExitCode,
		MetadataJSON: metadata,
		Stdout:       string(result.Stdout),
		Stderr:       string(stderrWithoutMetadata),
	}
	if req.MetadataJSON != "" && metadata == "" {
		resp.OK = false
		resp.Error = "launch helper did not confirm containment metadata"
	}
	return resp, nil
}

type BufferedHelperRunner struct{}

func (BufferedHelperRunner) Run(ctx context.Context, command HelperCommand) (HelperRunResult, error) {
	cmd, err := command.CommandContext(ctx)
	if err != nil {
		return HelperRunResult{}, err
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	result := HelperRunResult{
		ExitCode: 0,
		Stdout:   append([]byte(nil), stdout.Bytes()...),
		Stderr:   append([]byte(nil), stderr.Bytes()...),
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if ctx != nil && ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, fmt.Errorf("run launch helper: %w", err)
}

func splitConfirmedMetadata(stderr []byte, expected string) (string, []byte) {
	if expected == "" || len(stderr) == 0 {
		return "", append([]byte(nil), stderr...)
	}

	lines := bytes.SplitAfter(stderr, []byte("\n"))
	for i, line := range lines {
		if strings.TrimRight(string(line), "\r\n") != expected {
			continue
		}
		var rest []byte
		for j, candidate := range lines {
			if j == i {
				continue
			}
			rest = append(rest, candidate...)
		}
		return expected, rest
	}
	return "", append([]byte(nil), stderr...)
}
