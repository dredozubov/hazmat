package llmproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"hazmat/proxyruntime"
	"hazmat/sessionbackend"
)

func TestProxyForwardsOpenAIRoutesToFakeUpstream(t *testing.T) {
	var upstreamRequests []capturedRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
			return
		}
		upstreamRequests = append(upstreamRequests, capturedRequest{
			method:      r.Method,
			path:        r.URL.Path,
			query:       r.URL.RawQuery,
			body:        string(body),
			contentType: r.Header.Get("Content-Type"),
			auth:        r.Header.Get("Authorization"),
			cookie:      r.Header.Get("Cookie"),
			session:     r.Header.Get("X-Hazmat-Session-Token"),
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	proxy := newTestProxy(t, upstream.URL, nil)

	first := httptest.NewRequest(http.MethodPost, "/v1/responses?trace=1", strings.NewReader(`{"input":"hello"}`))
	first.Header.Set("Authorization", "Bearer token-1")
	first.Header.Set("Cookie", "sid=secret")
	first.Header.Set("Content-Type", "application/json")
	firstResult := httptest.NewRecorder()
	proxy.ServeHTTP(firstResult, first)

	second := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	second.Header.Set("X-Hazmat-Session-Token", "token-1")
	second.Header.Set("Content-Type", "application/json")
	secondResult := httptest.NewRecorder()
	proxy.ServeHTTP(secondResult, second)

	if firstResult.Code != http.StatusOK || secondResult.Code != http.StatusOK {
		t.Fatalf("status codes = %d, %d; bodies = %q / %q", firstResult.Code, secondResult.Code, firstResult.Body.String(), secondResult.Body.String())
	}
	if got := strings.TrimSpace(firstResult.Body.String()); got != `{"ok":true}` {
		t.Fatalf("first body = %q", got)
	}
	want := []capturedRequest{
		{method: http.MethodPost, path: "/v1/responses", query: "trace=1", body: `{"input":"hello"}`, contentType: "application/json"},
		{method: http.MethodPost, path: "/v1/chat/completions", body: `{"messages":[]}`, contentType: "application/json"},
	}
	if len(upstreamRequests) != len(want) {
		t.Fatalf("upstream requests = %+v, want %d", upstreamRequests, len(want))
	}
	for i := range want {
		if upstreamRequests[i] != want[i] {
			t.Fatalf("upstream request %d = %+v, want %+v", i, upstreamRequests[i], want[i])
		}
	}
}

func TestProxyPreservesSSEStream(t *testing.T) {
	var events []proxyruntime.Event
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: one\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(upstream.Close)

	proxy := newTestProxy(t, upstream.URL, &events)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Header.Set("Authorization", "Bearer token-1")
	res := httptest.NewRecorder()

	proxy.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", res.Code, res.Body.String())
	}
	if !isEventStream(res.Header().Get("Content-Type")) {
		t.Fatalf("content-type = %q, want event-stream", res.Header().Get("Content-Type"))
	}
	if got, want := res.Body.String(), "data: one\n\ndata: [DONE]\n\n"; got != want {
		t.Fatalf("stream body = %q, want %q", got, want)
	}
	gotOperations := eventOperations(events)
	wantOperations := []string{"POST /v1/chat/completions", "stream:start", "stream:end"}
	if !slices.Equal(gotOperations, wantOperations) {
		t.Fatalf("operations = %v, want %v; events=%+v", gotOperations, wantOperations, events)
	}
}

func TestProxyRejectsMissingInvalidTokenAndUnsupportedEndpoints(t *testing.T) {
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	proxy := newTestProxy(t, upstream.URL, nil)
	cases := []struct {
		name   string
		method string
		path   string
		token  string
		status int
	}{
		{name: "missing token", method: http.MethodPost, path: "/v1/responses", status: http.StatusUnauthorized},
		{name: "invalid token", method: http.MethodPost, path: "/v1/responses", token: "wrong", status: http.StatusUnauthorized},
		{name: "unsupported path", method: http.MethodPost, path: "/v1/models", token: "token-1", status: http.StatusNotFound},
		{name: "unsupported method", method: http.MethodGet, path: "/v1/responses", token: "token-1", status: http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			res := httptest.NewRecorder()
			proxy.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%q", res.Code, tc.status, res.Body.String())
			}
		})
	}
	if upstreamCalls.Load() != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls.Load())
	}
}

func TestProxyEvidenceRedactsCredentialsAndOmitsRequestBody(t *testing.T) {
	var events []proxyruntime.Event
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	proxy := newTestProxy(t, upstream.URL, &events)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"body-secret"}`))
	req.Header.Set("Authorization", "Bearer token-1")
	req.Header.Set("Cookie", "sid=cookie-secret")
	req.Header.Set("X-Hazmat-Session-Token", "token-1")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	proxy.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", res.Code, res.Body.String())
	}
	if len(events) == 0 {
		t.Fatal("no events emitted")
	}
	event := events[0]
	for _, key := range []string{"authorization", "cookie", "session_token"} {
		if event.Attributes[key] != proxyruntime.RedactedValue {
			t.Fatalf("attribute %s = %q, want redacted; event=%+v", key, event.Attributes[key], event)
		}
	}
	raw, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	for _, secret := range []string{"token-1", "cookie-secret", "body-secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("event JSON leaked %q: %s", secret, raw)
		}
	}
}

func newTestProxy(t *testing.T, upstreamURL string, events *[]proxyruntime.Event) *Proxy {
	t.Helper()
	proxy, err := New(Config{
		SessionID:       "session-1",
		SessionToken:    "token-1",
		Downstream:      proxyruntime.DownstreamIdentity{ID: "fake-client", Endpoint: "http://127.0.0.1:12345"},
		Backend:         sessionbackend.KindDockerSandbox,
		UpstreamBaseURL: upstreamURL,
		Events: func(event proxyruntime.Event) {
			if events != nil {
				*events = append(*events, event)
			}
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return proxy
}

func eventOperations(events []proxyruntime.Event) []string {
	operations := make([]string, 0, len(events))
	for _, event := range events {
		operations = append(operations, event.Operation)
	}
	return operations
}

type capturedRequest struct {
	method      string
	path        string
	query       string
	body        string
	contentType string
	auth        string
	cookie      string
	session     string
}
