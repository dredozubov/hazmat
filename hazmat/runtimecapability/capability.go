// Package runtimecapability emits and verifies Hazmat runtime.capability.v1
// declarations without reading live host state or launching sessions.
package runtimecapability

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"hazmat/attestationkey"
)

const (
	PayloadSchema       = "runtime.capability.v1"
	PayloadDomain       = "runtime.capability.authority.v1"
	PayloadFieldCount   = uint16(18)
	DeclarationSchema   = "hazmat.runtime.capability.declaration.v1"
	DeclarationDomain   = "hazmat.runtime.capability.declaration.signature.v1"
	DeclarationFieldCnt = uint16(13)
)

const (
	tlvUTF8String        = byte(0x02)
	tlvSortedUTF8List    = byte(0x03)
	tlvSHA256Fingerprint = byte(0x04)
	tlvU64BE             = byte(0x05)
)

var sha256Fingerprint = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type BackendKind string

const (
	BackendMacOSLocal         BackendKind = "macos_local"
	BackendLinuxProcess       BackendKind = "linux_process"
	BackendLinuxContainer     BackendKind = "linux_container"
	BackendHermesVM           BackendKind = "hermes_vm"
	BackendFirecrackerMicroVM BackendKind = "firecracker_microvm"
)

type IsolationTier string

const (
	IsolationSameUIDProcess IsolationTier = "same_uid_process"
	IsolationOSSandbox      IsolationTier = "os_sandbox"
	IsolationContainer      IsolationTier = "container"
	IsolationVM             IsolationTier = "vm"
	IsolationMicroVM        IsolationTier = "microvm"
)

type CredentialMode string

const (
	CredentialNone             CredentialMode = "none"
	CredentialBrokerScoped     CredentialMode = "broker_scoped"
	CredentialOperatorApproved CredentialMode = "operator_approved"
)

type CapabilityInput struct {
	CapabilitySetID              string
	BackendID                    string
	BackendKind                  BackendKind
	BackendVersion               string
	IsolationTier                IsolationTier
	WorkspaceGrantPatterns       []string
	NetworkGrantPatterns         []string
	CredentialModes              []CredentialMode
	ServiceGrantPatterns         []string
	ConformanceResultFingerprint string
	CoverageCatalogFingerprint   string
	RevocationFeedFingerprint    string
	SignerTrustRoot              string
	TrustRootEpoch               uint64
	DeclarationNonce             string
	ValidAfter                   time.Time
	ValidUntil                   time.Time
}

type Capability struct {
	capabilitySetID              string
	backendID                    string
	backendKind                  BackendKind
	backendVersion               string
	isolationTier                IsolationTier
	workspaceGrantPatterns       []string
	networkGrantPatterns         []string
	credentialModes              []CredentialMode
	serviceGrantPatterns         []string
	conformanceResultFingerprint string
	coverageCatalogFingerprint   string
	revocationFeedFingerprint    string
	signerTrustRoot              string
	trustRootEpoch               uint64
	declarationNonce             string
	validAfter                   time.Time
	validUntil                   time.Time
}

type CapabilityPayload struct {
	Schema                       string   `json:"schema"`
	CapabilitySetID              string   `json:"capability_set_id"`
	BackendID                    string   `json:"backend_id"`
	BackendKind                  string   `json:"backend_kind"`
	BackendVersion               string   `json:"backend_version"`
	IsolationTier                string   `json:"isolation_tier"`
	WorkspaceGrantPatterns       []string `json:"workspace_grant_patterns"`
	NetworkGrantPatterns         []string `json:"network_grant_patterns"`
	CredentialModes              []string `json:"credential_modes"`
	ServiceGrantPatterns         []string `json:"service_grant_patterns"`
	ConformanceResultFingerprint string   `json:"conformance_result_fingerprint"`
	CoverageCatalogFingerprint   string   `json:"coverage_catalog_fingerprint"`
	RevocationFeedFingerprint    string   `json:"revocation_feed_fingerprint"`
	SignerTrustRoot              string   `json:"signer_trust_root"`
	TrustRootEpoch               uint64   `json:"trust_root_epoch"`
	DeclarationNonce             string   `json:"declaration_nonce"`
	ValidAfter                   string   `json:"valid_after"`
	ValidUntil                   string   `json:"valid_until"`
}

type DeclarationInput struct {
	Capability          Capability
	BackendCodeRevision string
	AttestationTier     string
	ReattestAfter       time.Time
	RevocationFeedRef   string
	Signer              attestationkey.Key
}

type Declaration struct {
	Schema                    string            `json:"schema"`
	Capability                CapabilityPayload `json:"capability"`
	CapabilitySetFingerprint  string            `json:"capability_set_fingerprint"`
	BackendVersion            string            `json:"backend_version"`
	BackendCodeRevision       string            `json:"backend_code_revision"`
	IsolationTier             string            `json:"isolation_tier"`
	AttestationTier           string            `json:"attestation_tier"`
	ValidFrom                 string            `json:"valid_from"`
	ValidUntil                string            `json:"valid_until"`
	ReattestAfter             string            `json:"reattest_after"`
	RevocationFeedRef         string            `json:"revocation_feed_ref"`
	RevocationFeedFingerprint string            `json:"revocation_feed_fingerprint"`
	SignerTrustRoot           string            `json:"signer_trust_root"`
	TrustRootEpoch            uint64            `json:"trust_root_epoch"`
	Signature                 string            `json:"signature"`
}

type VerifyInput struct {
	Signer                  attestationkey.Key
	ExpectedSignerTrustRoot string
	ExpectedBackendVersion  string
	Now                     time.Time
}

func NewCapability(input CapabilityInput) (Capability, error) {
	backendKind, err := parseBackendKind(string(input.BackendKind))
	if err != nil {
		return Capability{}, err
	}
	isolationTier, err := parseIsolationTier(string(input.IsolationTier))
	if err != nil {
		return Capability{}, err
	}
	workspace, err := normalizedList("workspace_grant_patterns", input.WorkspaceGrantPatterns)
	if err != nil {
		return Capability{}, err
	}
	network, err := normalizedList("network_grant_patterns", input.NetworkGrantPatterns)
	if err != nil {
		return Capability{}, err
	}
	credentialModes, err := normalizedCredentialModes(input.CredentialModes)
	if err != nil {
		return Capability{}, err
	}
	services, err := normalizedList("service_grant_patterns", input.ServiceGrantPatterns)
	if err != nil {
		return Capability{}, err
	}
	for field, value := range map[string]string{
		"capability_set_id":              input.CapabilitySetID,
		"backend_id":                     input.BackendID,
		"backend_version":                input.BackendVersion,
		"signer_trust_root":              input.SignerTrustRoot,
		"declaration_nonce":              input.DeclarationNonce,
		"conformance_result_fingerprint": input.ConformanceResultFingerprint,
		"coverage_catalog_fingerprint":   input.CoverageCatalogFingerprint,
		"revocation_feed_fingerprint":    input.RevocationFeedFingerprint,
	} {
		if fieldHasFingerprintSuffix(field) {
			if err := requireFingerprint(field, value); err != nil {
				return Capability{}, err
			}
			continue
		}
		if err := requireText(field, value); err != nil {
			return Capability{}, err
		}
	}
	if input.ValidAfter.IsZero() || input.ValidUntil.IsZero() {
		return Capability{}, fmt.Errorf("runtimecapability: valid_after and valid_until are required")
	}
	if !input.ValidAfter.Before(input.ValidUntil) {
		return Capability{}, fmt.Errorf("runtimecapability: valid_after must be before valid_until")
	}
	return Capability{
		capabilitySetID:              strings.TrimSpace(input.CapabilitySetID),
		backendID:                    strings.TrimSpace(input.BackendID),
		backendKind:                  backendKind,
		backendVersion:               strings.TrimSpace(input.BackendVersion),
		isolationTier:                isolationTier,
		workspaceGrantPatterns:       workspace,
		networkGrantPatterns:         network,
		credentialModes:              credentialModes,
		serviceGrantPatterns:         services,
		conformanceResultFingerprint: input.ConformanceResultFingerprint,
		coverageCatalogFingerprint:   input.CoverageCatalogFingerprint,
		revocationFeedFingerprint:    input.RevocationFeedFingerprint,
		signerTrustRoot:              strings.TrimSpace(input.SignerTrustRoot),
		trustRootEpoch:               input.TrustRootEpoch,
		declarationNonce:             strings.TrimSpace(input.DeclarationNonce),
		validAfter:                   input.ValidAfter.UTC(),
		validUntil:                   input.ValidUntil.UTC(),
	}, nil
}

func ParseCapabilityJSON(data []byte) (Capability, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var payload CapabilityPayload
	if err := decoder.Decode(&payload); err != nil {
		return Capability{}, fmt.Errorf("runtimecapability: parse runtime.capability.v1: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Capability{}, err
	}
	return capabilityFromPayload(payload)
}

func (c Capability) Payload() CapabilityPayload {
	return CapabilityPayload{
		Schema:                       PayloadSchema,
		CapabilitySetID:              c.capabilitySetID,
		BackendID:                    c.backendID,
		BackendKind:                  string(c.backendKind),
		BackendVersion:               c.backendVersion,
		IsolationTier:                string(c.isolationTier),
		WorkspaceGrantPatterns:       copyStrings(c.workspaceGrantPatterns),
		NetworkGrantPatterns:         copyStrings(c.networkGrantPatterns),
		CredentialModes:              credentialModeStrings(c.credentialModes),
		ServiceGrantPatterns:         copyStrings(c.serviceGrantPatterns),
		ConformanceResultFingerprint: c.conformanceResultFingerprint,
		CoverageCatalogFingerprint:   c.coverageCatalogFingerprint,
		RevocationFeedFingerprint:    c.revocationFeedFingerprint,
		SignerTrustRoot:              c.signerTrustRoot,
		TrustRootEpoch:               c.trustRootEpoch,
		DeclarationNonce:             c.declarationNonce,
		ValidAfter:                   c.validAfter.Format(time.RFC3339),
		ValidUntil:                   c.validUntil.Format(time.RFC3339),
	}
}

func (c Capability) AuthorityPreimage() []byte {
	out := make([]byte, 0)
	out = append(out, []byte(PayloadDomain)...)
	out = appendU16(out, PayloadFieldCount)
	out = appendStringField(out, 1, PayloadSchema)
	out = appendStringField(out, 2, c.capabilitySetID)
	out = appendStringField(out, 3, c.backendID)
	out = appendStringField(out, 4, string(c.backendKind))
	out = appendStringField(out, 5, c.backendVersion)
	out = appendStringField(out, 6, string(c.isolationTier))
	out = appendStringListField(out, 7, c.workspaceGrantPatterns)
	out = appendStringListField(out, 8, c.networkGrantPatterns)
	out = appendCredentialModeListField(out, 9, c.credentialModes)
	out = appendStringListField(out, 10, c.serviceGrantPatterns)
	out = appendFingerprintField(out, 11, c.conformanceResultFingerprint)
	out = appendFingerprintField(out, 12, c.coverageCatalogFingerprint)
	out = appendFingerprintField(out, 13, c.revocationFeedFingerprint)
	out = appendStringField(out, 14, c.signerTrustRoot)
	out = appendU64Field(out, 15, c.trustRootEpoch)
	out = appendStringField(out, 16, c.declarationNonce)
	out = appendStringField(out, 17, c.validAfter.Format(time.RFC3339))
	out = appendStringField(out, 18, c.validUntil.Format(time.RFC3339))
	return out
}

func (c Capability) AuthorityFingerprint() string {
	return fingerprint(c.AuthorityPreimage())
}

func SignDeclaration(input DeclarationInput) (Declaration, error) {
	if !input.Signer.Valid() {
		return Declaration{}, fmt.Errorf("runtimecapability: signer is not configured")
	}
	if err := requireText("backend_code_revision", input.BackendCodeRevision); err != nil {
		return Declaration{}, err
	}
	if err := requireText("attestation_tier", input.AttestationTier); err != nil {
		return Declaration{}, err
	}
	if err := requireText("revocation_feed_ref", input.RevocationFeedRef); err != nil {
		return Declaration{}, err
	}
	if input.ReattestAfter.IsZero() {
		return Declaration{}, fmt.Errorf("runtimecapability: reattest_after is required")
	}
	if input.ReattestAfter.Before(input.Capability.validAfter) || !input.ReattestAfter.Before(input.Capability.validUntil) {
		return Declaration{}, fmt.Errorf("runtimecapability: reattest_after must fall within the capability validity window")
	}
	declaration := Declaration{
		Schema:                    DeclarationSchema,
		Capability:                input.Capability.Payload(),
		CapabilitySetFingerprint:  input.Capability.AuthorityFingerprint(),
		BackendVersion:            input.Capability.backendVersion,
		BackendCodeRevision:       strings.TrimSpace(input.BackendCodeRevision),
		IsolationTier:             string(input.Capability.isolationTier),
		AttestationTier:           strings.TrimSpace(input.AttestationTier),
		ValidFrom:                 input.Capability.validAfter.Format(time.RFC3339),
		ValidUntil:                input.Capability.validUntil.Format(time.RFC3339),
		ReattestAfter:             input.ReattestAfter.UTC().Format(time.RFC3339),
		RevocationFeedRef:         strings.TrimSpace(input.RevocationFeedRef),
		RevocationFeedFingerprint: input.Capability.revocationFeedFingerprint,
		SignerTrustRoot:           input.Capability.signerTrustRoot,
		TrustRootEpoch:            input.Capability.trustRootEpoch,
	}
	signature := input.Signer.Sign(declarationSignaturePreimage(declaration))
	declaration.Signature = "hmac-sha256:" + hex.EncodeToString(signature)
	return declaration, nil
}

func ParseDeclarationJSON(data []byte) (Declaration, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var declaration Declaration
	if err := decoder.Decode(&declaration); err != nil {
		return Declaration{}, fmt.Errorf("runtimecapability: parse declaration: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Declaration{}, err
	}
	return declaration, nil
}

func VerifyDeclaration(declaration Declaration, input VerifyInput) error {
	if !input.Signer.Valid() {
		return fmt.Errorf("runtimecapability: signer is not configured")
	}
	if declaration.Schema != DeclarationSchema {
		return fmt.Errorf("runtimecapability: unsupported declaration schema %q", declaration.Schema)
	}
	capability, err := capabilityFromPayload(declaration.Capability)
	if err != nil {
		return err
	}
	if declaration.CapabilitySetFingerprint != capability.AuthorityFingerprint() {
		return fmt.Errorf("runtimecapability: capability_set_fingerprint mismatch")
	}
	if declaration.BackendVersion != capability.backendVersion {
		return fmt.Errorf("runtimecapability: backend_version does not match capability payload")
	}
	if declaration.IsolationTier != string(capability.isolationTier) {
		return fmt.Errorf("runtimecapability: isolation_tier does not match capability payload")
	}
	if declaration.ValidFrom != capability.validAfter.Format(time.RFC3339) || declaration.ValidUntil != capability.validUntil.Format(time.RFC3339) {
		return fmt.Errorf("runtimecapability: validity window does not match capability payload")
	}
	if declaration.RevocationFeedFingerprint != capability.revocationFeedFingerprint {
		return fmt.Errorf("runtimecapability: revocation_feed_fingerprint does not match capability payload")
	}
	if declaration.SignerTrustRoot != capability.signerTrustRoot || declaration.TrustRootEpoch != capability.trustRootEpoch {
		return fmt.Errorf("runtimecapability: signer trust root does not match capability payload")
	}
	if input.ExpectedSignerTrustRoot != "" && input.ExpectedSignerTrustRoot != declaration.SignerTrustRoot {
		return fmt.Errorf("runtimecapability: signer_trust_root %q is not expected root %q", declaration.SignerTrustRoot, input.ExpectedSignerTrustRoot)
	}
	if input.ExpectedBackendVersion != "" && input.ExpectedBackendVersion != declaration.BackendVersion {
		return fmt.Errorf("runtimecapability: backend_version %q is not expected version %q", declaration.BackendVersion, input.ExpectedBackendVersion)
	}
	for field, value := range map[string]string{
		"backend_code_revision": declaration.BackendCodeRevision,
		"attestation_tier":      declaration.AttestationTier,
		"revocation_feed_ref":   declaration.RevocationFeedRef,
	} {
		if err := requireText(field, value); err != nil {
			return err
		}
	}
	reattestAfter, err := parseTime("reattest_after", declaration.ReattestAfter)
	if err != nil {
		return err
	}
	if reattestAfter.Before(capability.validAfter) || !reattestAfter.Before(capability.validUntil) {
		return fmt.Errorf("runtimecapability: reattest_after must fall within the capability validity window")
	}
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Before(capability.validAfter) {
		return fmt.Errorf("runtimecapability: declaration is not valid yet")
	}
	if !now.Before(capability.validUntil) {
		return fmt.Errorf("runtimecapability: declaration is expired")
	}
	signature, err := parseSignature(declaration.Signature)
	if err != nil {
		return err
	}
	if !input.Signer.Verify(declarationSignaturePreimage(declaration), signature) {
		return fmt.Errorf("runtimecapability: declaration signature verification failed")
	}
	return nil
}

func capabilityFromPayload(payload CapabilityPayload) (Capability, error) {
	if payload.Schema != PayloadSchema {
		return Capability{}, fmt.Errorf("runtimecapability: unsupported payload schema %q", payload.Schema)
	}
	validAfter, err := parseTime("valid_after", payload.ValidAfter)
	if err != nil {
		return Capability{}, err
	}
	validUntil, err := parseTime("valid_until", payload.ValidUntil)
	if err != nil {
		return Capability{}, err
	}
	modes := make([]CredentialMode, 0, len(payload.CredentialModes))
	for _, mode := range payload.CredentialModes {
		parsed, err := parseCredentialMode(mode)
		if err != nil {
			return Capability{}, err
		}
		modes = append(modes, parsed)
	}
	return NewCapability(CapabilityInput{
		CapabilitySetID:              payload.CapabilitySetID,
		BackendID:                    payload.BackendID,
		BackendKind:                  BackendKind(payload.BackendKind),
		BackendVersion:               payload.BackendVersion,
		IsolationTier:                IsolationTier(payload.IsolationTier),
		WorkspaceGrantPatterns:       payload.WorkspaceGrantPatterns,
		NetworkGrantPatterns:         payload.NetworkGrantPatterns,
		CredentialModes:              modes,
		ServiceGrantPatterns:         payload.ServiceGrantPatterns,
		ConformanceResultFingerprint: payload.ConformanceResultFingerprint,
		CoverageCatalogFingerprint:   payload.CoverageCatalogFingerprint,
		RevocationFeedFingerprint:    payload.RevocationFeedFingerprint,
		SignerTrustRoot:              payload.SignerTrustRoot,
		TrustRootEpoch:               payload.TrustRootEpoch,
		DeclarationNonce:             payload.DeclarationNonce,
		ValidAfter:                   validAfter,
		ValidUntil:                   validUntil,
	})
}

func declarationSignaturePreimage(declaration Declaration) []byte {
	out := make([]byte, 0)
	out = append(out, []byte(DeclarationDomain)...)
	out = appendU16(out, DeclarationFieldCnt)
	out = appendStringField(out, 1, DeclarationSchema)
	out = appendFingerprintField(out, 2, declaration.CapabilitySetFingerprint)
	out = appendStringField(out, 3, declaration.BackendVersion)
	out = appendStringField(out, 4, declaration.BackendCodeRevision)
	out = appendStringField(out, 5, declaration.IsolationTier)
	out = appendStringField(out, 6, declaration.AttestationTier)
	out = appendStringField(out, 7, declaration.ValidFrom)
	out = appendStringField(out, 8, declaration.ValidUntil)
	out = appendStringField(out, 9, declaration.ReattestAfter)
	out = appendStringField(out, 10, declaration.RevocationFeedRef)
	out = appendFingerprintField(out, 11, declaration.RevocationFeedFingerprint)
	out = appendStringField(out, 12, declaration.SignerTrustRoot)
	out = appendU64Field(out, 13, declaration.TrustRootEpoch)
	return out
}

func parseBackendKind(value string) (BackendKind, error) {
	kind := BackendKind(strings.TrimSpace(value))
	switch kind {
	case BackendMacOSLocal, BackendLinuxProcess, BackendLinuxContainer, BackendHermesVM, BackendFirecrackerMicroVM:
		return kind, nil
	case "":
		return "", fmt.Errorf("runtimecapability: backend_kind is required")
	default:
		return "", fmt.Errorf("runtimecapability: unsupported backend_kind %q", value)
	}
}

func parseIsolationTier(value string) (IsolationTier, error) {
	tier := IsolationTier(strings.TrimSpace(value))
	switch tier {
	case IsolationSameUIDProcess, IsolationOSSandbox, IsolationContainer, IsolationVM, IsolationMicroVM:
		return tier, nil
	case "":
		return "", fmt.Errorf("runtimecapability: isolation_tier is required")
	default:
		return "", fmt.Errorf("runtimecapability: unsupported isolation_tier %q", value)
	}
}

func parseCredentialMode(value string) (CredentialMode, error) {
	mode := CredentialMode(strings.TrimSpace(value))
	switch mode {
	case CredentialNone, CredentialBrokerScoped, CredentialOperatorApproved:
		return mode, nil
	case "":
		return "", fmt.Errorf("runtimecapability: credential_modes contains empty value")
	default:
		return "", fmt.Errorf("runtimecapability: unsupported credential_mode %q", value)
	}
}

func normalizedCredentialModes(values []CredentialMode) ([]CredentialMode, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("runtimecapability: credential_modes is required")
	}
	seen := make(map[CredentialMode]struct{}, len(values))
	out := make([]CredentialMode, 0, len(values))
	for _, mode := range values {
		parsed, err := parseCredentialMode(string(mode))
		if err != nil {
			return nil, err
		}
		if _, ok := seen[parsed]; ok {
			return nil, fmt.Errorf("runtimecapability: credential_modes contains duplicate value %q", parsed)
		}
		seen[parsed] = struct{}{}
		out = append(out, parsed)
	}
	sort.Slice(out, func(i, j int) bool {
		return string(out[i]) < string(out[j])
	})
	return out, nil
}

func normalizedList(field string, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("runtimecapability: %s is required", field)
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for i, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil, fmt.Errorf("runtimecapability: %s[%d] is empty", field, i)
		}
		if _, ok := seen[value]; ok {
			return nil, fmt.Errorf("runtimecapability: %s contains duplicate value %q", field, value)
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Compare(out[i], out[j]) < 0
	})
	return out, nil
}

func requireText(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("runtimecapability: %s is required", field)
	}
	if strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("runtimecapability: %s must not contain control characters", field)
	}
	return nil
}

func requireFingerprint(field, value string) error {
	if !sha256Fingerprint.MatchString(value) {
		return fmt.Errorf("runtimecapability: %s must be a lowercase sha256 fingerprint", field)
	}
	return nil
}

func fieldHasFingerprintSuffix(field string) bool {
	return strings.HasSuffix(field, "_fingerprint")
}

func parseTime(field, value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, fmt.Errorf("runtimecapability: %s is required", field)
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("runtimecapability: %s must be RFC3339: %w", field, err)
	}
	return parsed.UTC(), nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing struct{}
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("runtimecapability: parse trailing JSON: %w", err)
	}
	return fmt.Errorf("runtimecapability: trailing JSON value")
}

func parseSignature(value string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("runtimecapability: declaration is unsigned")
	}
	const prefix = "hmac-sha256:"
	if !strings.HasPrefix(value, prefix) {
		return nil, fmt.Errorf("runtimecapability: signature must use hmac-sha256")
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return nil, fmt.Errorf("runtimecapability: signature is not hex: %w", err)
	}
	if len(raw) != sha256.Size {
		return nil, fmt.Errorf("runtimecapability: signature has invalid length")
	}
	return raw, nil
}

func appendStringField(out []byte, tag byte, value string) []byte {
	return appendField(out, tag, tlvUTF8String, []byte(value))
}

func appendFingerprintField(out []byte, tag byte, value string) []byte {
	return appendField(out, tag, tlvSHA256Fingerprint, []byte(value))
}

func appendU64Field(out []byte, tag byte, value uint64) []byte {
	var buf [8]byte
	for i := 7; i >= 0; i-- {
		buf[i] = byte(value)
		value >>= 8
	}
	return appendField(out, tag, tlvU64BE, buf[:])
}

func appendStringListField(out []byte, tag byte, values []string) []byte {
	var payload []byte
	payload = appendU32(payload, uint32(len(values)))
	for _, value := range values {
		payload = appendLenBytes(payload, []byte(value))
	}
	return appendField(out, tag, tlvSortedUTF8List, payload)
}

func appendCredentialModeListField(out []byte, tag byte, values []CredentialMode) []byte {
	var payload []byte
	payload = appendU32(payload, uint32(len(values)))
	for _, value := range values {
		payload = appendLenBytes(payload, []byte(value))
	}
	return appendField(out, tag, tlvSortedUTF8List, payload)
}

func appendField(out []byte, tag byte, kind byte, value []byte) []byte {
	out = append(out, tag, kind)
	return appendLenBytes(out, value)
}

func appendLenBytes(out []byte, value []byte) []byte {
	out = appendU32(out, uint32(len(value)))
	out = append(out, value...)
	return out
}

func appendU16(out []byte, value uint16) []byte {
	return append(out, byte(value>>8), byte(value))
}

func appendU32(out []byte, value uint32) []byte {
	return append(out, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}

func fingerprint(preimage []byte) string {
	digest := sha256.Sum256(preimage)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func credentialModeStrings(values []CredentialMode) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}
