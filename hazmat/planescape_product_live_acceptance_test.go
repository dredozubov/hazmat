package hazmat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"hazmat/configmodel"
	"hazmat/planescapeprovider"
)

const (
	planescapeLiveAcceptanceOptInEnv = "HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE"
	planescapeLiveAcceptanceTimeout  = 2 * time.Minute
	planescapeLiveHandoffSchemaV1    = "hazmat.planescape.live_acceptance_handoff.v1"
)

type planescapeLiveAcceptancePhase string

const (
	planescapeLivePhaseLifecycle     planescapeLiveAcceptancePhase = "lifecycle"
	planescapeLivePhaseRestartPrime  planescapeLiveAcceptancePhase = "restart-prime"
	planescapeLivePhaseRestartReplay planescapeLiveAcceptancePhase = "restart-replay"
	planescapeLivePhaseUnavailable   planescapeLiveAcceptancePhase = "unavailable"
	planescapeLivePhaseDenial        planescapeLiveAcceptancePhase = "denial"
)

type planescapeLivePreflightReason string

const (
	planescapeLivePreflightMissingInput      planescapeLivePreflightReason = "missing-input"
	planescapeLivePreflightInvalidPhase      planescapeLivePreflightReason = "invalid-phase"
	planescapeLivePreflightInvalidEndpoint   planescapeLivePreflightReason = "invalid-endpoint"
	planescapeLivePreflightInvalidPath       planescapeLivePreflightReason = "invalid-path"
	planescapeLivePreflightUnsafeFile        planescapeLivePreflightReason = "unsafe-file"
	planescapeLivePreflightUnsafeDirectory   planescapeLivePreflightReason = "unsafe-directory"
	planescapeLivePreflightInvalidHash       planescapeLivePreflightReason = "invalid-hash"
	planescapeLivePreflightInvalidHandoff    planescapeLivePreflightReason = "invalid-handoff"
	planescapeLivePreflightInvalidErrorClass planescapeLivePreflightReason = "invalid-error-class"
)

type planescapeLivePreflightError struct {
	reason planescapeLivePreflightReason
}

func (e *planescapeLivePreflightError) Error() string {
	return "configured Planescape live acceptance preflight rejected"
}

type planescapeLiveAcceptanceInputs struct {
	phase              planescapeLiveAcceptancePhase
	endpoint           netip.AddrPort
	endpointText       string
	configFile         string
	authorityFile      string
	authoritySHA256    planescapeprovider.Fingerprint
	clientSeedFile     string
	checkpointRoot     string
	handoffFile        string
	expectedErrorClass planescapeprovider.ErrorClass
}

type planescapeLiveHandoffV1 struct {
	Schema                 string `json:"schema"`
	SessionID              string `json:"session_id"`
	PlanSHA256             string `json:"plan_sha256"`
	BackendIdentitySHA256  string `json:"backend_identity_sha256"`
	ToolOperationID        string `json:"tool_operation_id"`
	ToolResultSHA256       string `json:"tool_result_sha256"`
	PauseOperationID       string `json:"pause_operation_id"`
	QuiescenceResultSHA256 string `json:"quiescence_result_sha256"`
}

type planescapeLiveComposition struct {
	dependencies planescapeProductDependencies
	source       *planescapeProductFileAuthoritySource
	invocation   planescapeProductInvocation
}

func TestConfiguredPlanescapeProviderLiveAcceptance(t *testing.T) {
	if os.Getenv(planescapeLiveAcceptanceOptInEnv) != "1" {
		t.Skip("configured Planescape live acceptance is explicitly opt-in")
	}

	inputs, err := loadPlanescapeLiveAcceptanceInputs(os.LookupEnv)
	if err != nil {
		t.Fatal("configured Planescape live acceptance failed: preflight")
	}
	savedConfigPath := configFilePath
	configFilePath = inputs.configFile
	t.Cleanup(func() {
		configFilePath = savedConfigPath
	})

	composition, err := loadPlanescapeLiveComposition(inputs)
	if err != nil {
		t.Fatal("configured Planescape live acceptance failed: composition")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		planescapeLiveAcceptanceTimeout,
	)
	defer cancel()

	switch inputs.phase {
	case planescapeLivePhaseLifecycle:
		err = runPlanescapeLiveLifecycle(ctx, composition)
	case planescapeLivePhaseRestartPrime:
		err = runPlanescapeLiveRestartPrime(
			ctx,
			composition,
			inputs.handoffFile,
		)
	case planescapeLivePhaseRestartReplay:
		err = runPlanescapeLiveRestartReplay(
			ctx,
			composition,
			inputs.handoffFile,
		)
	case planescapeLivePhaseUnavailable, planescapeLivePhaseDenial:
		err = runPlanescapeLiveFailure(
			ctx,
			composition,
			inputs,
		)
	default:
		err = &planescapeLivePreflightError{
			reason: planescapeLivePreflightInvalidPhase,
		}
	}
	if err != nil {
		t.Fatal("configured Planescape live acceptance failed")
	}
}

func loadPlanescapeLiveAcceptanceInputs(
	lookup func(string) (string, bool),
) (planescapeLiveAcceptanceInputs, error) {
	required := func(name string) (string, error) {
		value, ok := lookup(name)
		if !ok || value == "" {
			return "", &planescapeLivePreflightError{
				reason: planescapeLivePreflightMissingInput,
			}
		}
		return value, nil
	}

	phaseText, err := required(
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_PHASE",
	)
	if err != nil {
		return planescapeLiveAcceptanceInputs{}, err
	}
	phase, err := parsePlanescapeLiveAcceptancePhase(phaseText)
	if err != nil {
		return planescapeLiveAcceptanceInputs{}, err
	}
	endpointText, err := required(
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_ENDPOINT",
	)
	if err != nil {
		return planescapeLiveAcceptanceInputs{}, err
	}
	endpoint, err := netip.ParseAddrPort(endpointText)
	if err != nil ||
		endpoint.Port() == 0 ||
		endpoint.Addr().IsUnspecified() ||
		endpoint.Addr().Zone() != "" ||
		endpoint.String() != endpointText {
		return planescapeLiveAcceptanceInputs{},
			&planescapeLivePreflightError{
				reason: planescapeLivePreflightInvalidEndpoint,
			}
	}

	configFile, err := required(
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_CONFIG_FILE",
	)
	if err != nil {
		return planescapeLiveAcceptanceInputs{}, err
	}
	authorityFile, err := required(
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_AUTHORITY_FILE",
	)
	if err != nil {
		return planescapeLiveAcceptanceInputs{}, err
	}
	authorityHashText, err := required(
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_AUTHORITY_SHA256",
	)
	if err != nil {
		return planescapeLiveAcceptanceInputs{}, err
	}
	authoritySHA256, err := planescapeprovider.ParseFingerprint(
		authorityHashText,
	)
	if err != nil {
		return planescapeLiveAcceptanceInputs{},
			&planescapeLivePreflightError{
				reason: planescapeLivePreflightInvalidHash,
			}
	}
	clientSeedFile, err := required(
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_CLIENT_SEED_FILE",
	)
	if err != nil {
		return planescapeLiveAcceptanceInputs{}, err
	}
	checkpointRoot, err := required(
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_CHECKPOINT_ROOT",
	)
	if err != nil {
		return planescapeLiveAcceptanceInputs{}, err
	}
	handoffFile, _ := lookup(
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_HANDOFF_FILE",
	)
	expectedClassText, _ := lookup(
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_EXPECTED_ERROR_CLASS",
	)

	for _, path := range []string{
		configFile,
		authorityFile,
		clientSeedFile,
		checkpointRoot,
	} {
		if !canonicalAbsolutePath(path) {
			return planescapeLiveAcceptanceInputs{},
				&planescapeLivePreflightError{
					reason: planescapeLivePreflightInvalidPath,
				}
		}
	}
	for _, path := range []string{
		configFile,
		authorityFile,
		clientSeedFile,
	} {
		if !securePlanescapeLivePrivateFile(path) {
			return planescapeLiveAcceptanceInputs{},
				&planescapeLivePreflightError{
					reason: planescapeLivePreflightUnsafeFile,
				}
		}
	}
	if !securePlanescapeLiveCheckpointRoot(checkpointRoot) {
		return planescapeLiveAcceptanceInputs{},
			&planescapeLivePreflightError{
				reason: planescapeLivePreflightUnsafeDirectory,
			}
	}

	expectedErrorClass, err := parsePlanescapeLiveExpectedErrorClass(
		phase,
		expectedClassText,
	)
	if err != nil {
		return planescapeLiveAcceptanceInputs{}, err
	}
	if err := validatePlanescapeLiveHandoffInput(phase, handoffFile); err != nil {
		return planescapeLiveAcceptanceInputs{}, err
	}

	return planescapeLiveAcceptanceInputs{
		phase:              phase,
		endpoint:           endpoint,
		endpointText:       endpointText,
		configFile:         configFile,
		authorityFile:      authorityFile,
		authoritySHA256:    authoritySHA256,
		clientSeedFile:     clientSeedFile,
		checkpointRoot:     checkpointRoot,
		handoffFile:        handoffFile,
		expectedErrorClass: expectedErrorClass,
	}, nil
}

func parsePlanescapeLiveAcceptancePhase(
	value string,
) (planescapeLiveAcceptancePhase, error) {
	phase := planescapeLiveAcceptancePhase(value)
	switch phase {
	case planescapeLivePhaseLifecycle,
		planescapeLivePhaseRestartPrime,
		planescapeLivePhaseRestartReplay,
		planescapeLivePhaseUnavailable,
		planescapeLivePhaseDenial:
		return phase, nil
	default:
		return "", &planescapeLivePreflightError{
			reason: planescapeLivePreflightInvalidPhase,
		}
	}
}

func parsePlanescapeLiveExpectedErrorClass(
	phase planescapeLiveAcceptancePhase,
	value string,
) (planescapeprovider.ErrorClass, error) {
	switch phase {
	case planescapeLivePhaseUnavailable:
		if value == "" {
			return planescapeprovider.ErrorUnavailable, nil
		}
	case planescapeLivePhaseDenial:
		class := planescapeprovider.ErrorClass(value)
		switch class {
		case planescapeprovider.ErrorInvalid,
			planescapeprovider.ErrorUnsupported,
			planescapeprovider.ErrorConflict:
			return class, nil
		}
	default:
		if value == "" {
			return "", nil
		}
	}
	return "", &planescapeLivePreflightError{
		reason: planescapeLivePreflightInvalidErrorClass,
	}
}

func validatePlanescapeLiveHandoffInput(
	phase planescapeLiveAcceptancePhase,
	path string,
) error {
	switch phase {
	case planescapeLivePhaseRestartPrime:
		if !canonicalAbsolutePath(path) ||
			!securePlanescapeLivePrivateDirectory(filepath.Dir(path)) {
			return &planescapeLivePreflightError{
				reason: planescapeLivePreflightInvalidHandoff,
			}
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return &planescapeLivePreflightError{
				reason: planescapeLivePreflightInvalidHandoff,
			}
		}
	case planescapeLivePhaseRestartReplay:
		if !canonicalAbsolutePath(path) ||
			!securePlanescapeLivePrivateFile(path) {
			return &planescapeLivePreflightError{
				reason: planescapeLivePreflightInvalidHandoff,
			}
		}
	default:
		if path != "" {
			return &planescapeLivePreflightError{
				reason: planescapeLivePreflightInvalidHandoff,
			}
		}
	}
	return nil
}

func canonicalAbsolutePath(path string) bool {
	return path != "" &&
		filepath.IsAbs(path) &&
		filepath.Clean(path) == path
}

func securePlanescapeLivePrivateFile(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !validPlanescapeProductSecureRegularFile(info) {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	openedInfo, statErr := file.Stat()
	closeErr := file.Close()
	return statErr == nil &&
		closeErr == nil &&
		os.SameFile(info, openedInfo) &&
		validPlanescapeProductSecureRegularFile(openedInfo)
}

func securePlanescapeLiveCheckpointRoot(path string) bool {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		return securePlanescapeLivePrivateDirectoryInfo(info)
	case errors.Is(err, os.ErrNotExist):
		return securePlanescapeLivePrivateDirectory(filepath.Dir(path))
	default:
		return false
	}
}

func securePlanescapeLivePrivateDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && securePlanescapeLivePrivateDirectoryInfo(info)
}

func securePlanescapeLivePrivateDirectoryInfo(info os.FileInfo) bool {
	if info == nil ||
		!info.IsDir() ||
		info.Mode().Perm() != 0o700 ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky|os.ModeSymlink) != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func loadPlanescapeLiveComposition(
	inputs planescapeLiveAcceptanceInputs,
) (planescapeLiveComposition, error) {
	cfg, err := loadConfig()
	if err != nil ||
		cfg.SessionExecutionProvider() !=
			configmodel.ExecutionProviderPlanescape ||
		cfg.Session.Planescape == nil {
		return planescapeLiveComposition{}, errors.New(
			"configured Planescape composition unavailable",
		)
	}
	config := cfg.Session.Planescape
	if config.Endpoint != inputs.endpointText ||
		config.InvocationAuthorityFile != inputs.authorityFile ||
		config.InvocationAuthorityFileSHA256 !=
			inputs.authoritySHA256.String() ||
		config.ClientSigningSeedFile != inputs.clientSeedFile ||
		filepath.Join(
			filepath.Dir(inputs.configFile),
			"planescape-provider-checkpoints",
		) != inputs.checkpointRoot {
		return planescapeLiveComposition{}, errors.New(
			"configured Planescape composition mismatch",
		)
	}

	dependencies, err := defaultPlanescapeProductDependencies()
	if err != nil ||
		dependencies.CheckpointRoot != inputs.checkpointRoot {
		return planescapeLiveComposition{}, errors.New(
			"configured Planescape dependencies unavailable",
		)
	}
	if _, ok := dependencies.Endpoint.(*planescapeprovider.ProtectedBrokerEndpointV1); !ok {
		return planescapeLiveComposition{}, errors.New(
			"configured Planescape endpoint is not protected",
		)
	}
	source, ok := dependencies.InvocationSource.(*planescapeProductFileAuthoritySource)
	if !ok ||
		dependencies.CompiledPlanSource != source ||
		dependencies.OperationSource != source ||
		dependencies.TerminalSource != source {
		return planescapeLiveComposition{}, errors.New(
			"configured Planescape authority composition mismatch",
		)
	}
	invocation, err := source.Invocation(
		source.invocation.CommandName(),
		source.invocation.ForwardedArgs(),
	)
	if err != nil || !invocation.matches(source.invocation) {
		return planescapeLiveComposition{}, errors.New(
			"configured Planescape invocation mismatch",
		)
	}
	return planescapeLiveComposition{
		dependencies: dependencies,
		source:       source,
		invocation:   invocation,
	}, nil
}

func runPlanescapeLiveLifecycle(
	ctx context.Context,
	composition planescapeLiveComposition,
) error {
	localStarts := 0
	result, err := runSessionStartupWithExecutionProvider(
		ctx,
		sessionConfig{
			ExecutionProvider: configmodel.ExecutionProviderPlanescape,
		},
		composition.invocation,
		composition.dependencies,
		func() error {
			localStarts++
			return errors.New("local fallback invoked")
		},
	)
	if err != nil ||
		localStarts != 0 ||
		result == nil ||
		!result.valid() ||
		result.Tool().ResultKind() != planescapeprovider.ResultCompleted ||
		result.Tool().Sequence().Uint64() != 1 ||
		result.Closeout().TerminalOutcome() !=
			planescapeprovider.OutcomeSucceeded ||
		!result.Binding().Invocation().matches(composition.invocation) {
		return errors.New("configured Planescape lifecycle failed")
	}
	return nil
}

func runPlanescapeLiveRestartPrime(
	ctx context.Context,
	composition planescapeLiveComposition,
	handoffPath string,
) error {
	admission, err := admitPlanescapeProductSession(
		ctx,
		composition.invocation,
		composition.dependencies,
	)
	if err != nil {
		return errors.New("configured Planescape restart prime admission failed")
	}
	continuation, err := advancePlanescapeProductLifecycle(
		ctx,
		admission,
		composition.source,
	)
	if err != nil {
		return errors.New("configured Planescape restart prime lifecycle failed")
	}
	quiesced, ok := continuation.(planescapeProductQuiescedLifecycle)
	if !ok || !quiesced.valid() {
		return errors.New("configured Planescape restart prime did not quiesce")
	}
	binding, ok := admission.Binding()
	if !ok {
		return errors.New("configured Planescape restart prime binding failed")
	}
	intent, err := composition.source.PostToolIntent(
		ctx,
		binding,
		quiesced.Tool(),
	)
	if err != nil {
		return errors.New("configured Planescape restart prime intent failed")
	}
	pause, ok := intent.(planescapeProductPauseIntent)
	if !ok || !pause.valid() {
		return errors.New("configured Planescape restart prime pause failed")
	}
	handoff := planescapeLiveHandoffV1{
		Schema:                planescapeLiveHandoffSchemaV1,
		SessionID:             binding.SessionID().String(),
		PlanSHA256:            binding.PlanHash().String(),
		BackendIdentitySHA256: binding.Backend().IdentitySHA256().String(),
		ToolOperationID:       quiesced.Tool().OperationID().String(),
		ToolResultSHA256:      quiesced.Tool().CanonicalHash().String(),
		PauseOperationID:      pause.operation.OperationID().String(),
		QuiescenceResultSHA256: quiesced.Quiescence().
			CanonicalHash().String(),
	}
	if !validPlanescapeLiveHandoff(handoff) {
		return errors.New("configured Planescape restart prime handoff invalid")
	}
	if err := writePlanescapeLiveHandoff(handoffPath, handoff); err != nil {
		return errors.New("configured Planescape restart prime handoff failed")
	}
	return nil
}

func runPlanescapeLiveRestartReplay(
	ctx context.Context,
	composition planescapeLiveComposition,
	handoffPath string,
) error {
	handoff, err := readPlanescapeLiveHandoff(handoffPath)
	if err != nil {
		return errors.New("configured Planescape restart handoff unavailable")
	}
	artifact, err := composition.source.CompiledContainmentPlan(
		ctx,
		composition.invocation,
	)
	if err != nil ||
		artifact.plan.CanonicalHash().String() != handoff.PlanSHA256 ||
		composition.dependencies.Endpoint.BackendBinding().
			IdentitySHA256().String() != handoff.BackendIdentitySHA256 {
		return errors.New("configured Planescape restart authority mismatch")
	}
	input, err := planescapeprovider.NewAdmissionInput(artifact.plan)
	if err != nil {
		return errors.New("configured Planescape restart plan invalid")
	}
	client, err := openPlanescapeProductClient(composition.dependencies)
	if err != nil {
		return errors.New("configured Planescape restart client unavailable")
	}
	discovery, err := client.Discover(ctx)
	if err != nil {
		return errors.New("configured Planescape restart discovery failed")
	}
	session, err := client.Reconnect(ctx, discovery, handoff.SessionID)
	if err != nil {
		return errors.New("configured Planescape restart reconnect failed")
	}
	admission, err := newPlanescapeProductAdmission(
		input,
		session,
		composition.dependencies.Endpoint.BackendBinding(),
		composition.invocation,
	)
	if err != nil {
		return errors.New("configured Planescape restart admission invalid")
	}
	binding, ok := admission.Binding()
	if !ok {
		return errors.New("configured Planescape restart binding invalid")
	}
	toolInput, err := composition.source.ToolOperation(ctx, binding)
	if err != nil ||
		toolInput.OperationID().String() != handoff.ToolOperationID {
		return errors.New("configured Planescape restart Tool authority mismatch")
	}
	toolResponse, err := session.Replay(ctx, handoff.ToolOperationID)
	if err != nil {
		return errors.New("configured Planescape restart Tool replay failed")
	}
	tool, ok := toolResponse.(planescapeprovider.OperationResult)
	if !ok ||
		tool.CanonicalHash().String() != handoff.ToolResultSHA256 ||
		tool.OperationID() != toolInput.OperationID() {
		return errors.New("configured Planescape restart Tool replay conflicted")
	}
	intent, err := composition.source.PostToolIntent(ctx, binding, tool)
	if err != nil {
		return errors.New("configured Planescape restart Pause authority failed")
	}
	pause, ok := intent.(planescapeProductPauseIntent)
	if !ok ||
		!pause.valid() ||
		pause.operation.OperationID().String() != handoff.PauseOperationID {
		return errors.New("configured Planescape restart Pause authority mismatch")
	}
	pauseResponse, err := session.Replay(ctx, handoff.PauseOperationID)
	if err != nil {
		return errors.New("configured Planescape restart Pause replay failed")
	}
	quiescence, ok := pauseResponse.(planescapeprovider.Quiescence)
	if !ok ||
		quiescence.CanonicalHash().String() !=
			handoff.QuiescenceResultSHA256 {
		return errors.New("configured Planescape restart Pause replay conflicted")
	}
	evidence, err := session.Evidence(ctx)
	if err != nil {
		return errors.New("configured Planescape restart evidence failed")
	}
	quiesced := planescapeProductQuiescedLifecycle{
		admission:  admission,
		tool:       tool,
		quiescence: quiescence,
		evidence:   evidence,
	}
	if !quiesced.valid() {
		return errors.New("configured Planescape restart quiescence invalid")
	}
	result, err := closePlanescapeProductLifecycle(
		ctx,
		quiesced,
		composition.source,
	)
	if err != nil ||
		!result.valid() ||
		result.Closeout().TerminalOutcome() !=
			planescapeprovider.OutcomeSucceeded {
		return errors.New("configured Planescape restart closeout failed")
	}
	return nil
}

func runPlanescapeLiveFailure(
	ctx context.Context,
	composition planescapeLiveComposition,
	inputs planescapeLiveAcceptanceInputs,
) error {
	localStarts := 0
	result, err := runSessionStartupWithExecutionProvider(
		ctx,
		sessionConfig{
			ExecutionProvider: configmodel.ExecutionProviderPlanescape,
		},
		composition.invocation,
		composition.dependencies,
		func() error {
			localStarts++
			return errors.New("local fallback invoked")
		},
	)
	if result != nil || err == nil || localStarts != 0 {
		return errors.New("configured Planescape failure reached fallback")
	}
	var productError *planescapeProductError
	if !errors.As(err, &productError) ||
		productError.Class() != inputs.expectedErrorClass ||
		err.Error() != "configured Planescape provider failed closed: "+
			string(inputs.expectedErrorClass) {
		return errors.New("configured Planescape failure class mismatch")
	}
	cfg, loadErr := loadConfig()
	if loadErr != nil || cfg.Session.Planescape == nil {
		return errors.New("configured Planescape failure config unavailable")
	}
	config := cfg.Session.Planescape
	for _, sensitive := range []string{
		inputs.endpointText,
		inputs.configFile,
		inputs.authorityFile,
		inputs.clientSeedFile,
		inputs.checkpointRoot,
		config.BrokerPublicKeyBase64URL,
		config.ClientPublicKeyBase64URL,
		config.Backend.IdentitySHA256,
		config.Backend.BackendInstanceSHA256,
		config.Backend.ExecutableSHA256,
		config.Backend.ExecutionEnvironmentSHA256,
		config.Backend.ProfileSHA256,
	} {
		if sensitive != "" && strings.Contains(err.Error(), sensitive) {
			return errors.New("configured Planescape failure diagnostic leaked")
		}
	}
	return nil
}

func validPlanescapeLiveHandoff(value planescapeLiveHandoffV1) bool {
	if value.Schema != planescapeLiveHandoffSchemaV1 {
		return false
	}
	for _, identifier := range []string{
		value.SessionID,
		value.ToolOperationID,
		value.PauseOperationID,
	} {
		if _, err := planescapeprovider.NewIdentifier(identifier); err != nil {
			return false
		}
	}
	if value.ToolOperationID == value.PauseOperationID {
		return false
	}
	for _, fingerprint := range []string{
		value.PlanSHA256,
		value.BackendIdentitySHA256,
		value.ToolResultSHA256,
		value.QuiescenceResultSHA256,
	} {
		if _, err := planescapeprovider.ParseFingerprint(fingerprint); err != nil {
			return false
		}
	}
	return true
}

func writePlanescapeLiveHandoff(
	path string,
	value planescapeLiveHandoffV1,
) error {
	if !validPlanescapeLiveHandoff(value) ||
		!canonicalAbsolutePath(path) ||
		!securePlanescapeLivePrivateDirectory(filepath.Dir(path)) {
		return errors.New("invalid live handoff")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("encode live handoff")
	}
	data = append(data, '\n')
	file, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return errors.New("create live handoff")
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return errors.New("write live handoff")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return errors.New("sync live handoff")
	}
	if err := file.Close(); err != nil {
		return errors.New("close live handoff")
	}
	if !securePlanescapeLivePrivateFile(path) {
		return errors.New("validate live handoff")
	}
	remove = false
	return nil
}

func readPlanescapeLiveHandoff(
	path string,
) (planescapeLiveHandoffV1, error) {
	if !securePlanescapeLivePrivateFile(path) {
		return planescapeLiveHandoffV1{}, errors.New("unsafe live handoff")
	}
	file, err := os.Open(path)
	if err != nil {
		return planescapeLiveHandoffV1{}, errors.New("open live handoff")
	}
	data, readErr := io.ReadAll(
		io.LimitReader(file, planescapeprovider.MaxRecordBytes+1),
	)
	closeErr := file.Close()
	if readErr != nil ||
		closeErr != nil ||
		len(data) == 0 ||
		len(data) > planescapeprovider.MaxRecordBytes {
		return planescapeLiveHandoffV1{}, errors.New("read live handoff")
	}
	var value planescapeLiveHandoffV1
	if err := decodePlanescapeProductAuthorityJSON(data, &value); err != nil ||
		!validPlanescapeLiveHandoff(value) {
		return planescapeLiveHandoffV1{}, errors.New("decode live handoff")
	}
	return value, nil
}

func TestPlanescapeLiveAcceptancePreflightRejectsMissingAndUnsafeInputs(
	t *testing.T,
) {
	t.Run("missing endpoint", func(t *testing.T) {
		values := planescapeLivePreflightFixture(t, planescapeLivePhaseLifecycle)
		delete(values, "HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_ENDPOINT")
		requirePlanescapeLivePreflightReason(
			t,
			values,
			planescapeLivePreflightMissingInput,
		)
	})
	t.Run("non-numeric endpoint", func(t *testing.T) {
		values := planescapeLivePreflightFixture(t, planescapeLivePhaseLifecycle)
		values["HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_ENDPOINT"] =
			"provider.invalid:43191"
		requirePlanescapeLivePreflightReason(
			t,
			values,
			planescapeLivePreflightInvalidEndpoint,
		)
	})
	t.Run("relative private path", func(t *testing.T) {
		values := planescapeLivePreflightFixture(t, planescapeLivePhaseLifecycle)
		values["HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_AUTHORITY_FILE"] =
			"authority.json"
		requirePlanescapeLivePreflightReason(
			t,
			values,
			planescapeLivePreflightInvalidPath,
		)
	})
	t.Run("world-readable seed", func(t *testing.T) {
		values := planescapeLivePreflightFixture(t, planescapeLivePhaseLifecycle)
		if err := os.Chmod(
			values["HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_CLIENT_SEED_FILE"],
			0o644,
		); err != nil {
			t.Fatal("failed to prepare unsafe live acceptance input")
		}
		requirePlanescapeLivePreflightReason(
			t,
			values,
			planescapeLivePreflightUnsafeFile,
		)
	})
	t.Run("symlinked authority", func(t *testing.T) {
		values := planescapeLivePreflightFixture(t, planescapeLivePhaseLifecycle)
		target := values["HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_AUTHORITY_FILE"]
		link := filepath.Join(filepath.Dir(target), "authority-link.json")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal("failed to prepare unsafe live acceptance input")
		}
		values["HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_AUTHORITY_FILE"] = link
		requirePlanescapeLivePreflightReason(
			t,
			values,
			planescapeLivePreflightUnsafeFile,
		)
	})
	t.Run("restart prime requires handoff", func(t *testing.T) {
		values := planescapeLivePreflightFixture(
			t,
			planescapeLivePhaseRestartPrime,
		)
		delete(values, "HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_HANDOFF_FILE")
		requirePlanescapeLivePreflightReason(
			t,
			values,
			planescapeLivePreflightInvalidHandoff,
		)
	})
	t.Run("denial requires exact class", func(t *testing.T) {
		values := planescapeLivePreflightFixture(t, planescapeLivePhaseDenial)
		values["HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_EXPECTED_ERROR_CLASS"] =
			"unavailable"
		requirePlanescapeLivePreflightReason(
			t,
			values,
			planescapeLivePreflightInvalidErrorClass,
		)
	})
}

func TestPlanescapeLiveAcceptanceScriptRedactsInputValues(t *testing.T) {
	const secretValue = "sensitive-input-value"
	script := filepath.Join(
		"..",
		"scripts",
		"check-planescape-configured-provider-live.sh",
	)
	command := exec.Command(
		"sh",
		script,
		"--run",
		"--i-understand-this-contacts-a-live-planescape-provider",
		"--phase",
		"lifecycle",
		"--endpoint",
		secretValue,
		"--config-file",
		secretValue,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("live acceptance script accepted incomplete inputs")
	}
	if strings.Contains(string(output), secretValue) ||
		string(output) !=
			"hazmat-planescape-live-acceptance: status=fail reason=missing-input\n" {
		t.Fatal("live acceptance script diagnostic was unstable or unredacted")
	}
}

func planescapeLivePreflightFixture(
	t *testing.T,
	phase planescapeLiveAcceptancePhase,
) map[string]string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal("failed to prepare private live acceptance directory")
	}
	configFile := filepath.Join(root, "config.yaml")
	authorityFile := filepath.Join(root, "authority.json")
	seedFile := filepath.Join(root, "client.seed")
	for _, path := range []string{configFile, authorityFile, seedFile} {
		if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
			t.Fatal("failed to prepare live acceptance input")
		}
	}
	values := map[string]string{
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_PHASE":          phase.String(),
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_ENDPOINT":       "127.0.0.1:43191",
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_CONFIG_FILE":    configFile,
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_AUTHORITY_FILE": authorityFile,
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_AUTHORITY_SHA256": "sha256:" +
			strings.Repeat("a", 64),
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_CLIENT_SEED_FILE": seedFile,
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_CHECKPOINT_ROOT": filepath.Join(
			root,
			"planescape-provider-checkpoints",
		),
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_HANDOFF_FILE":         "",
		"HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_EXPECTED_ERROR_CLASS": "",
	}
	switch phase {
	case planescapeLivePhaseRestartPrime:
		values["HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_HANDOFF_FILE"] =
			filepath.Join(root, "handoff.json")
	case planescapeLivePhaseRestartReplay:
		handoff := filepath.Join(root, "handoff.json")
		if err := os.WriteFile(handoff, []byte("{}"), 0o600); err != nil {
			t.Fatal("failed to prepare live acceptance input")
		}
		values["HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_HANDOFF_FILE"] = handoff
	case planescapeLivePhaseDenial:
		values["HAZMAT_PLANESCAPE_LIVE_ACCEPTANCE_EXPECTED_ERROR_CLASS"] =
			string(planescapeprovider.ErrorConflict)
	}
	return values
}

func (p planescapeLiveAcceptancePhase) String() string {
	return string(p)
}

func requirePlanescapeLivePreflightReason(
	t *testing.T,
	values map[string]string,
	want planescapeLivePreflightReason,
) {
	t.Helper()
	_, err := loadPlanescapeLiveAcceptanceInputs(
		func(name string) (string, bool) {
			value, ok := values[name]
			return value, ok
		},
	)
	var preflight *planescapeLivePreflightError
	if !errors.As(err, &preflight) {
		t.Fatal("live acceptance preflight returned an unclassified error")
	}
	if preflight.reason != want {
		t.Fatalf(
			"live acceptance preflight reason = %q, want %q",
			preflight.reason,
			want,
		)
	}
}
