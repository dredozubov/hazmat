// Package attestationtier derives the honest Beadpost containment tier a host
// broker may assert in a beadpost.containment.attestation.v2 token, from the
// EFFECTIVE enforced launch posture (never the requested posture).
//
// It is a pure, library-first over-claim guard: it never returns the strong
// Contained tier unless the posture actually supports it. The broker (bead .4)
// derives the tier from launch facts with this package and stamps it into the
// token; it never accepts a tier from the contained agent.
package attestationtier

import (
	"fmt"

	"hazmat/sessionmeta"
)

// Tier is a Beadpost containment tier label.
type Tier string

const (
	// Contained is the strong tier. It maps to Beadpost's DefaultTier
	// ("contained") and is asserted ONLY for a native session with deny-all
	// egress and an enforced credential floor.
	Contained Tier = "contained"

	// DockerSandbox is the distinct weaker tier for docker-sandbox sessions.
	// Egress parity with native deny-all is not proven; pursuing it is a
	// separate MC_TierPolicyEquivalence design bead, not this one.
	DockerSandbox Tier = "docker-sandbox"

	// NativeUncontained is a native session that does not meet the full
	// contained bar (for example egress is allowed, or the credential floor is
	// not enforced).
	NativeUncontained Tier = "native-uncontained"
)

// Posture is the effective, enforced launch posture a tier is derived from.
type Posture struct {
	Mode                    sessionmeta.Mode
	Network                 sessionmeta.NetworkPolicyMetadata
	CredentialFloorEnforced bool
}

// Derive returns the truthful Beadpost containment tier for the effective
// posture. It never returns Contained unless the posture is native mode with
// effective deny-all egress and an enforced credential floor. docker-sandbox
// always maps to DockerSandbox. Unknown modes fail closed with an error rather
// than guessing a tier.
func Derive(p Posture) (Tier, error) {
	switch p.Mode {
	case sessionmeta.ModeNative:
		if p.Network.DenyAllEgress && p.CredentialFloorEnforced {
			return Contained, nil
		}
		return NativeUncontained, nil
	case sessionmeta.ModeDockerSandbox:
		return DockerSandbox, nil
	default:
		return "", fmt.Errorf("attestationtier: unknown session mode %q", p.Mode)
	}
}

// IsContained reports whether the tier is the strong Beadpost contained tier.
func (t Tier) IsContained() bool {
	return t == Contained
}

// String returns the tier label.
func (t Tier) String() string {
	return string(t)
}
