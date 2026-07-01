package proxyruntime

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"hazmat/sessionbackend"
)

func TestNewEventRedactsSensitiveAttributes(t *testing.T) {
	event := NewEvent(EventInput{
		Timestamp: time.Date(2026, 7, 1, 12, 0, 0, 0, time.FixedZone("test", 3600)),
		SessionID: " session-1 ",
		ProxyKind: ProxyKindMCPStdio,
		Downstream: DownstreamIdentity{
			ID:      " fake-server ",
			Command: " /bin/fake ",
		},
		Backend:    sessionbackend.KindDarwinNative,
		AttachKind: AttachKindStdio,
		Direction:  DirectionInbound,
		Operation:  " tools/call:read_secret ",
		Decision:   DecisionAllow,
		Attributes: map[string]string{
			"Authorization": "Bearer sk-live",
			"session_token": "token-value",
			"client_secret": "secret-value",
			"password":      "password-value",
			"Cookie":        "a=b",
			"X-Api-Key":     "api-key-value",
			"safe":          "visible",
		},
	})

	if event.Timestamp.Location() != time.UTC {
		t.Fatalf("Timestamp location = %v, want UTC", event.Timestamp.Location())
	}
	if event.SessionID != "session-1" || event.Downstream.ID != "fake-server" || event.Operation != "tools/call:read_secret" {
		t.Fatalf("event normalization failed: %+v", event)
	}
	if event.Attributes["safe"] != "visible" {
		t.Fatalf("safe attribute = %q, want visible", event.Attributes["safe"])
	}
	for _, field := range []string{"Authorization", "session_token", "client_secret", "password", "Cookie", "X-Api-Key"} {
		if event.Attributes[field] != RedactedValue {
			t.Fatalf("field %s = %q, want redacted", field, event.Attributes[field])
		}
	}
	wantKinds := []RedactionKind{
		RedactionToken,
		RedactionCookie,
		RedactionToken,
		RedactionSecret,
		RedactionPassword,
		RedactionToken,
	}
	var gotKinds []RedactionKind
	for _, marker := range event.Redactions {
		gotKinds = append(gotKinds, marker.Kind)
	}
	if !slices.Equal(gotKinds, wantKinds) {
		t.Fatalf("redaction kinds = %v, want %v; markers=%+v", gotKinds, wantKinds, event.Redactions)
	}

	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	for _, secret := range []string{"sk-live", "token-value", "secret-value", "password-value", "api-key-value", "a=b"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("event JSON leaked %q: %s", secret, raw)
		}
	}
}

func TestPolicyEvaluateSupportsTokenDownstreamToolAndRouteRules(t *testing.T) {
	policy := Policy{
		DefaultDecision:          DecisionDeny,
		RequireLocalSessionToken: true,
		Downstreams: []DownstreamRule{{
			Identity: "fake",
			Decision: DecisionAllow,
			Reason:   "known downstream",
		}},
		MCPTools: []MCPToolRule{{
			DownstreamIdentity: "fake",
			ToolName:           "delete_all",
			Decision:           DecisionDeny,
			Reason:             "dangerous tool",
		}},
		HTTPRoutes: []HTTPRouteRule{{
			Method:   "POST",
			Path:     "/v1/responses",
			Decision: DecisionAllow,
			Reason:   "supported route",
		}},
	}

	decision := policy.Evaluate(PolicyRequest{
		DownstreamIdentity: "fake",
		MCPToolName:        "delete_all",
	})
	if decision.Decision != DecisionDeny || decision.Rule != "local-session-token" {
		t.Fatalf("missing-token decision = %+v, want token denial", decision)
	}

	decision = policy.Evaluate(PolicyRequest{
		DownstreamIdentity:       "fake",
		MCPToolName:              "delete_all",
		LocalSessionTokenPresent: true,
	})
	if decision.Decision != DecisionDeny || decision.Rule != "mcp-tool" || decision.Reason != "dangerous tool" {
		t.Fatalf("tool decision = %+v, want tool denial", decision)
	}

	decision = policy.Evaluate(PolicyRequest{
		DownstreamIdentity:       "other",
		HTTPMethod:               "post",
		HTTPPath:                 "/v1/responses",
		LocalSessionTokenPresent: true,
	})
	if decision.Decision != DecisionAllow || decision.Rule != "http-route" {
		t.Fatalf("route decision = %+v, want route allow", decision)
	}

	decision = policy.Evaluate(PolicyRequest{
		DownstreamIdentity:       "fake",
		LocalSessionTokenPresent: true,
	})
	if decision.Decision != DecisionAllow || decision.Rule != "downstream" {
		t.Fatalf("downstream decision = %+v, want downstream allow", decision)
	}

	decision = policy.Evaluate(PolicyRequest{
		DownstreamIdentity:       "unknown",
		LocalSessionTokenPresent: true,
	})
	if decision.Decision != DecisionDeny || decision.Rule != "default" {
		t.Fatalf("default decision = %+v, want default deny", decision)
	}
}
