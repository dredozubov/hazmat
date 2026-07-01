// Package proxyruntime defines protocol-neutral proxy evidence and policy DTOs.
//
// Proxy evidence describes what a proxy observed and decided. It is not runtime
// authority and does not replace the session contract enforced by a backend.
package proxyruntime

import (
	"sort"
	"strings"
	"time"

	"hazmat/sessionbackend"
)

type ProxyKind string

const (
	ProxyKindMCPStdio ProxyKind = "mcp-stdio"
	ProxyKindMCPHTTP  ProxyKind = "mcp-http"
	ProxyKindLLMHTTP  ProxyKind = "llm-http"
)

type AttachKind string

const (
	AttachKindStdio      AttachKind = "stdio"
	AttachKindLocalHTTP  AttachKind = "local-http"
	AttachKindUnixSocket AttachKind = "unix-socket"
	AttachKindRemoteHTTP AttachKind = "remote-http"
)

type Direction string

const (
	DirectionInbound   Direction = "inbound"
	DirectionOutbound  Direction = "outbound"
	DirectionLifecycle Direction = "lifecycle"
)

type Decision string

const (
	DecisionAllow   Decision = "allow"
	DecisionDeny    Decision = "deny"
	DecisionObserve Decision = "observe"
	DecisionError   Decision = "error"
)

type RedactionKind string

const (
	RedactionToken    RedactionKind = "token"
	RedactionSecret   RedactionKind = "secret"
	RedactionPassword RedactionKind = "password"
	RedactionCookie   RedactionKind = "cookie"
)

const RedactedValue = "[redacted]"

type DownstreamIdentity struct {
	ID       string `json:"id"`
	Command  string `json:"command,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

type RedactionMarker struct {
	Field string        `json:"field"`
	Kind  RedactionKind `json:"kind"`
}

type EventInput struct {
	Timestamp  time.Time
	SessionID  string
	ProxyKind  ProxyKind
	Downstream DownstreamIdentity
	Backend    sessionbackend.Kind
	AttachKind AttachKind
	Direction  Direction
	Operation  string
	Decision   Decision
	Reason     string
	Attributes map[string]string
	Redactions []RedactionMarker
}

type Event struct {
	Timestamp  time.Time           `json:"timestamp"`
	SessionID  string              `json:"session_id"`
	ProxyKind  ProxyKind           `json:"proxy_kind"`
	Downstream DownstreamIdentity  `json:"downstream"`
	Backend    sessionbackend.Kind `json:"backend"`
	AttachKind AttachKind          `json:"attach_kind"`
	Direction  Direction           `json:"direction"`
	Operation  string              `json:"operation"`
	Decision   Decision            `json:"decision"`
	Reason     string              `json:"reason,omitempty"`
	Attributes map[string]string   `json:"attributes,omitempty"`
	Redactions []RedactionMarker   `json:"redactions,omitempty"`
}

func NewEvent(input EventInput) Event {
	timestamp := input.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	attributes, redactions := RedactFields(input.Attributes)
	redactions = append(redactions, copyRedactions(input.Redactions)...)
	sortRedactions(redactions)
	return Event{
		Timestamp:  timestamp.UTC(),
		SessionID:  strings.TrimSpace(input.SessionID),
		ProxyKind:  input.ProxyKind,
		Downstream: normalizeDownstream(input.Downstream),
		Backend:    input.Backend,
		AttachKind: input.AttachKind,
		Direction:  input.Direction,
		Operation:  strings.TrimSpace(input.Operation),
		Decision:   normalizeDecision(input.Decision),
		Reason:     strings.TrimSpace(input.Reason),
		Attributes: attributes,
		Redactions: redactions,
	}
}

func RedactFields(values map[string]string) (map[string]string, []RedactionMarker) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	var redactions []RedactionMarker
	for key, value := range values {
		if kind, ok := SensitiveFieldKind(key); ok {
			out[key] = RedactedValue
			redactions = append(redactions, RedactionMarker{Field: key, Kind: kind})
			continue
		}
		out[key] = value
	}
	sortRedactions(redactions)
	return out, redactions
}

func SensitiveFieldKind(name string) (RedactionKind, bool) {
	field := normalizedFieldName(name)
	switch {
	case strings.Contains(field, "cookie"):
		return RedactionCookie, true
	case strings.Contains(field, "password") || strings.Contains(field, "passwd") || strings.Contains(field, "pwd"):
		return RedactionPassword, true
	case strings.Contains(field, "secret"):
		return RedactionSecret, true
	case strings.Contains(field, "authorization") ||
		strings.Contains(field, "bearer") ||
		strings.Contains(field, "token") ||
		strings.Contains(field, "apikey"):
		return RedactionToken, true
	default:
		return "", false
	}
}

type Policy struct {
	DefaultDecision          Decision         `json:"default_decision,omitempty"`
	Downstreams              []DownstreamRule `json:"downstreams,omitempty"`
	MCPTools                 []MCPToolRule    `json:"mcp_tools,omitempty"`
	HTTPRoutes               []HTTPRouteRule  `json:"http_routes,omitempty"`
	RequireLocalSessionToken bool             `json:"require_local_session_token,omitempty"`
	RedactBodiesByDefault    bool             `json:"redact_bodies_by_default,omitempty"`
}

type DownstreamRule struct {
	Identity string   `json:"identity"`
	Decision Decision `json:"decision"`
	Reason   string   `json:"reason,omitempty"`
}

type MCPToolRule struct {
	DownstreamIdentity string   `json:"downstream_identity,omitempty"`
	ToolName           string   `json:"tool_name"`
	Decision           Decision `json:"decision"`
	Reason             string   `json:"reason,omitempty"`
}

type HTTPRouteRule struct {
	DownstreamIdentity string   `json:"downstream_identity,omitempty"`
	Method             string   `json:"method"`
	Path               string   `json:"path"`
	Decision           Decision `json:"decision"`
	Reason             string   `json:"reason,omitempty"`
}

type PolicyRequest struct {
	DownstreamIdentity       string
	MCPToolName              string
	HTTPMethod               string
	HTTPPath                 string
	LocalSessionTokenPresent bool
}

type PolicyDecision struct {
	Decision Decision `json:"decision"`
	Reason   string   `json:"reason,omitempty"`
	Rule     string   `json:"rule,omitempty"`
}

func (p Policy) Evaluate(request PolicyRequest) PolicyDecision {
	if p.RequireLocalSessionToken && !request.LocalSessionTokenPresent {
		return PolicyDecision{Decision: DecisionDeny, Reason: "local session token required", Rule: "local-session-token"}
	}
	if request.MCPToolName != "" {
		for _, rule := range p.MCPTools {
			if rule.ToolName == request.MCPToolName && identityMatches(rule.DownstreamIdentity, request.DownstreamIdentity) {
				return PolicyDecision{Decision: normalizeDecision(rule.Decision), Reason: strings.TrimSpace(rule.Reason), Rule: "mcp-tool"}
			}
		}
	}
	if request.HTTPPath != "" {
		method := strings.ToUpper(strings.TrimSpace(request.HTTPMethod))
		for _, rule := range p.HTTPRoutes {
			if strings.ToUpper(strings.TrimSpace(rule.Method)) == method &&
				rule.Path == request.HTTPPath &&
				identityMatches(rule.DownstreamIdentity, request.DownstreamIdentity) {
				return PolicyDecision{Decision: normalizeDecision(rule.Decision), Reason: strings.TrimSpace(rule.Reason), Rule: "http-route"}
			}
		}
	}
	for _, rule := range p.Downstreams {
		if rule.Identity == request.DownstreamIdentity {
			return PolicyDecision{Decision: normalizeDecision(rule.Decision), Reason: strings.TrimSpace(rule.Reason), Rule: "downstream"}
		}
	}
	return PolicyDecision{Decision: normalizeDecision(p.DefaultDecision), Rule: "default"}
}

func normalizeDecision(decision Decision) Decision {
	switch decision {
	case DecisionAllow, DecisionDeny, DecisionObserve, DecisionError:
		return decision
	default:
		return DecisionDeny
	}
}

func normalizeDownstream(value DownstreamIdentity) DownstreamIdentity {
	return DownstreamIdentity{
		ID:       strings.TrimSpace(value.ID),
		Command:  strings.TrimSpace(value.Command),
		Endpoint: strings.TrimSpace(value.Endpoint),
	}
}

func identityMatches(ruleIdentity, requestIdentity string) bool {
	return ruleIdentity == "" || ruleIdentity == requestIdentity
}

func normalizedFieldName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range lower {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func copyRedactions(values []RedactionMarker) []RedactionMarker {
	if len(values) == 0 {
		return nil
	}
	out := make([]RedactionMarker, len(values))
	copy(out, values)
	return out
}

func sortRedactions(values []RedactionMarker) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Field != values[j].Field {
			return values[i].Field < values[j].Field
		}
		return values[i].Kind < values[j].Kind
	})
}
