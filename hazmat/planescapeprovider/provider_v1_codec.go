package planescapeprovider

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"time"
	"unicode/utf8"
)

// ProviderV1FrameCodec is the strict bounded-JSON and canonical-TLV mapping
// published by planescape.provider.v1. It has no transport behavior.
type ProviderV1FrameCodec struct{}

var _ FrameCodec = ProviderV1FrameCodec{}

type providerV1Discovery struct{}

type decodedProviderV1Record struct {
	kind              string
	value             any
	canonicalPreimage []byte
}

func (ProviderV1FrameCodec) EncodeDiscovery() ([]byte, error) {
	dto := providerV1DiscoveryDTO{
		Schema:          providerV1SchemaDiscovery,
		ProtocolVersion: ProtocolVersionV1,
	}
	preimage, err := dto.canonicalPreimage()
	if err != nil {
		return nil, errProviderV1Frame
	}
	dto.CanonicalHash = providerV1CanonicalHash(preimage)
	return encodeProviderV1DTO(providerV1KindDiscovery, dto)
}

func (ProviderV1FrameCodec) DecodeCapabilities(frame []byte) (ProviderCapabilities, error) {
	record, err := decodeProviderV1Expected(frame, providerV1KindCapabilities)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	value, ok := record.value.(ProviderCapabilities)
	if !ok {
		return ProviderCapabilities{}, errProviderV1Frame
	}
	return value, nil
}

func (ProviderV1FrameCodec) EncodeAdmission(
	requirement ExecutionRequirement,
) ([]byte, error) {
	dto := providerV1RequirementDTO{
		Schema:                     providerV1SchemaRequirement,
		RequirementID:              requirement.RequirementID().String(),
		ControllerAttemptID:        requirement.ControllerAttemptID().String(),
		AuthorityHash:              requirement.AuthorityHash().String(),
		RequiredCapabilities:       providerV1CapabilityStrings(requirement.RequiredCapabilities()),
		RequiredResourceDimensions: providerV1ResourceStrings(requirement.RequiredResourceDimensions()),
		EvidenceProfileHash:        requirement.EvidenceProfileHash().String(),
		CanonicalHash:              requirement.CanonicalHash().String(),
	}
	return encodeProviderV1DTO(providerV1KindRequirement, dto)
}

func (ProviderV1FrameCodec) DecodeAdmission(frame []byte) (SessionAdmission, error) {
	record, err := decodeProviderV1Expected(frame, providerV1KindAdmission)
	if err != nil {
		return SessionAdmission{}, err
	}
	value, ok := record.value.(SessionAdmission)
	if !ok {
		return SessionAdmission{}, errProviderV1Frame
	}
	return value, nil
}

func (ProviderV1FrameCodec) EncodeOperation(
	operation AgentOperation,
) ([]byte, error) {
	dto := providerV1OperationDTO{
		Schema:            providerV1SchemaOperation,
		SessionID:         operation.SessionID().String(),
		OperationID:       operation.OperationID().String(),
		OperationSequence: operation.Sequence().Uint64(),
		OperationKind:     string(operation.Kind()),
		PlanHash:          operation.PlanHash().String(),
		Nonce:             operation.Nonce().String(),
		PayloadHash:       operation.PayloadHash().String(),
		CanonicalHash:     operation.CanonicalHash().String(),
	}
	return encodeProviderV1DTO(providerV1KindOperation, dto)
}

func (ProviderV1FrameCodec) DecodeOperation(frame []byte) (OperationResponse, error) {
	record, err := decodeProviderV1Expected(
		frame,
		providerV1KindOperationResult,
		providerV1KindQuiescence,
		providerV1KindCloseout,
	)
	if err != nil {
		return nil, err
	}
	value, ok := record.value.(OperationResponse)
	if !ok || !value.valid() {
		return nil, errProviderV1Frame
	}
	return value, nil
}

func (ProviderV1FrameCodec) EncodeFreeze(freeze Freeze) ([]byte, error) {
	dto := providerV1FreezeDTO{
		Schema:         providerV1SchemaFreeze,
		SessionID:      freeze.SessionID().String(),
		FreezeID:       freeze.FreezeID().String(),
		QuiescenceHash: freeze.QuiescenceHash().String(),
		Nonce:          freeze.Nonce().String(),
		CanonicalHash:  freeze.CanonicalHash().String(),
	}
	return encodeProviderV1DTO(providerV1KindFreeze, dto)
}

func (ProviderV1FrameCodec) DecodeFreezeAck(frame []byte) (FreezeAck, error) {
	record, err := decodeProviderV1Expected(frame, providerV1KindFreezeAck)
	if err != nil {
		return FreezeAck{}, err
	}
	value, ok := record.value.(FreezeAck)
	if !ok {
		return FreezeAck{}, errProviderV1Frame
	}
	return value, nil
}

func (ProviderV1FrameCodec) EncodeCancellation(
	cancellation Cancellation,
) ([]byte, error) {
	dto := providerV1CancellationDTO{
		Schema:         providerV1SchemaCancellation,
		SessionID:      cancellation.SessionID().String(),
		CancellationID: cancellation.CancellationID().String(),
		Reason:         cancellation.Reason(),
		Nonce:          cancellation.Nonce().String(),
		CanonicalHash:  cancellation.CanonicalHash().String(),
	}
	return encodeProviderV1DTO(providerV1KindCancellation, dto)
}

func (ProviderV1FrameCodec) DecodeCancellationAck(
	frame []byte,
) (CancellationAck, error) {
	record, err := decodeProviderV1Expected(frame, providerV1KindCancellationAck)
	if err != nil {
		return CancellationAck{}, err
	}
	value, ok := record.value.(CancellationAck)
	if !ok {
		return CancellationAck{}, errProviderV1Frame
	}
	return value, nil
}

func encodeProviderV1DTO(kind string, dto any) ([]byte, error) {
	frame, err := json.Marshal(dto)
	if err != nil || len(frame) == 0 || len(frame) > MaxRecordBytes {
		return nil, errProviderV1Frame
	}
	record, err := decodeProviderV1Record(frame)
	if err != nil || record.kind != kind {
		return nil, errProviderV1Frame
	}
	return frame, nil
}

func decodeProviderV1Expected(
	frame []byte,
	expectedKinds ...string,
) (decodedProviderV1Record, error) {
	record, err := decodeProviderV1Record(frame)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	if failure, ok := record.value.(*ProviderFailure); ok {
		return decodedProviderV1Record{}, failure
	}
	for _, kind := range expectedKinds {
		if record.kind == kind {
			return record, nil
		}
	}
	return decodedProviderV1Record{}, errProviderV1Frame
}

type providerV1JSONObject struct {
	frame  []byte
	schema string
	fields map[string]json.RawMessage
}

func parseProviderV1JSONObject(frame []byte) (providerV1JSONObject, error) {
	if len(frame) == 0 || len(frame) > MaxRecordBytes ||
		!utf8.Valid(frame) || !json.Valid(frame) {
		return providerV1JSONObject{}, errProviderV1Frame
	}

	decoder := json.NewDecoder(bytes.NewReader(frame))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return providerV1JSONObject{}, errProviderV1Frame
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !validProviderV1Text(key) {
			return providerV1JSONObject{}, errProviderV1Frame
		}
		if _, duplicate := fields[key]; duplicate {
			return providerV1JSONObject{}, errProviderV1Frame
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil ||
			bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return providerV1JSONObject{}, errProviderV1Frame
		}
		fields[key] = append(json.RawMessage(nil), raw...)
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return providerV1JSONObject{}, errProviderV1Frame
	}
	if err := requireProviderV1JSONEOF(decoder); err != nil {
		return providerV1JSONObject{}, errProviderV1Frame
	}

	rawSchema, ok := fields["schema"]
	if !ok {
		return providerV1JSONObject{}, errProviderV1Frame
	}
	var schema string
	if err := json.Unmarshal(rawSchema, &schema); err != nil ||
		!validProviderV1Text(schema) {
		return providerV1JSONObject{}, errProviderV1Frame
	}
	return providerV1JSONObject{
		frame:  append([]byte(nil), frame...),
		schema: schema,
		fields: fields,
	}, nil
}

func (o providerV1JSONObject) decode(
	schema string,
	target any,
	fieldNames ...string,
) error {
	if o.schema != schema || target == nil || len(o.fields) != len(fieldNames) {
		return errProviderV1Frame
	}
	for _, fieldName := range fieldNames {
		if _, ok := o.fields[fieldName]; !ok {
			return errProviderV1Frame
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(o.frame))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errProviderV1Frame
	}
	return requireProviderV1JSONEOF(decoder)
}

func requireProviderV1JSONEOF(decoder *json.Decoder) error {
	if decoder == nil {
		return errProviderV1Frame
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errProviderV1Frame
	}
	return nil
}

func decodeProviderV1Record(frame []byte) (decodedProviderV1Record, error) {
	object, err := parseProviderV1JSONObject(frame)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	switch object.schema {
	case providerV1SchemaDiscovery:
		return decodeProviderV1Discovery(object)
	case providerV1SchemaCapabilities:
		return decodeProviderV1Capabilities(object)
	case providerV1SchemaRequirement:
		return decodeProviderV1Requirement(object)
	case providerV1SchemaAdmission:
		return decodeProviderV1Admission(object)
	case providerV1SchemaOperation:
		return decodeProviderV1OperationRecord(object)
	case providerV1SchemaOperationResult:
		return decodeProviderV1OperationResult(object)
	case providerV1SchemaQuiescence:
		return decodeProviderV1Quiescence(object)
	case providerV1SchemaFreeze:
		return decodeProviderV1Freeze(object)
	case providerV1SchemaFreezeAck:
		return decodeProviderV1FreezeAck(object)
	case providerV1SchemaCloseout:
		return decodeProviderV1Closeout(object)
	case providerV1SchemaCancellation:
		return decodeProviderV1Cancellation(object)
	case providerV1SchemaCancellationAck:
		return decodeProviderV1CancellationAck(object)
	case providerV1SchemaError:
		return decodeProviderV1Error(object)
	default:
		return decodedProviderV1Record{}, errProviderV1Frame
	}
}

func decodeProviderV1Discovery(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1DiscoveryDTO
	if err := object.decode(
		providerV1SchemaDiscovery,
		&dto,
		"schema",
		"protocol_version",
		"canonical_hash",
	); err != nil || dto.ProtocolVersion != ProtocolVersionV1 {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindDiscovery,
		value:             providerV1Discovery{},
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1Capabilities(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1CapabilitiesDTO
	if err := object.decode(
		providerV1SchemaCapabilities,
		&dto,
		"schema",
		"provider_id",
		"provider_epoch",
		"profile_id",
		"protocol_version",
		"capabilities",
		"resource_dimensions",
		"capability_hash",
		"canonical_hash",
	); err != nil || dto.ProtocolVersion != ProtocolVersionV1 {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	capabilities, err := providerV1Capabilities(dto.Capabilities)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	resources, err := providerV1Resources(dto.ResourceDimensions)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	capabilityPreimage, err := dto.capabilitySetPreimage()
	if err != nil ||
		providerV1ValidateCanonicalHash(dto.CapabilityHash, capabilityPreimage) != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := NewProviderCapabilities(ProviderCapabilitiesInput{
		ProviderID:         dto.ProviderID,
		ProviderEpoch:      dto.ProviderEpoch,
		Profile:            Profile(dto.ProfileID),
		CapabilityHash:     dto.CapabilityHash,
		CanonicalHash:      dto.CanonicalHash,
		Capabilities:       capabilities,
		ResourceDimensions: resources,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindCapabilities,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1Requirement(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1RequirementDTO
	if err := object.decode(
		providerV1SchemaRequirement,
		&dto,
		"schema",
		"requirement_id",
		"controller_attempt_id",
		"authority_hash",
		"required_capabilities",
		"required_resource_dimensions",
		"evidence_profile_hash",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	capabilities, err := providerV1Capabilities(dto.RequiredCapabilities)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	resources, err := providerV1Resources(dto.RequiredResourceDimensions)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := NewExecutionRequirement(ExecutionRequirementInput{
		RequirementID:              dto.RequirementID,
		ControllerAttemptID:        dto.ControllerAttemptID,
		AuthorityHash:              dto.AuthorityHash,
		RequiredCapabilities:       capabilities,
		RequiredResourceDimensions: resources,
		EvidenceProfileHash:        dto.EvidenceProfileHash,
		CanonicalHash:              dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindRequirement,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1Admission(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1AdmissionDTO
	if err := object.decode(
		providerV1SchemaAdmission,
		&dto,
		"schema",
		"session_id",
		"provider_id",
		"provider_epoch",
		"requirement_hash",
		"compiled_plan_hash",
		"session_capability_hash",
		"expires_at_ms",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	expiresAt, err := providerV1Time(dto.ExpiresAtMS)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := NewSessionAdmission(SessionAdmissionInput{
		SessionID:             dto.SessionID,
		ProviderID:            dto.ProviderID,
		ProviderEpoch:         dto.ProviderEpoch,
		RequirementHash:       dto.RequirementHash,
		CompiledPlanHash:      dto.CompiledPlanHash,
		SessionCapabilityHash: dto.SessionCapabilityHash,
		ExpiresAt:             expiresAt,
		CanonicalHash:         dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindAdmission,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1OperationRecord(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1OperationDTO
	if err := object.decode(
		providerV1SchemaOperation,
		&dto,
		"schema",
		"session_id",
		"operation_id",
		"operation_sequence",
		"operation_kind",
		"plan_hash",
		"nonce",
		"payload_hash",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	sessionID, err := NewIdentifier(dto.SessionID)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	sequence, err := NewOperationSequence(dto.OperationSequence)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	planHash, err := ParseFingerprint(dto.PlanHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	input, err := NewOperationInput(OperationInputValues{
		OperationID:   dto.OperationID,
		Kind:          OperationKind(dto.OperationKind),
		Nonce:         dto.Nonce,
		PayloadHash:   dto.PayloadHash,
		CanonicalHash: dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := newAgentOperation(sessionID, sequence, planHash, input)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindOperation,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1OperationResult(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1OperationResultDTO
	if err := object.decode(
		providerV1SchemaOperationResult,
		&dto,
		"schema",
		"session_id",
		"operation_id",
		"operation_sequence",
		"result_kind",
		"artifact_hash",
		"evidence_hash",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := NewOperationResult(OperationResultInput{
		SessionID:     dto.SessionID,
		OperationID:   dto.OperationID,
		Sequence:      dto.OperationSequence,
		ResultKind:    ResultKind(dto.ResultKind),
		ArtifactHash:  dto.ArtifactHash,
		EvidenceHash:  dto.EvidenceHash,
		CanonicalHash: dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindOperationResult,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1Quiescence(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1QuiescenceDTO
	if err := object.decode(
		providerV1SchemaQuiescence,
		&dto,
		"schema",
		"session_id",
		"quiescence_hash",
		"resource_evidence_hash",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := NewQuiescence(QuiescenceInput{
		SessionID:            dto.SessionID,
		QuiescenceHash:       dto.QuiescenceHash,
		ResourceEvidenceHash: dto.ResourceEvidenceHash,
		CanonicalHash:        dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindQuiescence,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1Freeze(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1FreezeDTO
	if err := object.decode(
		providerV1SchemaFreeze,
		&dto,
		"schema",
		"session_id",
		"freeze_id",
		"quiescence_hash",
		"nonce",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	sessionID, err := NewIdentifier(dto.SessionID)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	quiescenceHash, err := ParseFingerprint(dto.QuiescenceHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	input, err := NewFreezeInput(FreezeInputValues{
		FreezeID:      dto.FreezeID,
		Nonce:         dto.Nonce,
		CanonicalHash: dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := newFreeze(sessionID, quiescenceHash, input)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindFreeze,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1FreezeAck(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1FreezeAckDTO
	if err := object.decode(
		providerV1SchemaFreezeAck,
		&dto,
		"schema",
		"session_id",
		"freeze_id",
		"quiescence_hash",
		"frozen_at_ms",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	frozenAt, err := providerV1Time(dto.FrozenAtMS)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := NewFreezeAck(FreezeAckInput{
		SessionID:      dto.SessionID,
		FreezeID:       dto.FreezeID,
		QuiescenceHash: dto.QuiescenceHash,
		FrozenAt:       frozenAt,
		CanonicalHash:  dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindFreezeAck,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1Closeout(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1CloseoutDTO
	if err := object.decode(
		providerV1SchemaCloseout,
		&dto,
		"schema",
		"session_id",
		"closeout_id",
		"terminal_outcome",
		"quiescence_hash",
		"logical_evidence_hash",
		"native_extension_hash",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := NewCloseout(CloseoutInput{
		SessionID:           dto.SessionID,
		CloseoutID:          dto.CloseoutID,
		TerminalOutcome:     TerminalOutcome(dto.TerminalOutcome),
		QuiescenceHash:      dto.QuiescenceHash,
		LogicalEvidenceHash: dto.LogicalEvidenceHash,
		NativeExtensionHash: dto.NativeExtensionHash,
		CanonicalHash:       dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindCloseout,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1Cancellation(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1CancellationDTO
	if err := object.decode(
		providerV1SchemaCancellation,
		&dto,
		"schema",
		"session_id",
		"cancellation_id",
		"reason",
		"nonce",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	sessionID, err := NewIdentifier(dto.SessionID)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	input, err := NewCancellationInput(CancellationInputValues{
		CancellationID: dto.CancellationID,
		Reason:         dto.Reason,
		Nonce:          dto.Nonce,
		CanonicalHash:  dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := newCancellation(sessionID, input)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindCancellation,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1CancellationAck(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1CancellationAckDTO
	if err := object.decode(
		providerV1SchemaCancellationAck,
		&dto,
		"schema",
		"session_id",
		"cancellation_id",
		"terminal_outcome",
		"logical_evidence_hash",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := NewCancellationAck(CancellationAckInput{
		SessionID:           dto.SessionID,
		CancellationID:      dto.CancellationID,
		TerminalOutcome:     TerminalOutcome(dto.TerminalOutcome),
		LogicalEvidenceHash: dto.LogicalEvidenceHash,
		CanonicalHash:       dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindCancellationAck,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

func decodeProviderV1Error(
	object providerV1JSONObject,
) (decodedProviderV1Record, error) {
	var dto providerV1ErrorDTO
	if err := object.decode(
		providerV1SchemaError,
		&dto,
		"schema",
		"error_code",
		"provider_id",
		"provider_epoch",
		"retry_from_transition",
		"canonical_hash",
	); err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	preimage, err := validatedProviderV1Preimage(dto, dto.CanonicalHash)
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	value, err := NewProviderFailure(ProviderFailureInput{
		Code:          ProviderErrorCode(dto.ErrorCode),
		ProviderID:    dto.ProviderID,
		ProviderEpoch: dto.ProviderEpoch,
		RetryFrom:     Transition(dto.RetryFromTransition),
		CanonicalHash: dto.CanonicalHash,
	})
	if err != nil {
		return decodedProviderV1Record{}, errProviderV1Frame
	}
	return decodedProviderV1Record{
		kind:              providerV1KindError,
		value:             value,
		canonicalPreimage: preimage,
	}, nil
}

type providerV1CanonicalDTO interface {
	canonicalPreimage() ([]byte, error)
}

func validatedProviderV1Preimage(
	dto providerV1CanonicalDTO,
	canonicalHash string,
) ([]byte, error) {
	if dto == nil {
		return nil, errProviderV1Frame
	}
	preimage, err := dto.canonicalPreimage()
	if err != nil ||
		providerV1ValidateCanonicalHash(canonicalHash, preimage) != nil {
		return nil, errProviderV1Frame
	}
	return preimage, nil
}

func providerV1Capabilities(values []string) ([]Capability, error) {
	if len(values) > maxCapabilities || !providerV1StringsStrictlySorted(values) {
		return nil, errProviderV1Frame
	}
	capabilities := make([]Capability, len(values))
	for index, value := range values {
		capability := Capability(value)
		if !capability.valid() {
			return nil, errProviderV1Frame
		}
		capabilities[index] = capability
	}
	return capabilities, nil
}

func providerV1Resources(values []string) ([]ResourceDimension, error) {
	if len(values) > maxResources || !providerV1StringsStrictlySorted(values) {
		return nil, errProviderV1Frame
	}
	resources := make([]ResourceDimension, len(values))
	for index, value := range values {
		resource := ResourceDimension(value)
		if !resource.valid() {
			return nil, errProviderV1Frame
		}
		resources[index] = resource
	}
	return resources, nil
}

func providerV1CapabilityStrings(values []Capability) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func providerV1ResourceStrings(values []ResourceDimension) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func providerV1Time(milliseconds uint64) (time.Time, error) {
	if milliseconds == 0 || milliseconds > math.MaxInt64 {
		return time.Time{}, errProviderV1Frame
	}
	return time.UnixMilli(int64(milliseconds)).UTC(), nil
}
