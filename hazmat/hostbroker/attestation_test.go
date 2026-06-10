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

	"local/beadpost-contracts/contractfixture"
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

// TestMintMatchesContractFixture pins the Hazmat v2 mint to the single shared
// contractfixture (not a local golden), proving Hazmat and Beadpost agree on the
// canonical v2 token against one anchor. Hazmat consumes the fixture; it does not
// reimplement the canonicalization.
func TestMintMatchesContractFixture(t *testing.T) {
	key := loadFixedKey(t, string(contractfixture.Key))
	in := contractfixture.AttestationV2Input
	tok, err := Mint(MintInput{
		ProjectPath: in.ProjectPath,
		AgentUID:    in.AgentUID,
		Tier:        attestationtier.Tier(in.Tier),
		Fingerprint: in.Fingerprint,
		IssuedAt:    in.IssuedAt,
		ExpiresAt:   in.ExpiresAt,
		Nonce:       in.Nonce,
	}, key)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if tok != contractfixture.AttestationV2Token {
		t.Fatalf("token drift vs fixture:\n got: %+v\nwant: %+v", tok, contractfixture.AttestationV2Token)
	}
	if tok.Signature != contractfixture.AttestationV2Signature {
		t.Fatalf("signature = %q, want fixture %q", tok.Signature, contractfixture.AttestationV2Signature)
	}
	got, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != contractfixture.AttestationV2JSON {
		t.Fatalf("token JSON drift vs fixture:\n got: %s\nwant: %s", got, contractfixture.AttestationV2JSON)
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
