package hazmat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"hazmat/configmodel"
	"hazmat/planescapeprovider"
)

const (
	planescapeProductAuthorityFileSchemaV2           = "hazmat.planescape.rust_invocation_authority.v2"
	planescapeProductAuthorityTerminalCloseoutV2     = "closeout"
	planescapeProductAuthorityTerminalCancellationV2 = "cancellation"
	maxPlanescapeProductAuthorityFileBytes           = 1024 * 1024
)

type planescapeProductAuthorityFileV2 struct {
	Schema                    string                                     `json:"schema"`
	Invocation                planescapeProductAuthorityInvocationFileV2 `json:"invocation"`
	CompiledPlanJSONBase64URL string                                     `json:"compiled_plan_json_b64"`
	Tool                      planescapeProductAuthorityToolFileV2       `json:"tool"`
	Terminal                  json.RawMessage                            `json:"terminal"`
}

type planescapeProductAuthorityInvocationFileV2 struct {
	CommandName      string   `json:"command_name"`
	ForwardedArgs    []string `json:"forwarded_args"`
	SessionRequestID string   `json:"session_request_id"`
}

type planescapeProductAuthorityToolFileV2 struct {
	OperationJSONBase64URL    string `json:"operation_json_b64"`
	NormalizedRecordBase64URL string `json:"normalized_record_b64"`
	NormalizedRecordSHA256    string `json:"normalized_record_sha256"`
}

type planescapeProductAuthorityTerminalHeaderFileV2 struct {
	Kind string `json:"kind"`
}

type planescapeProductAuthorityCloseoutFileV2 struct {
	Kind                        string                                         `json:"kind"`
	PauseOperationJSONBase64URL string                                         `json:"pause_operation_json_b64"`
	Freeze                      planescapeProductAuthorityFreezeIntentFileV2   `json:"freeze"`
	Closeout                    planescapeProductAuthorityCloseoutIntentFileV2 `json:"closeout"`
}

type planescapeProductAuthorityFreezeIntentFileV2 struct {
	FreezeID string `json:"freeze_id"`
	Nonce    string `json:"nonce"`
}

type planescapeProductAuthorityCloseoutIntentFileV2 struct {
	OperationID       string `json:"operation_id"`
	OperationSequence uint64 `json:"operation_sequence"`
	Nonce             string `json:"nonce"`
}

type planescapeProductAuthorityCancellationFileV2 struct {
	Kind                      string `json:"kind"`
	CancellationJSONBase64URL string `json:"cancellation_json_b64"`
}

type planescapeProductExpectedOperation struct {
	record planescapeprovider.AgentOperation
	input  planescapeprovider.OperationInput
}

func (v planescapeProductExpectedOperation) valid(
	kind planescapeprovider.OperationKind,
	sequence uint64,
) bool {
	return validPlanescapeProductOperationInput(v.input, kind) &&
		v.record.Kind() == kind &&
		v.record.Sequence().Uint64() == sequence &&
		v.record.OperationID() == v.input.OperationID() &&
		v.record.Nonce() == v.input.Nonce() &&
		v.record.PayloadHash() == v.input.PayloadHash()
}

func (v planescapeProductExpectedOperation) matches(
	binding planescapeProductBinding,
) bool {
	return binding.valid() &&
		v.record.SessionID() == binding.SessionID() &&
		v.record.PlanHash() == binding.PlanHash()
}

type planescapeProductFileTerminalAuthority interface {
	planescapeProductFileTerminalAuthority()
	valid(planescapeProductExpectedOperation) bool
}

type planescapeProductFileCloseoutAuthority struct {
	pause    planescapeProductExpectedOperation
	freeze   planescapeprovider.FreezeInput
	closeout planescapeProductFileCloseoutIntent
}

func (planescapeProductFileCloseoutAuthority) planescapeProductFileTerminalAuthority() {
}

func (v planescapeProductFileCloseoutAuthority) valid(
	tool planescapeProductExpectedOperation,
) bool {
	return tool.valid(planescapeprovider.OperationTool, 1) &&
		v.pause.valid(planescapeprovider.OperationPause, 2) &&
		validPlanescapeProductFreezeInput(v.freeze) &&
		v.closeout.valid() &&
		v.pause.record.SessionID() == tool.record.SessionID() &&
		v.pause.record.PlanHash() == tool.record.PlanHash() &&
		v.closeout.operationID != tool.record.OperationID() &&
		v.closeout.operationID != v.pause.record.OperationID()
}

type planescapeProductFileCloseoutIntent struct {
	operationID planescapeprovider.Identifier
	sequence    planescapeprovider.OperationSequence
	nonce       planescapeprovider.Nonce
}

func newPlanescapeProductFileCloseoutIntent(
	operationID string,
	sequence uint64,
	nonce string,
) (planescapeProductFileCloseoutIntent, error) {
	id, err := planescapeprovider.NewIdentifier(operationID)
	if err != nil {
		return planescapeProductFileCloseoutIntent{}, err
	}
	operationSequence, err := planescapeprovider.NewOperationSequence(sequence)
	if err != nil {
		return planescapeProductFileCloseoutIntent{}, err
	}
	operationNonce, err := planescapeprovider.NewNonce(nonce)
	if err != nil {
		return planescapeProductFileCloseoutIntent{}, err
	}
	value := planescapeProductFileCloseoutIntent{
		operationID: id,
		sequence:    operationSequence,
		nonce:       operationNonce,
	}
	if !value.valid() {
		return planescapeProductFileCloseoutIntent{},
			invalidPlanescapeProductAuthority()
	}
	return value, nil
}

func (v planescapeProductFileCloseoutIntent) valid() bool {
	return v.operationID.String() != "" &&
		v.sequence.Uint64() == 3 &&
		v.nonce.String() != ""
}

func (v planescapeProductFileCloseoutIntent) bind(
	freeze planescapeprovider.FreezeAck,
) (planescapeProductCloseoutIntent, error) {
	if !v.valid() || !validPlanescapeProductFreezeAck(freeze) {
		return planescapeProductCloseoutIntent{},
			invalidPlanescapeProductAuthority()
	}
	input, err := planescapeprovider.NewOperationInput(
		planescapeprovider.OperationInputValues{
			OperationID: v.operationID.String(),
			Kind:        planescapeprovider.OperationCloseout,
			Nonce:       v.nonce.String(),
			PayloadHash: freeze.CanonicalHash().String(),
		},
	)
	if err != nil {
		return planescapeProductCloseoutIntent{},
			invalidPlanescapeProductAuthority()
	}
	return newPlanescapeProductCloseoutIntent(
		input,
		v.operationID.String(),
	)
}

type planescapeProductFileCancellationAuthority struct {
	record planescapeprovider.Cancellation
	input  planescapeprovider.CancellationInput
}

func (planescapeProductFileCancellationAuthority) planescapeProductFileTerminalAuthority() {
}

func (v planescapeProductFileCancellationAuthority) valid(
	tool planescapeProductExpectedOperation,
) bool {
	return tool.valid(planescapeprovider.OperationTool, 1) &&
		validPlanescapeProductCancellationInput(v.input) &&
		v.record.SessionID() == tool.record.SessionID() &&
		v.record.CancellationID() == v.input.CancellationID() &&
		v.record.Reason() == v.input.Reason() &&
		v.record.Nonce() == v.input.Nonce()
}

// planescapeProductFileAuthoritySource is immutable after construction. The
// configured file is read once, hash-pinned, and projected only after every
// live lifecycle binding needed by the next effect matches.
type planescapeProductFileAuthoritySource struct {
	invocation planescapeProductInvocation
	plan       planescapeprovider.CompiledContainmentPlan
	tool       planescapeProductExpectedOperation
	terminal   planescapeProductFileTerminalAuthority
}

var (
	_ planescapeProductInvocationSource   = (*planescapeProductFileAuthoritySource)(nil)
	_ planescapeProductCompiledPlanSource = (*planescapeProductFileAuthoritySource)(nil)
	_ planescapeProductOperationSource    = (*planescapeProductFileAuthoritySource)(nil)
	_ planescapeProductTerminalSource     = (*planescapeProductFileAuthoritySource)(nil)
)

func configuredPlanescapeProductAuthoritySource(
	config *configmodel.PlanescapeProviderConfig,
) (*planescapeProductFileAuthoritySource, error) {
	if config == nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	data, err := readPlanescapeProductAuthorityFile(
		config.InvocationAuthorityFile,
		config.InvocationAuthorityFileSHA256,
	)
	if err != nil {
		return nil, err
	}
	source, err := decodePlanescapeProductAuthorityFile(data)
	if err != nil {
		return nil, err
	}
	expectedIdentity, err := planescapeprovider.ParseFingerprint(
		config.Backend.IdentitySHA256,
	)
	if err != nil ||
		source.plan.ProviderID().String() != expectedIdentity.String() ||
		source.plan.ProviderEpoch().Uint64() != config.Backend.BrokerEpoch {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	return source, nil
}

func readPlanescapeProductAuthorityFile(
	path string,
	expectedSHA256 string,
) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	expected, err := planescapeprovider.ParseFingerprint(expectedSHA256)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	if !validPlanescapeProductSecureRegularFile(pathInfo) {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil ||
		!os.SameFile(pathInfo, openedInfo) ||
		!validPlanescapeProductSecureRegularFile(openedInfo) {
		_ = file.Close()
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	data, readErr := io.ReadAll(
		io.LimitReader(file, maxPlanescapeProductAuthorityFileBytes+1),
	)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	if len(data) == 0 || len(data) > maxPlanescapeProductAuthorityFileBytes {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	digest := sha256.Sum256(data)
	actual := "sha256:" + hex.EncodeToString(digest[:])
	if actual != expected.String() {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	return data, nil
}

func decodePlanescapeProductAuthorityFile(
	data []byte,
) (*planescapeProductFileAuthoritySource, error) {
	var envelope planescapeProductAuthorityFileV2
	if err := decodePlanescapeProductAuthorityJSON(data, &envelope); err != nil ||
		envelope.Schema != planescapeProductAuthorityFileSchemaV2 ||
		envelope.Invocation.ForwardedArgs == nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	invocation, err := newPlanescapeProductInvocation(
		envelope.Invocation.CommandName,
		envelope.Invocation.ForwardedArgs,
		envelope.Invocation.SessionRequestID,
	)
	if err != nil {
		return nil, err
	}
	planJSON, err := decodePlanescapeProductAuthorityBase64URL(
		envelope.CompiledPlanJSONBase64URL,
	)
	if err != nil {
		return nil, err
	}
	codec := planescapeprovider.ProviderV1FrameCodec{}
	plan, err := codec.DecodeCompiledContainmentPlan(planJSON)
	if err != nil {
		return nil, invalidPlanescapeProductAuthority()
	}
	canonicalPlan, err := codec.EncodeCompiledContainmentPlan(plan)
	if err != nil || !bytes.Equal(planJSON, canonicalPlan) {
		return nil, invalidPlanescapeProductAuthority()
	}
	normalizedRecord, err := decodePlanescapeProductAuthorityBase64URL(
		envelope.Tool.NormalizedRecordBase64URL,
	)
	if err != nil ||
		planescapeProductAuthorityBytesSHA256(normalizedRecord) !=
			envelope.Tool.NormalizedRecordSHA256 {
		return nil, invalidPlanescapeProductAuthority()
	}
	tool, err := decodePlanescapeProductExpectedOperation(
		codec,
		envelope.Tool.OperationJSONBase64URL,
		normalizedRecord,
	)
	if err != nil ||
		!tool.valid(planescapeprovider.OperationTool, 1) ||
		tool.record.PlanHash() != plan.CanonicalHash() {
		return nil, invalidPlanescapeProductAuthority()
	}
	terminal, err := decodePlanescapeProductFileTerminalAuthority(
		codec,
		envelope.Terminal,
		tool,
	)
	if err != nil || terminal == nil || !terminal.valid(tool) {
		return nil, invalidPlanescapeProductAuthority()
	}
	return &planescapeProductFileAuthoritySource{
		invocation: invocation,
		plan:       plan,
		tool:       tool,
		terminal:   terminal,
	}, nil
}

func decodePlanescapeProductFileTerminalAuthority(
	codec planescapeprovider.ProviderV1FrameCodec,
	raw json.RawMessage,
	tool planescapeProductExpectedOperation,
) (planescapeProductFileTerminalAuthority, error) {
	var header planescapeProductAuthorityTerminalHeaderFileV2
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, invalidPlanescapeProductAuthority()
	}
	switch header.Kind {
	case planescapeProductAuthorityTerminalCloseoutV2:
		var dto planescapeProductAuthorityCloseoutFileV2
		if err := decodePlanescapeProductAuthorityJSON(raw, &dto); err != nil {
			return nil, invalidPlanescapeProductAuthority()
		}
		pause, err := decodePlanescapeProductExpectedOperation(
			codec,
			dto.PauseOperationJSONBase64URL,
			nil,
		)
		if err != nil {
			return nil, err
		}
		freezeInput, err := planescapeprovider.NewFreezeInput(
			planescapeprovider.FreezeInputValues{
				FreezeID: dto.Freeze.FreezeID,
				Nonce:    dto.Freeze.Nonce,
			},
		)
		if err != nil {
			return nil, invalidPlanescapeProductAuthority()
		}
		closeout, err := newPlanescapeProductFileCloseoutIntent(
			dto.Closeout.OperationID,
			dto.Closeout.OperationSequence,
			dto.Closeout.Nonce,
		)
		if err != nil {
			return nil, invalidPlanescapeProductAuthority()
		}
		value := planescapeProductFileCloseoutAuthority{
			pause:    pause,
			freeze:   freezeInput,
			closeout: closeout,
		}
		if !value.valid(tool) {
			return nil, invalidPlanescapeProductAuthority()
		}
		return value, nil
	case planescapeProductAuthorityTerminalCancellationV2:
		var dto planescapeProductAuthorityCancellationFileV2
		if err := decodePlanescapeProductAuthorityJSON(raw, &dto); err != nil {
			return nil, invalidPlanescapeProductAuthority()
		}
		cancellationJSON, err := decodePlanescapeProductAuthorityBase64URL(
			dto.CancellationJSONBase64URL,
		)
		if err != nil {
			return nil, err
		}
		cancellation, err := codec.DecodeCancellation(cancellationJSON)
		if err != nil {
			return nil, invalidPlanescapeProductAuthority()
		}
		canonicalCancellation, err := codec.EncodeCancellation(cancellation)
		if err != nil ||
			!bytes.Equal(cancellationJSON, canonicalCancellation) {
			return nil, invalidPlanescapeProductAuthority()
		}
		input, err := planescapeprovider.NewCancellationInput(
			planescapeprovider.CancellationInputValues{
				CancellationID: cancellation.CancellationID().String(),
				Reason:         cancellation.Reason(),
				Nonce:          cancellation.Nonce().String(),
			},
		)
		if err != nil {
			return nil, invalidPlanescapeProductAuthority()
		}
		value := planescapeProductFileCancellationAuthority{
			record: cancellation,
			input:  input,
		}
		if !value.valid(tool) {
			return nil, invalidPlanescapeProductAuthority()
		}
		return value, nil
	default:
		return nil, invalidPlanescapeProductAuthority()
	}
}

func decodePlanescapeProductExpectedOperation(
	codec planescapeprovider.ProviderV1FrameCodec,
	encoded string,
	normalizedRecord []byte,
) (planescapeProductExpectedOperation, error) {
	operationJSON, err := decodePlanescapeProductAuthorityBase64URL(encoded)
	if err != nil {
		return planescapeProductExpectedOperation{}, err
	}
	record, err := codec.DecodeAgentOperation(operationJSON)
	if err != nil {
		return planescapeProductExpectedOperation{},
			invalidPlanescapeProductAuthority()
	}
	canonical, err := codec.EncodeOperation(record)
	if err != nil || !bytes.Equal(operationJSON, canonical) {
		return planescapeProductExpectedOperation{},
			invalidPlanescapeProductAuthority()
	}
	input, err := planescapeprovider.NewOperationInput(
		planescapeprovider.OperationInputValues{
			OperationID:      record.OperationID().String(),
			Kind:             record.Kind(),
			Nonce:            record.Nonce().String(),
			PayloadHash:      record.PayloadHash().String(),
			NormalizedRecord: normalizedRecord,
		},
	)
	if err != nil {
		return planescapeProductExpectedOperation{},
			invalidPlanescapeProductAuthority()
	}
	return planescapeProductExpectedOperation{
		record: record,
		input:  input,
	}, nil
}

func decodePlanescapeProductAuthorityBase64URL(value string) ([]byte, error) {
	if value == "" ||
		len(value) > base64.RawURLEncoding.EncodedLen(planescapeprovider.MaxRecordBytes) {
		return nil, invalidPlanescapeProductAuthority()
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil ||
		len(decoded) == 0 ||
		len(decoded) > planescapeprovider.MaxRecordBytes ||
		base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, invalidPlanescapeProductAuthority()
	}
	return decoded, nil
}

func decodePlanescapeProductAuthorityJSON(data []byte, target any) error {
	if target == nil || len(data) == 0 ||
		validatePlanescapeProductAuthorityJSON(data) != nil {
		return invalidPlanescapeProductAuthority()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalidPlanescapeProductAuthority()
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return invalidPlanescapeProductAuthority()
	}
	return nil
}

func validatePlanescapeProductAuthorityJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validatePlanescapeProductAuthorityJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return invalidPlanescapeProductAuthority()
	}
	return nil
}

func validatePlanescapeProductAuthorityJSONValue(
	decoder *json.Decoder,
) error {
	token, err := decoder.Token()
	if err != nil || token == nil {
		return invalidPlanescapeProductAuthority()
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return invalidPlanescapeProductAuthority()
			}
			if _, duplicate := seen[key]; duplicate {
				return invalidPlanescapeProductAuthority()
			}
			seen[key] = struct{}{}
			if err := validatePlanescapeProductAuthorityJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return invalidPlanescapeProductAuthority()
		}
	case '[':
		for decoder.More() {
			if err := validatePlanescapeProductAuthorityJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return invalidPlanescapeProductAuthority()
		}
	default:
		return invalidPlanescapeProductAuthority()
	}
	return nil
}

func planescapeProductAuthorityBytesSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func invalidPlanescapeProductAuthority() error {
	return newPlanescapeProductError(
		planescapeprovider.ErrorInvalid,
		planescapeProductProviderFailure,
	)
}

func (s *planescapeProductFileAuthoritySource) Invocation(
	commandName string,
	forwardedArgs []string,
) (planescapeProductInvocation, error) {
	if s == nil || !s.valid() {
		return planescapeProductInvocation{}, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	candidate, err := newPlanescapeProductInvocation(
		commandName,
		forwardedArgs,
		s.invocation.SessionRequestID().String(),
	)
	if err != nil {
		return planescapeProductInvocation{}, err
	}
	if !candidate.matches(s.invocation) {
		return planescapeProductInvocation{}, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	return candidate, nil
}

func (s *planescapeProductFileAuthoritySource) CompiledContainmentPlan(
	ctx context.Context,
	invocation planescapeProductInvocation,
) (planescapeProductCompiledPlanArtifact, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || !s.valid() {
		return planescapeProductCompiledPlanArtifact{},
			newPlanescapeProductError(
				planescapeprovider.ErrorUnavailable,
				planescapeProductProviderFailure,
			)
	}
	if !s.invocation.matches(invocation) {
		return planescapeProductCompiledPlanArtifact{},
			newPlanescapeProductError(
				planescapeprovider.ErrorConflict,
				planescapeProductProviderFailure,
			)
	}
	return newPlanescapeProductCompiledPlanArtifact(s.plan, s.invocation)
}

func (s *planescapeProductFileAuthoritySource) ToolOperation(
	ctx context.Context,
	binding planescapeProductBinding,
) (planescapeprovider.OperationInput, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || !s.valid() {
		return planescapeprovider.OperationInput{},
			newPlanescapeProductError(
				planescapeprovider.ErrorUnavailable,
				planescapeProductProviderFailure,
			)
	}
	if !s.matchesBinding(binding) ||
		!s.tool.matches(binding) ||
		!s.tool.valid(planescapeprovider.OperationTool, 1) {
		return planescapeprovider.OperationInput{},
			newPlanescapeProductError(
				planescapeprovider.ErrorConflict,
				planescapeProductProviderFailure,
			)
	}
	return s.tool.input, nil
}

func (s *planescapeProductFileAuthoritySource) PostToolIntent(
	ctx context.Context,
	binding planescapeProductBinding,
	tool planescapeprovider.OperationResult,
) (planescapeProductPostToolIntent, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || !s.valid() {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	if !s.matchesBinding(binding) ||
		tool.SessionID() != s.tool.record.SessionID() ||
		tool.OperationID() != s.tool.record.OperationID() ||
		tool.Sequence() != s.tool.record.Sequence() ||
		tool.ResultKind() != planescapeprovider.ResultCompleted {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	switch terminal := s.terminal.(type) {
	case planescapeProductFileCloseoutAuthority:
		if !terminal.pause.matches(binding) {
			return nil, newPlanescapeProductError(
				planescapeprovider.ErrorConflict,
				planescapeProductProviderFailure,
			)
		}
		return newPlanescapeProductPauseIntent(terminal.pause.input)
	case planescapeProductFileCancellationAuthority:
		if terminal.record.SessionID() != binding.SessionID() {
			return nil, newPlanescapeProductError(
				planescapeprovider.ErrorConflict,
				planescapeProductProviderFailure,
			)
		}
		return newPlanescapeProductCancellationIntent(terminal.input)
	default:
		return nil, invalidPlanescapeProductAuthority()
	}
}

func (s *planescapeProductFileAuthoritySource) FreezeInput(
	ctx context.Context,
	quiesced planescapeProductQuiescedLifecycle,
) (planescapeprovider.FreezeInput, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || !s.valid() ||
		!quiesced.valid() {
		return planescapeprovider.FreezeInput{},
			newPlanescapeProductError(
				planescapeprovider.ErrorUnavailable,
				planescapeProductProviderFailure,
			)
	}
	terminal, ok := s.terminal.(planescapeProductFileCloseoutAuthority)
	if !ok {
		return planescapeprovider.FreezeInput{},
			newPlanescapeProductError(
				planescapeprovider.ErrorUnsupported,
				planescapeProductProviderFailure,
			)
	}
	binding := quiesced.Binding()
	if !s.matchesBinding(binding) {
		return planescapeprovider.FreezeInput{},
			newPlanescapeProductError(
				planescapeprovider.ErrorConflict,
				planescapeProductProviderFailure,
			)
	}
	return terminal.freeze, nil
}

func (s *planescapeProductFileAuthoritySource) CloseoutIntent(
	ctx context.Context,
	frozen planescapeProductFrozenLifecycle,
) (planescapeProductCloseoutIntent, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || !s.valid() ||
		!frozen.valid() {
		return planescapeProductCloseoutIntent{},
			newPlanescapeProductError(
				planescapeprovider.ErrorUnavailable,
				planescapeProductProviderFailure,
			)
	}
	terminal, ok := s.terminal.(planescapeProductFileCloseoutAuthority)
	if !ok {
		return planescapeProductCloseoutIntent{},
			newPlanescapeProductError(
				planescapeprovider.ErrorUnsupported,
				planescapeProductProviderFailure,
			)
	}
	binding := frozen.Binding()
	if !s.matchesBinding(binding) ||
		terminal.freeze.FreezeID() != frozen.Freeze().FreezeID() {
		return planescapeProductCloseoutIntent{},
			newPlanescapeProductError(
				planescapeprovider.ErrorConflict,
				planescapeProductProviderFailure,
			)
	}
	return terminal.closeout.bind(frozen.Freeze())
}

func (s *planescapeProductFileAuthoritySource) valid() bool {
	if s == nil || !s.invocation.valid() || s.terminal == nil ||
		!s.tool.valid(planescapeprovider.OperationTool, 1) ||
		!s.terminal.valid(s.tool) {
		return false
	}
	_, err := planescapeprovider.NewAdmissionInput(s.plan)
	return err == nil && s.tool.record.PlanHash() == s.plan.CanonicalHash()
}

func (s *planescapeProductFileAuthoritySource) matchesBinding(
	binding planescapeProductBinding,
) bool {
	return s.valid() &&
		binding.valid() &&
		binding.Invocation().matches(s.invocation) &&
		binding.PlanHash() == s.plan.CanonicalHash() &&
		binding.Backend().IdentitySHA256().String() ==
			s.plan.ProviderID().String() &&
		binding.Backend().ProviderEpoch() == s.plan.ProviderEpoch()
}
