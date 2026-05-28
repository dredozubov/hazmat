package containment

import (
	"reflect"
	"testing"

	"hazmat/sessionmeta"
)

func TestContractEffectivePathSets(t *testing.T) {
	contract := Contract{
		Project:       PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		ReadOnlyDirs:  PathGrants([]string{"/workspace", "/opt/toolchain", "/opt/toolchain/go", "/var/cache/build"}, PathReadOnly),
		ReadWriteDirs: PathGrants([]string{"/workspace/project/tmp", "/var/cache", "/var/cache/sub"}, PathReadWrite),
		AgentHome:     AgentHomePolicy{Path: "/home/agent"},
		Temp:          TempPolicy{Path: "/tmp/hazmat"},
		Network:       NetworkPolicy{Mode: sessionmeta.NetworkDefault},
		Process:       ProcessPolicy{AllowFork: true},
	}

	if got, want := contract.EffectiveReadOnlyDirs(), []string{"/workspace", "/opt/toolchain"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveReadOnlyDirs = %v, want %v", got, want)
	}
	if got, want := contract.EffectiveWritableDirs(), []string{"/var/cache"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveWritableDirs = %v, want %v", got, want)
	}
}

func TestContractCopiesPathLists(t *testing.T) {
	readDirs := []string{"/opt/sdk"}
	grants := PathGrants(readDirs, PathReadOnly)
	readDirs[0] = "/mutated"

	contract := Contract{
		Project:          PathGrant{Path: "/workspace/project", Access: PathReadWrite},
		ReadOnlyDirs:     grants,
		ReadWriteDirs:    PathGrants([]string{"/tmp/cache"}, PathReadWrite),
		CredentialDenies: []CredentialDeny{{Path: "/home/agent/.ssh"}},
	}

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
