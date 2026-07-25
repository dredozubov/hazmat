package hazmat

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hazmat/configmodel"
	"hazmat/planescapeprovider"
)

func TestConfiguredPlanescapeQuiescenceIsNotTerminalAndNeverFallsBack(
	t *testing.T,
) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	capabilities, plan, admission := planescapeProductAdmissionFixtures(t)
	endpoint := &planescapeProductEndpointFake{
		capabilities: capabilities,
		admission:    admission,
		responses: []planescapeprovider.OperationResponse{
			newPlanescapeProductToolResult(t, admission.SessionID().String()),
			newPlanescapeProductQuiescence(t, admission.SessionID().String()),
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
	localStarts := 0
	result, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{ExecutionProvider: configmodel.ExecutionProviderPlanescape},
		testPlanescapeProductInvocation(
			t,
			"exec",
			[]string{"/usr/bin/true"},
			"request-terminal-source-absent",
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
	requirePlanescapeProductErrorClass(
		t,
		err,
		planescapeprovider.ErrorUnsupported,
	)
	if result != nil {
		t.Fatal("quiescence returned a terminal product result")
	}
	const want = "configured Planescape provider reached quiescence, but terminal lifecycle execution is unavailable; local execution is disabled"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
	if localStarts != 0 ||
		len(endpoint.operations) != 2 ||
		len(endpoint.freezes) != 0 ||
		endpoint.cancelCalls != 0 {
		t.Fatalf(
			"non-terminal quiescence reached completion/fallback/terminal effect: local=%d operations=%d freezes=%d cancellations=%d",
			localStarts,
			len(endpoint.operations),
			len(endpoint.freezes),
			endpoint.cancelCalls,
		)
	}
}

func TestConfiguredPlanescapeTerminalFailuresNeverFallbackAndAreRedacted(
	t *testing.T,
) {
	const (
		sourceDiagnostic   = "rust-terminal-source-diagnostic-secret"
		providerDiagnostic = "terminal-provider-diagnostic-secret"
	)
	cases := []struct {
		name                   string
		mode                   string
		want                   planescapeprovider.ErrorClass
		wantFreezeSourceCalls  int
		wantProviderFreezes    int
		wantCloseoutSourceCall int
		wantProviderOperations int
	}{
		{
			name:                   "freeze source unavailable",
			mode:                   "freeze_source",
			want:                   planescapeprovider.ErrorUnavailable,
			wantFreezeSourceCalls:  1,
			wantProviderOperations: 2,
		},
		{
			name:                   "invalid freeze intent",
			mode:                   "invalid_freeze",
			want:                   planescapeprovider.ErrorInvalid,
			wantFreezeSourceCalls:  1,
			wantProviderOperations: 2,
		},
		{
			name:                   "provider dies during freeze",
			mode:                   "freeze_provider",
			want:                   planescapeprovider.ErrorUnavailable,
			wantFreezeSourceCalls:  1,
			wantProviderFreezes:    1,
			wantProviderOperations: 2,
		},
		{
			name:                   "closeout source unavailable",
			mode:                   "closeout_source",
			want:                   planescapeprovider.ErrorUnavailable,
			wantFreezeSourceCalls:  1,
			wantProviderFreezes:    1,
			wantCloseoutSourceCall: 1,
			wantProviderOperations: 2,
		},
		{
			name:                   "unrelated closeout intent",
			mode:                   "invalid_closeout",
			want:                   planescapeprovider.ErrorInvalid,
			wantFreezeSourceCalls:  1,
			wantProviderFreezes:    1,
			wantCloseoutSourceCall: 1,
			wantProviderOperations: 2,
		},
		{
			name:                   "provider dies during closeout",
			mode:                   "closeout_provider",
			want:                   planescapeprovider.ErrorUnavailable,
			wantFreezeSourceCalls:  1,
			wantProviderFreezes:    1,
			wantCloseoutSourceCall: 1,
			wantProviderOperations: 3,
		},
		{
			name:                   "failed terminal outcome is not success",
			mode:                   "failed_outcome",
			want:                   planescapeprovider.ErrorConflict,
			wantFreezeSourceCalls:  1,
			wantProviderFreezes:    1,
			wantCloseoutSourceCall: 1,
			wantProviderOperations: 3,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
			capabilities, plan, admission :=
				planescapeProductAdmissionFixtures(t)
			closeout := newPlanescapeProductCloseout(
				t,
				admission.SessionID().String(),
			)
			endpoint := &planescapeProductEndpointFake{
				capabilities: capabilities,
				admission:    admission,
				freezeAck: newPlanescapeProductFreezeAck(
					t,
					admission.SessionID().String(),
				),
				responses: []planescapeprovider.OperationResponse{
					newPlanescapeProductToolResult(
						t,
						admission.SessionID().String(),
					),
					newPlanescapeProductQuiescence(
						t,
						admission.SessionID().String(),
					),
					closeout,
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
			terminal := &planescapeProductTerminalSourceFake{
				freezeInput: newPlanescapeProductFreezeInput(t),
				closeout:    newPlanescapeProductCloseoutIntentForTest(t),
			}

			switch test.mode {
			case "freeze_source":
				terminal.freezeErr = errors.New(sourceDiagnostic)
			case "invalid_freeze":
				terminal.freezeInput = planescapeprovider.FreezeInput{}
			case "freeze_provider":
				endpoint.freezeErr = errors.New(providerDiagnostic)
			case "closeout_source":
				terminal.closeoutErr = errors.New(sourceDiagnostic)
			case "invalid_closeout":
				closeoutID, err := planescapeprovider.NewIdentifier("closeout-1")
				if err != nil {
					t.Fatal(err)
				}
				terminal.closeout = planescapeProductCloseoutIntent{
					operation: newPlanescapeProductOperationInput(
						t,
						"unrelated-tool",
						planescapeprovider.OperationTool,
						"unrelated-nonce",
						"f",
					),
					closeoutID: closeoutID,
				}
			case "closeout_provider":
				endpoint.operationErrors =
					map[planescapeprovider.OperationKind]error{
						planescapeprovider.OperationCloseout: errors.New(
							providerDiagnostic,
						),
					}
			case "failed_outcome":
				failed, err := planescapeprovider.NewCloseout(
					planescapeprovider.CloseoutInput{
						SessionID:           admission.SessionID().String(),
						CloseoutID:          "closeout-1",
						TerminalOutcome:     planescapeprovider.OutcomeFailed,
						QuiescenceHash:      planescapeProductHash("7"),
						LogicalEvidenceHash: planescapeProductHash("c"),
						NativeExtensionHash: planescapeProductHash("d"),
						CanonicalHash:       planescapeProductHash("e"),
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				endpoint.responses[2] = failed
			default:
				t.Fatalf("unknown test mode %q", test.mode)
			}

			invocation := testPlanescapeProductInvocation(
				t,
				"exec",
				[]string{"/usr/bin/true", "terminal-argument-secret"},
				"request-terminal-failure-secret",
			)
			localStarts := 0
			result, err := runSessionStartupWithExecutionProvider(
				context.Background(),
				sessionConfig{
					ExecutionProvider: configmodel.ExecutionProviderPlanescape,
				},
				invocation,
				planescapeProductDependencies{
					Endpoint: endpoint,
					CompiledPlanSource: &planescapeProductCompiledPlanSourceFake{
						plan: plan,
					},
					OperationSource: operations,
					TerminalSource:  terminal,
					CheckpointRoot: filepath.Join(
						t.TempDir(),
						"checkpoints",
					),
					Now: func() time.Time { return now },
				},
				func() error {
					localStarts++
					return nil
				},
			)
			requirePlanescapeProductErrorClass(t, err, test.want)
			if result != nil {
				t.Fatal("terminal failure returned a completed lifecycle")
			}
			if localStarts != 0 ||
				terminal.freezeCalls != test.wantFreezeSourceCalls ||
				len(endpoint.freezes) != test.wantProviderFreezes ||
				terminal.closeoutCalls != test.wantCloseoutSourceCall ||
				len(endpoint.operations) != test.wantProviderOperations ||
				endpoint.cancelCalls != 0 {
				t.Fatalf(
					"terminal failure reached fallback or unexpected effect: local=%d freeze_source=%d freezes=%d closeout_source=%d operations=%d cancellations=%d",
					localStarts,
					terminal.freezeCalls,
					len(endpoint.freezes),
					terminal.closeoutCalls,
					len(endpoint.operations),
					endpoint.cancelCalls,
				)
			}
			for _, secret := range []string{
				sourceDiagnostic,
				providerDiagnostic,
				"terminal-argument-secret",
				"request-terminal-failure-secret",
			} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("terminal error leaked sensitive input: %v", err)
				}
			}
			if terminal.freezeCalls == 1 &&
				!terminal.quiesced.Binding().Invocation().matches(invocation) {
				t.Fatal("freeze source did not receive exact invocation binding")
			}
			if terminal.closeoutCalls == 1 &&
				!terminal.frozen.Binding().Invocation().matches(invocation) {
				t.Fatal("closeout source did not receive exact invocation binding")
			}
		})
	}
}

func TestConfiguredPlanescapeCancellationDoesNotFabricateTerminalAuthority(
	t *testing.T,
) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	capabilities, plan, admission := planescapeProductAdmissionFixtures(t)
	endpoint := &planescapeProductEndpointFake{
		capabilities: capabilities,
		admission:    admission,
		operationErr: context.Canceled,
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
	terminal := &planescapeProductTerminalSourceFake{
		freezeInput: newPlanescapeProductFreezeInput(t),
		closeout:    newPlanescapeProductCloseoutIntentForTest(t),
	}
	localStarts := 0
	result, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{ExecutionProvider: configmodel.ExecutionProviderPlanescape},
		testPlanescapeProductInvocation(
			t,
			"exec",
			[]string{"/usr/bin/true"},
			"request-cancelled-tool",
		),
		planescapeProductDependencies{
			Endpoint:           endpoint,
			CompiledPlanSource: &planescapeProductCompiledPlanSourceFake{plan: plan},
			OperationSource:    operations,
			TerminalSource:     terminal,
			CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
			Now:                func() time.Time { return now },
		},
		func() error {
			localStarts++
			return nil
		},
	)
	requirePlanescapeProductErrorClass(
		t,
		err,
		planescapeprovider.ErrorUnavailable,
	)
	if result != nil ||
		localStarts != 0 ||
		len(endpoint.operations) != 1 ||
		endpoint.cancelCalls != 0 ||
		terminal.freezeCalls != 0 ||
		terminal.closeoutCalls != 0 {
		t.Fatalf(
			"cancelled Tool fabricated authority or fallback: result=%v local=%d operations=%d cancellations=%d terminal=%d/%d",
			result != nil,
			localStarts,
			len(endpoint.operations),
			endpoint.cancelCalls,
			terminal.freezeCalls,
			terminal.closeoutCalls,
		)
	}
}
