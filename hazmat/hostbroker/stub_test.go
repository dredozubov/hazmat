//go:build !beadpost_hostbroker

package hostbroker

import (
	"context"
	"errors"
	"os"
	"testing"

	"hazmat/attestationkey"
)

// These tests run in the default/public Hazmat build. They prove the host-broker
// support is compiled out and fails closed without importing
// local/beadpost-contracts (the absence of that import — and of beadpost/dolt —
// is enforced for every package by TestImportBoundaries in default mode).

func TestStubMintVerifyFailClosed(t *testing.T) {
	if _, err := Mint(MintInput{}, attestationkey.Key{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("stub Mint = %v, want ErrDisabled", err)
	}
	if err := Verify(Token{}, attestationkey.Key{}, VerifyInput{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("stub Verify = %v, want ErrDisabled", err)
	}
}

func TestStubOpenCreatesNoSocket(t *testing.T) {
	runtimeDir := t.TempDir()
	s, err := Open(SessionConfig{
		Facts: LaunchFacts{
			OriginProject: "api", ProjectPath: "/p", AgentUID: 1, Tier: "contained",
			RegistryPath: "/r", LedgerPath: "/l", SandboxConfirmed: true,
		},
		RuntimeDir: runtimeDir,
	})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("stub Open err = %v, want ErrDisabled", err)
	}
	if s != nil {
		t.Fatal("stub Open must not return a session")
	}
	entries, readErr := os.ReadDir(runtimeDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("stub Open must not create a socket; runtime dir has %d entries", len(entries))
	}
}

func TestStubClientNeverDials(t *testing.T) {
	// A path that would error on dial if the stub tried to connect; it must
	// instead return ErrDisabled without touching the network.
	c := NewClient("/nonexistent/definitely-not-a-socket.sock")
	for name, call := range map[string]func() error{
		"deliver": func() error { _, err := c.Deliver(context.Background(), Submission{}); return err },
		"review":  func() error { _, err := c.Review(context.Background(), Submission{}); return err },
		"decide":  func() error { _, err := c.Decide(context.Background(), Submission{}); return err },
	} {
		if err := call(); !errors.Is(err, ErrDisabled) {
			t.Fatalf("stub Client.%s = %v, want ErrDisabled (must not dial)", name, err)
		}
	}
}
