//go:build !beadpost_hostbroker

// Package hostbroker (stub build): Beadpost host-broker support is compiled OUT
// of the default/public Hazmat build. These dependency-free stubs let the
// package and its consumers compile without importing local/beadpost-contracts;
// every operation fails closed with a clear "rebuild with -tags
// beadpost_hostbroker" error. The real implementation — which mints v2
// containment attestations and speaks the beadpost.hostbroker.v1 IPC — lives in
// the files behind //go:build beadpost_hostbroker.
package hostbroker

import (
	"context"
	"errors"
	"os"
	"time"

	"hazmat/attestationkey"
	"hazmat/attestationtier"
)

// SchemaV2 mirrors the shared v2 attestation schema constant.
const SchemaV2 = "beadpost.containment.attestation.v2"

// ErrDisabled is returned by every host-broker operation in the default build.
// Operators enable real support with `-tags beadpost_hostbroker` (and a go.work
// that includes local/beadpost-contracts).
var ErrDisabled = errors.New("hazmat: Beadpost host-broker support is not built; rebuild with -tags beadpost_hostbroker")

// Token mirrors the shared attestation token shape so consumers compile; the
// default build never produces or verifies one.
type Token struct {
	Schema      string `json:"schema"`
	ProjectPath string `json:"project_path"`
	AgentUID    int    `json:"agent_uid"`
	Tier        string `json:"tier"`
	IssuedAt    string `json:"issued_at"`
	ExpiresAt   string `json:"expires_at"`
	Nonce       string `json:"nonce"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Signature   string `json:"signature"`
}

// MintInput is the trusted, host-derived authority for a v2 token.
type MintInput struct {
	ProjectPath string
	AgentUID    int
	Tier        attestationtier.Tier
	Fingerprint string
	IssuedAt    time.Time
	ExpiresAt   time.Time
	Nonce       string
}

// VerifyInput is the expected authority a verifier checks a token against.
type VerifyInput struct {
	ProjectPath string
	AgentUID    int
	Tier        string
	Fingerprint string
	Now         time.Time
}

// Mint always fails closed in the default build.
func Mint(MintInput, attestationkey.Key) (Token, error) { return Token{}, ErrDisabled }

// Verify always fails closed in the default build.
func Verify(Token, attestationkey.Key, VerifyInput) error { return ErrDisabled }

// Result is the successful broker outcome surfaced to the caller.
type Result struct {
	Message string
}

// Submission is the dr-assembled host-broker request.
type Submission struct {
	RegistryPath string
	LedgerPath   string

	OriginProject string
	OriginIssueID string
	TargetProject string

	Attestation Token

	DryRun bool

	Decision   string
	DecidedBy  string
	Note       string
	DeferUntil *time.Time
}

// Client is a no-op host-broker client in the default build.
type Client struct{}

func NewClient(string) *Client { return &Client{} }

func (*Client) Deliver(context.Context, Submission) (Result, error) { return Result{}, ErrDisabled }
func (*Client) Review(context.Context, Submission) (Result, error)  { return Result{}, ErrDisabled }
func (*Client) Decide(context.Context, Submission) (Result, error)  { return Result{}, ErrDisabled }

// LaunchFacts is the host-derived authority for a contained session (.6).
type LaunchFacts struct {
	OriginProject string
	ProjectPath   string
	AgentUID      int
	Tier          attestationtier.Tier
	RegistryPath  string
	LedgerPath    string

	SandboxConfirmed bool
}

// Submitter is the .4 host-broker client surface the session forwards to.
type Submitter interface {
	Deliver(context.Context, Submission) (Result, error)
	Review(context.Context, Submission) (Result, error)
}

// SessionConfig configures a per-session agent socket (.6).
type SessionConfig struct {
	Facts           LaunchFacts
	RuntimeDir      string
	Submitter       Submitter
	Key             attestationkey.Key
	SocketMode      os.FileMode
	MaxRequestBytes int
}

// Session is a no-op per-session agent socket in the default build.
type Session struct{}

// Open always fails closed in the default build: no agent socket is created.
func Open(SessionConfig) (*Session, error) { return nil, ErrDisabled }

func (*Session) SocketPath() string { return "" }

func (*Session) Close() error { return nil }
