package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSessionPreparationProgressRendersStepsAndDone(t *testing.T) {
	var buf bytes.Buffer
	progress := &sessionPreparationProgress{
		w:     &buf,
		start: time.Unix(100, 0),
		now:   func() time.Time { return time.Unix(102, 0) },
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
