package hazmat

import (
	"context"
	"encoding/base64"
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

func TestDefaultPlanescapeProductDependenciesBuildExactProtectedEndpoint(
	t *testing.T,
) {
	isolateConfig(t)
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	hazmatConfig := defaultConfig()
	hazmatConfig.Session.ExecutionProvider =
		configmodel.ExecutionProviderPlanescape
	hazmatConfig.Session.Planescape = config
	if err := saveConfig(hazmatConfig); err != nil {
		t.Fatal(err)
	}

	dependencies, err := defaultPlanescapeProductDependencies()
	if err != nil {
		t.Fatal(err)
	}
	if dependencies.Endpoint == nil {
		t.Fatal("configured protected endpoint is absent")
	}
	expectedIdentity, err := planescapeprovider.ParseFingerprint(
		config.Backend.IdentitySHA256,
	)
	if err != nil {
		t.Fatal(err)
	}
	binding := dependencies.Endpoint.BackendBinding()
	if binding.IdentitySHA256() != expectedIdentity ||
		binding.ProviderEpoch().Uint64() != config.Backend.BrokerEpoch {
		t.Fatalf("endpoint binding = %+v, want exact configured identity", binding)
	}
	if dependencies.InvocationSource == nil ||
		dependencies.CompiledPlanSource == nil ||
		dependencies.OperationSource == nil ||
		dependencies.TerminalSource == nil {
		t.Fatal("default dependencies omitted configured Rust authority")
	}
	invocation, err := dependencies.InvocationSource.Invocation(
		"exec",
		[]string{"/usr/bin/true"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.SessionRequestID().String() !=
		"configured-authority-session" {
		t.Fatal("default dependencies did not retain exact Rust session binding")
	}
	wantCheckpointRoot := filepath.Join(
		filepath.Dir(configFilePath),
		"planescape-provider-checkpoints",
	)
	if dependencies.CheckpointRoot != wantCheckpointRoot {
		t.Fatalf(
			"checkpoint root = %q, want %q",
			dependencies.CheckpointRoot,
			wantCheckpointRoot,
		)
	}
}

func TestConfiguredPlanescapeProviderAbsenceDoesNotStartNativeRuntime(
	t *testing.T,
) {
	isolateConfig(t)
	hazmatConfig := defaultConfig()
	hazmatConfig.Session.ExecutionProvider =
		configmodel.ExecutionProviderPlanescape
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
	err := command.Execute()
	requirePlanescapeProductErrorClass(
		t,
		err,
		planescapeprovider.ErrorUnavailable,
	)
	if nativeRunnerCalls != 0 {
		t.Fatalf("native runner calls = %d, want 0", nativeRunnerCalls)
	}
	if err.Error() !=
		"configured Planescape provider failed closed: unavailable" {
		t.Fatalf("unredacted provider absence error: %q", err)
	}
}

func TestConfiguredEndpointWithoutRustAuthorityDoesNotDialOrFallback(
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
	config.InvocationAuthorityFile = ""
	config.InvocationAuthorityFileSHA256 = ""
	hazmatConfig := defaultConfig()
	hazmatConfig.Session.ExecutionProvider =
		configmodel.ExecutionProviderPlanescape
	hazmatConfig.Session.Planescape = config
	if err := saveConfig(hazmatConfig); err != nil {
		t.Fatal(err)
	}
	_, err = defaultPlanescapeProductDependencies()
	requirePlanescapeProductErrorClass(
		t,
		err,
		planescapeprovider.ErrorInvalid,
	)

	if err := tcpListener.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	connection, acceptErr := listener.Accept()
	if connection != nil {
		_ = connection.Close()
		t.Fatal("missing Rust authority still dialed the provider")
	}
	var networkError net.Error
	if !errors.As(acceptErr, &networkError) || !networkError.Timeout() {
		t.Fatalf("Accept error = %v, want timeout without a provider dial", acceptErr)
	}
}

func TestConfiguredPlanescapeProviderDeathDoesNotFallback(
	t *testing.T,
) {
	isolateConfig(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	config := planescapeProductProviderConfigFixture(t, target)
	config.DialTimeoutMS = 100
	hazmatConfig := defaultConfig()
	hazmatConfig.Session.ExecutionProvider =
		configmodel.ExecutionProviderPlanescape
	hazmatConfig.Session.Planescape = config
	if err := saveConfig(hazmatConfig); err != nil {
		t.Fatal(err)
	}
	dependencies, err := defaultPlanescapeProductDependencies()
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := dependencies.InvocationSource.Invocation(
		"exec",
		[]string{"/usr/bin/true"},
	)
	if err != nil {
		t.Fatal(err)
	}

	localStarts := 0
	_, err = runSessionStartupWithExecutionProvider(
		context.Background(),
		sessionConfig{
			ExecutionProvider: configmodel.ExecutionProviderPlanescape,
		},
		invocation,
		dependencies,
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
	if localStarts != 0 {
		t.Fatalf(
			"provider death reached fallback: local=%d",
			localStarts,
		)
	}
	for _, sensitive := range []string{
		target,
		config.ClientSigningSeedFile,
		config.ClientPublicKeyBase64URL,
	} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("provider death diagnostic leaked configuration: %v", err)
		}
	}
}

func TestConfiguredPlanescapeEndpointRejectsUnsafeKeySourceWithRedaction(
	t *testing.T,
) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	const secretPathComponent = "client-seed-secret-path"
	config.ClientSigningSeedFile = filepath.Join(
		t.TempDir(),
		secretPathComponent,
	)

	_, err := configuredPlanescapeProductEndpoint(config)
	requirePlanescapeProductErrorClass(
		t,
		err,
		planescapeprovider.ErrorUnavailable,
	)
	if strings.Contains(err.Error(), secretPathComponent) {
		t.Fatalf("key-source diagnostic leaked its path: %v", err)
	}
}

func TestConfiguredPlanescapeEndpointRejectsWrongSeedBeforeDial(t *testing.T) {
	config := planescapeProductProviderConfigFixture(
		t,
		"127.0.0.1:43191",
	)
	foreignSeed := make([]byte, planescapeProductSigningSeedSize)
	for index := range foreignSeed {
		foreignSeed[index] = 0x7f
	}
	if err := os.WriteFile(
		config.ClientSigningSeedFile,
		foreignSeed,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	clear(foreignSeed)

	_, err := configuredPlanescapeProductEndpoint(config)
	requirePlanescapeProductErrorClass(
		t,
		err,
		planescapeprovider.ErrorConflict,
	)
	if strings.Contains(err.Error(), config.ClientPublicKeyBase64URL) {
		t.Fatalf("wrong-key diagnostic leaked configured authority: %v", err)
	}
}

func planescapeProductProviderConfigFixture(
	t *testing.T,
	endpoint string,
) *configmodel.PlanescapeProviderConfig {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(
		"planescapeprovider",
		"testdata",
		"protected_broker.v1",
		"interop.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		BackendInstanceSHA256      string `json:"backend_instance_sha256"`
		ExecutableSHA256           string `json:"executable_sha256"`
		ExecutionEnvironmentSHA256 string `json:"execution_environment_sha256"`
		ProfileSHA256              string `json:"profile_sha256"`
		BrokerEpoch                uint64 `json:"broker_epoch"`
		BackendIdentitySHA256      string `json:"backend_identity_sha256"`
		BrokerPublicKeyBase64URL   string `json:"broker_public_key_base64url"`
		ClientPublicKeyBase64URL   string `json:"client_public_key_base64url"`
		ClientSeedBase64URL        string `json:"client_seed_base64url"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	seed, err := base64.RawURLEncoding.Strict().DecodeString(
		fixture.ClientSeedBase64URL,
	)
	if err != nil || len(seed) != planescapeProductSigningSeedSize {
		t.Fatal("protected-broker fixture has an invalid client seed")
	}
	seedPath := filepath.Join(t.TempDir(), "client-signing.seed")
	if err := os.WriteFile(seedPath, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	clear(seed)

	config := &configmodel.PlanescapeProviderConfig{
		Endpoint:                 endpoint,
		DialTimeoutMS:            250,
		BrokerPublicKeyBase64URL: fixture.BrokerPublicKeyBase64URL,
		ClientPublicKeyBase64URL: fixture.ClientPublicKeyBase64URL,
		ClientSigningSeedFile:    seedPath,
		Backend: configmodel.PlanescapeProviderBackendIdentityConfig{
			IdentitySHA256:             fixture.BackendIdentitySHA256,
			BackendInstanceSHA256:      fixture.BackendInstanceSHA256,
			ExecutableSHA256:           fixture.ExecutableSHA256,
			ExecutionEnvironmentSHA256: fixture.ExecutionEnvironmentSHA256,
			ProfileSHA256:              fixture.ProfileSHA256,
			BrokerEpoch:                fixture.BrokerEpoch,
		},
	}
	configurePlanescapeProductAuthorityFixture(
		t,
		config,
		"exec",
		[]string{"/usr/bin/true"},
		"configured-authority-session",
		planescapeProductAuthorityTerminalCloseoutV1,
	)
	return config
}
