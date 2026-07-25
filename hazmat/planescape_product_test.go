package hazmat

import (
	"bytes"
	"context"
	"encoding/json"
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
	capabilities, plan, admission := planescapeProductAdmissionFixtures(t)
	cases := []struct {
		name              string
		sourceAbsent      bool
		sourcePlan        planescapeprovider.CompiledContainmentPlan
		sourceErr         error
		discoverErr       error
		admission         planescapeprovider.SessionAdmission
		admitErr          error
		want              planescapeprovider.ErrorClass
		wantMessage       string
		wantSourceCalls   int
		wantDiscoverCalls int
		wantAdmitCalls    int
		wantAdmission     bool
		secret            string
	}{
		{
			name:              "compiled plan source is absent",
			sourceAbsent:      true,
			admission:         admission,
			want:              planescapeprovider.ErrorUnavailable,
			wantMessage:       "configured Planescape provider failed closed: unavailable",
			wantSourceCalls:   0,
			wantDiscoverCalls: 0,
		},
		{
			name:              "compiled plan source fails",
			sourceErr:         errors.New("compiled-plan-source-secret"),
			admission:         admission,
			want:              planescapeprovider.ErrorUnavailable,
			wantMessage:       "configured Planescape provider failed closed: unavailable",
			wantSourceCalls:   1,
			wantDiscoverCalls: 0,
			secret:            "compiled-plan-source-secret",
		},
		{
			name:              "compiled plan source returns an invalid zero value",
			admission:         admission,
			want:              planescapeprovider.ErrorInvalid,
			wantMessage:       "configured Planescape provider failed closed: invalid",
			wantSourceCalls:   1,
			wantDiscoverCalls: 0,
		},
		{
			name:       "discovery is unsupported",
			sourcePlan: plan,
			discoverErr: newPlanescapeProviderFailureForTest(
				t,
				capabilities,
				planescapeprovider.ProviderErrorUnsupported,
				planescapeprovider.TransitionDiscover,
			),
			admission:         admission,
			want:              planescapeprovider.ErrorUnsupported,
			wantMessage:       "configured Planescape provider failed closed: unsupported",
			wantSourceCalls:   1,
			wantDiscoverCalls: 1,
		},
		{
			name:              "provider dies during discovery",
			sourcePlan:        plan,
			discoverErr:       errors.New("provider-discovery-death-secret"),
			admission:         admission,
			want:              planescapeprovider.ErrorUnavailable,
			wantMessage:       "configured Planescape provider failed closed: unavailable",
			wantSourceCalls:   1,
			wantDiscoverCalls: 1,
			secret:            "provider-discovery-death-secret",
		},
		{
			name:       "admission is unavailable",
			sourcePlan: plan,
			admission:  admission,
			admitErr: newPlanescapeProviderFailureForTest(
				t,
				capabilities,
				planescapeprovider.ProviderErrorUnavailable,
				planescapeprovider.TransitionAdmit,
			),
			want:              planescapeprovider.ErrorUnavailable,
			wantMessage:       "configured Planescape provider failed closed: unavailable",
			wantSourceCalls:   1,
			wantDiscoverCalls: 1,
			wantAdmitCalls:    1,
		},
		{
			name:              "provider dies during admission",
			sourcePlan:        plan,
			admission:         admission,
			admitErr:          errors.New("provider-admission-death-secret"),
			want:              planescapeprovider.ErrorUnavailable,
			wantMessage:       "configured Planescape provider failed closed: unavailable",
			wantSourceCalls:   1,
			wantDiscoverCalls: 1,
			wantAdmitCalls:    1,
			secret:            "provider-admission-death-secret",
		},
		{
			name:              "provider returns invalid admission",
			sourcePlan:        plan,
			want:              planescapeprovider.ErrorConflict,
			wantMessage:       "configured Planescape provider failed closed: conflict",
			wantSourceCalls:   1,
			wantDiscoverCalls: 1,
			wantAdmitCalls:    1,
		},
		{
			name:              "exact admission succeeds but lifecycle is pending",
			sourcePlan:        plan,
			admission:         admission,
			want:              planescapeprovider.ErrorUnsupported,
			wantMessage:       "configured Planescape provider admitted the session, but product lifecycle execution is unavailable; local execution is disabled",
			wantSourceCalls:   1,
			wantDiscoverCalls: 1,
			wantAdmitCalls:    1,
			wantAdmission:     true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var source *planescapeProductCompiledPlanSourceFake
			var compiledPlanSource planescapeProductCompiledPlanSource
			if !test.sourceAbsent {
				source = &planescapeProductCompiledPlanSourceFake{
					plan: test.sourcePlan,
					err:  test.sourceErr,
				}
				compiledPlanSource = source
			}
			endpoint := &planescapeProductEndpointFake{
				capabilities: capabilities,
				admission:    test.admission,
				discoverErr:  test.discoverErr,
				admitErr:     test.admitErr,
			}
			localStarts := 0
			err := runSessionStartupWithExecutionProvider(
				context.Background(),
				sessionConfig{ExecutionProvider: configmodel.ExecutionProviderPlanescape},
				planescapeProductDependencies{
					Endpoint:           endpoint,
					CompiledPlanSource: compiledPlanSource,
					CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
					Now:                func() time.Time { return now },
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
			if source != nil && source.calls != test.wantSourceCalls {
				t.Fatalf("compiled plan source calls = %d, want %d", source.calls, test.wantSourceCalls)
			}
			if endpoint.discoverCalls != test.wantDiscoverCalls {
				t.Fatalf("Discover calls = %d, want %d", endpoint.discoverCalls, test.wantDiscoverCalls)
			}
			if endpoint.admitCalls != test.wantAdmitCalls {
				t.Fatalf("Admit calls = %d, want %d", endpoint.admitCalls, test.wantAdmitCalls)
			}
			if err.Error() != test.wantMessage {
				t.Fatalf("error = %q, want %q", err, test.wantMessage)
			}
			if test.secret != "" && strings.Contains(err.Error(), test.secret) {
				t.Fatalf("product error leaked endpoint diagnostic: %v", err)
			}
			var productError *planescapeProductError
			if !errors.As(err, &productError) {
				t.Fatalf("error %T = %v, want planescapeProductError", err, err)
			}
			_, retained := productError.admissionState()
			if retained != test.wantAdmission {
				t.Fatalf("retained admission = %v, want %v", retained, test.wantAdmission)
			}
		})
	}
}

func TestConfiguredPlanescapeProviderTransmitsAndRetainsExactCompiledPlan(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	capabilities, plan, admission := planescapeProductAdmissionFixtures(t)
	source := &planescapeProductCompiledPlanSourceFake{plan: plan}
	endpoint := &planescapeProductEndpointFake{
		capabilities: capabilities,
		admission:    admission,
	}
	localStarts := 0
	startupErr := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{ExecutionProvider: configmodel.ExecutionProviderPlanescape},
		planescapeProductDependencies{
			Endpoint:           endpoint,
			CompiledPlanSource: source,
			CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
			Now:                func() time.Time { return now },
		},
		func() error {
			localStarts++
			return nil
		},
	)
	requirePlanescapeProductErrorClass(t, startupErr, planescapeprovider.ErrorUnsupported)
	if localStarts != 0 {
		t.Fatalf("local startup calls = %d, want 0", localStarts)
	}
	if source.calls != 1 {
		t.Fatalf("compiled plan source calls = %d, want 1", source.calls)
	}
	if len(endpoint.admittedPlans) != 1 {
		t.Fatalf("admitted plans = %d, want 1", len(endpoint.admittedPlans))
	}

	codec := planescapeprovider.ProviderV1FrameCodec{}
	wantPlan, err := codec.EncodeCompiledContainmentPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	gotPlan, err := codec.EncodeCompiledContainmentPlan(endpoint.admittedPlans[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotPlan, wantPlan) {
		t.Fatal("admission did not transmit the exact compiled plan")
	}

	var productError *planescapeProductError
	if !errors.As(startupErr, &productError) {
		t.Fatalf("error %T = %v, want planescapeProductError", startupErr, startupErr)
	}
	state, ok := productError.admissionState()
	if !ok {
		t.Fatal("successful admission did not retain typed lifecycle state")
	}
	retainedPlan, ok := state.Plan()
	if !ok {
		t.Fatal("retained admission has no valid compiled plan")
	}
	retainedPlanFrame, err := codec.EncodeCompiledContainmentPlan(retainedPlan)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retainedPlanFrame, wantPlan) {
		t.Fatal("retained admission substituted the compiled plan")
	}
	retainedSession, ok := state.Session()
	if !ok {
		t.Fatal("retained admission has no valid session")
	}
	sessionID, ok := retainedSession.ID()
	if !ok || sessionID != admission.SessionID() {
		t.Fatalf("retained session ID = %q, %v; want %q, true", sessionID.String(), ok, admission.SessionID().String())
	}
}

func TestUnconfiguredPlanescapeEndpointDoesNotAffectLocalStartup(t *testing.T) {
	endpoint := &planescapeProductEndpointFake{
		discoverErr: errors.New("must not be called"),
	}
	source := &planescapeProductCompiledPlanSourceFake{
		err: errors.New("must not be called"),
	}
	localStarts := 0
	err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{ExecutionProvider: configmodel.ExecutionProviderLocal},
		planescapeProductDependencies{
			Endpoint:           endpoint,
			CompiledPlanSource: source,
			CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
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
	if source.calls != 0 {
		t.Fatalf("compiled plan source calls = %d, want 0 without explicit provider configuration", source.calls)
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
	capabilities, plan, admission := planescapeProductAdmissionFixtures(t)
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
	admissionInput, err := planescapeprovider.NewAdmissionInput(plan)
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
	if len(endpoint.admittedPlans) != 1 ||
		endpoint.admittedPlans[0].CanonicalHash() != plan.CanonicalHash() {
		t.Fatal("product endpoint did not receive the exact compiled plan")
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
	admitErr      error
	discoverCalls int
	admitCalls    int
	admittedPlans []planescapeprovider.CompiledContainmentPlan
}

func (e *planescapeProductEndpointFake) Discover(context.Context) (planescapeprovider.ProviderCapabilities, error) {
	e.discoverCalls++
	return e.capabilities, e.discoverErr
}

func (e *planescapeProductEndpointFake) Admit(
	_ context.Context,
	plan planescapeprovider.CompiledContainmentPlan,
) (planescapeprovider.SessionAdmission, error) {
	e.admitCalls++
	e.admittedPlans = append(e.admittedPlans, plan)
	return e.admission, e.admitErr
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

type planescapeProductCompiledPlanSourceFake struct {
	plan  planescapeprovider.CompiledContainmentPlan
	err   error
	calls int
}

func (s *planescapeProductCompiledPlanSourceFake) CompiledContainmentPlan(
	context.Context,
) (planescapeprovider.CompiledContainmentPlan, error) {
	s.calls++
	return s.plan, s.err
}

func planescapeProductAdmissionFixtures(
	t *testing.T,
) (
	planescapeprovider.ProviderCapabilities,
	planescapeprovider.CompiledContainmentPlan,
	planescapeprovider.SessionAdmission,
) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(
		"planescapeprovider",
		"testdata",
		"planescape.provider.v1",
		"wire",
		"vectors.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Records []struct {
			Kind     string `json:"kind"`
			WireJSON string `json:"wire_json"`
		} `json:"records"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	record := func(kind string) []byte {
		t.Helper()
		for _, candidate := range vectors.Records {
			if candidate.Kind == kind {
				return []byte(candidate.WireJSON)
			}
		}
		t.Fatalf("provider-v1 vector %q is missing", kind)
		return nil
	}
	codec := planescapeprovider.ProviderV1FrameCodec{}
	capabilities, err := codec.DecodeCapabilities(record("provider_capabilities"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := codec.DecodeCompiledContainmentPlan(record("compiled_containment_plan"))
	if err != nil {
		t.Fatal(err)
	}
	admission, err := codec.DecodeAdmission(record("session_admission"))
	if err != nil {
		t.Fatal(err)
	}
	return capabilities, plan, admission
}

func newPlanescapeProviderFailureForTest(
	t *testing.T,
	capabilities planescapeprovider.ProviderCapabilities,
	code planescapeprovider.ProviderErrorCode,
	retryFrom planescapeprovider.Transition,
) error {
	t.Helper()
	failure, err := planescapeprovider.NewProviderFailure(
		planescapeprovider.ProviderFailureInput{
			Code:          code,
			ProviderID:    capabilities.ProviderID().String(),
			ProviderEpoch: capabilities.ProviderEpoch().Uint64(),
			RetryFrom:     retryFrom,
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
