package hazmat

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"hazmat/llmproxy"
	"hazmat/llmproxyadapter"
)

type apiProxyMode string

const (
	apiProxyModeNone             apiProxyMode = "none"
	apiProxyModeOpenAICompatible apiProxyMode = "openai-compatible"
)

type openAICompatibleProxyInput struct {
	baseURL string
	apiKey  string
}

func parseAPIProxyMode(raw string) (apiProxyMode, error) {
	switch mode := apiProxyMode(strings.ToLower(strings.TrimSpace(raw))); mode {
	case "", apiProxyModeNone:
		return apiProxyModeNone, nil
	case apiProxyModeOpenAICompatible:
		return apiProxyModeOpenAICompatible, nil
	default:
		return "", fmt.Errorf("unsupported --api-proxy mode %q (want none or openai-compatible)", raw)
	}
}

func validateAPIProxySession(cfg sessionConfig, mode sessionMode, proxyMode apiProxyMode, explicit bool) error {
	if proxyMode == apiProxyModeNone {
		return nil
	}
	if !explicit {
		return fmt.Errorf("--api-proxy=%s must be selected explicitly", proxyMode)
	}
	if cfg.HarnessID != HarnessHermes {
		return fmt.Errorf("--api-proxy=%s is supported only for hazmat hermes in this release", proxyMode)
	}
	if mode != sessionModeNative {
		return fmt.Errorf("--api-proxy=%s is supported only for native sessions; use --docker=none", proxyMode)
	}
	if normalizeSessionNetworkMode(cfg.NetworkMode) == sessionNetworkNone {
		return fmt.Errorf("--api-proxy=%s requires native network access to the configured endpoint; remove --network none", proxyMode)
	}
	return nil
}

func applyHarnessCredentialEnvForSession(cfg *sessionConfig, proxyMode apiProxyMode, planOnly bool) error {
	switch proxyMode {
	case apiProxyModeNone:
		return applyHarnessAPIKeyEnvForSession(cfg, planOnly)
	case apiProxyModeOpenAICompatible:
		return applyOpenAICompatibleAPIProxyEnvForSession(cfg)
	default:
		return fmt.Errorf("unsupported API proxy mode %q", proxyMode)
	}
}

func applyOpenAICompatibleAPIProxyEnvForSession(cfg *sessionConfig) error {
	input, err := openAICompatibleProxyInputFromEnvironment()
	if err != nil {
		return err
	}

	additionalEnv := copyStringMap(cfg.HarnessEnv)
	delete(additionalEnv, "OPENAI_MODEL")
	plan, err := llmproxyadapter.PlanEnv(llmproxyadapter.Request{
		Harness:              cfg.HarnessID,
		ProxyBaseURL:         input.baseURL,
		SessionToken:         input.apiKey,
		ProviderEnv:          cfg.HarnessEnv,
		AdditionalEnv:        additionalEnv,
		ModelUpdatesRequired: false,
	})
	if err != nil {
		return err
	}
	cfg.HarnessEnv = envPairsToMap(plan.EnvPairs())
	cfg.CredentialEnvGrants = appendSessionCredentialEnvGrant(cfg.CredentialEnvGrants, sessionCredentialEnvGrant{
		EnvVar:          "OPENAI_API_KEY",
		Source:          "invoking environment proxy token",
		ConsumerHarness: cfg.HarnessID,
	})
	cfg.ServiceAccess = appendUniqueString(cfg.ServiceAccess, "openai-compatible-api-proxy")
	cfg.SessionNotes = append(cfg.SessionNotes,
		"Hermes OpenAI-compatible traffic is routed through the configured external endpoint; endpoint lifecycle and model discovery stay outside Hazmat, and durable provider keys are not injected into the Hermes process.",
	)
	return nil
}

func openAICompatibleProxyInputFromEnvironment() (openAICompatibleProxyInput, error) {
	return newOpenAICompatibleProxyInput(
		os.Getenv(llmproxy.OpenAIBaseURLEnv),
		os.Getenv(llmproxy.OpenAIAPIKeyEnv),
	)
}

func newOpenAICompatibleProxyInput(baseURL, apiKey string) (openAICompatibleProxyInput, error) {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	if baseURL == "" || apiKey == "" {
		return openAICompatibleProxyInput{}, fmt.Errorf(
			"--api-proxy=%s requires %s and %s together in the invoking environment",
			apiProxyModeOpenAICompatible,
			llmproxy.OpenAIBaseURLEnv,
			llmproxy.OpenAIAPIKeyEnv,
		)
	}
	if err := validateOpenAICompatibleBaseURL(baseURL); err != nil {
		return openAICompatibleProxyInput{}, err
	}
	return openAICompatibleProxyInput{baseURL: baseURL, apiKey: apiKey}, nil
}

func validateOpenAICompatibleBaseURL(raw string) error {
	endpoint, err := url.Parse(raw)
	if err != nil ||
		!endpoint.IsAbs() ||
		endpoint.Opaque != "" ||
		endpoint.Hostname() == "" ||
		endpoint.User != nil ||
		endpoint.RawQuery != "" ||
		endpoint.Fragment != "" {
		return fmt.Errorf("%s must be an absolute HTTPS URL or loopback HTTP URL without credentials, query, or fragment", llmproxy.OpenAIBaseURLEnv)
	}

	switch strings.ToLower(endpoint.Scheme) {
	case "https":
		return nil
	case "http":
		if openAICompatibleLoopbackHost(endpoint.Hostname()) {
			return nil
		}
	}
	return fmt.Errorf("%s must use HTTPS or loopback HTTP", llmproxy.OpenAIBaseURLEnv)
}

func openAICompatibleLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
