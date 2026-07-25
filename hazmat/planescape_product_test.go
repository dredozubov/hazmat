package hazmat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hazmat/configmodel"
	"hazmat/planescapeprovider"
)

func TestConfiguredPlanescapeProviderFailuresNeverStartLocalRuntime(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	capabilities := planescapeProductCapabilities(t)
	cases := map[string]struct {
		endpoint *planescapeProductEndpointFake
		want     planescapeprovider.ErrorClass
		secret   string
	}{
		"unsupported": {
			endpoint: &planescapeProductEndpointFake{
				discoverErr: newPlanescapeProviderFailureForTest(
					t,
					planescapeprovider.ProviderErrorUnsupported,
				),
			},
			want: planescapeprovider.ErrorUnsupported,
		},
		"unavailable": {
			endpoint: &planescapeProductEndpointFake{
				discoverErr: newPlanescapeProviderFailureForTest(
					t,
					planescapeprovider.ProviderErrorUnavailable,
				),
			},
			want: planescapeprovider.ErrorUnavailable,
		},
		"endpoint death": {
			endpoint: &planescapeProductEndpointFake{
				discoverErr: errors.New("provider-death-secret-diagnostic"),
			},
			want:   planescapeprovider.ErrorUnavailable,
			secret: "provider-death-secret-diagnostic",
		},
		"discovery succeeds but RPC admission is unavailable": {
			endpoint: &planescapeProductEndpointFake{capabilities: capabilities},
			want:     planescapeprovider.ErrorUnsupported,
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			localStarts := 0
			err := runSessionStartupWithExecutionProvider(
				context.Background(),
				sessionConfig{ExecutionProvider: configmodel.ExecutionProviderPlanescape},
				planescapeProductDependencies{
					Endpoint:       test.endpoint,
					CheckpointRoot: filepath.Join(t.TempDir(), "checkpoints"),
					Now:            func() time.Time { return now },
				},
				func() error {
					localStarts++
					return nil
				},
			)
			requirePlanescapeProductErrorClass(t, err, test.want)
			if localStarts != 0 {
				t.Fatalf("local startup calls = %d, want 0", localStarts)
			}
			if test.endpoint.discoverCalls != 1 {
				t.Fatalf("Discover calls = %d, want 1", test.endpoint.discoverCalls)
			}
			if test.endpoint.admitCalls != 0 {
				t.Fatalf("Admit calls = %d, want 0 before RPC exists", test.endpoint.admitCalls)
			}
			if test.secret != "" && strings.Contains(err.Error(), test.secret) {
				t.Fatalf("product error leaked endpoint diagnostic: %v", err)
			}
		})
	}
}

func TestUnconfiguredPlanescapeEndpointDoesNotAffectLocalStartup(t *testing.T) {
	endpoint := &planescapeProductEndpointFake{
		discoverErr: errors.New("must not be called"),
	}
	localStarts := 0
	err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{ExecutionProvider: configmodel.ExecutionProviderLocal},
		planescapeProductDependencies{
			Endpoint:       endpoint,
			CheckpointRoot: filepath.Join(t.TempDir(), "checkpoints"),
		},
		func() error {
			localStarts++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if localStarts != 1 {
		t.Fatalf("local startup calls = %d, want 1", localStarts)
	}
	if endpoint.discoverCalls != 0 {
		t.Fatalf("Discover calls = %d, want 0 without explicit provider configuration", endpoint.discoverCalls)
	}
}

func TestConfiguredPlanescapeProviderCannotReachNativeRunner(t *testing.T) {
	runnerCalls := 0
	err := runPreparedAgentSeatbeltScriptWithRunner(
		preparedSession{
			Config: sessionConfig{
				ExecutionProvider: configmodel.ExecutionProviderPlanescape,
			},
		},
		sessionLaunchUI{},
		`exec "$@"`,
		func(sessionConfig, sessionBackendPlan, sessionLaunchUI, string, ...string) error {
			runnerCalls++
			return nil
		},
		"/usr/bin/true",
	)
	requirePlanescapeProductErrorClass(t, err, planescapeprovider.ErrorUnsupported)
	if runnerCalls != 0 {
		t.Fatalf("native runner calls = %d, want 0", runnerCalls)
	}

	err = runPreparedAgentSeatbeltScriptWithRunner(
		preparedSession{
			Config: sessionConfig{
				ExecutionProvider: configmodel.ExecutionProviderLocal,
			},
		},
		sessionLaunchUI{},
		`exec "$@"`,
		func(sessionConfig, sessionBackendPlan, sessionLaunchUI, string, ...string) error {
			runnerCalls++
			return nil
		},
		"/usr/bin/true",
	)
	if err != nil {
		t.Fatal(err)
	}
	if runnerCalls != 1 {
		t.Fatalf("native runner calls after local session = %d, want 1", runnerCalls)
	}
}

func TestPlanescapeProductClientReopensDurableCheckpointStore(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	capabilities := planescapeProductCapabilities(t)
	requirement, err := planescapeprovider.NewExecutionRequirement(
		planescapeprovider.ExecutionRequirementInput{
			RequirementID:              "requirement-product-1",
			ControllerAttemptID:        "attempt-product-1",
			AuthorityHash:              planescapeProductHash("a"),
			RequiredCapabilities:       []planescapeprovider.Capability{planescapeprovider.CapabilityToolExecute},
			RequiredResourceDimensions: []planescapeprovider.ResourceDimension{planescapeprovider.ResourceMemoryBytes},
			EvidenceProfileHash:        planescapeProductHash("b"),
			CanonicalHash:              planescapeProductHash("c"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := planescapeprovider.NewSessionAdmission(
		planescapeprovider.SessionAdmissionInput{
			SessionID:             "session-product-1",
			ProviderID:            capabilities.ProviderID().String(),
			ProviderEpoch:         capabilities.ProviderEpoch().Uint64(),
			RequirementHash:       requirement.CanonicalHash().String(),
			CompiledPlanHash:      planescapeProductHash("d"),
			SessionCapabilityHash: planescapeProductHash("e"),
			ExpiresAt:             now.Add(time.Hour),
			CanonicalHash:         planescapeProductHash("f"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &planescapeProductEndpointFake{
		capabilities: capabilities,
		admission:    admission,
	}
	checkpointRoot := filepath.Join(t.TempDir(), "checkpoints")
	dependencies := planescapeProductDependencies{
		Endpoint:       endpoint,
		CheckpointRoot: checkpointRoot,
		Now:            func() time.Time { return now },
	}

	firstClient, err := openPlanescapeProductClient(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	firstDiscovery, err := firstClient.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	admissionInput, err := planescapeprovider.NewAdmissionInput(
		requirement,
		planescapeprovider.ProfilePortable,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstSession, err := firstClient.Admit(
		context.Background(),
		firstDiscovery,
		admissionInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstID, ok := firstSession.ID()
	if !ok {
		t.Fatal("first admitted session has no ID")
	}

	secondClient, err := openPlanescapeProductClient(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	secondDiscovery, err := secondClient.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := secondClient.Reconnect(
		context.Background(),
		secondDiscovery,
		firstID.String(),
	)
	if err != nil {
		t.Fatal(err)
	}
	reopenedID, ok := reopened.ID()
	if !ok || reopenedID.String() != firstID.String() {
		t.Fatalf("reopened session ID = %q, %v; want %q, true", reopenedID.String(), ok, firstID.String())
	}
	if info, err := os.Stat(checkpointRoot); err != nil {
		t.Fatalf("checkpoint root was not persisted: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("checkpoint root mode = %v, want directory", info.Mode())
	}
	if endpoint.admitCalls != 1 {
		t.Fatalf("Admit calls = %d, want 1; reconnect must use durable state", endpoint.admitCalls)
	}
}

func TestConfiguredSessionExecutionProviderLoadsExplicitPlanescapeSelection(t *testing.T) {
	savedConfigPath := configFilePath
	configDir := t.TempDir()
	configFilePath = filepath.Join(configDir, "config.yaml")
	t.Cleanup(func() {
		configFilePath = savedConfigPath
	})
	if err := runConfigSet("session.execution_provider", "planescape"); err != nil {
		t.Fatal(err)
	}

	provider, err := configuredSessionExecutionProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider != configmodel.ExecutionProviderPlanescape {
		t.Fatalf("execution provider = %q, want %q", provider, configmodel.ExecutionProviderPlanescape)
	}
	dependencies := defaultPlanescapeProductDependencies()
	wantRoot := filepath.Join(configDir, "planescape-provider-checkpoints")
	if dependencies.CheckpointRoot != wantRoot {
		t.Fatalf("checkpoint root = %q, want %q", dependencies.CheckpointRoot, wantRoot)
	}
	if err := runConfigSet("session.execution_provider", "none"); err != nil {
		t.Fatal(err)
	}
	provider, err = configuredSessionExecutionProvider()
	if err != nil {
		t.Fatal(err)
	}
	if provider != configmodel.ExecutionProviderLocal {
		t.Fatalf("execution provider after none = %q, want local", provider)
	}
}

type planescapeProductEndpointFake struct {
	capabilities  planescapeprovider.ProviderCapabilities
	admission     planescapeprovider.SessionAdmission
	discoverErr   error
	discoverCalls int
	admitCalls    int
}

func (e *planescapeProductEndpointFake) Discover(context.Context) (planescapeprovider.ProviderCapabilities, error) {
	e.discoverCalls++
	return e.capabilities, e.discoverErr
}

func (e *planescapeProductEndpointFake) Admit(
	context.Context,
	planescapeprovider.ExecutionRequirement,
) (planescapeprovider.SessionAdmission, error) {
	e.admitCalls++
	return e.admission, nil
}

func (*planescapeProductEndpointFake) Operate(
	context.Context,
	planescapeprovider.AgentOperation,
) (planescapeprovider.OperationResponse, error) {
	return nil, errors.New("unexpected Operate call")
}

func (*planescapeProductEndpointFake) Freeze(
	context.Context,
	planescapeprovider.Freeze,
) (planescapeprovider.FreezeAck, error) {
	return planescapeprovider.FreezeAck{}, errors.New("unexpected Freeze call")
}

func (*planescapeProductEndpointFake) Cancel(
	context.Context,
	planescapeprovider.Cancellation,
) (planescapeprovider.CancellationAck, error) {
	return planescapeprovider.CancellationAck{}, errors.New("unexpected Cancel call")
}

func planescapeProductCapabilities(t *testing.T) planescapeprovider.ProviderCapabilities {
	t.Helper()
	capabilities, err := planescapeprovider.NewProviderCapabilities(
		planescapeprovider.ProviderCapabilitiesInput{
			ProviderID:     "planescape-linux-test",
			ProviderEpoch:  7,
			Profile:        planescapeprovider.ProfilePortable,
			CapabilityHash: planescapeProductHash("1"),
			CanonicalHash:  planescapeProductHash("2"),
			Capabilities: []planescapeprovider.Capability{
				planescapeprovider.CapabilityToolExecute,
			},
			ResourceDimensions: []planescapeprovider.ResourceDimension{
				planescapeprovider.ResourceMemoryBytes,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return capabilities
}

func newPlanescapeProviderFailureForTest(
	t *testing.T,
	code planescapeprovider.ProviderErrorCode,
) error {
	t.Helper()
	failure, err := planescapeprovider.NewProviderFailure(
		planescapeprovider.ProviderFailureInput{
			Code:          code,
			ProviderID:    "planescape-linux-test",
			ProviderEpoch: 7,
			RetryFrom:     planescapeprovider.TransitionDiscover,
			CanonicalHash: planescapeProductHash("9"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return failure
}

func planescapeProductHash(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func requirePlanescapeProductErrorClass(
	t *testing.T,
	err error,
	want planescapeprovider.ErrorClass,
) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want class %s", want)
	}
	var productError *planescapeProductError
	if !errors.As(err, &productError) {
		t.Fatalf("error %T = %v, want planescapeProductError", err, err)
	}
	if got := productError.Class(); got != want {
		t.Fatalf("error class = %s, want %s", got, want)
	}
}
