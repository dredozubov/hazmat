package sessionlaunch

import (
	"context"
	"fmt"
	"sync"
)

// RuntimeStep prepares one runtime component such as temp directories,
// harness auth, or a broker-backed credential service.
type RuntimeStep struct {
	Name    string
	Prepare func(context.Context) (PreparedRuntime, error)
}

// RuntimeRequest describes the ordered runtime preparation steps for a session.
type RuntimeRequest struct {
	Steps []RuntimeStep
}

// PreparedRuntimeInput constructs a prepared runtime without exposing mutable
// backing slices to callers.
type PreparedRuntimeInput struct {
	EnvPairs            []string
	TempDir             string
	LaunchHelperTempDir string
	Cleanup             func()
}

// PreparedRuntime is the protocol-neutral runtime preparation result.
type PreparedRuntime struct {
	envPairs            []string
	tempDir             string
	launchHelperTempDir string
	cleanup             *runtimeCleanup
}

type runtimeCleanup struct {
	once    sync.Once
	cleanup func()
}

// NewPreparedRuntime builds a defensive-copy prepared runtime value.
func NewPreparedRuntime(input PreparedRuntimeInput) PreparedRuntime {
	cleanup := input.Cleanup
	if cleanup == nil {
		cleanup = func() {}
	}
	return PreparedRuntime{
		envPairs:            copyStrings(input.EnvPairs),
		tempDir:             input.TempDir,
		launchHelperTempDir: input.LaunchHelperTempDir,
		cleanup:             &runtimeCleanup{cleanup: cleanup},
	}
}

// EnvPairs returns launch environment pairs prepared for the session runtime.
func (r PreparedRuntime) EnvPairs() []string {
	return copyStrings(r.envPairs)
}

// TempDir returns the agent-owned session temp dir, when prepared.
func (r PreparedRuntime) TempDir() string {
	return r.tempDir
}

// LaunchHelperTempDir returns the temp dir that a capable launch helper should
// create inside the final contained launch.
func (r PreparedRuntime) LaunchHelperTempDir() string {
	return r.launchHelperTempDir
}

// Cleanup releases runtime artifacts. It is idempotent.
func (r PreparedRuntime) Cleanup() {
	if r.cleanup == nil {
		return
	}
	r.cleanup.once.Do(r.cleanup.cleanup)
}

// PrepareRuntime runs runtime steps in order and cleans already-prepared steps
// in reverse order if a later step fails.
func PrepareRuntime(ctx context.Context, request RuntimeRequest) (PreparedRuntime, error) {
	prepared, err := PrepareRuntimeSteps(ctx, request.Steps)
	if err != nil {
		return PreparedRuntime{}, err
	}
	return MergePreparedRuntimes(prepared...), nil
}

// PrepareRuntimeSteps runs runtime steps in order and returns each prepared
// component. Already-prepared steps are cleaned in reverse order if a later step
// fails.
func PrepareRuntimeSteps(ctx context.Context, steps []RuntimeStep) ([]PreparedRuntime, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	prepared := make([]PreparedRuntime, 0, len(steps))
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			cleanupPreparedRuntimes(prepared)
			return nil, err
		}
		if step.Prepare == nil {
			cleanupPreparedRuntimes(prepared)
			if step.Name == "" {
				return nil, fmt.Errorf("runtime step prepare function is required")
			}
			return nil, fmt.Errorf("runtime step %q prepare function is required", step.Name)
		}
		runtime, err := step.Prepare(ctx)
		if err != nil {
			cleanupPreparedRuntimes(prepared)
			return nil, err
		}
		prepared = append(prepared, runtime)
	}
	return prepared, nil
}

// MergePreparedRuntimes composes runtime env, temp dirs, and cleanup handles.
// Cleanup runs in reverse preparation order.
func MergePreparedRuntimes(runtimes ...PreparedRuntime) PreparedRuntime {
	if len(runtimes) == 0 {
		return NewPreparedRuntime(PreparedRuntimeInput{})
	}

	var merged PreparedRuntimeInput
	cleanups := make([]PreparedRuntime, 0, len(runtimes))
	for _, runtime := range runtimes {
		merged.EnvPairs = append(merged.EnvPairs, runtime.EnvPairs()...)
		if runtime.TempDir() != "" {
			merged.TempDir = runtime.TempDir()
		}
		if runtime.LaunchHelperTempDir() != "" {
			merged.LaunchHelperTempDir = runtime.LaunchHelperTempDir()
		}
		cleanups = append(cleanups, runtime)
	}
	merged.Cleanup = func() {
		cleanupPreparedRuntimes(cleanups)
	}
	return NewPreparedRuntime(merged)
}

func cleanupPreparedRuntimes(runtimes []PreparedRuntime) {
	for i := len(runtimes) - 1; i >= 0; i-- {
		runtimes[i].Cleanup()
	}
}
