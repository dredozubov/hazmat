// Package attestationkey is a reusable, dr-owned host-authority key-custody
// library for the Beadpost containment attestation HMAC key.
//
// It is library-first: callers perform explicit generate/load/rotate/remove/
// status operations. It never touches `hazmat init`/rollback, never lazily
// creates a key on first use, and never exposes key material or the key path to
// a contained agent. The key never leaves this package except as an HMAC
// signature; raw bytes are not exported, logged, or printed.
//
// On-disk format is a whitespace-free hex token. Both this package's Load and
// Beadpost's attestation.LoadKey derive HMAC key material the same way — read
// the file, trim surrounding whitespace, and use the remaining bytes as the key
// — so a token minted by the Hazmat host broker verifies byte-for-byte under
// Beadpost. The host broker (bead .4) is responsible for choosing a path
// outside project/agent grant surfaces and registering it as a pathpolicy
// host-authority deny.
package attestationkey

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MinKeyBytes is the minimum HMAC key length, matching Beadpost's LoadKey.
const MinKeyBytes = 32

// generatedEntropyBytes is the random entropy for a generated key. It is
// hex-encoded on disk, so the on-disk token is twice this many ASCII bytes —
// whitespace-free and comfortably above MinKeyBytes.
const generatedEntropyBytes = 32

// Key is an opaque HMAC key. Material never leaves the package except as a
// signature.
type Key struct {
	material []byte
}

// KeyInfo is non-secret metadata about a key file, safe for audit/ops output.
type KeyInfo struct {
	Path    string
	Mode    os.FileMode
	Size    int64
	ModTime time.Time
}

// Generate creates a new dr-owned host-authority key at path: parent dirs are
// created 0700, the key file is written 0600 and atomically, from
// cryptographically-random entropy. It returns non-secret KeyInfo and never
// returns or logs the key material.
func Generate(path string) (KeyInfo, error) {
	if err := validateKeyPath(path); err != nil {
		return KeyInfo{}, err
	}
	token, err := newTokenString()
	if err != nil {
		return KeyInfo{}, err
	}
	if err := writeKeyFileAtomic(path, token); err != nil {
		return KeyInfo{}, err
	}
	return Status(path)
}

// Load reads and validates a key file, returning an opaque Key. It mirrors
// Beadpost's attestation.LoadKey (trim surrounding whitespace, require at least
// MinKeyBytes) so both sides derive identical HMAC key material, and it
// additionally fails closed if the key file is group- or world-accessible.
func Load(path string) (Key, error) {
	if err := validateKeyPath(path); err != nil {
		return Key{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Key{}, fmt.Errorf("stat attestation key: %w", err)
	}
	if info.IsDir() {
		return Key{}, fmt.Errorf("attestation key path %q is a directory", path)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return Key{}, fmt.Errorf("attestation key %q has insecure mode %#o; must be 0600 (no group/other access)", path, perm)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Key{}, fmt.Errorf("read attestation key: %w", err)
	}
	material := []byte(strings.TrimSpace(string(raw)))
	if len(material) < MinKeyBytes {
		return Key{}, fmt.Errorf("attestation key must be at least %d bytes", MinKeyBytes)
	}
	return Key{material: material}, nil
}

// Sign returns the HMAC-SHA256 of message under the key.
func (k Key) Sign(message []byte) []byte {
	mac := hmac.New(sha256.New, k.material)
	mac.Write(message)
	return mac.Sum(nil)
}

// Verify reports whether sig is a valid HMAC-SHA256 of message under the key,
// using a constant-time comparison.
func (k Key) Verify(message, sig []byte) bool {
	return hmac.Equal(k.Sign(message), sig)
}

// Valid reports whether the key holds usable material.
func (k Key) Valid() bool {
	return len(k.material) >= MinKeyBytes
}

// String redacts key material so it is never accidentally logged.
func (k Key) String() string {
	return fmt.Sprintf("attestationkey.Key(%d bytes, redacted)", len(k.material))
}

// Rotate replaces the key at path with freshly generated material, atomically.
// Tokens minted under the prior key stop verifying once rotation completes.
func Rotate(path string) (KeyInfo, error) {
	if err := validateKeyPath(path); err != nil {
		return KeyInfo{}, err
	}
	if _, err := os.Stat(path); err != nil {
		return KeyInfo{}, fmt.Errorf("rotate attestation key: %w", err)
	}
	token, err := newTokenString()
	if err != nil {
		return KeyInfo{}, err
	}
	if err := writeKeyFileAtomic(path, token); err != nil {
		return KeyInfo{}, err
	}
	return Status(path)
}

// Remove best-effort overwrites the key file and unlinks it. Parent
// directories are left in place. Overwrite-before-unlink is best effort: on
// CoW/SSD filesystems the original bytes may persist, so treat any prior
// exposure as key compromise regardless.
func Remove(path string) error {
	if err := validateKeyPath(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat attestation key: %w", err)
	}
	if info.Mode().IsRegular() {
		_ = overwriteFile(path, info.Size())
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove attestation key: %w", err)
	}
	return nil
}

// Status returns non-secret metadata about the key file.
func Status(path string) (KeyInfo, error) {
	if err := validateKeyPath(path); err != nil {
		return KeyInfo{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("stat attestation key: %w", err)
	}
	return KeyInfo{
		Path:    path,
		Mode:    info.Mode().Perm(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

func newTokenString() (string, error) {
	entropy := make([]byte, generatedEntropyBytes)
	if _, err := rand.Read(entropy); err != nil {
		return "", fmt.Errorf("generate attestation key entropy: %w", err)
	}
	return hex.EncodeToString(entropy), nil
}

func writeKeyFileAtomic(path, token string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create attestation key dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".attestation-key-*")
	if err != nil {
		return fmt.Errorf("create temp attestation key: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp attestation key: %w", err)
	}
	if _, err := tmp.WriteString(token + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write attestation key: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close attestation key: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install attestation key: %w", err)
	}
	return nil
}

func overwriteFile(path string, size int64) error {
	if size <= 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	_, err = f.Write(buf)
	return err
}

func validateKeyPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("attestation key path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("attestation key path %q must be absolute", path)
	}
	return nil
}
