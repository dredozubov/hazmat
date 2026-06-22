// Package runtimeauthoritytrace defines stable runtime-authority trace records
// for ops-side conformance replay. It does not inspect host state or grant
// authority; callers pass already-derived ids and fingerprints.
package runtimeauthoritytrace

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	Schema       = "hazmat.runtime.authority.trace.v1"
	hashDomain   = "hazmat.runtime.authority.trace.record.v1"
	maxFieldLen  = 256
	maxReasonLen = 80
)

type EventKind string

const (
	EventAuthorityPreviewed         EventKind = "authority_previewed"
	EventCapabilityDeclared         EventKind = "capability_declared"
	EventConformanceChecked         EventKind = "conformance_checked"
	EventRevocationChecked          EventKind = "revocation_checked"
	EventRouteFactsDerived          EventKind = "route_facts_derived"
	EventDispatchDenied             EventKind = "dispatch_denied"
	EventDispatchPreconditionFailed EventKind = "dispatch_precondition_failed"
)

type Event struct {
	Kind                         EventKind `json:"kind"`
	Sequence                     uint64    `json:"sequence"`
	OccurredAt                   string    `json:"occurred_at"`
	AuthorityID                  string    `json:"authority_id,omitempty"`
	RouteID                      string    `json:"route_id,omitempty"`
	PrincipalKey                 string    `json:"principal_key,omitempty"`
	ProjectKey                   string    `json:"project_key,omitempty"`
	SessionKey                   string    `json:"session_key,omitempty"`
	BackendID                    string    `json:"backend_id,omitempty"`
	CapabilitySetID              string    `json:"capability_set_id,omitempty"`
	RuntimeAuthorityFingerprint  string    `json:"runtime_authority_fingerprint,omitempty"`
	CapabilitySetFingerprint     string    `json:"capability_set_fingerprint,omitempty"`
	PolicyHash                   string    `json:"policy_hash,omitempty"`
	VerifierResultHash           string    `json:"verifier_result_hash,omitempty"`
	CoverageCatalogFingerprint   string    `json:"coverage_catalog_fingerprint,omitempty"`
	ConformanceResultFingerprint string    `json:"conformance_result_fingerprint,omitempty"`
	RevocationFeedFingerprint    string    `json:"revocation_feed_fingerprint,omitempty"`
	RevocationFeedHash           string    `json:"revocation_feed_hash,omitempty"`
	DispatchDisposition          string    `json:"dispatch_disposition,omitempty"`
	ReasonCode                   string    `json:"reason_code,omitempty"`
}

type Record struct {
	Schema    string `json:"schema"`
	EventHash string `json:"event_hash"`
	Event     Event  `json:"event"`
}

var (
	sha256FingerprintPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	reasonCodePattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,79}$`)
	secretValuePatterns      = []*regexp.Regexp{
		regexp.MustCompile(`(?i)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
		regexp.MustCompile(`(?i)\bssh-rsa\s+[A-Za-z0-9+/=]{40,}`),
		regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
		regexp.MustCompile(`(?i)\b(xox[baprs]-|gh[pousr]_|sk-[a-z0-9])`),
		regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key)\s*[:=]`),
	}
)

func NewRecord(event Event) (Record, error) {
	normalized, err := normalizeEvent(event)
	if err != nil {
		return Record{}, err
	}
	record := Record{Schema: Schema, Event: normalized}
	record.EventHash = eventHash(normalized)
	return record, nil
}

func (r Record) Verify() error {
	if r.Schema != Schema {
		return fmt.Errorf("runtimeauthoritytrace: unsupported schema %q", r.Schema)
	}
	if !sha256FingerprintPattern.MatchString(r.EventHash) {
		return fmt.Errorf("runtimeauthoritytrace: event_hash must be sha256 fingerprint")
	}
	expected, err := NewRecord(r.Event)
	if err != nil {
		return err
	}
	if r.EventHash != expected.EventHash {
		return fmt.Errorf("runtimeauthoritytrace: event_hash mismatch")
	}
	return nil
}

func MarshalJSONL(records []Record) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	for i, record := range records {
		if err := record.Verify(); err != nil {
			return nil, fmt.Errorf("runtimeauthoritytrace: record %d: %w", i, err)
		}
		if err := encoder.Encode(record); err != nil {
			return nil, fmt.Errorf("runtimeauthoritytrace: encode record %d: %w", i, err)
		}
	}
	return buf.Bytes(), nil
}

func ParseJSONL(data []byte) ([]Record, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	var records []Record
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var record Record
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("runtimeauthoritytrace: parse line %d: %w", lineNo, err)
		}
		if err := rejectTrailingJSON(decoder); err != nil {
			return nil, fmt.Errorf("runtimeauthoritytrace: parse line %d: %w", lineNo, err)
		}
		if err := record.Verify(); err != nil {
			return nil, fmt.Errorf("runtimeauthoritytrace: line %d: %w", lineNo, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("runtimeauthoritytrace: scan jsonl: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("runtimeauthoritytrace: trace requires at least one record")
	}
	return records, nil
}

func normalizeEvent(event Event) (Event, error) {
	kind, err := parseKind(event.Kind)
	if err != nil {
		return Event{}, err
	}
	if event.Sequence == 0 {
		return Event{}, fmt.Errorf("runtimeauthoritytrace: sequence is required")
	}
	occurredAt, err := parseTime(event.OccurredAt)
	if err != nil {
		return Event{}, err
	}
	normalized := Event{
		Kind:                         kind,
		Sequence:                     event.Sequence,
		OccurredAt:                   occurredAt,
		AuthorityID:                  normalizeText(event.AuthorityID),
		RouteID:                      normalizeText(event.RouteID),
		PrincipalKey:                 normalizeText(event.PrincipalKey),
		ProjectKey:                   normalizeText(event.ProjectKey),
		SessionKey:                   normalizeText(event.SessionKey),
		BackendID:                    normalizeText(event.BackendID),
		CapabilitySetID:              normalizeText(event.CapabilitySetID),
		RuntimeAuthorityFingerprint:  normalizeText(event.RuntimeAuthorityFingerprint),
		CapabilitySetFingerprint:     normalizeText(event.CapabilitySetFingerprint),
		PolicyHash:                   normalizeText(event.PolicyHash),
		VerifierResultHash:           normalizeText(event.VerifierResultHash),
		CoverageCatalogFingerprint:   normalizeText(event.CoverageCatalogFingerprint),
		ConformanceResultFingerprint: normalizeText(event.ConformanceResultFingerprint),
		RevocationFeedFingerprint:    normalizeText(event.RevocationFeedFingerprint),
		RevocationFeedHash:           normalizeText(event.RevocationFeedHash),
		DispatchDisposition:          normalizeText(event.DispatchDisposition),
		ReasonCode:                   normalizeText(event.ReasonCode),
	}
	if err := validateBoundedFields(normalized); err != nil {
		return Event{}, err
	}
	if err := validateKindFields(normalized); err != nil {
		return Event{}, err
	}
	return normalized, nil
}

func validateKindFields(event Event) error {
	switch event.Kind {
	case EventAuthorityPreviewed:
		return requireFields(event, "authority_id", "route_id", "runtime_authority_fingerprint", "policy_hash")
	case EventCapabilityDeclared:
		return requireFields(event, "backend_id", "capability_set_id", "capability_set_fingerprint")
	case EventConformanceChecked:
		return requireFields(event, "backend_id", "capability_set_fingerprint", "verifier_result_hash", "coverage_catalog_fingerprint", "conformance_result_fingerprint", "dispatch_disposition")
	case EventRevocationChecked:
		return requireFields(event, "capability_set_fingerprint", "revocation_feed_fingerprint", "revocation_feed_hash", "dispatch_disposition")
	case EventRouteFactsDerived:
		return requireFields(event, "route_id", "principal_key", "project_key", "session_key", "backend_id")
	case EventDispatchDenied, EventDispatchPreconditionFailed:
		if err := requireFields(event, "authority_id", "route_id", "policy_hash", "reason_code"); err != nil {
			return err
		}
		if !reasonCodePattern.MatchString(event.ReasonCode) {
			return fmt.Errorf("runtimeauthoritytrace: reason_code must be a bounded machine code")
		}
		return nil
	default:
		return fmt.Errorf("runtimeauthoritytrace: unsupported event kind %q", event.Kind)
	}
}

func requireFields(event Event, names ...string) error {
	values := eventFieldMap(event)
	for _, name := range names {
		if values[name] == "" {
			return fmt.Errorf("runtimeauthoritytrace: %s is required for %s", name, event.Kind)
		}
	}
	for _, name := range fingerprintFields() {
		value := values[name]
		if value != "" && !sha256FingerprintPattern.MatchString(value) {
			return fmt.Errorf("runtimeauthoritytrace: %s must be sha256 fingerprint", name)
		}
	}
	return nil
}

func validateBoundedFields(event Event) error {
	values := eventFieldMap(event)
	for field, value := range values {
		if value == "" {
			continue
		}
		if len(value) > maxFieldLen {
			return fmt.Errorf("runtimeauthoritytrace: %s exceeds %d bytes", field, maxFieldLen)
		}
		if field == "reason_code" && len(value) > maxReasonLen {
			return fmt.Errorf("runtimeauthoritytrace: reason_code exceeds %d bytes", maxReasonLen)
		}
		for _, pattern := range secretValuePatterns {
			if pattern.MatchString(value) {
				return fmt.Errorf("runtimeauthoritytrace: %s looks like secret material", field)
			}
		}
	}
	return nil
}

func eventFieldMap(event Event) map[string]string {
	return map[string]string{
		"authority_id":                   event.AuthorityID,
		"route_id":                       event.RouteID,
		"principal_key":                  event.PrincipalKey,
		"project_key":                    event.ProjectKey,
		"session_key":                    event.SessionKey,
		"backend_id":                     event.BackendID,
		"capability_set_id":              event.CapabilitySetID,
		"runtime_authority_fingerprint":  event.RuntimeAuthorityFingerprint,
		"capability_set_fingerprint":     event.CapabilitySetFingerprint,
		"policy_hash":                    event.PolicyHash,
		"verifier_result_hash":           event.VerifierResultHash,
		"coverage_catalog_fingerprint":   event.CoverageCatalogFingerprint,
		"conformance_result_fingerprint": event.ConformanceResultFingerprint,
		"revocation_feed_fingerprint":    event.RevocationFeedFingerprint,
		"revocation_feed_hash":           event.RevocationFeedHash,
		"dispatch_disposition":           event.DispatchDisposition,
		"reason_code":                    event.ReasonCode,
	}
}

func fingerprintFields() []string {
	return []string{
		"runtime_authority_fingerprint",
		"capability_set_fingerprint",
		"policy_hash",
		"verifier_result_hash",
		"coverage_catalog_fingerprint",
		"conformance_result_fingerprint",
		"revocation_feed_fingerprint",
		"revocation_feed_hash",
	}
}

func eventHash(event Event) string {
	values := eventFieldMap(event)
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	preimage := make([]byte, 0)
	preimage = appendLenBytes(preimage, []byte(hashDomain))
	preimage = appendLenBytes(preimage, []byte(event.Kind))
	preimage = appendU64(preimage, event.Sequence)
	preimage = appendLenBytes(preimage, []byte(event.OccurredAt))
	for _, key := range keys {
		preimage = appendLenBytes(preimage, []byte(key))
		preimage = appendLenBytes(preimage, []byte(values[key]))
	}
	sum := sha256.Sum256(preimage)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func parseKind(kind EventKind) (EventKind, error) {
	switch kind {
	case EventAuthorityPreviewed,
		EventCapabilityDeclared,
		EventConformanceChecked,
		EventRevocationChecked,
		EventRouteFactsDerived,
		EventDispatchDenied,
		EventDispatchPreconditionFailed:
		return kind, nil
	default:
		return "", fmt.Errorf("runtimeauthoritytrace: unsupported event kind %q", kind)
	}
}

func parseTime(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("runtimeauthoritytrace: occurred_at is required")
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("runtimeauthoritytrace: occurred_at must be RFC3339: %w", err)
	}
	return parsed.UTC().Format(time.RFC3339), nil
}

func normalizeText(value string) string {
	return strings.TrimSpace(value)
}

func appendU64(out []byte, value uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], value)
	return append(out, buf[:]...)
}

func appendLenBytes(out []byte, value []byte) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(len(value)))
	out = append(out, buf[:]...)
	return append(out, value...)
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("runtimeauthoritytrace: decode trailing json: %w", err)
	}
	return fmt.Errorf("runtimeauthoritytrace: unexpected trailing json")
}
