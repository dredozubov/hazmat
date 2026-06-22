package runtimecapability

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"hazmat/attestationkey"
)

const (
	CoverageCatalogSchema = "hazmat.runtime.capability.coverage_catalog.v1"
	CoverageCatalogDomain = "hazmat.runtime.capability.coverage_catalog.fingerprint.v1"
	VerifierResultSchema  = "hazmat.runtime.capability.verifier_result.v1"
	VerifierResultDomain  = "hazmat.runtime.capability.verifier_result.fingerprint.v1"
	VerifierSigDomain     = "hazmat.runtime.capability.verifier_result.signature.v1"
)

type CapabilityFlag string

const (
	FlagIsolationTier          CapabilityFlag = "isolation_tier"
	FlagWorkspaceGrantPatterns CapabilityFlag = "workspace_grant_patterns"
	FlagNetworkGrantPatterns   CapabilityFlag = "network_grant_patterns"
	FlagCredentialModes        CapabilityFlag = "credential_modes"
	FlagServiceGrantPatterns   CapabilityFlag = "service_grant_patterns"
)

type ArtifactDigest struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type CoverageEntry struct {
	CapabilityFlag      CapabilityFlag `json:"capability_flag"`
	BackendVersion      string         `json:"backend_version"`
	BackendCodeRevision string         `json:"backend_code_revision"`
	ArtifactName        string         `json:"artifact_name"`
	ArtifactHash        string         `json:"artifact_hash"`
	VerifierIdentity    string         `json:"verifier_identity"`
	VerifierResultHash  string         `json:"verifier_result_hash"`
	ObligationName      string         `json:"obligation_name"`
}

type CoverageCatalog struct {
	entries []CoverageEntry
}

type VerifierResultInput struct {
	VerifierIdentity    string
	BackendVersion      string
	BackendCodeRevision string
	Artifacts           []ArtifactDigest
	Obligations         []string
	IssuedAt            time.Time
	ValidUntil          time.Time
	Passed              bool
	Signer              attestationkey.Key
}

type SignedVerifierResult struct {
	Schema              string           `json:"schema"`
	VerifierIdentity    string           `json:"verifier_identity"`
	BackendVersion      string           `json:"backend_version"`
	BackendCodeRevision string           `json:"backend_code_revision"`
	Artifacts           []ArtifactDigest `json:"artifacts"`
	Obligations         []string         `json:"obligations"`
	IssuedAt            string           `json:"issued_at"`
	ValidUntil          string           `json:"valid_until"`
	Passed              bool             `json:"passed"`
	Signature           string           `json:"signature"`
}

type ConformanceVerifyInput struct {
	DeclarationKey         attestationkey.Key
	VerifierKey            attestationkey.Key
	ExpectedVerifier       string
	ExpectedBackendVersion string
	Now                    time.Time
	ExpectedArtifactHashes map[string]string
	ExpectedObligations    map[CapabilityFlag]string
}

func (c Capability) DeclaredFlags() []CapabilityFlag {
	flags := []CapabilityFlag{FlagIsolationTier}
	if len(c.workspaceGrantPatterns) > 0 {
		flags = append(flags, FlagWorkspaceGrantPatterns)
	}
	if len(c.networkGrantPatterns) > 0 {
		flags = append(flags, FlagNetworkGrantPatterns)
	}
	if len(c.credentialModes) > 0 {
		flags = append(flags, FlagCredentialModes)
	}
	if len(c.serviceGrantPatterns) > 0 {
		flags = append(flags, FlagServiceGrantPatterns)
	}
	sort.Slice(flags, func(i, j int) bool {
		return string(flags[i]) < string(flags[j])
	})
	return flags
}

func NewCoverageCatalog(entries []CoverageEntry) (CoverageCatalog, error) {
	if len(entries) == 0 {
		return CoverageCatalog{}, fmt.Errorf("runtimecapability: coverage catalog requires at least one entry")
	}
	out := make([]CoverageEntry, len(entries))
	for i, entry := range entries {
		normalized, err := normalizeCoverageEntry(entry)
		if err != nil {
			return CoverageCatalog{}, fmt.Errorf("runtimecapability: coverage entry %d: %w", i, err)
		}
		out[i] = normalized
	}
	sortCoverageEntries(out)
	return CoverageCatalog{entries: out}, nil
}

func (c CoverageCatalog) Entries() []CoverageEntry {
	if len(c.entries) == 0 {
		return nil
	}
	out := make([]CoverageEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

func (c CoverageCatalog) Fingerprint() string {
	preimage := make([]byte, 0)
	preimage = append(preimage, []byte(CoverageCatalogDomain)...)
	preimage = appendU16(preimage, uint16(len(c.entries)))
	for _, entry := range c.entries {
		preimage = appendLenBytes(preimage, []byte(entryString(entry.CapabilityFlag)))
		preimage = appendLenBytes(preimage, []byte(entry.BackendVersion))
		preimage = appendLenBytes(preimage, []byte(entry.BackendCodeRevision))
		preimage = appendLenBytes(preimage, []byte(entry.ArtifactName))
		preimage = appendLenBytes(preimage, []byte(entry.ArtifactHash))
		preimage = appendLenBytes(preimage, []byte(entry.VerifierIdentity))
		preimage = appendLenBytes(preimage, []byte(entry.VerifierResultHash))
		preimage = appendLenBytes(preimage, []byte(entry.ObligationName))
	}
	return fingerprint(preimage)
}

func SignVerifierResult(input VerifierResultInput) (SignedVerifierResult, error) {
	if !input.Signer.Valid() {
		return SignedVerifierResult{}, fmt.Errorf("runtimecapability: verifier signer is not configured")
	}
	artifacts, err := normalizedArtifacts(input.Artifacts)
	if err != nil {
		return SignedVerifierResult{}, err
	}
	obligations, err := normalizedList("obligations", input.Obligations)
	if err != nil {
		return SignedVerifierResult{}, err
	}
	for field, value := range map[string]string{
		"verifier_identity":     input.VerifierIdentity,
		"backend_version":       input.BackendVersion,
		"backend_code_revision": input.BackendCodeRevision,
	} {
		if err := requireText(field, value); err != nil {
			return SignedVerifierResult{}, err
		}
	}
	if input.IssuedAt.IsZero() || input.ValidUntil.IsZero() {
		return SignedVerifierResult{}, fmt.Errorf("runtimecapability: verifier result issued_at and valid_until are required")
	}
	if !input.IssuedAt.Before(input.ValidUntil) {
		return SignedVerifierResult{}, fmt.Errorf("runtimecapability: verifier result issued_at must be before valid_until")
	}
	result := SignedVerifierResult{
		Schema:              VerifierResultSchema,
		VerifierIdentity:    strings.TrimSpace(input.VerifierIdentity),
		BackendVersion:      strings.TrimSpace(input.BackendVersion),
		BackendCodeRevision: strings.TrimSpace(input.BackendCodeRevision),
		Artifacts:           artifacts,
		Obligations:         obligations,
		IssuedAt:            input.IssuedAt.UTC().Format(time.RFC3339),
		ValidUntil:          input.ValidUntil.UTC().Format(time.RFC3339),
		Passed:              input.Passed,
	}
	signature := input.Signer.Sign(verifierSignaturePreimage(result))
	result.Signature = "hmac-sha256:" + hexBytes(signature)
	return result, nil
}

func (r SignedVerifierResult) Fingerprint() string {
	return fingerprint(verifierResultFingerprintPreimage(r))
}

func VerifyCapabilityConformance(declaration Declaration, catalog CoverageCatalog, result SignedVerifierResult, input ConformanceVerifyInput) error {
	if err := VerifyDeclaration(declaration, VerifyInput{
		Signer:                  input.DeclarationKey,
		ExpectedSignerTrustRoot: declaration.SignerTrustRoot,
		ExpectedBackendVersion:  input.ExpectedBackendVersion,
		Now:                     input.Now,
	}); err != nil {
		return err
	}
	capability, err := capabilityFromPayload(declaration.Capability)
	if err != nil {
		return err
	}
	if capability.coverageCatalogFingerprint != catalog.Fingerprint() {
		return fmt.Errorf("runtimecapability: coverage catalog fingerprint mismatch")
	}
	resultHash := result.Fingerprint()
	if capability.conformanceResultFingerprint != resultHash {
		return fmt.Errorf("runtimecapability: conformance result fingerprint mismatch")
	}
	if err := verifySignedVerifierResult(result, input, declaration); err != nil {
		return err
	}
	entries := catalog.Entries()
	for _, flag := range capability.DeclaredFlags() {
		if err := verifyFlagCoverage(flag, declaration, entries, result, input); err != nil {
			return err
		}
	}
	return nil
}

func verifySignedVerifierResult(result SignedVerifierResult, input ConformanceVerifyInput, declaration Declaration) error {
	if !input.VerifierKey.Valid() {
		return fmt.Errorf("runtimecapability: verifier key is not configured")
	}
	if result.Schema != VerifierResultSchema {
		return fmt.Errorf("runtimecapability: unsupported verifier result schema %q", result.Schema)
	}
	if input.ExpectedVerifier != "" && result.VerifierIdentity != input.ExpectedVerifier {
		return fmt.Errorf("runtimecapability: verifier identity %q is not expected verifier %q", result.VerifierIdentity, input.ExpectedVerifier)
	}
	if result.BackendVersion != declaration.BackendVersion {
		return fmt.Errorf("runtimecapability: verifier result backend_version does not match declaration")
	}
	if result.BackendCodeRevision != declaration.BackendCodeRevision {
		return fmt.Errorf("runtimecapability: verifier result backend_code_revision does not match declaration")
	}
	issuedAt, err := parseTime("verifier_result.issued_at", result.IssuedAt)
	if err != nil {
		return err
	}
	validUntil, err := parseTime("verifier_result.valid_until", result.ValidUntil)
	if err != nil {
		return err
	}
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Before(issuedAt) {
		return fmt.Errorf("runtimecapability: verifier result is not valid yet")
	}
	if !now.Before(validUntil) {
		return fmt.Errorf("runtimecapability: verifier result is stale")
	}
	if !result.Passed {
		return fmt.Errorf("runtimecapability: verifier result did not pass")
	}
	if _, err := normalizedArtifacts(result.Artifacts); err != nil {
		return err
	}
	if _, err := normalizedList("verifier_result.obligations", result.Obligations); err != nil {
		return err
	}
	signature, err := parseSignature(result.Signature)
	if err != nil {
		return fmt.Errorf("runtimecapability: verifier result %w", err)
	}
	if !input.VerifierKey.Verify(verifierSignaturePreimage(result), signature) {
		return fmt.Errorf("runtimecapability: verifier result signature verification failed")
	}
	return nil
}

func verifyFlagCoverage(flag CapabilityFlag, declaration Declaration, entries []CoverageEntry, result SignedVerifierResult, input ConformanceVerifyInput) error {
	expectedObligation, ok := input.ExpectedObligations[flag]
	if !ok || strings.TrimSpace(expectedObligation) == "" {
		return fmt.Errorf("runtimecapability: no expected obligation configured for capability flag %q", flag)
	}
	for _, entry := range entries {
		if entry.CapabilityFlag != flag {
			continue
		}
		if entry.BackendVersion != declaration.BackendVersion || entry.BackendCodeRevision != declaration.BackendCodeRevision {
			continue
		}
		if entry.VerifierIdentity != result.VerifierIdentity || entry.VerifierResultHash != result.Fingerprint() {
			continue
		}
		if entry.ObligationName != expectedObligation {
			return fmt.Errorf("runtimecapability: coverage entry for %q names obligation %q, want %q", flag, entry.ObligationName, expectedObligation)
		}
		expectedHash, ok := input.ExpectedArtifactHashes[entry.ArtifactName]
		if !ok {
			return fmt.Errorf("runtimecapability: no expected artifact hash configured for %q", entry.ArtifactName)
		}
		if entry.ArtifactHash != expectedHash {
			return fmt.Errorf("runtimecapability: artifact %q hash %q does not match expected %q", entry.ArtifactName, entry.ArtifactHash, expectedHash)
		}
		if !resultHasArtifact(result, entry.ArtifactName, entry.ArtifactHash) {
			return fmt.Errorf("runtimecapability: verifier result does not bind artifact %q hash %q", entry.ArtifactName, entry.ArtifactHash)
		}
		if !resultHasObligation(result, entry.ObligationName) {
			return fmt.Errorf("runtimecapability: verifier result does not bind obligation %q", entry.ObligationName)
		}
		return nil
	}
	return fmt.Errorf("runtimecapability: missing conformance coverage for capability flag %q", flag)
}

func normalizeCoverageEntry(entry CoverageEntry) (CoverageEntry, error) {
	flag, err := parseCapabilityFlag(string(entry.CapabilityFlag))
	if err != nil {
		return CoverageEntry{}, err
	}
	for field, value := range map[string]string{
		"backend_version":       entry.BackendVersion,
		"backend_code_revision": entry.BackendCodeRevision,
		"artifact_name":         entry.ArtifactName,
		"verifier_identity":     entry.VerifierIdentity,
		"obligation_name":       entry.ObligationName,
	} {
		if err := requireText(field, value); err != nil {
			return CoverageEntry{}, err
		}
	}
	if err := requireFingerprint("artifact_hash", entry.ArtifactHash); err != nil {
		return CoverageEntry{}, err
	}
	if err := requireFingerprint("verifier_result_hash", entry.VerifierResultHash); err != nil {
		return CoverageEntry{}, err
	}
	return CoverageEntry{
		CapabilityFlag:      flag,
		BackendVersion:      strings.TrimSpace(entry.BackendVersion),
		BackendCodeRevision: strings.TrimSpace(entry.BackendCodeRevision),
		ArtifactName:        strings.TrimSpace(entry.ArtifactName),
		ArtifactHash:        entry.ArtifactHash,
		VerifierIdentity:    strings.TrimSpace(entry.VerifierIdentity),
		VerifierResultHash:  entry.VerifierResultHash,
		ObligationName:      strings.TrimSpace(entry.ObligationName),
	}, nil
}

func parseCapabilityFlag(value string) (CapabilityFlag, error) {
	flag := CapabilityFlag(strings.TrimSpace(value))
	switch flag {
	case FlagIsolationTier, FlagWorkspaceGrantPatterns, FlagNetworkGrantPatterns, FlagCredentialModes, FlagServiceGrantPatterns:
		return flag, nil
	case "":
		return "", fmt.Errorf("runtimecapability: capability_flag is required")
	default:
		return "", fmt.Errorf("runtimecapability: unsupported capability_flag %q", value)
	}
}

func normalizedArtifacts(values []ArtifactDigest) ([]ArtifactDigest, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("runtimecapability: verifier result artifacts are required")
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]ArtifactDigest, 0, len(values))
	for _, artifact := range values {
		if err := requireText("artifact.name", artifact.Name); err != nil {
			return nil, err
		}
		if err := requireFingerprint("artifact.hash", artifact.Hash); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(artifact.Name)
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("runtimecapability: duplicate verifier result artifact %q", name)
		}
		seen[name] = struct{}{}
		out = append(out, ArtifactDigest{Name: name, Hash: artifact.Hash})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func verifierResultFingerprintPreimage(result SignedVerifierResult) []byte {
	out := make([]byte, 0)
	out = append(out, []byte(VerifierResultDomain)...)
	out = appendU16(out, 9)
	out = appendStringField(out, 1, VerifierResultSchema)
	out = appendStringField(out, 2, result.VerifierIdentity)
	out = appendStringField(out, 3, result.BackendVersion)
	out = appendStringField(out, 4, result.BackendCodeRevision)
	out = appendArtifactListField(out, 5, result.Artifacts)
	out = appendStringListField(out, 6, result.Obligations)
	out = appendStringField(out, 7, result.IssuedAt)
	out = appendStringField(out, 8, result.ValidUntil)
	if result.Passed {
		out = appendStringField(out, 9, "passed")
	} else {
		out = appendStringField(out, 9, "failed")
	}
	return out
}

func verifierSignaturePreimage(result SignedVerifierResult) []byte {
	out := make([]byte, 0)
	out = append(out, []byte(VerifierSigDomain)...)
	out = appendLenBytes(out, []byte(result.Fingerprint()))
	return out
}

func appendArtifactListField(out []byte, tag byte, values []ArtifactDigest) []byte {
	var payload []byte
	payload = appendU32(payload, uint32(len(values)))
	for _, value := range values {
		payload = appendLenBytes(payload, []byte(value.Name))
		payload = appendLenBytes(payload, []byte(value.Hash))
	}
	return appendField(out, tag, tlvSortedUTF8List, payload)
}

func resultHasArtifact(result SignedVerifierResult, name, hash string) bool {
	for _, artifact := range result.Artifacts {
		if artifact.Name == name && artifact.Hash == hash {
			return true
		}
	}
	return false
}

func resultHasObligation(result SignedVerifierResult, obligation string) bool {
	for _, value := range result.Obligations {
		if value == obligation {
			return true
		}
	}
	return false
}

func sortCoverageEntries(values []CoverageEntry) {
	sort.Slice(values, func(i, j int) bool {
		left, right := values[i], values[j]
		for _, pair := range [][2]string{
			{string(left.CapabilityFlag), string(right.CapabilityFlag)},
			{left.BackendVersion, right.BackendVersion},
			{left.BackendCodeRevision, right.BackendCodeRevision},
			{left.ArtifactName, right.ArtifactName},
			{left.ArtifactHash, right.ArtifactHash},
			{left.VerifierIdentity, right.VerifierIdentity},
			{left.VerifierResultHash, right.VerifierResultHash},
			{left.ObligationName, right.ObligationName},
		} {
			if pair[0] == pair[1] {
				continue
			}
			return pair[0] < pair[1]
		}
		return false
	})
}

func entryString(flag CapabilityFlag) string {
	return string(flag)
}

func hexBytes(value []byte) string {
	const alphabet = "0123456789abcdef"
	out := make([]byte, len(value)*2)
	for i, b := range value {
		out[i*2] = alphabet[b>>4]
		out[i*2+1] = alphabet[b&0x0f]
	}
	return string(out)
}
