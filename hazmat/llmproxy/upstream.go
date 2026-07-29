package llmproxy

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type UpstreamKind string

const (
	UpstreamKindOpenAICompatible UpstreamKind = "openai-compatible"
)

type CredentialMode string

const (
	CredentialModeUnmanaged         CredentialMode = "unmanaged"
	CredentialModeUpstreamBearer    CredentialMode = "upstream-bearer"
	CredentialModeProxySessionToken CredentialMode = "proxy-session-token"
)

type UpstreamConfig struct {
	Kind                 UpstreamKind
	BaseURL              string
	BearerToken          string
	CredentialMode       CredentialMode
	ModelUpdatesRequired bool
	SanitizeFailures     bool
}

type BearerUpstreamConfig struct {
	BaseURL              string
	BearerToken          string
	ModelUpdatesRequired bool
}

type UpstreamPlan struct {
	Kind                       UpstreamKind    `json:"kind"`
	BaseURL                    string          `json:"base_url"`
	CredentialMode             CredentialMode  `json:"credential_mode"`
	ModelUpdatesRequired       bool            `json:"model_updates_required"`
	ProviderCredentialInjected bool            `json:"provider_credential_injected"`
	Redactions                 []RedactionNote `json:"redactions,omitempty"`
}

type RedactionNote struct {
	Field string `json:"field"`
	Kind  string `json:"kind"`
}

type normalizedUpstream struct {
	kind                 UpstreamKind
	baseURL              *url.URL
	bearerToken          string
	credentialMode       CredentialMode
	modelUpdatesRequired bool
	sanitizeFailures     bool
}

func NewBearerUpstream(config BearerUpstreamConfig) (UpstreamConfig, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		return UpstreamConfig{}, errors.New("llmproxy: upstream base URL is required")
	}
	bearerToken := strings.TrimSpace(config.BearerToken)
	if bearerToken == "" {
		return UpstreamConfig{}, errors.New("llmproxy: upstream bearer token is required")
	}
	return UpstreamConfig{
		Kind:                 UpstreamKindOpenAICompatible,
		BaseURL:              baseURL,
		BearerToken:          bearerToken,
		CredentialMode:       CredentialModeUpstreamBearer,
		ModelUpdatesRequired: config.ModelUpdatesRequired,
		SanitizeFailures:     true,
	}, nil
}

func (c UpstreamConfig) Plan() UpstreamPlan {
	kind := normalizeUpstreamKind(c.Kind)
	credentialMode := normalizeCredentialMode(c.CredentialMode)
	plan := UpstreamPlan{
		Kind:                       kind,
		BaseURL:                    strings.TrimSpace(c.BaseURL),
		CredentialMode:             credentialMode,
		ModelUpdatesRequired:       c.ModelUpdatesRequired,
		ProviderCredentialInjected: false,
	}
	if strings.TrimSpace(c.BearerToken) != "" {
		plan.Redactions = append(plan.Redactions, RedactionNote{Field: "bearer_token", Kind: "token"})
	}
	return plan
}

func normalizeUpstreamConfig(config UpstreamConfig, fallbackBaseURL string) (normalizedUpstream, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		config.BaseURL = fallbackBaseURL
	}
	if !knownUpstreamKind(config.Kind) {
		return normalizedUpstream{}, fmt.Errorf("llmproxy: unsupported upstream kind %q", config.Kind)
	}
	if !knownCredentialMode(config.CredentialMode) {
		return normalizedUpstream{}, fmt.Errorf("llmproxy: unsupported credential mode %q", config.CredentialMode)
	}
	kind := normalizeUpstreamKind(config.Kind)
	credentialMode := normalizeCredentialMode(config.CredentialMode)
	if credentialMode == CredentialModeUpstreamBearer {
		config.SanitizeFailures = true
	}
	upstreamURL, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil {
		return normalizedUpstream{}, fmt.Errorf("parse upstream base URL: %w", err)
	}
	if upstreamURL.Scheme != "http" && upstreamURL.Scheme != "https" || upstreamURL.Host == "" {
		return normalizedUpstream{}, errors.New("llmproxy: upstream base URL must be http or https")
	}
	if credentialMode == CredentialModeUpstreamBearer && strings.TrimSpace(config.BearerToken) == "" {
		return normalizedUpstream{}, errors.New("llmproxy: upstream bearer token is required")
	}
	return normalizedUpstream{
		kind:                 kind,
		baseURL:              upstreamURL,
		bearerToken:          strings.TrimSpace(config.BearerToken),
		credentialMode:       credentialMode,
		modelUpdatesRequired: config.ModelUpdatesRequired,
		sanitizeFailures:     config.SanitizeFailures,
	}, nil
}

func (u normalizedUpstream) plan() UpstreamPlan {
	baseURL := ""
	if u.baseURL != nil {
		baseURL = u.baseURL.String()
	}
	return UpstreamPlan{
		Kind:                       u.kind,
		BaseURL:                    baseURL,
		CredentialMode:             u.credentialMode,
		ModelUpdatesRequired:       u.modelUpdatesRequired,
		ProviderCredentialInjected: false,
		Redactions:                 redactionsForBearerToken(u.bearerToken),
	}
}

func redactionsForBearerToken(token string) []RedactionNote {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	return []RedactionNote{{Field: "bearer_token", Kind: "token"}}
}

func normalizeUpstreamKind(kind UpstreamKind) UpstreamKind {
	switch kind {
	case "", UpstreamKindOpenAICompatible:
		return UpstreamKindOpenAICompatible
	default:
		return UpstreamKindOpenAICompatible
	}
}

func knownUpstreamKind(kind UpstreamKind) bool {
	switch kind {
	case "", UpstreamKindOpenAICompatible:
		return true
	default:
		return false
	}
}

func normalizeCredentialMode(mode CredentialMode) CredentialMode {
	switch mode {
	case "", CredentialModeUnmanaged:
		return CredentialModeUnmanaged
	case CredentialModeUpstreamBearer, CredentialModeProxySessionToken:
		return mode
	default:
		return CredentialModeUnmanaged
	}
}

func knownCredentialMode(mode CredentialMode) bool {
	switch mode {
	case "", CredentialModeUnmanaged, CredentialModeUpstreamBearer, CredentialModeProxySessionToken:
		return true
	default:
		return false
	}
}
