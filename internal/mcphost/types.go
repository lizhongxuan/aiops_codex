// Package mcphost provides a generic MCP (Model Context Protocol) server
// management framework. It supports STDIO and HTTP transports, OAuth/Bearer
// authentication, dynamic tool discovery, and hot-reload of server configs.
package mcphost

import (
	"context"
	"time"
)

// Transport enumerates supported MCP server transports.
type Transport string

const (
	TransportSTDIO Transport = "stdio"
	TransportHTTP  Transport = "http"
)

// AuthType enumerates supported authentication methods.
type AuthType string

const (
	AuthNone   AuthType = "none"
	AuthBearer AuthType = "bearer"
	AuthOAuth  AuthType = "oauth"
)

// ServerConfig describes a single MCP server to manage.
type ServerConfig struct {
	// Name is the unique identifier for this server.
	Name string `json:"name"`
	// Transport is "stdio" or "http".
	Transport Transport `json:"transport"`
	// Command is the executable for STDIO servers (e.g. "npx", "uvx").
	Command string `json:"command,omitempty"`
	// Args are command-line arguments for STDIO servers.
	Args []string `json:"args,omitempty"`
	// URL is the endpoint for HTTP servers.
	URL string `json:"url,omitempty"`
	// Env holds extra environment variables for STDIO servers.
	Env map[string]string `json:"env,omitempty"`
	// Auth configures authentication.
	Auth AuthConfig `json:"auth,omitempty"`
	// Disabled allows temporarily disabling a server.
	Disabled bool `json:"disabled,omitempty"`
	// AutoApprove lists tool names that skip approval.
	AutoApprove []string `json:"autoApprove,omitempty"`
	// Timeout for tool calls (0 = default 30s).
	Timeout time.Duration `json:"timeout,omitempty"`
}

// AuthConfig holds authentication parameters.
type AuthConfig struct {
	Type         AuthType `json:"type,omitempty"`
	Token        string   `json:"token,omitempty"`
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	TokenURL     string   `json:"token_url,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

// ToolDefinition describes a tool exposed by an MCP server.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
	Meta        map[string]interface{} `json:"_meta,omitempty"`
	ServerName  string                 `json:"serverName"`
}

// ToolCallRequest is a request to invoke an MCP tool.
type ToolCallRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// ToolCallResponse is the result of an MCP tool invocation.
type ToolCallResponse struct {
	Content  []ContentBlock    `json:"content,omitempty"`
	Contents []ResourceContent `json:"contents,omitempty"`
	IsError  bool              `json:"isError,omitempty"`
}

// ContentBlock is a piece of content returned by an MCP tool.
type ContentBlock struct {
	Type string `json:"type"` // "text", "image", "resource"
	Text string `json:"text,omitempty"`
}

// ResourceContent is a piece of content returned by resources/read.
type ResourceContent struct {
	URI      string `json:"uri,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// Resource describes an MCP resource.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ServerStatus represents the connection state of an MCP server.
type ServerStatus string

const (
	ServerStatusDisconnected ServerStatus = "disconnected"
	ServerStatusConnecting   ServerStatus = "connecting"
	ServerStatusConnected    ServerStatus = "connected"
	ServerStatusError        ServerStatus = "error"
)

// ServerInfo holds runtime information about a managed MCP server.
type ServerInfo struct {
	Name      string           `json:"name"`
	Transport Transport        `json:"transport"`
	Status    ServerStatus     `json:"status"`
	Tools     []ToolDefinition `json:"tools,omitempty"`
	Resources []Resource       `json:"resources,omitempty"`
	Error     string           `json:"error,omitempty"`
}

// Connection is the interface for an active MCP server connection.
type Connection interface {
	// ListTools returns all tools exposed by this server.
	ListTools(ctx context.Context) ([]ToolDefinition, error)
	// CallTool invokes a tool on this server.
	CallTool(ctx context.Context, req ToolCallRequest) (*ToolCallResponse, error)
	// ListResources returns all resources exposed by this server.
	ListResources(ctx context.Context) ([]Resource, error)
	// ReadResource reads a specific resource.
	ReadResource(ctx context.Context, uri string) (*ToolCallResponse, error)
	// Close shuts down the connection.
	Close() error
}
