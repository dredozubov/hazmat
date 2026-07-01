// Package mcpproxy implements MCP protocol mediation on top of proxyruntime.
package mcpproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"hazmat/proxyruntime"
	"hazmat/sessionbackend"
)

const (
	jsonRPCVersion      = "2.0"
	operationMalformed  = "jsonrpc:malformed"
	denyErrorCode       = -32000
	invalidParamsCode   = -32602
	denyErrorMessage    = "request denied by Hazmat MCP proxy"
	invalidParamMessage = "invalid MCP request parameters"
)

type StdioProxy struct {
	Starter proxyruntime.ProcessStarter
	Events  proxyruntime.EventSink
}

type StdioRequest struct {
	SessionID  string
	Downstream proxyruntime.DownstreamIdentity
	Backend    sessionbackend.Kind
	Spec       proxyruntime.ProcessSpec
	Policy     proxyruntime.Policy
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Cleanup    func()
}

type StdioResult struct {
	Process proxyruntime.ProcessResult
	Events  []proxyruntime.Event
}

func (p StdioProxy) Run(ctx context.Context, req StdioRequest) (StdioResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req.Stdin == nil {
		req.Stdin = strings.NewReader("")
	}
	if req.Stdout == nil {
		req.Stdout = io.Discard
	}
	if req.Stderr == nil {
		req.Stderr = io.Discard
	}

	var result StdioResult
	emit := func(event proxyruntime.Event) {
		result.Events = append(result.Events, event)
		if p.Events != nil {
			p.Events(event)
		}
	}
	clientOut := &lockedWriter{writer: req.Stdout}
	session := stdioSession{request: req, emit: emit}
	processResult, err := (proxyruntime.ProcessRunner{
		Starter: p.Starter,
		Events:  emit,
	}).Run(ctx, proxyruntime.ProcessRequest{
		Spec:    req.Spec,
		Cleanup: req.Cleanup,
		Event: proxyruntime.EventInput{
			SessionID:  req.SessionID,
			ProxyKind:  proxyruntime.ProxyKindMCPStdio,
			Downstream: req.Downstream,
			Backend:    req.Backend,
			AttachKind: proxyruntime.AttachKindStdio,
		},
	}, func(ctx context.Context, streams proxyruntime.ProcessStreams) error {
		errs := make(chan error, 2)
		go copyStream(errs, clientOut, streams.Stdout)
		go copyStream(errs, req.Stderr, streams.Stderr)

		clientErr := session.forwardClient(ctx, req.Stdin, streams.Stdin, clientOut)
		closeErr := streams.Stdin.Close()
		if clientErr != nil {
			return clientErr
		}
		if closeErr != nil {
			return closeErr
		}
		for i := 0; i < 2; i++ {
			if err := <-errs; err != nil {
				return err
			}
		}
		return nil
	})
	result.Process = processResult
	return result, err
}

type stdioSession struct {
	request StdioRequest
	emit    proxyruntime.EventSink
}

func (s stdioSession) forwardClient(ctx context.Context, clientIn io.Reader, serverIn io.Writer, clientOut io.Writer) error {
	reader := bufio.NewReader(clientIn)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if err := s.forwardClientLine(line, serverIn, clientOut); err != nil {
				return err
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

func (s stdioSession) forwardClientLine(line []byte, serverIn io.Writer, clientOut io.Writer) error {
	message, err := parseMessage(line)
	if err != nil {
		s.emitEvent(operationMalformed, proxyruntime.DecisionError, err.Error(), nil)
		return err
	}
	if !message.hasMethod {
		_, err := serverIn.Write(line)
		return err
	}

	operation := normalizeOperation(message)
	attrs := message.attributes()
	decision := s.request.Policy.Evaluate(proxyruntime.PolicyRequest{
		DownstreamIdentity: s.request.Downstream.ID,
		MCPToolName:        message.toolName,
	})
	if decision.Decision == proxyruntime.DecisionDeny {
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = denyErrorMessage
		}
		s.emitEvent(operation, proxyruntime.DecisionDeny, reason, attrs)
		if message.hasID {
			return writeJSONRPCError(clientOut, message.id, denyErrorCode, reason)
		}
		return nil
	}
	if message.method == "tools/call" && strings.TrimSpace(message.toolName) == "" {
		s.emitEvent(operation, proxyruntime.DecisionDeny, invalidParamMessage, attrs)
		if message.hasID {
			return writeJSONRPCError(clientOut, message.id, invalidParamsCode, invalidParamMessage)
		}
		return nil
	}

	if _, err := serverIn.Write(line); err != nil {
		return err
	}
	s.emitEvent(operation, decision.Decision, decision.Reason, attrs)
	return nil
}

func (s stdioSession) emitEvent(operation string, decision proxyruntime.Decision, reason string, attrs map[string]string) {
	if s.emit == nil {
		return
	}
	s.emit(proxyruntime.NewEvent(proxyruntime.EventInput{
		SessionID:  s.request.SessionID,
		ProxyKind:  proxyruntime.ProxyKindMCPStdio,
		Downstream: s.request.Downstream,
		Backend:    s.request.Backend,
		AttachKind: proxyruntime.AttachKindStdio,
		Direction:  proxyruntime.DirectionInbound,
		Operation:  operation,
		Decision:   decision,
		Reason:     reason,
		Attributes: attrs,
	}))
}

type rpcMessage struct {
	method     string
	id         json.RawMessage
	params     json.RawMessage
	hasID      bool
	hasMethod  bool
	toolName   string
	resource   string
	promptName string
}

func parseMessage(line []byte) (rpcMessage, error) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return rpcMessage{}, errors.New("empty JSON-RPC frame")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return rpcMessage{}, fmt.Errorf("parse JSON-RPC frame: %w", err)
	}
	version, ok, err := stringField(fields, "jsonrpc")
	if err != nil {
		return rpcMessage{}, err
	}
	if !ok || version != jsonRPCVersion {
		return rpcMessage{}, errors.New("JSON-RPC frame must declare jsonrpc 2.0")
	}

	message := rpcMessage{}
	if id, ok := fields["id"]; ok {
		message.hasID = true
		message.id = append(json.RawMessage(nil), id...)
	}
	method, hasMethod, err := stringField(fields, "method")
	if err != nil {
		return rpcMessage{}, err
	}
	message.method = strings.TrimSpace(method)
	message.hasMethod = hasMethod
	if message.hasMethod && message.method == "" {
		return rpcMessage{}, errors.New("JSON-RPC method must not be empty")
	}
	if params, ok := fields["params"]; ok {
		message.params = append(json.RawMessage(nil), params...)
	}
	if message.hasMethod {
		switch message.method {
		case "tools/call":
			message.toolName = paramString(message.params, "name")
		case "resources/read":
			message.resource = paramString(message.params, "uri")
		case "prompts/get":
			message.promptName = paramString(message.params, "name")
		}
		return message, nil
	}
	if !message.hasID {
		return rpcMessage{}, errors.New("JSON-RPC response must carry an id")
	}
	if _, hasResult := fields["result"]; hasResult {
		return message, nil
	}
	if _, hasError := fields["error"]; hasError {
		return message, nil
	}
	return rpcMessage{}, errors.New("JSON-RPC response must carry result or error")
}

func stringField(fields map[string]json.RawMessage, name string) (string, bool, error) {
	raw, ok := fields[name]
	if !ok {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, fmt.Errorf("JSON-RPC %s field must be a string", name)
	}
	return value, true, nil
}

func paramString(params json.RawMessage, name string) string {
	if len(params) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(params, &fields); err != nil {
		return ""
	}
	raw, ok := fields[name]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func normalizeOperation(message rpcMessage) string {
	switch message.method {
	case "tools/call":
		if message.toolName != "" {
			return "tools/call:" + message.toolName
		}
	case "prompts/get":
		if message.promptName != "" {
			return "prompts/get:" + message.promptName
		}
	}
	if strings.TrimSpace(message.method) == "" {
		return "response"
	}
	return message.method
}

func (m rpcMessage) attributes() map[string]string {
	attrs := map[string]string{"mcp_method": m.method}
	if m.toolName != "" {
		attrs["mcp_tool_name"] = m.toolName
	}
	if m.resource != "" {
		attrs["mcp_resource_uri"] = m.resource
	}
	if m.promptName != "" && m.method == "prompts/get" {
		attrs["mcp_prompt_name"] = m.promptName
	}
	return attrs
}

func writeJSONRPCError(w io.Writer, id json.RawMessage, code int, message string) error {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	raw, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		Error   jsonrpcError    `json:"error"`
		ID      json.RawMessage `json:"id"`
	}{
		JSONRPC: jsonRPCVersion,
		Error: jsonrpcError{
			Code:    code,
			Message: message,
		},
		ID: id,
	})
	if err != nil {
		return err
	}
	_, err = w.Write(append(raw, '\n'))
	return err
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func copyStream(errs chan<- error, dst io.Writer, src io.Reader) {
	_, err := io.Copy(dst, src)
	errs <- err
}

type lockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}
