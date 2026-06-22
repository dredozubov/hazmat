package runtimecapability

import (
	"strings"
	"testing"

	"hazmat/attestationkey"
)

func TestLifecycleActiveAllowsNewDispatch(t *testing.T) {
	fixture := lifecycleFixture(t, nil)
	decision, err := EvaluateLifecycle(fixture.declaration, fixture.feed, fixture.inputAt(t, "2026-06-22T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != LifecycleActive || decision.Dispatch != DispatchAllow {
		t.Fatalf("decision = %+v, want active/allow", decision)
	}
}

func TestLifecycleGraceRequiresParentPolicy(t *testing.T) {
	fixture := lifecycleFixture(t, []RevocationRecord{{State: LifecycleGrace, Reason: "operator grace window"}})
	decision, err := EvaluateLifecycle(fixture.declaration, fixture.feed, fixture.inputAt(t, "2026-06-22T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != LifecycleGrace || decision.Dispatch != DispatchRequiresParentPolicy {
		t.Fatalf("decision = %+v, want grace/parent-policy", decision)
	}
}

func TestLifecycleRevokedDeniesNewDispatch(t *testing.T) {
	fixture := lifecycleFixture(t, []RevocationRecord{{State: LifecycleRevoked, Reason: "compromised signer"}})
	decision, err := EvaluateLifecycle(fixture.declaration, fixture.feed, fixture.inputAt(t, "2026-06-22T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != LifecycleRevoked || decision.Dispatch != DispatchDeny {
		t.Fatalf("decision = %+v, want revoked/deny", decision)
	}
}

func TestLifecycleStaleFeedDeniesNewDispatch(t *testing.T) {
	fixture := lifecycleFixture(t, nil)
	decision, err := EvaluateLifecycle(fixture.declaration, fixture.feed, fixture.inputAt(t, "2026-06-26T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != LifecycleStale || decision.Dispatch != DispatchDeny || !strings.Contains(decision.Reason, "stale") {
		t.Fatalf("decision = %+v, want stale/deny", decision)
	}
}

func TestLifecycleMissingCoverageFailsClosed(t *testing.T) {
	fixture := lifecycleFixture(t, nil)
	feed := fixture.feed
	feed.Coverage = []RevocationCoverage{{SignerTrustRoot: "other-root", BackendFamily: "hazmat-macos-local"}}
	feed.Signature = resignFeed(t, feed, fixture.feedKey)
	fixture.declaration.Capability.RevocationFeedFingerprint = feed.Fingerprint()
	fixture.declaration.RevocationFeedFingerprint = feed.Fingerprint()
	fixture.declaration.CapabilitySetFingerprint = fingerprintFromPayload(t, fixture.declaration.Capability)
	fixture.declaration.Signature = resignDeclaration(t, fixture.declaration, fixture.declarationKey)
	decision, err := EvaluateLifecycle(fixture.declaration, feed, fixture.inputAt(t, "2026-06-22T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != LifecycleStale || decision.Dispatch != DispatchDeny || !strings.Contains(decision.Reason, "does not cover") {
		t.Fatalf("decision = %+v, want missing coverage fail-closed", decision)
	}
}

func TestLifecycleReattestationNeededDeniesNewDispatch(t *testing.T) {
	fixture := lifecycleFixture(t, nil)
	decision, err := EvaluateLifecycle(fixture.declaration, fixture.feed, fixture.inputAt(t, "2026-06-23T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != LifecycleReattestationNeeded || decision.Dispatch != DispatchDeny {
		t.Fatalf("decision = %+v, want reattestation-needed/deny", decision)
	}
}

func TestLifecycleRejectsTamperedFeedSignature(t *testing.T) {
	fixture := lifecycleFixture(t, nil)
	feed := fixture.feed
	feed.FeedID = "tampered"
	decision, err := EvaluateLifecycle(fixture.declaration, feed, fixture.inputAt(t, "2026-06-22T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != LifecycleStale || !strings.Contains(decision.Reason, "signature") {
		t.Fatalf("decision = %+v, want signature failure as stale/deny", decision)
	}
}

type lifecycleTestFixture struct {
	declaration    Declaration
	feed           SignedRevocationFeed
	declarationKey attestationkey.Key
	feedKey        attestationkey.Key
}

func (f lifecycleTestFixture) inputAt(t *testing.T, ts string) LifecycleVerifyInput {
	return LifecycleVerifyInput{
		DeclarationKey: f.declarationKey,
		FeedKey:        f.feedKey,
		Now:            parseTestTime(t, ts),
	}
}

func lifecycleFixture(t *testing.T, records []RevocationRecord) lifecycleTestFixture {
	t.Helper()
	key := testKey(t, "lifecycle")
	feed, err := SignRevocationFeed(RevocationFeedInput{
		FeedID: "hazmat-revocations",
		Coverage: []RevocationCoverage{{
			SignerTrustRoot: "planescape-runtime-root-demo",
			BackendFamily:   "hazmat-macos-local",
		}},
		Records:    recordsWithCapabilitySetID(records),
		IssuedAt:   parseTestTime(t, "2026-06-22T00:00:00Z"),
		ValidUntil: parseTestTime(t, "2026-06-25T00:00:00Z"),
		Signer:     key,
	})
	if err != nil {
		t.Fatal(err)
	}
	capability, err := NewCapability(CapabilityInput{
		CapabilitySetID:              "hazmat-local-readonly-demo",
		BackendID:                    "hazmat-macos-local",
		BackendKind:                  BackendMacOSLocal,
		BackendVersion:               "hazmat-dev-2026-06-22",
		IsolationTier:                IsolationSameUIDProcess,
		WorkspaceGrantPatterns:       []string{"read:repo:*"},
		NetworkGrantPatterns:         []string{"deny:all"},
		CredentialModes:              []CredentialMode{CredentialNone},
		ServiceGrantPatterns:         []string{"git:status"},
		ConformanceResultFingerprint: "sha256:4444444444444444444444444444444444444444444444444444444444444444",
		CoverageCatalogFingerprint:   "sha256:5555555555555555555555555555555555555555555555555555555555555555",
		RevocationFeedFingerprint:    feed.Fingerprint(),
		SignerTrustRoot:              "planescape-runtime-root-demo",
		TrustRootEpoch:               7,
		DeclarationNonce:             "runtime-capability-lifecycle-demo",
		ValidAfter:                   parseTestTime(t, "2026-06-22T00:00:00Z"),
		ValidUntil:                   parseTestTime(t, "2026-06-29T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := SignDeclaration(DeclarationInput{
		Capability:          capability,
		BackendCodeRevision: "git:abc123",
		AttestationTier:     "native-uncontained",
		ReattestAfter:       parseTestTime(t, "2026-06-23T00:00:00Z"),
		RevocationFeedRef:   "revocations/hazmat-local.json",
		Signer:              key,
	})
	if err != nil {
		t.Fatal(err)
	}
	return lifecycleTestFixture{
		declaration:    declaration,
		feed:           feed,
		declarationKey: key,
		feedKey:        key,
	}
}

func recordsWithCapabilitySetID(records []RevocationRecord) []RevocationRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]RevocationRecord, len(records))
	for i, record := range records {
		out[i] = record
		out[i].CapabilitySetID = "hazmat-local-readonly-demo"
	}
	return out
}

func resignFeed(t *testing.T, feed SignedRevocationFeed, key attestationkey.Key) string {
	t.Helper()
	signature := key.Sign(revocationFeedSignaturePreimage(feed))
	return "hmac-sha256:" + hexBytes(signature)
}
