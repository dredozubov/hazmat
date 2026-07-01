package hazmat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"hazmat/mcpproxy"
	"hazmat/proxyruntime"

	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run MCP proxy helpers",
	}
	cmd.AddCommand(newMCPProxyCmd())
	return cmd
}

func newMCPProxyCmd() *cobra.Command {
	var stdio bool
	var denyTools []string
	var sessionID string
	var downstreamID string
	var eventsPath string
	cmd := &cobra.Command{
		Use:   "proxy --stdio -- <command> [args...]",
		Short: "Proxy a local stdio MCP server",
		Args: func(cmd *cobra.Command, args []string) error {
			if !stdio {
				return fmt.Errorf("only --stdio MCP proxy mode is supported")
			}
			if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
				return fmt.Errorf("downstream MCP server command is required")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPStdioProxy(cmd.Context(), mcpStdioProxyOptions{
				SessionID:    sessionID,
				DownstreamID: downstreamID,
				Command:      args[0],
				Args:         args[1:],
				DenyTools:    denyTools,
				EventsPath:   eventsPath,
			})
		},
	}
	cmd.Flags().BoolVar(&stdio, "stdio", false, "proxy a local stdio MCP server")
	cmd.Flags().StringArrayVar(&denyTools, "deny-tool", nil, "deny an MCP tool name")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "session id for proxy evidence")
	cmd.Flags().StringVar(&downstreamID, "downstream-id", "", "downstream identity for proxy evidence")
	cmd.Flags().StringVar(&eventsPath, "events-jsonl", "", "write proxy evidence events as JSONL")
	return cmd
}

type mcpStdioProxyOptions struct {
	SessionID    string
	DownstreamID string
	Command      string
	Args         []string
	DenyTools    []string
	EventsPath   string
}

func runMCPStdioProxy(ctx context.Context, opts mcpStdioProxyOptions) error {
	events, closeEvents, err := mcpEventSink(opts.EventsPath)
	if err != nil {
		return err
	}

	downstreamID := strings.TrimSpace(opts.DownstreamID)
	if downstreamID == "" {
		downstreamID = filepath.Base(opts.Command)
	}
	_, runErr := (mcpproxy.StdioProxy{Events: events}).Run(ctx, mcpproxy.StdioRequest{
		SessionID: strings.TrimSpace(opts.SessionID),
		Downstream: proxyruntime.DownstreamIdentity{
			ID:      downstreamID,
			Command: opts.Command,
		},
		Spec: proxyruntime.ProcessSpec{
			Command: opts.Command,
			Args:    append([]string(nil), opts.Args...),
		},
		Policy: mcpProxyPolicy(opts),
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	return errors.Join(runErr, closeEvents())
}

func mcpProxyPolicy(opts mcpStdioProxyOptions) proxyruntime.Policy {
	policy := proxyruntime.Policy{DefaultDecision: proxyruntime.DecisionAllow}
	for _, tool := range opts.DenyTools {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			continue
		}
		policy.MCPTools = append(policy.MCPTools, proxyruntime.MCPToolRule{
			ToolName: tool,
			Decision: proxyruntime.DecisionDeny,
			Reason:   "tool denied by MCP proxy policy",
		})
	}
	return policy
}

func mcpEventSink(path string) (proxyruntime.EventSink, func() error, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, func() error { return nil }, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open MCP proxy evidence log: %w", err)
	}
	encoder := json.NewEncoder(file)
	var mu sync.Mutex
	var firstErr error
	return func(event proxyruntime.Event) {
			mu.Lock()
			defer mu.Unlock()
			if err := encoder.Encode(event); err != nil && firstErr == nil {
				firstErr = err
			}
		}, func() error {
			mu.Lock()
			defer mu.Unlock()
			return errors.Join(firstErr, file.Close())
		}, nil
}
