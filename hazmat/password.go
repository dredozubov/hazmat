package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/sethvargo/go-diceware/diceware"
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

// generatePassphrase returns a diceware passphrase of n words joined by
// hyphens. Uses the EFF large wordlist (7776 words, 12.92 bits/word).
//
//	6 words = 77.5 bits  (good for user-facing passwords)
//	7 words = 90.4 bits  (strong)
//	10 words = 129.2 bits (encryption key material)
//
// For passwords that a human might need to write down or type during
// disaster recovery.
func generatePassphrase(nWords int) (string, error) {
	words, err := diceware.Generate(nWords)
	if err != nil {
		return "", fmt.Errorf("generate passphrase: %w", err)
	}
	return strings.Join(words, "-"), nil
}
