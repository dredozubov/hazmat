package runtimeauthoritytrace

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const (
	testTime = "2026-06-22T12:00:00Z"
	fpA      = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fpB      = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fpC      = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	fpD      = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	fpE      = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	fpF      = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	fp0      = "sha256:000000000000000000000000000000000000000000000000000000000000"
	fp1      = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
)

func TestFixtureTraceMatchesGeneratedRecords(t *testing.T) {
	records := fixtureRecords(t)
	generated, err := MarshalJSONL(records)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	committed, err := os.ReadFile("testdata/runtime_authority_trace.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !bytes.Equal(committed, generated) {
		t.Fatalf("committed fixture differs from generated trace\ncommitted:\n%s\ngenerated:\n%s", committed, generated)
	}
	parsed, err := ParseJSONL(committed)
	if err != nil {
		t.Fatalf("parse committed fixture: %v", err)
	}
	if len(parsed) != 7 {
		t.Fatalf("parsed %d records, want 7", len(parsed))
	}
	for i, record := range parsed {
		if record.Event.Sequence != uint64(i+1) {
			t.Fatalf("record %d sequence = %d, want %d", i, record.Event.Sequence, i+1)
		}
	}
}

func TestRejectsTamperedEventHash(t *testing.T) {
	record := fixtureRecords(t)[0]
	record.EventHash = fp0
	if err := record.Verify(); err == nil {
		t.Fatal("tampered event hash accepted")
	}
}

func TestRejectsMissingKindRequiredField(t *testing.T) {
	_, err := NewRecord(Event{
		Kind:       EventRouteFactsDerived,
		Sequence:   1,
		OccurredAt: testTime,
		RouteID:    "route-local",
		ProjectKey: "project:ops",
		SessionKey: "session:s1",
		BackendID:  "hazmat:macos-local",
	})
	if err == nil || !strings.Contains(err.Error(), "principal_key is required") {
		t.Fatalf("missing principal_key error = %v", err)
	}
}

func TestRejectsSecretLikeValues(t *testing.T) {
	_, err := NewRecord(Event{
		Kind:        EventDispatchDenied,
		Sequence:    1,
		OccurredAt:  testTime,
		AuthorityID: "password=secret",
		RouteID:     "route-local",
		PolicyHash:  fpA,
		ReasonCode:  "policy_denied",
	})
	if err == nil || !strings.Contains(err.Error(), "looks like secret material") {
		t.Fatalf("secret-looking value error = %v", err)
	}
}

func TestRejectsUnboundedFields(t *testing.T) {
	_, err := NewRecord(Event{
		Kind:                        EventAuthorityPreviewed,
		Sequence:                    1,
		OccurredAt:                  testTime,
		AuthorityID:                 strings.Repeat("a", maxFieldLen+1),
		RouteID:                     "route-local",
		RuntimeAuthorityFingerprint: fpA,
		PolicyHash:                  fpB,
	})
	if err == nil || !strings.Contains(err.Error(), "authority_id exceeds") {
		t.Fatalf("oversized field error = %v", err)
	}
}

func TestRejectsReasonTextInsteadOfMachineCode(t *testing.T) {
	_, err := NewRecord(Event{
		Kind:        EventDispatchPreconditionFailed,
		Sequence:    1,
		OccurredAt:  testTime,
		AuthorityID: "auth-local",
		RouteID:     "route-local",
		PolicyHash:  fpA,
		ReasonCode:  "needs human because this has prose",
	})
	if err == nil || !strings.Contains(err.Error(), "bounded machine code") {
		t.Fatalf("reason prose error = %v", err)
	}
}

func TestRejectsUnknownJSONFields(t *testing.T) {
	trace := []byte(`{"schema":"hazmat.runtime.authority.trace.v1","event_hash":"` + fpA + `","event":{"kind":"authority_previewed","sequence":1,"occurred_at":"2026-06-22T12:00:00Z","authority_id":"auth-local","route_id":"route-local","runtime_authority_fingerprint":"` + fpB + `","policy_hash":"` + fpC + `","secret":"nope"}}` + "\n")
	_, err := ParseJSONL(trace)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func fixtureRecords(t *testing.T) []Record {
	t.Helper()
	events := []Event{
		{
			Kind:                        EventAuthorityPreviewed,
			Sequence:                    1,
			OccurredAt:                  testTime,
			AuthorityID:                 "auth-local-1",
			RouteID:                     "route-local-1",
			RuntimeAuthorityFingerprint: fpA,
			PolicyHash:                  fpB,
		},
		{
			Kind:                     EventCapabilityDeclared,
			Sequence:                 2,
			OccurredAt:               testTime,
			BackendID:                "hazmat:macos-local",
			CapabilitySetID:          "cap-hazmat-local",
			CapabilitySetFingerprint: fpC,
		},
		{
			Kind:                         EventConformanceChecked,
			Sequence:                     3,
			OccurredAt:                   testTime,
			BackendID:                    "hazmat:macos-local",
			CapabilitySetFingerprint:     fpC,
			VerifierResultHash:           fpD,
			CoverageCatalogFingerprint:   fpE,
			ConformanceResultFingerprint: fpD,
			DispatchDisposition:          "allow_new_dispatch",
		},
		{
			Kind:                      EventRevocationChecked,
			Sequence:                  4,
			OccurredAt:                testTime,
			CapabilitySetFingerprint:  fpC,
			RevocationFeedFingerprint: fpF,
			RevocationFeedHash:        fpF,
			DispatchDisposition:       "allow_new_dispatch",
		},
		{
			Kind:         EventRouteFactsDerived,
			Sequence:     5,
			OccurredAt:   testTime,
			RouteID:      "route-local-1",
			PrincipalKey: "uid:501",
			ProjectKey:   "project:ops",
			SessionKey:   "session:hazmat-local-1",
			BackendID:    "hazmat:macos-local",
		},
		{
			Kind:        EventDispatchDenied,
			Sequence:    6,
			OccurredAt:  testTime,
			AuthorityID: "auth-local-2",
			RouteID:     "route-local-2",
			PolicyHash:  fpB,
			ReasonCode:  "risk_ceiling_exceeded",
		},
		{
			Kind:        EventDispatchPreconditionFailed,
			Sequence:    7,
			OccurredAt:  testTime,
			AuthorityID: "auth-local-3",
			RouteID:     "route-local-3",
			PolicyHash:  fpB,
			ReasonCode:  "capability_revocation_stale",
		},
	}
	records := make([]Record, 0, len(events))
	for _, event := range events {
		record, err := NewRecord(event)
		if err != nil {
			t.Fatalf("new record %s: %v", event.Kind, err)
		}
		records = append(records, record)
	}
	return records
}
