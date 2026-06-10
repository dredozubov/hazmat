//go:build beadpost_hostbroker

package hostbroker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hazmat/attestationkey"
	"hazmat/attestationtier"
)

// loadFixedKey writes material to a 0600 file and loads it via the .3 key
// custody, yielding a Key with deterministic material for golden vectors.
func loadFixedKey(t *testing.T, material string) attestationkey.Key {
	t.Helper()
	path := filepath.Join(t.TempDir(), "attestation.key")
	if err := os.WriteFile(path, []byte(material+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := attestationkey.Load(path)
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	return key
}

// TestMintByteCompatibleWithBeadpostGolden pins the Hazmat v2 mint to a golden
// token produced by beadpost/attestation.SignV2 with identical fixed inputs. If
// either side's canonical payload or HMAC drifts, this fails — proving the two
// reimplementations stay byte/schema compatible without a shared dependency.
func TestMintByteCompatibleWithBeadpostGolden(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	tok, err := Mint(MintInput{
		ProjectPath: "/hazmat/golden/project",
		AgentUID:    4242,
		Tier:        attestationtier.Contained,
		Fingerprint: "sha256:abc123",
		IssuedAt:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		ExpiresAt:   time.Date(2026, 1, 2, 4, 4, 5, 0, time.UTC),
		Nonce:       "feedfacecafebeeffeedfacecafebeef",
	}, key)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	const goldenSignature = "hmac-sha256:42bb77939f1d80333b2975d1074ce1c55012dc88550a66458faac12fe56a242c"
	if tok.Signature != goldenSignature {
		t.Fatalf("signature = %q, want Beadpost golden %q", tok.Signature, goldenSignature)
	}

	// The full marshaled token must match the Beadpost golden byte-for-byte.
	const goldenJSON = `{
  "schema": "beadpost.containment.attestation.v2",
  "project_path": "/hazmat/golden/project",
  "agent_uid": 4242,
  "tier": "contained",
  "issued_at": "2026-01-02T03:04:05Z",
  "expires_at": "2026-01-02T04:04:05Z",
  "nonce": "feedfacecafebeeffeedfacecafebeef",
  "fingerprint": "sha256:abc123",
  "signature": "hmac-sha256:42bb77939f1d80333b2975d1074ce1c55012dc88550a66458faac12fe56a242c"
}`
	got, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != goldenJSON {
		t.Fatalf("token JSON not byte-compatible with Beadpost golden:\n got: %s\nwant: %s", got, goldenJSON)
	}
}

func TestMintRequiresFingerprintAndKey(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	base := MintInput{ProjectPath: t.TempDir(), AgentUID: 599, Tier: attestationtier.Contained, Fingerprint: "sha256:x", IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}

	noFP := base
	noFP.Fingerprint = ""
	if _, err := Mint(noFP, key); err == nil {
		t.Fatal("mint without a fingerprint must fail (v2 binds the request)")
	}
	if _, err := Mint(base, attestationkey.Key{}); err == nil {
		t.Fatal("mint without a configured key must fail")
	}
}

func TestVerifyAcceptsAndFailsClosed(t *testing.T) {
	key := loadFixedKey(t, "0123456789abcdef0123456789abcdef")
	projectDir := t.TempDir() // real dir → stable path normalization
	const fp = "sha256:route"

	mint := func() Token {
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

	good := VerifyInput{ProjectPath: projectDir, AgentUID: 599, Tier: "contained", Fingerprint: fp, Now: time.Now().UTC()}
	if err := Verify(mint(), key, good); err != nil {
		t.Fatalf("verify good token: %v", err)
	}

	for name, in := range map[string]VerifyInput{
		"wrong project":     {ProjectPath: t.TempDir(), AgentUID: 599, Tier: "contained", Fingerprint: fp},
		"wrong uid":         {ProjectPath: projectDir, AgentUID: 600, Tier: "contained", Fingerprint: fp},
		"wrong tier":        {ProjectPath: projectDir, AgentUID: 599, Tier: "native-uncontained", Fingerprint: fp},
		"wrong fingerprint": {ProjectPath: projectDir, AgentUID: 599, Tier: "contained", Fingerprint: "sha256:other"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := Verify(mint(), key, in); err == nil {
				t.Fatal("expected fail-closed verification")
			}
		})
	}

	t.Run("tampered signed field breaks signature", func(t *testing.T) {
		tok := mint()
		tok.Tier = "native-uncontained" // mutate a signed field without re-signing
		err := Verify(tok, key, VerifyInput{ProjectPath: projectDir, AgentUID: 599, Tier: "native-uncontained", Fingerprint: fp})
		if err == nil {
			t.Fatal("tampering a signed field must invalidate the signature")
		}
	})

	t.Run("v1 schema rejected", func(t *testing.T) {
		tok := mint()
		tok.Schema = "beadpost.containment.attestation.v1"
		if err := Verify(tok, key, good); err == nil {
			t.Fatal("v1 schema must be rejected on the host-broker path")
		}
	})

	t.Run("wrong key rejected", func(t *testing.T) {
		other := loadFixedKey(t, "ffffffffffffffffffffffffffffffff")
		if err := Verify(mint(), other, good); err == nil {
			t.Fatal("a token signed by a different key must be rejected")
		}
	})

	t.Run("expired rejected", func(t *testing.T) {
		tok, err := Mint(MintInput{
			ProjectPath: projectDir, AgentUID: 599, Tier: attestationtier.Contained, Fingerprint: fp,
			IssuedAt: time.Now().UTC().Add(-2 * time.Hour), ExpiresAt: time.Now().UTC().Add(-time.Hour),
		}, key)
		if err != nil {
			t.Fatal(err)
		}
		if err := Verify(tok, key, good); err == nil {
			t.Fatal("an expired token must be rejected")
		}
	})
}
