package hostbroker

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hazmat/attestationkey"
	"hazmat/attestationtier"
)

// fakeBroker is an in-test Unix-socket server speaking beadpost.hostbroker.v1.
// It replies via a caller-supplied handler and publishes the decoded request on
// a buffered channel (no shared-memory race with the test goroutine).
type fakeBroker struct {
	ln      net.Listener
	reqCh   chan wireRequest
	handler func(wireRequest) wireResponse
}

func startFakeBroker(t *testing.T, handler func(wireRequest) wireResponse) *fakeBroker {
	t.Helper()
	// A short dir keeps the socket path under the ~104-byte sun_path limit
	// (t.TempDir() embeds the long test name and overflows it on macOS).
	dir, err := os.MkdirTemp("", "hb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	fb := &fakeBroker{ln: ln, reqCh: make(chan wireRequest, 1), handler: handler}
	go fb.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return fb
}

func (fb *fakeBroker) socketPath() string { return fb.ln.Addr().String() }

func (fb *fakeBroker) serve() {
	for {
		conn, err := fb.ln.Accept()
		if err != nil {
			return
		}
		var req wireRequest
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			_ = conn.Close()
			continue
		}
		resp := fb.handler(req)
		encErr := json.NewEncoder(conn).Encode(resp)
		_ = conn.Close()
		if encErr != nil {
			continue
		}
		select {
		case fb.reqCh <- req:
		default:
		}
	}
}

func okHandler(message string) func(wireRequest) wireResponse {
	return func(wireRequest) wireResponse {
		return wireResponse{Schema: WireSchema, OK: true, Message: message}
	}
}

// verifyingHandler simulates the broker: it independently verifies the token
// against the authority it expects (registry/envelope-derived), rejecting on any
// mismatch.
func verifyingHandler(key attestationkey.Key, expect VerifyInput) func(wireRequest) wireResponse {
	return func(req wireRequest) wireResponse {
		if err := Verify(req.Attestation, key, expect); err != nil {
			return wireResponse{Schema: WireSchema, OK: false, Error: err.Error()}
		}
		return wireResponse{Schema: WireSchema, OK: true, Message: "accepted"}
	}
}

func testSubmission(tok Token) Submission {
	return Submission{
		RegistryPath:  "/dr/projects.json",
		LedgerPath:    "/dr/ledger.sqlite3",
		OriginProject: "api",
		OriginIssueID: "api-1",
		TargetProject: "web",
		Attestation:   tok,
	}
}

func mintFor(t *testing.T, key attestationkey.Key, projectDir, fp string) Token {
	t.Helper()
	tok, err := Mint(MintInput{
		ProjectPath: projectDir, AgentUID: 599, Tier: attestationtier.Contained, Fingerprint: fp,
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	}, key)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return tok
}

func TestClientDispatchAndMapping(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	projectDir := t.TempDir()
	tok := mintFor(t, key, projectDir, "sha256:route")
	sub := testSubmission(tok)

	type opcall struct {
		name string
		call func(*Client, context.Context, Submission) (Result, error)
		want string
	}
	for _, oc := range []opcall{
		{"deliver", (*Client).Deliver, opDeliver},
		{"review", (*Client).Review, opReview},
		{"decide", func(c *Client, ctx context.Context, s Submission) (Result, error) {
			s.Decision = "disclosure_grant"
			s.DecidedBy = "operator-1"
			return c.Decide(ctx, s)
		}, opDecide},
	} {
		t.Run(oc.name, func(t *testing.T) {
			fb := startFakeBroker(t, okHandler("done-"+oc.name))
			res, err := oc.call(NewClient(fb.socketPath()), context.Background(), sub)
			if err != nil {
				t.Fatalf("%s: %v", oc.name, err)
			}
			if res.Message != "done-"+oc.name {
				t.Fatalf("%s: message = %q", oc.name, res.Message)
			}
			got := <-fb.reqCh
			if got.Schema != WireSchema || got.Op != oc.want {
				t.Fatalf("%s: schema/op = %q/%q", oc.name, got.Schema, got.Op)
			}
			// Authority fields must be carried from the signed token, never blank.
			if got.Fingerprint != tok.Fingerprint || got.ExpectedAgentUID != tok.AgentUID || got.RequiredTier != tok.Tier {
				t.Fatalf("%s: authority not derived from token: %+v", oc.name, got)
			}
			if got.Attestation.Signature != tok.Signature {
				t.Fatalf("%s: attestation not forwarded intact", oc.name)
			}
			if got.OriginProject != "api" || got.TargetProject != "web" {
				t.Fatalf("%s: route mapped incorrectly: %+v", oc.name, got)
			}
		})
	}
}

func TestClientFailsClosedOnBrokerError(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	tok := mintFor(t, key, t.TempDir(), "sha256:route")
	fb := startFakeBroker(t, func(wireRequest) wireResponse {
		return wireResponse{Schema: WireSchema, OK: false, Error: "containment attestation: tier mismatch"}
	})
	if _, err := NewClient(fb.socketPath()).Deliver(context.Background(), testSubmission(tok)); err == nil {
		t.Fatal("a broker rejection must surface as an error")
	}
}

func TestClientRejectsUnexpectedResponseSchema(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	tok := mintFor(t, key, t.TempDir(), "sha256:route")
	fb := startFakeBroker(t, func(wireRequest) wireResponse {
		return wireResponse{Schema: "beadpost.hostbroker.v999", OK: true, Message: "x"}
	})
	if _, err := NewClient(fb.socketPath()).Deliver(context.Background(), testSubmission(tok)); err == nil {
		t.Fatal("an unexpected response schema must fail closed")
	}
}

// TestClientNeverSendsNonV2 proves v1 is never used on this path: the client
// refuses a non-v2 attestation before dialing the broker.
func TestClientNeverSendsNonV2(t *testing.T) {
	dialed := false
	fb := startFakeBroker(t, func(wireRequest) wireResponse {
		dialed = true
		return okHandler("x")(wireRequest{})
	})
	v1 := Token{Schema: "beadpost.containment.attestation.v1", Fingerprint: "sha256:x"}
	if _, err := NewClient(fb.socketPath()).Deliver(context.Background(), testSubmission(v1)); err == nil {
		t.Fatal("client must refuse a non-v2 attestation")
	}
	if dialed {
		t.Fatal("client must not contact the broker with a non-v2 attestation")
	}
}

// TestSubmitBindingFailsClosed proves end-to-end that a token minted from the
// true launch facts cannot produce an accepted request when the broker expects a
// different project / uid / tier / fingerprint.
func TestSubmitBindingFailsClosed(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	projectDir := t.TempDir()
	const fp = "sha256:route"
	sub := testSubmission(mintFor(t, key, projectDir, fp))

	// Happy: the broker expects exactly the minting facts.
	fb := startFakeBroker(t, verifyingHandler(key, VerifyInput{ProjectPath: projectDir, AgentUID: 599, Tier: "contained", Fingerprint: fp}))
	if res, err := NewClient(fb.socketPath()).Deliver(context.Background(), sub); err != nil || res.Message != "accepted" {
		t.Fatalf("happy path: res=%+v err=%v", res, err)
	}

	for name, expect := range map[string]VerifyInput{
		"wrong project":     {ProjectPath: t.TempDir(), AgentUID: 599, Tier: "contained", Fingerprint: fp},
		"wrong uid":         {ProjectPath: projectDir, AgentUID: 600, Tier: "contained", Fingerprint: fp},
		"wrong tier":        {ProjectPath: projectDir, AgentUID: 599, Tier: "native-uncontained", Fingerprint: fp},
		"wrong fingerprint": {ProjectPath: projectDir, AgentUID: 599, Tier: "contained", Fingerprint: "sha256:other"},
	} {
		t.Run(name, func(t *testing.T) {
			fb := startFakeBroker(t, verifyingHandler(key, expect))
			if _, err := NewClient(fb.socketPath()).Deliver(context.Background(), sub); err == nil {
				t.Fatalf("%s: a mismatched broker expectation must reject the request", name)
			}
		})
	}
}
