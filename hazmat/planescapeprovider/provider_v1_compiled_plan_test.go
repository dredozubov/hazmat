package planescapeprovider

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

func TestProviderV1CompiledContainmentPlanReplaysAndValidatesBindings(
	t *testing.T,
) {
	vectors := loadReleasedProviderV1Vectors(t)
	codec := ProviderV1FrameCodec{}
	planVector := releasedProviderV1RecordByKind(
		t,
		vectors,
		providerV1KindCompiledPlan,
	)
	plan, err := codec.DecodeCompiledContainmentPlan([]byte(planVector.WireJSON))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := codec.EncodeCompiledContainmentPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(replayed, []byte(planVector.WireJSON)) {
		t.Fatal("compiled containment plan replay differs from Rust vector")
	}

	capabilities := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindCapabilities),
	).value.(ProviderCapabilities)
	admission := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindAdmission),
	).value.(SessionAdmission)
	operation := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindOperation),
	).value.(AgentOperation)
	if err := plan.ValidateProvider(capabilities); err != nil {
		t.Fatal(err)
	}
	if err := plan.ValidateSessionAdmission(admission); err != nil {
		t.Fatal(err)
	}
	if err := plan.ValidateAgentOperation(admission, operation); err != nil {
		t.Fatal(err)
	}
	if plan.Requirement().CanonicalHash() != admission.RequirementHash() ||
		plan.CanonicalHash() != admission.CompiledPlanHash() ||
		plan.ProviderCapabilityHash() != admission.SessionCapabilityHash() {
		t.Fatal("compiled plan accessors do not expose exact admission bindings")
	}
	deadline, ok := plan.DeadlineAt()
	if !ok || !deadline.Equal(admission.ExpiresAt()) {
		t.Fatal("compiled plan deadline does not bind admission expiry")
	}

	wrongHash, err := ParseFingerprint(fingerprint("f"))
	if err != nil {
		t.Fatal(err)
	}
	wrongAdmission := admission
	wrongAdmission.sessionCapabilityHash = wrongHash
	if err := plan.ValidateSessionAdmission(wrongAdmission); !errors.Is(err, errProviderV1Frame) {
		t.Fatalf("session-capability mismatch error = %v, want rejection", err)
	}
	wrongAdmission = admission
	wrongAdmission.expiresAt = wrongAdmission.expiresAt.Add(1)
	if err := plan.ValidateSessionAdmission(wrongAdmission); !errors.Is(err, errProviderV1Frame) {
		t.Fatalf("deadline mismatch error = %v, want rejection", err)
	}
	wrongProvider := capabilities
	wrongProvider.capabilityHash = wrongHash
	if err := plan.ValidateProvider(wrongProvider); !errors.Is(err, errProviderV1Frame) {
		t.Fatalf("provider-capability mismatch error = %v, want rejection", err)
	}

	requirementJSON := plan.RequirementRecordJSON()
	requirementJSON[0] ^= 0xff
	if bytes.Equal(requirementJSON, plan.RequirementRecordJSON()) {
		t.Fatal("requirement record accessor aliases compiled plan storage")
	}
	containmentJSON := plan.ContainmentRequestJSON()
	containmentJSON[0] ^= 0xff
	if bytes.Equal(containmentJSON, plan.ContainmentRequestJSON()) {
		t.Fatal("containment request accessor aliases compiled plan storage")
	}

	if _, err := codec.EncodeCompiledContainmentPlan(CompiledContainmentPlan{}); !errors.Is(err, errProviderV1Frame) {
		t.Fatalf("zero compiled plan encode error = %v, want rejection", err)
	}
}

func TestProviderV1CompiledContainmentPlanRejectsCarrierMutations(
	t *testing.T,
) {
	vectors := loadReleasedProviderV1Vectors(t)
	vector := releasedProviderV1RecordByKind(
		t,
		vectors,
		providerV1KindCompiledPlan,
	)

	mutations := map[string]func(*providerV1CompiledContainmentPlanDTO){
		"padded requirement base64url": func(dto *providerV1CompiledContainmentPlanDTO) {
			dto.RequirementRecordBase64URL += "="
		},
		"noncanonical embedded requirement": func(dto *providerV1CompiledContainmentPlanDTO) {
			dto.RequirementRecordBase64URL = reorderEmbeddedProviderV1JSON(
				t,
				dto.RequirementRecordBase64URL,
			)
		},
		"requirement hash binding": func(dto *providerV1CompiledContainmentPlanDTO) {
			dto.RequirementHash = fingerprint("f")
		},
		"embedded requirement hash": func(dto *providerV1CompiledContainmentPlanDTO) {
			var requirement providerV1RequirementDTO
			decodeEmbeddedProviderV1JSON(t, dto.RequirementRecordBase64URL, &requirement)
			requirement.CanonicalHash = fingerprint("f")
			dto.RequirementRecordBase64URL = encodeEmbeddedProviderV1JSON(t, requirement)
			dto.RequirementHash = fingerprint("f")
		},
		"authority hash binding": func(dto *providerV1CompiledContainmentPlanDTO) {
			dto.AuthorityHash = fingerprint("f")
		},
		"evidence profile binding": func(dto *providerV1CompiledContainmentPlanDTO) {
			dto.EvidenceProfileHash = fingerprint("f")
		},
		"provider epoch binding": func(dto *providerV1CompiledContainmentPlanDTO) {
			dto.ProviderEpoch = 0
		},
		"profile relation": func(dto *providerV1CompiledContainmentPlanDTO) {
			dto.RequiredProfileID = string(ProfileStockLinux)
			dto.ProviderProfileID = string(ProfilePortable)
		},
		"padded containment base64url": func(dto *providerV1CompiledContainmentPlanDTO) {
			dto.ContainmentRequestBase64URL += "="
		},
		"noncanonical embedded containment request": func(dto *providerV1CompiledContainmentPlanDTO) {
			dto.ContainmentRequestBase64URL = reorderEmbeddedProviderV1JSON(
				t,
				dto.ContainmentRequestBase64URL,
			)
		},
		"containment request hash binding": func(dto *providerV1CompiledContainmentPlanDTO) {
			dto.ContainmentRequestHash = fingerprint("f")
		},
		"embedded containment request hash": func(dto *providerV1CompiledContainmentPlanDTO) {
			var request providerV1ContainmentLeaseRequestDTO
			decodeEmbeddedProviderV1JSON(t, dto.ContainmentRequestBase64URL, &request)
			request.RequestSHA256 = fingerprint("f")
			dto.ContainmentRequestBase64URL = encodeEmbeddedProviderV1JSON(t, request)
			dto.ContainmentRequestHash = fingerprint("f")
		},
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			frame := mutateReleasedProviderV1CompiledPlan(t, vector, mutate)
			if _, err := (ProviderV1FrameCodec{}).DecodeCompiledContainmentPlan(frame); !errors.Is(err, errProviderV1Frame) {
				t.Fatalf("decode error = %v, want stable carrier rejection", err)
			}
		})
	}
}

func mutateReleasedProviderV1CompiledPlan(
	t *testing.T,
	vector releasedProviderV1Record,
	mutate func(*providerV1CompiledContainmentPlanDTO),
) []byte {
	t.Helper()
	var dto providerV1CompiledContainmentPlanDTO
	if err := json.Unmarshal([]byte(vector.WireJSON), &dto); err != nil {
		t.Fatal(err)
	}
	mutate(&dto)
	preimage, err := dto.canonicalPreimage()
	if err != nil {
		t.Fatal(err)
	}
	dto.CanonicalHash = providerV1CanonicalHash(preimage)
	frame, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func reorderEmbeddedProviderV1JSON(t *testing.T, encoded string) string {
	t.Helper()
	record, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(record, &object); err != nil {
		t.Fatal(err)
	}
	reordered, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(record, reordered) {
		t.Fatal("test mutation did not change embedded JSON member order")
	}
	return base64.RawURLEncoding.EncodeToString(reordered)
}

func decodeEmbeddedProviderV1JSON(t *testing.T, encoded string, target any) {
	t.Helper()
	record, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(record, target); err != nil {
		t.Fatal(err)
	}
}

func encodeEmbeddedProviderV1JSON(t *testing.T, value any) string {
	t.Helper()
	record, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(record)
}
