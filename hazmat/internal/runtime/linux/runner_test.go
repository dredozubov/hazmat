package linux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	linuxspec "hazmat/containment/linux"
	platformlinux "hazmat/platform/linux"
	"hazmat/sessionmeta"
)

func TestRunCurrentUserRequiresExperimentalGateBeforeSideEffects(t *testing.T) {
	store := SidecarStore{Dir: filepath.Join(t.TempDir(), "sidecar")}
	_, err := RunCurrentUser(context.Background(), currentUserExecutableSpec(t, sessionmeta.NetworkNone), availableReport(), RunOptions{
		Sidecar:  store,
		Enforcer: &fakeCurrentUserEnforcer{},
	})
	if err == nil || !strings.Contains(err.Error(), EnvExperimentalCurrentUser) {
		t.Fatalf("err = %v, want experimental gate error", err)
	}
	if _, statErr := os.Stat(store.Dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("sidecar dir stat err = %v, want no side effects", statErr)
	}
}

func TestRunCurrentUserRejectsSpecGapsBeforeSideEffects(t *testing.T) {
	t.Setenv(EnvExperimentalCurrentUser, "1")
	store := SidecarStore{Dir: filepath.Join(t.TempDir(), "sidecar")}
	spec := currentUserExecutableSpec(t, sessionmeta.NetworkNone)
	spec.CapabilityGaps = []linuxspec.CapabilityGap{{
		Code:    linuxspec.GapRuntimeNotLinux,
		Message: "not Linux",
	}}

	_, err := RunCurrentUser(context.Background(), spec, availableReport(), RunOptions{
		Sidecar:  store,
		Enforcer: &fakeCurrentUserEnforcer{},
	})
	var gapErr GapError
	if !errors.As(err, &gapErr) || !hasGap(gapErr.Gaps, linuxspec.GapRuntimeNotLinux) {
		t.Fatalf("err = %v, want runtime GapError", err)
	}
	if _, statErr := os.Stat(store.Dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("sidecar dir stat err = %v, want no side effects", statErr)
	}
}

func TestRunCurrentUserRejectsMissingPrimitivesBeforeSideEffects(t *testing.T) {
	t.Setenv(EnvExperimentalCurrentUser, "1")
	store := SidecarStore{Dir: filepath.Join(t.TempDir(), "sidecar")}
	report := availableReport()
	report.Features.UserNamespaces = platformlinux.FeatureReport{State: platformlinux.FeatureUnavailable, Source: "userns"}

	_, err := RunCurrentUser(context.Background(), currentUserExecutableSpec(t, sessionmeta.NetworkNone), report, RunOptions{
		Sidecar:  store,
		Enforcer: &fakeCurrentUserEnforcer{},
	})
	var gapErr GapError
	if !errors.As(err, &gapErr) || !hasGap(gapErr.Gaps, linuxspec.GapUserNamespaceUnavailable) {
		t.Fatalf("err = %v, want user namespace GapError", err)
	}
	if _, statErr := os.Stat(store.Dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("sidecar dir stat err = %v, want no side effects", statErr)
	}
}

func TestRunCurrentUserOrderMetadataAndRawStreams(t *testing.T) {
	t.Setenv(EnvExperimentalCurrentUser, "1")
	store := SidecarStore{Dir: t.TempDir()}
	stdout := []byte("harness stdout\n\x00raw\n")
	stderr := []byte("harness stderr\n\x00raw\n")
	enforcer := &fakeCurrentUserEnforcer{
		stdout:   stdout,
		stderr:   stderr,
		exitCode: 7,
	}
	var stdoutBuf, stderrBuf bytes.Buffer

	result, err := RunCurrentUser(context.Background(), currentUserExecutableSpec(t, sessionmeta.NetworkNone), availableReport(), RunOptions{
		Stdout:   &stdoutBuf,
		Stderr:   &stderrBuf,
		Sidecar:  store,
		Enforcer: enforcer,
	})
	if err != nil {
		t.Fatalf("RunCurrentUser: %v", err)
	}
	if result.Record.Phase != PhaseExited || result.Record.ExitCode != 7 {
		t.Fatalf("record = %+v, want exited code 7", result.Record)
	}
	wantSteps := []Stage{
		StageFDSClosed,
		StageNamespaces,
		StageMounts,
		StageNetwork,
		StagePrivileges,
		StageLandlock,
		StageSeccomp,
		StageExec,
	}
	if !reflect.DeepEqual(enforcer.steps, wantSteps) {
		t.Fatalf("steps = %#v, want %#v", enforcer.steps, wantSteps)
	}
	if !bytes.Equal(stdoutBuf.Bytes(), stdout) {
		t.Fatalf("stdout = %q, want %q", stdoutBuf.Bytes(), stdout)
	}
	if !bytes.Equal(stderrBuf.Bytes(), stderr) {
		t.Fatalf("stderr = %q, want %q", stderrBuf.Bytes(), stderr)
	}
	wantMetadata := []MetadataEvent{
		{Phase: PhasePlanned},
		{Phase: PhaseLaunched},
		{Phase: PhaseContained, EnforcementComplete: true},
	}
	if !reflect.DeepEqual(enforcer.metadataAtExec, wantMetadata) {
		t.Fatalf("metadata at exec = %#v, want %#v", enforcer.metadataAtExec, wantMetadata)
	}
	if _, err := os.Stat(store.MetadataPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata sidecar stat err = %v, want removed after terminal result", err)
	}
	record := readRunnerRecord(t, store.ResultPath())
	if record.Phase != PhaseExited || record.ExitCode != 7 {
		t.Fatalf("stored record = %+v, want exited code 7", record)
	}
}

func TestRunCurrentUserCancellationWritesAtomicResultAndRemovesSidecar(t *testing.T) {
	t.Setenv(EnvExperimentalCurrentUser, "1")
	store := SidecarStore{Dir: t.TempDir()}
	if err := os.WriteFile(store.ResultPath()+".tmp", []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	enforcer := &fakeCurrentUserEnforcer{execErr: context.Canceled}

	result, err := RunCurrentUser(context.Background(), currentUserExecutableSpec(t, sessionmeta.NetworkNone), availableReport(), RunOptions{
		Sidecar:  store,
		Enforcer: enforcer,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if result.Record.Phase != PhaseCancelled {
		t.Fatalf("record = %+v, want cancelled", result.Record)
	}
	record := readRunnerRecord(t, store.ResultPath())
	if record.Phase != PhaseCancelled {
		t.Fatalf("stored record = %+v, want cancelled", record)
	}
	for _, path := range []string{store.MetadataPath(), store.ResultPath() + ".tmp"} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s stat err = %v, want removed", path, err)
		}
	}
}

func TestReadMetadataSidecarRejectsMalformedInput(t *testing.T) {
	store := SidecarStore{Dir: t.TempDir()}
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "malformed", data: `[{`},
		{name: "unknown field", data: `[{"phase":"planned","extra":true}]`},
		{name: "trailing data", data: `[{"phase":"planned"}] []`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(store.MetadataPath(), []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadMetadataSidecar(store.MetadataPath()); err == nil {
				t.Fatal("ReadMetadataSidecar accepted malformed metadata")
			}
		})
	}
}

type fakeCurrentUserEnforcer struct {
	steps          []Stage
	stdout         []byte
	stderr         []byte
	exitCode       int
	execErr        error
	metadataAtExec []MetadataEvent
}

func (f *fakeCurrentUserEnforcer) CloseInheritedFDs(context.Context, FDClosurePlan) error {
	f.steps = append(f.steps, StageFDSClosed)
	return nil
}

func (f *fakeCurrentUserEnforcer) CreateNamespaces(context.Context, NamespacePlan) error {
	f.steps = append(f.steps, StageNamespaces)
	return nil
}

func (f *fakeCurrentUserEnforcer) ApplyMounts(context.Context, linuxspec.LaunchSpec) error {
	f.steps = append(f.steps, StageMounts)
	return nil
}

func (f *fakeCurrentUserEnforcer) ConfigureNetwork(context.Context, NetworkAdmission) error {
	f.steps = append(f.steps, StageNetwork)
	return nil
}

func (f *fakeCurrentUserEnforcer) DropPrivileges(context.Context, linuxspec.ProcessSpec) error {
	f.steps = append(f.steps, StagePrivileges)
	return nil
}

func (f *fakeCurrentUserEnforcer) ApplyLandlock(context.Context, PolicyPlan) error {
	f.steps = append(f.steps, StageLandlock)
	return nil
}

func (f *fakeCurrentUserEnforcer) ApplySeccomp(context.Context, PolicyPlan) error {
	f.steps = append(f.steps, StageSeccomp)
	return nil
}

func (f *fakeCurrentUserEnforcer) Exec(_ context.Context, _ linuxspec.LaunchSpec, opts RunOptions) (ExecResult, error) {
	f.steps = append(f.steps, StageExec)
	events, err := ReadMetadataSidecar(opts.Sidecar.MetadataPath())
	if err != nil {
		return ExecResult{}, err
	}
	f.metadataAtExec = events
	if opts.Stdout != nil {
		if _, err := opts.Stdout.Write(f.stdout); err != nil {
			return ExecResult{}, err
		}
	}
	if opts.Stderr != nil {
		if _, err := opts.Stderr.Write(f.stderr); err != nil {
			return ExecResult{}, err
		}
	}
	if f.execErr != nil {
		return ExecResult{}, f.execErr
	}
	return ExecResult{ExitCode: f.exitCode}, nil
}

func currentUserExecutableSpec(t *testing.T, network sessionmeta.NetworkMode) linuxspec.LaunchSpec {
	t.Helper()
	spec := currentUserSpec(t, network)
	spec.Phase = linuxspec.PhaseExperimental
	spec.Command = []string{"/bin/sh", "-c", "printf hazmat"}
	spec.CapabilityGaps = nil
	return spec
}

func readRunnerRecord(t *testing.T, path string) RunRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record
}
