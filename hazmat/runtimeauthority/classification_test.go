package runtimeauthority

import (
	"reflect"
	"strings"
	"testing"
)

func TestFieldClassificationsCoverRuntimeAuthorityJSON(t *testing.T) {
	assertClassifiedJSONFields(t, requestRecord, requestDTO{})
	assertClassifiedJSONFields(t, previewRecord, Preview{})
	assertClassifiedJSONFields(t, pathGrantPreviewRecord, PathGrantPreview{})
	assertClassifiedJSONFields(t, sessionHomePreviewRecord, SessionHomePreview{})
	assertClassifiedJSONFields(t, unsupportedFieldRecord, UnsupportedField{})
}

func TestRuntimeAuthorityClassificationSeparatesAuditPreviewAndVerifiedRouting(t *testing.T) {
	cases := []struct {
		record  string
		field   string
		class   DataClass
		routing RoutingUse
	}{
		{record: requestRecord, field: "credential_scope_refs", class: ClassSecretAdjacent, routing: RoutingPreviewCompatibility},
		{record: requestRecord, field: "broker_grant_refs", class: ClassSecretAdjacent, routing: RoutingPreviewCompatibility},
		{record: requestRecord, field: "required_capability_set_fingerprint", class: ClassControlPlanePrivate, routing: RoutingVerifiedCapability},
		{record: requestRecord, field: "authority_id", class: ClassControlPlanePrivate, routing: RoutingAuditOnly},
		{record: previewRecord, field: "unsupported_fields", class: ClassPublicDiagnostic, routing: RoutingAuditOnly},
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
