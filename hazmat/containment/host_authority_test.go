package containment

import (
	"reflect"
	"strings"
	"testing"

	"hazmat/sessionmeta"
)

func TestCredentialFloorWithHostAuthorityDenies(t *testing.T) {
	floor, err := NewCredentialFloor("/home/agent", []string{"/.ssh"})
	if err != nil {
		t.Fatal(err)
	}
	keyPath := "/var/lib/hazmat/keys/attestation.key"
	floor, err = floor.WithHostAuthorityDenies(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := NewContract(ContractInput{
		Project:   PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		AgentHome: AgentHomePolicy{Path: "/home/agent"},
		Temp:      TempPolicy{Path: "/tmp/hazmat"},
		Network:   NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:   ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/home/agent/.ssh", keyPath}
	if got := contract.CredentialDenyPaths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("CredentialDenyPaths = %v, want %v", got, want)
	}
}

func TestWithHostAuthorityDeniesRejectsOverlappingGrant(t *testing.T) {
	floor, err := NewCredentialFloor("/home/agent", []string{"/.ssh"})
	if err != nil {
		t.Fatal(err)
	}
	floor, err = floor.WithHostAuthorityDenies("/var/lib/hazmat/keys")
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewContract(ContractInput{
		Project:      PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		ReadOnlyDirs: PathGrants([]string{"/var/lib/hazmat/keys/attestation.key"}, PathReadOnly),
		AgentHome:    AgentHomePolicy{Path: "/home/agent"},
		Temp:         TempPolicy{Path: "/tmp/hazmat"},
		Network:      NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:      ProcessPolicy{AllowFork: true},
	}, floor)
	if err == nil {
		t.Fatal("a grant overlapping a host-authority deny must be rejected")
	}
	if !strings.Contains(err.Error(), "overlaps credential deny path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWithHostAuthorityDeniesValidation(t *testing.T) {
	if _, err := (CredentialFloor{}).WithHostAuthorityDenies("/var/lib/hazmat/keys"); err == nil {
		t.Fatal("an empty floor must reject host-authority denies")
	}
	floor, err := NewCredentialFloor("/home/agent", []string{"/.ssh"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := floor.WithHostAuthorityDenies("relative/key"); err == nil {
		t.Fatal("a relative host-authority path must be rejected")
	}
	if _, err := floor.WithHostAuthorityDenies(""); err == nil {
		t.Fatal("an empty host-authority path must be rejected")
	}
}
