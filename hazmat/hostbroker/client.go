package hostbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	// WireSchema mirrors internal/hostbrokerd.Schema (the daemon's versioned IPC
	// envelope). An unknown response schema fails closed.
	WireSchema = "beadpost.hostbroker.v1"

	opDeliver = "deliver"
	opReview  = "review"
	opDecide  = "decide"

	dialTimeout = 5 * time.Second
	ioTimeout   = 30 * time.Second
)

// wireRequest mirrors internal/hostbrokerd.Request. Reimplemented here so Hazmat
// speaks the contract without importing Beadpost.
type wireRequest struct {
	Schema string `json:"schema"`
	Op     string `json:"op"`

	RegistryPath string `json:"registry_path"`
	LedgerPath   string `json:"ledger_path"`

	OriginProject string `json:"origin_project"`
	OriginIssueID string `json:"origin_issue_id"`
	TargetProject string `json:"target_project"`
	Fingerprint   string `json:"fingerprint"`

	ExpectedAgentUID int    `json:"expected_agent_uid"`
	RequiredTier     string `json:"required_tier"`

	Attestation Token `json:"attestation"`

	DryRun bool `json:"dry_run,omitempty"`

	Decision   string `json:"decision,omitempty"`
	DecidedBy  string `json:"decided_by,omitempty"`
	Note       string `json:"note,omitempty"`
	DeferUntil string `json:"defer_until,omitempty"`
}

// wireResponse mirrors internal/hostbrokerd.Response.
type wireResponse struct {
	Schema  string `json:"schema"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Result is the successful broker outcome surfaced to the caller.
type Result struct {
	Message string
}

// Submission is the dr-assembled request: opaque broker config (registry/ledger
// paths the broker resolves — Hazmat never reads them), the route, the minted v2
// token, and (for decisions) the operator's verdict. The request's authority
// fields (uid/tier/fingerprint) are taken from the signed token, so the wire
// request can never disagree with the attestation.
type Submission struct {
	RegistryPath string
	LedgerPath   string

	OriginProject string
	OriginIssueID string
	TargetProject string

	Attestation Token

	DryRun bool

	// Decide-only.
	Decision   string
	DecidedBy  string
	Note       string
	DeferUntil *time.Time
}

// Client forwards typed requests to the dr-owned beadpost-broker over its
// Unix-domain socket. It never reads registry/ledger/policy.
type Client struct {
	socketPath string
}

func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

func (c *Client) Deliver(ctx context.Context, s Submission) (Result, error) {
	return c.call(ctx, opDeliver, s)
}

func (c *Client) Review(ctx context.Context, s Submission) (Result, error) {
	return c.call(ctx, opReview, s)
}

func (c *Client) Decide(ctx context.Context, s Submission) (Result, error) {
	return c.call(ctx, opDecide, s)
}

func (c *Client) call(ctx context.Context, op string, s Submission) (Result, error) {
	if c.socketPath == "" {
		return Result{}, errors.New("broker socket path is not configured")
	}
	if s.Attestation.Schema != SchemaV2 {
		return Result{}, fmt.Errorf("host-broker path requires a %s attestation, got %q", SchemaV2, s.Attestation.Schema)
	}
	req := wireRequest{
		Schema:        WireSchema,
		Op:            op,
		RegistryPath:  s.RegistryPath,
		LedgerPath:    s.LedgerPath,
		OriginProject: s.OriginProject,
		OriginIssueID: s.OriginIssueID,
		TargetProject: s.TargetProject,
		// Authority fields come from the signed token, never set independently.
		Fingerprint:      s.Attestation.Fingerprint,
		ExpectedAgentUID: s.Attestation.AgentUID,
		RequiredTier:     s.Attestation.Tier,
		Attestation:      s.Attestation,
		DryRun:           s.DryRun,
		Decision:         s.Decision,
		DecidedBy:        s.DecidedBy,
		Note:             s.Note,
	}
	if s.DeferUntil != nil {
		req.DeferUntil = s.DeferUntil.UTC().Format(time.RFC3339)
	}
	return c.roundTrip(ctx, req)
}

func (c *Client) roundTrip(ctx context.Context, req wireRequest) (Result, error) {
	var dialer net.Dialer
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, err := dialer.DialContext(dialCtx, "unix", c.socketPath)
	if err != nil {
		return Result{}, fmt.Errorf("connect to broker: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(ioTimeout))
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Result{}, fmt.Errorf("send request: %w", err)
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}

	var resp wireResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Result{}, fmt.Errorf("read broker response: %w", err)
	}
	if resp.Schema != WireSchema {
		return Result{}, fmt.Errorf("unexpected broker response schema %q", resp.Schema)
	}
	if !resp.OK {
		if resp.Error == "" {
			return Result{}, errors.New("broker rejected the request")
		}
		return Result{}, fmt.Errorf("broker rejected the request: %s", resp.Error)
	}
	return Result{Message: resp.Message}, nil
}
