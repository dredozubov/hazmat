//go:build beadpost_hostbroker

// Package hostbroker is the Hazmat-side, Dolt-free client for the dr-owned
// Beadpost host broker. It mints beadpost.containment.attestation.v2 tokens from
// trusted launch facts + the effective .5 tier + a request fingerprint, and
// forwards typed requests to beadpost-broker over the beadpost.hostbroker.v1
// Unix-socket IPC contract.
//
// Arch B: Hazmat does NOT import the Beadpost root module and does NOT read
// registry/ledger/policy. The v2 attestation, request fingerprint, and IPC
// schema are the single, shared local/beadpost-contracts module —
// Hazmat reuses it rather than reimplementing it, and never links Beadpost/Dolt.
// The HMAC host-authority key (custody: hazmat/attestationkey) is shared by the
// two dr-owned processes — Hazmat signs, beadpost-broker verifies — and is
// denied to the contained agent.
package hostbroker

import (
	"errors"
	"time"

	"hazmat/attestationkey"
	"hazmat/attestationtier"

	"local/beadpost-contracts/attestation"
)

// SchemaV2 is the v2 containment attestation schema (shared contract).
const SchemaV2 = attestation.SchemaV2

// Token is the shared attestation token; aliased so the Hazmat host-broker API
// stays self-contained while the definition lives in one place.
type Token = attestation.Token

// MintInput is the trusted, host-derived authority for a v2 token: launch facts
// (ProjectPath, AgentUID), the effective .5 tier, and the request fingerprint.
// None of these come from the contained agent.
type MintInput struct {
	ProjectPath string
	AgentUID    int
	Tier        attestationtier.Tier
	Fingerprint string
	IssuedAt    time.Time
	ExpiresAt   time.Time
	// Nonce is random when empty; set it only for deterministic tests.
	Nonce string
}

// Mint produces a v2 token signed with the shared host-authority key. The
// attestationkey.Key signs (it implements attestation.Signer), so raw key bytes
// never cross the API. The signature binds project/uid/tier/fingerprint.
func Mint(in MintInput, key attestationkey.Key) (Token, error) {
	if !key.Valid() {
		return Token{}, errors.New("attestation key is not configured")
	}
	return attestation.SignV2(attestation.SignInputV2{
		ProjectPath: in.ProjectPath,
		AgentUID:    in.AgentUID,
		Tier:        string(in.Tier),
		IssuedAt:    in.IssuedAt,
		ExpiresAt:   in.ExpiresAt,
		Nonce:       in.Nonce,
		Fingerprint: in.Fingerprint,
	}, key)
}

// VerifyInput is the expected authority a verifier checks a token against.
type VerifyInput struct {
	ProjectPath string
	AgentUID    int
	Tier        string
	Fingerprint string
	Now         time.Time
}

// Verify checks a v2 token via the shared contract (v2-only, fingerprint-bound).
// Hazmat uses it to self-check before forwarding and in tests; the authoritative
// verification is the broker's.
func Verify(token Token, key attestationkey.Key, in VerifyInput) error {
	if !key.Valid() {
		return errors.New("attestation key is not configured")
	}
	return attestation.VerifyV2(token, key, attestation.VerifyInputV2{
		ProjectPath: in.ProjectPath,
		AgentUID:    in.AgentUID,
		Tier:        in.Tier,
		Fingerprint: in.Fingerprint,
		Now:         in.Now,
	})
}
