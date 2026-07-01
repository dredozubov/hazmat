package llmproxy

import (
	"errors"
	"sort"
	"strings"
)

const (
	OpenAIBaseURLEnv = "OPENAI_BASE_URL"
	OpenAIAPIKeyEnv  = "OPENAI_API_KEY"
)

type ClientEnvRequest struct {
	ProxyBaseURL         string
	SessionToken         string
	ProviderEnv          map[string]string
	ModelUpdatesRequired bool
	AdditionalEnv        map[string]string
}

type ClientEnvPlan struct {
	Env                  []string        `json:"env"`
	CredentialMode       CredentialMode  `json:"credential_mode"`
	ModelUpdatesRequired bool            `json:"model_updates_required"`
	ExcludedProviderEnv  []string        `json:"excluded_provider_env,omitempty"`
	Redactions           []RedactionNote `json:"redactions,omitempty"`
}

func PlanOpenAIClientEnv(request ClientEnvRequest) (ClientEnvPlan, error) {
	proxyBaseURL := strings.TrimSpace(request.ProxyBaseURL)
	if proxyBaseURL == "" {
		return ClientEnvPlan{}, errors.New("llmproxy: proxy base URL is required")
	}
	sessionToken := strings.TrimSpace(request.SessionToken)
	if sessionToken == "" {
		return ClientEnvPlan{}, errors.New("llmproxy: session token is required")
	}
	env := []string{
		OpenAIBaseURLEnv + "=" + proxyBaseURL,
		OpenAIAPIKeyEnv + "=" + sessionToken,
	}
	for _, item := range sortedEnvItems(request.AdditionalEnv) {
		key := item.key
		if key == OpenAIBaseURLEnv || key == OpenAIAPIKeyEnv || isSensitiveEnvName(key) {
			continue
		}
		env = append(env, key+"="+item.value)
	}
	return ClientEnvPlan{
		Env:                  env,
		CredentialMode:       CredentialModeProxySessionToken,
		ModelUpdatesRequired: request.ModelUpdatesRequired,
		ExcludedProviderEnv:  providerEnvNames(request.ProviderEnv),
		Redactions: []RedactionNote{{
			Field: OpenAIAPIKeyEnv,
			Kind:  "token",
		}},
	}, nil
}

func (p ClientEnvPlan) EnvPairs() []string {
	if len(p.Env) == 0 {
		return nil
	}
	out := make([]string, len(p.Env))
	copy(out, p.Env)
	return out
}

func providerEnvNames(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	var names []string
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" || strings.TrimSpace(value) == "" {
			continue
		}
		if isSensitiveEnvName(key) {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	return names
}

func sortedEnvItems(values map[string]string) []envItem {
	if len(values) == 0 {
		return nil
	}
	items := make([]envItem, 0, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			items = append(items, envItem{key: key, value: value})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].key < items[j].key
	})
	return items
}

type envItem struct {
	key   string
	value string
}

func isSensitiveEnvName(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	return strings.Contains(name, "API_KEY") ||
		strings.Contains(name, "TOKEN") ||
		strings.Contains(name, "SECRET") ||
		strings.Contains(name, "PASSWORD")
}
