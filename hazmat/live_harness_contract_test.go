package hazmat

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type liveHarnessContract struct {
	SchemaVersion  int                      `json:"schema_version"`
	ExpectedMarker string                   `json:"expected_marker"`
	ArtifactFields []string                 `json:"artifact_fields"`
	Harnesses      []liveHarnessContractRow `json:"harnesses"`
}

type liveHarnessContractRow struct {
	ID                       string                           `json:"id"`
	DisplayName              string                           `json:"display_name"`
	SupportSources           []string                         `json:"support_sources"`
	LaunchCommand            string                           `json:"launch_command"`
	BootstrapCommand         string                           `json:"bootstrap_command"`
	InferenceShape           string                           `json:"inference_shape"`
	LiveArgv                 []string                         `json:"live_argv"`
	TimeoutSeconds           int                              `json:"timeout_seconds"`
	ExpectedMarker           string                           `json:"expected_marker"`
	AuthTokenMapping         map[string]string                `json:"auth_token_mapping"`
	DirectProviderTokens     []liveHarnessDirectProviderToken `json:"direct_provider_tokens"`
	DirectProviderSkipReason string                           `json:"direct_provider_skip_reason"`
	StateRoots               []string                         `json:"state_roots"`
	SkipConditions           []string                         `json:"skip_conditions"`
	OSSupport                []liveHarnessOSSupport           `json:"os_support"`
}

type liveHarnessDirectProviderToken struct {
	Provider     string `json:"provider"`
	CIEnv        string `json:"ci_env"`
	SessionEnv   string `json:"session_env"`
	StoreRelPath string `json:"store_rel_path"`
}

type liveHarnessOSSupport struct {
	Lane       string `json:"lane"`
	Status     string `json:"status"`
	Reason     string `json:"reason"`
	SkipReason string `json:"skip_reason"`
}

func TestLiveHarnessSmokeContractMatchesManagedRegistry(t *testing.T) {
	contract := loadLiveHarnessContract(t)
	if contract.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", contract.SchemaVersion)
	}
	if contract.ExpectedMarker != "HAZMAT_LIVE_SMOKE_OK" {
		t.Fatalf("expected_marker = %q", contract.ExpectedMarker)
	}
	for _, field := range []string{"harness", "os_lane", "status", "transcript", "skip_reason"} {
		if !liveContractContainsString(contract.ArtifactFields, field) {
			t.Fatalf("artifact_fields missing %q", field)
		}
	}

	rows := map[HarnessID]liveHarnessContractRow{}
	for _, row := range contract.Harnesses {
		id := HarnessID(row.ID)
		if _, duplicate := rows[id]; duplicate {
			t.Fatalf("duplicate harness contract row %q", id)
		}
		rows[id] = row
	}

	for _, harness := range managedHarnessRegistry {
		row, ok := rows[harness.Spec.ID]
		if !ok {
			t.Fatalf("live harness contract missing managed harness %q", harness.Spec.ID)
		}
		if row.DisplayName != harness.Spec.DisplayName {
			t.Fatalf("%s display_name = %q, want %q", row.ID, row.DisplayName, harness.Spec.DisplayName)
		}
		if row.LaunchCommand != harness.LaunchCommand {
			t.Fatalf("%s launch_command = %q, want %q", row.ID, row.LaunchCommand, harness.LaunchCommand)
		}
		if row.BootstrapCommand != harness.BootstrapCommand {
			t.Fatalf("%s bootstrap_command = %q, want %q", row.ID, row.BootstrapCommand, harness.BootstrapCommand)
		}
		assertLiveHarnessRowComplete(t, row, contract.ExpectedMarker)
		delete(rows, harness.Spec.ID)
	}
	for id := range rows {
		t.Fatalf("live harness contract lists unknown harness %q", id)
	}
}

func TestLiveHarnessSmokeContractExcludesRecipeOnlyHarnesses(t *testing.T) {
	contract := loadLiveHarnessContract(t)
	for _, row := range contract.Harnesses {
		switch row.ID {
		case "openhands", "ollama", "torch-hub", "gemini", "aider", "cline", "goose", "qoder", "trae", "vibe":
			t.Fatalf("recipe-only or removed harness %q must not appear in supported live matrix", row.ID)
		}
	}
}

func loadLiveHarnessContract(t *testing.T) liveHarnessContract {
	t.Helper()
	raw, err := os.ReadFile("../docs/live-harness-smoke-contract.json")
	if err != nil {
		t.Fatalf("read live harness contract: %v", err)
	}
	var contract liveHarnessContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatalf("decode live harness contract: %v", err)
	}
	return contract
}

func assertLiveHarnessRowComplete(t *testing.T, row liveHarnessContractRow, marker string) {
	t.Helper()
	for label, value := range map[string]string{
		"inference_shape": row.InferenceShape,
		"expected_marker": row.ExpectedMarker,
	} {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s %s is empty", row.ID, label)
		}
	}
	if row.ExpectedMarker != marker {
		t.Fatalf("%s expected_marker = %q, want %q", row.ID, row.ExpectedMarker, marker)
	}
	if row.TimeoutSeconds <= 0 {
		t.Fatalf("%s timeout_seconds = %d, want positive", row.ID, row.TimeoutSeconds)
	}
	if len(row.LiveArgv) < 3 || row.LiveArgv[0] != "hazmat" {
		t.Fatalf("%s live_argv = %#v, want hazmat command", row.ID, row.LiveArgv)
	}
	if !liveContractContainsString(row.LiveArgv, "{marker}") && !liveContractContainsJoined(row.LiveArgv, "{marker}") {
		t.Fatalf("%s live_argv must include marker placeholder: %#v", row.ID, row.LiveArgv)
	}
	if !liveContractContainsString(row.LiveArgv, "{project}") {
		t.Fatalf("%s live_argv must include project placeholder: %#v", row.ID, row.LiveArgv)
	}
	for _, source := range []string{"hazmat/harnesses/harnesses.go", "hazmat/harness.go", "docs/harnesses.md"} {
		if !liveContractContainsString(row.SupportSources, source) {
			t.Fatalf("%s support_sources missing %s", row.ID, source)
		}
	}
	for _, key := range []string{"mode", "materializer", "ci_token_envs", "harness_delivery"} {
		if strings.TrimSpace(row.AuthTokenMapping[key]) == "" {
			t.Fatalf("%s auth_token_mapping missing %s", row.ID, key)
		}
	}
	if strings.Contains(strings.Join(liveContractMapValues(row.AuthTokenMapping), "\n"), "MUGINN") ||
		strings.Contains(strings.ToLower(strings.Join(liveContractMapValues(row.AuthTokenMapping), "\n")), "muginn") {
		t.Fatalf("%s auth_token_mapping must not depend on Muginn", row.ID)
	}
	switch row.AuthTokenMapping["mode"] {
	case "direct-provider-secret":
		if len(row.DirectProviderTokens) == 0 {
			t.Fatalf("%s direct-provider-secret mode needs direct_provider_tokens", row.ID)
		}
	case "contained-harness-auth":
		if len(row.DirectProviderTokens) != 0 {
			t.Fatalf("%s contained-harness-auth mode must not list direct_provider_tokens", row.ID)
		}
		if strings.TrimSpace(row.DirectProviderSkipReason) == "" {
			t.Fatalf("%s contained-harness-auth mode needs direct_provider_skip_reason", row.ID)
		}
	default:
		t.Fatalf("%s auth_token_mapping mode = %q", row.ID, row.AuthTokenMapping["mode"])
	}
	for _, token := range row.DirectProviderTokens {
		if strings.TrimSpace(token.Provider) == "" {
			t.Fatalf("%s direct_provider_tokens has empty provider", row.ID)
		}
		if !strings.HasPrefix(token.CIEnv, "HAZMAT_LIVE_PROVIDER_") {
			t.Fatalf("%s ci_env = %q, want HAZMAT_LIVE_PROVIDER_*", row.ID, token.CIEnv)
		}
		if strings.TrimSpace(token.SessionEnv) == "" {
			t.Fatalf("%s direct_provider_tokens has empty session_env", row.ID)
		}
		if !strings.HasPrefix(token.StoreRelPath, "providers/") || strings.Contains(token.StoreRelPath, "..") {
			t.Fatalf("%s store_rel_path = %q, want providers/*", row.ID, token.StoreRelPath)
		}
		if !strings.Contains(row.AuthTokenMapping["ci_token_envs"], token.CIEnv) {
			t.Fatalf("%s ci_token_envs does not list %s", row.ID, token.CIEnv)
		}
	}
	for label, values := range map[string][]string{
		"state_roots":     row.StateRoots,
		"skip_conditions": row.SkipConditions,
	} {
		if len(values) == 0 {
			t.Fatalf("%s %s is empty", row.ID, label)
		}
	}
	supportByLane := map[string]liveHarnessOSSupport{}
	for _, support := range row.OSSupport {
		supportByLane[support.Lane] = support
	}
	for _, lane := range []string{"macos-agent-user", "docker-sandbox", "macos-current-user", "linux-current-user", "linux-agent-user"} {
		support, ok := supportByLane[lane]
		if !ok {
			t.Fatalf("%s os_support missing lane %s", row.ID, lane)
		}
		switch support.Status {
		case "supported":
			if support.Reason == "" {
				t.Fatalf("%s lane %s supported row needs reason", row.ID, lane)
			}
		case "typed_skip":
			if support.SkipReason == "" {
				t.Fatalf("%s lane %s typed skip row needs skip_reason", row.ID, lane)
			}
		default:
			t.Fatalf("%s lane %s has unsupported status %q", row.ID, lane, support.Status)
		}
	}
}

func liveContractContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func liveContractContainsJoined(values []string, want string) bool {
	return strings.Contains(strings.Join(values, "\x00"), want)
}

func liveContractMapValues(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
