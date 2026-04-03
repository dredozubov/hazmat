package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// generateRandomPassword returns a base64-encoded password from n random
// bytes. Uses crypto/rand (OS CSPRNG). The result has 8*n bits of entropy.
//
// For machine-consumed passwords (agent user, tokens) where human readability
// is irrelevant.
func generateRandomPassword(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// generateToken returns a 128-bit random token as a 26-char base32 string.
// Uses crypto/rand.Text() (Go 1.24+). Suitable for API tokens, session IDs,
// and machine-only secrets.
func generateToken() string {
	return rand.Text()
}
