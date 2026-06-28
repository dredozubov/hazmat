package runtimecapability

import (
	"reflect"
	"strings"
	"testing"
)

func TestFieldClassificationsCoverRuntimeCapabilityJSON(t *testing.T) {
	assertClassifiedJSONFields(t, capabilityPayloadRecord, CapabilityPayload{})
	assertClassifiedJSONFields(t, declarationRecord, Declaration{})
	assertClassifiedJSONFields(t, coverageEntryRecord, CoverageEntry{})
	assertClassifiedJSONFields(t, artifactDigestRecord, ArtifactDigest{})
	assertClassifiedJSONFields(t, verifierResultRecord, SignedVerifierResult{})
	assertClassifiedJSONFields(t, revocationCoverageRecord, RevocationCoverage{})
	assertClassifiedJSONFields(t, revocationRecordRecord, RevocationRecord{})
	assertClassifiedJSONFields(t, revocationFeedRecord, SignedRevocationFeed{})
	assertClassifiedJSONFields(t, lifecycleDecisionRecord, LifecycleDecision{})
}

func TestRuntimeCapabilityClassificationSeparatesAuditAndRoutingFields(t *testing.T) {
	cases := []struct {
		record  string
		field   string
		class   DataClass
		routing RoutingUse
	}{
		{record: capabilityPayloadRecord, field: "credential_modes", class: ClassSecretAdjacent, routing: RoutingVerifiedCapability},
		{record: capabilityPayloadRecord, field: "declaration_nonce", class: ClassControlPlanePrivate, routing: RoutingAuditOnly},
		{record: declarationRecord, field: "signature", class: ClassControlPlanePrivate, routing: RoutingConformanceGate},
		{record: revocationFeedRecord, field: "records", class: ClassControlPlanePrivate, routing: RoutingLifecycleGate},
		{record: lifecycleDecisionRecord, field: "dispatch", class: ClassPublicDiagnostic, routing: RoutingLifecycleGate},
	}
	for _, tc := range cases {
		t.Run(tc.record+"/"+tc.field, func(t *testing.T) {
			classification, ok := ClassificationFor(tc.record, tc.field)
			if !ok {
				t.Fatalf("missing classification for %s.%s", tc.record, tc.field)
			}
			if classification.Class != tc.class || classification.Routing != tc.routing {
				t.Fatalf("classification = %+v, want class %q routing %q", classification, tc.class, tc.routing)
			}
		})
	}
}

func assertClassifiedJSONFields(t *testing.T, record string, value any) {
	t.Helper()
	for _, field := range jsonFields(t, value) {
		if _, ok := ClassificationFor(record, field); !ok {
			t.Fatalf("missing classification for %s.%s", record, field)
		}
	}
}

func jsonFields(t *testing.T, value any) []string {
	t.Helper()
	typ := reflect.TypeOf(value)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		t.Fatalf("%T is not a struct", value)
	}
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			fields = append(fields, name)
		}
	}
	return fields
}
