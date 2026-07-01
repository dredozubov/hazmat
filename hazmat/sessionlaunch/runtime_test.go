package sessionlaunch

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestMergePreparedRuntimesDefensiveCopiesAndReverseCleanup(t *testing.T) {
	var cleanupOrder []string
	firstEnv := []string{"A=1"}
	first := NewPreparedRuntime(PreparedRuntimeInput{
		EnvPairs: firstEnv,
		TempDir:  "/tmp/first",
		Cleanup: func() {
			cleanupOrder = append(cleanupOrder, "first")
		},
	})
	second := NewPreparedRuntime(PreparedRuntimeInput{
		EnvPairs:            []string{"B=2"},
		TempDir:             "/tmp/second",
		LaunchHelperTempDir: "/tmp/helper",
		Cleanup: func() {
			cleanupOrder = append(cleanupOrder, "second")
		},
	})
	firstEnv[0] = "A=mutated"

	merged := MergePreparedRuntimes(first, second)
	if got := merged.EnvPairs(); !slices.Equal(got, []string{"A=1", "B=2"}) {
		t.Fatalf("EnvPairs = %v, want merged defensive copy", got)
	}
	env := merged.EnvPairs()
	env[0] = "A=mutated"
	if got := merged.EnvPairs(); !slices.Equal(got, []string{"A=1", "B=2"}) {
		t.Fatalf("EnvPairs shares mutable slice: %v", got)
	}
	if merged.TempDir() != "/tmp/second" {
		t.Fatalf("TempDir = %q, want last non-empty temp dir", merged.TempDir())
	}
	if merged.LaunchHelperTempDir() != "/tmp/helper" {
		t.Fatalf("LaunchHelperTempDir = %q, want helper dir", merged.LaunchHelperTempDir())
	}

	merged.Cleanup()
	merged.Cleanup()
	if !slices.Equal(cleanupOrder, []string{"second", "first"}) {
		t.Fatalf("cleanup order = %v, want reverse preparation order once", cleanupOrder)
	}
}

func TestPrepareRuntimeCleansPreparedStepsOnFailure(t *testing.T) {
	errStep := errors.New("prepare failed")
	var order []string

	_, err := PrepareRuntime(context.Background(), RuntimeRequest{
		Steps: []RuntimeStep{
			{
				Name: "first",
				Prepare: func(context.Context) (PreparedRuntime, error) {
					order = append(order, "prepare:first")
					return NewPreparedRuntime(PreparedRuntimeInput{
						Cleanup: func() { order = append(order, "cleanup:first") },
					}), nil
				},
			},
			{
				Name: "second",
				Prepare: func(context.Context) (PreparedRuntime, error) {
					order = append(order, "prepare:second")
					return NewPreparedRuntime(PreparedRuntimeInput{
						Cleanup: func() { order = append(order, "cleanup:second") },
					}), nil
				},
			},
			{
				Name: "third",
				Prepare: func(context.Context) (PreparedRuntime, error) {
					order = append(order, "prepare:third")
					return PreparedRuntime{}, errStep
				},
			},
		},
	})
	if !errors.Is(err, errStep) {
		t.Fatalf("PrepareRuntime error = %v, want %v", err, errStep)
	}
	want := []string{"prepare:first", "prepare:second", "prepare:third", "cleanup:second", "cleanup:first"}
	if !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestPrepareRuntimeHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var prepared bool
	_, err := PrepareRuntime(ctx, RuntimeRequest{
		Steps: []RuntimeStep{{
			Name: "first",
			Prepare: func(context.Context) (PreparedRuntime, error) {
				prepared = true
				return NewPreparedRuntime(PreparedRuntimeInput{}), nil
			},
		}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PrepareRuntime error = %v, want context canceled", err)
	}
	if prepared {
		t.Fatal("PrepareRuntime ran a step after context cancellation")
	}
}
