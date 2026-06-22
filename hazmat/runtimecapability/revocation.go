package runtimecapability

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"hazmat/attestationkey"
)

const (
	RevocationFeedSchema = "hazmat.runtime.capability.revocation_feed.v1"
	RevocationFeedDomain = "hazmat.runtime.capability.revocation_feed.fingerprint.v1"
	RevocationSigDomain  = "hazmat.runtime.capability.revocation_feed.signature.v1"
)

type LifecycleState string

const (
	LifecycleActive              LifecycleState = "active"
	LifecycleGrace               LifecycleState = "grace"
	LifecycleRevoked             LifecycleState = "revoked"
	LifecycleStale               LifecycleState = "stale"
	LifecycleReattestationNeeded LifecycleState = "reattestation_needed"
)

type DispatchDisposition string

const (
	DispatchAllow                DispatchDisposition = "allow_new_dispatch"
	DispatchDeny                 DispatchDisposition = "deny_new_dispatch"
	DispatchRequiresParentPolicy DispatchDisposition = "requires_parent_policy_gate"
)

type RevocationCoverage struct {
	SignerTrustRoot string `json:"signer_trust_root"`
	BackendFamily   string `json:"backend_family"`
}

type RevocationRecord struct {
	CapabilitySetID string         `json:"capability_set_id"`
	State           LifecycleState `json:"state"`
	Reason          string         `json:"reason,omitempty"`
}

type RevocationFeedInput struct {
	FeedID     string
	Coverage   []RevocationCoverage
	Records    []RevocationRecord
	IssuedAt   time.Time
	ValidUntil time.Time
	Signer     attestationkey.Key
}

type SignedRevocationFeed struct {
	Schema     string               `json:"schema"`
	FeedID     string               `json:"feed_id"`
	Coverage   []RevocationCoverage `json:"coverage"`
	Records    []RevocationRecord   `json:"records,omitempty"`
	IssuedAt   string               `json:"issued_at"`
	ValidUntil string               `json:"valid_until"`
	Signature  string               `json:"signature"`
}

type LifecycleVerifyInput struct {
	DeclarationKey attestationkey.Key
	FeedKey        attestationkey.Key
	Now            time.Time
}

type LifecycleDecision struct {
	State      LifecycleState      `json:"state"`
	Dispatch   DispatchDisposition `json:"dispatch"`
	Reason     string              `json:"reason,omitempty"`
	Coverage   RevocationCoverage  `json:"coverage"`
	FeedID     string              `json:"feed_id"`
	FeedHash   string              `json:"feed_hash"`
	Capability string              `json:"capability_set_fingerprint"`
	ReattestAt string              `json:"reattest_at,omitempty"`
}

func SignRevocationFeed(input RevocationFeedInput) (SignedRevocationFeed, error) {
	if !input.Signer.Valid() {
		return SignedRevocationFeed{}, fmt.Errorf("runtimecapability: revocation feed signer is not configured")
	}
	if err := requireText("feed_id", input.FeedID); err != nil {
		return SignedRevocationFeed{}, err
	}
	coverage, err := normalizedCoverage(input.Coverage)
	if err != nil {
		return SignedRevocationFeed{}, err
	}
	records, err := normalizedRecords(input.Records)
	if err != nil {
		return SignedRevocationFeed{}, err
	}
	if input.IssuedAt.IsZero() || input.ValidUntil.IsZero() {
		return SignedRevocationFeed{}, fmt.Errorf("runtimecapability: revocation feed issued_at and valid_until are required")
	}
	if !input.IssuedAt.Before(input.ValidUntil) {
		return SignedRevocationFeed{}, fmt.Errorf("runtimecapability: revocation feed issued_at must be before valid_until")
	}
	feed := SignedRevocationFeed{
		Schema:     RevocationFeedSchema,
		FeedID:     strings.TrimSpace(input.FeedID),
		Coverage:   coverage,
		Records:    records,
		IssuedAt:   input.IssuedAt.UTC().Format(time.RFC3339),
		ValidUntil: input.ValidUntil.UTC().Format(time.RFC3339),
	}
	signature := input.Signer.Sign(revocationFeedSignaturePreimage(feed))
	feed.Signature = "hmac-sha256:" + hexBytes(signature)
	return feed, nil
}

func (f SignedRevocationFeed) Fingerprint() string {
	return fingerprint(revocationFeedFingerprintPreimage(f))
}

func EvaluateLifecycle(declaration Declaration, feed SignedRevocationFeed, input LifecycleVerifyInput) (LifecycleDecision, error) {
	if err := VerifyDeclaration(declaration, VerifyInput{
		Signer:                  input.DeclarationKey,
		ExpectedSignerTrustRoot: declaration.SignerTrustRoot,
		ExpectedBackendVersion:  declaration.BackendVersion,
		Now:                     input.Now,
	}); err != nil {
		return LifecycleDecision{}, err
	}
	capability, err := capabilityFromPayload(declaration.Capability)
	if err != nil {
		return LifecycleDecision{}, err
	}
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := verifyRevocationFeed(feed, input.FeedKey, now); err != nil {
		return LifecycleDecision{
			State:      LifecycleStale,
			Dispatch:   DispatchDeny,
			Reason:     err.Error(),
			FeedID:     feed.FeedID,
			FeedHash:   feed.Fingerprint(),
			Capability: declaration.CapabilitySetFingerprint,
		}, nil
	}
	if capability.revocationFeedFingerprint != feed.Fingerprint() {
		return LifecycleDecision{}, fmt.Errorf("runtimecapability: revocation feed fingerprint mismatch")
	}
	coverage, ok := feedCovers(feed.Coverage, declaration.SignerTrustRoot, declaration.Capability.BackendID)
	if !ok {
		return LifecycleDecision{
			State:      LifecycleStale,
			Dispatch:   DispatchDeny,
			Reason:     "revocation feed does not cover signer root/backend family",
			FeedID:     feed.FeedID,
			FeedHash:   feed.Fingerprint(),
			Capability: declaration.CapabilitySetFingerprint,
		}, nil
	}
	record, hasRecord := findRecord(feed.Records, declaration.Capability.CapabilitySetID)
	if hasRecord && record.State == LifecycleRevoked {
		return lifecycleDecision(declaration, feed, coverage, LifecycleRevoked, DispatchDeny, record.Reason), nil
	}
	reattestAt, err := parseTime("reattest_after", declaration.ReattestAfter)
	if err != nil {
		return LifecycleDecision{}, err
	}
	if !now.Before(reattestAt) {
		decision := lifecycleDecision(declaration, feed, coverage, LifecycleReattestationNeeded, DispatchDeny, "re-attestation window elapsed")
		decision.ReattestAt = declaration.ReattestAfter
		return decision, nil
	}
	if hasRecord && record.State == LifecycleGrace {
		return lifecycleDecision(declaration, feed, coverage, LifecycleGrace, DispatchRequiresParentPolicy, record.Reason), nil
	}
	return lifecycleDecision(declaration, feed, coverage, LifecycleActive, DispatchAllow, ""), nil
}

func verifyRevocationFeed(feed SignedRevocationFeed, key attestationkey.Key, now time.Time) error {
	if !key.Valid() {
		return fmt.Errorf("runtimecapability: revocation feed key is not configured")
	}
	if feed.Schema != RevocationFeedSchema {
		return fmt.Errorf("runtimecapability: unsupported revocation feed schema %q", feed.Schema)
	}
	if err := requireText("feed_id", feed.FeedID); err != nil {
		return err
	}
	if _, err := normalizedCoverage(feed.Coverage); err != nil {
		return err
	}
	if _, err := normalizedRecords(feed.Records); err != nil {
		return err
	}
	issuedAt, err := parseTime("revocation_feed.issued_at", feed.IssuedAt)
	if err != nil {
		return err
	}
	validUntil, err := parseTime("revocation_feed.valid_until", feed.ValidUntil)
	if err != nil {
		return err
	}
	if now.Before(issuedAt) {
		return fmt.Errorf("runtimecapability: revocation feed is not valid yet")
	}
	if !now.Before(validUntil) {
		return fmt.Errorf("runtimecapability: revocation feed is stale")
	}
	signature, err := parseSignature(feed.Signature)
	if err != nil {
		return fmt.Errorf("runtimecapability: revocation feed %w", err)
	}
	if !key.Verify(revocationFeedSignaturePreimage(feed), signature) {
		return fmt.Errorf("runtimecapability: revocation feed signature verification failed")
	}
	return nil
}

func normalizedCoverage(values []RevocationCoverage) ([]RevocationCoverage, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("runtimecapability: revocation feed coverage is required")
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]RevocationCoverage, 0, len(values))
	for _, value := range values {
		if err := requireText("coverage.signer_trust_root", value.SignerTrustRoot); err != nil {
			return nil, err
		}
		if err := requireText("coverage.backend_family", value.BackendFamily); err != nil {
			return nil, err
		}
		coverage := RevocationCoverage{
			SignerTrustRoot: strings.TrimSpace(value.SignerTrustRoot),
			BackendFamily:   strings.TrimSpace(value.BackendFamily),
		}
		key := coverage.SignerTrustRoot + "\x00" + coverage.BackendFamily
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("runtimecapability: duplicate revocation coverage %q/%q", coverage.SignerTrustRoot, coverage.BackendFamily)
		}
		seen[key] = struct{}{}
		out = append(out, coverage)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SignerTrustRoot == out[j].SignerTrustRoot {
			return out[i].BackendFamily < out[j].BackendFamily
		}
		return out[i].SignerTrustRoot < out[j].SignerTrustRoot
	})
	return out, nil
}

func normalizedRecords(values []RevocationRecord) ([]RevocationRecord, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]RevocationRecord, 0, len(values))
	for _, value := range values {
		if err := requireText("capability_set_id", value.CapabilitySetID); err != nil {
			return nil, err
		}
		state, err := parseLifecycleState(string(value.State))
		if err != nil {
			return nil, err
		}
		capabilitySetID := strings.TrimSpace(value.CapabilitySetID)
		if _, ok := seen[capabilitySetID]; ok {
			return nil, fmt.Errorf("runtimecapability: duplicate revocation record for %q", capabilitySetID)
		}
		seen[capabilitySetID] = struct{}{}
		out = append(out, RevocationRecord{
			CapabilitySetID: capabilitySetID,
			State:           state,
			Reason:          strings.TrimSpace(value.Reason),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CapabilitySetID < out[j].CapabilitySetID
	})
	return out, nil
}

func parseLifecycleState(value string) (LifecycleState, error) {
	state := LifecycleState(strings.TrimSpace(value))
	switch state {
	case LifecycleActive, LifecycleGrace, LifecycleRevoked:
		return state, nil
	case LifecycleStale, LifecycleReattestationNeeded:
		return "", fmt.Errorf("runtimecapability: lifecycle state %q is derived, not feed-authored", value)
	case "":
		return "", fmt.Errorf("runtimecapability: lifecycle state is required")
	default:
		return "", fmt.Errorf("runtimecapability: unsupported lifecycle state %q", value)
	}
}

func feedCovers(values []RevocationCoverage, signerTrustRoot, backendID string) (RevocationCoverage, bool) {
	family := backendFamily(backendID)
	for _, value := range values {
		if value.SignerTrustRoot == signerTrustRoot && value.BackendFamily == family {
			return value, true
		}
	}
	return RevocationCoverage{}, false
}

func backendFamily(backendID string) string {
	if before, _, ok := strings.Cut(backendID, ":"); ok {
		return before
	}
	return backendID
}

func findRecord(values []RevocationRecord, capabilitySetID string) (RevocationRecord, bool) {
	for _, value := range values {
		if value.CapabilitySetID == capabilitySetID {
			return value, true
		}
	}
	return RevocationRecord{}, false
}

func lifecycleDecision(declaration Declaration, feed SignedRevocationFeed, coverage RevocationCoverage, state LifecycleState, dispatch DispatchDisposition, reason string) LifecycleDecision {
	return LifecycleDecision{
		State:      state,
		Dispatch:   dispatch,
		Reason:     reason,
		Coverage:   coverage,
		FeedID:     feed.FeedID,
		FeedHash:   feed.Fingerprint(),
		Capability: declaration.CapabilitySetFingerprint,
	}
}

func revocationFeedFingerprintPreimage(feed SignedRevocationFeed) []byte {
	out := make([]byte, 0)
	out = append(out, []byte(RevocationFeedDomain)...)
	out = appendU16(out, 6)
	out = appendStringField(out, 1, RevocationFeedSchema)
	out = appendStringField(out, 2, feed.FeedID)
	out = appendCoverageListField(out, 3, feed.Coverage)
	out = appendRecordListField(out, 4, feed.Records)
	out = appendStringField(out, 5, feed.IssuedAt)
	out = appendStringField(out, 6, feed.ValidUntil)
	return out
}

func revocationFeedSignaturePreimage(feed SignedRevocationFeed) []byte {
	out := make([]byte, 0)
	out = append(out, []byte(RevocationSigDomain)...)
	out = appendLenBytes(out, []byte(feed.Fingerprint()))
	return out
}

func appendCoverageListField(out []byte, tag byte, values []RevocationCoverage) []byte {
	var payload []byte
	payload = appendU32(payload, uint32(len(values)))
	for _, value := range values {
		payload = appendLenBytes(payload, []byte(value.SignerTrustRoot))
		payload = appendLenBytes(payload, []byte(value.BackendFamily))
	}
	return appendField(out, tag, tlvSortedUTF8List, payload)
}

func appendRecordListField(out []byte, tag byte, values []RevocationRecord) []byte {
	var payload []byte
	payload = appendU32(payload, uint32(len(values)))
	for _, value := range values {
		payload = appendLenBytes(payload, []byte(value.CapabilitySetID))
		payload = appendLenBytes(payload, []byte(value.State))
		payload = appendLenBytes(payload, []byte(value.Reason))
	}
	return appendField(out, tag, tlvSortedUTF8List, payload)
}
