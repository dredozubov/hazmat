package hazmat

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSessionPreparationProgressRendersStepsAndDone(t *testing.T) {
	var buf bytes.Buffer
	times := []time.Time{
		time.Unix(100, 0),
		time.Unix(101, 0),
		time.Unix(102, 0),
	}
	progress := &sessionPreparationProgress{
		w:     &buf,
		start: time.Unix(100, 0),
		now: func() time.Time {
			if len(times) == 0 {
				t.Fatal("unexpected time request")
			}
			got := times[0]
			times = times[1:]
			return got
		},
	}

	progress.Step("resolving launch context")
	progress.Step("checking Docker routing")
	progress.Done()

	got := buf.String()
	for _, want := range []string{
		"hazmat: preparing session startup",
		"  resolving launch context...",
		"  checking Docker routing...",
		"hazmat: session startup preparation complete (2.0s)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("progress output missing %q in:\n%s", want, got)
		}
	}
}

func TestSessionPreparationProgressProfileRendersPhaseDurations(t *testing.T) {
	var buf bytes.Buffer
	times := []time.Time{
		time.Unix(100, 0),
		time.Unix(100, int64(400*time.Millisecond)),
		time.Unix(102, 0),
	}
	nextTime := func() time.Time {
		if len(times) == 0 {
			t.Fatal("unexpected time request")
		}
		got := times[0]
		times = times[1:]
		return got
	}
	progress := &sessionPreparationProgress{
		w:       &buf,
		start:   time.Unix(100, 0),
		now:     nextTime,
		profile: true,
	}

	progress.Step("resolving launch context")
	progress.Step("checking Docker routing")
	progress.Done()

	got := buf.String()
	for _, want := range []string{
		"hazmat: session startup preparation complete (2.0s)",
		"hazmat: session startup preparation profile:",
		"  resolving launch context: 0.4s",
		"  checking Docker routing: 1.6s",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("profile output missing %q in:\n%s", want, got)
		}
	}
}

func TestSessionPreparationProgressProfileEnvGate(t *testing.T) {
	t.Setenv("HAZMAT_SESSION_PREP_PROFILE", "yes")
	progress := newSessionPreparationProgress(&bytes.Buffer{})
	if !progress.profile {
		t.Fatal("expected HAZMAT_SESSION_PREP_PROFILE=yes to enable profiling")
	}
}

func TestExecuteSessionMutationPlanToWriterLogsLifecycle(t *testing.T) {
	var buf bytes.Buffer
	err := executeSessionMutationPlanToWriter(&buf, sessionMutationPlan{
		Mutations: []plannedSessionMutation{
			{
				Metadata: sessionMutation{Summary: "project ACL repair"},
				Apply: func() (sessionMutationExecution, error) {
					return sessionMutationExecution{}, nil
				},
			},
			{
				Metadata: sessionMutation{Summary: "git safe.directory trust"},
				Apply: func() (sessionMutationExecution, error) {
					return sessionMutationExecution{
						AppliedMessage: "  Trusted project repo for agent-side Git metadata access",
					}, nil
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSessionMutationPlanToWriter: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		"  Running project ACL repair...",
		"  Finished project ACL repair (",
		"  Running git safe.directory trust...",
		"  Trusted project repo for agent-side Git metadata access (",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("mutation output missing %q in:\n%s", want, got)
		}
	}
}

func TestExecuteSessionMutationPlanToWriterLogsFailure(t *testing.T) {
	var buf bytes.Buffer
	wantErr := errors.New("boom")

	err := executeSessionMutationPlanToWriter(&buf, sessionMutationPlan{
		Mutations: []plannedSessionMutation{
			{
				Metadata: sessionMutation{Summary: "harness asset sync"},
				Apply: func() (sessionMutationExecution, error) {
					return sessionMutationExecution{}, wantErr
				},
			},
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("executeSessionMutationPlanToWriter error = %v, want %v", err, wantErr)
	}

	got := buf.String()
	for _, want := range []string{
		"  Running harness asset sync...",
		"  Failed harness asset sync after ",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("failure output missing %q in:\n%s", want, got)
		}
	}
}
