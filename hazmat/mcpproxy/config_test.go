package mcpproxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderClaudeConfigGolden(t *testing.T) {
	raw, err := RenderClaudeConfig(configRequestFixture())
	if err != nil {
		t.Fatalf("RenderClaudeConfig: %v", err)
	}
	want := `{
  "mcpServers": {
    "project-fs": {
      "type": "stdio",
      "command": "/usr/local/bin/hazmat",
      "args": [
        "mcp",
        "proxy",
        "--stdio",
        "--",
        "node",
        "server.js",
        "--root",
        "."
      ],
      "env": {
        "SAFE_TOKEN": "token-value"
      },
      "cwd": "/work/project"
    }
  }
}
`
	if string(raw) != want {
		t.Fatalf("Claude config:\n%s\nwant:\n%s", string(raw), want)
	}
	var parsed struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("rendered Claude config is not JSON: %v", err)
	}
	server := parsed.MCPServers["project-fs"]
	if server.Command != "/usr/local/bin/hazmat" || len(server.Args) < 6 || server.Args[4] != "node" {
		t.Fatalf("server wrapper shape = %+v", server)
	}
}

func TestRenderCodexConfigGolden(t *testing.T) {
	got, err := RenderCodexConfig(configRequestFixture())
	if err != nil {
		t.Fatalf("RenderCodexConfig: %v", err)
	}
	want := `[mcp_servers."project-fs"]
command = "/usr/local/bin/hazmat"
args = ["mcp", "proxy", "--stdio", "--", "node", "server.js", "--root", "."]
cwd = "/work/project"

[mcp_servers."project-fs".env]
"SAFE_TOKEN" = "token-value"
`
	if got != want {
		t.Fatalf("Codex config:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderConfigRedactsEnvValues(t *testing.T) {
	claudeRaw, err := RenderRedactedClaudeConfig(configRequestFixture())
	if err != nil {
		t.Fatalf("RenderRedactedClaudeConfig: %v", err)
	}
	codexRaw, err := RenderRedactedCodexConfig(configRequestFixture())
	if err != nil {
		t.Fatalf("RenderRedactedCodexConfig: %v", err)
	}
	for _, raw := range []string{string(claudeRaw), codexRaw} {
		if strings.Contains(raw, "token-value") {
			t.Fatalf("redacted config leaked env value:\n%s", raw)
		}
		if !strings.Contains(raw, "[redacted]") {
			t.Fatalf("redacted config missing marker:\n%s", raw)
		}
		if !strings.Contains(raw, "/usr/local/bin/hazmat") || !strings.Contains(raw, "server.js") {
			t.Fatalf("redaction removed wrapper/downstream shape:\n%s", raw)
		}
	}
}

func TestRenderConfigRejectsMissingAuthorityShape(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ConfigRequest)
	}{
		{"name", func(req *ConfigRequest) { req.Name = "" }},
		{"wrapper", func(req *ConfigRequest) { req.WrapperCommand = "" }},
		{"downstream", func(req *ConfigRequest) { req.DownstreamCommand = "" }},
		{"env key", func(req *ConfigRequest) { req.Env = map[string]string{"BAD=KEY": "value"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := configRequestFixture()
			tc.mutate(&req)
			if _, err := RenderClaudeConfig(req); err == nil {
				t.Fatal("RenderClaudeConfig succeeded, want validation error")
			}
			if _, err := RenderCodexConfig(req); err == nil {
				t.Fatal("RenderCodexConfig succeeded, want validation error")
			}
		})
	}
}

func configRequestFixture() ConfigRequest {
	return ConfigRequest{
		Name:              "project-fs",
		WrapperCommand:    "/usr/local/bin/hazmat",
		DownstreamCommand: "node",
		DownstreamArgs:    []string{"server.js", "--root", "."},
		CWD:               "/work/project",
		Env:               map[string]string{"SAFE_TOKEN": "token-value"},
	}
}
