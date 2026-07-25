package hazmat

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"hazmat/configmodel"
	"hazmat/planescapeprovider"
)

type planescapeProductFailureReason string

const (
	planescapeProductProviderFailure  planescapeProductFailureReason = "provider_failure"
	planescapeProductLifecyclePending planescapeProductFailureReason = "lifecycle_pending"
	planescapeProductLocalFallback    planescapeProductFailureReason = "local_fallback"
)

// planescapeProductError is deliberately detached from endpoint diagnostics.
// Product callers may select behavior by Class without logging provider bytes.
type planescapeProductError struct {
	class     planescapeprovider.ErrorClass
	reason    planescapeProductFailureReason
	admission *planescapeProductAdmission
}

func (e *planescapeProductError) Error() string {
	if e == nil {
		return "configured Planescape provider failed closed"
	}
	switch e.reason {
	case planescapeProductLifecyclePending:
		return "configured Planescape provider admitted the session, but product lifecycle execution is unavailable; local execution is disabled"
	case planescapeProductLocalFallback:
		return "configured Planescape provider session cannot use a local execution path"
	default:
		return "configured Planescape provider failed closed: " + string(e.class)
	}
}

func (e *planescapeProductError) Class() planescapeprovider.ErrorClass {
	if e == nil {
		return ""
	}
	return e.class
}

func (e *planescapeProductError) admissionState() (planescapeProductAdmission, bool) {
	if e == nil || e.reason != planescapeProductLifecyclePending ||
		e.admission == nil || !e.admission.valid() {
		return planescapeProductAdmission{}, false
	}
	return *e.admission, true
}

type planescapeProductCompiledPlanSource interface {
	CompiledContainmentPlan(context.Context) (planescapeprovider.CompiledContainmentPlan, error)
}

type planescapeProductDependencies struct {
	Endpoint           planescapeprovider.Endpoint
	CompiledPlanSource planescapeProductCompiledPlanSource
	CheckpointRoot     string
	Now                func() time.Time
}

// planescapeProductAdmission retains the exact Rust-produced plan together
// with the client-bound admitted session. The zero value is inert.
type planescapeProductAdmission struct {
	input   planescapeprovider.AdmissionInput
	session planescapeprovider.Session
}

func newPlanescapeProductAdmission(
	input planescapeprovider.AdmissionInput,
	session planescapeprovider.Session,
) (planescapeProductAdmission, error) {
	if _, err := planescapeprovider.NewAdmissionInput(input.Plan()); err != nil {
		return planescapeProductAdmission{}, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	if _, ok := session.ID(); !ok {
		return planescapeProductAdmission{}, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	return planescapeProductAdmission{input: input, session: session}, nil
}

func (v planescapeProductAdmission) valid() bool {
	if _, err := planescapeprovider.NewAdmissionInput(v.input.Plan()); err != nil {
		return false
	}
	_, ok := v.session.ID()
	return ok
}

func (v planescapeProductAdmission) Plan() (planescapeprovider.CompiledContainmentPlan, bool) {
	if !v.valid() {
		return planescapeprovider.CompiledContainmentPlan{}, false
	}
	return v.input.Plan(), true
}

func (v planescapeProductAdmission) Session() (planescapeprovider.Session, bool) {
	if !v.valid() {
		return planescapeprovider.Session{}, false
	}
	return v.session, true
}

func configuredSessionExecutionProvider() (configmodel.ExecutionProvider, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	return cfg.SessionExecutionProvider(), nil
}

func defaultPlanescapeProductDependencies() planescapeProductDependencies {
	return planescapeProductDependencies{
		CheckpointRoot: filepath.Join(filepath.Dir(configFilePath), "planescape-provider-checkpoints"),
	}
}

// openPlanescapeProductClient binds the semantic client to one injected
// Endpoint and a stable on-disk checkpoint root. Reopening this function in a
// later process creates a new client over the same durable records.
func openPlanescapeProductClient(
	dependencies planescapeProductDependencies,
) (*planescapeprovider.Client, error) {
	if dependencies.Endpoint == nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	store, err := planescapeprovider.NewFileStore(dependencies.CheckpointRoot)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	client, err := planescapeprovider.NewClient(planescapeprovider.ClientConfig{
		Endpoint: dependencies.Endpoint,
		Store:    store,
		Now:      dependencies.Now,
	})
	if err != nil {
		return nil, mapPlanescapeProductError(err)
	}
	return client, nil
}

// runSessionStartupWithExecutionProvider is the product construction gate. A
// configured Planescape provider must produce an admitted external session,
// and it never authorizes the local startup path.
func runSessionStartupWithExecutionProvider(
	ctx context.Context,
	cfg sessionConfig,
	dependencies planescapeProductDependencies,
	localStartup func() error,
) error {
	switch cfg.ExecutionProvider {
	case configmodel.ExecutionProviderLocal:
		if localStartup == nil {
			return newPlanescapeProductError(
				planescapeprovider.ErrorInvalid,
				planescapeProductLocalFallback,
			)
		}
		return localStartup()
	case configmodel.ExecutionProviderPlanescape:
	default:
		return newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	if ctx == nil {
		return newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}

	admission, err := admitPlanescapeProductSession(ctx, dependencies)
	if err != nil {
		return err
	}
	return newPlanescapeProductLifecyclePendingError(admission)
}

func admitPlanescapeProductSession(
	ctx context.Context,
	dependencies planescapeProductDependencies,
) (planescapeProductAdmission, error) {
	if ctx == nil {
		return planescapeProductAdmission{}, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	if dependencies.CompiledPlanSource == nil {
		return planescapeProductAdmission{}, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	plan, err := dependencies.CompiledPlanSource.CompiledContainmentPlan(ctx)
	if err != nil {
		return planescapeProductAdmission{}, mapPlanescapeProductError(err)
	}
	input, err := planescapeprovider.NewAdmissionInput(plan)
	if err != nil {
		return planescapeProductAdmission{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	client, err := openPlanescapeProductClient(dependencies)
	if err != nil {
		return planescapeProductAdmission{}, err
	}
	discovery, err := client.Discover(ctx)
	if err != nil {
		return planescapeProductAdmission{}, mapPlanescapeProductError(err)
	}
	session, err := client.Admit(ctx, discovery, input)
	if err != nil {
		return planescapeProductAdmission{}, mapPlanescapeProductError(err)
	}
	return newPlanescapeProductAdmission(input, session)
}

func rejectConfiguredProviderLocalFallback(cfg sessionConfig) error {
	switch cfg.ExecutionProvider {
	case configmodel.ExecutionProviderLocal:
		return nil
	case configmodel.ExecutionProviderPlanescape:
		return newPlanescapeProductError(
			planescapeprovider.ErrorUnsupported,
			planescapeProductLocalFallback,
		)
	default:
		return newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductLocalFallback,
		)
	}
}

func mapPlanescapeProductError(err error) error {
	if err == nil {
		return newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	var productError *planescapeProductError
	if errors.As(err, &productError) {
		return productError
	}
	var clientError *planescapeprovider.Error
	if errors.As(err, &clientError) {
		class := clientError.Class()
		switch class {
		case planescapeprovider.ErrorInvalid,
			planescapeprovider.ErrorUnsupported,
			planescapeprovider.ErrorUnavailable,
			planescapeprovider.ErrorConflict:
			return newPlanescapeProductError(class, planescapeProductProviderFailure)
		}
	}
	return newPlanescapeProductError(
		planescapeprovider.ErrorUnavailable,
		planescapeProductProviderFailure,
	)
}

func newPlanescapeProductError(
	class planescapeprovider.ErrorClass,
	reason planescapeProductFailureReason,
) error {
	return &planescapeProductError{class: class, reason: reason}
}

func newPlanescapeProductLifecyclePendingError(
	admission planescapeProductAdmission,
) error {
	if !admission.valid() {
		return newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	return &planescapeProductError{
		class:     planescapeprovider.ErrorUnsupported,
		reason:    planescapeProductLifecyclePending,
		admission: &admission,
	}
}
