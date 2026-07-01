package hazmat

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hazmat/proxyruntime"
)

func TestMCPProxyCommandRequiresStdioAndDownstreamCommand(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing stdio", []string{"proxy", "--", "fake-server"}, "only --stdio"},
		{"missing downstream", []string{"proxy", "--stdio"}, "downstream MCP server command is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newMCPCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.ExecuteContext(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ExecuteContext error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMCPProxyPolicyDefaultsAllowAndDeniesNamedTools(t *testing.T) {
	policy := mcpProxyPolicy(mcpStdioProxyOptions{
		DenyTools: []string{" delete_all ", "", "write_secret"},
	})
	decision := policy.Evaluate(proxyruntime.PolicyRequest{MCPToolName: "read_file"})
	if decision.Decision != proxyruntime.DecisionAllow {
		t.Fatalf("read_file decision = %+v, want allow", decision)
	}
	decision = policy.Evaluate(proxyruntime.PolicyRequest{MCPToolName: "delete_all"})
	if decision.Decision != proxyruntime.DecisionDeny || decision.Rule != "mcp-tool" {
		t.Fatalf("delete_all decision = %+v, want tool deny", decision)
	}
}

func TestMCPEventSinkWritesJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	sink, closeSink, err := mcpEventSink(path)
	if err != nil {
		t.Fatalf("mcpEventSink: %v", err)
	}
	sink(proxyruntime.NewEvent(proxyruntime.EventInput{
		SessionID: "session-1",
		ProxyKind: proxyruntime.ProxyKindMCPStdio,
		Operation: "tools/list",
		Decision:  proxyruntime.DecisionAllow,
	}))
	if err := closeSink(); err != nil {
		t.Fatalf("closeSink: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("event lines = %d, want 1: %q", len(lines), raw)
	}
	var event proxyruntime.Event
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("event line is not JSON: %v", err)
	}
	if event.Operation != "tools/list" || event.Decision != proxyruntime.DecisionAllow {
		t.Fatalf("event = %+v", event)
	}
}
