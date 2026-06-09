package attestationkey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateLoadSignVerify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys", "attestation.key")

	info, err := Generate(path)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if info.Mode.Perm() != 0o600 {
		t.Fatalf("key mode = %#o, want 0600", info.Mode.Perm())
	}
	dinfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dinfo.Mode().Perm() != 0o700 {
		t.Fatalf("key dir mode = %#o, want 0700", dinfo.Mode().Perm())
	}

	key, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !key.Valid() {
		t.Fatal("loaded key should be valid")
	}
	msg := []byte("beadpost.containment.attestation.v1 payload")
	sig := key.Sign(msg)
	if !key.Verify(msg, sig) {
		t.Fatal("Verify should accept a valid signature")
	}
	if key.Verify([]byte("tampered"), sig) {
		t.Fatal("Verify must reject a signature over different content")
	}
	if strings.Contains(key.String(), string(key.material)) {
		t.Fatal("String() must not leak key material")
	}
	if !strings.Contains(key.String(), "redacted") {
		t.Fatalf("String() should be redacted, got %q", key.String())
	}
}

func TestLoadRejectsInsecureMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "attestation.key")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 64)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load must reject a group/world-accessible key file")
	}
}

// Load must derive key material the same way Beadpost's attestation.LoadKey
// does (trim surrounding whitespace), so a token minted by the Hazmat broker
// verifies byte-for-byte under Beadpost.
func TestLoadTrimsWhitespaceLikeBeadpost(t *testing.T) {
	dir := t.TempDir()
	token := strings.Repeat("ab", 32) // 64-char whitespace-free token
	clean := filepath.Join(dir, "clean.key")
	padded := filepath.Join(dir, "padded.key")
	if err := os.WriteFile(clean, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(padded, []byte("  \n"+token+"\n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	k1, err := Load(clean)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := Load(padded)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("payload")
	if string(k1.Sign(msg)) != string(k2.Sign(msg)) {
		t.Fatal("surrounding whitespace must not change the derived key (Beadpost LoadKey parity)")
	}
}

func TestRotateInvalidatesPriorKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "attestation.key")
	if _, err := Generate(path); err != nil {
		t.Fatal(err)
	}
	old, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("payload")
	oldSig := old.Sign(msg)

	if _, err := Rotate(path); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	rotated, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Verify(msg, oldSig) {
		t.Fatal("a token minted under the prior key must not verify after rotation")
	}
}

func TestRemoveDeletesKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "attestation.key")
	if _, err := Generate(path); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("key should be gone, stat err = %v", err)
	}
	if err := Remove(path); err != nil {
		t.Fatalf("Remove of a missing key should be a no-op, got %v", err)
	}
}

func TestPathValidation(t *testing.T) {
	if _, err := Generate("relative/key"); err == nil {
		t.Fatal("Generate must reject a relative path")
	}
	if _, err := Load("relative/key"); err == nil {
		t.Fatal("Load must reject a relative path")
	}
}
