package containment

import (
	"reflect"
	"strings"
	"testing"

	"hazmat/sessionmeta"
)

func TestContractEffectivePathSets(t *testing.T) {
	contract := mustContract(t, ContractInput{
		Project:       PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		ReadOnlyDirs:  PathGrants([]string{"/workspace", "/opt/toolchain", "/opt/toolchain/go", "/var/cache/build"}, PathReadOnly),
		ReadWriteDirs: PathGrants([]string{"/workspace/project/tmp", "/var/cache", "/var/cache/sub"}, PathReadWrite),
		AgentHome:     AgentHomePolicy{Path: "/home/agent"},
		Temp:          TempPolicy{Path: "/tmp/hazmat"},
		Network:       NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:       ProcessPolicy{AllowFork: true},
	})

	if got, want := contract.EffectiveReadOnlyDirs(), []string{"/workspace", "/opt/toolchain"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveReadOnlyDirs = %v, want %v", got, want)
	}
	if got, want := contract.EffectiveWritableDirs(), []string{"/var/cache"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveWritableDirs = %v, want %v", got, want)
	}
}

func TestContractAncestorMetadataDirsAreSorted(t *testing.T) {
	contract := mustContract(t, ContractInput{
		Project:       PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		ReadOnlyDirs:  PathGrants([]string{"/var/cache/build", "/opt/toolchain/go"}, PathReadOnly),
		ReadWriteDirs: PathGrants([]string{"/workspace/project/tmp"}, PathReadWrite),
		AgentHome:     AgentHomePolicy{Path: "/home/agent"},
		Temp:          TempPolicy{Path: "/tmp/hazmat"},
		Network:       NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:       ProcessPolicy{AllowFork: true},
	})

	want := []string{"/opt", "/opt/toolchain", "/var", "/var/cache", "/workspace", "/workspace/project"}
	if got := contract.AncestorMetadataDirs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AncestorMetadataDirs = %v, want %v", got, want)
	}
}

func TestContractCopiesPathLists(t *testing.T) {
	readDirs := []string{"/opt/sdk"}
	grants := PathGrants(readDirs, PathReadOnly)
	readDirs[0] = "/mutated"
	floorInput := []CredentialDeny{{Path: "/home/agent/.ssh"}}
	floor, err := CredentialFloorFromDenies(floorInput)
	if err != nil {
		t.Fatal(err)
	}

	contract, err := NewContract(ContractInput{
		Project:       PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		ReadOnlyDirs:  grants,
		ReadWriteDirs: PathGrants([]string{"/tmp/cache"}, PathReadWrite),
		AgentHome:     AgentHomePolicy{Path: "/home/agent"},
		Temp:          TempPolicy{Path: "/tmp/hazmat"},
		Network:       NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:       ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	grants[0].Path = "/changed"
	floorInput[0].Path = "/changed"

	if got := contract.ReadOnlyPaths(); !reflect.DeepEqual(got, []string{"/opt/sdk"}) {
		t.Fatalf("ReadOnlyPaths = %v", got)
	}
	readPaths := contract.ReadOnlyPaths()
	readPaths[0] = "/changed"
	if got := contract.ReadOnlyPaths(); !reflect.DeepEqual(got, []string{"/opt/sdk"}) {
		t.Fatalf("ReadOnlyPaths after caller mutation = %v", got)
	}
	if got := contract.CredentialDenyPaths(); !reflect.DeepEqual(got, []string{"/home/agent/.ssh"}) {
		t.Fatalf("CredentialDenyPaths = %v", got)
	}
}

func TestNewContractDerivesCredentialFloor(t *testing.T) {
	floor, err := NewCredentialFloor("/home/agent", []string{"/.ssh", "/.aws"})
	if err != nil {
		t.Fatal(err)
	}
	contract := mustContractWithFloor(t, ContractInput{
		Project:   PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		AgentHome: AgentHomePolicy{Path: "/home/agent"},
		Temp:      TempPolicy{Path: "/tmp/hazmat"},
		Network:   NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:   ProcessPolicy{AllowFork: true},
	}, floor)

	if got, want := contract.CredentialDenyPaths(), []string{"/home/agent/.ssh", "/home/agent/.aws"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CredentialDenyPaths = %v, want %v", got, want)
	}
}

func TestContractValidateRejectsUnconstructedCredentialFloor(t *testing.T) {
	contract := Contract{
		Project:          PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		AgentHome:        AgentHomePolicy{Path: "/home/agent"},
		Temp:             TempPolicy{Path: "/tmp/hazmat"},
		CredentialDenies: []CredentialDeny{{Path: "/home/agent/.ssh"}},
		Network:          NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:          ProcessPolicy{AllowFork: true},
	}

	err := contract.Validate()
	if err == nil {
		t.Fatal("expected unconstructed contract to fail closed")
	}
	if !strings.Contains(err.Error(), "credential deny floor is required") {
		t.Fatalf("error = %v, want credential floor requirement", err)
	}
}

func TestContractValidateRejectsMutatedCredentialFloor(t *testing.T) {
	contract := mustContract(t, ContractInput{
		Project:   PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		AgentHome: AgentHomePolicy{Path: "/home/agent"},
		Temp:      TempPolicy{Path: "/tmp/hazmat"},
		Network:   NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:   ProcessPolicy{AllowFork: true},
	})
	contract.CredentialDenies = []CredentialDeny{{Path: "/tmp/not-a-credential-floor"}}

	err := contract.Validate()
	if err == nil {
		t.Fatal("expected mutated credential floor to fail closed")
	}
	if !strings.Contains(err.Error(), "credential deny floor must match structural floor") {
		t.Fatalf("error = %v, want structural floor mismatch", err)
	}
	if got, want := contract.CredentialDenyPaths(), []string{"/home/agent/.ssh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CredentialDenyPaths = %v, want structural floor %v", got, want)
	}
}

func TestNewContractRejectsCredentialDenyOverlap(t *testing.T) {
	floor, err := CredentialFloorFromDenies([]CredentialDeny{{Path: "/home/agent/.ssh"}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewContract(ContractInput{
		Project:      PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		ReadOnlyDirs: PathGrants([]string{"/home/agent"}, PathReadOnly),
		AgentHome:    AgentHomePolicy{Path: "/home/agent"},
		Temp:         TempPolicy{Path: "/tmp/hazmat"},
		Network:      NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:      ProcessPolicy{AllowFork: true},
	}, floor)
	if err == nil {
		t.Fatal("expected credential deny overlap to be rejected")
	}
	if !strings.Contains(err.Error(), `"/home/agent" overlaps credential deny path "/home/agent/.ssh"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewContractRejectsMismatchedPathGrantAccess(t *testing.T) {
	floor, err := CredentialFloorFromDenies([]CredentialDeny{{Path: "/home/agent/.ssh"}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewContract(ContractInput{
		Project:      PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		ReadOnlyDirs: []PathGrant{{Path: "/opt/sdk", Access: PathReadWrite}},
		AgentHome:    AgentHomePolicy{Path: "/home/agent"},
		Temp:         TempPolicy{Path: "/tmp/hazmat"},
		Network:      NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:      ProcessPolicy{AllowFork: true},
	}, floor)
	if err == nil {
		t.Fatal("expected mismatched read-only grant to be rejected")
	}
	if !strings.Contains(err.Error(), `read-only path grant "/opt/sdk" must be read-only`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsWithinDir(t *testing.T) {
	for _, tc := range []struct {
		base   string
		target string
		want   bool
	}{
		{"/workspace/project", "/workspace/project", true},
		{"/workspace/project", "/workspace/project/src", true},
		{"/workspace/project", "/workspace/project-other", false},
		{"/workspace/project", "/workspace", false},
	} {
		if got := IsWithinDir(tc.base, tc.target); got != tc.want {
			t.Fatalf("IsWithinDir(%q, %q) = %v, want %v", tc.base, tc.target, got, tc.want)
		}
	}
}

func mustContract(t *testing.T, input ContractInput) Contract {
	t.Helper()
	floor, err := CredentialFloorFromDenies([]CredentialDeny{{Path: "/home/agent/.ssh"}})
	if err != nil {
		t.Fatal(err)
	}
	return mustContractWithFloor(t, input, floor)
}

func mustContractWithFloor(t *testing.T, input ContractInput, floor CredentialFloor) Contract {
	t.Helper()
	contract, err := NewContract(input, floor)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}
