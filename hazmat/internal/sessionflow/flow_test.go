package sessionflow

import (
	"reflect"
	"strings"
	"testing"

	"hazmat/sessionmeta"
)

func TestNewOrdersStartupPhases(t *testing.T) {
	tests := []struct {
		name                   string
		input                  Input
		wantKinds              []PhaseKind
		wantSnapshotSkip       bool
		wantHostMutationBefore bool
		wantHostMutationAfter  bool
	}{
		{
			name: "native mutates after snapshot by default",
			input: Input{
				Mode: sessionmeta.ModeNative,
			},
			wantKinds: []PhaseKind{
				PhaseRenderContract,
				PhaseSnapshot,
				PhaseHostMutations,
				PhaseRuntimeLaunch,
			},
			wantHostMutationAfter: true,
		},
		{
			name: "native preflight mutates before snapshot",
			input: Input{
				Mode:                    sessionmeta.ModeNative,
				PreflightBeforeSnapshot: true,
			},
			wantKinds: []PhaseKind{
				PhaseRenderContract,
				PhaseHostMutations,
				PhaseSnapshot,
				PhaseRuntimeLaunch,
			},
			wantHostMutationBefore: true,
		},
		{
			name: "docker mutates before snapshot",
			input: Input{
				Mode: sessionmeta.ModeDockerSandbox,
			},
			wantKinds: []PhaseKind{
				PhaseRenderContract,
				PhaseHostMutations,
				PhaseSnapshot,
				PhaseRuntimeLaunch,
			},
			wantHostMutationBefore: true,
		},
		{
			name: "skip snapshot still records snapshot phase",
			input: Input{
				Mode:         sessionmeta.ModeNative,
				SkipSnapshot: true,
			},
			wantKinds: []PhaseKind{
				PhaseRenderContract,
				PhaseSnapshot,
				PhaseHostMutations,
				PhaseRuntimeLaunch,
			},
			wantSnapshotSkip:      true,
			wantHostMutationAfter: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := New(tt.input)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			phases := plan.Phases()
			if got := phaseKinds(phases); !reflect.DeepEqual(got, tt.wantKinds) {
				t.Fatalf("phase kinds = %v, want %v", got, tt.wantKinds)
			}
			snapshot, ok := findPhase(phases, PhaseSnapshot)
			if !ok {
				t.Fatal("snapshot phase missing")
			}
			if snapshot.SnapshotSkipped() != tt.wantSnapshotSkip {
				t.Fatalf("SnapshotSkipped = %v, want %v", snapshot.SnapshotSkipped(), tt.wantSnapshotSkip)
			}
			if got := phaseBefore(phases, PhaseHostMutations, PhaseSnapshot); got != tt.wantHostMutationBefore {
				t.Fatalf("host mutation before snapshot = %v, want %v", got, tt.wantHostMutationBefore)
			}
			if got := phaseBefore(phases, PhaseSnapshot, PhaseHostMutations); got != tt.wantHostMutationAfter {
				t.Fatalf("snapshot before host mutation = %v, want %v", got, tt.wantHostMutationAfter)
			}

			phases[0] = Phase{kind: PhaseRuntimeLaunch}
			if got := phaseKinds(plan.Phases()); !reflect.DeepEqual(got, tt.wantKinds) {
				t.Fatalf("Phases returned storage alias: %v", got)
			}
		})
	}
}

func TestNewRejectsUnsupportedMode(t *testing.T) {
	_, err := New(Input{Mode: sessionmeta.ModeAppleContainer})
	if err == nil || !strings.Contains(err.Error(), "unsupported session startup mode") {
		t.Fatalf("New error = %v, want unsupported mode", err)
	}
}

func phaseKinds(phases []Phase) []PhaseKind {
	out := make([]PhaseKind, len(phases))
	for i, phase := range phases {
		out[i] = phase.Kind()
	}
	return out
}

func findPhase(phases []Phase, kind PhaseKind) (Phase, bool) {
	for _, phase := range phases {
		if phase.Kind() == kind {
			return phase, true
		}
	}
	return Phase{}, false
}

func phaseBefore(phases []Phase, first, second PhaseKind) bool {
	firstIndex := -1
	secondIndex := -1
	for i, phase := range phases {
		switch phase.Kind() {
		case first:
			firstIndex = i
		case second:
			secondIndex = i
		}
	}
	return firstIndex >= 0 && secondIndex >= 0 && firstIndex < secondIndex
}
