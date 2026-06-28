package linux

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

func TestRunAgentUserRequiresExperimentalGateBeforeSideEffects(t *testing.T) {
	store := SidecarStore{Dir: filepath.Join(t.TempDir(), "sidecar")}
	_, err := RunAgentUser(context.Background(), agentUserExecutableSpec(t, sessionmeta.NetworkNone), agentUserReadyReport(), RunOptions{
		Sidecar:    store,
		RootHelper: &fakeAgentUserRootHelper{},
	})
	if err == nil || !strings.Contains(err.Error(), EnvExperimentalAgentUser) {
		t.Fatalf("err = %v, want experimental gate error", err)
	}
	if _, statErr := os.Stat(store.Dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("sidecar dir stat err = %v, want no side effects", statErr)
	}
}

func TestRunAgentUserRejectsSpecGapsBeforeSideEffects(t *testing.T) {
	t.Setenv(EnvExperimentalAgentUser, "1")
	store := SidecarStore{Dir: filepath.Join(t.TempDir(), "sidecar")}
	spec := agentUserExecutableSpec(t, sessionmeta.NetworkNone)
	spec.CapabilityGaps = []linuxspec.CapabilityGap{{
		Code:    linuxspec.GapSetupRequired,
		Message: "setup required",
	}}

	_, err := RunAgentUser(context.Background(), spec, agentUserReadyReport(), RunOptions{
		Sidecar:    store,
		RootHelper: &fakeAgentUserRootHelper{},
	})
	var gapErr GapError
	if !errors.As(err, &gapErr) || !hasGap(gapErr.Gaps, linuxspec.GapSetupRequired) {
		t.Fatalf("err = %v, want setup GapError", err)
	}
	if _, statErr := os.Stat(store.Dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("sidecar dir stat err = %v, want no side effects", statErr)
	}
}

func TestRunAgentUserRejectsCurrentUserSpecWithoutFallback(t *testing.T) {
	t.Setenv(EnvExperimentalAgentUser, "1")
	store := SidecarStore{Dir: filepath.Join(t.TempDir(), "sidecar")}
	_, err := RunAgentUser(context.Background(), currentUserExecutableSpec(t, sessionmeta.NetworkNone), agentUserReadyReport(), RunOptions{
		Sidecar:    store,
		RootHelper: &fakeAgentUserRootHelper{},
	})
	if err == nil || !strings.Contains(err.Error(), `identity "agent-user"`) {
		t.Fatalf("err = %v, want explicit agent-user refusal", err)
	}
	if _, statErr := os.Stat(store.Dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("sidecar dir stat err = %v, want no side effects", statErr)
	}
}

func TestRunAgentUserRequiresInjectedRootHelperBeforeSideEffects(t *testing.T) {
	t.Setenv(EnvExperimentalAgentUser, "1")
	store := SidecarStore{Dir: filepath.Join(t.TempDir(), "sidecar")}
	_, err := RunAgentUser(context.Background(), agentUserExecutableSpec(t, sessionmeta.NetworkNone), agentUserReadyReport(), RunOptions{
		Sidecar: store,
	})
	if err == nil || !strings.Contains(err.Error(), "injected root helper") {
		t.Fatalf("err = %v, want injected root helper error", err)
	}
	if _, statErr := os.Stat(store.Dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("sidecar dir stat err = %v, want no side effects", statErr)
	}
}

func TestRunAgentUserRootHelperMetadataAndRawStreams(t *testing.T) {
	t.Setenv(EnvExperimentalAgentUser, "1")
	store := SidecarStore{Dir: t.TempDir()}
	stdout := []byte("agent stdout\n\x00raw\n")
	stderr := []byte("agent stderr\n\x00raw\n")
	helper := &fakeAgentUserRootHelper{
		stdout:   stdout,
		stderr:   stderr,
		exitCode: 11,
	}
	var stdoutBuf, stderrBuf bytes.Buffer

	result, err := RunAgentUser(context.Background(), agentUserExecutableSpec(t, sessionmeta.NetworkNone), agentUserReadyReport(), RunOptions{
		Stdout:     &stdoutBuf,
		Stderr:     &stderrBuf,
		Sidecar:    store,
		RootHelper: helper,
	})
	if err != nil {
		t.Fatalf("RunAgentUser: %v", err)
	}
	if result.Record.Phase != PhaseExited || result.Record.ExitCode != 11 {
		t.Fatalf("record = %+v, want exited code 11", result.Record)
	}
	if !bytes.Equal(stdoutBuf.Bytes(), stdout) {
		t.Fatalf("stdout = %q, want %q", stdoutBuf.Bytes(), stdout)
	}
	if !bytes.Equal(stderrBuf.Bytes(), stderr) {
		t.Fatalf("stderr = %q, want %q", stderrBuf.Bytes(), stderr)
	}
	if helper.request.Plan.Identity.Lane != linuxspec.IdentityAgentUser ||
		helper.request.Plan.Identity.HelperStrategy != linuxspec.HelperRoot ||
		!helper.request.Plan.Identity.DropToAgent {
		t.Fatalf("helper plan identity = %+v, want agent-user/root-helper drop", helper.request.Plan.Identity)
	}
	if helper.request.Plan.Namespaces.User {
		t.Fatalf("helper plan namespaces = %+v, want root-helper path without rootless user namespace", helper.request.Plan.Namespaces)
	}
	assertLaunchSpecSidecar(t, helper.request)
	wantMetadata := []MetadataEvent{
		{Phase: PhasePlanned},
		{Phase: PhaseLaunched},
		{Phase: PhaseContained, EnforcementComplete: true},
	}
	if !reflect.DeepEqual(helper.metadataAtExec, wantMetadata) {
		t.Fatalf("metadata at helper exec = %#v, want %#v", helper.metadataAtExec, wantMetadata)
	}
	if _, err := os.Stat(store.MetadataPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata sidecar stat err = %v, want removed after terminal result", err)
	}
	record := readRunnerRecord(t, store.ResultPath())
	if record.Phase != PhaseExited || record.ExitCode != 11 {
		t.Fatalf("stored record = %+v, want exited code 11", record)
	}
}

func TestRunAgentUserRejectsMalformedHelperMetadata(t *testing.T) {
	t.Setenv(EnvExperimentalAgentUser, "1")
	store := SidecarStore{Dir: t.TempDir()}
	helper := &fakeAgentUserRootHelper{
		metadata: []MetadataEvent{
			{Phase: PhasePlanned},
			{Phase: PhaseLaunched},
			{Phase: PhaseContained},
		},
	}

	result, err := RunAgentUser(context.Background(), agentUserExecutableSpec(t, sessionmeta.NetworkNone), agentUserReadyReport(), RunOptions{
		Sidecar:    store,
		RootHelper: helper,
	})
	if err == nil || !strings.Contains(err.Error(), "before enforcement") {
		t.Fatalf("err = %v, want metadata enforcement error", err)
	}
	if result.Record.Phase != PhaseFailed {
		t.Fatalf("record = %+v, want failed", result.Record)
	}
	record := readRunnerRecord(t, store.ResultPath())
	if record.Phase != PhaseFailed {
		t.Fatalf("stored record = %+v, want failed", record)
	}
}

func TestRunAgentUserCancellationWritesAtomicResultAndRemovesSidecar(t *testing.T) {
	t.Setenv(EnvExperimentalAgentUser, "1")
	store := SidecarStore{Dir: t.TempDir()}
	if err := os.WriteFile(store.ResultPath()+".tmp", []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	helper := &fakeAgentUserRootHelper{err: context.Canceled}

	result, err := RunAgentUser(context.Background(), agentUserExecutableSpec(t, sessionmeta.NetworkNone), agentUserReadyReport(), RunOptions{
		Sidecar:    store,
		RootHelper: helper,
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

type fakeAgentUserRootHelper struct {
	stdout         []byte
	stderr         []byte
	exitCode       int
	err            error
	metadata       []MetadataEvent
	request        AgentUserHelperRequest
	metadataAtExec []MetadataEvent
}

func (f *fakeAgentUserRootHelper) Execute(_ context.Context, request AgentUserHelperRequest, opts RunOptions) (ExecResult, error) {
	f.request = request
	events := f.metadata
	if events == nil {
		events = []MetadataEvent{
			{Phase: PhasePlanned},
			{Phase: PhaseLaunched},
			{Phase: PhaseContained, EnforcementComplete: true},
		}
	}
	if err := writeMetadataSidecar(opts.Sidecar.MetadataPath(), events); err != nil {
		return ExecResult{}, err
	}
	metadata, err := ReadMetadataSidecar(opts.Sidecar.MetadataPath())
	if err != nil {
		return ExecResult{}, err
	}
	f.metadataAtExec = metadata
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
	if f.err != nil {
		return ExecResult{}, f.err
	}
	return ExecResult{ExitCode: f.exitCode}, nil
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

func agentUserExecutableSpec(t *testing.T, network sessionmeta.NetworkMode) linuxspec.LaunchSpec {
	t.Helper()
	spec := agentUserSpec(t, network)
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

func assertLaunchSpecSidecar(t *testing.T, request AgentUserHelperRequest) {
	t.Helper()
	if request.SpecPath == "" || request.SpecSHA256 == "" || request.SpecNonce == "" {
		t.Fatalf("helper request missing spec path/digest/nonce: %+v", request)
	}
	if len(request.SpecNonce) != 32 {
		t.Fatalf("helper nonce length = %d, want 32 hex chars", len(request.SpecNonce))
	}
	info, err := os.Stat(request.SpecPath)
	if err != nil {
		t.Fatalf("stat spec sidecar: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("spec sidecar mode = %o, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(request.SpecPath)
	if err != nil {
		t.Fatalf("read spec sidecar: %v", err)
	}
	sum := sha256.Sum256(data)
	if got := fmt.Sprintf("%x", sum[:]); got != request.SpecSHA256 {
		t.Fatalf("spec digest = %s, want %s", request.SpecSHA256, got)
	}
	var spec linuxspec.LaunchSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse spec sidecar: %v", err)
	}
	if spec.Identity != linuxspec.IdentityAgentUser || spec.HelperStrategy != linuxspec.HelperRoot {
		t.Fatalf("spec identity/helper = %s/%s, want agent-user/root-helper", spec.Identity, spec.HelperStrategy)
	}
}
