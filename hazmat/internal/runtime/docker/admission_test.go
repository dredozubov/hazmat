package docker

import (
	"errors"
	"reflect"
	"testing"
)

type fakeAdmissionProbe struct{}
type fakeAdmissionBackend struct{}
type fakeAdmissionAdapter struct{}
type fakeAdmissionSpec struct {
	name string
}

func TestPrepareLaunchAdmissionRunsOrderedSteps(t *testing.T) {
	var calls []string
	req := AdmissionRequest[fakeAdmissionProbe, fakeAdmissionBackend, fakeAdmissionAdapter, fakeAdmissionSpec]{
		ProbeFactory: func() fakeAdmissionProbe {
			calls = append(calls, "probe")
			return fakeAdmissionProbe{}
		},
		LoadHealthyBackend: func(fakeAdmissionProbe) (fakeAdmissionBackend, error) {
			calls = append(calls, "load-backend")
			return fakeAdmissionBackend{}, nil
		},
		SelectBackendAdapter: func(fakeAdmissionBackend) (fakeAdmissionAdapter, error) {
			calls = append(calls, "select-adapter")
			return fakeAdmissionAdapter{}, nil
		},
		CompileLaunchSpec: func(fakeAdmissionBackend) (fakeAdmissionSpec, error) {
			calls = append(calls, "compile-spec")
			return fakeAdmissionSpec{name: "hazmat-claude-project"}, nil
		},
		ValidateLaunchCompatibility: func(fakeAdmissionAdapter, fakeAdmissionSpec, fakeAdmissionBackend) error {
			calls = append(calls, "validate")
			return nil
		},
		VerifyApproval: func(fakeAdmissionBackend) error {
			calls = append(calls, "approve")
			return nil
		},
		PrepareSandbox: func(fakeAdmissionProbe, fakeAdmissionAdapter, fakeAdmissionSpec) error {
			calls = append(calls, "prepare")
			return nil
		},
		RecordManagedSandbox: func(fakeAdmissionBackend, fakeAdmissionSpec) error {
			calls = append(calls, "record")
			return nil
		},
		SandboxName: func(spec fakeAdmissionSpec) string {
			return spec.name
		},
	}

	result, err := PrepareLaunchAdmission(req)
	if err != nil {
		t.Fatalf("PrepareLaunchAdmission: %v", err)
	}
	wantCalls := []string{
		"probe",
		"load-backend",
		"select-adapter",
		"compile-spec",
		"validate",
		"approve",
		"prepare",
		"record",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}
	if result.SandboxName != "hazmat-claude-project" {
		t.Fatalf("SandboxName = %q", result.SandboxName)
	}
}

func TestPrepareLaunchAdmissionRejectsIntegrationEnvBeforeProbe(t *testing.T) {
	var probeCalled bool
	_, err := PrepareLaunchAdmission(AdmissionRequest[fakeAdmissionProbe, fakeAdmissionBackend, fakeAdmissionAdapter, fakeAdmissionSpec]{
		IntegrationEnvKeys: []string{"GOPROXY"},
		ProbeFactory: func() fakeAdmissionProbe {
			probeCalled = true
			return fakeAdmissionProbe{}
		},
	})
	if !errors.Is(err, ErrIntegrationEnvPassthrough) {
		t.Fatalf("PrepareLaunchAdmission error = %v, want ErrIntegrationEnvPassthrough", err)
	}
	if probeCalled {
		t.Fatal("probe was called before integration env rejection")
	}
}

func TestPrepareLaunchAdmissionDoesNotRecordAfterPrepareFailure(t *testing.T) {
	var calls []string
	prepareErr := errors.New("prepare failed")
	req := AdmissionRequest[fakeAdmissionProbe, fakeAdmissionBackend, fakeAdmissionAdapter, fakeAdmissionSpec]{
		ProbeFactory: func() fakeAdmissionProbe {
			calls = append(calls, "probe")
			return fakeAdmissionProbe{}
		},
		LoadHealthyBackend: func(fakeAdmissionProbe) (fakeAdmissionBackend, error) {
			calls = append(calls, "load-backend")
			return fakeAdmissionBackend{}, nil
		},
		SelectBackendAdapter: func(fakeAdmissionBackend) (fakeAdmissionAdapter, error) {
			calls = append(calls, "select-adapter")
			return fakeAdmissionAdapter{}, nil
		},
		CompileLaunchSpec: func(fakeAdmissionBackend) (fakeAdmissionSpec, error) {
			calls = append(calls, "compile-spec")
			return fakeAdmissionSpec{name: "hazmat-claude-project"}, nil
		},
		ValidateLaunchCompatibility: func(fakeAdmissionAdapter, fakeAdmissionSpec, fakeAdmissionBackend) error {
			calls = append(calls, "validate")
			return nil
		},
		VerifyApproval: func(fakeAdmissionBackend) error {
			calls = append(calls, "approve")
			return nil
		},
		PrepareSandbox: func(fakeAdmissionProbe, fakeAdmissionAdapter, fakeAdmissionSpec) error {
			calls = append(calls, "prepare")
			return prepareErr
		},
		RecordManagedSandbox: func(fakeAdmissionBackend, fakeAdmissionSpec) error {
			calls = append(calls, "record")
			return nil
		},
		SandboxName: func(spec fakeAdmissionSpec) string {
			return spec.name
		},
	}

	_, err := PrepareLaunchAdmission(req)
	if !errors.Is(err, prepareErr) {
		t.Fatalf("PrepareLaunchAdmission error = %v, want %v", err, prepareErr)
	}
	wantCalls := []string{
		"probe",
		"load-backend",
		"select-adapter",
		"compile-spec",
		"validate",
		"approve",
		"prepare",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}
}
