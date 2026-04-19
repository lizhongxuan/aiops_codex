package mcphost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// stdioConnection implements Connection for STDIO-based MCP servers.
// It launches a child process and communicates via JSON-RPC over stdin/stdout.
type stdioConnection struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	nextID uint64
}

// JSON-RPC message types for MCP protocol.
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      uint64      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newSTDIOConnection(ctx context.Context, cfg ServerConfig) (Connection, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("stdio server %q: command is required", cfg.Name)
	}

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	// Merge environment.
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdio pipe stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdio pipe stdout: %w", err)
	}
	// Discard stderr to avoid blocking.
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("stdio start %q: %w", cfg.Command, err)
	}

	conn := &stdioConnection{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}

	// Send initialize request.
	_, err = conn.call(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "aiops-codex",
			"version": "1.0.0",
		},
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("stdio initialize: %w", err)
	}

	// Send initialized notification (no response expected, but we send it).
	_ = conn.notify(ctx, "notifications/initialized", nil)

	return conn, nil
}

func (c *stdioConnection) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := atomic.AddUint64(&c.nextID, 1)
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write to stdio: %w", err)
	}

	// Read response line.
	line, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read from stdio: %w", err)
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("parse stdio response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

func (c *stdioConnection) notify(_ context.Context, method string, params interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := struct {
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = c.stdin.Write(data)
	return err
}

func (c *stdioConnection) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description,omitempty"`
			InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
			Meta        map[string]interface{} `json:"_meta,omitempty"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	tools := make([]ToolDefinition, len(resp.Tools))
	for i, t := range resp.Tools {
		tools[i] = ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Meta:        t.Meta,
		}
	}
	return tools, nil
}

func (c *stdioConnection) CallTool(ctx context.Context, req ToolCallRequest) (*ToolCallResponse, error) {
	result, err := c.call(ctx, "tools/call", map[string]interface{}{
		"name":      req.Name,
		"arguments": req.Arguments,
	})
	if err != nil {
		return nil, err
	}
	var resp ToolCallResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *stdioConnection) ListResources(ctx context.Context) ([]Resource, error) {
	result, err := c.call(ctx, "resources/list", nil)
	if err != nil {
		return nil, nil // Resources are optional.
	}
	var resp struct {
		Resources []Resource `json:"resources"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, nil
	}
	return resp.Resources, nil
}

func (c *stdioConnection) ReadResource(ctx context.Context, uri string) (*ToolCallResponse, error) {
	result, err := c.call(ctx, "resources/read", map[string]interface{}{
		"uri": uri,
	})
	if err != nil {
		return nil, err
	}
	var resp ToolCallResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *stdioConnection) Close() error {
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}
