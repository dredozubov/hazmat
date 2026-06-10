//go:build beadpost_hostbroker

package hostbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"hazmat/attestationkey"
	"hazmat/attestationtier"

	"local/beadpost-contracts/request"
)

const (
	// defaultMaxAgentRequestBytes bounds a single agent payload so an oversized
	// request fails closed instead of exhausting memory.
	defaultMaxAgentRequestBytes = 64 * 1024

	// defaultSocketMode is group-rw so the contained agent (in the agent group)
	// can connect, while others cannot. The runtime dir restricts the group.
	defaultSocketMode os.FileMode = 0o660

	agentConnDeadline = 30 * time.Second

	// attestationTTL bounds the minted token's validity. The token is used
	// immediately for the synchronous submit; a short TTL limits replay.
	attestationTTL = 5 * time.Minute

	opDeliver = "deliver"
	opReview  = "review"
)

// LaunchFacts is the host-derived, trusted authority for a contained session.
// Every authority field comes from the launch/session context; none is ever
// taken from the contained agent. SandboxConfirmed records that sandbox_init
// succeeded for this session — the socket is created only when it is true.
type LaunchFacts struct {
	OriginProject string               // registry key of the contained project
	ProjectPath   string               // filesystem path the attestation binds
	AgentUID      int                  // the contained agent's uid
	Tier          attestationtier.Tier // effective .5 tier (over-claim guarded)

	// dr-side broker config, passed opaquely to beadpost-broker; never read here.
	RegistryPath string
	LedgerPath   string

	SandboxConfirmed bool
}

// Submitter is the .4 host-broker client surface the session forwards to.
// *Client satisfies it; tests inject a fake.
type Submitter interface {
	Deliver(context.Context, Submission) (Result, error)
	Review(context.Context, Submission) (Result, error)
}

// SessionConfig configures a per-session agent socket. RuntimeDir is a
// per-session directory the caller created with appropriate (agent-group)
// permissions; the session binds its socket inside it and removes it on Close.
type SessionConfig struct {
	Facts           LaunchFacts
	RuntimeDir      string
	Submitter       Submitter
	Key             attestationkey.Key
	SocketMode      os.FileMode
	MaxRequestBytes int
}

// authority is the request authority derived solely from launch facts.
type authority struct {
	originProject string
	projectPath   string
	agentUID      int
	tier          attestationtier.Tier
	registryPath  string
	ledgerPath    string
}

// Session is a per-session contained-agent-facing broker socket. The agent
// submits bounded request content; the host derives all authority from launch
// facts, computes the request fingerprint with the shared contract, mints the v2
// attestation, and forwards to beadpost-broker via the .4 client.
type Session struct {
	facts      LaunchFacts
	submitter  Submitter
	key        attestationkey.Key
	maxRequest int

	runtimeDir string
	socketPath string
	listener   net.Listener
	done       chan struct{}
	closeOnce  sync.Once
}

// Open validates the launch facts, confirms the sandbox boundary, allocates the
// ephemeral agent socket, and starts serving. It fails closed (no socket) unless
// containment is confirmed and every authority fact is present.
func Open(cfg SessionConfig) (*Session, error) {
	if cfg.Submitter == nil {
		return nil, errors.New("host broker submitter is required")
	}
	if !cfg.Key.Valid() {
		return nil, errors.New("attestation key is not configured")
	}
	if err := confirmSandboxBoundary(cfg.Facts); err != nil {
		return nil, err
	}
	if cfg.RuntimeDir == "" {
		return nil, errors.New("session runtime dir is required")
	}

	mode := cfg.SocketMode
	if mode == 0 {
		mode = defaultSocketMode
	}
	listener, socketPath, err := allocateBrokerSocket(cfg.RuntimeDir, mode)
	if err != nil {
		return nil, err
	}

	maxReq := cfg.MaxRequestBytes
	if maxReq <= 0 {
		maxReq = defaultMaxAgentRequestBytes
	}
	s := &Session{
		facts:      cfg.Facts,
		submitter:  cfg.Submitter,
		key:        cfg.Key,
		maxRequest: maxReq,
		runtimeDir: cfg.RuntimeDir,
		socketPath: socketPath,
		listener:   listener,
		done:       make(chan struct{}),
	}
	go s.serve()
	return s, nil
}

// SocketPath is the agent-facing Unix socket path.
func (s *Session) SocketPath() string { return s.socketPath }

// confirmSandboxBoundary fails closed unless sandbox_init is confirmed and the
// host authority facts are all present. It is the gate enforcing
// BrokerSocketOnlyAfterConfirmedSession and AcceptedRequestHasConfirmedSession.
func confirmSandboxBoundary(facts LaunchFacts) error {
	if !facts.SandboxConfirmed {
		return errors.New("containment not confirmed: refusing to open the agent broker socket before sandbox_init")
	}
	switch {
	case facts.OriginProject == "":
		return errors.New("launch facts missing origin project")
	case facts.ProjectPath == "":
		return errors.New("launch facts missing project path")
	case facts.AgentUID <= 0:
		return errors.New("launch facts missing agent uid")
	case facts.Tier == "":
		return errors.New("launch facts missing effective tier")
	case facts.RegistryPath == "":
		return errors.New("launch facts missing broker registry path")
	case facts.LedgerPath == "":
		return errors.New("launch facts missing broker ledger path")
	}
	return nil
}

// allocateBrokerSocket binds the ephemeral Unix socket inside runtimeDir and
// restricts its mode so only the contained agent (group) can connect.
func allocateBrokerSocket(runtimeDir string, mode os.FileMode) (net.Listener, string, error) {
	socketPath := filepath.Join(runtimeDir, "agent-broker.sock")
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, "", fmt.Errorf("allocate agent broker socket at %s: %w", socketPath, err)
	}
	if err := os.Chmod(socketPath, mode); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, "", fmt.Errorf("restrict agent broker socket mode at %s: %w", socketPath, err)
	}
	return listener, socketPath, nil
}

// deriveAuthorityFromLaunchFacts returns the request authority taken solely from
// launch facts. The contained agent cannot influence any of these fields; this
// is the AcceptedAuthorityEqualsLaunchFacts enforcement point.
func deriveAuthorityFromLaunchFacts(facts LaunchFacts) authority {
	return authority{
		originProject: facts.OriginProject,
		projectPath:   facts.ProjectPath,
		agentUID:      facts.AgentUID,
		tier:          facts.Tier,
		registryPath:  facts.RegistryPath,
		ledgerPath:    facts.LedgerPath,
	}
}

func (s *Session) serve() {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.handleConn(conn)
	}
}

func (s *Session) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(agentConnDeadline))
	resp := s.handle(conn)
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		return
	}
}

// agentRequest is the bounded, authority-free payload the contained agent may
// submit. Decoding is strict (DisallowUnknownFields), so any agent-supplied
// authority field (project, uid, tier, registry, ledger, key, token,
// attestation, fingerprint, …) is rejected.
type agentRequest struct {
	Op                 string             `json:"op"`
	OriginIssueID      string             `json:"origin_issue_id"`
	TargetProject      string             `json:"target_project"`
	Title              string             `json:"title"`
	Description        string             `json:"description"`
	AcceptanceCriteria string             `json:"acceptance_criteria"`
	Priority           int                `json:"priority"`
	IssueType          string             `json:"issue_type"`
	Dependencies       *agentDependencies `json:"dependencies,omitempty"`
}

type agentDependencies struct {
	Refs   []agentDependencyRef `json:"refs,omitempty"`
	Labels []string             `json:"labels,omitempty"`
}

type agentDependencyRef struct {
	Project string `json:"project"`
	IssueID string `json:"issue_id"`
}

// agentResponse is the bounded reply. It never carries token, key, or
// attestation material — only the broker's outcome message or a non-secret error.
type agentResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func failClosed(err error) agentResponse {
	return agentResponse{OK: false, Error: err.Error()}
}

func (s *Session) handle(conn net.Conn) agentResponse {
	// Defense in depth: never serve a request for an unconfirmed session.
	if err := confirmSandboxBoundary(s.facts); err != nil {
		return failClosed(err)
	}

	dec := json.NewDecoder(io.LimitReader(conn, int64(s.maxRequest)+1))
	dec.DisallowUnknownFields()
	var req agentRequest
	if err := dec.Decode(&req); err != nil {
		return failClosed(fmt.Errorf("malformed or unauthorized request: %w", err))
	}

	switch req.Op {
	case opDeliver, opReview:
	default:
		return failClosed(fmt.Errorf("unsupported op %q (allowed: deliver, review)", req.Op))
	}
	if req.OriginIssueID == "" {
		return failClosed(errors.New("origin_issue_id is required"))
	}
	if req.TargetProject == "" {
		return failClosed(errors.New("target_project is required"))
	}

	auth := deriveAuthorityFromLaunchFacts(s.facts)

	var deps *request.DependencyIntent
	if req.Dependencies != nil {
		normalized := request.NormalizeDependencyIntent(toContractDeps(req.Dependencies))
		if !normalized.Empty() {
			deps = &normalized
		}
	}
	fingerprint, err := request.Fingerprint(request.Input{
		OriginProject:      auth.originProject,
		OriginIssueID:      req.OriginIssueID,
		TargetProject:      req.TargetProject,
		Title:              req.Title,
		Description:        req.Description,
		AcceptanceCriteria: req.AcceptanceCriteria,
		Priority:           req.Priority,
		IssueType:          req.IssueType,
		Dependencies:       deps,
	})
	if err != nil {
		return failClosed(fmt.Errorf("compute request fingerprint: %w", err))
	}

	result, err := s.invokeDelivery(context.Background(), req.Op, auth, req, fingerprint)
	if err != nil {
		return failClosed(err)
	}
	return agentResponse{OK: true, Message: result.Message}
}

// invokeDelivery mints the v2 attestation from the derived authority + computed
// fingerprint and forwards the request to beadpost-broker via the .4 client.
// It is the DeliveryOnlyFromAcceptedRequest enforcement point.
func (s *Session) invokeDelivery(ctx context.Context, op string, auth authority, req agentRequest, fingerprint string) (Result, error) {
	now := time.Now().UTC()
	token, err := Mint(MintInput{
		ProjectPath: auth.projectPath,
		AgentUID:    auth.agentUID,
		Tier:        auth.tier,
		Fingerprint: fingerprint,
		IssuedAt:    now,
		ExpiresAt:   now.Add(attestationTTL),
	}, s.key)
	if err != nil {
		return Result{}, fmt.Errorf("mint containment attestation: %w", err)
	}
	sub := Submission{
		RegistryPath:  auth.registryPath,
		LedgerPath:    auth.ledgerPath,
		OriginProject: auth.originProject,
		OriginIssueID: req.OriginIssueID,
		TargetProject: req.TargetProject,
		Attestation:   token,
	}
	switch op {
	case opReview:
		return s.submitter.Review(ctx, sub)
	default:
		return s.submitter.Deliver(ctx, sub)
	}
}

func toContractDeps(in *agentDependencies) request.DependencyIntent {
	out := request.DependencyIntent{Labels: append([]string(nil), in.Labels...)}
	for _, ref := range in.Refs {
		out.Refs = append(out.Refs, request.DependencyRef{Project: ref.Project, IssueID: ref.IssueID})
	}
	return out
}

// Close stops serving, removes the socket, and cleans up the runtime dir. It is
// idempotent. This is the CloseSession lifecycle endpoint.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.listener != nil {
			_ = s.listener.Close()
		}
		if s.done != nil {
			<-s.done
		}
		_ = os.RemoveAll(s.runtimeDir)
	})
	return nil
}
