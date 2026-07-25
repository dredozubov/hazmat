package planescapeprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const checkpointSchema = "hazmat.planescapeprovider.checkpoint.v2"

// Endpoint is the transport-independent provider boundary. A concrete endpoint
// owns authentication and wire framing; it must decode bounded v1 records
// before returning these constructor-validated values. This client never uses a
// legacy or in-process containment implementation when Endpoint is unavailable.
type Endpoint interface {
	Discover(context.Context) (ProviderCapabilities, error)
	Admit(context.Context, CompiledContainmentPlan) (SessionAdmission, error)
	Operate(context.Context, AgentOperation) (OperationResponse, error)
	Freeze(context.Context, Freeze) (FreezeAck, error)
	Cancel(context.Context, Cancellation) (CancellationAck, error)
}

// BoundEndpoint exposes the immutable backend identity authenticated by its
// transport. Client refuses endpoints without this proof and persists the
// binding with every admitted session.
type BoundEndpoint interface {
	Endpoint
	BackendBinding() BackendIdentityBinding
}

// CheckpointStore persists opaque, bounded client checkpoints. Implementations
// must durably save a request before Client sends an effect-bearing operation.
// Values contain only protocol bindings and hashes, never source, output, or
// credential bytes.
type CheckpointStore interface {
	Load(context.Context, string) ([]byte, bool, error)
	Save(context.Context, string, []byte) error
}

// MemoryStore is a zero-value-safe test and embedding store. Product callers
// that need reconnect/replay across process restarts must provide durable
// storage instead.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string][]byte
}

func (s *MemoryStore) Load(_ context.Context, key string) ([]byte, bool, error) {
	if s == nil {
		return nil, false, fmt.Errorf("nil checkpoint store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.records[key]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), value...), true, nil
}

func (s *MemoryStore) Save(_ context.Context, key string, value []byte) error {
	if s == nil {
		return fmt.Errorf("nil checkpoint store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = make(map[string][]byte)
	}
	s.records[key] = append([]byte(nil), value...)
	return nil
}

// ClientConfig has no default endpoint: a configured shared provider that is
// absent is unavailable, not permission to select weaker containment.
type ClientConfig struct {
	Endpoint BoundEndpoint
	Store    CheckpointStore
	Now      func() time.Time
}

// Client maps Hazmat product lifecycle calls to exact provider records.
type Client struct {
	endpoint       BoundEndpoint
	backendBinding BackendIdentityBinding
	store          CheckpointStore
	now            func() time.Time
	mu             sync.Mutex
}

func NewClient(config ClientConfig) (*Client, error) {
	if config.Endpoint == nil || config.Store == nil {
		return nil, clientError(ErrorUnavailable)
	}
	backendBinding := config.Endpoint.BackendBinding()
	if !backendBinding.valid() {
		return nil, clientError(ErrorUnavailable)
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		endpoint:       config.Endpoint,
		backendBinding: backendBinding,
		store:          config.Store,
		now:            now,
	}, nil
}

// Discovery is a client-bound, non-reserving provider declaration.
type Discovery struct {
	client       *Client
	capabilities ProviderCapabilities
}

func (d Discovery) Capabilities() ProviderCapabilities { return d.capabilities }

func (d Discovery) validFor(client *Client) bool {
	return client != nil && d.client == client && d.capabilities.valid()
}

func (c *Client) Discover(ctx context.Context) (Discovery, error) {
	if c == nil || c.endpoint == nil {
		return Discovery{}, clientError(ErrorUnavailable)
	}
	if err := c.validateEndpointBinding(); err != nil {
		return Discovery{}, err
	}
	capabilities, err := c.endpoint.Discover(ctx)
	if err != nil {
		return Discovery{}, c.endpointError(err, nil)
	}
	if !capabilities.valid() ||
		capabilities.ProviderEpoch() != c.backendBinding.ProviderEpoch() {
		return Discovery{}, clientError(ErrorConflict)
	}
	return Discovery{client: c, capabilities: capabilities}, nil
}

// Session is a product adapter bound to one durable provider session. Its zero
// value is deliberately inert; methods return a fail-closed error instead of
// performing any fallback work.
type Session struct {
	client *Client
	id     Identifier
}

func (s Session) ID() (Identifier, bool) {
	if s.client == nil || !s.id.valid() {
		return Identifier{}, false
	}
	return s.id, true
}

// Admit validates discovery against a Rust-produced containment plan, then
// persists its exact provider/session/plan bindings before returning a usable
// Session.
func (c *Client) Admit(ctx context.Context, discovery Discovery, input AdmissionInput) (Session, error) {
	if c == nil || c.endpoint == nil || c.store == nil {
		return Session{}, clientError(ErrorUnavailable)
	}
	if !discovery.validFor(c) || !input.valid() {
		return Session{}, clientError(ErrorInvalid)
	}
	capabilities := discovery.capabilities
	plan := input.plan
	if err := c.validateEndpointBinding(); err != nil {
		return Session{}, err
	}
	if plan.ProviderEpoch() != c.backendBinding.ProviderEpoch() {
		return Session{}, clientError(ErrorConflict)
	}
	if err := plan.ValidateProvider(capabilities); err != nil {
		return Session{}, clientError(ErrorConflict)
	}
	deadline, ok := plan.DeadlineAt()
	if !ok {
		return Session{}, clientError(ErrorInvalid)
	}
	if !deadline.After(c.now().UTC()) {
		return Session{}, clientError(ErrorUnavailable)
	}

	admission, err := c.endpoint.Admit(ctx, plan)
	if err != nil {
		return Session{}, c.endpointError(err, &capabilities)
	}
	if err := c.validateEndpointBinding(); err != nil {
		return Session{}, err
	}
	if err := plan.ValidateSessionAdmission(admission); err != nil {
		return Session{}, clientError(ErrorConflict)
	}
	if !admission.expiresAt.After(c.now().UTC()) {
		return Session{}, clientError(ErrorUnavailable)
	}

	state := sessionState{
		sessionID:             admission.sessionID,
		providerID:            plan.ProviderID(),
		epoch:                 plan.ProviderEpoch(),
		profile:               plan.ProviderProfile(),
		requirementHash:       plan.Requirement().CanonicalHash(),
		planHash:              plan.CanonicalHash(),
		sessionCapabilityHash: plan.ProviderCapabilityHash(),
		backendIdentityHash:   c.backendBinding.IdentitySHA256(),
		expiresAt:             deadline,
		phase:                 phaseAdmitted,
		nextSequence:          1,
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.saveState(ctx, state); err != nil {
		return Session{}, err
	}
	return Session{client: c, id: admission.sessionID}, nil
}

// Reconnect binds a new product client instance to a durable session only if
// the discovery identity, epoch, and profile remain exact. A restarted provider
// with a changed epoch therefore fails closed rather than reusing a handle.
func (c *Client) Reconnect(ctx context.Context, discovery Discovery, sessionID string) (Session, error) {
	if c == nil || c.store == nil || !discovery.validFor(c) {
		return Session{}, clientError(ErrorUnavailable)
	}
	id, err := NewIdentifier(sessionID)
	if err != nil {
		return Session{}, clientError(ErrorInvalid)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state, err := c.loadState(ctx, id)
	if err != nil {
		return Session{}, err
	}
	if err := c.validateUsableState(state, discovery.capabilities); err != nil {
		return Session{}, err
	}
	return Session{client: c, id: id}, nil
}

// Launch maps a product launch to one agent_start operation and the observed
// operation_result record.
func (s Session) Launch(ctx context.Context, input OperationInput) (OperationResult, error) {
	if input.kind != OperationAgentStart {
		return OperationResult{}, clientError(ErrorInvalid)
	}
	response, err := s.runOperation(
		ctx,
		input,
		operationPhaseAdmitted,
		responseOperationResult,
		Identifier{},
	)
	if err != nil {
		return OperationResult{}, err
	}
	result, ok := response.(OperationResult)
	if !ok {
		return OperationResult{}, clientError(ErrorConflict)
	}
	return result, nil
}

// RunTool maps one product tool action to an exact tool operation.
func (s Session) RunTool(ctx context.Context, input OperationInput) (OperationResult, error) {
	if input.kind != OperationTool {
		return OperationResult{}, clientError(ErrorInvalid)
	}
	response, err := s.runOperation(
		ctx,
		input,
		operationPhaseAdmittedOrActive,
		responseOperationResult,
		Identifier{},
	)
	if err != nil {
		return OperationResult{}, err
	}
	result, ok := response.(OperationResult)
	if !ok {
		return OperationResult{}, clientError(ErrorConflict)
	}
	return result, nil
}

// Workspace maps a product workspace action to an exact workspace operation.
func (s Session) Workspace(ctx context.Context, input OperationInput) (OperationResult, error) {
	if input.kind != OperationWorkspace {
		return OperationResult{}, clientError(ErrorInvalid)
	}
	response, err := s.runOperation(
		ctx,
		input,
		operationPhaseActive,
		responseOperationResult,
		Identifier{},
	)
	if err != nil {
		return OperationResult{}, err
	}
	result, ok := response.(OperationResult)
	if !ok {
		return OperationResult{}, clientError(ErrorConflict)
	}
	return result, nil
}

// Quiesce maps a pause operation to the provider's quiescence evidence.
func (s Session) Quiesce(ctx context.Context, input OperationInput) (Quiescence, error) {
	if input.kind != OperationPause {
		return Quiescence{}, clientError(ErrorInvalid)
	}
	response, err := s.runOperation(
		ctx,
		input,
		operationPhaseActive,
		responseQuiescence,
		Identifier{},
	)
	if err != nil {
		return Quiescence{}, err
	}
	result, ok := response.(Quiescence)
	if !ok {
		return Quiescence{}, clientError(ErrorConflict)
	}
	return result, nil
}

// Closeout maps the terminal client request to a provider closeout record with
// an explicit closeout identity, preventing an unrelated terminal record from
// being attached to a frozen session.
func (s Session) Closeout(ctx context.Context, input OperationInput, closeoutID string) (Closeout, error) {
	if input.kind != OperationCloseout {
		return Closeout{}, clientError(ErrorInvalid)
	}
	id, err := NewIdentifier(closeoutID)
	if err != nil {
		return Closeout{}, clientError(ErrorInvalid)
	}
	response, err := s.runOperation(
		ctx,
		input,
		operationPhaseFrozen,
		responseCloseout,
		id,
	)
	if err != nil {
		return Closeout{}, err
	}
	result, ok := response.(Closeout)
	if !ok {
		return Closeout{}, clientError(ErrorConflict)
	}
	return result, nil
}

// Observe is an explicit exact replay of a previously persisted operation. It
// never creates a fresh operation or substitutes a legacy observation path.
func (s Session) Observe(ctx context.Context, operationID string) (OperationResponse, error) {
	return s.Replay(ctx, operationID)
}

// Replay redelivers a byte-for-byte-equivalent logical operation using its
// original id, sequence, nonce, payload hash, plan hash, and canonical hash.
func (s Session) Replay(ctx context.Context, operationID string) (OperationResponse, error) {
	if s.client == nil || !s.id.valid() {
		return nil, clientError(ErrorUnavailable)
	}
	id, err := NewIdentifier(operationID)
	if err != nil {
		return nil, clientError(ErrorInvalid)
	}
	s.client.mu.Lock()
	defer s.client.mu.Unlock()
	state, err := s.client.loadState(ctx, s.id)
	if err != nil {
		return nil, err
	}
	if state.poisoned {
		return nil, clientError(ErrorConflict)
	}
	entry, index := state.operation(id)
	if index < 0 {
		return nil, clientError(ErrorConflict)
	}
	return s.client.sendOperation(ctx, &state, index, entry.operation, entry.expected, entry.closeoutID)
}

// Freeze submits an exact freeze record bound to the only quiescence evidence
// persisted for the session.
func (s Session) Freeze(ctx context.Context, input FreezeInput) (FreezeAck, error) {
	if s.client == nil || !s.id.valid() || !input.valid() {
		return FreezeAck{}, clientError(ErrorInvalid)
	}
	s.client.mu.Lock()
	defer s.client.mu.Unlock()
	state, err := s.client.loadState(ctx, s.id)
	if err != nil {
		return FreezeAck{}, err
	}
	if state.poisoned {
		return FreezeAck{}, clientError(ErrorConflict)
	}
	if !state.expiresAt.After(s.client.now().UTC()) {
		return FreezeAck{}, clientError(ErrorUnavailable)
	}
	if state.freeze == nil {
		if state.phase != phaseQuiescent {
			return FreezeAck{}, clientError(ErrorInvalid)
		}
	} else if state.phase != phaseQuiescent && state.phase != phaseFrozen {
		return FreezeAck{}, clientError(ErrorInvalid)
	}
	request, err := newFreeze(state.sessionID, state.quiescenceHash, input)
	if err != nil {
		return FreezeAck{}, clientError(ErrorInvalid)
	}
	if state.freeze != nil {
		if state.freeze.request.freezeID != request.freezeID || state.freeze.request.nonce != request.nonce || state.freeze.request.canonicalHash != request.canonicalHash {
			return FreezeAck{}, s.client.poison(ctx, &state)
		}
	} else {
		state.freeze = &freezeEntry{request: request}
		if err := s.client.saveState(ctx, state); err != nil {
			return FreezeAck{}, err
		}
	}
	ack, err := s.client.endpoint.Freeze(ctx, request)
	if err != nil {
		return FreezeAck{}, s.client.endpointError(err, state.capabilities())
	}
	if err := s.client.validateEndpointBinding(); err != nil {
		return FreezeAck{}, err
	}
	if !ack.valid() || ack.sessionID != state.sessionID || ack.freezeID != request.freezeID || ack.quiescenceHash != state.quiescenceHash {
		return FreezeAck{}, s.client.poison(ctx, &state)
	}
	state.freeze.done = true
	state.phase = phaseFrozen
	if err := s.client.saveState(ctx, state); err != nil {
		return FreezeAck{}, err
	}
	return ack, nil
}

// Cancel submits an exact cancellation record from an active session. A changed
// id, nonce, reason, or canonical hash poisons the durable session instead of
// allowing an ambiguous second cancellation effect.
func (s Session) Cancel(ctx context.Context, input CancellationInput) (CancellationAck, error) {
	if s.client == nil || !s.id.valid() || !input.valid() {
		return CancellationAck{}, clientError(ErrorInvalid)
	}
	s.client.mu.Lock()
	defer s.client.mu.Unlock()
	state, err := s.client.loadState(ctx, s.id)
	if err != nil {
		return CancellationAck{}, err
	}
	if state.poisoned {
		return CancellationAck{}, clientError(ErrorConflict)
	}
	if !state.expiresAt.After(s.client.now().UTC()) {
		return CancellationAck{}, clientError(ErrorUnavailable)
	}
	if state.cancellation == nil {
		if state.phase != phaseActive {
			return CancellationAck{}, clientError(ErrorInvalid)
		}
	} else if state.phase != phaseActive && state.phase != phaseCancelled {
		return CancellationAck{}, clientError(ErrorInvalid)
	}
	request, err := newCancellation(state.sessionID, input)
	if err != nil {
		return CancellationAck{}, clientError(ErrorInvalid)
	}
	if state.cancellation != nil {
		prior := state.cancellation.request
		if prior.cancellationID != request.cancellationID || prior.reason != request.reason || prior.nonce != request.nonce || prior.canonicalHash != request.canonicalHash {
			return CancellationAck{}, s.client.poison(ctx, &state)
		}
	} else {
		state.cancellation = &cancellationEntry{request: request}
		if err := s.client.saveState(ctx, state); err != nil {
			return CancellationAck{}, err
		}
	}
	ack, err := s.client.endpoint.Cancel(ctx, request)
	if err != nil {
		return CancellationAck{}, s.client.endpointError(err, state.capabilities())
	}
	if err := s.client.validateEndpointBinding(); err != nil {
		return CancellationAck{}, err
	}
	if !ack.valid() || ack.sessionID != state.sessionID || ack.cancellationID != request.cancellationID {
		return CancellationAck{}, s.client.poison(ctx, &state)
	}
	state.cancellation.done = true
	state.phase = phaseCancelled
	state.evidence.logicalEvidence = ack.logicalEvidenceHash
	state.evidence.hasLogicalEvidence = true
	if err := s.client.saveState(ctx, state); err != nil {
		return CancellationAck{}, err
	}
	return ack, nil
}

// Evidence returns only provider-originated evidence and artifact commitments
// already durably associated with this session. It never synthesizes evidence.
func (s Session) Evidence(ctx context.Context) (EvidenceReferences, error) {
	if s.client == nil || !s.id.valid() {
		return EvidenceReferences{}, clientError(ErrorUnavailable)
	}
	s.client.mu.Lock()
	defer s.client.mu.Unlock()
	state, err := s.client.loadState(ctx, s.id)
	if err != nil {
		return EvidenceReferences{}, err
	}
	return state.evidence.public(), nil
}

func (s Session) runOperation(
	ctx context.Context,
	input OperationInput,
	requiredPhase operationPhaseRequirement,
	expected responseKind,
	closeoutID Identifier,
) (OperationResponse, error) {
	if s.client == nil || !s.id.valid() || !input.valid() {
		return nil, clientError(ErrorInvalid)
	}
	if expected == responseCloseout && !closeoutID.valid() {
		return nil, clientError(ErrorInvalid)
	}
	if expected != responseCloseout && closeoutID.valid() {
		return nil, clientError(ErrorInvalid)
	}
	s.client.mu.Lock()
	defer s.client.mu.Unlock()
	state, err := s.client.loadState(ctx, s.id)
	if err != nil {
		return nil, err
	}
	if err := s.client.validateStateForEffect(state, requiredPhase); err != nil {
		return nil, err
	}

	if entry, index := state.operation(input.operationID); index >= 0 {
		if entry.operation.kind != input.kind || entry.operation.nonce != input.nonce || entry.operation.payloadHash != input.payloadHash || entry.operation.canonicalHash != input.canonicalHash || entry.expected != expected || entry.closeoutID != closeoutID {
			return nil, s.client.poison(ctx, &state)
		}
		return s.client.sendOperation(ctx, &state, index, entry.operation, entry.expected, entry.closeoutID)
	}
	if len(state.operations) >= maxCapabilities {
		return nil, clientError(ErrorInvalid)
	}
	sequence, err := NewOperationSequence(state.nextSequence)
	if err != nil {
		return nil, clientError(ErrorConflict)
	}
	operation, err := newAgentOperation(state.sessionID, sequence, state.planHash, input)
	if err != nil {
		return nil, clientError(ErrorInvalid)
	}
	state.operations = append(state.operations, operationEntry{operation: operation, expected: expected, closeoutID: closeoutID})
	state.nextSequence++
	if err := s.client.saveState(ctx, state); err != nil {
		return nil, err
	}
	return s.client.sendOperation(ctx, &state, len(state.operations)-1, operation, expected, closeoutID)
}

func (c *Client) sendOperation(ctx context.Context, state *sessionState, index int, operation AgentOperation, expected responseKind, closeoutID Identifier) (OperationResponse, error) {
	response, err := c.endpoint.Operate(ctx, operation)
	if err != nil {
		return nil, c.endpointError(err, state.capabilities())
	}
	if err := c.validateEndpointBinding(); err != nil {
		return nil, err
	}
	if response == nil || !response.valid() || response.responseKind() != expected {
		return nil, c.poison(ctx, state)
	}
	alreadyDone := state.operations[index].done

	switch typed := response.(type) {
	case OperationResult:
		if typed.sessionID != state.sessionID || typed.operationID != operation.operationID || typed.sequence != operation.sequence {
			return nil, c.poison(ctx, state)
		}
		if typed.resultKind == ResultUnavailable {
			return nil, clientError(ErrorUnavailable)
		}
		if !alreadyDone {
			state.phase = phaseActive
			state.evidence.addArtifact(typed.artifactHash)
			state.evidence.addOperationEvidence(typed.evidenceHash)
		}
	case Quiescence:
		if typed.sessionID != state.sessionID {
			return nil, c.poison(ctx, state)
		}
		if !alreadyDone {
			state.phase = phaseQuiescent
			state.quiescenceHash = typed.quiescenceHash
			state.hasQuiescence = true
			state.evidence.resourceEvidence = typed.resourceEvidenceHash
			state.evidence.hasResourceEvidence = true
		}
	case Closeout:
		if typed.sessionID != state.sessionID || !closeoutID.valid() || typed.closeoutID != closeoutID || !state.hasQuiescence || typed.quiescenceHash != state.quiescenceHash {
			return nil, c.poison(ctx, state)
		}
		if !alreadyDone {
			state.phase = phaseClosed
			state.evidence.logicalEvidence = typed.logicalEvidenceHash
			state.evidence.hasLogicalEvidence = true
			state.evidence.nativeExtension = typed.nativeExtensionHash
			state.evidence.hasNativeExtension = true
		}
	default:
		return nil, c.poison(ctx, state)
	}

	state.operations[index].done = true
	if err := c.saveState(ctx, *state); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *Client) validateUsableState(state sessionState, capabilities ProviderCapabilities) error {
	if state.poisoned ||
		state.backendIdentityHash != c.backendBinding.IdentitySHA256() ||
		!capabilities.valid() ||
		state.providerID != capabilities.providerID ||
		state.epoch != capabilities.epoch ||
		state.profile != capabilities.profile ||
		state.sessionCapabilityHash != capabilities.capabilityHash {
		return clientError(ErrorConflict)
	}
	if !state.expiresAt.After(c.now().UTC()) {
		return clientError(ErrorUnavailable)
	}
	return nil
}

func (c *Client) validateStateForEffect(
	state sessionState,
	required operationPhaseRequirement,
) error {
	if state.poisoned {
		return clientError(ErrorConflict)
	}
	if !required.allows(state.phase) {
		return clientError(ErrorInvalid)
	}
	if !state.expiresAt.After(c.now().UTC()) {
		return clientError(ErrorUnavailable)
	}
	return nil
}

func (c *Client) validateEndpointBinding() error {
	if c == nil || c.endpoint == nil || !c.backendBinding.valid() {
		return clientError(ErrorUnavailable)
	}
	if current := c.endpoint.BackendBinding(); !current.valid() ||
		current != c.backendBinding {
		return clientError(ErrorConflict)
	}
	return nil
}

func (c *Client) endpointError(err error, expected *ProviderCapabilities) error {
	if err == nil {
		return clientError(ErrorConflict)
	}
	var frameFailure framingError
	if errors.As(err, &frameFailure) {
		return clientError(frameFailure.class)
	}
	var providerFailure *ProviderFailure
	if errors.As(err, &providerFailure) {
		if !providerFailure.valid() {
			return clientError(ErrorConflict)
		}
		if expected != nil && (providerFailure.providerID != expected.providerID || providerFailure.epoch != expected.epoch) {
			return clientError(ErrorConflict)
		}
		switch providerFailure.code {
		case ProviderErrorInvalid:
			return clientError(ErrorInvalid)
		case ProviderErrorUnsupported, ProviderErrorProfileMismatch:
			return clientError(ErrorUnsupported)
		case ProviderErrorUnavailable:
			return clientError(ErrorUnavailable)
		case ProviderErrorConflict, ProviderErrorStaleEpoch, ProviderErrorReplayConflict:
			return clientError(ErrorConflict)
		default:
			return clientError(ErrorConflict)
		}
	}
	// Deliberately discard arbitrary transport error text: it could include
	// provider diagnostics which the contract forbids surfacing to the product.
	return clientError(ErrorUnavailable)
}

func (c *Client) poison(ctx context.Context, state *sessionState) error {
	state.poisoned = true
	if err := c.saveState(ctx, *state); err != nil {
		return err
	}
	return clientError(ErrorConflict)
}

type sessionPhase string

const (
	phaseAdmitted  sessionPhase = "admitted"
	phaseActive    sessionPhase = "active"
	phaseQuiescent sessionPhase = "quiescent"
	phaseFrozen    sessionPhase = "frozen"
	phaseCancelled sessionPhase = "cancelled"
	phaseClosed    sessionPhase = "closed"
)

func (p sessionPhase) valid() bool {
	switch p {
	case phaseAdmitted, phaseActive, phaseQuiescent, phaseFrozen, phaseCancelled, phaseClosed:
		return true
	default:
		return false
	}
}

type operationPhaseRequirement uint8

const (
	operationPhaseAdmitted operationPhaseRequirement = iota + 1
	operationPhaseAdmittedOrActive
	operationPhaseActive
	operationPhaseFrozen
)

func (r operationPhaseRequirement) allows(phase sessionPhase) bool {
	switch r {
	case operationPhaseAdmitted:
		return phase == phaseAdmitted
	case operationPhaseAdmittedOrActive:
		return phase == phaseAdmitted || phase == phaseActive
	case operationPhaseActive:
		return phase == phaseActive
	case operationPhaseFrozen:
		return phase == phaseFrozen
	default:
		return false
	}
}

type operationEntry struct {
	operation  AgentOperation
	expected   responseKind
	closeoutID Identifier
	done       bool
}

type freezeEntry struct {
	request Freeze
	done    bool
}

type cancellationEntry struct {
	request Cancellation
	done    bool
}

type sessionState struct {
	sessionID             Identifier
	providerID            Identifier
	epoch                 ProviderEpoch
	profile               Profile
	requirementHash       Fingerprint
	planHash              Fingerprint
	sessionCapabilityHash Fingerprint
	backendIdentityHash   Fingerprint
	expiresAt             time.Time
	phase                 sessionPhase
	nextSequence          uint64
	quiescenceHash        Fingerprint
	hasQuiescence         bool
	operations            []operationEntry
	freeze                *freezeEntry
	cancellation          *cancellationEntry
	evidence              evidenceState
	poisoned              bool
}

func (s sessionState) operation(id Identifier) (operationEntry, int) {
	for index, entry := range s.operations {
		if entry.operation.operationID == id {
			return entry, index
		}
	}
	return operationEntry{}, -1
}

func (s sessionState) capabilities() *ProviderCapabilities {
	value := ProviderCapabilities{
		providerID:     s.providerID,
		epoch:          s.epoch,
		profile:        s.profile,
		capabilityHash: s.sessionCapabilityHash,
		canonicalHash:  s.requirementHash,
	}
	return &value
}

type evidenceState struct {
	artifacts           []Fingerprint
	operationEvidence   []Fingerprint
	resourceEvidence    Fingerprint
	hasResourceEvidence bool
	logicalEvidence     Fingerprint
	hasLogicalEvidence  bool
	nativeExtension     Fingerprint
	hasNativeExtension  bool
}

func (e *evidenceState) addArtifact(value Fingerprint) {
	e.artifacts = addFingerprint(e.artifacts, value)
}

func (e *evidenceState) addOperationEvidence(value Fingerprint) {
	e.operationEvidence = addFingerprint(e.operationEvidence, value)
}

func addFingerprint(values []Fingerprint, value Fingerprint) []Fingerprint {
	for _, candidate := range values {
		if candidate == value {
			return values
		}
	}
	return append(values, value)
}

// EvidenceReferences holds content-addressed references only. It intentionally
// has no API for materializing artifact bytes or accepting caller-made evidence.
type EvidenceReferences struct {
	artifacts         []Fingerprint
	operationEvidence []Fingerprint
	resourceEvidence  Fingerprint
	hasResource       bool
	logicalEvidence   Fingerprint
	hasLogical        bool
	nativeExtension   Fingerprint
	hasNative         bool
}

func (e evidenceState) public() EvidenceReferences {
	return EvidenceReferences{
		artifacts:         append([]Fingerprint(nil), e.artifacts...),
		operationEvidence: append([]Fingerprint(nil), e.operationEvidence...),
		resourceEvidence:  e.resourceEvidence,
		hasResource:       e.hasResourceEvidence,
		logicalEvidence:   e.logicalEvidence,
		hasLogical:        e.hasLogicalEvidence,
		nativeExtension:   e.nativeExtension,
		hasNative:         e.hasNativeExtension,
	}
}

func (e EvidenceReferences) Artifacts() []Fingerprint {
	return append([]Fingerprint(nil), e.artifacts...)
}
func (e EvidenceReferences) OperationEvidence() []Fingerprint {
	return append([]Fingerprint(nil), e.operationEvidence...)
}
func (e EvidenceReferences) ResourceEvidence() (Fingerprint, bool) {
	return e.resourceEvidence, e.hasResource
}
func (e EvidenceReferences) LogicalEvidence() (Fingerprint, bool) {
	return e.logicalEvidence, e.hasLogical
}
func (e EvidenceReferences) NativeExtension() (Fingerprint, bool) {
	return e.nativeExtension, e.hasNative
}

type checkpointDTO struct {
	Schema                string                     `json:"schema"`
	SessionID             string                     `json:"session_id"`
	ProviderID            string                     `json:"provider_id"`
	ProviderEpoch         uint64                     `json:"provider_epoch"`
	Profile               Profile                    `json:"profile"`
	RequirementHash       string                     `json:"requirement_hash"`
	PlanHash              string                     `json:"plan_hash"`
	SessionCapabilityHash string                     `json:"session_capability_hash"`
	BackendIdentitySHA256 string                     `json:"backend_identity_sha256"`
	ExpiresAtMS           int64                      `json:"expires_at_ms"`
	Phase                 sessionPhase               `json:"phase"`
	NextSequence          uint64                     `json:"next_sequence"`
	QuiescenceHash        string                     `json:"quiescence_hash,omitempty"`
	Operations            []operationCheckpointDTO   `json:"operations"`
	Freeze                *freezeCheckpointDTO       `json:"freeze,omitempty"`
	Cancellation          *cancellationCheckpointDTO `json:"cancellation,omitempty"`
	Evidence              evidenceCheckpointDTO      `json:"evidence"`
	Poisoned              bool                       `json:"poisoned"`
}

type operationCheckpointDTO struct {
	OperationID   string        `json:"operation_id"`
	Sequence      uint64        `json:"sequence"`
	Kind          OperationKind `json:"kind"`
	Nonce         string        `json:"nonce"`
	PayloadHash   string        `json:"payload_hash"`
	CanonicalHash string        `json:"canonical_hash"`
	Expected      responseKind  `json:"expected_response"`
	CloseoutID    string        `json:"closeout_id,omitempty"`
	Done          bool          `json:"done"`
}

type freezeCheckpointDTO struct {
	FreezeID      string `json:"freeze_id"`
	Nonce         string `json:"nonce"`
	CanonicalHash string `json:"canonical_hash"`
	Done          bool   `json:"done"`
}

type cancellationCheckpointDTO struct {
	CancellationID string `json:"cancellation_id"`
	Reason         string `json:"reason"`
	Nonce          string `json:"nonce"`
	CanonicalHash  string `json:"canonical_hash"`
	Done           bool   `json:"done"`
}

type evidenceCheckpointDTO struct {
	Artifacts           []string `json:"artifacts"`
	OperationEvidence   []string `json:"operation_evidence"`
	ResourceEvidence    string   `json:"resource_evidence,omitempty"`
	LogicalEvidence     string   `json:"logical_evidence,omitempty"`
	NativeExtensionHash string   `json:"native_extension_hash,omitempty"`
}

func (c *Client) saveState(ctx context.Context, state sessionState) error {
	data, err := json.Marshal(state.checkpoint())
	if err != nil || len(data) > MaxRecordBytes {
		return clientError(ErrorConflict)
	}
	if err := c.store.Save(ctx, state.sessionID.String(), data); err != nil {
		return clientError(ErrorUnavailable)
	}
	return nil
}

func (c *Client) loadState(ctx context.Context, id Identifier) (sessionState, error) {
	data, ok, err := c.store.Load(ctx, id.String())
	if err != nil {
		return sessionState{}, clientError(ErrorUnavailable)
	}
	if !ok {
		return sessionState{}, clientError(ErrorConflict)
	}
	if len(data) == 0 || len(data) > MaxRecordBytes {
		return sessionState{}, clientError(ErrorConflict)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var checkpoint checkpointDTO
	if err := decoder.Decode(&checkpoint); err != nil {
		return sessionState{}, clientError(ErrorConflict)
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return sessionState{}, clientError(ErrorConflict)
	}
	state, err := sessionStateFromCheckpoint(checkpoint)
	if err != nil || state.sessionID != id {
		return sessionState{}, clientError(ErrorConflict)
	}
	if err := c.validateEndpointBinding(); err != nil {
		return sessionState{}, err
	}
	if state.backendIdentityHash != c.backendBinding.IdentitySHA256() {
		return sessionState{}, clientError(ErrorConflict)
	}
	return state, nil
}

func (s sessionState) checkpoint() checkpointDTO {
	operations := make([]operationCheckpointDTO, 0, len(s.operations))
	for _, entry := range s.operations {
		operations = append(operations, operationCheckpointDTO{
			OperationID:   entry.operation.operationID.String(),
			Sequence:      entry.operation.sequence.Uint64(),
			Kind:          entry.operation.kind,
			Nonce:         entry.operation.nonce.String(),
			PayloadHash:   entry.operation.payloadHash.String(),
			CanonicalHash: entry.operation.canonicalHash.String(),
			Expected:      entry.expected,
			CloseoutID:    entry.closeoutID.String(),
			Done:          entry.done,
		})
	}
	checkpoint := checkpointDTO{
		Schema:                checkpointSchema,
		SessionID:             s.sessionID.String(),
		ProviderID:            s.providerID.String(),
		ProviderEpoch:         s.epoch.Uint64(),
		Profile:               s.profile,
		RequirementHash:       s.requirementHash.String(),
		PlanHash:              s.planHash.String(),
		SessionCapabilityHash: s.sessionCapabilityHash.String(),
		BackendIdentitySHA256: s.backendIdentityHash.String(),
		ExpiresAtMS:           s.expiresAt.UnixMilli(),
		Phase:                 s.phase,
		NextSequence:          s.nextSequence,
		Operations:            operations,
		Evidence:              s.evidence.checkpoint(),
		Poisoned:              s.poisoned,
	}
	if s.hasQuiescence {
		checkpoint.QuiescenceHash = s.quiescenceHash.String()
	}
	if s.freeze != nil {
		checkpoint.Freeze = &freezeCheckpointDTO{FreezeID: s.freeze.request.freezeID.String(), Nonce: s.freeze.request.nonce.String(), CanonicalHash: s.freeze.request.canonicalHash.String(), Done: s.freeze.done}
	}
	if s.cancellation != nil {
		checkpoint.Cancellation = &cancellationCheckpointDTO{CancellationID: s.cancellation.request.cancellationID.String(), Reason: s.cancellation.request.reason, Nonce: s.cancellation.request.nonce.String(), CanonicalHash: s.cancellation.request.canonicalHash.String(), Done: s.cancellation.done}
	}
	return checkpoint
}

func (e evidenceState) checkpoint() evidenceCheckpointDTO {
	checkpoint := evidenceCheckpointDTO{Artifacts: fingerprintStrings(e.artifacts), OperationEvidence: fingerprintStrings(e.operationEvidence)}
	if e.hasResourceEvidence {
		checkpoint.ResourceEvidence = e.resourceEvidence.String()
	}
	if e.hasLogicalEvidence {
		checkpoint.LogicalEvidence = e.logicalEvidence.String()
	}
	if e.hasNativeExtension {
		checkpoint.NativeExtensionHash = e.nativeExtension.String()
	}
	return checkpoint
}

func fingerprintStrings(values []Fingerprint) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = value.String()
	}
	return out
}

func sessionStateFromCheckpoint(checkpoint checkpointDTO) (sessionState, error) {
	if checkpoint.Schema != checkpointSchema || !checkpoint.Phase.valid() || checkpoint.ExpiresAtMS <= 0 || checkpoint.NextSequence == 0 || len(checkpoint.Operations) > maxCapabilities {
		return sessionState{}, fmt.Errorf("invalid checkpoint header")
	}
	sessionID, err := NewIdentifier(checkpoint.SessionID)
	if err != nil {
		return sessionState{}, err
	}
	providerID, err := NewIdentifier(checkpoint.ProviderID)
	if err != nil {
		return sessionState{}, err
	}
	epoch, err := NewProviderEpoch(checkpoint.ProviderEpoch)
	if err != nil {
		return sessionState{}, err
	}
	if !checkpoint.Profile.valid() {
		return sessionState{}, fmt.Errorf("invalid checkpoint profile")
	}
	requirementHash, err := ParseFingerprint(checkpoint.RequirementHash)
	if err != nil {
		return sessionState{}, err
	}
	planHash, err := ParseFingerprint(checkpoint.PlanHash)
	if err != nil {
		return sessionState{}, err
	}
	sessionCapabilityHash, err := ParseFingerprint(checkpoint.SessionCapabilityHash)
	if err != nil {
		return sessionState{}, err
	}
	backendIdentityHash, err := ParseFingerprint(checkpoint.BackendIdentitySHA256)
	if err != nil {
		return sessionState{}, err
	}
	state := sessionState{
		sessionID:             sessionID,
		providerID:            providerID,
		epoch:                 epoch,
		profile:               checkpoint.Profile,
		requirementHash:       requirementHash,
		planHash:              planHash,
		sessionCapabilityHash: sessionCapabilityHash,
		backendIdentityHash:   backendIdentityHash,
		expiresAt:             time.UnixMilli(checkpoint.ExpiresAtMS).UTC(),
		phase:                 checkpoint.Phase,
		nextSequence:          checkpoint.NextSequence,
		poisoned:              checkpoint.Poisoned,
	}
	if checkpoint.QuiescenceHash != "" {
		quiescenceHash, err := ParseFingerprint(checkpoint.QuiescenceHash)
		if err != nil {
			return sessionState{}, err
		}
		state.quiescenceHash = quiescenceHash
		state.hasQuiescence = true
	}
	seenIDs := make(map[Identifier]struct{}, len(checkpoint.Operations))
	seenSequences := make(map[OperationSequence]struct{}, len(checkpoint.Operations))
	var maximumSequence uint64
	for _, value := range checkpoint.Operations {
		input, err := NewOperationInput(OperationInputValues{OperationID: value.OperationID, Kind: value.Kind, Nonce: value.Nonce, PayloadHash: value.PayloadHash, CanonicalHash: value.CanonicalHash})
		if err != nil {
			return sessionState{}, err
		}
		sequence, err := NewOperationSequence(value.Sequence)
		if err != nil {
			return sessionState{}, err
		}
		if value.Expected != responseOperationResult && value.Expected != responseQuiescence && value.Expected != responseCloseout {
			return sessionState{}, fmt.Errorf("invalid checkpoint response kind")
		}
		if _, ok := seenIDs[input.operationID]; ok {
			return sessionState{}, fmt.Errorf("duplicate checkpoint operation id")
		}
		if _, ok := seenSequences[sequence]; ok {
			return sessionState{}, fmt.Errorf("duplicate checkpoint operation sequence")
		}
		operation, err := newAgentOperation(sessionID, sequence, planHash, input)
		if err != nil {
			return sessionState{}, err
		}
		seenIDs[input.operationID] = struct{}{}
		seenSequences[sequence] = struct{}{}
		if value.Sequence > maximumSequence {
			maximumSequence = value.Sequence
		}
		var closeoutID Identifier
		if value.Expected == responseCloseout {
			closeoutID, err = NewIdentifier(value.CloseoutID)
			if err != nil {
				return sessionState{}, fmt.Errorf("closeout checkpoint lacks closeout id")
			}
		} else if value.CloseoutID != "" {
			return sessionState{}, fmt.Errorf("non-closeout checkpoint has closeout id")
		}
		state.operations = append(state.operations, operationEntry{operation: operation, expected: value.Expected, closeoutID: closeoutID, done: value.Done})
	}
	if checkpoint.NextSequence <= maximumSequence {
		return sessionState{}, fmt.Errorf("checkpoint next sequence does not advance")
	}
	if checkpoint.Freeze != nil {
		input, err := NewFreezeInput(FreezeInputValues{FreezeID: checkpoint.Freeze.FreezeID, Nonce: checkpoint.Freeze.Nonce, CanonicalHash: checkpoint.Freeze.CanonicalHash})
		if err != nil || !state.hasQuiescence {
			return sessionState{}, fmt.Errorf("invalid freeze checkpoint")
		}
		request, err := newFreeze(sessionID, state.quiescenceHash, input)
		if err != nil {
			return sessionState{}, err
		}
		state.freeze = &freezeEntry{request: request, done: checkpoint.Freeze.Done}
	}
	if checkpoint.Cancellation != nil {
		input, err := NewCancellationInput(CancellationInputValues{CancellationID: checkpoint.Cancellation.CancellationID, Reason: checkpoint.Cancellation.Reason, Nonce: checkpoint.Cancellation.Nonce, CanonicalHash: checkpoint.Cancellation.CanonicalHash})
		if err != nil {
			return sessionState{}, err
		}
		request, err := newCancellation(sessionID, input)
		if err != nil {
			return sessionState{}, err
		}
		state.cancellation = &cancellationEntry{request: request, done: checkpoint.Cancellation.Done}
	}
	evidence, err := evidenceStateFromCheckpoint(checkpoint.Evidence)
	if err != nil {
		return sessionState{}, err
	}
	state.evidence = evidence
	if (state.phase == phaseQuiescent || state.phase == phaseFrozen || state.phase == phaseClosed) && !state.hasQuiescence {
		return sessionState{}, fmt.Errorf("checkpoint phase lacks quiescence")
	}
	if state.phase == phaseFrozen && (state.freeze == nil || !state.freeze.done) {
		return sessionState{}, fmt.Errorf("checkpoint frozen without acknowledged freeze")
	}
	if state.phase == phaseCancelled && (state.cancellation == nil || !state.cancellation.done) {
		return sessionState{}, fmt.Errorf("checkpoint cancelled without acknowledgement")
	}
	return state, nil
}

func evidenceStateFromCheckpoint(checkpoint evidenceCheckpointDTO) (evidenceState, error) {
	artifacts, err := parseFingerprintSlice(checkpoint.Artifacts)
	if err != nil {
		return evidenceState{}, err
	}
	operationEvidence, err := parseFingerprintSlice(checkpoint.OperationEvidence)
	if err != nil {
		return evidenceState{}, err
	}
	state := evidenceState{artifacts: artifacts, operationEvidence: operationEvidence}
	if checkpoint.ResourceEvidence != "" {
		value, err := ParseFingerprint(checkpoint.ResourceEvidence)
		if err != nil {
			return evidenceState{}, err
		}
		state.resourceEvidence = value
		state.hasResourceEvidence = true
	}
	if checkpoint.LogicalEvidence != "" {
		value, err := ParseFingerprint(checkpoint.LogicalEvidence)
		if err != nil {
			return evidenceState{}, err
		}
		state.logicalEvidence = value
		state.hasLogicalEvidence = true
	}
	if checkpoint.NativeExtensionHash != "" {
		value, err := ParseFingerprint(checkpoint.NativeExtensionHash)
		if err != nil {
			return evidenceState{}, err
		}
		state.nativeExtension = value
		state.hasNativeExtension = true
	}
	return state, nil
}

func parseFingerprintSlice(values []string) ([]Fingerprint, error) {
	if len(values) > maxCapabilities {
		return nil, fmt.Errorf("too many evidence references")
	}
	out := make([]Fingerprint, 0, len(values))
	for _, raw := range values {
		value, err := ParseFingerprint(raw)
		if err != nil {
			return nil, err
		}
		out = addFingerprint(out, value)
	}
	if len(out) != len(values) {
		return nil, fmt.Errorf("duplicate evidence reference")
	}
	return out, nil
}
