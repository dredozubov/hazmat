package hazmat

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"hazmat/configmodel"
	"hazmat/planescapeprovider"
)

type planescapeProductFailureReason string

const (
	planescapeProductProviderFailure  planescapeProductFailureReason = "provider_failure"
	planescapeProductLifecyclePending planescapeProductFailureReason = "lifecycle_pending"
	planescapeProductTerminalPending  planescapeProductFailureReason = "terminal_pending"
	planescapeProductCancelled        planescapeProductFailureReason = "cancelled"
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
	case planescapeProductTerminalPending:
		return "configured Planescape provider reached quiescence, but terminal lifecycle execution is unavailable; local execution is disabled"
	case planescapeProductCancelled:
		return "configured Planescape provider cancelled the session; local execution is disabled"
	case planescapeProductLocalFallback:
		return "configured Planescape provider session cannot use a local execution path"
	case planescapeProductProviderFailure:
		return "configured Planescape provider failed closed: " + string(e.class)
	default:
		return "configured Planescape provider failed closed"
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

const (
	maxPlanescapeProductCommandBytes   = 128
	maxPlanescapeProductForwardedArgs  = 1024
	maxPlanescapeProductArgumentBytes  = 16 * 1024
	maxPlanescapeProductArgumentsBytes = 64 * 1024
)

// planescapeProductInvocation is the exact product request presented to the
// Rust plan source. Its externally authored session request ID is distinct
// from the provider's deterministic admitted session ID.
type planescapeProductInvocation struct {
	commandName      string
	forwardedArgs    []string
	sessionRequestID planescapeprovider.Identifier
}

func newPlanescapeProductInvocation(
	commandName string,
	forwardedArgs []string,
	sessionRequestID string,
) (planescapeProductInvocation, error) {
	id, err := planescapeprovider.NewIdentifier(sessionRequestID)
	if err != nil ||
		!validPlanescapeProductCommandName(commandName) ||
		!validPlanescapeProductForwardedArgs(forwardedArgs) {
		return planescapeProductInvocation{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	return planescapeProductInvocation{
		commandName:      commandName,
		forwardedArgs:    slices.Clone(forwardedArgs),
		sessionRequestID: id,
	}, nil
}

func validPlanescapeProductCommandName(value string) bool {
	return value != "" &&
		value == strings.TrimSpace(value) &&
		len(value) <= maxPlanescapeProductCommandBytes &&
		utf8.ValidString(value) &&
		!strings.ContainsFunc(value, func(r rune) bool {
			return r < 0x20 || r == 0x7f
		})
}

func validPlanescapeProductForwardedArgs(values []string) bool {
	if len(values) > maxPlanescapeProductForwardedArgs {
		return false
	}
	total := 0
	for _, value := range values {
		if !utf8.ValidString(value) ||
			strings.ContainsRune(value, '\x00') ||
			len(value) > maxPlanescapeProductArgumentBytes {
			return false
		}
		total += len(value)
		if total > maxPlanescapeProductArgumentsBytes {
			return false
		}
	}
	return true
}

func (v planescapeProductInvocation) valid() bool {
	_, err := planescapeprovider.NewIdentifier(v.sessionRequestID.String())
	return err == nil &&
		validPlanescapeProductCommandName(v.commandName) &&
		validPlanescapeProductForwardedArgs(v.forwardedArgs)
}

func (v planescapeProductInvocation) clone() planescapeProductInvocation {
	v.forwardedArgs = slices.Clone(v.forwardedArgs)
	return v
}

func (v planescapeProductInvocation) matches(other planescapeProductInvocation) bool {
	return v.valid() &&
		other.valid() &&
		v.commandName == other.commandName &&
		v.sessionRequestID == other.sessionRequestID &&
		slices.Equal(v.forwardedArgs, other.forwardedArgs)
}

func (v planescapeProductInvocation) CommandName() string {
	return v.commandName
}

func (v planescapeProductInvocation) ForwardedArgs() []string {
	return slices.Clone(v.forwardedArgs)
}

func (v planescapeProductInvocation) SessionRequestID() planescapeprovider.Identifier {
	return v.sessionRequestID
}

// planescapeProductCompiledPlanArtifact is an opaque, invocation-bound result
// from a Rust plan source. A production source must verify the Rust artifact's
// cryptographic plan/invocation binding before constructing this value.
type planescapeProductCompiledPlanArtifact struct {
	plan       planescapeprovider.CompiledContainmentPlan
	invocation planescapeProductInvocation
}

func newPlanescapeProductCompiledPlanArtifact(
	plan planescapeprovider.CompiledContainmentPlan,
	invocation planescapeProductInvocation,
) (planescapeProductCompiledPlanArtifact, error) {
	if !invocation.valid() {
		return planescapeProductCompiledPlanArtifact{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	if _, err := planescapeprovider.NewAdmissionInput(plan); err != nil {
		return planescapeProductCompiledPlanArtifact{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	return planescapeProductCompiledPlanArtifact{
		plan:       plan,
		invocation: invocation.clone(),
	}, nil
}

func (v planescapeProductCompiledPlanArtifact) valid() bool {
	if !v.invocation.valid() {
		return false
	}
	_, err := planescapeprovider.NewAdmissionInput(v.plan)
	return err == nil
}

// planescapeProductInvocationSource binds the actual command and forwarded
// arguments to the session request identity authored outside Go.
type planescapeProductInvocationSource interface {
	Invocation(string, []string) (planescapeProductInvocation, error)
}

type planescapeProductCompiledPlanSource interface {
	CompiledContainmentPlan(
		context.Context,
		planescapeProductInvocation,
	) (planescapeProductCompiledPlanArtifact, error)
}

// planescapeProductPostToolIntent is closed so a source must explicitly select
// either the pause/freeze/closeout path or the cancellation path.
type planescapeProductPostToolIntent interface {
	planescapeProductPostToolIntent()
	valid() bool
}

type planescapeProductPauseIntent struct {
	operation planescapeprovider.OperationInput
}

func newPlanescapeProductPauseIntent(
	operation planescapeprovider.OperationInput,
) (planescapeProductPauseIntent, error) {
	if !validPlanescapeProductOperationInput(
		operation,
		planescapeprovider.OperationPause,
	) {
		return planescapeProductPauseIntent{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	return planescapeProductPauseIntent{operation: operation}, nil
}

func (planescapeProductPauseIntent) planescapeProductPostToolIntent() {}

func (v planescapeProductPauseIntent) valid() bool {
	return validPlanescapeProductOperationInput(
		v.operation,
		planescapeprovider.OperationPause,
	)
}

type planescapeProductCancellationIntent struct {
	input planescapeprovider.CancellationInput
}

func newPlanescapeProductCancellationIntent(
	input planescapeprovider.CancellationInput,
) (planescapeProductCancellationIntent, error) {
	if !validPlanescapeProductCancellationInput(input) {
		return planescapeProductCancellationIntent{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	return planescapeProductCancellationIntent{input: input}, nil
}

func (planescapeProductCancellationIntent) planescapeProductPostToolIntent() {}

func (v planescapeProductCancellationIntent) valid() bool {
	return validPlanescapeProductCancellationInput(v.input)
}

// planescapeProductOperationSource supplies only Rust-authored unbound intent.
// Session adds the exact admitted session, sequence, plan, and backend binding.
type planescapeProductOperationSource interface {
	ToolOperation(
		context.Context,
		planescapeProductBinding,
	) (planescapeprovider.OperationInput, error)
	PostToolIntent(
		context.Context,
		planescapeProductBinding,
		planescapeprovider.OperationResult,
	) (planescapeProductPostToolIntent, error)
}

// planescapeProductTerminalSource supplies Rust-authored terminal intent for
// one exact quiesced lifecycle. Hazmat binds that intent to the admitted
// session and prior provider evidence; it never derives terminal authority.
type planescapeProductTerminalSource interface {
	FreezeInput(
		context.Context,
		planescapeProductQuiescedLifecycle,
	) (planescapeprovider.FreezeInput, error)
	CloseoutIntent(
		context.Context,
		planescapeProductFrozenLifecycle,
	) (planescapeProductCloseoutIntent, error)
}

type planescapeProductDependencies struct {
	InvocationSource   planescapeProductInvocationSource
	Endpoint           planescapeprovider.BoundEndpoint
	CompiledPlanSource planescapeProductCompiledPlanSource
	OperationSource    planescapeProductOperationSource
	TerminalSource     planescapeProductTerminalSource
	CheckpointRoot     string
	Now                func() time.Time
}

type planescapeProductBinding struct {
	planHash   planescapeprovider.Fingerprint
	session    planescapeprovider.Identifier
	backend    planescapeprovider.BackendIdentityBinding
	invocation planescapeProductInvocation
}

func newPlanescapeProductBinding(
	plan planescapeprovider.CompiledContainmentPlan,
	session planescapeprovider.Session,
	backend planescapeprovider.BackendIdentityBinding,
	invocation planescapeProductInvocation,
) (planescapeProductBinding, error) {
	sessionID, ok := session.ID()
	if !ok ||
		!invocation.valid() ||
		backend.ProviderEpoch() != plan.ProviderEpoch() ||
		backend.IdentitySHA256().String() == "" {
		return planescapeProductBinding{}, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	return planescapeProductBinding{
		planHash:   plan.CanonicalHash(),
		session:    sessionID,
		backend:    backend,
		invocation: invocation.clone(),
	}, nil
}

func (v planescapeProductBinding) valid() bool {
	return v.planHash.String() != "" &&
		v.session.String() != "" &&
		v.backend.IdentitySHA256().String() != "" &&
		v.backend.ProviderEpoch().Uint64() != 0 &&
		v.invocation.valid()
}

func (v planescapeProductBinding) PlanHash() planescapeprovider.Fingerprint {
	return v.planHash
}

func (v planescapeProductBinding) SessionID() planescapeprovider.Identifier {
	return v.session
}

func (v planescapeProductBinding) Backend() planescapeprovider.BackendIdentityBinding {
	return v.backend
}

func (v planescapeProductBinding) Invocation() planescapeProductInvocation {
	return v.invocation.clone()
}

// planescapeProductAdmission retains the exact Rust-produced plan together
// with the client-bound admitted session. The zero value is inert.
type planescapeProductAdmission struct {
	input   planescapeprovider.AdmissionInput
	session planescapeprovider.Session
	binding planescapeProductBinding
}

func newPlanescapeProductAdmission(
	input planescapeprovider.AdmissionInput,
	session planescapeprovider.Session,
	backend planescapeprovider.BackendIdentityBinding,
	invocation planescapeProductInvocation,
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
	binding, err := newPlanescapeProductBinding(
		input.Plan(),
		session,
		backend,
		invocation,
	)
	if err != nil {
		return planescapeProductAdmission{}, err
	}
	return planescapeProductAdmission{
		input:   input,
		session: session,
		binding: binding,
	}, nil
}

func (v planescapeProductAdmission) valid() bool {
	if _, err := planescapeprovider.NewAdmissionInput(v.input.Plan()); err != nil {
		return false
	}
	_, ok := v.session.ID()
	return ok &&
		v.binding.valid() &&
		v.binding.planHash == v.input.Plan().CanonicalHash() &&
		v.binding.backend.ProviderEpoch() == v.input.Plan().ProviderEpoch()
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

func (v planescapeProductAdmission) Binding() (planescapeProductBinding, bool) {
	if !v.valid() {
		return planescapeProductBinding{}, false
	}
	return v.binding, true
}

type planescapeProductPostToolLifecycle interface {
	planescapeProductPostToolLifecycle()
	valid() bool
}

// planescapeProductQuiescedLifecycle is explicitly non-terminal. Its exact
// admitted session is retained so only Session can bind later terminal intent.
type planescapeProductQuiescedLifecycle struct {
	admission  planescapeProductAdmission
	tool       planescapeprovider.OperationResult
	quiescence planescapeprovider.Quiescence
	evidence   planescapeprovider.EvidenceReferences
}

func (planescapeProductQuiescedLifecycle) planescapeProductPostToolLifecycle() {}

func (v planescapeProductQuiescedLifecycle) valid() bool {
	return v.admission.valid() &&
		v.tool.SessionID() == v.admission.binding.SessionID() &&
		v.quiescence.SessionID() == v.admission.binding.SessionID() &&
		planescapeProductEvidenceMatches(v.tool, v.quiescence, v.evidence)
}

func (v planescapeProductQuiescedLifecycle) Binding() planescapeProductBinding {
	return v.admission.binding
}

func (v planescapeProductQuiescedLifecycle) Tool() planescapeprovider.OperationResult {
	return v.tool
}

func (v planescapeProductQuiescedLifecycle) Quiescence() planescapeprovider.Quiescence {
	return v.quiescence
}

func (v planescapeProductQuiescedLifecycle) Evidence() planescapeprovider.EvidenceReferences {
	return v.evidence
}

type planescapeProductCancelledLifecycle struct {
	admission    planescapeProductAdmission
	tool         planescapeprovider.OperationResult
	cancellation planescapeprovider.CancellationAck
	evidence     planescapeprovider.EvidenceReferences
}

func (planescapeProductCancelledLifecycle) planescapeProductPostToolLifecycle() {}

func (v planescapeProductCancelledLifecycle) valid() bool {
	logicalEvidence, hasLogicalEvidence := v.evidence.LogicalEvidence()
	return v.admission.valid() &&
		v.tool.SessionID() == v.admission.binding.SessionID() &&
		validPlanescapeProductCancellationAck(v.cancellation) &&
		v.cancellation.SessionID() == v.admission.binding.SessionID() &&
		v.cancellation.TerminalOutcome() ==
			planescapeprovider.OutcomeCancelled &&
		hasLogicalEvidence &&
		logicalEvidence == v.cancellation.LogicalEvidenceHash() &&
		containsPlanescapeProductFingerprint(
			v.evidence.Artifacts(),
			v.tool.ArtifactHash(),
		) &&
		containsPlanescapeProductFingerprint(
			v.evidence.OperationEvidence(),
			v.tool.EvidenceHash(),
		)
}

type planescapeProductFrozenLifecycle struct {
	quiesced planescapeProductQuiescedLifecycle
	freeze   planescapeprovider.FreezeAck
}

func (v planescapeProductFrozenLifecycle) valid() bool {
	return v.quiesced.valid() &&
		validPlanescapeProductFreezeAck(v.freeze) &&
		v.freeze.SessionID() == v.quiesced.Binding().SessionID() &&
		v.freeze.QuiescenceHash() == v.quiesced.quiescence.QuiescenceHash()
}

func (v planescapeProductFrozenLifecycle) Binding() planescapeProductBinding {
	return v.quiesced.Binding()
}

func (v planescapeProductFrozenLifecycle) Tool() planescapeprovider.OperationResult {
	return v.quiesced.Tool()
}

func (v planescapeProductFrozenLifecycle) Quiescence() planescapeprovider.Quiescence {
	return v.quiesced.Quiescence()
}

func (v planescapeProductFrozenLifecycle) Evidence() planescapeprovider.EvidenceReferences {
	return v.quiesced.Evidence()
}

func (v planescapeProductFrozenLifecycle) Freeze() planescapeprovider.FreezeAck {
	return v.freeze
}

// planescapeProductCloseoutIntent keeps the provider operation and closeout
// identity inseparable. Its zero value cannot reach Session.Closeout.
type planescapeProductCloseoutIntent struct {
	operation  planescapeprovider.OperationInput
	closeoutID planescapeprovider.Identifier
}

func newPlanescapeProductCloseoutIntent(
	operation planescapeprovider.OperationInput,
	closeoutID string,
) (planescapeProductCloseoutIntent, error) {
	id, err := planescapeprovider.NewIdentifier(closeoutID)
	if err != nil || !validPlanescapeProductOperationInput(
		operation,
		planescapeprovider.OperationCloseout,
	) {
		return planescapeProductCloseoutIntent{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	return planescapeProductCloseoutIntent{
		operation:  operation,
		closeoutID: id,
	}, nil
}

func (v planescapeProductCloseoutIntent) valid() bool {
	_, err := planescapeprovider.NewIdentifier(v.closeoutID.String())
	return err == nil && validPlanescapeProductOperationInput(
		v.operation,
		planescapeprovider.OperationCloseout,
	)
}

// planescapeProductLifecycleResult represents only a provider-closed,
// successful lifecycle. Quiescence alone can never satisfy valid.
type planescapeProductLifecycleResult struct {
	frozen   planescapeProductFrozenLifecycle
	closeout planescapeprovider.Closeout
	evidence planescapeprovider.EvidenceReferences
}

func (v planescapeProductLifecycleResult) valid() bool {
	return v.frozen.valid() &&
		validPlanescapeProductCloseout(v.closeout) &&
		v.closeout.SessionID() == v.frozen.Binding().SessionID() &&
		v.closeout.QuiescenceHash() ==
			v.frozen.Quiescence().QuiescenceHash() &&
		v.closeout.TerminalOutcome() ==
			planescapeprovider.OutcomeSucceeded &&
		planescapeProductEvidenceMatches(
			v.frozen.Tool(),
			v.frozen.Quiescence(),
			v.evidence,
		) &&
		planescapeProductTerminalEvidenceMatches(v.closeout, v.evidence)
}

func (v planescapeProductLifecycleResult) Binding() planescapeProductBinding {
	return v.frozen.Binding()
}

func (v planescapeProductLifecycleResult) Tool() planescapeprovider.OperationResult {
	return v.frozen.Tool()
}

func (v planescapeProductLifecycleResult) Quiescence() planescapeprovider.Quiescence {
	return v.frozen.Quiescence()
}

func (v planescapeProductLifecycleResult) Freeze() planescapeprovider.FreezeAck {
	return v.frozen.Freeze()
}

func (v planescapeProductLifecycleResult) Closeout() planescapeprovider.Closeout {
	return v.closeout
}

func (v planescapeProductLifecycleResult) Evidence() planescapeprovider.EvidenceReferences {
	return v.evidence
}

func configuredSessionExecutionProvider() (configmodel.ExecutionProvider, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	return cfg.SessionExecutionProvider(), nil
}

type planescapeProductDependencyFactory func() (planescapeProductDependencies, error)

func defaultPlanescapeProductDependencies() (
	planescapeProductDependencies,
	error,
) {
	dependencies := planescapeProductDependencies{
		CheckpointRoot: filepath.Join(filepath.Dir(configFilePath), "planescape-provider-checkpoints"),
	}
	cfg, err := loadConfig()
	if err != nil {
		return planescapeProductDependencies{}, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	if cfg.SessionExecutionProvider() != configmodel.ExecutionProviderPlanescape {
		return dependencies, nil
	}
	source, err := configuredPlanescapeProductAuthoritySource(
		cfg.Session.Planescape,
	)
	if err != nil {
		return planescapeProductDependencies{}, mapPlanescapeProductError(err)
	}
	dependencies.Endpoint, err = configuredPlanescapeProductEndpoint(
		cfg.Session.Planescape,
	)
	if err != nil {
		return planescapeProductDependencies{}, mapPlanescapeProductError(err)
	}
	dependencies.InvocationSource = source
	dependencies.CompiledPlanSource = source
	dependencies.OperationSource = source
	dependencies.TerminalSource = source
	return dependencies, nil
}

var planescapeProductDependenciesForSession planescapeProductDependencyFactory = defaultPlanescapeProductDependencies

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
// configured Planescape provider must complete the external lifecycle and
// returns that completion instead of authorizing the local startup path.
func runSessionStartupWithExecutionProvider(
	ctx context.Context,
	cfg sessionConfig,
	invocation planescapeProductInvocation,
	dependencies planescapeProductDependencies,
	localStartup func() error,
) (*planescapeProductLifecycleResult, error) {
	switch cfg.ExecutionProvider {
	case configmodel.ExecutionProviderLocal:
		if localStartup == nil {
			return nil, newPlanescapeProductError(
				planescapeprovider.ErrorInvalid,
				planescapeProductLocalFallback,
			)
		}
		return nil, localStartup()
	case configmodel.ExecutionProviderPlanescape:
	default:
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	if ctx == nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	if !invocation.valid() {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}

	admission, err := admitPlanescapeProductSession(ctx, invocation, dependencies)
	if err != nil {
		return nil, err
	}
	if dependencies.OperationSource == nil {
		return nil, newPlanescapeProductLifecyclePendingError(admission)
	}
	continuation, err := advancePlanescapeProductLifecycle(
		ctx,
		admission,
		dependencies.OperationSource,
	)
	if err != nil {
		return nil, err
	}
	quiesced, ok := continuation.(planescapeProductQuiescedLifecycle)
	if !ok {
		cancelled, cancelledOK :=
			continuation.(planescapeProductCancelledLifecycle)
		if !cancelledOK || !cancelled.valid() {
			return nil, newPlanescapeProductError(
				planescapeprovider.ErrorConflict,
				planescapeProductProviderFailure,
			)
		}
		return nil, newPlanescapeProductCancelledError()
	}
	if dependencies.TerminalSource == nil {
		return nil, newPlanescapeProductTerminalPendingError()
	}
	result, err := closePlanescapeProductLifecycle(
		ctx,
		quiesced,
		dependencies.TerminalSource,
	)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func admitPlanescapeProductSession(
	ctx context.Context,
	invocation planescapeProductInvocation,
	dependencies planescapeProductDependencies,
) (planescapeProductAdmission, error) {
	if ctx == nil || !invocation.valid() {
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
	artifact, err := dependencies.CompiledPlanSource.CompiledContainmentPlan(
		ctx,
		invocation.clone(),
	)
	if err != nil {
		return planescapeProductAdmission{}, mapPlanescapeProductError(err)
	}
	if !artifact.valid() {
		return planescapeProductAdmission{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	if !artifact.invocation.matches(invocation) {
		return planescapeProductAdmission{}, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	plan := artifact.plan
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
	return newPlanescapeProductAdmission(
		input,
		session,
		dependencies.Endpoint.BackendBinding(),
		invocation,
	)
}

func advancePlanescapeProductLifecycle(
	ctx context.Context,
	admission planescapeProductAdmission,
	source planescapeProductOperationSource,
) (planescapeProductPostToolLifecycle, error) {
	if ctx == nil || source == nil || !admission.valid() {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	binding, ok := admission.Binding()
	if !ok {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	toolInput, err := source.ToolOperation(ctx, binding)
	if err != nil {
		return nil, mapPlanescapeProductError(err)
	}
	if toolInput.Kind() != planescapeprovider.OperationTool {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	tool, err := admission.session.RunTool(ctx, toolInput)
	if err != nil {
		return nil, mapPlanescapeProductError(err)
	}
	intent, err := source.PostToolIntent(ctx, binding, tool)
	if err != nil {
		return nil, mapPlanescapeProductError(err)
	}
	if intent == nil || !intent.valid() {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	switch value := intent.(type) {
	case planescapeProductPauseIntent:
		quiescence, err := admission.session.Quiesce(
			ctx,
			value.operation,
		)
		if err != nil {
			return nil, mapPlanescapeProductError(err)
		}
		evidence, err := admission.session.Evidence(ctx)
		if err != nil {
			return nil, mapPlanescapeProductError(err)
		}
		if !planescapeProductEvidenceMatches(tool, quiescence, evidence) {
			return nil, newPlanescapeProductError(
				planescapeprovider.ErrorConflict,
				planescapeProductProviderFailure,
			)
		}
		return planescapeProductQuiescedLifecycle{
			admission:  admission,
			tool:       tool,
			quiescence: quiescence,
			evidence:   evidence,
		}, nil
	case planescapeProductCancellationIntent:
		cancellation, err := admission.session.Cancel(ctx, value.input)
		if err != nil {
			return nil, mapPlanescapeProductError(err)
		}
		evidence, err := admission.session.Evidence(ctx)
		if err != nil {
			return nil, mapPlanescapeProductError(err)
		}
		cancelled := planescapeProductCancelledLifecycle{
			admission:    admission,
			tool:         tool,
			cancellation: cancellation,
			evidence:     evidence,
		}
		if !cancelled.valid() {
			return nil, newPlanescapeProductError(
				planescapeprovider.ErrorConflict,
				planescapeProductProviderFailure,
			)
		}
		return cancelled, nil
	default:
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
}

func closePlanescapeProductLifecycle(
	ctx context.Context,
	quiesced planescapeProductQuiescedLifecycle,
	source planescapeProductTerminalSource,
) (planescapeProductLifecycleResult, error) {
	if ctx == nil || source == nil || !quiesced.valid() {
		return planescapeProductLifecycleResult{}, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	freezeInput, err := source.FreezeInput(ctx, quiesced)
	if err != nil {
		return planescapeProductLifecycleResult{}, mapPlanescapeProductError(err)
	}
	if !validPlanescapeProductFreezeInput(freezeInput) {
		return planescapeProductLifecycleResult{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	freeze, err := quiesced.admission.session.Freeze(ctx, freezeInput)
	if err != nil {
		return planescapeProductLifecycleResult{}, mapPlanescapeProductError(err)
	}
	frozen := planescapeProductFrozenLifecycle{
		quiesced: quiesced,
		freeze:   freeze,
	}
	if !frozen.valid() {
		return planescapeProductLifecycleResult{}, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	closeoutIntent, err := source.CloseoutIntent(ctx, frozen)
	if err != nil {
		return planescapeProductLifecycleResult{}, mapPlanescapeProductError(err)
	}
	if !closeoutIntent.valid() {
		return planescapeProductLifecycleResult{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	closeout, err := quiesced.admission.session.Closeout(
		ctx,
		closeoutIntent.operation,
		closeoutIntent.closeoutID.String(),
	)
	if err != nil {
		return planescapeProductLifecycleResult{}, mapPlanescapeProductError(err)
	}
	evidence, err := quiesced.admission.session.Evidence(ctx)
	if err != nil {
		return planescapeProductLifecycleResult{}, mapPlanescapeProductError(err)
	}
	result := planescapeProductLifecycleResult{
		frozen:   frozen,
		closeout: closeout,
		evidence: evidence,
	}
	if !result.valid() {
		return planescapeProductLifecycleResult{}, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	return result, nil
}

func validPlanescapeProductOperationInput(
	input planescapeprovider.OperationInput,
	kind planescapeprovider.OperationKind,
) bool {
	if input.Kind() != kind {
		return false
	}
	_, err := planescapeprovider.NewOperationInput(
		planescapeprovider.OperationInputValues{
			OperationID:      input.OperationID().String(),
			Kind:             input.Kind(),
			Nonce:            input.Nonce().String(),
			PayloadHash:      input.PayloadHash().String(),
			NormalizedRecord: input.NormalizedRecord(),
		},
	)
	return err == nil
}

func validPlanescapeProductFreezeInput(
	input planescapeprovider.FreezeInput,
) bool {
	_, err := planescapeprovider.NewFreezeInput(
		planescapeprovider.FreezeInputValues{
			FreezeID: input.FreezeID().String(),
			Nonce:    input.Nonce().String(),
		},
	)
	return err == nil
}

func validPlanescapeProductFreezeAck(
	value planescapeprovider.FreezeAck,
) bool {
	_, err := planescapeprovider.NewFreezeAck(
		planescapeprovider.FreezeAckInput{
			SessionID:      value.SessionID().String(),
			FreezeID:       value.FreezeID().String(),
			QuiescenceHash: value.QuiescenceHash().String(),
			FrozenAt:       value.FrozenAt(),
			CanonicalHash:  value.CanonicalHash().String(),
		},
	)
	return err == nil
}

func validPlanescapeProductCancellationInput(
	input planescapeprovider.CancellationInput,
) bool {
	_, err := planescapeprovider.NewCancellationInput(
		planescapeprovider.CancellationInputValues{
			CancellationID: input.CancellationID().String(),
			Reason:         input.Reason(),
			Nonce:          input.Nonce().String(),
		},
	)
	return err == nil
}

func validPlanescapeProductCancellationAck(
	value planescapeprovider.CancellationAck,
) bool {
	_, err := planescapeprovider.NewCancellationAck(
		planescapeprovider.CancellationAckInput{
			SessionID:           value.SessionID().String(),
			CancellationID:      value.CancellationID().String(),
			TerminalOutcome:     value.TerminalOutcome(),
			LogicalEvidenceHash: value.LogicalEvidenceHash().String(),
			CanonicalHash:       value.CanonicalHash().String(),
		},
	)
	return err == nil
}

func validPlanescapeProductCloseout(
	value planescapeprovider.Closeout,
) bool {
	_, err := planescapeprovider.NewCloseout(
		planescapeprovider.CloseoutInput{
			SessionID:           value.SessionID().String(),
			CloseoutID:          value.CloseoutID().String(),
			TerminalOutcome:     value.TerminalOutcome(),
			QuiescenceHash:      value.QuiescenceHash().String(),
			LogicalEvidenceHash: value.LogicalEvidenceHash().String(),
			NativeExtensionHash: value.NativeExtensionHash().String(),
			CanonicalHash:       value.CanonicalHash().String(),
		},
	)
	return err == nil
}

func planescapeProductEvidenceMatches(
	tool planescapeprovider.OperationResult,
	quiescence planescapeprovider.Quiescence,
	evidence planescapeprovider.EvidenceReferences,
) bool {
	if !containsPlanescapeProductFingerprint(
		evidence.Artifacts(),
		tool.ArtifactHash(),
	) || !containsPlanescapeProductFingerprint(
		evidence.OperationEvidence(),
		tool.EvidenceHash(),
	) {
		return false
	}
	resourceEvidence, ok := evidence.ResourceEvidence()
	return ok && resourceEvidence == quiescence.ResourceEvidenceHash()
}

func planescapeProductTerminalEvidenceMatches(
	closeout planescapeprovider.Closeout,
	evidence planescapeprovider.EvidenceReferences,
) bool {
	logicalEvidence, hasLogicalEvidence := evidence.LogicalEvidence()
	nativeExtension, hasNativeExtension := evidence.NativeExtension()
	return hasLogicalEvidence &&
		hasNativeExtension &&
		logicalEvidence == closeout.LogicalEvidenceHash() &&
		nativeExtension == closeout.NativeExtensionHash()
}

func containsPlanescapeProductFingerprint(
	values []planescapeprovider.Fingerprint,
	expected planescapeprovider.Fingerprint,
) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
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

func newPlanescapeProductTerminalPendingError() error {
	return &planescapeProductError{
		class:  planescapeprovider.ErrorUnsupported,
		reason: planescapeProductTerminalPending,
	}
}

func newPlanescapeProductCancelledError() error {
	return &planescapeProductError{
		class:  planescapeprovider.ErrorUnavailable,
		reason: planescapeProductCancelled,
	}
}
