package mcphost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// httpConnection implements Connection for HTTP-based (Streamable HTTP) MCP servers.
type httpConnection struct {
	baseURL    string
	httpClient *http.Client
	auth       AuthConfig
	nextID     uint64
	mu         sync.Mutex
	// OAuth token cache.
	oauthToken    string
	oauthExpiry   time.Time
}

func newHTTPConnection(_ context.Context, cfg ServerConfig) (Connection, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("http server %q: url is required", cfg.Name)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	conn := &httpConnection{
		baseURL: strings.TrimRight(cfg.URL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		auth: cfg.Auth,
	}
	return conn, nil
}

func (c *httpConnection) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	id := atomic.AddUint64(&c.nextID, 1)
	c.mu.Unlock()

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.injectAuth(ctx, httpReq); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("mcp http %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("parse mcp response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func (c *httpConnection) injectAuth(ctx context.Context, req *http.Request) error {
	switch c.auth.Type {
	case AuthBearer:
		if c.auth.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.auth.Token)
		}
	case AuthOAuth:
		token, err := c.getOAuthToken(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return nil
}

func (c *httpConnection) getOAuthToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.oauthToken != "" && time.Now().Before(c.oauthExpiry) {
		return c.oauthToken, nil
	}

	if c.auth.TokenURL == "" {
		return "", fmt.Errorf("oauth token_url is required")
	}

	data := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s",
		c.auth.ClientID, c.auth.ClientSecret)
	if len(c.auth.Scopes) > 0 {
		data += "&scope=" + strings.Join(c.auth.Scopes, " ")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.auth.TokenURL, strings.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}
	c.oauthToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		c.oauthExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	} else {
		c.oauthExpiry = time.Now().Add(50 * time.Minute)
	}
	return c.oauthToken, nil
}

func (c *httpConnection) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description,omitempty"`
			InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
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
		}
	}
	return tools, nil
}

func (c *httpConnection) CallTool(ctx context.Context, req ToolCallRequest) (*ToolCallResponse, error) {
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

func (c *httpConnection) ListResources(ctx context.Context) ([]Resource, error) {
	result, err := c.call(ctx, "resources/list", nil)
	if err != nil {
		return nil, nil
	}
	var resp struct {
		Resources []Resource `json:"resources"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, nil
	}
	return resp.Resources, nil
}

func (c *httpConnection) ReadResource(ctx context.Context, uri string) (*ToolCallResponse, error) {
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

func (c *httpConnection) Close() error {
	return nil // HTTP connections are stateless.
}
