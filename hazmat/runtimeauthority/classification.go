package runtimeauthority

type DataClass string

const (
	ClassPublicDiagnostic    DataClass = "public-diagnostic"
	ClassOperatorPrivate     DataClass = "operator-private"
	ClassControlPlanePrivate DataClass = "control-plane-private"
	ClassSecretAdjacent      DataClass = "secret-adjacent"
)

type RoutingUse string

const (
	RoutingAuditOnly            RoutingUse = "audit-only"
	RoutingPreviewCompatibility RoutingUse = "preview-compatibility"
	RoutingVerifiedCapability   RoutingUse = "verified-capability-only"
)

type FieldClassification struct {
	Record  string
	Field   string
	Class   DataClass
	Routing RoutingUse
}

func FieldClassifications() []FieldClassification {
	out := make([]FieldClassification, len(fieldClassifications))
	copy(out, fieldClassifications)
	return out
}

func ClassificationFor(record, field string) (FieldClassification, bool) {
	for _, classification := range fieldClassifications {
		if classification.Record == record && classification.Field == field {
			return classification, true
		}
	}
	return FieldClassification{}, false
}

const (
	requestRecord            = Schema + ".request"
	previewRecord            = Schema + ".preview"
	pathGrantPreviewRecord   = Schema + ".preview.path_grant"
	sessionHomePreviewRecord = Schema + ".preview.session_home"
	unsupportedFieldRecord   = Schema + ".preview.unsupported_field"
)

var fieldClassifications = []FieldClassification{
	{Record: requestRecord, Field: "schema", Class: ClassPublicDiagnostic, Routing: RoutingAuditOnly},
	{Record: requestRecord, Field: "authority_id", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: requestRecord, Field: "route_id", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: requestRecord, Field: "source_project", Class: ClassOperatorPrivate, Routing: RoutingAuditOnly},
	{Record: requestRecord, Field: "target_project", Class: ClassOperatorPrivate, Routing: RoutingAuditOnly},
	{Record: requestRecord, Field: "requested_isolation_tier", Class: ClassPublicDiagnostic, Routing: RoutingPreviewCompatibility},
	{Record: requestRecord, Field: "workspace_grants", Class: ClassOperatorPrivate, Routing: RoutingPreviewCompatibility},
	{Record: requestRecord, Field: "network_grants", Class: ClassOperatorPrivate, Routing: RoutingPreviewCompatibility},
	{Record: requestRecord, Field: "credential_scope_refs", Class: ClassSecretAdjacent, Routing: RoutingPreviewCompatibility},
	{Record: requestRecord, Field: "service_grants", Class: ClassOperatorPrivate, Routing: RoutingPreviewCompatibility},
	{Record: requestRecord, Field: "broker_grant_refs", Class: ClassSecretAdjacent, Routing: RoutingPreviewCompatibility},
	{Record: requestRecord, Field: "required_capability_set_fingerprint", Class: ClassControlPlanePrivate, Routing: RoutingVerifiedCapability},
	{Record: requestRecord, Field: "policy_hash", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: requestRecord, Field: "nonce_namespace", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: requestRecord, Field: "restore_epoch", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: requestRecord, Field: "trust_root_epoch", Class: ClassControlPlanePrivate, Routing: RoutingVerifiedCapability},

	{Record: previewRecord, Field: "schema", Class: ClassPublicDiagnostic, Routing: RoutingAuditOnly},
	{Record: previewRecord, Field: "authority_id", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: previewRecord, Field: "route_id", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: previewRecord, Field: "source_project", Class: ClassOperatorPrivate, Routing: RoutingAuditOnly},
	{Record: previewRecord, Field: "target_project", Class: ClassOperatorPrivate, Routing: RoutingAuditOnly},
	{Record: previewRecord, Field: "requested_isolation_tier", Class: ClassPublicDiagnostic, Routing: RoutingPreviewCompatibility},
	{Record: previewRecord, Field: "workspace_grants", Class: ClassOperatorPrivate, Routing: RoutingPreviewCompatibility},
	{Record: previewRecord, Field: "path_grants", Class: ClassOperatorPrivate, Routing: RoutingPreviewCompatibility},
	{Record: previewRecord, Field: "network_posture", Class: ClassPublicDiagnostic, Routing: RoutingPreviewCompatibility},
	{Record: previewRecord, Field: "hazmat_network_mode", Class: ClassPublicDiagnostic, Routing: RoutingPreviewCompatibility},
	{Record: previewRecord, Field: "credential_mode", Class: ClassSecretAdjacent, Routing: RoutingPreviewCompatibility},
	{Record: previewRecord, Field: "service_grants", Class: ClassOperatorPrivate, Routing: RoutingPreviewCompatibility},
	{Record: previewRecord, Field: "logical_resources", Class: ClassOperatorPrivate, Routing: RoutingPreviewCompatibility},
	{Record: previewRecord, Field: "session_home", Class: ClassOperatorPrivate, Routing: RoutingPreviewCompatibility},
	{Record: previewRecord, Field: "required_capability_set_fingerprint", Class: ClassControlPlanePrivate, Routing: RoutingVerifiedCapability},
	{Record: previewRecord, Field: "policy_hash", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: previewRecord, Field: "nonce_namespace", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: previewRecord, Field: "restore_epoch", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: previewRecord, Field: "trust_root_epoch", Class: ClassControlPlanePrivate, Routing: RoutingVerifiedCapability},
	{Record: previewRecord, Field: "unsupported_fields", Class: ClassPublicDiagnostic, Routing: RoutingAuditOnly},

	{Record: pathGrantPreviewRecord, Field: "path", Class: ClassOperatorPrivate, Routing: RoutingPreviewCompatibility},
	{Record: pathGrantPreviewRecord, Field: "access", Class: ClassPublicDiagnostic, Routing: RoutingPreviewCompatibility},
	{Record: pathGrantPreviewRecord, Field: "source", Class: ClassOperatorPrivate, Routing: RoutingPreviewCompatibility},

	{Record: sessionHomePreviewRecord, Field: "mode", Class: ClassPublicDiagnostic, Routing: RoutingPreviewCompatibility},
	{Record: sessionHomePreviewRecord, Field: "persistent_home", Class: ClassOperatorPrivate, Routing: RoutingAuditOnly},
	{Record: sessionHomePreviewRecord, Field: "durable_bridge_roots", Class: ClassOperatorPrivate, Routing: RoutingAuditOnly},

	{Record: unsupportedFieldRecord, Field: "field", Class: ClassPublicDiagnostic, Routing: RoutingAuditOnly},
	{Record: unsupportedFieldRecord, Field: "value", Class: ClassOperatorPrivate, Routing: RoutingAuditOnly},
	{Record: unsupportedFieldRecord, Field: "reason", Class: ClassPublicDiagnostic, Routing: RoutingAuditOnly},
}
