package docker

import (
	"errors"
	"fmt"
)

var ErrIntegrationEnvPassthrough = errors.New("integration env passthrough is not supported with --docker=sandbox yet")

type AdmissionRequest[P, B, A, S any] struct {
	IntegrationEnvKeys []string

	ProbeFactory                func() P
	LoadHealthyBackend          func(P) (B, error)
	SelectBackendAdapter        func(B) (A, error)
	CompileLaunchSpec           func(B) (S, error)
	ValidateLaunchCompatibility func(A, S, B) error
	VerifyApproval              func(B) error
	PrepareSandbox              func(P, A, S) error
	RecordManagedSandbox        func(B, S) error
	SandboxName                 func(S) string
}

type AdmissionResult[P, A any] struct {
	Probe       P
	Adapter     A
	SandboxName string
}

func PrepareLaunchAdmission[P, B, A, S any](req AdmissionRequest[P, B, A, S]) (AdmissionResult[P, A], error) {
	if len(req.IntegrationEnvKeys) > 0 {
		return AdmissionResult[P, A]{}, ErrIntegrationEnvPassthrough
	}
	if err := req.validate(); err != nil {
		return AdmissionResult[P, A]{}, err
	}

	probe := req.ProbeFactory()
	backend, err := req.LoadHealthyBackend(probe)
	if err != nil {
		return AdmissionResult[P, A]{}, err
	}
	adapter, err := req.SelectBackendAdapter(backend)
	if err != nil {
		return AdmissionResult[P, A]{}, err
	}
	spec, err := req.CompileLaunchSpec(backend)
	if err != nil {
		return AdmissionResult[P, A]{}, err
	}
	if err := req.ValidateLaunchCompatibility(adapter, spec, backend); err != nil {
		return AdmissionResult[P, A]{}, err
	}
	if err := req.VerifyApproval(backend); err != nil {
		return AdmissionResult[P, A]{}, err
	}
	if err := req.PrepareSandbox(probe, adapter, spec); err != nil {
		return AdmissionResult[P, A]{}, err
	}
	if err := req.RecordManagedSandbox(backend, spec); err != nil {
		return AdmissionResult[P, A]{}, err
	}

	return AdmissionResult[P, A]{
		Probe:       probe,
		Adapter:     adapter,
		SandboxName: req.SandboxName(spec),
	}, nil
}

func (req AdmissionRequest[P, B, A, S]) validate() error {
	missing := ""
	switch {
	case req.ProbeFactory == nil:
		missing = "probe factory"
	case req.LoadHealthyBackend == nil:
		missing = "healthy backend loader"
	case req.SelectBackendAdapter == nil:
		missing = "backend adapter selector"
	case req.CompileLaunchSpec == nil:
		missing = "launch spec compiler"
	case req.ValidateLaunchCompatibility == nil:
		missing = "launch compatibility validator"
	case req.VerifyApproval == nil:
		missing = "approval verifier"
	case req.PrepareSandbox == nil:
		missing = "sandbox preparer"
	case req.RecordManagedSandbox == nil:
		missing = "managed sandbox recorder"
	case req.SandboxName == nil:
		missing = "sandbox name extractor"
	}
	if missing != "" {
		return fmt.Errorf("docker launch admission requires %s", missing)
	}
	return nil
}
