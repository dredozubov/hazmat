package llmproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hazmat/proxyruntime"
	"hazmat/sessionbackend"
)

func TestProxyClosesUpstreamResponseBodies(t *testing.T) {
	successBody := &trackingReadCloser{Reader: strings.NewReader(`{"ok":true}`)}
	successProxy, err := New(Config{
		SessionID:       "session-1",
		SessionToken:    "local-token",
		Downstream:      proxyruntime.DownstreamIdentity{ID: "test-harness"},
		Backend:         sessionbackend.KindDockerSandbox,
		UpstreamBaseURL: "http://upstream.example",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				Status:     "200 OK",
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       successBody,
			}, nil
		})},
	})
	if err != nil {
		t.Fatalf("New success proxy: %v", err)
	}
	successReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{}`))
	successReq.Header.Set("Authorization", "Bearer local-token")
	successRes := httptest.NewRecorder()
	successProxy.ServeHTTP(successRes, successReq)
	if successRes.Code != http.StatusOK {
		t.Fatalf("success status = %d, body=%q", successRes.Code, successRes.Body.String())
	}
	if !successBody.closed {
		t.Fatal("successful upstream response body was not closed")
	}

	failureBody := &trackingReadCloser{Reader: strings.NewReader("provider-secret")}
	upstream, err := NewBearerUpstream(BearerUpstreamConfig{
		BaseURL:     "http://facade.example",
		BearerToken: "upstream-token",
	})
	if err != nil {
		t.Fatalf("NewBearerUpstream: %v", err)
	}
	failureProxy, err := New(Config{
		SessionID:    "session-1",
		SessionToken: "local-token",
		Downstream:   proxyruntime.DownstreamIdentity{ID: "test-harness"},
		Backend:      sessionbackend.KindDockerSandbox,
		Upstream:     upstream,
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				Status:     "502 Bad Gateway",
				StatusCode: http.StatusBadGateway,
				Header:     make(http.Header),
				Body:       failureBody,
			}, nil
		})},
	})
	if err != nil {
		t.Fatalf("New failure proxy: %v", err)
	}
	failureReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{}`))
	failureReq.Header.Set("Authorization", "Bearer local-token")
	failureRes := httptest.NewRecorder()
	failureProxy.ServeHTTP(failureRes, failureReq)
	if failureRes.Code != http.StatusBadGateway {
		t.Fatalf("failure status = %d, body=%q", failureRes.Code, failureRes.Body.String())
	}
	if strings.Contains(failureRes.Body.String(), "provider-secret") {
		t.Fatalf("sanitized failure leaked upstream body: %q", failureRes.Body.String())
	}
	if !failureBody.closed {
		t.Fatal("sanitized upstream failure body was not closed")
	}
}

type trackingReadCloser struct {
	*strings.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

var _ io.ReadCloser = (*trackingReadCloser)(nil)
