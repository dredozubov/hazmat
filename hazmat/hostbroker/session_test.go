//go:build beadpost_hostbroker

package hostbroker

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"

	"hazmat/attestationkey"
	"hazmat/attestationtier"

	"local/beadpost-contracts/request"
)

type capturedCall struct {
	op  string
	sub Submission
}

type fakeSubmitter struct {
	ch     chan capturedCall
	result Result
	err    error
}

func newFakeSubmitter(result Result, err error) *fakeSubmitter {
	return &fakeSubmitter{ch: make(chan capturedCall, 4), result: result, err: err}
}

func (f *fakeSubmitter) Deliver(_ context.Context, s Submission) (Result, error) {
	f.ch <- capturedCall{"deliver", s}
	return f.result, f.err
}

func (f *fakeSubmitter) Review(_ context.Context, s Submission) (Result, error) {
	f.ch <- capturedCall{"review", s}
	return f.result, f.err
}

// shortRuntimeDir keeps the socket path under the ~104-byte sun_path limit.
func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hb6")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func validFacts(t *testing.T) LaunchFacts {
	return LaunchFacts{
		OriginProject:    "api",
		ProjectPath:      t.TempDir(), // real dir → stable normalization
		AgentUID:         599,
		Tier:             attestationtier.Contained,
		RegistryPath:     "/dr/projects.json",
		LedgerPath:       "/dr/ledger.sqlite3",
		SandboxConfirmed: true,
	}
}

func openTestSession(t *testing.T, facts LaunchFacts, sub Submitter, key attestationkey.Key, maxBytes int) *Session {
	t.Helper()
	s, err := Open(SessionConfig{
		Facts:           facts,
		RuntimeDir:      shortRuntimeDir(t),
		Submitter:       sub,
		Key:             key,
		MaxRequestBytes: maxBytes,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// roundTrip sends raw bytes to the agent socket and returns the response.
func roundTrip(t *testing.T, socketPath string, payload []byte) agentResponse {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	var resp agentResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func deliverPayload(t *testing.T) []byte {
	return mustMarshal(t, agentRequest{
		Op:                 "deliver",
		OriginIssueID:      "api-1",
		TargetProject:      "web",
		Title:              "Need cursor pagination",
		Description:        "Expose cursor pagination.",
		AcceptanceCriteria: "Done when cursors work.",
		Priority:           2,
		IssueType:          "task",
	})
}

func TestOpenGatedOnConfirmedContainment(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")

	t.Run("unconfirmed sandbox", func(t *testing.T) {
		facts := validFacts(t)
		facts.SandboxConfirmed = false
		if _, err := Open(SessionConfig{Facts: facts, RuntimeDir: shortRuntimeDir(t), Submitter: newFakeSubmitter(Result{}, nil), Key: key}); err == nil {
			t.Fatal("Open must fail closed when containment is not confirmed")
		}
	})

	for name, mut := range map[string]func(*LaunchFacts){
		"missing origin project": func(f *LaunchFacts) { f.OriginProject = "" },
		"missing project path":   func(f *LaunchFacts) { f.ProjectPath = "" },
		"missing agent uid":      func(f *LaunchFacts) { f.AgentUID = 0 },
		"missing tier":           func(f *LaunchFacts) { f.Tier = "" },
		"missing registry":       func(f *LaunchFacts) { f.RegistryPath = "" },
		"missing ledger":         func(f *LaunchFacts) { f.LedgerPath = "" },
	} {
		t.Run(name, func(t *testing.T) {
			facts := validFacts(t)
			mut(&facts)
			if _, err := Open(SessionConfig{Facts: facts, RuntimeDir: shortRuntimeDir(t), Submitter: newFakeSubmitter(Result{}, nil), Key: key}); err == nil {
				t.Fatalf("Open must fail closed: %s", name)
			}
		})
	}

	t.Run("missing key", func(t *testing.T) {
		if _, err := Open(SessionConfig{Facts: validFacts(t), RuntimeDir: shortRuntimeDir(t), Submitter: newFakeSubmitter(Result{}, nil)}); err == nil {
			t.Fatal("Open must fail closed without a configured key")
		}
	})
}

func TestSessionLifecycleAndCleanup(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	runtimeDir := shortRuntimeDir(t)
	s, err := Open(SessionConfig{Facts: validFacts(t), RuntimeDir: runtimeDir, Submitter: newFakeSubmitter(Result{}, nil), Key: key})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.SocketPath() == "" {
		t.Fatal("expected a socket path")
	}
	if _, err := os.Stat(s.SocketPath()); err != nil {
		t.Fatalf("socket should exist: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(s.SocketPath()); !os.IsNotExist(err) {
		t.Fatal("Close must remove the socket")
	}
	if _, err := os.Stat(runtimeDir); !os.IsNotExist(err) {
		t.Fatal("Close must remove the runtime dir")
	}
	if err := s.Close(); err != nil { // idempotent
		t.Fatalf("second Close: %v", err)
	}
}

func TestDeliverDispatchDerivesAuthorityAndFingerprint(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	facts := validFacts(t)
	fake := newFakeSubmitter(Result{Message: "created decision=auto_deliver proxy=web-5"}, nil)
	s := openTestSession(t, facts, fake, key, 0)

	resp := roundTrip(t, s.SocketPath(), deliverPayload(t))
	if !resp.OK || resp.Message != "created decision=auto_deliver proxy=web-5" {
		t.Fatalf("response = %+v", resp)
	}

	got := <-fake.ch
	if got.op != "deliver" {
		t.Fatalf("op = %q, want deliver", got.op)
	}
	sub := got.sub
	// Route + dr config carried; authority comes from facts, not the agent.
	if sub.OriginProject != "api" || sub.OriginIssueID != "api-1" || sub.TargetProject != "web" {
		t.Fatalf("route mapped wrong: %+v", sub)
	}
	if sub.RegistryPath != facts.RegistryPath || sub.LedgerPath != facts.LedgerPath {
		t.Fatalf("dr config not from facts: %+v", sub)
	}
	if sub.Attestation.Schema != SchemaV2 || sub.Attestation.AgentUID != 599 || sub.Attestation.Tier != "contained" {
		t.Fatalf("attestation authority not from facts: %+v", sub.Attestation)
	}

	// The host computed the fingerprint from canonical content via the shared contract.
	wantFP, err := request.Fingerprint(request.Input{
		OriginProject: "api", OriginIssueID: "api-1", TargetProject: "web",
		Title: "Need cursor pagination", Description: "Expose cursor pagination.",
		AcceptanceCriteria: "Done when cursors work.", Priority: 2, IssueType: "task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Attestation.Fingerprint != wantFP {
		t.Fatalf("fingerprint = %q, want host-computed %q", sub.Attestation.Fingerprint, wantFP)
	}
	// The minted token verifies against the facts + computed fingerprint.
	if err := Verify(sub.Attestation, key, VerifyInput{ProjectPath: facts.ProjectPath, AgentUID: 599, Tier: "contained", Fingerprint: wantFP}); err != nil {
		t.Fatalf("minted token must verify against launch facts: %v", err)
	}
}

func TestReviewDispatch(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	fake := newFakeSubmitter(Result{Message: "beadpost review …"}, nil)
	s := openTestSession(t, validFacts(t), fake, key, 0)

	payload := mustMarshal(t, agentRequest{Op: "review", OriginIssueID: "api-1", TargetProject: "web", Title: "x"})
	resp := roundTrip(t, s.SocketPath(), payload)
	if !resp.OK {
		t.Fatalf("review response = %+v", resp)
	}
	if got := <-fake.ch; got.op != "review" {
		t.Fatalf("op = %q, want review", got.op)
	}
}

func TestRejectsUnsupportedOps(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	fake := newFakeSubmitter(Result{}, nil)
	s := openTestSession(t, validFacts(t), fake, key, 0)

	for _, op := range []string{"decide", "frobnicate", ""} {
		payload := mustMarshal(t, agentRequest{Op: op, OriginIssueID: "api-1", TargetProject: "web"})
		resp := roundTrip(t, s.SocketPath(), payload)
		if resp.OK {
			t.Fatalf("op %q must be rejected", op)
		}
	}
	if len(fake.ch) != 0 {
		t.Fatal("no request should have been dispatched")
	}
}

func TestRejectsAgentSuppliedAuthorityFields(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	fake := newFakeSubmitter(Result{}, nil)
	s := openTestSession(t, validFacts(t), fake, key, 0)

	base := `"op":"deliver","origin_issue_id":"api-1","target_project":"web","title":"t"`
	for _, field := range []string{
		`"origin_project":"evil"`,
		`"project":"evil"`,
		`"agent_uid":0`,
		`"uid":0`,
		`"tier":"contained"`,
		`"registry_path":"/x"`,
		`"ledger_path":"/x"`,
		`"key_path":"/x"`,
		`"key":"deadbeef"`,
		`"token":"x"`,
		`"attestation":{"schema":"v2"}`,
		`"fingerprint":"sha256:forged"`,
		`"signature":"hmac-sha256:forged"`,
	} {
		payload := []byte("{" + base + "," + field + "}")
		resp := roundTrip(t, s.SocketPath(), payload)
		if resp.OK {
			t.Fatalf("authority field %s must be rejected", field)
		}
	}
	if len(fake.ch) != 0 {
		t.Fatal("no request with an authority field should be dispatched")
	}
}

func TestRejectsMalformedAndOversized(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	fake := newFakeSubmitter(Result{}, nil)
	s := openTestSession(t, validFacts(t), fake, key, 256) // small cap

	if resp := roundTrip(t, s.SocketPath(), []byte("{not json")); resp.OK {
		t.Fatal("malformed payload must be rejected")
	}
	big := mustMarshal(t, agentRequest{Op: "deliver", OriginIssueID: "api-1", TargetProject: "web", Description: strings.Repeat("x", 1024)})
	if resp := roundTrip(t, s.SocketPath(), big); resp.OK {
		t.Fatal("oversized payload must be rejected")
	}
	if len(fake.ch) != 0 {
		t.Fatal("malformed/oversized requests must not dispatch")
	}
}

func TestResponseNeverLeaksKeyOrToken(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	fake := newFakeSubmitter(Result{Message: "created"}, nil)
	s := openTestSession(t, validFacts(t), fake, key, 0)

	resp := roundTrip(t, s.SocketPath(), deliverPayload(t))
	got := <-fake.ch
	data := mustMarshal(t, resp)
	// The response must not echo the minted signature/token or key material.
	if strings.Contains(string(data), "hmac-sha256:") || strings.Contains(string(data), got.sub.Attestation.Signature) || strings.Contains(string(data), "0123456789abcdef") {
		t.Fatalf("response leaked attestation/key material: %s", data)
	}
}

func TestSocketModeRestrictive(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	s := openTestSession(t, validFacts(t), newFakeSubmitter(Result{}, nil), key, 0)
	info, err := os.Stat(s.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o007 != 0 {
		t.Fatalf("socket mode %#o grants world access", perm)
	}
}
