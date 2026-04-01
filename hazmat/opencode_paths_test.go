package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestOpenCodeBinaryCandidatesPreferCurrentPath(t *testing.T) {
	want := []string{
		agentHome + openCodeCurrentBinRel,
		agentHome + openCodeLegacyBinRel,
	}
	if got := openCodeBinaryCandidates(); !reflect.DeepEqual(got, want) {
		t.Fatalf("openCodeBinaryCandidates() = %v, want %v", got, want)
	}
}

func TestFindInstalledOpenCodeBinaryWithPrefersCurrentPath(t *testing.T) {
	got, ok := findInstalledOpenCodeBinaryWith(func(args ...string) (string, error) {
		if args[len(args)-1] == agentHome+openCodeCurrentBinRel {
			return "", nil
		}
		if args[len(args)-1] == agentHome+openCodeLegacyBinRel {
			return "", nil
		}
		return "", errors.New("unexpected path")
	})
	if !ok {
		t.Fatal("expected an installed OpenCode binary")
	}
	if got != agentHome+openCodeCurrentBinRel {
		t.Fatalf("findInstalledOpenCodeBinaryWith() = %q, want %q", got, agentHome+openCodeCurrentBinRel)
	}
}

func TestFindInstalledOpenCodeBinaryWithFallsBackToLegacyPath(t *testing.T) {
	got, ok := findInstalledOpenCodeBinaryWith(func(args ...string) (string, error) {
		if args[len(args)-1] == agentHome+openCodeLegacyBinRel {
			return "", nil
		}
		return "", errors.New("missing")
	})
	if !ok {
		t.Fatal("expected legacy OpenCode binary to be detected")
	}
	if got != agentHome+openCodeLegacyBinRel {
		t.Fatalf("findInstalledOpenCodeBinaryWith() = %q, want %q", got, agentHome+openCodeLegacyBinRel)
	}
}

func TestOpenCodeLaunchScriptChecksBothLocations(t *testing.T) {
	script := openCodeLaunchScript()
	for _, want := range []string{
		`"$HOME/.opencode/bin/opencode"`,
		`"$HOME/.local/bin/opencode"`,
		openCodeMissingHelp,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("openCodeLaunchScript() missing %q in %q", want, script)
		}
	}
}
