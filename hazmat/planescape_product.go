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
	planescapeProductProviderFailure planescapeProductFailureReason = "provider_failure"
	planescapeProductRPCPending      planescapeProductFailureReason = "rpc_pending"
	planescapeProductLocalFallback   planescapeProductFailureReason = "local_fallback"
)

// planescapeProductError is deliberately detached from endpoint diagnostics.
// Product callers may select behavior by Class without logging provider bytes.
type planescapeProductError struct {
	class  planescapeprovider.ErrorClass
	reason planescapeProductFailureReason
}

func (e *planescapeProductError) Error() string {
	if e == nil {
		return "configured Planescape provider failed closed"
	}
	switch e.reason {
	case planescapeProductRPCPending:
		return "configured Planescape provider cannot admit sessions until its protected RPC contract is available; local execution is disabled"
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

type planescapeProductDependencies struct {
	Endpoint       planescapeprovider.Endpoint
	CheckpointRoot string
	Now            func() time.Time
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
// configured Planescape provider must produce an admitted external session
// before local startup can run. Until the RPC admission path exists, even a
// healthy discovery response remains terminal.
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

	client, err := openPlanescapeProductClient(dependencies)
	if err != nil {
		return err
	}
	if _, err := client.Discover(ctx); err != nil {
		return mapPlanescapeProductError(err)
	}
	return newPlanescapeProductError(
		planescapeprovider.ErrorUnsupported,
		planescapeProductRPCPending,
	)
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
