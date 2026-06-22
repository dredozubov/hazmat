package runtimecapability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hazmat/attestationkey"
)

func TestCapabilityFingerprintMatchesPlanescapeFixture(t *testing.T) {
	capability := mustCapability(t)
	if got, want := capability.AuthorityFingerprint(), "sha256:0965ea31c7ddfbfa5c89009a027329eb5abd6008beabc210402c6806c146b8ff"; got != want {
		t.Fatalf("AuthorityFingerprint = %q, want %q", got, want)
	}
}

func TestSignAndVerifyDeclaration(t *testing.T) {
	key := testKey(t, "primary")
	capability := mustCapability(t)
	declaration, err := SignDeclaration(DeclarationInput{
		Capability:          capability,
		BackendCodeRevision: "git:abc123",
		AttestationTier:     "native-uncontained",
		ReattestAfter:       parseTestTime(t, "2026-06-23T00:00:00Z"),
		RevocationFeedRef:   "revocations/hazmat-local.json",
		Signer:              key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if declaration.BackendVersion != "hazmat-dev-2026-06-22" ||
		declaration.BackendCodeRevision != "git:abc123" ||
		declaration.IsolationTier != "same_uid_process" ||
		declaration.AttestationTier != "native-uncontained" ||
		declaration.ValidFrom != "2026-06-22T00:00:00Z" ||
		declaration.ValidUntil != "2026-06-29T00:00:00Z" ||
		declaration.ReattestAfter != "2026-06-23T00:00:00Z" ||
		declaration.CapabilitySetFingerprint != capability.AuthorityFingerprint() ||
		declaration.RevocationFeedRef != "revocations/hazmat-local.json" {
		t.Fatalf("declaration missing audit fields: %+v", declaration)
	}
	if err := VerifyDeclaration(declaration, VerifyInput{
		Signer:                  key,
		ExpectedSignerTrustRoot: "planescape-runtime-root-demo",
		ExpectedBackendVersion:  "hazmat-dev-2026-06-22",
		Now:                     parseTestTime(t, "2026-06-22T12:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsUnsignedExpiredFutureWrongSignerAndWrongVersion(t *testing.T) {
	key := testKey(t, "primary")
	other := testKey(t, "other")
	declaration := mustDeclaration(t, key)

	unsigned := declaration
	unsigned.Signature = ""
	if err := VerifyDeclaration(unsigned, VerifyInput{Signer: key, Now: parseTestTime(t, "2026-06-22T12:00:00Z")}); err == nil || !strings.Contains(err.Error(), "unsigned") {
		t.Fatalf("unsigned err = %v", err)
	}
	if err := VerifyDeclaration(declaration, VerifyInput{Signer: key, Now: parseTestTime(t, "2026-06-21T23:00:00Z")}); err == nil || !strings.Contains(err.Error(), "not valid yet") {
		t.Fatalf("future err = %v", err)
	}
	if err := VerifyDeclaration(declaration, VerifyInput{Signer: key, Now: parseTestTime(t, "2026-06-29T00:00:00Z")}); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired err = %v", err)
	}
	if err := VerifyDeclaration(declaration, VerifyInput{Signer: other, Now: parseTestTime(t, "2026-06-22T12:00:00Z")}); err == nil || !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("wrong signer err = %v", err)
	}
	if err := VerifyDeclaration(declaration, VerifyInput{Signer: key, ExpectedBackendVersion: "other-version", Now: parseTestTime(t, "2026-06-22T12:00:00Z")}); err == nil || !strings.Contains(err.Error(), "expected version") {
		t.Fatalf("wrong version err = %v", err)
	}
}

func TestVerifyRejectsTamperedDeclarationAndPayload(t *testing.T) {
	key := testKey(t, "primary")
	declaration := mustDeclaration(t, key)

	tampered := declaration
	tampered.BackendCodeRevision = "git:tampered"
	if err := VerifyDeclaration(tampered, VerifyInput{Signer: key, Now: parseTestTime(t, "2026-06-22T12:00:00Z")}); err == nil || !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("tampered declaration err = %v", err)
	}

	tampered = declaration
	tampered.Capability.BackendVersion = "tampered-version"
	if err := VerifyDeclaration(tampered, VerifyInput{Signer: key, Now: parseTestTime(t, "2026-06-22T12:00:00Z")}); err == nil || !strings.Contains(err.Error(), "capability_set_fingerprint mismatch") {
		t.Fatalf("tampered payload err = %v", err)
	}
}

func TestParseCapabilityRejectsUnknownFieldsAndBooleanFlags(t *testing.T) {
	jsonText := strings.Replace(planescapeCapabilityFixture,
		`"valid_until": "2026-06-29T00:00:00Z"`,
		`"can_scope_credentials": true, "valid_until": "2026-06-29T00:00:00Z"`,
		1,
	)
	_, err := ParseCapabilityJSON([]byte(jsonText))
	if err == nil || !strings.Contains(err.Error(), `unknown field "can_scope_credentials"`) {
		t.Fatalf("err = %v, want unknown boolean flag rejection", err)
	}
}

func TestDeclarationRoundTripJSON(t *testing.T) {
	key := testKey(t, "primary")
	declaration := mustDeclaration(t, key)
	data, err := json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseDeclarationJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyDeclaration(parsed, VerifyInput{Signer: key, Now: parseTestTime(t, "2026-06-22T12:00:00Z")}); err != nil {
		t.Fatal(err)
	}
}

func mustCapability(t *testing.T) Capability {
	t.Helper()
	capability, err := ParseCapabilityJSON([]byte(planescapeCapabilityFixture))
	if err != nil {
		t.Fatal(err)
	}
	return capability
}

func mustDeclaration(t *testing.T, key attestationkey.Key) Declaration {
	t.Helper()
	declaration, err := SignDeclaration(DeclarationInput{
		Capability:          mustCapability(t),
		BackendCodeRevision: "git:abc123",
		AttestationTier:     "native-uncontained",
		ReattestAfter:       parseTestTime(t, "2026-06-23T00:00:00Z"),
		RevocationFeedRef:   "revocations/hazmat-local.json",
		Signer:              key,
	})
	if err != nil {
		t.Fatal(err)
	}
	return declaration
}

func testKey(t *testing.T, name string) attestationkey.Key {
	t.Helper()
	path := filepath.Join(t.TempDir(), name, "key")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := attestationkey.Generate(path); err != nil {
		t.Fatal(err)
	}
	key, err := attestationkey.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func parseTestTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.UTC()
}

const planescapeCapabilityFixture = `{
  "schema": "runtime.capability.v1",
  "capability_set_id": "hazmat-local-readonly-demo",
  "backend_id": "hazmat-macos-local",
  "backend_kind": "macos_local",
  "backend_version": "hazmat-dev-2026-06-22",
  "isolation_tier": "same_uid_process",
  "workspace_grant_patterns": [
    "read:repo:*",
    "read:state:*"
  ],
  "network_grant_patterns": [
    "allow:tailnet",
    "deny:public-internet"
  ],
  "credential_modes": [
    "none"
  ],
  "service_grant_patterns": [
    "beads:read",
    "git:status"
  ],
  "conformance_result_fingerprint": "sha256:4444444444444444444444444444444444444444444444444444444444444444",
  "coverage_catalog_fingerprint": "sha256:5555555555555555555555555555555555555555555555555555555555555555",
  "revocation_feed_fingerprint": "sha256:6666666666666666666666666666666666666666666666666666666666666666",
  "signer_trust_root": "planescape-runtime-root-demo",
  "trust_root_epoch": 7,
  "declaration_nonce": "runtime-capability-demo-1",
  "valid_after": "2026-06-22T00:00:00Z",
  "valid_until": "2026-06-29T00:00:00Z"
}`
