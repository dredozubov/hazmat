package llmproxyadapter

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"hazmat/harnesses"
	"hazmat/llmproxy"
)

func TestPlanEnvHermesGolden(t *testing.T) {
	plan, err := PlanEnv(Request{
		Harness:              harnesses.Hermes,
		ProxyBaseURL:         "http://127.0.0.1:9817",
		SessionToken:         "local-session-token",
		ModelUpdatesRequired: true,
		ProviderEnv: map[string]string{
			"ANTHROPIC_API_KEY": "provider-anthropic-secret",
			"OPENAI_API_KEY":    "provider-openai-secret",
		},
		AdditionalEnv: map[string]string{
			"ANTHROPIC_API_KEY": "must-not-win",
			"HERMES_HOME":       "/Users/agent/.hazmat/hermes/projects/example",
			"OPENAI_API_KEY":    "must-not-win",
		},
	})
	if err != nil {
		t.Fatalf("PlanEnv: %v", err)
	}

	wantEnv := []string{
		"OPENAI_BASE_URL=http://127.0.0.1:9817",
		"OPENAI_API_KEY=local-session-token",
		"HERMES_HOME=/Users/agent/.hazmat/hermes/projects/example",
	}
	if !slices.Equal(plan.EnvPairs(), wantEnv) {
		t.Fatalf("env = %v, want %v", plan.EnvPairs(), wantEnv)
	}
	if plan.Harness != harnesses.Hermes ||
		plan.CredentialMode != llmproxy.CredentialModeProxySessionToken ||
		!plan.ModelUpdatesRequired ||
		plan.HostProfileImported ||
		plan.ProviderCredentialInjected {
		t.Fatalf("plan metadata = %+v", plan)
	}
	if !strings.Contains(plan.Justification, "managed HERMES_HOME") ||
		!strings.Contains(plan.Justification, "does not import host ~/.hermes") {
		t.Fatalf("justification = %q", plan.Justification)
	}
	wantExcluded := []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}
	if !slices.Equal(plan.ExcludedProviderEnv, wantExcluded) {
		t.Fatalf("excluded provider env = %v, want %v", plan.ExcludedProviderEnv, wantExcluded)
	}

	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	const wantJSON = `{
  "harness": "hermes",
  "justification": "Hermes is the first API proxy adapter because Hazmat already runs it as a foreground process with a managed HERMES_HOME, and Hermes v1 does not import host ~/.hermes profile state.",
  "env": [
    "OPENAI_BASE_URL=http://127.0.0.1:9817",
    "OPENAI_API_KEY=local-session-token",
    "HERMES_HOME=/Users/agent/.hazmat/hermes/projects/example"
  ],
  "credential_mode": "proxy-session-token",
  "model_updates_required": true,
  "host_profile_imported": false,
  "provider_credential_injected": false,
  "excluded_provider_env": [
    "ANTHROPIC_API_KEY",
    "OPENAI_API_KEY"
  ],
  "redactions": [
    {
      "field": "OPENAI_API_KEY",
      "kind": "token"
    }
  ]
}`
	if string(raw) != wantJSON {
		t.Fatalf("plan JSON mismatch:\n%s", raw)
	}
	for _, secret := range []string{"provider-anthropic-secret", "provider-openai-secret", "must-not-win"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("plan leaked %q: %s", secret, raw)
		}
	}
}

func TestPlanEnvRejectsUnsupportedHarness(t *testing.T) {
	_, err := PlanEnv(Request{
		Harness:      harnesses.Codex,
		ProxyBaseURL: "http://127.0.0.1:9817",
		SessionToken: "local-session-token",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported API proxy harness") {
		t.Fatalf("PlanEnv err = %v, want unsupported harness", err)
	}
}

func TestPlanEnvValidatesTypedProxyInputs(t *testing.T) {
	cases := []struct {
		name    string
		request Request
		want    string
	}{
		{
			name: "missing harness",
			request: Request{
				ProxyBaseURL: "http://127.0.0.1:9817",
				SessionToken: "local-session-token",
			},
			want: "harness is required",
		},
		{
			name: "missing proxy base URL",
			request: Request{
				Harness:      harnesses.Hermes,
				SessionToken: "local-session-token",
			},
			want: "proxy base URL is required",
		},
		{
			name: "missing session token",
			request: Request{
				Harness:      harnesses.Hermes,
				ProxyBaseURL: "http://127.0.0.1:9817",
			},
			want: "session token is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PlanEnv(tc.request)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PlanEnv err = %v, want %q", err, tc.want)
			}
		})
	}
}
