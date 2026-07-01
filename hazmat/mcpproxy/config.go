package mcpproxy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"hazmat/proxyruntime"
)

var defaultWrapperArgs = []string{"mcp", "proxy", "--stdio"}

type ConfigRequest struct {
	Name              string
	WrapperCommand    string
	WrapperArgs       []string
	DownstreamCommand string
	DownstreamArgs    []string
	CWD               string
	Env               map[string]string
}

func RenderClaudeConfig(req ConfigRequest) ([]byte, error) {
	return renderClaudeConfig(req, false)
}

func RenderRedactedClaudeConfig(req ConfigRequest) ([]byte, error) {
	return renderClaudeConfig(req, true)
}

func RenderCodexConfig(req ConfigRequest) (string, error) {
	return renderCodexConfig(req, false)
}

func RenderRedactedCodexConfig(req ConfigRequest) (string, error) {
	return renderCodexConfig(req, true)
}

func renderClaudeConfig(req ConfigRequest, redact bool) ([]byte, error) {
	normalized, err := normalizeConfigRequest(req, redact)
	if err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(claudeConfig{
		MCPServers: map[string]claudeMCPServer{
			normalized.Name: {
				Type:    "stdio",
				Command: normalized.WrapperCommand,
				Args:    normalized.wrapperInvocationArgs(),
				Env:     normalized.Env,
				CWD:     normalized.CWD,
			},
		},
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func renderCodexConfig(req ConfigRequest, redact bool) (string, error) {
	normalized, err := normalizeConfigRequest(req, redact)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	serverKey := tomlString(normalized.Name)
	fmt.Fprintf(&b, "[mcp_servers.%s]\n", serverKey)
	fmt.Fprintf(&b, "command = %s\n", tomlString(normalized.WrapperCommand))
	fmt.Fprintf(&b, "args = %s\n", tomlStringArray(normalized.wrapperInvocationArgs()))
	if normalized.CWD != "" {
		fmt.Fprintf(&b, "cwd = %s\n", tomlString(normalized.CWD))
	}
	if len(normalized.Env) > 0 {
		fmt.Fprintf(&b, "\n[mcp_servers.%s.env]\n", serverKey)
		for _, key := range sortedMapKeys(normalized.Env) {
			fmt.Fprintf(&b, "%s = %s\n", tomlString(key), tomlString(normalized.Env[key]))
		}
	}
	return b.String(), nil
}

type normalizedConfigRequest struct {
	Name              string
	WrapperCommand    string
	WrapperArgs       []string
	DownstreamCommand string
	DownstreamArgs    []string
	CWD               string
	Env               map[string]string
}

func normalizeConfigRequest(req ConfigRequest, redact bool) (normalizedConfigRequest, error) {
	name := strings.TrimSpace(req.Name)
	wrapperCommand := strings.TrimSpace(req.WrapperCommand)
	downstreamCommand := strings.TrimSpace(req.DownstreamCommand)
	if name == "" {
		return normalizedConfigRequest{}, fmt.Errorf("mcpproxy: config name is required")
	}
	if wrapperCommand == "" {
		return normalizedConfigRequest{}, fmt.Errorf("mcpproxy: wrapper command is required")
	}
	if downstreamCommand == "" {
		return normalizedConfigRequest{}, fmt.Errorf("mcpproxy: downstream command is required")
	}
	wrapperArgs := append([]string(nil), req.WrapperArgs...)
	if len(wrapperArgs) == 0 {
		wrapperArgs = append([]string(nil), defaultWrapperArgs...)
	}
	normalized := normalizedConfigRequest{
		Name:              name,
		WrapperCommand:    wrapperCommand,
		WrapperArgs:       copyArgs(wrapperArgs),
		DownstreamCommand: downstreamCommand,
		DownstreamArgs:    copyArgs(req.DownstreamArgs),
		CWD:               strings.TrimSpace(req.CWD),
		Env:               normalizeConfigEnv(req.Env, redact),
	}
	if err := validateConfigEnv(normalized.Env); err != nil {
		return normalizedConfigRequest{}, err
	}
	return normalized, nil
}

func (r normalizedConfigRequest) wrapperInvocationArgs() []string {
	args := append([]string(nil), r.WrapperArgs...)
	args = append(args, "--", r.DownstreamCommand)
	args = append(args, r.DownstreamArgs...)
	return args
}

func copyArgs(values []string) []string {
	return append([]string(nil), values...)
}

func normalizeConfigEnv(env map[string]string, redact bool) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		key = strings.TrimSpace(key)
		if redact {
			value = proxyruntime.RedactedValue
		}
		out[key] = value
	}
	return out
}

func validateConfigEnv(env map[string]string) error {
	for key := range env {
		if key == "" {
			return fmt.Errorf("mcpproxy: env key is required")
		}
		if strings.ContainsAny(key, "=\x00\n\r") {
			return fmt.Errorf("mcpproxy: env key %q contains unsupported characters", key)
		}
	}
	return nil
}

type claudeConfig struct {
	MCPServers map[string]claudeMCPServer `json:"mcpServers"`
}

type claudeMCPServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
	CWD     string            `json:"cwd,omitempty"`
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func tomlStringArray(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, tomlString(value))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func tomlString(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
