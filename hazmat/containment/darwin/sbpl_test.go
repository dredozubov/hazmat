package darwin

import (
	"strings"
	"testing"

	"hazmat/containment"
	"hazmat/sessionmeta"
)

func TestCompileBuildsSeatbeltPolicy(t *testing.T) {
	policy, err := Compile(testContract(t), CompileOptions{MacOSSecurityFramework: true})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, want := range []string{
		`(allow file-read* (subpath "/workspace/reference"))`,
		`(allow file-read* file-write* (subpath "/workspace/cache"))`,
		`(allow file-read* file-write* (subpath "/home/agent/.config"))`,
		`(allow file-read* file-write* (literal "/home/agent/.zshrc"))`,
		`(allow process-exec (subpath "/home/agent/.local/bin"))`,
		`(deny file-read* file-write* (subpath "/home/agent/.ssh"))`,
		`(allow mach-lookup (global-name "com.apple.SecurityServer"))`,
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("policy missing %q\n%s", want, policy)
		}
	}
	for _, forbidden := range []string{
		`(allow file-read* file-write* (subpath "/home/agent"))`,
		`(allow process-exec (subpath "/home/agent"))`,
	} {
		if strings.Contains(policy, forbidden) {
			t.Fatalf("policy should not contain broad agent-home grant %q\n%s", forbidden, policy)
		}
	}
}

func TestCompileRejectsUnconstructedCredentialFloor(t *testing.T) {
	contract := containment.Contract{
		Project:          containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		AgentHome:        containment.AgentHomePolicy{Path: "/home/agent"},
		Temp:             containment.TempPolicy{Path: "/tmp/hazmat-session"},
		CredentialDenies: []containment.CredentialDeny{{Path: "/home/agent/.ssh"}},
		Network:          containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:          containment.ProcessPolicy{AllowFork: true},
	}
	_, err := Compile(contract, CompileOptions{})
	if err == nil {
		t.Fatal("Compile succeeded without structural credential floor")
	}
}

func testContract(t *testing.T) containment.Contract {
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
