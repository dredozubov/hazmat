package runtimecapability

type DataClass string

const (
	ClassPublicDiagnostic    DataClass = "public-diagnostic"
	ClassOperatorPrivate     DataClass = "operator-private"
	ClassControlPlanePrivate DataClass = "control-plane-private"
	ClassSecretAdjacent      DataClass = "secret-adjacent"
)

type RoutingUse string

const (
	RoutingAuditOnly          RoutingUse = "audit-only"
	RoutingVerifiedCapability RoutingUse = "verified-capability"
	RoutingConformanceGate    RoutingUse = "verified-conformance-gate"
	RoutingLifecycleGate      RoutingUse = "verified-lifecycle-gate"
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
	capabilityPayloadRecord  = PayloadSchema
	declarationRecord        = DeclarationSchema
	coverageEntryRecord      = CoverageCatalogSchema + ".entry"
	artifactDigestRecord     = VerifierResultSchema + ".artifact"
	verifierResultRecord     = VerifierResultSchema
	revocationCoverageRecord = RevocationFeedSchema + ".coverage"
	revocationRecordRecord   = RevocationFeedSchema + ".record"
	revocationFeedRecord     = RevocationFeedSchema
	lifecycleDecisionRecord  = RevocationFeedSchema + ".lifecycle_decision"
)

var fieldClassifications = []FieldClassification{
	{Record: capabilityPayloadRecord, Field: "schema", Class: ClassPublicDiagnostic, Routing: RoutingAuditOnly},
	{Record: capabilityPayloadRecord, Field: "capability_set_id", Class: ClassControlPlanePrivate, Routing: RoutingVerifiedCapability},
	{Record: capabilityPayloadRecord, Field: "backend_id", Class: ClassOperatorPrivate, Routing: RoutingVerifiedCapability},
	{Record: capabilityPayloadRecord, Field: "backend_kind", Class: ClassPublicDiagnostic, Routing: RoutingVerifiedCapability},
	{Record: capabilityPayloadRecord, Field: "backend_version", Class: ClassPublicDiagnostic, Routing: RoutingVerifiedCapability},
	{Record: capabilityPayloadRecord, Field: "isolation_tier", Class: ClassPublicDiagnostic, Routing: RoutingVerifiedCapability},
	{Record: capabilityPayloadRecord, Field: "workspace_grant_patterns", Class: ClassOperatorPrivate, Routing: RoutingVerifiedCapability},
	{Record: capabilityPayloadRecord, Field: "network_grant_patterns", Class: ClassOperatorPrivate, Routing: RoutingVerifiedCapability},
	{Record: capabilityPayloadRecord, Field: "credential_modes", Class: ClassSecretAdjacent, Routing: RoutingVerifiedCapability},
	{Record: capabilityPayloadRecord, Field: "service_grant_patterns", Class: ClassOperatorPrivate, Routing: RoutingVerifiedCapability},
	{Record: capabilityPayloadRecord, Field: "conformance_result_fingerprint", Class: ClassControlPlanePrivate, Routing: RoutingConformanceGate},
	{Record: capabilityPayloadRecord, Field: "coverage_catalog_fingerprint", Class: ClassControlPlanePrivate, Routing: RoutingConformanceGate},
	{Record: capabilityPayloadRecord, Field: "revocation_feed_fingerprint", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: capabilityPayloadRecord, Field: "signer_trust_root", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: capabilityPayloadRecord, Field: "trust_root_epoch", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: capabilityPayloadRecord, Field: "declaration_nonce", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: capabilityPayloadRecord, Field: "valid_after", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: capabilityPayloadRecord, Field: "valid_until", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},

	{Record: declarationRecord, Field: "schema", Class: ClassPublicDiagnostic, Routing: RoutingAuditOnly},
	{Record: declarationRecord, Field: "capability", Class: ClassControlPlanePrivate, Routing: RoutingVerifiedCapability},
	{Record: declarationRecord, Field: "capability_set_fingerprint", Class: ClassControlPlanePrivate, Routing: RoutingVerifiedCapability},
	{Record: declarationRecord, Field: "backend_version", Class: ClassPublicDiagnostic, Routing: RoutingVerifiedCapability},
	{Record: declarationRecord, Field: "backend_code_revision", Class: ClassOperatorPrivate, Routing: RoutingConformanceGate},
	{Record: declarationRecord, Field: "isolation_tier", Class: ClassPublicDiagnostic, Routing: RoutingVerifiedCapability},
	{Record: declarationRecord, Field: "attestation_tier", Class: ClassPublicDiagnostic, Routing: RoutingAuditOnly},
	{Record: declarationRecord, Field: "valid_from", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: declarationRecord, Field: "valid_until", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: declarationRecord, Field: "reattest_after", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: declarationRecord, Field: "revocation_feed_ref", Class: ClassControlPlanePrivate, Routing: RoutingAuditOnly},
	{Record: declarationRecord, Field: "revocation_feed_fingerprint", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: declarationRecord, Field: "signer_trust_root", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: declarationRecord, Field: "trust_root_epoch", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: declarationRecord, Field: "signature", Class: ClassControlPlanePrivate, Routing: RoutingConformanceGate},

	{Record: coverageEntryRecord, Field: "capability_flag", Class: ClassPublicDiagnostic, Routing: RoutingConformanceGate},
	{Record: coverageEntryRecord, Field: "backend_version", Class: ClassPublicDiagnostic, Routing: RoutingConformanceGate},
	{Record: coverageEntryRecord, Field: "backend_code_revision", Class: ClassOperatorPrivate, Routing: RoutingConformanceGate},
	{Record: coverageEntryRecord, Field: "artifact_name", Class: ClassOperatorPrivate, Routing: RoutingConformanceGate},
	{Record: coverageEntryRecord, Field: "artifact_hash", Class: ClassControlPlanePrivate, Routing: RoutingConformanceGate},
	{Record: coverageEntryRecord, Field: "verifier_identity", Class: ClassControlPlanePrivate, Routing: RoutingConformanceGate},
	{Record: coverageEntryRecord, Field: "verifier_result_hash", Class: ClassControlPlanePrivate, Routing: RoutingConformanceGate},
	{Record: coverageEntryRecord, Field: "obligation_name", Class: ClassPublicDiagnostic, Routing: RoutingConformanceGate},

	{Record: artifactDigestRecord, Field: "name", Class: ClassOperatorPrivate, Routing: RoutingConformanceGate},
	{Record: artifactDigestRecord, Field: "hash", Class: ClassControlPlanePrivate, Routing: RoutingConformanceGate},

	{Record: verifierResultRecord, Field: "schema", Class: ClassPublicDiagnostic, Routing: RoutingAuditOnly},
	{Record: verifierResultRecord, Field: "verifier_identity", Class: ClassControlPlanePrivate, Routing: RoutingConformanceGate},
	{Record: verifierResultRecord, Field: "backend_version", Class: ClassPublicDiagnostic, Routing: RoutingConformanceGate},
	{Record: verifierResultRecord, Field: "backend_code_revision", Class: ClassOperatorPrivate, Routing: RoutingConformanceGate},
	{Record: verifierResultRecord, Field: "artifacts", Class: ClassOperatorPrivate, Routing: RoutingConformanceGate},
	{Record: verifierResultRecord, Field: "obligations", Class: ClassPublicDiagnostic, Routing: RoutingConformanceGate},
	{Record: verifierResultRecord, Field: "issued_at", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: verifierResultRecord, Field: "valid_until", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: verifierResultRecord, Field: "passed", Class: ClassPublicDiagnostic, Routing: RoutingConformanceGate},
	{Record: verifierResultRecord, Field: "signature", Class: ClassControlPlanePrivate, Routing: RoutingConformanceGate},

	{Record: revocationCoverageRecord, Field: "signer_trust_root", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: revocationCoverageRecord, Field: "backend_family", Class: ClassOperatorPrivate, Routing: RoutingLifecycleGate},

	{Record: revocationRecordRecord, Field: "capability_set_id", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: revocationRecordRecord, Field: "state", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: revocationRecordRecord, Field: "reason", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},

	{Record: revocationFeedRecord, Field: "schema", Class: ClassPublicDiagnostic, Routing: RoutingAuditOnly},
	{Record: revocationFeedRecord, Field: "feed_id", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: revocationFeedRecord, Field: "coverage", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: revocationFeedRecord, Field: "records", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: revocationFeedRecord, Field: "issued_at", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: revocationFeedRecord, Field: "valid_until", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: revocationFeedRecord, Field: "signature", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},

	{Record: lifecycleDecisionRecord, Field: "state", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: lifecycleDecisionRecord, Field: "dispatch", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: lifecycleDecisionRecord, Field: "reason", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
	{Record: lifecycleDecisionRecord, Field: "coverage", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: lifecycleDecisionRecord, Field: "feed_id", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: lifecycleDecisionRecord, Field: "feed_hash", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: lifecycleDecisionRecord, Field: "capability_set_fingerprint", Class: ClassControlPlanePrivate, Routing: RoutingLifecycleGate},
	{Record: lifecycleDecisionRecord, Field: "reattest_at", Class: ClassPublicDiagnostic, Routing: RoutingLifecycleGate},
}
