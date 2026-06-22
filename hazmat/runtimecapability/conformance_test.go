package runtimecapability

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hazmat/attestationkey"
)

func TestVerifyCapabilityConformance(t *testing.T) {
	fixture := conformanceFixture(t)
	if err := VerifyCapabilityConformance(fixture.declaration, fixture.catalog, fixture.result, fixture.input); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyConformanceRejectsWrongArtifactHash(t *testing.T) {
	fixture := conformanceFixture(t)
	fixture.input.ExpectedArtifactHashes["tla/MC_CredentialCapabilityLifecycle.tla"] = strings.Replace(
		fixture.input.ExpectedArtifactHashes["tla/MC_CredentialCapabilityLifecycle.tla"],
		"sha256:",
		"sha256:0000",
		1,
	)[:71]
	err := VerifyCapabilityConformance(fixture.declaration, fixture.catalog, fixture.result, fixture.input)
	if err == nil || !strings.Contains(err.Error(), "does not match expected") {
		t.Fatalf("err = %v, want artifact hash mismatch", err)
	}
}

func TestVerifyConformanceRejectsWrongObligationName(t *testing.T) {
	fixture := conformanceFixture(t)
	fixture.input.ExpectedObligations[FlagNetworkGrantPatterns] = "WrongInvariant"
	err := VerifyCapabilityConformance(fixture.declaration, fixture.catalog, fixture.result, fixture.input)
	if err == nil || !strings.Contains(err.Error(), "names obligation") {
		t.Fatalf("err = %v, want obligation mismatch", err)
	}
}

func TestVerifyConformanceRejectsMismatchedBackendVersion(t *testing.T) {
	fixture := conformanceFixture(t)
	fixture.input.ExpectedBackendVersion = "hazmat-other"
	err := VerifyCapabilityConformance(fixture.declaration, fixture.catalog, fixture.result, fixture.input)
	if err == nil || !strings.Contains(err.Error(), "expected version") {
		t.Fatalf("err = %v, want backend version mismatch", err)
	}
}

func TestVerifyConformanceRejectsStaleVerifierResult(t *testing.T) {
	fixture := conformanceFixture(t)
	fixture.input.Now = parseTestTime(t, "2026-06-26T00:00:00Z")
	err := VerifyCapabilityConformance(fixture.declaration, fixture.catalog, fixture.result, fixture.input)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("err = %v, want stale verifier result", err)
	}
}

func TestVerifyConformanceRejectsMissingVerifierSignature(t *testing.T) {
	fixture := conformanceFixture(t)
	fixture.result.Signature = ""
	err := VerifyCapabilityConformance(fixture.declaration, fixture.catalog, fixture.result, fixture.input)
	if err == nil || !strings.Contains(err.Error(), "unsigned") {
		t.Fatalf("err = %v, want missing verifier signature", err)
	}
}

func TestVerifyConformanceRejectsMissingFlagCoverage(t *testing.T) {
	fixture := conformanceFixture(t)
	entries := fixture.catalog.Entries()
	var filtered []CoverageEntry
	for _, entry := range entries {
		if entry.CapabilityFlag != FlagServiceGrantPatterns {
			filtered = append(filtered, entry)
		}
	}
	catalog, err := NewCoverageCatalog(filtered)
	if err != nil {
		t.Fatal(err)
	}
	fixture.declaration.Capability.CoverageCatalogFingerprint = catalog.Fingerprint()
	fixture.declaration.CapabilitySetFingerprint = fingerprintFromPayload(t, fixture.declaration.Capability)
	fixture.declaration.Signature = resignDeclaration(t, fixture.declaration, fixture.declarationKey)
	err = VerifyCapabilityConformance(fixture.declaration, catalog, fixture.result, fixture.input)
	if err == nil || !strings.Contains(err.Error(), "missing conformance coverage") {
		t.Fatalf("err = %v, want missing coverage", err)
	}
}

func TestCoverageCatalogEntriesNameRequiredFields(t *testing.T) {
	fixture := conformanceFixture(t)
	for _, entry := range fixture.catalog.Entries() {
		if entry.CapabilityFlag == "" ||
			entry.BackendVersion == "" ||
			entry.BackendCodeRevision == "" ||
			entry.ArtifactHash == "" ||
			entry.VerifierIdentity == "" ||
			entry.VerifierResultHash == "" ||
			entry.ObligationName == "" {
			t.Fatalf("coverage entry missing required field: %+v", entry)
		}
	}
}

type conformanceTestFixture struct {
	declaration    Declaration
	catalog        CoverageCatalog
	result         SignedVerifierResult
	input          ConformanceVerifyInput
	declarationKey attestationkey.Key
}

func conformanceFixture(t *testing.T) conformanceTestFixture {
	t.Helper()
	key := testKey(t, "shared")
	backendVersion := "hazmat-dev-2026-06-22"
	backendRevision := "git:abc123"
	artifacts := []ArtifactDigest{
		{Name: "tla/MC_CredentialCapabilityLifecycle.tla", Hash: fileSHA256(t, "../../tla/MC_CredentialCapabilityLifecycle.tla")},
		{Name: "hazmat/runtimecapability/capability_test.go", Hash: fileSHA256(t, "capability_test.go")},
	}
	obligationsByFlag := map[CapabilityFlag]string{
		FlagCredentialModes:        "CredentialModeMatchesDelivery",
		FlagIsolationTier:          "IsolationTierRequiresBackendEvidence",
		FlagNetworkGrantPatterns:   "NetworkGrantPatternsAreDeniedOrScoped",
		FlagServiceGrantPatterns:   "ServiceGrantPatternsAreExplicit",
		FlagWorkspaceGrantPatterns: "WorkspaceGrantPatternsAreScoped",
	}
	obligations := make([]string, 0, len(obligationsByFlag))
	for _, obligation := range obligationsByFlag {
		obligations = append(obligations, obligation)
	}
	result, err := SignVerifierResult(VerifierResultInput{
		VerifierIdentity:    "hazmat-ci",
		BackendVersion:      backendVersion,
		BackendCodeRevision: backendRevision,
		Artifacts:           artifacts,
		Obligations:         obligations,
		IssuedAt:            parseTestTime(t, "2026-06-22T00:00:00Z"),
		ValidUntil:          parseTestTime(t, "2026-06-25T00:00:00Z"),
		Passed:              true,
		Signer:              key,
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewCoverageCatalog(coverageEntries(backendVersion, backendRevision, result, artifacts, obligationsByFlag))
	if err != nil {
		t.Fatal(err)
	}
	capability, err := NewCapability(CapabilityInput{
		CapabilitySetID:              "hazmat-local-readonly-demo",
		BackendID:                    "hazmat-macos-local",
		BackendKind:                  BackendMacOSLocal,
		BackendVersion:               backendVersion,
		IsolationTier:                IsolationSameUIDProcess,
		WorkspaceGrantPatterns:       []string{"read:repo:*", "read:state:*"},
		NetworkGrantPatterns:         []string{"allow:tailnet", "deny:public-internet"},
		CredentialModes:              []CredentialMode{CredentialNone},
		ServiceGrantPatterns:         []string{"beads:read", "git:status"},
		ConformanceResultFingerprint: result.Fingerprint(),
		CoverageCatalogFingerprint:   catalog.Fingerprint(),
		RevocationFeedFingerprint:    "sha256:6666666666666666666666666666666666666666666666666666666666666666",
		SignerTrustRoot:              "planescape-runtime-root-demo",
		TrustRootEpoch:               7,
		DeclarationNonce:             "runtime-capability-demo-1",
		ValidAfter:                   parseTestTime(t, "2026-06-22T00:00:00Z"),
		ValidUntil:                   parseTestTime(t, "2026-06-29T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := SignDeclaration(DeclarationInput{
		Capability:          capability,
		BackendCodeRevision: backendRevision,
		AttestationTier:     "native-uncontained",
		ReattestAfter:       parseTestTime(t, "2026-06-23T00:00:00Z"),
		RevocationFeedRef:   "revocations/hazmat-local.json",
		Signer:              key,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedHashes := make(map[string]string, len(artifacts))
	for _, artifact := range artifacts {
		expectedHashes[artifact.Name] = artifact.Hash
	}
	return conformanceTestFixture{
		declaration: declaration,
		catalog:     catalog,
		result:      result,
		input: ConformanceVerifyInput{
			DeclarationKey:         key,
			VerifierKey:            key,
			ExpectedVerifier:       "hazmat-ci",
			ExpectedBackendVersion: backendVersion,
			Now:                    parseTestTime(t, "2026-06-22T12:00:00Z"),
			ExpectedArtifactHashes: expectedHashes,
			ExpectedObligations:    obligationsByFlag,
		},
		declarationKey: key,
	}
}

func coverageEntries(backendVersion, backendRevision string, result SignedVerifierResult, artifacts []ArtifactDigest, obligations map[CapabilityFlag]string) []CoverageEntry {
	var entries []CoverageEntry
	for flag, obligation := range obligations {
		artifact := artifacts[0]
		if flag == FlagServiceGrantPatterns {
			artifact = artifacts[1]
		}
		entries = append(entries, CoverageEntry{
			CapabilityFlag:      flag,
			BackendVersion:      backendVersion,
			BackendCodeRevision: backendRevision,
			ArtifactName:        artifact.Name,
			ArtifactHash:        artifact.Hash,
			VerifierIdentity:    result.VerifierIdentity,
			VerifierResultHash:  result.Fingerprint(),
			ObligationName:      obligation,
		})
	}
	return entries
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	clean := filepath.Clean(path)
	data, err := os.ReadFile(clean)
	if err != nil {
		t.Fatalf("read %s: %v", clean, err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func fingerprintFromPayload(t *testing.T, payload CapabilityPayload) string {
	t.Helper()
	capability, err := capabilityFromPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	return capability.AuthorityFingerprint()
}

func resignDeclaration(t *testing.T, declaration Declaration, key attestationkey.Key) string {
	t.Helper()
	signature := key.Sign(declarationSignaturePreimage(declaration))
	return "hmac-sha256:" + hex.EncodeToString(signature)
}

func TestFileSHA256ReferencesExistingHazmatArtifacts(t *testing.T) {
	if got := fileSHA256(t, "../../tla/MC_CredentialCapabilityLifecycle.tla"); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("unexpected TLA artifact hash %q", got)
	}
	if got := fileSHA256(t, "capability_test.go"); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("unexpected Go test artifact hash %q", got)
	}
}
