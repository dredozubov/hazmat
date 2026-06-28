package runtimecapability

import (
	"strings"
	"testing"

	"hazmat/attestationkey"
)

func TestVerifyRoutingEligibilityAllowsVerifiedActiveCapability(t *testing.T) {
	input := routingEligibilityFixture(t, nil)
	eligibility, err := VerifyRoutingEligibility(input)
	if err != nil {
		t.Fatal(err)
	}
	if eligibility.CapabilitySetFingerprint != input.Declaration.CapabilitySetFingerprint ||
		eligibility.BackendKind != BackendMacOSLocal ||
		eligibility.Lifecycle.Dispatch != DispatchAllow {
		t.Fatalf("eligibility = %+v", eligibility)
	}
	eligibility.WorkspaceGrantPatterns[0] = "mutated"
	second, err := VerifyRoutingEligibility(input)
	if err != nil {
		t.Fatal(err)
	}
	if second.WorkspaceGrantPatterns[0] == "mutated" {
		t.Fatal("routing eligibility exposed internal workspace grants")
	}
}

func TestVerifyRoutingEligibilityRequiresExpectedTrustRoot(t *testing.T) {
	input := routingEligibilityFixture(t, nil)
	input.ExpectedSignerTrustRoot = ""
	_, err := VerifyRoutingEligibility(input)
	if err == nil || !strings.Contains(err.Error(), "expected signer trust root is required") {
		t.Fatalf("err = %v, want missing expected trust root", err)
	}
}

func TestVerifyRoutingEligibilityRejectsWrongTrustRoot(t *testing.T) {
	input := routingEligibilityFixture(t, nil)
	input.ExpectedSignerTrustRoot = "other-runtime-root"
	_, err := VerifyRoutingEligibility(input)
	if err == nil || !strings.Contains(err.Error(), "expected root") {
		t.Fatalf("err = %v, want signer trust root mismatch", err)
	}
}

func TestVerifyRoutingEligibilityRejectsUnsignedDeclaration(t *testing.T) {
	input := routingEligibilityFixture(t, nil)
	input.Declaration.Signature = ""
	_, err := VerifyRoutingEligibility(input)
	if err == nil || !strings.Contains(err.Error(), "unsigned") {
		t.Fatalf("err = %v, want unsigned declaration rejection", err)
	}
}

func TestVerifyRoutingEligibilityRejectsExpiredDeclaration(t *testing.T) {
	input := routingEligibilityFixture(t, nil)
	input.Now = parseTestTime(t, "2026-06-29T00:00:00Z")
	_, err := VerifyRoutingEligibility(input)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v, want expired declaration rejection", err)
	}
}

func TestVerifyRoutingEligibilityRejectsRevokedCapability(t *testing.T) {
	input := routingEligibilityFixture(t, []RevocationRecord{{State: LifecycleRevoked, Reason: "compromised provider"}})
	_, err := VerifyRoutingEligibility(input)
	if err == nil || !strings.Contains(err.Error(), "dispatch denied") {
		t.Fatalf("err = %v, want lifecycle dispatch denial", err)
	}
}

func routingEligibilityFixture(t *testing.T, records []RevocationRecord) RoutingEligibilityInput {
	t.Helper()
	fixture := conformanceFixture(t)
	feed, err := SignRevocationFeed(RevocationFeedInput{
		FeedID: "hazmat-revocations",
		Coverage: []RevocationCoverage{{
			SignerTrustRoot: "planescape-runtime-root-demo",
			BackendFamily:   backendFamily(fixture.declaration.Capability.BackendID),
		}},
		Records:    recordsWithCapabilitySetID(records),
		IssuedAt:   parseTestTime(t, "2026-06-22T00:00:00Z"),
		ValidUntil: parseTestTime(t, "2026-06-25T00:00:00Z"),
		Signer:     fixture.declarationKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	declaration := declarationWithRevocationFeed(t, fixture.declaration, feed, fixture.declarationKey)
	return RoutingEligibilityInput{
		Declaration:             declaration,
		Catalog:                 fixture.catalog,
		VerifierResult:          fixture.result,
		RevocationFeed:          feed,
		DeclarationKey:          fixture.declarationKey,
		VerifierKey:             fixture.declarationKey,
		RevocationFeedKey:       fixture.declarationKey,
		ExpectedSignerTrustRoot: "planescape-runtime-root-demo",
		ExpectedVerifier:        "hazmat-ci",
		ExpectedBackendVersion:  "hazmat-dev-2026-06-22",
		Now:                     parseTestTime(t, "2026-06-22T12:00:00Z"),
		ExpectedArtifactHashes:  fixture.input.ExpectedArtifactHashes,
		ExpectedObligations:     fixture.input.ExpectedObligations,
	}
}

func declarationWithRevocationFeed(t *testing.T, declaration Declaration, feed SignedRevocationFeed, key attestationkey.Key) Declaration {
	t.Helper()
	declaration.Capability.RevocationFeedFingerprint = feed.Fingerprint()
	declaration.RevocationFeedFingerprint = feed.Fingerprint()
	declaration.CapabilitySetFingerprint = fingerprintFromPayload(t, declaration.Capability)
	declaration.Signature = resignDeclaration(t, declaration, key)
	return declaration
}
