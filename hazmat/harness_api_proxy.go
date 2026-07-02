package hazmat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hazmat/llmproxyadapter"
)

type apiProxyMode string

const (
	apiProxyModeNone   apiProxyMode = "none"
	apiProxyModeMuginn apiProxyMode = "muginn"

	defaultMuginnProxyModel = "muginn/subscription-auto"
	envHazmatMuginnctl      = "HAZMAT_MUGINNCTL"
	envHazmatMuginnOpsDir   = "HAZMAT_MUGINN_OPS_DIR"
)

type muginnProxyRuntimeInfo struct {
	Schema             string `json:"schema"`
	Listen             string `json:"listen"`
	Upstream           string `json:"upstream"`
	Caller             string `json:"caller"`
	OpenAIBaseURL      string `json:"openai_base_url"`
	OpenAIAPIKey       string `json:"openai_api_key"`
	OpenAIModel        string `json:"openai_model"`
	WorkUnitMode       string `json:"work_unit_mode"`
	WorkUnitKeyPresent bool   `json:"work_unit_key_present"`
}

type commandOutputRunner func(name string, args ...string) ([]byte, []byte, error)

var (
	startMuginnOpenAIProxy                     = defaultStartMuginnOpenAIProxy
	runCommandOutput       commandOutputRunner = defaultRunCommandOutput
)

func parseAPIProxyMode(raw string) (apiProxyMode, error) {
	switch mode := apiProxyMode(strings.ToLower(strings.TrimSpace(raw))); mode {
	case "", apiProxyModeNone:
		return apiProxyModeNone, nil
	case apiProxyModeMuginn:
		return apiProxyModeMuginn, nil
	default:
		return "", fmt.Errorf("unsupported --api-proxy mode %q (want none or muginn)", raw)
	}
}

func validateAPIProxySession(cfg sessionConfig, mode sessionMode, proxyMode apiProxyMode) error {
	if proxyMode == apiProxyModeNone {
		return nil
	}
	if cfg.HarnessID != HarnessHermes {
		return fmt.Errorf("--api-proxy=%s is supported only for hazmat hermes in this release", proxyMode)
	}
	if mode != sessionModeNative {
		return fmt.Errorf("--api-proxy=%s is supported only for native sessions; use --docker=none", proxyMode)
	}
	if normalizeSessionNetworkMode(cfg.NetworkMode) == sessionNetworkNone {
		return fmt.Errorf("--api-proxy=%s requires native network access to the loopback proxy; remove --network none", proxyMode)
	}
	return nil
}

func applyHarnessCredentialEnvForSession(cfg *sessionConfig, proxyMode apiProxyMode, planOnly bool) error {
	switch proxyMode {
	case apiProxyModeNone:
		return applyHarnessAPIKeyEnvForSession(cfg, planOnly)
	case apiProxyModeMuginn:
		return applyMuginnAPIProxyEnvForSession(cfg, planOnly)
	default:
		return fmt.Errorf("unsupported API proxy mode %q", proxyMode)
	}
}

func applyMuginnAPIProxyEnvForSession(cfg *sessionConfig, planOnly bool) error {
	info := plannedMuginnProxyRuntimeInfo()
	if !planOnly {
		var err error
		info, err = startMuginnOpenAIProxy(defaultMuginnProxyModel)
		if err != nil {
			return err
		}
	}
	if err := validateMuginnProxyRuntimeInfo(info, defaultMuginnProxyModel); err != nil {
		return err
	}

	additionalEnv := copyStringMap(cfg.HarnessEnv)
	if additionalEnv == nil {
		additionalEnv = make(map[string]string, 1)
	}
	additionalEnv["OPENAI_MODEL"] = info.OpenAIModel
	plan, err := llmproxyadapter.PlanEnv(llmproxyadapter.Request{
		Harness:              cfg.HarnessID,
		ProxyBaseURL:         info.OpenAIBaseURL,
		SessionToken:         info.OpenAIAPIKey,
		AdditionalEnv:        additionalEnv,
		ModelUpdatesRequired: true,
	})
	if err != nil {
		return err
	}
	cfg.HarnessEnv = envPairsToMap(plan.EnvPairs())
	cfg.CredentialEnvGrants = appendSessionCredentialEnvGrant(cfg.CredentialEnvGrants, sessionCredentialEnvGrant{
		EnvVar:          "OPENAI_API_KEY",
		Source:          "Muginn local proxy session token",
		ConsumerHarness: cfg.HarnessID,
	})
	cfg.ServiceAccess = appendUniqueString(cfg.ServiceAccess, "muginn-api-proxy")
	cfg.SessionNotes = append(cfg.SessionNotes,
		"Hermes OpenAI-compatible traffic is routed through the Muginn local proxy using model "+info.OpenAIModel+"; durable provider keys are not injected into the Hermes process.",
	)
	return nil
}

func plannedMuginnProxyRuntimeInfo() muginnProxyRuntimeInfo {
	return muginnProxyRuntimeInfo{
		Schema:             "muginnctl.proxy.openai.v1",
		Listen:             "http://127.0.0.1:0",
		Upstream:           "muginn",
		Caller:             "configured by muginnctl",
		OpenAIBaseURL:      "http://127.0.0.1:0/v1",
		OpenAIAPIKey:       "muginn-local-proxy-token",
		OpenAIModel:        defaultMuginnProxyModel,
		WorkUnitMode:       "launch",
		WorkUnitKeyPresent: true,
	}
}

func defaultStartMuginnOpenAIProxy(model string) (muginnProxyRuntimeInfo, error) {
	name, args := muginnProxyStartCommand(model)
	stdout, stderr, err := runCommandOutput(name, args...)
	if err != nil {
		detail := oneLine(string(stderr))
		return muginnProxyRuntimeInfo{}, fmt.Errorf("start Muginn OpenAI proxy with %s: %w: %s", name, err, detail)
	}
	var info muginnProxyRuntimeInfo
	if err := json.Unmarshal(stdout, &info); err != nil {
		return muginnProxyRuntimeInfo{}, fmt.Errorf("parse Muginn proxy startup JSON: %w", err)
	}
	return info, nil
}

func muginnProxyStartCommand(model string) (string, []string) {
	muginnctl := configuredMuginnctlPath()
	args := []string{"proxy", "start", "--daemon", "--model", model, "--output", "json"}
	if opsDir := configuredMuginnOpsDir(); opsDir != "" {
		return "direnv", append([]string{"exec", opsDir, muginnctl}, args...)
	}
	return muginnctl, args
}

func configuredMuginnctlPath() string {
	if value := strings.TrimSpace(os.Getenv(envHazmatMuginnctl)); value != "" {
		return value
	}
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, "workspace", "muginn", "muginnctl")
		if executableFile(path) {
			return path
		}
	}
	return "muginnctl"
}

func configuredMuginnOpsDir() string {
	if value, ok := os.LookupEnv(envHazmatMuginnOpsDir); ok {
		value = strings.TrimSpace(value)
		if value == "-" {
			return ""
		}
		return value
	}
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, "ops")
		if directoryExists(path) {
			return path
		}
	}
	return ""
}

func defaultRunCommandOutput(name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func validateMuginnProxyRuntimeInfo(info muginnProxyRuntimeInfo, wantModel string) error {
	if strings.TrimSpace(info.OpenAIBaseURL) == "" {
		return fmt.Errorf("Muginn proxy did not report openai_base_url")
	}
	if strings.TrimSpace(info.OpenAIAPIKey) == "" {
		return fmt.Errorf("Muginn proxy did not report a local openai_api_key")
	}
	if strings.TrimSpace(info.OpenAIModel) != wantModel {
		return fmt.Errorf("Muginn proxy model = %q, want %q; stop the existing proxy and retry", info.OpenAIModel, wantModel)
	}
	return nil
}

func envPairsToMap(pairs []string) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if ok && key != "" {
			out[key] = value
		}
	}
	return out
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
