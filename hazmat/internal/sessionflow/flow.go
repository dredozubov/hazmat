// Package sessionflow describes the side-effect-free order of session startup
// phases. It does not render output, mutate the host, take snapshots, or launch
// processes.
package sessionflow

import (
	"fmt"

	"hazmat/sessionmeta"
)

// PhaseKind is the closed set of pre-launch session phases.
type PhaseKind string

const (
	PhaseRenderContract PhaseKind = "render-contract"
	PhaseHostMutations  PhaseKind = "host-mutations"
	PhaseSnapshot       PhaseKind = "snapshot"
	PhaseRuntimeLaunch  PhaseKind = "runtime-launch"
)

// Input contains the already-resolved session-start decisions.
type Input struct {
	Mode                    sessionmeta.Mode
	SkipSnapshot            bool
	PreflightBeforeSnapshot bool
}

// Phase is one side-effect-free startup phase.
type Phase struct {
	kind         PhaseKind
	snapshotSkip bool
}

// Kind returns this phase's kind.
func (p Phase) Kind() PhaseKind {
	return p.kind
}

// SnapshotSkipped reports whether a snapshot phase should call the runtime in
// skip mode. It is meaningful only for PhaseSnapshot.
func (p Phase) SnapshotSkipped() bool {
	return p.snapshotSkip
}

// Plan is an immutable startup order.
type Plan struct {
	phases []Phase
}

// New constructs the startup phase order for a prepared session.
func New(input Input) (Plan, error) {
	switch input.Mode {
	case sessionmeta.ModeNative:
		return Plan{phases: nativePhases(input)}, nil
	case sessionmeta.ModeDockerSandbox:
		return Plan{phases: []Phase{
			{kind: PhaseRenderContract},
			{kind: PhaseHostMutations},
			{kind: PhaseSnapshot, snapshotSkip: input.SkipSnapshot},
			{kind: PhaseRuntimeLaunch},
		}}, nil
	case sessionmeta.ModeAppleContainer:
		return Plan{}, fmt.Errorf("unsupported session startup mode %q", input.Mode)
	default:
		return Plan{}, fmt.Errorf("unsupported session startup mode %q", input.Mode)
	}
}

func nativePhases(input Input) []Phase {
	if input.PreflightBeforeSnapshot {
		return []Phase{
			{kind: PhaseRenderContract},
			{kind: PhaseHostMutations},
			{kind: PhaseSnapshot, snapshotSkip: input.SkipSnapshot},
			{kind: PhaseRuntimeLaunch},
		}
	}
	return []Phase{
		{kind: PhaseRenderContract},
		{kind: PhaseSnapshot, snapshotSkip: input.SkipSnapshot},
		{kind: PhaseHostMutations},
		{kind: PhaseRuntimeLaunch},
	}
}

// Phases returns a defensive copy of the startup phases.
func (p Plan) Phases() []Phase {
	if len(p.phases) == 0 {
		return nil
	}
	return append([]Phase(nil), p.phases...)
}
