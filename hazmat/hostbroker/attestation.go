// Package hostbroker is the Hazmat-side, Dolt-free client for the dr-owned
// Beadpost host broker. It mints beadpost.containment.attestation.v2 tokens from
// trusted launch facts + the effective .5 tier + a request fingerprint, and
// forwards typed requests to beadpost-broker over the beadpost.hostbroker.v1
// Unix-socket IPC contract.
//
// Arch B: Hazmat does NOT import Beadpost/Dolt and does NOT read
// registry/ledger/policy. The v2 token format and the IPC wire schema are shared
// contracts reimplemented here independently; byte/schema compatibility with
// Beadpost is proven by golden vectors in the tests. The HMAC host-authority key
// (custody: hazmat/attestationkey) is shared by the two dr-owned processes —
// Hazmat signs, beadpost-broker verifies — and is denied to the contained agent.
package hostbroker

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"hazmat/attestationkey"
	"hazmat/attestationtier"
)

const (
	// SchemaV2 mirrors beadpost/attestation.SchemaV2. Only v2 is ever minted or
	// accepted on the host-broker path; v1 is never used here.
	SchemaV2 = "beadpost.containment.attestation.v2"
	// DefaultTier mirrors beadpost/attestation.DefaultTier.
	DefaultTier = "contained"

	signaturePrefix = "hmac-sha256:"
)

// Token mirrors beadpost/attestation.Token field-for-field (JSON tags and order)
// so a token minted here marshals to the exact JSON the Beadpost broker decodes
// and verifies unchanged.
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

// tokenPayloadV2 is the exact signed payload (no signature field) whose canonical
// JSON must match Beadpost's byte-for-byte.
type tokenPayloadV2 struct {
	Schema      string `json:"schema"`
	ProjectPath string `json:"project_path"`
	AgentUID    int    `json:"agent_uid"`
	Tier        string `json:"tier"`
	IssuedAt    string `json:"issued_at"`
	ExpiresAt   string `json:"expires_at"`
	Nonce       string `json:"nonce"`
	Fingerprint string `json:"fingerprint"`
}

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
// signature binds project/uid/tier/fingerprint, so a token cannot be re-pointed
// at a different project, uid, tier, or request without invalidating it.
func Mint(in MintInput, key attestationkey.Key) (Token, error) {
	if !key.Valid() {
		return Token{}, errors.New("attestation key is not configured")
	}
	projectPath, err := normalizeProjectPath(in.ProjectPath)
	if err != nil {
		return Token{}, err
	}
	fingerprint := strings.TrimSpace(in.Fingerprint)
	if fingerprint == "" {
		return Token{}, errors.New("request fingerprint is required for v2")
	}
	tier, issuedAt, expiresAt, nonce, err := normalizeClaims(string(in.Tier), in.AgentUID, in.IssuedAt, in.ExpiresAt, in.Nonce)
	if err != nil {
		return Token{}, err
	}
	token := Token{
		Schema:      SchemaV2,
		ProjectPath: projectPath,
		AgentUID:    in.AgentUID,
		Tier:        tier,
		IssuedAt:    issuedAt,
		ExpiresAt:   expiresAt,
		Nonce:       nonce,
		Fingerprint: fingerprint,
	}
	token.Signature, err = sign(token, key)
	if err != nil {
		return Token{}, err
	}
	return token, nil
}

// VerifyInput is the expected authority a verifier checks a token against.
type VerifyInput struct {
	ProjectPath string
	AgentUID    int
	Tier        string
	Fingerprint string
	Now         time.Time
}

// Verify mirrors beadpost/attestation.VerifyV2: it requires a v2 schema, the
// signed fingerprint to equal the expected request fingerprint, and the
// project/uid/tier/expiry/signature claims to match. Hazmat uses it to self-check
// before forwarding and in tests; the authoritative verification is the broker's.
func Verify(token Token, key attestationkey.Key, in VerifyInput) error {
	if !key.Valid() {
		return errors.New("attestation key is not configured")
	}
	if token.Schema != SchemaV2 {
		return fmt.Errorf("unsupported attestation schema %q (host-broker path requires %s)", token.Schema, SchemaV2)
	}
	expectedFingerprint := strings.TrimSpace(in.Fingerprint)
	if expectedFingerprint == "" {
		return errors.New("expected request fingerprint is required")
	}
	if strings.TrimSpace(token.Fingerprint) == "" {
		return errors.New("attestation is missing a request fingerprint")
	}
	if token.Fingerprint != expectedFingerprint {
		return fmt.Errorf("attestation fingerprint %q does not match request %q", token.Fingerprint, expectedFingerprint)
	}
	expectedPath, err := normalizeProjectPath(in.ProjectPath)
	if err != nil {
		return err
	}
	tokenPath, err := normalizeProjectPath(token.ProjectPath)
	if err != nil {
		return fmt.Errorf("token project path: %w", err)
	}
	if tokenPath != expectedPath {
		return fmt.Errorf("attestation project path %q does not match expected %q", tokenPath, expectedPath)
	}
	if in.AgentUID > 0 && token.AgentUID != in.AgentUID {
		return fmt.Errorf("attestation agent UID %d does not match expected %d", token.AgentUID, in.AgentUID)
	}
	expectedTier := strings.TrimSpace(in.Tier)
	if expectedTier == "" {
		expectedTier = DefaultTier
	}
	if token.Tier != expectedTier {
		return fmt.Errorf("attestation tier %q does not match expected %q", token.Tier, expectedTier)
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, token.IssuedAt)
	if err != nil {
		return fmt.Errorf("attestation issued_at is invalid: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, token.ExpiresAt)
	if err != nil {
		return fmt.Errorf("attestation expires_at is invalid: %w", err)
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	if !expiresAt.After(now) {
		return errors.New("attestation is stale")
	}
	if issuedAt.After(now.Add(time.Minute)) {
		return errors.New("attestation issue time is in the future")
	}
	if err := verifySignature(token, key); err != nil {
		return err
	}
	return nil
}

// sign returns "hmac-sha256:"+hex over the canonical v2 payload, computed with
// the .3 key-custody Key (raw bytes never leave that package).
func sign(token Token, key attestationkey.Key) (string, error) {
	data, err := payloadBytes(token)
	if err != nil {
		return "", err
	}
	return signaturePrefix + hex.EncodeToString(key.Sign(data)), nil
}

// verifySignature recomputes the HMAC over the canonical payload and compares it
// to the token signature in constant time (via attestationkey.Key.Verify).
func verifySignature(token Token, key attestationkey.Key) error {
	data, err := payloadBytes(token)
	if err != nil {
		return err
	}
	hexSig, ok := strings.CutPrefix(token.Signature, signaturePrefix)
	if !ok {
		return errors.New("attestation signature has an unexpected format")
	}
	sig, err := hex.DecodeString(hexSig)
	if err != nil {
		return errors.New("attestation signature is not valid hex")
	}
	if !key.Verify(data, sig) {
		return errors.New("attestation signature is invalid")
	}
	return nil
}

func payloadBytes(token Token) ([]byte, error) {
	if token.Schema != SchemaV2 {
		return nil, fmt.Errorf("unsupported attestation schema %q", token.Schema)
	}
	return json.Marshal(tokenPayloadV2{
		Schema:      token.Schema,
		ProjectPath: token.ProjectPath,
		AgentUID:    token.AgentUID,
		Tier:        token.Tier,
		IssuedAt:    token.IssuedAt,
		ExpiresAt:   token.ExpiresAt,
		Nonce:       token.Nonce,
		Fingerprint: token.Fingerprint,
	})
}

// normalizeProjectPath mirrors beadpost/attestation.NormalizeProjectPath.
func normalizeProjectPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("project path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs), nil
}

// normalizeClaims mirrors beadpost/attestation.normalizeClaims so the signed
// payload is byte-identical for the same inputs.
func normalizeClaims(tier string, agentUID int, issuedAt, expiresAt time.Time, nonce string) (string, string, string, string, error) {
	tier = strings.TrimSpace(tier)
	if tier == "" {
		tier = DefaultTier
	}
	if agentUID <= 0 {
		return "", "", "", "", errors.New("agent UID is required")
	}
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}
	if !expiresAt.After(issuedAt) {
		return "", "", "", "", errors.New("attestation expiry must be after issue time")
	}
	nonce = strings.TrimSpace(nonce)
	if nonce == "" {
		var err error
		nonce, err = randomNonce()
		if err != nil {
			return "", "", "", "", err
		}
	}
	return tier, issuedAt.UTC().Format(time.RFC3339Nano), expiresAt.UTC().Format(time.RFC3339Nano), nonce, nil
}

func randomNonce() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
