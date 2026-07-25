package planescapeprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const conformanceSchema = "planescape.provider.conformance_case.v1"

var conformanceFields = map[string]struct{}{
	"schema": {}, "case_id": {}, "protocol_version": {}, "provider_profile": {},
	"required_profile": {}, "category": {}, "request_kind": {}, "response_kind": {},
	"expected_disposition": {}, "provider_epoch": {}, "session_id": {},
	"operation_id": {}, "operation_sequence": {}, "nonce": {}, "requirement_hash": {},
	"compiled_plan_hash": {}, "plan_hash": {}, "bound_plan_hash": {}, "payload_hash": {},
	"prior_payload_hash": {}, "logical_evidence_hash": {}, "required_capabilities": {},
	"advertised_capabilities": {}, "required_resource_dimensions": {},
	"advertised_resource_dimensions": {}, "containment_request_hash": {},
	"provider_capability_hash": {}, "authority_hash": {}, "evidence_profile_hash": {},
	"transition": {},
}

func TestPlanescapeProviderConformanceCorpus(t *testing.T) {
	root := filepath.Join("testdata", "planescape.provider.v1", "fixtures")
	files := fixtureFiles(t, root)
	hasher := sha256.New()
	listed := make([]string, 0, len(files))
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > MaxRecordBytes {
			t.Fatalf("%s exceeds bounded v1 record limit", path)
		}
		var fixture map[string]any
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := validateConformanceFixture(fixture); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		relative = filepath.ToSlash(relative)
		listed = append(listed, relative)
		_, _ = hasher.Write([]byte(relative))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write(data)
	}
	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))

	manifestBytes, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Schema       string   `json:"schema"`
		Files        []string `json:"files"`
		CorpusSHA256 string   `json:"corpus_sha256"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "planescape.provider.fixture_manifest.v1" || strings.Join(manifest.Files, "\n") != strings.Join(listed, "\n") || manifest.CorpusSHA256 != digest {
		t.Fatalf("fixture manifest mismatch: got %s", digest)
	}
	pinned, err := os.ReadFile(filepath.Join(root, "CORPUS.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(pinned)) != digest {
		t.Fatalf("pinned corpus digest mismatch: got %s", digest)
	}
}

func TestConformanceFixtureMutationsFailClosed(t *testing.T) {
	valid := map[string]any{
		"schema":               conformanceSchema,
		"case_id":              "mutation",
		"protocol_version":     ProtocolVersionV1,
		"provider_profile":     string(ProfilePortable),
		"category":             "valid",
		"request_kind":         "provider_discovery",
		"response_kind":        "provider_capabilities",
		"expected_disposition": "accepted",
		"transition":           string(TransitionDiscover),
	}
	if err := validateConformanceFixture(valid); err != nil {
		t.Fatal(err)
	}
	compilerInput := map[string]any{
		"schema":                         conformanceSchema,
		"case_id":                        "compiler-input",
		"protocol_version":               ProtocolVersionV1,
		"provider_profile":               string(ProfileStockLinux),
		"category":                       "valid",
		"request_kind":                   "execution_requirement",
		"response_kind":                  "provider_capabilities",
		"expected_disposition":           "accepted",
		"required_capabilities":          []any{"artifact_read"},
		"advertised_capabilities":        []any{"artifact_read"},
		"required_resource_dimensions":   []any{"memory_bytes"},
		"advertised_resource_dimensions": []any{"memory_bytes"},
	}
	if err := validateConformanceFixture(compilerInput); err != nil {
		t.Fatal(err)
	}
	compilerInput["transition"] = string(TransitionAdmit)
	if err := validateConformanceFixture(compilerInput); err == nil {
		t.Fatal("accepted lifecycle transition on compiler input")
	}
	valid["ambient_host_path"] = "/forbidden"
	if err := validateConformanceFixture(valid); err == nil {
		t.Fatal("accepted unknown ambient authority field")
	}
	conflict := map[string]any{
		"schema":               conformanceSchema,
		"case_id":              "crosswire",
		"protocol_version":     ProtocolVersionV1,
		"provider_profile":     string(ProfilePortable),
		"category":             "crosswire",
		"request_kind":         "agent_operation",
		"response_kind":        "operation_result",
		"expected_disposition": "conflict",
		"plan_hash":            fingerprint("a"),
		"bound_plan_hash":      fingerprint("a"),
	}
	if err := validateConformanceFixture(conflict); err == nil {
		t.Fatal("accepted same-plan crosswire fixture")
	}
}

func fixtureFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".json") && filepath.Base(path) != "manifest.json" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func validateConformanceFixture(fixture map[string]any) error {
	for _, field := range []string{"schema", "case_id", "protocol_version", "provider_profile", "category", "request_kind", "response_kind", "expected_disposition"} {
		if _, ok := fixture[field]; !ok {
			return fmt.Errorf("missing %s", field)
		}
	}
	if text(fixture["schema"]) != conformanceSchema {
		return fmt.Errorf("unsupported schema")
	}
	category := text(fixture["category"])
	if !oneOf(category, "valid", "invalid", "unavailable", "crosswire", "replay") {
		return fmt.Errorf("unknown category")
	}
	disposition := text(fixture["expected_disposition"])
	if !oneOf(disposition, "accepted", "invalid", "unsupported", "unavailable", "conflict") {
		return fmt.Errorf("unknown disposition")
	}
	if text(fixture["protocol_version"]) != ProtocolVersionV1 {
		if category == "invalid" && disposition == "invalid" {
			return nil
		}
		return fmt.Errorf("unsupported version has wrong disposition")
	}
	profile := Profile(text(fixture["provider_profile"]))
	if !profile.valid() {
		return fmt.Errorf("unknown provider profile")
	}
	if !oneOf(text(fixture["request_kind"]), "provider_discovery", "execution_requirement", "compiled_containment_plan", "agent_operation", "freeze", "cancellation") ||
		!oneOf(text(fixture["response_kind"]), "provider_capabilities", "session_admission", "operation_result", "quiescence", "freeze_ack", "cancellation_ack", "closeout", "error") {
		return fmt.Errorf("unknown request or response kind")
	}

	unknown := false
	for field := range fixture {
		if _, ok := conformanceFields[field]; !ok {
			unknown = true
		}
	}
	if category == "invalid" && disposition == "invalid" {
		if !unknown && number(fixture["operation_sequence"]) != 0 {
			return fmt.Errorf("invalid fixture lacks a rejection trigger")
		}
		return nil
	}
	if unknown {
		return fmt.Errorf("unknown field")
	}
	if category == "invalid" && disposition == "unsupported" {
		required := Profile(text(fixture["required_profile"]))
		if !required.valid() || profile.Satisfies(required) {
			return fmt.Errorf("invalid unsupported profile fixture")
		}
		return nil
	}
	if category == "invalid" {
		return fmt.Errorf("invalid fixture has unsupported disposition")
	}

	if category == "valid" {
		transitions := map[string]Transition{
			"provider_discovery/provider_capabilities":    TransitionDiscover,
			"compiled_containment_plan/session_admission": TransitionAdmit,
			"agent_operation/operation_result":            TransitionActivate,
			"agent_operation/quiescence":                  TransitionQuiesce,
			"freeze/freeze_ack":                           TransitionFreeze,
			"cancellation/cancellation_ack":               TransitionCancel,
			"agent_operation/closeout":                    TransitionCloseout,
		}
		key := text(fixture["request_kind"]) + "/" + text(fixture["response_kind"])
		if key == "execution_requirement/provider_capabilities" {
			if _, present := fixture["transition"]; present {
				return fmt.Errorf("compiler input must not declare a lifecycle transition")
			}
		} else if transition, ok := transitions[key]; !ok ||
			text(fixture["transition"]) != string(transition) {
			return fmt.Errorf("invalid lifecycle transition")
		}
	}
	for _, field := range []string{
		"requirement_hash",
		"compiled_plan_hash",
		"containment_request_hash",
		"provider_capability_hash",
		"authority_hash",
		"evidence_profile_hash",
		"plan_hash",
		"bound_plan_hash",
		"payload_hash",
		"prior_payload_hash",
		"logical_evidence_hash",
	} {
		if value, ok := fixture[field]; ok {
			if _, err := ParseFingerprint(text(value)); err != nil {
				return fmt.Errorf("%s must be sha256", field)
			}
		}
	}
	if category == "crosswire" && text(fixture["plan_hash"]) == text(fixture["bound_plan_hash"]) {
		return fmt.Errorf("crosswire plan must differ")
	}
	if category == "replay" && text(fixture["payload_hash"]) == text(fixture["prior_payload_hash"]) {
		return fmt.Errorf("replay payload must differ")
	}
	if category == "unavailable" && disposition != "unavailable" {
		return fmt.Errorf("unavailable fixture has wrong disposition")
	}
	if err := validateDeclaredSet(fixture, "required_capabilities", "advertised_capabilities", func(value string) bool { return Capability(value).valid() }); err != nil {
		return err
	}
	if err := validateDeclaredSet(fixture, "required_resource_dimensions", "advertised_resource_dimensions", func(value string) bool { return ResourceDimension(value).valid() }); err != nil {
		return err
	}
	return nil
}

func validateDeclaredSet(fixture map[string]any, requiredField, advertisedField string, allowed func(string) bool) error {
	requiredRaw, hasRequired := fixture[requiredField]
	advertisedRaw, hasAdvertised := fixture[advertisedField]
	if hasRequired != hasAdvertised {
		return fmt.Errorf("%s and %s must appear together", requiredField, advertisedField)
	}
	if !hasRequired {
		return nil
	}
	required, err := stringSet(requiredRaw, allowed)
	if err != nil {
		return fmt.Errorf("%s: %w", requiredField, err)
	}
	advertised, err := stringSet(advertisedRaw, allowed)
	if err != nil {
		return fmt.Errorf("%s: %w", advertisedField, err)
	}
	for value := range required {
		if _, ok := advertised[value]; !ok {
			return fmt.Errorf("advertised set omits required %s", value)
		}
	}
	return nil
}

func stringSet(value any, allowed func(string) bool) (map[string]struct{}, error) {
	values, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("must be a JSON array")
	}
	result := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := text(raw)
		if !allowed(value) {
			return nil, fmt.Errorf("unknown value")
		}
		if _, ok := result[value]; ok {
			return nil, fmt.Errorf("duplicate value")
		}
		result[value] = struct{}{}
	}
	return result, nil
}

func text(value any) string {
	textValue, ok := value.(string)
	if !ok {
		return ""
	}
	return textValue
}

func number(value any) float64 {
	numberValue, ok := value.(float64)
	if !ok {
		return -1
	}
	return numberValue
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
