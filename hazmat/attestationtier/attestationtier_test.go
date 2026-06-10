package attestationtier

import (
	"strings"
	"testing"

	"hazmat/sessionmeta"
)

func nativeNone() sessionmeta.NetworkPolicyMetadata {
	return sessionmeta.BuildNetworkPolicyMetadata(sessionmeta.NetworkNone, sessionmeta.ModeNative)
}

func nativeDefault() sessionmeta.NetworkPolicyMetadata {
	return sessionmeta.BuildNetworkPolicyMetadata(sessionmeta.NetworkDefault, sessionmeta.ModeNative)
}

func dockerNone() sessionmeta.NetworkPolicyMetadata {
	return sessionmeta.BuildNetworkPolicyMetadata(sessionmeta.NetworkNone, sessionmeta.ModeDockerSandbox)
}

func TestDeriveStrongTierOnlyForFullyContainedNative(t *testing.T) {
	tier, err := Derive(Posture{
		Mode:                    sessionmeta.ModeNative,
		Network:                 nativeNone(),
		CredentialFloorEnforced: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tier != Contained || !tier.IsContained() {
		t.Fatalf("tier = %q, want contained", tier)
	}
}

func TestDeriveNeverOverClaims(t *testing.T) {
	cases := []struct {
		name string
		p    Posture
		want Tier
	}{
		{
			name: "native egress allowed",
			p:    Posture{Mode: sessionmeta.ModeNative, Network: nativeDefault(), CredentialFloorEnforced: true},
			want: NativeUncontained,
		},
		{
			name: "native deny-all but no credential floor",
			p:    Posture{Mode: sessionmeta.ModeNative, Network: nativeNone(), CredentialFloorEnforced: false},
			want: NativeUncontained,
		},
		{
			name: "docker-sandbox with network none requested",
			p:    Posture{Mode: sessionmeta.ModeDockerSandbox, Network: dockerNone(), CredentialFloorEnforced: true},
			want: DockerSandbox,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, err := Derive(tc.p)
			if err != nil {
				t.Fatal(err)
			}
			if tier != tc.want {
				t.Fatalf("tier = %q, want %q", tier, tc.want)
			}
			if tier.IsContained() {
				t.Fatalf("posture %q must not yield the strong contained tier", tc.name)
			}
		})
	}
}

func TestDeriveDockerNeverContained(t *testing.T) {
	// Even if a docker posture somehow reported deny-all egress, docker maps to
	// its own weaker tier (parity is unproven).
	net := dockerNone()
	net.DenyAllEgress = true
	tier, err := Derive(Posture{Mode: sessionmeta.ModeDockerSandbox, Network: net, CredentialFloorEnforced: true})
	if err != nil {
		t.Fatal(err)
	}
	if tier != DockerSandbox {
		t.Fatalf("tier = %q, want docker-sandbox", tier)
	}
}

func TestDeriveUnknownModeFailsClosed(t *testing.T) {
	if _, err := Derive(Posture{Mode: sessionmeta.Mode("firecracker"), CredentialFloorEnforced: true}); err == nil {
		t.Fatal("unknown mode must fail closed")
	}
	if _, err := Derive(Posture{Mode: sessionmeta.Mode(""), CredentialFloorEnforced: true}); err == nil {
		t.Fatal("empty mode must fail closed")
	}
}

func TestDeriveAppleContainerFailsClosed(t *testing.T) {
	// Plan-only backend without a proven egress deny: no tier, even with a
	// claimed credential floor. Assigning one is a future deliberate change.
	_, err := Derive(Posture{Mode: sessionmeta.ModeAppleContainer, CredentialFloorEnforced: true})
	if err == nil {
		t.Fatal("apple-container mode must fail closed")
	}
	if !strings.Contains(err.Error(), "plan-only backend") {
		t.Fatalf("unexpected error: %v", err)
	}
}
