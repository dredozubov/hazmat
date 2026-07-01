// Package llmproxyadapter renders harness-specific client configuration for
// Hazmat's local OpenAI-compatible API proxy.
package llmproxyadapter

import (
	"errors"
	"fmt"
	"strings"

	"hazmat/harnesses"
	"hazmat/llmproxy"
)

const HermesProxyJustification = "Hermes is the first API proxy adapter because Hazmat already runs it as a foreground process with a managed HERMES_HOME, and Hermes v1 does not import host ~/.hermes profile state."

type Request struct {
	Harness              harnesses.ID
	ProxyBaseURL         string
	SessionToken         string
	ProviderEnv          map[string]string
	AdditionalEnv        map[string]string
	ModelUpdatesRequired bool
}

type Plan struct {
	Harness                    harnesses.ID             `json:"harness"`
	Justification              string                   `json:"justification"`
	Env                        []string                 `json:"env"`
	CredentialMode             llmproxy.CredentialMode  `json:"credential_mode"`
	ModelUpdatesRequired       bool                     `json:"model_updates_required"`
	HostProfileImported        bool                     `json:"host_profile_imported"`
	ProviderCredentialInjected bool                     `json:"provider_credential_injected"`
	ExcludedProviderEnv        []string                 `json:"excluded_provider_env,omitempty"`
	Redactions                 []llmproxy.RedactionNote `json:"redactions,omitempty"`
}

func PlanEnv(request Request) (Plan, error) {
	harness := normalizeHarness(request.Harness)
	if harness == "" {
		return Plan{}, errors.New("llmproxyadapter: harness is required")
	}
	if harness != harnesses.Hermes {
		return Plan{}, fmt.Errorf("llmproxyadapter: unsupported API proxy harness %q", harness)
	}
	clientPlan, err := llmproxy.PlanOpenAIClientEnv(llmproxy.ClientEnvRequest{
		ProxyBaseURL:         request.ProxyBaseURL,
		SessionToken:         request.SessionToken,
		ProviderEnv:          request.ProviderEnv,
		AdditionalEnv:        request.AdditionalEnv,
		ModelUpdatesRequired: request.ModelUpdatesRequired,
	})
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Harness:                    harness,
		Justification:              HermesProxyJustification,
		Env:                        clientPlan.EnvPairs(),
		CredentialMode:             clientPlan.CredentialMode,
		ModelUpdatesRequired:       clientPlan.ModelUpdatesRequired,
		HostProfileImported:        false,
		ProviderCredentialInjected: false,
		ExcludedProviderEnv:        append([]string(nil), clientPlan.ExcludedProviderEnv...),
		Redactions:                 append([]llmproxy.RedactionNote(nil), clientPlan.Redactions...),
	}, nil
}

func (p Plan) EnvPairs() []string {
	if len(p.Env) == 0 {
		return nil
	}
	out := make([]string, len(p.Env))
	copy(out, p.Env)
	return out
}

func normalizeHarness(harness harnesses.ID) harnesses.ID {
	return harnesses.ID(strings.TrimSpace(string(harness)))
}
