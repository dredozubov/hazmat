// Package llmproxy implements OpenAI-compatible HTTP proxy mediation.
package llmproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"hazmat/proxyruntime"
	"hazmat/sessionbackend"
)

var supportedRoutes = map[string]bool{
	"POST /v1/responses":        true,
	"POST /v1/chat/completions": true,
	"POST /v1/embeddings":       true,
}

type Config struct {
	SessionID       string
	SessionToken    string
	Downstream      proxyruntime.DownstreamIdentity
	Backend         sessionbackend.Kind
	UpstreamBaseURL string
	Client          *http.Client
	Policy          proxyruntime.Policy
	Events          proxyruntime.EventSink
}

type Proxy struct {
	sessionID    string
	sessionToken string
	downstream   proxyruntime.DownstreamIdentity
	backend      sessionbackend.Kind
	upstream     *url.URL
	client       *http.Client
	policy       proxyruntime.Policy
	events       proxyruntime.EventSink
}

func New(config Config) (*Proxy, error) {
	sessionToken := strings.TrimSpace(config.SessionToken)
	if sessionToken == "" {
		return nil, errors.New("llmproxy: session token is required")
	}
	upstream, err := url.Parse(strings.TrimSpace(config.UpstreamBaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse upstream base URL: %w", err)
	}
	if upstream.Scheme != "http" && upstream.Scheme != "https" || upstream.Host == "" {
		return nil, errors.New("llmproxy: upstream base URL must be http or https")
	}
	client := config.Client
	if client == nil {
		client = http.DefaultClient
	}
	policy := config.Policy
	if emptyPolicy(policy) {
		policy = proxyruntime.Policy{DefaultDecision: proxyruntime.DecisionAllow}
	}
	return &Proxy{
		sessionID:    strings.TrimSpace(config.SessionID),
		sessionToken: sessionToken,
		downstream:   config.Downstream,
		backend:      config.Backend,
		upstream:     upstream,
		client:       client,
		policy:       policy,
		events:       config.Events,
	}, nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	operation := operationForRequest(r)
	attrs := requestAttributes(r)
	token := requestSessionToken(r)
	tokenPresent := token != ""
	if token != p.sessionToken {
		p.emit(r.Context(), operation, proxyruntime.DirectionInbound, proxyruntime.DecisionDeny, "local session token required", attrs)
		writeError(w, http.StatusUnauthorized, "invalid or missing session token")
		return
	}
	if !supportedRoutes[operation] {
		p.emit(r.Context(), operation, proxyruntime.DirectionInbound, proxyruntime.DecisionDeny, "unsupported endpoint", attrs)
		writeError(w, http.StatusNotFound, "unsupported endpoint")
		return
	}
	decision := p.policy.Evaluate(proxyruntime.PolicyRequest{
		DownstreamIdentity:       p.downstream.ID,
		HTTPMethod:               r.Method,
		HTTPPath:                 r.URL.Path,
		LocalSessionTokenPresent: tokenPresent,
	})
	if decision.Decision == proxyruntime.DecisionDeny {
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "request denied by proxy policy"
		}
		p.emit(r.Context(), operation, proxyruntime.DirectionInbound, proxyruntime.DecisionDeny, reason, attrs)
		writeError(w, http.StatusForbidden, reason)
		return
	}
	p.emit(r.Context(), operation, proxyruntime.DirectionInbound, proxyruntime.DecisionAllow, decision.Reason, attrs)

	upstreamReq, err := p.newUpstreamRequest(r)
	if err != nil {
		p.emit(r.Context(), "upstream:error", proxyruntime.DirectionOutbound, proxyruntime.DecisionError, err.Error(), nil)
		writeError(w, http.StatusBadGateway, "build upstream request failed")
		return
	}
	resp, err := p.client.Do(upstreamReq)
	if err != nil {
		p.emit(r.Context(), "upstream:error", proxyruntime.DirectionOutbound, proxyruntime.DecisionError, err.Error(), nil)
		writeError(w, http.StatusBadGateway, "upstream request failed")
		return
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if isEventStream(resp.Header.Get("Content-Type")) {
		p.emit(r.Context(), "stream:start", proxyruntime.DirectionOutbound, proxyruntime.DecisionObserve, "", nil)
		err = copyStreaming(w, resp.Body)
		if err != nil {
			p.emit(r.Context(), "stream:error", proxyruntime.DirectionOutbound, proxyruntime.DecisionError, err.Error(), nil)
			return
		}
		p.emit(r.Context(), "stream:end", proxyruntime.DirectionOutbound, proxyruntime.DecisionObserve, "", nil)
		return
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		p.emit(r.Context(), "upstream:error", proxyruntime.DirectionOutbound, proxyruntime.DecisionError, err.Error(), nil)
	}
}

func (p *Proxy) newUpstreamRequest(r *http.Request) (*http.Request, error) {
	target := *p.upstream
	target.Path = joinURLPath(p.upstream.Path, r.URL.Path)
	target.RawQuery = r.URL.RawQuery
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		return nil, err
	}
	copyRequestHeaders(req.Header, r.Header)
	req.Host = target.Host
	return req, nil
}

func (p *Proxy) emit(_ context.Context, operation string, direction proxyruntime.Direction, decision proxyruntime.Decision, reason string, attrs map[string]string) {
	if p.events == nil {
		return
	}
	p.events(proxyruntime.NewEvent(proxyruntime.EventInput{
		SessionID:  p.sessionID,
		ProxyKind:  proxyruntime.ProxyKindLLMHTTP,
		Downstream: p.downstream,
		Backend:    p.backend,
		AttachKind: proxyruntime.AttachKindLocalHTTP,
		Direction:  direction,
		Operation:  operation,
		Decision:   decision,
		Reason:     reason,
		Attributes: attrs,
	}))
}

func operationForRequest(r *http.Request) string {
	return strings.ToUpper(strings.TrimSpace(r.Method)) + " " + r.URL.Path
}

func requestAttributes(r *http.Request) map[string]string {
	attrs := map[string]string{
		"method":       strings.ToUpper(strings.TrimSpace(r.Method)),
		"path":         r.URL.Path,
		"content_type": r.Header.Get("Content-Type"),
	}
	if value := r.Header.Get("Authorization"); value != "" {
		attrs["authorization"] = value
	}
	if value := r.Header.Get("Cookie"); value != "" {
		attrs["cookie"] = value
	}
	if value := r.Header.Get("X-Hazmat-Session-Token"); value != "" {
		attrs["session_token"] = value
	}
	return attrs
}

func bearerToken(header string) string {
	fields := strings.Fields(strings.TrimSpace(header))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return ""
	}
	return fields[1]
}

func requestSessionToken(r *http.Request) string {
	if token := bearerToken(r.Header.Get("Authorization")); token != "" {
		return token
	}
	return strings.TrimSpace(r.Header.Get("X-Hazmat-Session-Token"))
}

func copyRequestHeaders(dst, src http.Header) {
	for key, values := range src {
		if skipRequestHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		if skipHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func skipRequestHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "cookie", "host", "x-hazmat-session-token", "connection", "proxy-authorization", "proxy-authenticate", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func skipHopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]map[string]string{
		"error": {"message": message},
	})
}

func copyStreaming(w http.ResponseWriter, r io.Reader) error {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func isEventStream(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

func joinURLPath(basePath, requestPath string) string {
	switch {
	case basePath == "" || basePath == "/":
		return requestPath
	case strings.HasSuffix(basePath, "/") && strings.HasPrefix(requestPath, "/"):
		return basePath + strings.TrimPrefix(requestPath, "/")
	case !strings.HasSuffix(basePath, "/") && !strings.HasPrefix(requestPath, "/"):
		return basePath + "/" + requestPath
	default:
		return basePath + requestPath
	}
}

func emptyPolicy(policy proxyruntime.Policy) bool {
	return policy.DefaultDecision == "" &&
		!policy.RequireLocalSessionToken &&
		!policy.RedactBodiesByDefault &&
		len(policy.Downstreams) == 0 &&
		len(policy.MCPTools) == 0 &&
		len(policy.HTTPRoutes) == 0
}
