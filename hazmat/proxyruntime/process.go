package proxyruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

type ProcessSpec struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
}

type ProcessRequest struct {
	Spec    ProcessSpec
	Event   EventInput
	Cleanup func()
}

type ProcessStreams struct {
	Stdin  io.WriteCloser
	Stdout io.Reader
	Stderr io.Reader
}

type ProcessLoop func(context.Context, ProcessStreams) error

type ProcessResult struct {
	Started bool
	Events  []Event
}

type ProcessStarter interface {
	Start(context.Context, ProcessSpec) (ProcessHandle, error)
}

type ProcessHandle interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Wait() error
	Kill() error
}

type EventSink func(Event)

type ProcessRunner struct {
	Starter ProcessStarter
	Events  EventSink
}

func (r ProcessRunner) Run(ctx context.Context, request ProcessRequest, loop ProcessLoop) (ProcessResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if loop == nil {
		return ProcessResult{}, fmt.Errorf("proxyruntime: process loop is required")
	}
	starter := r.Starter
	if starter == nil {
		starter = OSProcessStarter{}
	}
	cleanup := request.Cleanup
	if cleanup == nil {
		cleanup = func() {}
	}

	var result ProcessResult
	emit := func(operation string, decision Decision, reason string) {
		event := NewEvent(processEventInput(request.Event, operation, decision, reason))
		result.Events = append(result.Events, event)
		if r.Events != nil {
			r.Events(event)
		}
	}

	emit("process:start", DecisionObserve, "")
	handle, err := starter.Start(ctx, request.Spec)
	if err != nil {
		cleanup()
		emit("process:start", DecisionError, err.Error())
		return result, err
	}
	result.Started = true

	loopErr := loop(ctx, ProcessStreams{
		Stdin:  handle.Stdin(),
		Stdout: handle.Stdout(),
		Stderr: handle.Stderr(),
	})
	if loopErr != nil {
		killErr := handle.Kill()
		waitErr := handle.Wait()
		cleanup()
		emit("process:loop", DecisionError, loopErr.Error())
		return result, errors.Join(loopErr, killErr, waitErr)
	}

	waitErr := handle.Wait()
	cleanup()
	if waitErr != nil {
		emit("process:exit", DecisionError, waitErr.Error())
		return result, waitErr
	}
	emit("process:exit", DecisionObserve, "")
	return result, nil
}

func processEventInput(base EventInput, operation string, decision Decision, reason string) EventInput {
	base.Direction = DirectionLifecycle
	base.Operation = operation
	base.Decision = decision
	base.Reason = reason
	return base
}

type OSProcessStarter struct{}

func (OSProcessStarter) Start(ctx context.Context, spec ProcessSpec) (ProcessHandle, error) {
	if spec.Command == "" {
		return nil, fmt.Errorf("proxyruntime: process command is required")
	}
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append([]string(nil), spec.Env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return osProcessHandle{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

type osProcessHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (h osProcessHandle) Stdin() io.WriteCloser {
	return h.stdin
}

func (h osProcessHandle) Stdout() io.ReadCloser {
	return h.stdout
}

func (h osProcessHandle) Stderr() io.ReadCloser {
	return h.stderr
}

func (h osProcessHandle) Wait() error {
	return h.cmd.Wait()
}

func (h osProcessHandle) Kill() error {
	if h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Kill()
}
