//go:build beadpost_hostbroker

package hostbroker

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hazmat/attestationtier"

	"local/beadpost-contracts/contractfixture"
	"local/beadpost-contracts/hostbrokerwire"
	"local/beadpost-contracts/request"
)

// TestRequestFingerprintMatchesContractFixture proves Hazmat computes the same
// canonical request fingerprint as the shared contract — consuming the fixture,
// not a local golden.
func TestRequestFingerprintMatchesContractFixture(t *testing.T) {
	fp, err := request.Fingerprint(contractfixture.RequestInput)
	if err != nil {
		t.Fatal(err)
	}
	if fp != contractfixture.RequestFingerprint {
		t.Fatalf("fingerprint = %q, want fixture %q", fp, contractfixture.RequestFingerprint)
	}
}

// TestHostBrokerWireMatchesContractFixture proves Hazmat's client encodes the
// beadpost.hostbroker.v1 request to the canonical fixture shape (the same types
// the Beadpost daemon decodes).
func TestHostBrokerWireMatchesContractFixture(t *testing.T) {
	data, err := json.Marshal(contractfixture.HostBrokerRequest)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"schema":"beadpost.hostbroker.v1"`,
		`"op":"deliver"`,
		`"origin_project":"api"`,
		`"required_tier":"contained"`,
		`"signature":"` + contractfixture.AttestationV2Signature + `"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("hostbroker request JSON missing %s:\n%s", want, data)
		}
	}
	var back hostbrokerwire.Request
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Op != hostbrokerwire.OpDeliver || back.Attestation != contractfixture.AttestationV2Token {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}

// TestTokenForOneRequestCannotAuthorizeAnother is the core non-interference
// property: a v2 token bound to request A's fingerprint cannot authorize a
// different request B.
func TestTokenForOneRequestCannotAuthorizeAnother(t *testing.T) {
	key := loadFixedKey(t, string(contractfixture.Key))
	projectDir := t.TempDir()
	now := time.Now().UTC()
	tokA, err := Mint(MintInput{
		ProjectPath: projectDir, AgentUID: 7, Tier: attestationtier.Contained,
		Fingerprint: "sha256:request-A", IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(tokA, key, VerifyInput{ProjectPath: projectDir, AgentUID: 7, Tier: "contained", Fingerprint: "sha256:request-B", Now: now.Add(time.Minute)}); err == nil {
		t.Fatal("a token bound to request A must not authorize request B")
	}
	if err := Verify(tokA, key, VerifyInput{ProjectPath: projectDir, AgentUID: 7, Tier: "contained", Fingerprint: "sha256:request-A", Now: now.Add(time.Minute)}); err != nil {
		t.Fatalf("token A must authorize request A: %v", err)
	}
}

// TestReplayNotYetPreventedAtVerify pins CURRENT behavior: bp-fyg.1 (the
// nonce-consumption ledger) is OPEN, so the v2 token signs its nonce but nothing
// consumes it — a valid token verifies repeatedly within its TTL. This test
// exists so we never accidentally claim replay prevention before bp-fyg.1 lands.
// Replay/idempotency is a Beadpost-side obligation (bp-fyg.1), not modeled here.
func TestReplayNotYetPreventedAtVerify(t *testing.T) {
	key := loadFixedKey(t, string(contractfixture.Key))
	projectDir := t.TempDir()
	now := time.Now().UTC()
	tok, err := Mint(MintInput{
		ProjectPath: projectDir, AgentUID: 7, Tier: attestationtier.Contained,
		Fingerprint: "sha256:replayable", IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	in := VerifyInput{ProjectPath: projectDir, AgentUID: 7, Tier: "contained", Fingerprint: "sha256:replayable", Now: now.Add(time.Minute)}
	if err := Verify(tok, key, in); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	if err := Verify(tok, key, in); err != nil {
		t.Fatalf("replay is not prevented at the Verify layer until bp-fyg.1; second verify unexpectedly failed: %v", err)
	}
}
