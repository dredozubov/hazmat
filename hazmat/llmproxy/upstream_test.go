package llmproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"hazmat/proxyruntime"
	"hazmat/sessionbackend"
)

func TestMuginnUpstreamAddsHostSideAuthAndRecordsCredentialMode(t *testing.T) {
	const muginnToken = "muginn-caller-token"
	var upstreamAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth = r.Header.Get("Authorization")
		if got := r.Header.Get("X-Hazmat-Session-Token"); got != "" {
			t.Errorf("upstream received local session token: %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
			return
		}
		if string(body) != `{"input":"hello"}` {
			t.Errorf("upstream body = %q", body)
		}
		_, _ = w.Write([]byte(`{"id":"rsp_fake"}`))
	}))
	t.Cleanup(upstream.Close)

	upstreamConfig, err := NewMuginnUpstream(MuginnUpstreamConfig{
		BaseURL:              upstream.URL,
		CallerToken:          muginnToken,
		ModelUpdatesRequired: true,
	})
	if err != nil {
		t.Fatalf("NewMuginnUpstream: %v", err)
	}
	var events []proxyruntime.Event
	proxy, err := New(Config{
		SessionID:    "session-1",
		SessionToken: "local-token",
		Downstream:   proxyruntime.DownstreamIdentity{ID: "codex"},
		Backend:      sessionbackend.KindDockerSandbox,
		Upstream:     upstreamConfig,
		Events: func(event proxyruntime.Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"hello"}`))
	req.Header.Set("Authorization", "Bearer local-token")
	req.Header.Set("X-Hazmat-Session-Token", "local-token")
	res := httptest.NewRecorder()
	proxy.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", res.Code, res.Body.String())
	}
	if upstreamAuth != "Bearer "+muginnToken {
		t.Fatalf("upstream auth = %q, want Muginn caller token", upstreamAuth)
	}
	plan := proxy.UpstreamPlan()
	if plan.Kind != UpstreamKindMuginn ||
		plan.CredentialMode != CredentialModeMuginnSide ||
		!plan.ModelUpdatesRequired ||
		plan.ProviderCredentialInjected {
		t.Fatalf("upstream plan = %+v", plan)
	}
	rawPlan, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if strings.Contains(string(rawPlan), muginnToken) {
		t.Fatalf("upstream plan leaked Muginn token: %s", rawPlan)
	}
	if got := eventOperations(events); !slices.Equal(got, []string{"POST /v1/responses"}) {
		t.Fatalf("events = %v, want request event", got)
	}
}

func TestMuginnUpstreamFailureIsBoundedAndRedacted(t *testing.T) {
	const muginnToken = "muginn-caller-secret"
	const providerSecret = "provider-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream failed with " + muginnToken + " and " + providerSecret))
	}))
	t.Cleanup(upstream.Close)

	upstreamConfig, err := NewMuginnUpstream(MuginnUpstreamConfig{
		BaseURL:     upstream.URL,
		CallerToken: muginnToken,
	})
	if err != nil {
		t.Fatalf("NewMuginnUpstream: %v", err)
	}
	var events []proxyruntime.Event
	proxy, err := New(Config{
		SessionID:    "session-1",
		SessionToken: "local-token",
		Downstream:   proxyruntime.DownstreamIdentity{ID: "codex"},
		Backend:      sessionbackend.KindDockerSandbox,
		Upstream:     upstreamConfig,
		Events: func(event proxyruntime.Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	req.Header.Set("Authorization", "Bearer local-token")
	res := httptest.NewRecorder()
	proxy.ServeHTTP(res, req)

	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body=%q", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), muginnToken) || strings.Contains(res.Body.String(), providerSecret) {
		t.Fatalf("bounded error leaked secret: %q", res.Body.String())
	}
	rawEvents, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	for _, secret := range []string{muginnToken, providerSecret} {
		if strings.Contains(string(rawEvents), secret) {
			t.Fatalf("events leaked %q: %s", secret, rawEvents)
		}
	}
	if got := eventOperations(events); !slices.Equal(got, []string{"POST /v1/chat/completions", "upstream:error"}) {
		t.Fatalf("events = %v, want request plus upstream error", got)
	}
}

func TestPlanOpenAIClientEnvUsesSessionTokenAndExcludesProviderKeys(t *testing.T) {
	plan, err := PlanOpenAIClientEnv(ClientEnvRequest{
		ProxyBaseURL:         "http://127.0.0.1:9191",
		SessionToken:         "local-session-token",
		ModelUpdatesRequired: true,
		ProviderEnv: map[string]string{
			"OPENAI_API_KEY":     "provider-openai-secret",
			"ANTHROPIC_API_KEY":  "provider-anthropic-secret",
			"GEMINI_API_KEY":     "provider-gemini-secret",
			"OPENROUTER_API_KEY": "provider-openrouter-secret",
		},
		AdditionalEnv: map[string]string{
			"OPENAI_API_KEY": "must-not-win",
			"TRACE":          "1",
		},
	})
	if err != nil {
		t.Fatalf("PlanOpenAIClientEnv: %v", err)
	}
	wantEnv := []string{
		"OPENAI_BASE_URL=http://127.0.0.1:9191",
		"OPENAI_API_KEY=local-session-token",
		"TRACE=1",
	}
	if !slices.Equal(plan.EnvPairs(), wantEnv) {
		t.Fatalf("env = %v, want %v", plan.EnvPairs(), wantEnv)
	}
	if plan.CredentialMode != CredentialModeProxySessionToken || !plan.ModelUpdatesRequired {
		t.Fatalf("plan metadata = %+v", plan)
	}
	wantExcluded := []string{"ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY"}
	if !slices.Equal(plan.ExcludedProviderEnv, wantExcluded) {
		t.Fatalf("excluded provider env = %v, want %v", plan.ExcludedProviderEnv, wantExcluded)
	}
	rawPlan, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	for _, secret := range []string{"provider-openai-secret", "provider-anthropic-secret", "provider-gemini-secret", "provider-openrouter-secret", "must-not-win"} {
		if strings.Contains(string(rawPlan), secret) {
			t.Fatalf("client env plan leaked %q: %s", secret, rawPlan)
		}
	}
}
