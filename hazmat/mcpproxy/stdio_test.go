package mcpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"hazmat/proxyruntime"
	"hazmat/sessionbackend"
)

func TestStdioProxyForwardsFakeServerAndRecordsMCPOperations(t *testing.T) {
	handle := &fakeProcessHandle{
		stdin:  &bufferWriteCloser{},
		stdout: io.NopCloser(strings.NewReader(responseLine(1, `{"server":"fake"}`))),
		stderr: io.NopCloser(strings.NewReader("server debug\n")),
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var cleanupCalls int

	result, err := (StdioProxy{Starter: &fakeProcessStarter{handle: handle}}).Run(context.Background(), StdioRequest{
		SessionID:  "session-1",
		Downstream: proxyruntime.DownstreamIdentity{ID: "fake-mcp", Command: "fake-server"},
		Backend:    sessionbackend.KindDockerSandbox,
		Spec:       proxyruntime.ProcessSpec{Command: "fake-server", Args: []string{"--stdio"}},
		Policy:     proxyruntime.Policy{DefaultDecision: proxyruntime.DecisionAllow},
		Stdin: strings.NewReader(
			requestLine(1, "initialize", nil) +
				requestLine(2, "tools/list", nil) +
				requestLine(3, "tools/call", map[string]any{"name": "read_file", "arguments": map[string]any{"path": "README.md"}}) +
				requestLine(4, "resources/list", nil) +
				requestLine(5, "resources/read", map[string]any{"uri": "file:///README.md"}) +
				requestLine(6, "prompts/list", nil) +
				requestLine(7, "prompts/get", map[string]any{"name": "review"}) +
				responseLine(99, `{"ok":true}`),
		),
		Stdout:  &stdout,
		Stderr:  &stderr,
		Cleanup: func() { cleanupCalls++ },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Process.Started || handle.killed || !handle.waited || cleanupCalls != 1 {
		t.Fatalf("process result=%+v killed=%v waited=%v cleanup=%d", result.Process, handle.killed, handle.waited, cleanupCalls)
	}
	if !strings.Contains(stdout.String(), `{"server":"fake"}`) {
		t.Fatalf("stdout = %q, want fake server response", stdout.String())
	}
	if strings.Contains(stdout.String(), "server debug") {
		t.Fatalf("stderr leaked into stdout: %q", stdout.String())
	}
	if stderr.String() != "server debug\n" {
		t.Fatalf("stderr = %q, want server debug", stderr.String())
	}
	if !strings.Contains(handle.stdin.String(), `"tools/call"`) || !strings.Contains(handle.stdin.String(), `"read_file"`) {
		t.Fatalf("server stdin missing forwarded tool call: %q", handle.stdin.String())
	}

	gotOperations := protocolOperations(result.Events)
	wantOperations := []string{
		"initialize",
		"tools/list",
		"tools/call:read_file",
		"resources/list",
		"resources/read",
		"prompts/list",
		"prompts/get:review",
	}
	if !slices.Equal(gotOperations, wantOperations) {
		t.Fatalf("operations = %v, want %v; events=%+v", gotOperations, wantOperations, result.Events)
	}
	call := eventByOperation(t, result.Events, "tools/call:read_file")
	if call.Decision != proxyruntime.DecisionAllow || call.Attributes["mcp_tool_name"] != "read_file" {
		t.Fatalf("tool call event = %+v, want allow with tool attr", call)
	}
	resource := eventByOperation(t, result.Events, "resources/read")
	if resource.Attributes["mcp_resource_uri"] != "file:///README.md" {
		t.Fatalf("resource attr = %+v", resource.Attributes)
	}
	prompt := eventByOperation(t, result.Events, "prompts/get:review")
	if prompt.Attributes["mcp_prompt_name"] != "review" {
		t.Fatalf("prompt attr = %+v", prompt.Attributes)
	}
	if _, ok := prompt.Attributes["mcp_tool_name"]; ok {
		t.Fatalf("prompt was classified as tool event: %+v", prompt.Attributes)
	}
}

func TestStdioProxyDenyByToolReturnsJSONRPCErrorWithoutForwarding(t *testing.T) {
	handle := &fakeProcessHandle{
		stdin:  &bufferWriteCloser{},
		stdout: io.NopCloser(strings.NewReader("")),
		stderr: io.NopCloser(strings.NewReader("")),
	}
	var stdout bytes.Buffer

	result, err := (StdioProxy{Starter: &fakeProcessStarter{handle: handle}}).Run(context.Background(), StdioRequest{
		SessionID:  "session-1",
		Downstream: proxyruntime.DownstreamIdentity{ID: "fake-mcp"},
		Backend:    sessionbackend.KindDockerSandbox,
		Spec:       proxyruntime.ProcessSpec{Command: "fake-server"},
		Policy: proxyruntime.Policy{
			DefaultDecision: proxyruntime.DecisionAllow,
			MCPTools: []proxyruntime.MCPToolRule{{
				ToolName: "delete_all",
				Decision: proxyruntime.DecisionDeny,
				Reason:   "dangerous tool",
			}},
		},
		Stdin:  strings.NewReader(requestLine(42, "tools/call", map[string]any{"name": "delete_all"})),
		Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(handle.stdin.String(), "delete_all") {
		t.Fatalf("denied request was forwarded: %q", handle.stdin.String())
	}
	denied := eventByOperation(t, result.Events, "tools/call:delete_all")
	if denied.Decision != proxyruntime.DecisionDeny || denied.Reason != "dangerous tool" {
		t.Fatalf("denied event = %+v", denied)
	}
	var response struct {
		JSONRPC string `json:"jsonrpc"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		ID int `json:"id"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("deny response is not JSON-RPC: %v\n%s", err, stdout.String())
	}
	if response.JSONRPC != "2.0" || response.ID != 42 || response.Error.Code != denyErrorCode || response.Error.Message != "dangerous tool" {
		t.Fatalf("deny response = %+v", response)
	}
}

func TestStdioProxyMalformedJSONRPCFailsClosed(t *testing.T) {
	handle := &fakeProcessHandle{
		stdin:  &bufferWriteCloser{},
		stdout: io.NopCloser(strings.NewReader("")),
		stderr: io.NopCloser(strings.NewReader("")),
	}
	var cleanupCalls int

	result, err := (StdioProxy{Starter: &fakeProcessStarter{handle: handle}}).Run(context.Background(), StdioRequest{
		SessionID:  "session-1",
		Downstream: proxyruntime.DownstreamIdentity{ID: "fake-mcp"},
		Backend:    sessionbackend.KindDockerSandbox,
		Spec:       proxyruntime.ProcessSpec{Command: "fake-server"},
		Policy:     proxyruntime.Policy{DefaultDecision: proxyruntime.DecisionAllow},
		Stdin:      strings.NewReader("{bad json}\n"),
		Cleanup:    func() { cleanupCalls++ },
	})
	if err == nil {
		t.Fatal("Run succeeded, want malformed JSON-RPC failure")
	}
	if !handle.killed || !handle.waited || cleanupCalls != 1 {
		t.Fatalf("malformed frame did not kill/wait/cleanup child: killed=%v waited=%v cleanup=%d", handle.killed, handle.waited, cleanupCalls)
	}
	if handle.stdin.String() != "" {
		t.Fatalf("malformed frame was forwarded: %q", handle.stdin.String())
	}
	malformed := eventByOperation(t, result.Events, operationMalformed)
	if malformed.Decision != proxyruntime.DecisionError {
		t.Fatalf("malformed event = %+v", malformed)
	}
}

func TestParseMessageRejectsInvalidFramesAndAcceptsResponses(t *testing.T) {
	validResponse := []byte(responseLine(7, `{"ok":true}`))
	message, err := parseMessage(validResponse)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if message.hasMethod || !message.hasID {
		t.Fatalf("response parse = %+v, want response with id", message)
	}

	cases := []string{
		``,
		`[]`,
		`{"jsonrpc":"1.0","method":"initialize"}`,
		`{"jsonrpc":"2.0","method":1}`,
		`{"jsonrpc":"2.0","result":{}}`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			if _, err := parseMessage([]byte(input)); err == nil {
				t.Fatal("parseMessage succeeded, want failure")
			}
		})
	}
}

func TestNormalizeOperationAddsKnownMCPNames(t *testing.T) {
	cases := []struct {
		method string
		params map[string]any
		want   string
	}{
		{"initialize", nil, "initialize"},
		{"tools/list", nil, "tools/list"},
		{"tools/call", map[string]any{"name": "search"}, "tools/call:search"},
		{"resources/list", nil, "resources/list"},
		{"resources/read", map[string]any{"uri": "file:///a"}, "resources/read"},
		{"prompts/list", nil, "prompts/list"},
		{"prompts/get", map[string]any{"name": "summarize"}, "prompts/get:summarize"},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			message, err := parseMessage([]byte(requestLine(1, tc.method, tc.params)))
			if err != nil {
				t.Fatalf("parseMessage: %v", err)
			}
			if got := normalizeOperation(message); got != tc.want {
				t.Fatalf("normalizeOperation = %q, want %q", got, tc.want)
			}
		})
	}
}

func protocolOperations(events []proxyruntime.Event) []string {
	var operations []string
	for _, event := range events {
		if event.ProxyKind == proxyruntime.ProxyKindMCPStdio && event.Direction == proxyruntime.DirectionInbound {
			operations = append(operations, event.Operation)
		}
	}
	return operations
}

func eventByOperation(t *testing.T, events []proxyruntime.Event, operation string) proxyruntime.Event {
	t.Helper()
	for _, event := range events {
		if event.Operation == operation {
			return event
		}
	}
	t.Fatalf("event %q not found in %+v", operation, events)
	return proxyruntime.Event{}
}

func requestLine(id int, method string, params map[string]any) string {
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		request["params"] = params
	}
	raw, err := json.Marshal(request)
	if err != nil {
		panic(err)
	}
	return string(raw) + "\n"
}

func responseLine(id int, result string) string {
	return fmtJSON(map[string]json.RawMessage{
		"jsonrpc": json.RawMessage(`"2.0"`),
		"id":      json.RawMessage(fmtInt(id)),
		"result":  json.RawMessage(result),
	}) + "\n"
}

func fmtJSON(values map[string]json.RawMessage) string {
	raw, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func fmtInt(value int) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

type fakeProcessStarter struct {
	handle *fakeProcessHandle
	err    error
	spec   proxyruntime.ProcessSpec
}

func (s *fakeProcessStarter) Start(_ context.Context, spec proxyruntime.ProcessSpec) (proxyruntime.ProcessHandle, error) {
	s.spec = spec
	if s.err != nil {
		return nil, s.err
	}
	return s.handle, nil
}

type fakeProcessHandle struct {
	stdin  *bufferWriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	killed bool
	waited bool
}

func (h *fakeProcessHandle) Stdin() io.WriteCloser {
	return h.stdin
}

func (h *fakeProcessHandle) Stdout() io.ReadCloser {
	return h.stdout
}

func (h *fakeProcessHandle) Stderr() io.ReadCloser {
	return h.stderr
}

func (h *fakeProcessHandle) Wait() error {
	h.waited = true
	return nil
}

func (h *fakeProcessHandle) Kill() error {
	h.killed = true
	return nil
}

type bufferWriteCloser struct {
	bytes.Buffer
	closed bool
}

func (w *bufferWriteCloser) Close() error {
	w.closed = true
	return nil
}
