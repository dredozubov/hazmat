package hazmat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hazmat/configmodel"
	"hazmat/planescapeprovider"
)

type planescapeProductRustAuthorityFixture struct {
	HazmatRustInvocationAuthorityV2 struct {
		Cancellation       planescapeProductRustAuthorityVector `json:"cancellation"`
		SuccessfulCloseout planescapeProductRustAuthorityVector `json:"successful_closeout"`
	} `json:"hazmat_rust_invocation_authority_v2"`
	ProviderAdmissionRPC struct {
		CompiledPlan struct {
			CanonicalJSON string `json:"canonical_json"`
		} `json:"compiled_plan"`
	} `json:"provider_admission_rpc"`
	ProviderToolRPC struct {
		Operation struct {
			CanonicalJSON string `json:"canonical_json"`
		} `json:"operation"`
		NormalizedRecord struct {
			BytesBase64URL string `json:"bytes_b64"`
		} `json:"normalized_record"`
		Result struct {
			CanonicalJSON string `json:"canonical_json"`
		} `json:"result"`
	} `json:"provider_tool_rpc"`
	ProviderQuiescenceRPC struct {
		Operation struct {
			CanonicalJSON string `json:"canonical_json"`
		} `json:"operation"`
		Quiescence struct {
			CanonicalJSON string `json:"canonical_json"`
		} `json:"quiescence"`
	} `json:"provider_quiescence_rpc"`
	ProviderTerminalRPC struct {
		Freeze struct {
			RequestRecord struct {
				CanonicalJSON string `json:"canonical_json"`
			} `json:"request_record"`
			ResponseRecord struct {
				CanonicalJSON string `json:"canonical_json"`
			} `json:"response_record"`
		} `json:"freeze"`
		Closeout struct {
			RequestRecord struct {
				CanonicalJSON string `json:"canonical_json"`
			} `json:"request_record"`
			ResponseRecord struct {
				CanonicalJSON string `json:"canonical_json"`
			} `json:"response_record"`
		} `json:"closeout"`
		Cancellation struct {
			RequestRecord struct {
				CanonicalJSON string `json:"canonical_json"`
			} `json:"request_record"`
			ResponseRecord struct {
				CanonicalJSON string `json:"canonical_json"`
			} `json:"response_record"`
		} `json:"cancellation"`
	} `json:"provider_terminal_rpc"`
}

type planescapeProductRustAuthorityVector struct {
	CanonicalJSON string `json:"canonical_json"`
	JSONByteCount uint64 `json:"json_byte_count"`
	SHA256        string `json:"sha256"`
}

func TestConfiguredPlanescapeAuthoritySourceAcceptsExactRustV2Vectors(
	t *testing.T,
) {
	fixture := loadPlanescapeProductRustAuthorityFixture(t)
	cases := []struct {
		name                  string
		vector                planescapeProductRustAuthorityVector
		expectedJSONByteCount uint64
		expectedSHA256        string
		assertTerminal        func(*testing.T, planescapeProductFileTerminalAuthority)
	}{
		{
			name:                  "successful closeout",
			vector:                fixture.HazmatRustInvocationAuthorityV2.SuccessfulCloseout,
			expectedJSONByteCount: 6443,
			expectedSHA256:        "sha256:c635d41e5e2ac900dc88cf5334b26739eeac98bb5131abb1a87bc65a5d28260c",
			assertTerminal: func(
				t *testing.T,
				terminal planescapeProductFileTerminalAuthority,
			) {
				t.Helper()
				if _, ok := terminal.(planescapeProductFileCloseoutAuthority); !ok {
					t.Fatalf("terminal authority = %T, want closeout", terminal)
				}
			},
		},
		{
			name:                  "cancellation",
			vector:                fixture.HazmatRustInvocationAuthorityV2.Cancellation,
			expectedJSONByteCount: 6620,
			expectedSHA256:        "sha256:53c860d8fd2444765e836518ce01dfbcc175f2509938bb976889d5b8075107af",
			assertTerminal: func(
				t *testing.T,
				terminal planescapeProductFileTerminalAuthority,
			) {
				t.Helper()
				if _, ok := terminal.(planescapeProductFileCancellationAuthority); !ok {
					t.Fatalf("terminal authority = %T, want cancellation", terminal)
				}
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if test.vector.JSONByteCount != test.expectedJSONByteCount {
				t.Fatalf(
					"Rust vector byte count = %d, want %d",
					test.vector.JSONByteCount,
					test.expectedJSONByteCount,
				)
			}
			if test.vector.SHA256 != test.expectedSHA256 {
				t.Fatalf(
					"Rust vector SHA-256 = %q, want %q",
					test.vector.SHA256,
					test.expectedSHA256,
				)
			}
			config := planescapeProductProviderConfigFixture(
				t,
				"127.0.0.1:43191",
			)
			configurePlanescapeProductExactAuthorityVector(
				t,
				config,
				test.vector,
			)
			configuredData, err := os.ReadFile(
				config.InvocationAuthorityFile,
			)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(
				configuredData,
				[]byte(test.vector.CanonicalJSON),
			) {
				t.Fatal("configured authority bytes differ from Rust vector")
			}

			source, err := configuredPlanescapeProductAuthoritySource(config)
			if err != nil {
				t.Fatal(err)
			}
			invocation, err := source.Invocation(
				"exec",
				[]string{"/usr/bin/true"},
			)
			if err != nil {
				t.Fatal(err)
			}
			if invocation.SessionRequestID().String() !=
				"configured-authority-session" {
				t.Fatalf(
					"session request ID = %q",
					invocation.SessionRequestID().String(),
				)
			}
			test.assertTerminal(t, source.terminal)
		})
	}
}

func TestConfiguredPlanescapeAuthoritySourceBindsExactInvocationAndRecords(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	source, err := configuredPlanescapeProductAuthoritySource(config)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := source.Invocation(
		"exec",
		[]string{"/usr/bin/true"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.SessionRequestID().String() !=
		"configured-authority-session" {
		t.Fatalf(
			"session request ID = %q",
			invocation.SessionRequestID().String(),
		)
	}
	for _, mismatch := range []struct {
		command string
		args    []string
	}{
		{command: "shell", args: []string{"/usr/bin/true"}},
		{command: "exec", args: []string{"/usr/bin/false"}},
		{command: "exec", args: []string{"/usr/bin/true", "--extra"}},
	} {
		_, err := source.Invocation(mismatch.command, mismatch.args)
		requirePlanescapeProductErrorClass(
			t,
			err,
			planescapeprovider.ErrorConflict,
		)
	}
	artifact, err := source.CompiledContainmentPlan(
		context.Background(),
		invocation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.valid() ||
		artifact.invocation.SessionRequestID() !=
			invocation.SessionRequestID() {
		t.Fatal("configured authority lost its invocation binding")
	}
}

func TestConfiguredPlanescapeAuthorityUsesFirstRunTerminalIntentBoundary(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	data, err := os.ReadFile(config.InvocationAuthorityFile)
	if err != nil {
		t.Fatal(err)
	}
	var envelope planescapeProductAuthorityFileV2
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Schema != planescapeProductAuthorityFileSchemaV2 {
		t.Fatalf("authority schema = %q", envelope.Schema)
	}
	var terminal planescapeProductAuthorityCloseoutFileV2
	if err := json.Unmarshal(envelope.Terminal, &terminal); err != nil {
		t.Fatal(err)
	}
	if terminal.Kind != planescapeProductAuthorityTerminalCloseoutV2 ||
		terminal.Pause.OperationID == "" ||
		terminal.Pause.OperationSequence != 2 ||
		terminal.Pause.Nonce == "" ||
		terminal.Freeze.FreezeID == "" ||
		terminal.Freeze.Nonce == "" ||
		terminal.Closeout.OperationID == "" ||
		terminal.Closeout.OperationSequence != 3 ||
		terminal.Closeout.Nonce == "" {
		t.Fatalf("terminal intent is incomplete: %+v", terminal)
	}
	for _, forbidden := range []string{
		"pause_operation_json_b64",
		"freeze_json_b64",
		"closeout_operation_json_b64",
		"quiescence_hash",
		"freeze_ack",
		"payload_hash",
		"evidence_hash",
		"session_id",
		"plan_hash",
		"canonical_hash",
	} {
		if bytes.Contains(envelope.Terminal, []byte(forbidden)) {
			t.Fatalf("first-run terminal authority contains %q", forbidden)
		}
	}

	fixture := loadPlanescapeProductRustAuthorityFixture(t)
	for _, priorRunRecord := range []string{
		fixture.ProviderQuiescenceRPC.Operation.CanonicalJSON,
		fixture.ProviderTerminalRPC.Freeze.RequestRecord.CanonicalJSON,
		fixture.ProviderTerminalRPC.Closeout.RequestRecord.CanonicalJSON,
	} {
		encoded := base64.RawURLEncoding.EncodeToString(
			[]byte(priorRunRecord),
		)
		if bytes.Contains(data, []byte(encoded)) {
			t.Fatal("authority retained a prior-run terminal request record")
		}
	}
	if _, err := configuredPlanescapeProductAuthoritySource(config); err != nil {
		t.Fatalf("fresh first-run authority did not load: %v", err)
	}
}

func TestConfiguredPlanescapeAuthorityBindsPauseToLiveToolEvidence(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	source, err := configuredPlanescapeProductAuthoritySource(config)
	if err != nil {
		t.Fatal(err)
	}
	liveEvidenceHash := planescapeProductHash("d")
	tool, err := planescapeprovider.NewOperationResult(
		planescapeprovider.OperationResultInput{
			SessionID:     source.tool.record.SessionID().String(),
			OperationID:   source.tool.record.OperationID().String(),
			Sequence:      source.tool.record.Sequence().Uint64(),
			ResultKind:    planescapeprovider.ResultCompleted,
			ArtifactHash:  planescapeProductHash("e"),
			EvidenceHash:  liveEvidenceHash,
			CanonicalHash: planescapeProductHash("f"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	binding := planescapeProductBinding{
		planHash:   source.plan.CanonicalHash(),
		session:    source.tool.record.SessionID(),
		backend:    configuredPlanescapeProductBackendBinding(t, config),
		invocation: source.invocation,
	}
	intent, err := source.PostToolIntent(
		context.Background(),
		binding,
		tool,
	)
	if err != nil {
		t.Fatal(err)
	}
	pause, ok := intent.(planescapeProductPauseIntent)
	if !ok || !pause.valid() {
		t.Fatalf("post-Tool intent = %T, want valid Pause", intent)
	}
	if pause.operation.PayloadHash().String() != liveEvidenceHash {
		t.Fatalf(
			"Pause payload hash = %q, want live Tool evidence %q",
			pause.operation.PayloadHash().String(),
			liveEvidenceHash,
		)
	}
	terminal, ok := source.terminal.(planescapeProductFileCloseoutAuthority)
	if !ok {
		t.Fatalf("terminal authority = %T, want closeout", source.terminal)
	}
	if pause.operation.OperationID() != terminal.pause.operationID ||
		pause.operation.Nonce() != terminal.pause.nonce {
		t.Fatal("runtime Pause did not retain exact authored identity")
	}
}

func TestConfiguredPlanescapeAuthorityRejectsPriorEnvelopeVersion(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	data, err := os.ReadFile(config.InvocationAuthorityFile)
	if err != nil {
		t.Fatal(err)
	}
	var envelope planescapeProductAuthorityFileV2
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Schema = "hazmat.planescape.rust_invocation_authority.v1"
	writePlanescapeProductAuthorityEnvelope(t, config, envelope)

	_, err = configuredPlanescapeProductAuthoritySource(config)
	requirePlanescapeProductErrorClass(
		t,
		err,
		planescapeprovider.ErrorInvalid,
	)
}

func TestConfiguredPlanescapeAuthorityRejectsInvalidTerminalIntent(
	t *testing.T,
) {
	cases := []struct {
		name   string
		mutate func(*planescapeProductAuthorityCloseoutFileV2)
	}{
		{
			name: "missing Pause identity",
			mutate: func(value *planescapeProductAuthorityCloseoutFileV2) {
				value.Pause.OperationID = ""
			},
		},
		{
			name: "wrong Pause sequence",
			mutate: func(value *planescapeProductAuthorityCloseoutFileV2) {
				value.Pause.OperationSequence = 3
			},
		},
		{
			name: "missing Pause nonce",
			mutate: func(value *planescapeProductAuthorityCloseoutFileV2) {
				value.Pause.Nonce = ""
			},
		},
		{
			name: "missing Freeze identity",
			mutate: func(value *planescapeProductAuthorityCloseoutFileV2) {
				value.Freeze.FreezeID = ""
			},
		},
		{
			name: "wrong Closeout sequence",
			mutate: func(value *planescapeProductAuthorityCloseoutFileV2) {
				value.Closeout.OperationSequence = 4
			},
		},
		{
			name: "missing Closeout nonce",
			mutate: func(value *planescapeProductAuthorityCloseoutFileV2) {
				value.Closeout.Nonce = ""
			},
		},
		{
			name: "Pause reuses Tool identity",
			mutate: func(value *planescapeProductAuthorityCloseoutFileV2) {
				value.Pause.OperationID = "conformance-provider-tool"
			},
		},
		{
			name: "Closeout reuses Pause identity",
			mutate: func(value *planescapeProductAuthorityCloseoutFileV2) {
				value.Closeout.OperationID = value.Pause.OperationID
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config := planescapeProductProviderConfigFixture(
				t,
				"127.0.0.1:43191",
			)
			data, err := os.ReadFile(config.InvocationAuthorityFile)
			if err != nil {
				t.Fatal(err)
			}
			var envelope planescapeProductAuthorityFileV2
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatal(err)
			}
			var terminal planescapeProductAuthorityCloseoutFileV2
			if err := json.Unmarshal(
				envelope.Terminal,
				&terminal,
			); err != nil {
				t.Fatal(err)
			}
			test.mutate(&terminal)
			envelope.Terminal, err = json.Marshal(terminal)
			if err != nil {
				t.Fatal(err)
			}
			writePlanescapeProductAuthorityEnvelope(t, config, envelope)

			_, err = configuredPlanescapeProductAuthoritySource(config)
			requirePlanescapeProductErrorClass(
				t,
				err,
				planescapeprovider.ErrorInvalid,
			)
		})
	}
}

func TestConfiguredPlanescapeAuthorityRejectsAuthoredPauseBinding(
	t *testing.T,
) {
	fixture := loadPlanescapeProductRustAuthorityFixture(t)
	legacyPause := base64.RawURLEncoding.EncodeToString(
		[]byte(fixture.ProviderQuiescenceRPC.Operation.CanonicalJSON),
	)
	setTerminalField := func(
		t *testing.T,
		terminal map[string]json.RawMessage,
		name string,
		value any,
	) {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		terminal[name] = encoded
	}
	setPauseField := func(
		t *testing.T,
		terminal map[string]json.RawMessage,
		name string,
		value any,
	) {
		t.Helper()
		var pause map[string]json.RawMessage
		if err := json.Unmarshal(terminal["pause"], &pause); err != nil {
			t.Fatal(err)
		}
		setTerminalField(t, pause, name, value)
		encoded, err := json.Marshal(pause)
		if err != nil {
			t.Fatal(err)
		}
		terminal["pause"] = encoded
	}
	cases := []struct {
		name   string
		mutate func(*testing.T, map[string]json.RawMessage)
	}{
		{
			name: "legacy Pause operation record",
			mutate: func(
				t *testing.T,
				terminal map[string]json.RawMessage,
			) {
				delete(terminal, "pause")
				setTerminalField(
					t,
					terminal,
					"pause_operation_json_b64",
					legacyPause,
				)
			},
		},
		{
			name: "ambiguous intent and legacy record",
			mutate: func(
				t *testing.T,
				terminal map[string]json.RawMessage,
			) {
				setTerminalField(
					t,
					terminal,
					"pause_operation_json_b64",
					legacyPause,
				)
			},
		},
		{
			name: "authored payload hash",
			mutate: func(
				t *testing.T,
				terminal map[string]json.RawMessage,
			) {
				setPauseField(
					t,
					terminal,
					"payload_hash",
					planescapeProductHash("a"),
				)
			},
		},
		{
			name: "authored evidence hash",
			mutate: func(
				t *testing.T,
				terminal map[string]json.RawMessage,
			) {
				setPauseField(
					t,
					terminal,
					"evidence_hash",
					planescapeProductHash("b"),
				)
			},
		},
		{
			name: "authored session binding",
			mutate: func(
				t *testing.T,
				terminal map[string]json.RawMessage,
			) {
				setPauseField(
					t,
					terminal,
					"session_id",
					"authored-session",
				)
			},
		},
		{
			name: "authored plan binding",
			mutate: func(
				t *testing.T,
				terminal map[string]json.RawMessage,
			) {
				setPauseField(
					t,
					terminal,
					"plan_hash",
					planescapeProductHash("c"),
				)
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config := planescapeProductProviderConfigFixture(
				t,
				"127.0.0.1:43191",
			)
			data, err := os.ReadFile(config.InvocationAuthorityFile)
			if err != nil {
				t.Fatal(err)
			}
			var envelope planescapeProductAuthorityFileV2
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatal(err)
			}
			var terminal map[string]json.RawMessage
			if err := json.Unmarshal(
				envelope.Terminal,
				&terminal,
			); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, terminal)
			envelope.Terminal, err = json.Marshal(terminal)
			if err != nil {
				t.Fatal(err)
			}
			writePlanescapeProductAuthorityEnvelope(t, config, envelope)

			_, err = configuredPlanescapeProductAuthoritySource(config)
			requirePlanescapeProductErrorClass(
				t,
				err,
				planescapeprovider.ErrorInvalid,
			)
		})
	}
}

func TestConfiguredPlanescapeLegacyPauseDoesNotDialOrFallback(
	t *testing.T,
) {
	isolateConfig(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		t.Fatalf("listener %T is not TCP", listener)
	}
	config := planescapeProductProviderConfigFixture(
		t,
		listener.Addr().String(),
	)
	data, err := os.ReadFile(config.InvocationAuthorityFile)
	if err != nil {
		t.Fatal(err)
	}
	var envelope planescapeProductAuthorityFileV2
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var terminal map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Terminal, &terminal); err != nil {
		t.Fatal(err)
	}
	delete(terminal, "pause")
	fixture := loadPlanescapeProductRustAuthorityFixture(t)
	legacyPause, err := json.Marshal(
		base64.RawURLEncoding.EncodeToString(
			[]byte(fixture.ProviderQuiescenceRPC.Operation.CanonicalJSON),
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	terminal["pause_operation_json_b64"] = legacyPause
	envelope.Terminal, err = json.Marshal(terminal)
	if err != nil {
		t.Fatal(err)
	}
	writePlanescapeProductAuthorityEnvelope(t, config, envelope)

	hazmatConfig := defaultConfig()
	hazmatConfig.Session.ExecutionProvider =
		configmodel.ExecutionProviderPlanescape
	hazmatConfig.Session.Planescape = config
	if err := saveConfig(hazmatConfig); err != nil {
		t.Fatal(err)
	}
	savedDependencies := planescapeProductDependenciesForSession
	savedRunner := runAgentSeatbeltScriptWithPlan
	planescapeProductDependenciesForSession =
		defaultPlanescapeProductDependencies
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
	t.Cleanup(func() {
		planescapeProductDependenciesForSession = savedDependencies
		runAgentSeatbeltScriptWithPlan = savedRunner
	})

	command := newExecCmd()
	command.SetArgs([]string{"/usr/bin/true"})
	command.SilenceErrors = true
	command.SilenceUsage = true
	err = command.Execute()
	requirePlanescapeProductErrorClass(
		t,
		err,
		planescapeprovider.ErrorInvalid,
	)
	if nativeRunnerCalls != 0 {
		t.Fatalf("native runner calls = %d, want 0", nativeRunnerCalls)
	}
	if err := tcpListener.SetDeadline(
		time.Now().Add(100 * time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	connection, acceptErr := listener.Accept()
	if connection != nil {
		_ = connection.Close()
		t.Fatal("legacy Pause authority dialed the provider")
	}
	var networkError net.Error
	if !errors.As(acceptErr, &networkError) || !networkError.Timeout() {
		t.Fatalf("Accept error = %v, want timeout without a dial", acceptErr)
	}
}

func TestConfiguredPlanescapeInvocationMismatchDoesNotDialOrFallback(
	t *testing.T,
) {
	isolateConfig(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		t.Fatalf("listener %T is not TCP", listener)
	}
	config := planescapeProductProviderConfigFixture(
		t,
		listener.Addr().String(),
	)
	hazmatConfig := defaultConfig()
	hazmatConfig.Session.ExecutionProvider =
		configmodel.ExecutionProviderPlanescape
	hazmatConfig.Session.Planescape = config
	if err := saveConfig(hazmatConfig); err != nil {
		t.Fatal(err)
	}

	savedDependencies := planescapeProductDependenciesForSession
	savedRunner := runAgentSeatbeltScriptWithPlan
	planescapeProductDependenciesForSession =
		defaultPlanescapeProductDependencies
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
	t.Cleanup(func() {
		planescapeProductDependenciesForSession = savedDependencies
		runAgentSeatbeltScriptWithPlan = savedRunner
	})

	command := newExecCmd()
	command.SetArgs([]string{"/usr/bin/false"})
	command.SilenceErrors = true
	command.SilenceUsage = true
	err = command.Execute()
	requirePlanescapeProductErrorClass(
		t,
		err,
		planescapeprovider.ErrorConflict,
	)
	if nativeRunnerCalls != 0 {
		t.Fatalf("native runner calls = %d, want 0", nativeRunnerCalls)
	}
	if err := tcpListener.SetDeadline(
		time.Now().Add(100 * time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	connection, acceptErr := listener.Accept()
	if connection != nil {
		_ = connection.Close()
		t.Fatal("invocation mismatch dialed the provider")
	}
	var networkError net.Error
	if !errors.As(acceptErr, &networkError) || !networkError.Timeout() {
		t.Fatalf("Accept error = %v, want timeout without a dial", acceptErr)
	}
	for _, sensitive := range []string{
		config.InvocationAuthorityFile,
		"/usr/bin/true",
		"/usr/bin/false",
		"configured-authority-session",
	} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("invocation diagnostic leaked authority: %v", err)
		}
	}
}

func TestConfiguredPlanescapeAuthoritySourceRejectsUnsafeOrChangedFileRedacted(
	t *testing.T,
) {
	cases := []struct {
		name   string
		mutate func(*testing.T, *configmodel.PlanescapeProviderConfig)
		want   planescapeprovider.ErrorClass
	}{
		{
			name: "relative path",
			mutate: func(
				_ *testing.T,
				config *configmodel.PlanescapeProviderConfig,
			) {
				config.InvocationAuthorityFile = "authority-secret.json"
			},
			want: planescapeprovider.ErrorInvalid,
		},
		{
			name: "group readable",
			mutate: func(
				t *testing.T,
				config *configmodel.PlanescapeProviderConfig,
			) {
				t.Helper()
				if err := os.Chmod(
					config.InvocationAuthorityFile,
					0o640,
				); err != nil {
					t.Fatal(err)
				}
			},
			want: planescapeprovider.ErrorInvalid,
		},
		{
			name: "changed content",
			mutate: func(
				t *testing.T,
				config *configmodel.PlanescapeProviderConfig,
			) {
				t.Helper()
				data, err := os.ReadFile(config.InvocationAuthorityFile)
				if err != nil {
					t.Fatal(err)
				}
				data = append(data, '\n')
				if err := os.WriteFile(
					config.InvocationAuthorityFile,
					data,
					0o600,
				); err != nil {
					t.Fatal(err)
				}
			},
			want: planescapeprovider.ErrorConflict,
		},
		{
			name: "hard link",
			mutate: func(
				t *testing.T,
				config *configmodel.PlanescapeProviderConfig,
			) {
				t.Helper()
				if err := os.Link(
					config.InvocationAuthorityFile,
					config.InvocationAuthorityFile+".link",
				); err != nil {
					t.Fatal(err)
				}
			},
			want: planescapeprovider.ErrorInvalid,
		},
		{
			name: "symbolic link",
			mutate: func(
				t *testing.T,
				config *configmodel.PlanescapeProviderConfig,
			) {
				t.Helper()
				link := config.InvocationAuthorityFile + ".symlink"
				if err := os.Symlink(
					config.InvocationAuthorityFile,
					link,
				); err != nil {
					t.Fatal(err)
				}
				config.InvocationAuthorityFile = link
			},
			want: planescapeprovider.ErrorInvalid,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config := planescapeProductProviderConfigFixture(
				t,
				"127.0.0.1:43191",
			)
			test.mutate(t, config)
			_, err := configuredPlanescapeProductAuthoritySource(config)
			requirePlanescapeProductErrorClass(t, err, test.want)
			for _, sensitive := range []string{
				config.InvocationAuthorityFile,
				"authority-secret.json",
				"configured-authority-session",
				"/usr/bin/true",
			} {
				if sensitive != "" && strings.Contains(err.Error(), sensitive) {
					t.Fatalf("authority diagnostic leaked input: %v", err)
				}
			}
		})
	}
}

func TestConfiguredPlanescapeAuthorityRejectsRecordMutationBeforeEffect(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	data, err := os.ReadFile(config.InvocationAuthorityFile)
	if err != nil {
		t.Fatal(err)
	}
	var envelope planescapeProductAuthorityFileV2
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	operation, err := base64.RawURLEncoding.Strict().DecodeString(
		envelope.Tool.OperationJSONBase64URL,
	)
	if err != nil {
		t.Fatal(err)
	}
	operation = []byte(strings.Replace(
		string(operation),
		`"operation_kind":"tool"`,
		`"operation_kind":"workspace"`,
		1,
	))
	envelope.Tool.OperationJSONBase64URL =
		base64.RawURLEncoding.EncodeToString(operation)
	writePlanescapeProductAuthorityEnvelope(t, config, envelope)

	_, err = configuredPlanescapeProductAuthoritySource(config)
	requirePlanescapeProductErrorClass(
		t,
		err,
		planescapeprovider.ErrorInvalid,
	)
	if strings.Contains(err.Error(), "workspace") {
		t.Fatalf("record diagnostic leaked authority bytes: %v", err)
	}
}

func TestConfiguredPlanescapeAuthorityRejectsAmbiguousEnvelope(
	t *testing.T,
) {
	cases := []struct {
		name   string
		mutate func(string) string
	}{
		{
			name: "missing forwarded args",
			mutate: func(value string) string {
				return strings.Replace(
					value,
					`"forwarded_args":["/usr/bin/true"],`,
					"",
					1,
				)
			},
		},
		{
			name: "duplicate schema",
			mutate: func(value string) string {
				return strings.Replace(
					value,
					`{"schema":`,
					`{"schema":"shadow","schema":`,
					1,
				)
			},
		},
		{
			name: "unknown field",
			mutate: func(value string) string {
				return strings.Replace(
					value,
					`{"schema":`,
					`{"unexpected":"authority-secret","schema":`,
					1,
				)
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config := planescapeProductProviderConfigFixture(
				t,
				"127.0.0.1:43191",
			)
			data, err := os.ReadFile(config.InvocationAuthorityFile)
			if err != nil {
				t.Fatal(err)
			}
			mutated := []byte(test.mutate(string(data)))
			if bytes.Equal(mutated, data) {
				t.Fatal("test mutation did not change authority")
			}
			if err := os.WriteFile(
				config.InvocationAuthorityFile,
				mutated,
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			config.InvocationAuthorityFileSHA256 =
				planescapeProductAuthorityBytesSHA256(mutated)

			_, err = configuredPlanescapeProductAuthoritySource(config)
			requirePlanescapeProductErrorClass(
				t,
				err,
				planescapeprovider.ErrorInvalid,
			)
			for _, sensitive := range []string{
				config.InvocationAuthorityFile,
				"authority-secret",
				"configured-authority-session",
			} {
				if strings.Contains(err.Error(), sensitive) {
					t.Fatalf(
						"ambiguous-envelope diagnostic leaked input: %v",
						err,
					)
				}
			}
		})
	}
}

func TestConfiguredPlanescapeFileAuthorityCompletesCloseoutWithoutFallback(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	source, err := configuredPlanescapeProductAuthoritySource(config)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := source.Invocation(
		"exec",
		[]string{"/usr/bin/true"},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture := loadPlanescapeProductRustAuthorityFixture(t)
	capabilities, admission, tool, quiescence, freeze, closeout :=
		planescapeProductAuthorityLifecycleFixture(t, source, fixture)
	endpoint := &planescapeProductEndpointFake{
		backend:      configuredPlanescapeProductBackendBinding(t, config),
		capabilities: capabilities,
		admission:    admission,
		freezeAck:    freeze,
		responses: []planescapeprovider.OperationResponse{
			tool,
			quiescence,
			closeout,
		},
	}
	localStarts := 0
	result, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{
			ExecutionProvider: configmodel.ExecutionProviderPlanescape,
		},
		invocation,
		planescapeProductDependencies{
			InvocationSource:   source,
			Endpoint:           endpoint,
			CompiledPlanSource: source,
			OperationSource:    source,
			TerminalSource:     source,
			CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
			Now:                func() time.Time { return time.UnixMilli(1).UTC() },
		},
		func() error {
			localStarts++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil ||
		result.Closeout().CanonicalHash() != closeout.CanonicalHash() ||
		localStarts != 0 ||
		len(endpoint.operations) != 3 ||
		len(endpoint.freezes) != 1 ||
		endpoint.cancelCalls != 0 {
		t.Fatalf(
			"closeout composition = result:%v local:%d operations:%d freezes:%d cancellations:%d",
			result != nil,
			localStarts,
			len(endpoint.operations),
			len(endpoint.freezes),
			endpoint.cancelCalls,
		)
	}
	codec := planescapeprovider.ProviderV1FrameCodec{}
	wantOperations := []string{
		fixture.ProviderToolRPC.Operation.CanonicalJSON,
		fixture.ProviderQuiescenceRPC.Operation.CanonicalJSON,
		fixture.ProviderTerminalRPC.Closeout.RequestRecord.CanonicalJSON,
	}
	for index, want := range wantOperations {
		got, err := codec.EncodeOperation(endpoint.operations[index])
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf(
				"runtime operation %d did not match the exact Rust record",
				index+1,
			)
		}
	}
	runtimePause := endpoint.operations[1]
	if runtimePause.SessionID() != admission.SessionID() ||
		runtimePause.PlanHash() != source.plan.CanonicalHash() ||
		runtimePause.Sequence().Uint64() != 2 ||
		runtimePause.PayloadHash() != tool.EvidenceHash() {
		t.Fatal("runtime Pause lost live session, plan, sequence, or Tool evidence binding")
	}
	freezeRequest, err := codec.EncodeFreeze(endpoint.freezes[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(freezeRequest) !=
		fixture.ProviderTerminalRPC.Freeze.RequestRecord.CanonicalJSON {
		t.Fatal("runtime Freeze did not match the exact Rust record")
	}
	if endpoint.operations[2].PayloadHash() != freeze.CanonicalHash() {
		t.Fatal("Closeout payload was not bound to the live Freeze acknowledgement")
	}
}

func TestConfiguredPlanescapeFileAuthorityRejectsProviderSessionBeforeTool(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	source, err := configuredPlanescapeProductAuthoritySource(config)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := source.Invocation(
		"exec",
		[]string{"/usr/bin/true"},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture := loadPlanescapeProductRustAuthorityFixture(t)
	capabilities, admission, tool, quiescence, freeze, closeout :=
		planescapeProductAuthorityLifecycleFixture(t, source, fixture)
	foreignAdmission, err := planescapeprovider.NewSessionAdmission(
		planescapeprovider.SessionAdmissionInput{
			SessionID:             "foreign-provider-session",
			ProviderID:            admission.ProviderID().String(),
			ProviderEpoch:         admission.ProviderEpoch().Uint64(),
			RequirementHash:       admission.RequirementHash().String(),
			CompiledPlanHash:      admission.CompiledPlanHash().String(),
			SessionCapabilityHash: admission.SessionCapabilityHash().String(),
			ExpiresAt:             admission.ExpiresAt(),
			CanonicalHash:         planescapeProductHash("3"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &planescapeProductEndpointFake{
		backend:      configuredPlanescapeProductBackendBinding(t, config),
		capabilities: capabilities,
		admission:    foreignAdmission,
		freezeAck:    freeze,
		responses: []planescapeprovider.OperationResponse{
			tool,
			quiescence,
			closeout,
		},
	}
	localStarts := 0
	result, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{
			ExecutionProvider: configmodel.ExecutionProviderPlanescape,
		},
		invocation,
		planescapeProductDependencies{
			InvocationSource:   source,
			Endpoint:           endpoint,
			CompiledPlanSource: source,
			OperationSource:    source,
			TerminalSource:     source,
			CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
			Now:                func() time.Time { return time.UnixMilli(1).UTC() },
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
	if result != nil ||
		localStarts != 0 ||
		len(endpoint.operations) != 0 ||
		len(endpoint.freezes) != 0 ||
		endpoint.cancelCalls != 0 {
		t.Fatalf(
			"foreign session reached effect/fallback: result=%v local=%d operations=%d freezes=%d cancellations=%d",
			result != nil,
			localStarts,
			len(endpoint.operations),
			len(endpoint.freezes),
			endpoint.cancelCalls,
		)
	}
}

func TestConfiguredPlanescapeFileAuthorityRejectsProviderRestartWithoutFallback(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	source, err := configuredPlanescapeProductAuthoritySource(config)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := source.Invocation(
		"exec",
		[]string{"/usr/bin/true"},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture := loadPlanescapeProductRustAuthorityFixture(t)
	capabilities, admission, _, _, _, _ :=
		planescapeProductAuthorityLifecycleFixture(t, source, fixture)
	endpoint := &planescapeProductEndpointFake{
		backend:      configuredPlanescapeProductBackendBinding(t, config),
		capabilities: capabilities,
		admission:    admission,
		afterAdmit: func(endpoint *planescapeProductEndpointFake) {
			endpoint.backend =
				newPlanescapeProductBackendBinding(t, "f")
		},
	}
	localStarts := 0
	result, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{
			ExecutionProvider: configmodel.ExecutionProviderPlanescape,
		},
		invocation,
		planescapeProductDependencies{
			InvocationSource:   source,
			Endpoint:           endpoint,
			CompiledPlanSource: source,
			OperationSource:    source,
			TerminalSource:     source,
			CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
			Now:                func() time.Time { return time.UnixMilli(1).UTC() },
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
	if result != nil ||
		localStarts != 0 ||
		len(endpoint.operations) != 0 ||
		len(endpoint.freezes) != 0 ||
		endpoint.cancelCalls != 0 {
		t.Fatalf(
			"provider restart reached effect/fallback: result=%v local=%d operations=%d freezes=%d cancellations=%d",
			result != nil,
			localStarts,
			len(endpoint.operations),
			len(endpoint.freezes),
			endpoint.cancelCalls,
		)
	}
}

func TestConfiguredPlanescapeFileAuthorityReplayConflictDoesNotFallback(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	source, err := configuredPlanescapeProductAuthoritySource(config)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := source.Invocation(
		"exec",
		[]string{"/usr/bin/true"},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture := loadPlanescapeProductRustAuthorityFixture(t)
	capabilities, admission, _, _, _, _ :=
		planescapeProductAuthorityLifecycleFixture(t, source, fixture)
	endpoint := &planescapeProductEndpointFake{
		backend:      configuredPlanescapeProductBackendBinding(t, config),
		capabilities: capabilities,
		admission:    admission,
		operationErrors: map[planescapeprovider.OperationKind]error{
			planescapeprovider.OperationTool: newPlanescapeProviderFailureForTest(
				t,
				capabilities,
				planescapeprovider.ProviderErrorReplayConflict,
				planescapeprovider.TransitionActivate,
			),
		},
	}
	localStarts := 0
	result, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{
			ExecutionProvider: configmodel.ExecutionProviderPlanescape,
		},
		invocation,
		planescapeProductDependencies{
			InvocationSource:   source,
			Endpoint:           endpoint,
			CompiledPlanSource: source,
			OperationSource:    source,
			TerminalSource:     source,
			CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
			Now:                func() time.Time { return time.UnixMilli(1).UTC() },
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
	if result != nil ||
		localStarts != 0 ||
		len(endpoint.operations) != 1 ||
		len(endpoint.freezes) != 0 ||
		endpoint.cancelCalls != 0 {
		t.Fatalf(
			"provider replay reached fallback/later effect: result=%v local=%d operations=%d freezes=%d cancellations=%d",
			result != nil,
			localStarts,
			len(endpoint.operations),
			len(endpoint.freezes),
			endpoint.cancelCalls,
		)
	}
}

func TestConfiguredPlanescapeFileAuthorityUsesDistinctCancellationPath(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	configurePlanescapeProductAuthorityFixture(
		t,
		config,
		"exec",
		[]string{"/usr/bin/true"},
		"configured-cancellation-session",
		planescapeProductAuthorityTerminalCancellationV2,
	)
	source, err := configuredPlanescapeProductAuthoritySource(config)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := source.Invocation(
		"exec",
		[]string{"/usr/bin/true"},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture := loadPlanescapeProductRustAuthorityFixture(t)
	capabilities, admission, tool, _, _, _ :=
		planescapeProductAuthorityLifecycleFixture(t, source, fixture)
	cancellation, err := (planescapeprovider.ProviderV1FrameCodec{}).
		DecodeCancellationAck(
			[]byte(
				fixture.ProviderTerminalRPC.Cancellation.
					ResponseRecord.CanonicalJSON,
			),
		)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &planescapeProductEndpointFake{
		backend:         configuredPlanescapeProductBackendBinding(t, config),
		capabilities:    capabilities,
		admission:       admission,
		cancellationAck: cancellation,
		responses:       []planescapeprovider.OperationResponse{tool},
	}
	localStarts := 0
	result, err := runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{
			ExecutionProvider: configmodel.ExecutionProviderPlanescape,
		},
		invocation,
		planescapeProductDependencies{
			InvocationSource:   source,
			Endpoint:           endpoint,
			CompiledPlanSource: source,
			OperationSource:    source,
			TerminalSource:     source,
			CheckpointRoot:     filepath.Join(t.TempDir(), "checkpoints"),
			Now:                func() time.Time { return time.UnixMilli(1).UTC() },
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
		len(endpoint.freezes) != 0 ||
		endpoint.cancelCalls != 1 {
		t.Fatalf(
			"cancellation composition = result:%v local:%d operations:%d freezes:%d cancellations:%d",
			result != nil,
			localStarts,
			len(endpoint.operations),
			len(endpoint.freezes),
			endpoint.cancelCalls,
		)
	}
	if len(endpoint.cancellations) != 1 {
		t.Fatalf(
			"cancellation requests = %d, want 1",
			len(endpoint.cancellations),
		)
	}
	request, err := (planescapeprovider.ProviderV1FrameCodec{}).
		EncodeCancellation(endpoint.cancellations[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(request) !=
		fixture.ProviderTerminalRPC.Cancellation.RequestRecord.CanonicalJSON {
		t.Fatal("runtime Cancellation did not match the exact Rust record")
	}
}

func configurePlanescapeProductAuthorityFixture(
	t *testing.T,
	config *configmodel.PlanescapeProviderConfig,
	commandName string,
	forwardedArgs []string,
	sessionRequestID string,
	terminalKind string,
) {
	t.Helper()
	fixture := loadPlanescapeProductRustAuthorityFixture(t)
	toolRecord, err := base64.RawURLEncoding.Strict().DecodeString(
		fixture.ProviderToolRPC.NormalizedRecord.BytesBase64URL,
	)
	if err != nil {
		t.Fatal(err)
	}
	envelope := planescapeProductAuthorityFileV2{
		Schema: planescapeProductAuthorityFileSchemaV2,
		Invocation: planescapeProductAuthorityInvocationFileV2{
			CommandName:      commandName,
			ForwardedArgs:    append([]string(nil), forwardedArgs...),
			SessionRequestID: sessionRequestID,
		},
		CompiledPlanJSONBase64URL: base64.RawURLEncoding.EncodeToString(
			[]byte(
				fixture.ProviderAdmissionRPC.CompiledPlan.CanonicalJSON,
			),
		),
		Tool: planescapeProductAuthorityToolFileV2{
			OperationJSONBase64URL: base64.RawURLEncoding.EncodeToString(
				[]byte(fixture.ProviderToolRPC.Operation.CanonicalJSON),
			),
			NormalizedRecordBase64URL: fixture.ProviderToolRPC.NormalizedRecord.BytesBase64URL,
			NormalizedRecordSHA256: planescapeProductAuthorityBytesSHA256(
				toolRecord,
			),
		},
	}
	switch terminalKind {
	case planescapeProductAuthorityTerminalCloseoutV2:
		codec := planescapeprovider.ProviderV1FrameCodec{}
		freeze, err := codec.DecodeFreeze(
			[]byte(
				fixture.ProviderTerminalRPC.Freeze.RequestRecord.
					CanonicalJSON,
			),
		)
		if err != nil {
			t.Fatal(err)
		}
		pause, err := codec.DecodeAgentOperation(
			[]byte(
				fixture.ProviderQuiescenceRPC.Operation.
					CanonicalJSON,
			),
		)
		if err != nil {
			t.Fatal(err)
		}
		closeout, err := codec.DecodeAgentOperation(
			[]byte(
				fixture.ProviderTerminalRPC.Closeout.RequestRecord.
					CanonicalJSON,
			),
		)
		if err != nil {
			t.Fatal(err)
		}
		terminal, err := json.Marshal(
			planescapeProductAuthorityCloseoutFileV2{
				Kind: terminalKind,
				Pause: planescapeProductAuthorityPauseIntentFileV2{
					OperationID: pause.OperationID().String(),
					OperationSequence: pause.Sequence().
						Uint64(),
					Nonce: pause.Nonce().String(),
				},
				Freeze: planescapeProductAuthorityFreezeIntentFileV2{
					FreezeID: freeze.FreezeID().String(),
					Nonce:    freeze.Nonce().String(),
				},
				Closeout: planescapeProductAuthorityCloseoutIntentFileV2{
					OperationID: closeout.OperationID().String(),
					OperationSequence: closeout.Sequence().
						Uint64(),
					Nonce: closeout.Nonce().String(),
				},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		envelope.Terminal = terminal
	case planescapeProductAuthorityTerminalCancellationV2:
		terminal, err := json.Marshal(
			planescapeProductAuthorityCancellationFileV2{
				Kind: terminalKind,
				CancellationJSONBase64URL: base64.RawURLEncoding.EncodeToString(
					[]byte(
						fixture.ProviderTerminalRPC.Cancellation.
							RequestRecord.CanonicalJSON,
					),
				),
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		envelope.Terminal = terminal
	default:
		t.Fatalf("unsupported test terminal kind %q", terminalKind)
	}
	writePlanescapeProductAuthorityEnvelope(t, config, envelope)
}

func writePlanescapeProductAuthorityEnvelope(
	t *testing.T,
	config *configmodel.PlanescapeProviderConfig,
	envelope planescapeProductAuthorityFileV2,
) {
	t.Helper()
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rust-invocation-authority.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	config.InvocationAuthorityFile = path
	config.InvocationAuthorityFileSHA256 =
		planescapeProductAuthorityBytesSHA256(data)
}

func configurePlanescapeProductExactAuthorityVector(
	t *testing.T,
	config *configmodel.PlanescapeProviderConfig,
	vector planescapeProductRustAuthorityVector,
) {
	t.Helper()
	data := []byte(vector.CanonicalJSON)
	if vector.JSONByteCount == 0 ||
		uint64(len(data)) != vector.JSONByteCount {
		t.Fatalf(
			"Rust authority byte count = %d, want %d",
			len(data),
			vector.JSONByteCount,
		)
	}
	if actual := planescapeProductAuthorityBytesSHA256(data); actual != vector.SHA256 {
		t.Fatalf("Rust authority SHA-256 = %q, want %q", actual, vector.SHA256)
	}
	path := filepath.Join(t.TempDir(), "rust-invocation-authority.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	config.InvocationAuthorityFile = path
	config.InvocationAuthorityFileSHA256 = vector.SHA256
}

func loadPlanescapeProductRustAuthorityFixture(
	t *testing.T,
) planescapeProductRustAuthorityFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(
		"planescapeprovider",
		"testdata",
		"protected_broker.v1",
		"protected_broker_v1.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var fixture planescapeProductRustAuthorityFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func configuredPlanescapeProductBackendBinding(
	t *testing.T,
	config *configmodel.PlanescapeProviderConfig,
) planescapeprovider.BackendIdentityBinding {
	t.Helper()
	binding, err := planescapeprovider.NewBackendIdentityBinding(
		planescapeprovider.BackendIdentityBindingInput{
			IdentitySHA256: config.Backend.IdentitySHA256,
			ProviderEpoch:  config.Backend.BrokerEpoch,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func planescapeProductAuthorityLifecycleFixture(
	t *testing.T,
	source *planescapeProductFileAuthoritySource,
	fixture planescapeProductRustAuthorityFixture,
) (
	planescapeprovider.ProviderCapabilities,
	planescapeprovider.SessionAdmission,
	planescapeprovider.OperationResult,
	planescapeprovider.Quiescence,
	planescapeprovider.FreezeAck,
	planescapeprovider.Closeout,
) {
	t.Helper()
	codec := planescapeprovider.ProviderV1FrameCodec{}
	capabilities, err := planescapeprovider.NewProviderCapabilities(
		planescapeprovider.ProviderCapabilitiesInput{
			ProviderID:     source.plan.ProviderID().String(),
			ProviderEpoch:  source.plan.ProviderEpoch().Uint64(),
			Profile:        source.plan.ProviderProfile(),
			CapabilityHash: source.plan.ProviderCapabilityHash().String(),
			CanonicalHash:  planescapeProductHash("1"),
			Capabilities: source.plan.Requirement().
				RequiredCapabilities(),
			ResourceDimensions: source.plan.Requirement().
				RequiredResourceDimensions(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	deadline, ok := source.plan.DeadlineAt()
	if !ok {
		t.Fatal("Rust plan fixture has no deadline")
	}
	admission, err := planescapeprovider.NewSessionAdmission(
		planescapeprovider.SessionAdmissionInput{
			SessionID:             source.tool.record.SessionID().String(),
			ProviderID:            source.plan.ProviderID().String(),
			ProviderEpoch:         source.plan.ProviderEpoch().Uint64(),
			RequirementHash:       source.plan.Requirement().CanonicalHash().String(),
			CompiledPlanHash:      source.plan.CanonicalHash().String(),
			SessionCapabilityHash: source.plan.ProviderCapabilityHash().String(),
			ExpiresAt:             deadline,
			CanonicalHash:         planescapeProductHash("2"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	toolResponse, err := codec.DecodeOperation(
		[]byte(fixture.ProviderToolRPC.Result.CanonicalJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := toolResponse.(planescapeprovider.OperationResult)
	if !ok {
		t.Fatal("Rust Tool response is not an operation result")
	}
	quiescenceResponse, err := codec.DecodeOperation(
		[]byte(
			fixture.ProviderQuiescenceRPC.Quiescence.CanonicalJSON,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	quiescence, ok := quiescenceResponse.(planescapeprovider.Quiescence)
	if !ok {
		t.Fatal("Rust Pause response is not quiescence")
	}
	freeze, err := codec.DecodeFreezeAck(
		[]byte(
			fixture.ProviderTerminalRPC.Freeze.ResponseRecord.CanonicalJSON,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	closeoutResponse, err := codec.DecodeOperation(
		[]byte(
			fixture.ProviderTerminalRPC.Closeout.ResponseRecord.CanonicalJSON,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	closeout, ok := closeoutResponse.(planescapeprovider.Closeout)
	if !ok {
		t.Fatal("Rust Closeout response is not closeout")
	}
	return capabilities, admission, tool, quiescence, freeze, closeout
}

func TestPlanescapeProductAuthorityHashHelper(t *testing.T) {
	value := []byte("authority")
	digest := sha256.Sum256(value)
	want := "sha256:" + hex.EncodeToString(digest[:])
	if got := planescapeProductAuthorityBytesSHA256(value); got != want {
		t.Fatalf("authority SHA-256 = %q, want %q", got, want)
	}
}
