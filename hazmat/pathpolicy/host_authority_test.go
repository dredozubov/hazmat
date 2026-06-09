package pathpolicy

import (
	"errors"
	"testing"
)

func TestWithHostAuthorityDenyPaths(t *testing.T) {
	keyDir := "/var/lib/hazmat/keys"
	keyFile := "/var/lib/hazmat/keys/attestation.key"
	policy := DefaultDenyPolicy("/Users/agent", "/Users/dr").WithHostAuthorityDenyPaths(keyDir)

	if !policy.HostAuthorityDenyPath(keyDir) {
		t.Fatal("key dir should be host-authority denied")
	}
	if !policy.HostAuthorityDenyPath(keyFile) {
		t.Fatal("file under the denied key dir should overlap and be denied")
	}
	if policy.HostAuthorityDenyPath("/workspace/project") {
		t.Fatal("unrelated path must not be host-authority denied")
	}

	err := policy.ValidateAllowedPath("read dir", keyDir)
	if err == nil {
		t.Fatal("ValidateAllowedPath must refuse the host-authority key dir")
	}
	var de DenyZoneError
	if !errors.As(err, &de) || de.Zone != "host-authority" {
		t.Fatalf("expected host-authority DenyZoneError, got %v", err)
	}
	if err := policy.ValidateAllowedPath("read dir", keyFile); err == nil {
		t.Fatal("ValidateAllowedPath must refuse a path under the host-authority key dir")
	}
	if err := policy.ValidateAllowedPath("project dir", "/workspace/project"); err != nil {
		t.Fatalf("unrelated path must be allowed: %v", err)
	}
}

func TestWithHostAuthorityDenyPathsIgnoresInvalid(t *testing.T) {
	policy := DefaultDenyPolicy("/Users/agent", "/Users/dr").
		WithHostAuthorityDenyPaths("", "relative/key")
	if policy.HostAuthorityDenyPath("/Users/agent/relative/key") {
		t.Fatal("empty and relative host-authority entries must be ignored")
	}
	if !policy.CredentialDenyPath("/Users/agent/.ssh") {
		t.Fatal("credential deny floor must remain intact")
	}
}

func TestWithHostAuthorityDenyPathsDoesNotMutateReceiver(t *testing.T) {
	base := DefaultDenyPolicy("/Users/agent", "/Users/dr")
	withKey := base.WithHostAuthorityDenyPaths("/var/lib/hazmat/keys")
	if base.HostAuthorityDenyPath("/var/lib/hazmat/keys") {
		t.Fatal("WithHostAuthorityDenyPaths must return a copy, not mutate the receiver")
	}
	if !withKey.HostAuthorityDenyPath("/var/lib/hazmat/keys") {
		t.Fatal("returned policy must carry the host-authority deny")
	}
}
