package darwin

import (
	"strings"
	"testing"

	"hazmat/containment"
	"hazmat/sessionmeta"
)

// The dr-owned Beadpost broker attestation key must be denied in the generated
// SBPL and never re-allowed, even with the keychain re-allow options enabled.
func TestCompileDeniesHostAuthorityKey(t *testing.T) {
	keyPath := "/var/lib/hazmat/keys/attestation.key"
	floor, err := containment.NewCredentialFloor("/home/agent", []string{"/.ssh"})
	if err != nil {
		t.Fatal(err)
	}
	floor, err = floor.WithHostAuthorityDenies(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := containment.NewContract(containment.ContractInput{
		Project:   containment.PathGrant{Path: "/workspace/project", Access: containment.PathReadWrite},
		AgentHome: containment.AgentHomePolicy{Path: "/home/agent"},
		Temp:      containment.TempPolicy{Path: "/tmp/hazmat-session"},
		Network:   containment.NetworkPolicy{Mode: sessionmeta.NetworkNone},
		Process:   containment.ProcessPolicy{AllowFork: true},
	}, floor)
	if err != nil {
		t.Fatal(err)
	}

	policy, err := Compile(contract, CompileOptions{
		MacOSSecurityFramework:   true,
		MacOSAgentKeychainAccess: true,
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	wantDeny := `(deny file-read* file-write* (subpath "` + keyPath + `"))`
	if !strings.Contains(policy, wantDeny) {
		t.Fatalf("policy missing host-authority deny %q\n%s", wantDeny, policy)
	}
	for _, reAllow := range []string{
		`(allow file-read* (subpath "` + keyPath,
		`(allow file-read* file-write* (subpath "` + keyPath,
		`(allow file-read* (literal "` + keyPath,
		`(allow file-read* file-write* (literal "` + keyPath,
	} {
		if strings.Contains(policy, reAllow) {
			t.Fatalf("host-authority key must never be re-allowed (found %q)\n%s", reAllow, policy)
		}
	}
}
