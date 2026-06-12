package darwin

import (
	"errors"
	"os"
	"strings"
	"testing"

	"hazmat/containment"
	"hazmat/sessionmeta"
)

func TestPreparePolicyArtifactCompilesWritesAndCleansPolicy(t *testing.T) {
	artifact, err := PreparePolicyArtifact(PolicyArtifactRequest{
		Contract:                 runtimeTestContract(t),
		MacOSSecurityFramework:   true,
		MacOSAgentKeychainAccess: true,
		RuntimeTempDirs:          []string{"/private/tmp/claude-777"},
		PID:                      os.Getpid(),
	})
	if err != nil {
		t.Fatalf("PreparePolicyArtifact: %v", err)
	}
	t.Cleanup(artifact.Cleanup)

	if artifact.Path == "" {
		t.Fatal("artifact path is empty")
	}
	info, err := os.Stat(artifact.Path)
	if err != nil {
		t.Fatalf("stat policy artifact: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o644); got != want {
		t.Fatalf("policy artifact mode = %v, want %v", got, want)
	}
	data, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatalf("read policy artifact: %v", err)
	}
	policy := string(data)
	for _, want := range []string{
		`(version 1)`,
		`(allow file-read* (subpath "/workspace/reference"))`,
		`(allow file-read* file-write* (subpath "/workspace/cache"))`,
		`(allow mach-lookup (global-name "com.apple.SecurityServer"))`,
		`(allow file-read* file-write* (subpath "/private/tmp/claude-777"))`,
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("policy artifact missing %q\n%s", want, policy)
		}
	}

	artifact.Cleanup()
	if _, err := os.Stat(artifact.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup left policy artifact behind: %v", err)
	}
}

func TestPreparePolicyArtifactRequiresPositivePID(t *testing.T) {
	_, err := PreparePolicyArtifact(PolicyArtifactRequest{
		Contract: runtimeTestContract(t),
	})
	if err == nil || !strings.Contains(err.Error(), "pid must be positive") {
		t.Fatalf("PreparePolicyArtifact error = %v, want pid error", err)
	}
}

func TestPreparePolicyArtifactRejectsInvalidContract(t *testing.T) {
	_, err := PreparePolicyArtifact(PolicyArtifactRequest{
		PID: os.Getpid(),
	})
	if err == nil || !strings.Contains(err.Error(), "compile seatbelt policy") {
		t.Fatalf("PreparePolicyArtifact error = %v, want compile error", err)
	}
}

func runtimeTestContract(t *testing.T) containment.Contract {
	t.Helper()
	floor, err := containment.CredentialFloorFromDenies([]containment.CredentialDeny{
		{Path: "/home/agent/.ssh"},
		{Path: "/home/agent/.aws"},
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project: containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		ReadOnlyDirs: containment.PathGrants([]string{
			"/workspace/reference",
		}, containment.PathReadOnly),
		ReadWriteDirs: containment.PathGrants([]string{
			"/workspace/cache",
		}, containment.PathReadWrite),
		AgentHome: containment.AgentHomePolicy{Path: "/home/agent"},
		Temp:      containment.TempPolicy{Path: "/tmp/hazmat-session"},
		Network:   containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:   containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}
