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
// Rust plan source. Its session request ID is generated before compilation and
// is distinct from the provider-minted admitted session ID.
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

type planescapeProductCompiledPlanSource interface {
	CompiledContainmentPlan(
		context.Context,
		planescapeProductInvocation,
	) (planescapeProductCompiledPlanArtifact, error)
}

// planescapeProductOperationSource supplies only unbound operation intent.
// Session adds the exact admitted session, sequence, plan, and backend binding.
type planescapeProductOperationSource interface {
	ToolOperation(
		context.Context,
		planescapeProductBinding,
	) (planescapeprovider.OperationInput, error)
	QuiescenceOperation(
		context.Context,
		planescapeProductBinding,
		planescapeprovider.OperationResult,
	) (planescapeprovider.OperationInput, error)
}

type planescapeProductDependencies struct {
	Endpoint           planescapeprovider.BoundEndpoint
	CompiledPlanSource planescapeProductCompiledPlanSource
	OperationSource    planescapeProductOperationSource
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

type planescapeProductLifecycleResult struct {
	binding    planescapeProductBinding
	tool       planescapeprovider.OperationResult
	quiescence planescapeprovider.Quiescence
	evidence   planescapeprovider.EvidenceReferences
}

func (v planescapeProductLifecycleResult) valid() bool {
	return v.binding.valid() &&
		v.tool.SessionID() == v.binding.SessionID() &&
		v.quiescence.SessionID() == v.binding.SessionID() &&
		planescapeProductEvidenceMatches(v.tool, v.quiescence, v.evidence)
}

func (v planescapeProductLifecycleResult) Binding() planescapeProductBinding {
	return v.binding
}

func (v planescapeProductLifecycleResult) Tool() planescapeprovider.OperationResult {
	return v.tool
}

func (v planescapeProductLifecycleResult) Quiescence() planescapeprovider.Quiescence {
	return v.quiescence
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
	dependencies.Endpoint, err = configuredPlanescapeProductEndpoint(
		cfg.Session.Planescape,
	)
	if err != nil {
		return planescapeProductDependencies{}, mapPlanescapeProductError(err)
	}
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
	result, err := runPlanescapeProductLifecycle(
		ctx,
		admission,
		dependencies.OperationSource,
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

func runPlanescapeProductLifecycle(
	ctx context.Context,
	admission planescapeProductAdmission,
	source planescapeProductOperationSource,
) (planescapeProductLifecycleResult, error) {
	if ctx == nil || source == nil || !admission.valid() {
		return planescapeProductLifecycleResult{}, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	binding, ok := admission.Binding()
	if !ok {
		return planescapeProductLifecycleResult{}, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	toolInput, err := source.ToolOperation(ctx, binding)
	if err != nil {
		return planescapeProductLifecycleResult{}, mapPlanescapeProductError(err)
	}
	if toolInput.Kind() != planescapeprovider.OperationTool {
		return planescapeProductLifecycleResult{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	tool, err := admission.session.RunTool(ctx, toolInput)
	if err != nil {
		return planescapeProductLifecycleResult{}, mapPlanescapeProductError(err)
	}
	quiescenceInput, err := source.QuiescenceOperation(ctx, binding, tool)
	if err != nil {
		return planescapeProductLifecycleResult{}, mapPlanescapeProductError(err)
	}
	if quiescenceInput.Kind() != planescapeprovider.OperationPause {
		return planescapeProductLifecycleResult{}, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	quiescence, err := admission.session.Quiesce(ctx, quiescenceInput)
	if err != nil {
		return planescapeProductLifecycleResult{}, mapPlanescapeProductError(err)
	}
	evidence, err := admission.session.Evidence(ctx)
	if err != nil {
		return planescapeProductLifecycleResult{}, mapPlanescapeProductError(err)
	}
	if !planescapeProductEvidenceMatches(tool, quiescence, evidence) {
		return planescapeProductLifecycleResult{}, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	return planescapeProductLifecycleResult{
		binding:    binding,
		tool:       tool,
		quiescence: quiescence,
		evidence:   evidence,
	}, nil
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
