package linux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	linuxspec "hazmat/containment/linux"
	platformlinux "hazmat/platform/linux"
)

const EnvExperimentalCurrentUser = "HAZMAT_EXPERIMENTAL_LINUX_CURRENT_USER"

type RunPhase string

const (
	PhasePlanned   RunPhase = "planned"
	PhaseLaunched  RunPhase = "launched"
	PhaseContained RunPhase = "contained"
	PhaseExited    RunPhase = "exited"
	PhaseFailed    RunPhase = "failed"
	PhaseCancelled RunPhase = "cancelled"
)

type MetadataEvent struct {
	Phase               RunPhase `json:"phase"`
	EnforcementComplete bool     `json:"enforcement_complete,omitempty"`
}

type RunRecord struct {
	Phase    RunPhase `json:"phase"`
	ExitCode int      `json:"exit_code,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type SidecarStore struct {
	Dir string
}

func (s SidecarStore) MetadataPath() string {
	return filepath.Join(s.Dir, "metadata.json")
}

func (s SidecarStore) ResultPath() string {
	return filepath.Join(s.Dir, "result.json")
}

type RunOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Sidecar  SidecarStore
	Enforcer CurrentUserEnforcer
}

type ExecResult struct {
	ExitCode int
}

type CurrentUserEnforcer interface {
	CloseInheritedFDs(context.Context, FDClosurePlan) error
	CreateNamespaces(context.Context, NamespacePlan) error
	ApplyMounts(context.Context, linuxspec.LaunchSpec) error
	ConfigureNetwork(context.Context, NetworkAdmission) error
	DropPrivileges(context.Context, linuxspec.ProcessSpec) error
	ApplyLandlock(context.Context, PolicyPlan) error
	ApplySeccomp(context.Context, PolicyPlan) error
	Exec(context.Context, linuxspec.LaunchSpec, RunOptions) (ExecResult, error)
}

type RunResult struct {
	Record RunRecord
}

func GateEnabled() bool {
	return os.Getenv(EnvExperimentalCurrentUser) == "1"
}

func GateError() error {
	return fmt.Errorf("the Linux current-user backend is experimental and disabled by default; set %s=1 to enable this invocation", EnvExperimentalCurrentUser)
}

func RunCurrentUser(ctx context.Context, spec linuxspec.LaunchSpec, report platformlinux.Report, opts RunOptions) (RunResult, error) {
	if !GateEnabled() {
		return RunResult{}, GateError()
	}
	if spec.Phase != linuxspec.PhaseExperimental {
		return RunResult{}, fmt.Errorf("linux current-user runner refuses a %q spec; compile with the executable runtime option", spec.Phase)
	}
	if len(spec.CapabilityGaps) > 0 {
		return RunResult{}, GapError{Gaps: spec.CapabilityGaps}
	}
	if len(spec.Command) == 0 || strings.TrimSpace(spec.Command[0]) == "" {
		return RunResult{}, fmt.Errorf("linux current-user runner requires a command argv")
	}
	plan, err := AdmitCurrentUser(spec, report)
	if err != nil {
		return RunResult{}, err
	}
	enforcer := opts.Enforcer
	if enforcer == nil {
		enforcer = HostCurrentUserEnforcer()
	}
	if err := validateRunOptions(opts); err != nil {
		return RunResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return writeTerminalResult(opts.Sidecar, RunRecord{Phase: PhaseCancelled, Error: err.Error()}, true)
	}
	if err := os.MkdirAll(opts.Sidecar.Dir, 0o700); err != nil {
		return RunResult{}, err
	}

	events := []MetadataEvent{{Phase: PhasePlanned}}
	if err := writeMetadataSidecar(opts.Sidecar.MetadataPath(), events); err != nil {
		return RunResult{}, err
	}
	events = append(events, MetadataEvent{Phase: PhaseLaunched})
	if err := writeMetadataSidecar(opts.Sidecar.MetadataPath(), events); err != nil {
		return RunResult{}, err
	}

	if err := enforceCurrentUser(ctx, enforcer, spec, plan); err != nil {
		record := RunRecord{Phase: PhaseFailed, Error: err.Error()}
		result, writeErr := writeTerminalResult(opts.Sidecar, record, true)
		if writeErr != nil {
			return result, writeErr
		}
		return result, err
	}

	events = append(events, MetadataEvent{Phase: PhaseContained, EnforcementComplete: true})
	if err := writeMetadataSidecar(opts.Sidecar.MetadataPath(), events); err != nil {
		return RunResult{}, err
	}
	if err := validateRunMetadata(events); err != nil {
		return RunResult{}, err
	}

	execResult, err := enforcer.Exec(ctx, spec, opts)
	if err != nil {
		phase := PhaseFailed
		if errors.Is(err, context.Canceled) {
			phase = PhaseCancelled
		}
		record := RunRecord{Phase: phase, Error: err.Error()}
		result, writeErr := writeTerminalResult(opts.Sidecar, record, true)
		if writeErr != nil {
			return result, writeErr
		}
		return result, err
	}
	return writeTerminalResult(opts.Sidecar, RunRecord{Phase: PhaseExited, ExitCode: execResult.ExitCode}, true)
}

func validateRunOptions(opts RunOptions) error {
	if strings.TrimSpace(opts.Sidecar.Dir) == "" {
		return fmt.Errorf("linux current-user runner requires a sidecar directory")
	}
	return nil
}

func enforceCurrentUser(ctx context.Context, enforcer CurrentUserEnforcer, spec linuxspec.LaunchSpec, plan AdmissionPlan) error {
	steps := []struct {
		stage Stage
		run   func(context.Context) error
	}{
		{stage: StageFDSClosed, run: func(ctx context.Context) error { return enforcer.CloseInheritedFDs(ctx, plan.FDs) }},
		{stage: StageNamespaces, run: func(ctx context.Context) error { return enforcer.CreateNamespaces(ctx, plan.Namespaces) }},
		{stage: StageMounts, run: func(ctx context.Context) error { return enforcer.ApplyMounts(ctx, spec) }},
		{stage: StageNetwork, run: func(ctx context.Context) error { return enforcer.ConfigureNetwork(ctx, plan.Network) }},
		{stage: StagePrivileges, run: func(ctx context.Context) error { return enforcer.DropPrivileges(ctx, spec.Process) }},
		{stage: StageLandlock, run: func(ctx context.Context) error { return enforcer.ApplyLandlock(ctx, plan.Policy) }},
		{stage: StageSeccomp, run: func(ctx context.Context) error { return enforcer.ApplySeccomp(ctx, plan.Policy) }},
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := step.run(ctx); err != nil {
			return fmt.Errorf("%s: %w", step.stage, err)
		}
	}
	return nil
}

func ReadMetadataSidecar(path string) ([]MetadataEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Linux helper metadata sidecar: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var events []MetadataEvent
	if err := dec.Decode(&events); err != nil {
		return nil, fmt.Errorf("parse Linux helper metadata sidecar: %w", err)
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("parse Linux helper metadata sidecar: trailing data")
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("Linux helper metadata sidecar is empty")
	}
	return events, nil
}

func validateRunMetadata(events []MetadataEvent) error {
	want := []RunPhase{PhasePlanned, PhaseLaunched, PhaseContained}
	if len(events) < len(want) {
		return fmt.Errorf("Linux helper metadata missing required phases")
	}
	for i, phase := range want {
		if events[i].Phase != phase {
			return fmt.Errorf("Linux helper metadata phase %d = %q, want %q", i, events[i].Phase, phase)
		}
	}
	if !events[2].EnforcementComplete {
		return fmt.Errorf("Linux helper metadata contained phase arrived before enforcement completed")
	}
	final := events[len(events)-1].Phase
	switch final {
	case PhaseContained, PhaseExited, PhaseFailed, PhaseCancelled:
		return nil
	default:
		return fmt.Errorf("Linux helper metadata final phase %q is not terminal", final)
	}
}

func writeTerminalResult(store SidecarStore, record RunRecord, removeMetadata bool) (RunResult, error) {
	if err := store.writeResultAtomic(record); err != nil {
		return RunResult{Record: record}, err
	}
	if removeMetadata {
		_ = os.Remove(store.MetadataPath())
	}
	return RunResult{Record: record}, nil
}

func (s SidecarStore) writeResultAtomic(record RunRecord) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	temp := s.ResultPath() + ".tmp"
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, s.ResultPath())
}

func writeMetadataSidecar(path string, events []MetadataEvent) error {
	data, err := json.Marshal(events)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
