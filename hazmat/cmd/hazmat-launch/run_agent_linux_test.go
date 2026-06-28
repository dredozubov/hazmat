//go:build linux && (amd64 || arm64)

package main

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestParseRunAgentArgsValidatesCommandShape(t *testing.T) {
	digest := strings.Repeat("a", sha256.Size*2)
	nonce := strings.Repeat("b", 32)
	request, err := parseRunAgentArgs([]string{
		"--spec", "/tmp/hazmat/launch-spec.json",
		"--spec-sha256", digest,
		"--nonce", nonce,
		"--metadata", "/tmp/hazmat/metadata.json",
	})
	if err != nil {
		t.Fatalf("parseRunAgentArgs: %v", err)
	}
	if request.SpecSHA256 != digest || request.SpecNonce != nonce {
		t.Fatalf("request = %+v", request)
	}

	for _, args := range [][]string{
		{"--spec", "relative.json", "--spec-sha256", digest, "--nonce", nonce, "--metadata", "/tmp/hazmat/metadata.json"},
		{"--spec", "/tmp/hazmat/launch-spec.json", "--spec-sha256", "bad", "--nonce", nonce, "--metadata", "/tmp/hazmat/metadata.json"},
		{"--spec", "/tmp/hazmat/launch-spec.json", "--spec-sha256", digest, "--nonce", nonce, "--metadata", "/tmp/hazmat/other.json"},
	} {
		if _, err := parseRunAgentArgs(args); err == nil {
			t.Fatalf("parseRunAgentArgs(%v) succeeded, want error", args)
		}
	}
}

func TestValidateRunAgentPathRejectsTraversal(t *testing.T) {
	err := validateRunAgentPath("--spec", "/tmp/hazmat/../launch-spec.json")
	if err == nil || !strings.Contains(fmt.Sprint(err), "absolute and clean") {
		t.Fatalf("err = %v, want clean-path error", err)
	}
}
