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

	"github.com/spf13/cobra"
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
			_, err := runSessionStartupWithExecutionProvider(
				context.Background(),
				sessionConfig{ExecutionProvider: configmodel.ExecutionProviderPlanescape},
				testPlanescapeProductInvocation(
					t,
					"exec",
					[]string{"/usr/bin/true"},
					"request-failure",
				),
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
	_, startupErr := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{ExecutionProvider: configmodel.ExecutionProviderPlanescape},
		testPlanescapeProductInvocation(
			t,
			"exec",
			[]string{"/usr/bin/true"},
			"request-retain-plan",
		),
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

func TestConfiguredPlanescapeProviderRejectsWrongInvocationBeforeDialOrEffect(
	t *testing.T,
) {
	capabilities, plan, admission := planescapeProductAdmissionFixtures(t)
	requested := testPlanescapeProductInvocation(
		t,
		"exec",
		[]string{"/usr/bin/printf", "requested-argument-secret"},
		"requested-session-secret",
	)
	cases := []struct {
		name  string
		bound planescapeProductInvocation
	}{
		{
			name: "command",
			bound: testPlanescapeProductInvocation(
				t,
				"shell",
				[]string{"/usr/bin/printf", "requested-argument-secret"},
				"requested-session-secret",
			),
		},
		{
			name: "forwarded args",
			bound: testPlanescapeProductInvocation(
				t,
				"exec",
				[]string{"/usr/bin/printf", "different-argument-secret"},
				"requested-session-secret",
			),
		},
		{
			name: "session request",
			bound: testPlanescapeProductInvocation(
				t,
				"exec",
				[]string{"/usr/bin/printf", "requested-argument-secret"},
				"different-session-secret",
			),
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			endpoint := &planescapeProductEndpointFake{
				capabilities: capabilities,
				admission:    admission,
			}
			source := &planescapeProductCompiledPlanSourceFake{
				plan:       plan,
				invocation: &test.bound,
			}
			operations := &planescapeProductOperationSourceFake{}
			localStarts := 0
			_, err := runSessionStartupWithExecutionProvider(
				context.Background(),
				sessionConfig{
					ExecutionProvider: configmodel.ExecutionProviderPlanescape,
				},
				requested,
				planescapeProductDependencies{
					Endpoint:           endpoint,
					CompiledPlanSource: source,
					OperationSource:    operations,
					CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
				},
				func() error {
					localStarts++
					return nil
				},
			)
			requirePlanescapeProductErrorClass(
				t,
				err,
				planescapeprovider.ErrorConflict,
			)
			if localStarts != 0 ||
				endpoint.discoverCalls != 0 ||
				endpoint.admitCalls != 0 ||
				len(endpoint.operations) != 0 ||
				operations.toolCalls != 0 ||
				operations.quiescenceCalls != 0 {
				t.Fatalf(
					"wrong invocation reached fallback/dial/effect: local=%d discover=%d admit=%d provider_ops=%d source_ops=%d/%d",
					localStarts,
					endpoint.discoverCalls,
					endpoint.admitCalls,
					len(endpoint.operations),
					operations.toolCalls,
					operations.quiescenceCalls,
				)
			}
			for _, secret := range []string{
				"requested-argument-secret",
				"different-argument-secret",
				"requested-session-secret",
				"different-session-secret",
			} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("product error leaked invocation material: %v", err)
				}
			}
			if len(source.received) != 1 ||
				!source.received[0].matches(requested) {
				t.Fatal("compiled-plan source did not receive the exact invocation")
			}
		})
	}
}

func TestConfiguredPlanescapeProviderRunsExactClosedLifecycleWithoutLocalFallback(
	t *testing.T,
) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	capabilities, plan, admission := planescapeProductAdmissionFixtures(t)
	toolInput := newPlanescapeProductOperationInput(
		t,
		"tool-1",
		planescapeprovider.OperationTool,
		"tool-nonce",
		"a",
	)
	quiescenceInput := newPlanescapeProductOperationInput(
		t,
		"pause-1",
		planescapeprovider.OperationPause,
		"pause-nonce",
		"c",
	)
	toolResult := newPlanescapeProductToolResult(t, admission.SessionID().String())
	quiescence := newPlanescapeProductQuiescence(t, admission.SessionID().String())
	freezeAck := newPlanescapeProductFreezeAck(t, admission.SessionID().String())
	closeout := newPlanescapeProductCloseout(t, admission.SessionID().String())
	endpoint := &planescapeProductEndpointFake{
		capabilities: capabilities,
		admission:    admission,
		freezeAck:    freezeAck,
		responses: []planescapeprovider.OperationResponse{
			toolResult,
			quiescence,
			closeout,
		},
	}
	operations := &planescapeProductOperationSourceFake{
		tool:       toolInput,
		quiescence: quiescenceInput,
	}
	terminal := &planescapeProductTerminalSourceFake{
		freezeInput: newPlanescapeProductFreezeInput(t),
		closeout:    newPlanescapeProductCloseoutIntentForTest(t),
	}
	invocation := testPlanescapeProductInvocation(
		t,
		"exec",
		[]string{"/usr/bin/true"},
		"request-lifecycle",
	)
	checkpointRoot := filepath.Join(t.TempDir(), "checkpoints")
	localStarts := 0
	lifecycle, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{ExecutionProvider: configmodel.ExecutionProviderPlanescape},
		invocation,
		planescapeProductDependencies{
			Endpoint:           endpoint,
			CompiledPlanSource: &planescapeProductCompiledPlanSourceFake{plan: plan},
			OperationSource:    operations,
			TerminalSource:     terminal,
			CheckpointRoot:     checkpointRoot,
			Now:                func() time.Time { return now },
		},
		func() error {
			localStarts++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if lifecycle == nil ||
		lifecycle.Binding().PlanHash() != plan.CanonicalHash() ||
		lifecycle.Binding().SessionID() != admission.SessionID() {
		t.Fatal("successful provider lifecycle did not return exact completion")
	}
	if localStarts != 0 {
		t.Fatalf("local startup calls = %d, want 0", localStarts)
	}
	if endpoint.discoverCalls != 1 || endpoint.admitCalls != 1 {
		t.Fatalf(
			"discovery/admission calls = %d/%d, want 1/1",
			endpoint.discoverCalls,
			endpoint.admitCalls,
		)
	}
	if operations.toolCalls != 1 || operations.quiescenceCalls != 1 {
		t.Fatalf(
			"operation source calls = %d/%d, want 1/1",
			operations.toolCalls,
			operations.quiescenceCalls,
		)
	}
	if terminal.freezeCalls != 1 || terminal.closeoutCalls != 1 {
		t.Fatalf(
			"terminal source calls = %d/%d, want 1/1",
			terminal.freezeCalls,
			terminal.closeoutCalls,
		)
	}
	if len(endpoint.freezes) != 1 || len(endpoint.operations) != 3 {
		t.Fatalf(
			"provider terminal calls = freezes:%d operations:%d, want 1/3",
			len(endpoint.freezes),
			len(endpoint.operations),
		)
	}
	for index, operation := range endpoint.operations {
		wantKind := planescapeprovider.OperationTool
		switch index {
		case 1:
			wantKind = planescapeprovider.OperationPause
		case 2:
			wantKind = planescapeprovider.OperationCloseout
		}
		if operation.SessionID() != admission.SessionID() ||
			operation.PlanHash() != plan.CanonicalHash() ||
			operation.Sequence().Uint64() != uint64(index+1) ||
			operation.Kind() != wantKind {
			t.Fatalf("operation %d lost an exact binding: %+v", index, operation)
		}
	}
	if operations.toolResult.CanonicalHash() != toolResult.CanonicalHash() {
		t.Fatal("quiescence source did not receive the exact tool result")
	}
	wantBackend := endpoint.BackendBinding()
	for index, binding := range operations.bindings {
		if binding.PlanHash() != plan.CanonicalHash() ||
			binding.SessionID() != admission.SessionID() ||
			binding.Backend() != wantBackend ||
			!binding.Invocation().matches(invocation) {
			t.Fatalf("source binding %d = %+v, want exact plan/session/backend", index, binding)
		}
	}
	if !terminal.quiesced.valid() ||
		!terminal.quiesced.Binding().Invocation().matches(invocation) ||
		terminal.quiesced.Quiescence().CanonicalHash() !=
			quiescence.CanonicalHash() {
		t.Fatal("freeze source did not receive exact quiesced invocation state")
	}
	if !terminal.frozen.valid() ||
		!terminal.frozen.Binding().Invocation().matches(invocation) ||
		terminal.frozen.Freeze().CanonicalHash() != freezeAck.CanonicalHash() {
		t.Fatal("closeout source did not receive exact frozen invocation state")
	}
	if lifecycle.Freeze().CanonicalHash() != freezeAck.CanonicalHash() ||
		lifecycle.Closeout().CanonicalHash() != closeout.CanonicalHash() {
		t.Fatal("terminal result did not retain exact provider records")
	}

	store, err := planescapeprovider.NewFileStore(checkpointRoot)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, ok, err := store.Load(context.Background(), admission.SessionID().String())
	if err != nil || !ok {
		t.Fatalf("load lifecycle checkpoint = %v, %v", ok, err)
	}
	var durable struct {
		Schema                string `json:"schema"`
		PlanHash              string `json:"plan_hash"`
		BackendIdentitySHA256 string `json:"backend_identity_sha256"`
		Phase                 string `json:"phase"`
		Operations            []struct {
			Kind planescapeprovider.OperationKind `json:"kind"`
		} `json:"operations"`
		Evidence struct {
			Artifacts         []string `json:"artifacts"`
			OperationEvidence []string `json:"operation_evidence"`
			ResourceEvidence  string   `json:"resource_evidence"`
			LogicalEvidence   string   `json:"logical_evidence"`
			NativeExtension   string   `json:"native_extension_hash"`
		} `json:"evidence"`
		Freeze *struct {
			FreezeID string `json:"freeze_id"`
			Done     bool   `json:"done"`
		} `json:"freeze"`
	}
	if err := json.Unmarshal(checkpoint, &durable); err != nil {
		t.Fatal(err)
	}
	if durable.Schema != "hazmat.planescapeprovider.checkpoint.v2" ||
		durable.PlanHash != plan.CanonicalHash().String() ||
		durable.BackendIdentitySHA256 != wantBackend.IdentitySHA256().String() ||
		durable.Phase != "closed" ||
		len(durable.Operations) != 3 ||
		durable.Operations[0].Kind != planescapeprovider.OperationTool ||
		durable.Operations[1].Kind != planescapeprovider.OperationPause ||
		durable.Operations[2].Kind != planescapeprovider.OperationCloseout ||
		durable.Freeze == nil ||
		durable.Freeze.FreezeID != freezeAck.FreezeID().String() ||
		!durable.Freeze.Done ||
		len(durable.Evidence.Artifacts) != 1 ||
		len(durable.Evidence.OperationEvidence) != 1 ||
		durable.Evidence.ResourceEvidence != quiescence.ResourceEvidenceHash().String() ||
		durable.Evidence.LogicalEvidence != closeout.LogicalEvidenceHash().String() ||
		durable.Evidence.NativeExtension != closeout.NativeExtensionHash().String() {
		t.Fatalf("durable lifecycle evidence lost an exact binding: %+v", durable)
	}
}

func TestConfiguredPlanescapeCommandsCompleteExternallyWithoutNativeRunner(
	t *testing.T,
) {
	savedConfigPath := configFilePath
	savedDependencies := planescapeProductDependenciesForSession
	savedRunner := runAgentSeatbeltScriptWithPlan
	configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	t.Cleanup(func() {
		configFilePath = savedConfigPath
		planescapeProductDependenciesForSession = savedDependencies
		runAgentSeatbeltScriptWithPlan = savedRunner
	})
	if err := runConfigSet("session.execution_provider", "planescape"); err != nil {
		t.Fatal(err)
	}

	nativeRunnerCalls := 0
	runAgentSeatbeltScriptWithPlan = func(
		sessionConfig,
		sessionBackendPlan,
		sessionLaunchUI,
		string,
		...string,
	) error {
		nativeRunnerCalls++
		return nil
	}

	cases := []struct {
		name    string
		command func() *cobra.Command
		args    []string
	}{
		{name: "shell", command: newShellCmd},
		{name: "exec", command: newExecCmd, args: []string{"/usr/bin/true"}},
		{name: "claude", command: newClaudeCmd},
		{name: "opencode", command: newOpenCodeCmd},
		{name: "codex", command: newCodexCmd},
		{name: "codex app server", command: newCodexAppServerCmd},
		{name: "codex app shim", command: newCodexAppShimCmd},
		{name: "antigravity", command: newAntigravityCmd},
		{name: "cursor agent", command: newCursorAgentCmd},
		{name: "hermes", command: newHermesCmd},
		{name: "pi", command: newPiCmd},
		{name: "qwen", command: newQwenCmd},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
			capabilities, plan, admission := planescapeProductAdmissionFixtures(t)
			endpoint := &planescapeProductEndpointFake{
				capabilities: capabilities,
				admission:    admission,
				freezeAck: newPlanescapeProductFreezeAck(
					t,
					admission.SessionID().String(),
				),
				responses: []planescapeprovider.OperationResponse{
					newPlanescapeProductToolResult(t, admission.SessionID().String()),
					newPlanescapeProductQuiescence(t, admission.SessionID().String()),
					newPlanescapeProductCloseout(t, admission.SessionID().String()),
				},
			}
			operations := &planescapeProductOperationSourceFake{
				tool: newPlanescapeProductOperationInput(
					t,
					"tool-1",
					planescapeprovider.OperationTool,
					"tool-nonce",
					"a",
				),
				quiescence: newPlanescapeProductOperationInput(
					t,
					"pause-1",
					planescapeprovider.OperationPause,
					"pause-nonce",
					"c",
				),
			}
			planescapeProductDependenciesForSession = func() (
				planescapeProductDependencies,
				error,
			) {
				return planescapeProductDependencies{
					InvocationSource: planescapeProductInvocationSourceFake{
						sessionRequestID: "configured-command-session",
					},
					Endpoint:           endpoint,
					CompiledPlanSource: &planescapeProductCompiledPlanSourceFake{plan: plan},
					OperationSource:    operations,
					TerminalSource: &planescapeProductTerminalSourceFake{
						freezeInput: newPlanescapeProductFreezeInput(t),
						closeout:    newPlanescapeProductCloseoutIntentForTest(t),
					},
					CheckpointRoot: filepath.Join(t.TempDir(), "checkpoints"),
					Now:            func() time.Time { return now },
				}, nil
			}

			command := test.command()
			args := test.args
			if args == nil {
				args = []string{}
			}
			command.SetArgs(args)
			command.SilenceErrors = true
			command.SilenceUsage = true
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if nativeRunnerCalls != 0 {
				t.Fatalf("native runner calls = %d, want 0", nativeRunnerCalls)
			}
			if len(endpoint.freezes) != 1 ||
				len(endpoint.operations) != 3 ||
				endpoint.operations[0].Kind() != planescapeprovider.OperationTool ||
				endpoint.operations[1].Kind() != planescapeprovider.OperationPause ||
				endpoint.operations[2].Kind() != planescapeprovider.OperationCloseout {
				t.Fatalf("external lifecycle = %+v", endpoint.operations)
			}
		})
	}
}

func TestConfiguredPlanescapeProviderBackendDriftFailsBeforeToolWithoutFallback(
	t *testing.T,
) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	capabilities, plan, admission := planescapeProductAdmissionFixtures(t)
	endpoint := &planescapeProductEndpointFake{
		capabilities: capabilities,
		admission:    admission,
		responses: []planescapeprovider.OperationResponse{
			newPlanescapeProductToolResult(t, admission.SessionID().String()),
		},
	}
	operations := &planescapeProductOperationSourceFake{
		tool: newPlanescapeProductOperationInput(
			t,
			"tool-1",
			planescapeprovider.OperationTool,
			"tool-nonce",
			"a",
		),
		beforeTool: func() {
			endpoint.backend = newPlanescapeProductBackendBinding(t, "f")
		},
	}
	localStarts := 0
	_, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{ExecutionProvider: configmodel.ExecutionProviderPlanescape},
		testPlanescapeProductInvocation(
			t,
			"exec",
			[]string{"/usr/bin/true"},
			"request-backend-drift",
		),
		planescapeProductDependencies{
			Endpoint:           endpoint,
			CompiledPlanSource: &planescapeProductCompiledPlanSourceFake{plan: plan},
			OperationSource:    operations,
			CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
			Now:                func() time.Time { return now },
		},
		func() error {
			localStarts++
			return nil
		},
	)
	requirePlanescapeProductErrorClass(t, err, planescapeprovider.ErrorConflict)
	if localStarts != 0 || len(endpoint.operations) != 0 ||
		operations.quiescenceCalls != 0 {
		t.Fatalf(
			"backend drift reached fallback/effect/quiescence: local=%d operations=%d quiescence=%d",
			localStarts,
			len(endpoint.operations),
			operations.quiescenceCalls,
		)
	}
}

func TestConfiguredPlanescapeProviderToolFailureDoesNotFallbackOrQuiesce(
	t *testing.T,
) {
	const diagnostic = "provider-tool-diagnostic-secret"
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	capabilities, plan, admission := planescapeProductAdmissionFixtures(t)
	endpoint := &planescapeProductEndpointFake{
		capabilities: capabilities,
		admission:    admission,
		operationErr: errors.New(diagnostic),
	}
	operations := &planescapeProductOperationSourceFake{
		tool: newPlanescapeProductOperationInput(
			t,
			"tool-1",
			planescapeprovider.OperationTool,
			"tool-nonce",
			"a",
		),
	}
	localStarts := 0
	_, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{ExecutionProvider: configmodel.ExecutionProviderPlanescape},
		testPlanescapeProductInvocation(
			t,
			"exec",
			[]string{"/usr/bin/true"},
			"request-tool-failure",
		),
		planescapeProductDependencies{
			Endpoint:           endpoint,
			CompiledPlanSource: &planescapeProductCompiledPlanSourceFake{plan: plan},
			OperationSource:    operations,
			CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
			Now:                func() time.Time { return now },
		},
		func() error {
			localStarts++
			return nil
		},
	)
	requirePlanescapeProductErrorClass(t, err, planescapeprovider.ErrorUnavailable)
	if strings.Contains(err.Error(), diagnostic) {
		t.Fatalf("product error leaked provider diagnostic: %v", err)
	}
	if localStarts != 0 || len(endpoint.operations) != 1 ||
		operations.quiescenceCalls != 0 {
		t.Fatalf(
			"tool failure reached fallback or quiescence: local=%d operations=%d quiescence=%d",
			localStarts,
			len(endpoint.operations),
			operations.quiescenceCalls,
		)
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
	lifecycle, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{ExecutionProvider: configmodel.ExecutionProviderLocal},
		planescapeProductInvocation{},
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
	if lifecycle != nil {
		t.Fatal("local startup returned an external lifecycle result")
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
	if _, err := defaultPlanescapeProductDependencies(); err == nil {
		t.Fatal("Planescape selection without protected endpoint config succeeded")
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
	dependencies, err := defaultPlanescapeProductDependencies()
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(configDir, "planescape-provider-checkpoints")
	if dependencies.CheckpointRoot != wantRoot {
		t.Fatalf("checkpoint root = %q, want %q", dependencies.CheckpointRoot, wantRoot)
	}
}

type planescapeProductEndpointFake struct {
	backend         planescapeprovider.BackendIdentityBinding
	capabilities    planescapeprovider.ProviderCapabilities
	admission       planescapeprovider.SessionAdmission
	freezeAck       planescapeprovider.FreezeAck
	cancellationAck planescapeprovider.CancellationAck
	discoverErr     error
	admitErr        error
	operationErr    error
	operationErrors map[planescapeprovider.OperationKind]error
	freezeErr       error
	cancellationErr error
	discoverCalls   int
	admitCalls      int
	cancelCalls     int
	admittedPlans   []planescapeprovider.CompiledContainmentPlan
	operations      []planescapeprovider.AgentOperation
	freezes         []planescapeprovider.Freeze
	cancellations   []planescapeprovider.Cancellation
	responses       []planescapeprovider.OperationResponse
	afterAdmit      func(*planescapeProductEndpointFake)
}

func (e *planescapeProductEndpointFake) BackendBinding() planescapeprovider.BackendIdentityBinding {
	if e != nil && e.backend.IdentitySHA256().String() != "" {
		return e.backend
	}
	value, _ := planescapeprovider.NewBackendIdentityBinding(
		planescapeprovider.BackendIdentityBindingInput{
			IdentitySHA256: planescapeProductHash("e"),
			ProviderEpoch:  7,
		},
	)
	return value
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
	admission, err := e.admission, e.admitErr
	if e.afterAdmit != nil {
		e.afterAdmit(e)
	}
	return admission, err
}

func (e *planescapeProductEndpointFake) Operate(
	_ context.Context,
	operation planescapeprovider.AgentOperation,
) (planescapeprovider.OperationResponse, error) {
	e.operations = append(e.operations, operation)
	if e.operationErr != nil {
		return nil, e.operationErr
	}
	if err := e.operationErrors[operation.Kind()]; err != nil {
		return nil, err
	}
	if len(e.responses) == 0 {
		return nil, errors.New("unexpected Operate call")
	}
	response := e.responses[0]
	e.responses = e.responses[1:]
	return response, nil
}

func (e *planescapeProductEndpointFake) Freeze(
	_ context.Context,
	freeze planescapeprovider.Freeze,
) (planescapeprovider.FreezeAck, error) {
	e.freezes = append(e.freezes, freeze)
	if e.freezeErr != nil {
		return planescapeprovider.FreezeAck{}, e.freezeErr
	}
	return e.freezeAck, nil
}

func (e *planescapeProductEndpointFake) Cancel(
	_ context.Context,
	cancellation planescapeprovider.Cancellation,
) (planescapeprovider.CancellationAck, error) {
	e.cancelCalls++
	e.cancellations = append(e.cancellations, cancellation)
	if e.cancellationErr != nil {
		return planescapeprovider.CancellationAck{}, e.cancellationErr
	}
	if !validPlanescapeProductCancellationAck(e.cancellationAck) {
		return planescapeprovider.CancellationAck{},
			errors.New("unexpected Cancel call")
	}
	return e.cancellationAck, nil
}

type planescapeProductCompiledPlanSourceFake struct {
	plan       planescapeprovider.CompiledContainmentPlan
	err        error
	invocation *planescapeProductInvocation
	received   []planescapeProductInvocation
	calls      int
}

type planescapeProductInvocationSourceFake struct {
	sessionRequestID string
	err              error
}

type planescapeProductOperationSourceFake struct {
	tool             planescapeprovider.OperationInput
	quiescence       planescapeprovider.OperationInput
	cancellation     planescapeprovider.CancellationInput
	toolErr          error
	quiescenceErr    error
	toolCalls        int
	quiescenceCalls  int
	bindings         []planescapeProductBinding
	toolResult       planescapeprovider.OperationResult
	beforeTool       func()
	beforeQuiescence func()
}

type planescapeProductTerminalSourceFake struct {
	freezeInput    planescapeprovider.FreezeInput
	closeout       planescapeProductCloseoutIntent
	freezeErr      error
	closeoutErr    error
	freezeCalls    int
	closeoutCalls  int
	quiesced       planescapeProductQuiescedLifecycle
	frozen         planescapeProductFrozenLifecycle
	beforeFreeze   func()
	beforeCloseout func()
}

func (s *planescapeProductOperationSourceFake) ToolOperation(
	_ context.Context,
	binding planescapeProductBinding,
) (planescapeprovider.OperationInput, error) {
	s.toolCalls++
	s.bindings = append(s.bindings, binding)
	if s.beforeTool != nil {
		s.beforeTool()
	}
	return s.tool, s.toolErr
}

func (s *planescapeProductOperationSourceFake) PostToolIntent(
	_ context.Context,
	binding planescapeProductBinding,
	tool planescapeprovider.OperationResult,
) (planescapeProductPostToolIntent, error) {
	s.quiescenceCalls++
	s.bindings = append(s.bindings, binding)
	s.toolResult = tool
	if s.beforeQuiescence != nil {
		s.beforeQuiescence()
	}
	if s.quiescenceErr != nil {
		return nil, s.quiescenceErr
	}
	if validPlanescapeProductCancellationInput(s.cancellation) {
		intent, err := newPlanescapeProductCancellationIntent(s.cancellation)
		if err != nil {
			return nil, err
		}
		return intent, nil
	}
	intent, err := newPlanescapeProductPauseIntent(s.quiescence)
	if err != nil {
		return nil, err
	}
	return intent, nil
}

func (s *planescapeProductTerminalSourceFake) FreezeInput(
	_ context.Context,
	quiesced planescapeProductQuiescedLifecycle,
) (planescapeprovider.FreezeInput, error) {
	s.freezeCalls++
	s.quiesced = quiesced
	if s.beforeFreeze != nil {
		s.beforeFreeze()
	}
	return s.freezeInput, s.freezeErr
}

func (s *planescapeProductTerminalSourceFake) CloseoutIntent(
	_ context.Context,
	frozen planescapeProductFrozenLifecycle,
) (planescapeProductCloseoutIntent, error) {
	s.closeoutCalls++
	s.frozen = frozen
	if s.beforeCloseout != nil {
		s.beforeCloseout()
	}
	return s.closeout, s.closeoutErr
}

func (s *planescapeProductCompiledPlanSourceFake) CompiledContainmentPlan(
	_ context.Context,
	invocation planescapeProductInvocation,
) (planescapeProductCompiledPlanArtifact, error) {
	s.calls++
	s.received = append(s.received, invocation.clone())
	bound := invocation
	if s.invocation != nil {
		bound = s.invocation.clone()
	}
	return planescapeProductCompiledPlanArtifact{
		plan:       s.plan,
		invocation: bound,
	}, s.err
}

func (s planescapeProductInvocationSourceFake) Invocation(
	commandName string,
	forwardedArgs []string,
) (planescapeProductInvocation, error) {
	if s.err != nil {
		return planescapeProductInvocation{}, s.err
	}
	return newPlanescapeProductInvocation(
		commandName,
		forwardedArgs,
		s.sessionRequestID,
	)
}

func testPlanescapeProductInvocation(
	t *testing.T,
	commandName string,
	forwardedArgs []string,
	sessionRequestID string,
) planescapeProductInvocation {
	t.Helper()
	value, err := newPlanescapeProductInvocation(
		commandName,
		forwardedArgs,
		sessionRequestID,
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
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

func newPlanescapeProductBackendBinding(
	t *testing.T,
	character string,
) planescapeprovider.BackendIdentityBinding {
	t.Helper()
	value, err := planescapeprovider.NewBackendIdentityBinding(
		planescapeprovider.BackendIdentityBindingInput{
			IdentitySHA256: planescapeProductHash(character),
			ProviderEpoch:  7,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newPlanescapeProductOperationInput(
	t *testing.T,
	operationID string,
	kind planescapeprovider.OperationKind,
	nonce string,
	payloadCharacter string,
) planescapeprovider.OperationInput {
	t.Helper()
	var normalizedRecord []byte
	if kind == planescapeprovider.OperationTool {
		normalizedRecord = []byte("normalized:" + operationID)
	}
	value, err := planescapeprovider.NewOperationInput(
		planescapeprovider.OperationInputValues{
			OperationID:      operationID,
			Kind:             kind,
			Nonce:            nonce,
			PayloadHash:      planescapeProductHash(payloadCharacter),
			NormalizedRecord: normalizedRecord,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newPlanescapeProductToolResult(
	t *testing.T,
	sessionID string,
) planescapeprovider.OperationResult {
	t.Helper()
	value, err := planescapeprovider.NewOperationResult(
		planescapeprovider.OperationResultInput{
			SessionID:     sessionID,
			OperationID:   "tool-1",
			Sequence:      1,
			ResultKind:    planescapeprovider.ResultCompleted,
			ArtifactHash:  planescapeProductHash("4"),
			EvidenceHash:  planescapeProductHash("5"),
			CanonicalHash: planescapeProductHash("6"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newPlanescapeProductQuiescence(
	t *testing.T,
	sessionID string,
) planescapeprovider.Quiescence {
	t.Helper()
	value, err := planescapeprovider.NewQuiescence(
		planescapeprovider.QuiescenceInput{
			SessionID:            sessionID,
			QuiescenceHash:       planescapeProductHash("7"),
			ResourceEvidenceHash: planescapeProductHash("8"),
			CanonicalHash:        planescapeProductHash("9"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newPlanescapeProductFreezeInput(
	t *testing.T,
) planescapeprovider.FreezeInput {
	t.Helper()
	value, err := planescapeprovider.NewFreezeInput(
		planescapeprovider.FreezeInputValues{
			FreezeID: "freeze-1",
			Nonce:    "freeze-nonce",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newPlanescapeProductFreezeAck(
	t *testing.T,
	sessionID string,
) planescapeprovider.FreezeAck {
	t.Helper()
	value, err := planescapeprovider.NewFreezeAck(
		planescapeprovider.FreezeAckInput{
			SessionID:      sessionID,
			FreezeID:       "freeze-1",
			QuiescenceHash: planescapeProductHash("7"),
			FrozenAt:       time.Date(2026, 7, 25, 12, 1, 0, 0, time.UTC),
			CanonicalHash:  planescapeProductHash("b"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newPlanescapeProductCloseoutIntentForTest(
	t *testing.T,
) planescapeProductCloseoutIntent {
	t.Helper()
	operation := newPlanescapeProductOperationInput(
		t,
		"closeout-operation-1",
		planescapeprovider.OperationCloseout,
		"closeout-nonce",
		"d",
	)
	value, err := newPlanescapeProductCloseoutIntent(
		operation,
		"closeout-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newPlanescapeProductCloseout(
	t *testing.T,
	sessionID string,
) planescapeprovider.Closeout {
	t.Helper()
	value, err := planescapeprovider.NewCloseout(
		planescapeprovider.CloseoutInput{
			SessionID:           sessionID,
			CloseoutID:          "closeout-1",
			TerminalOutcome:     planescapeprovider.OutcomeSucceeded,
			QuiescenceHash:      planescapeProductHash("7"),
			LogicalEvidenceHash: planescapeProductHash("c"),
			NativeExtensionHash: planescapeProductHash("d"),
			CanonicalHash:       planescapeProductHash("e"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
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
