package mcphost

import (
	"context"
	"fmt"
	"sync"
)

// DeferredServer wraps a server configuration and lazily connects/discovers tools
// on first use rather than at startup time.
type DeferredServer struct {
	mu     sync.Mutex
	config ServerConfig
	loaded bool
	tools  []ToolDefinition
	conn   Connection
	err    error
}

// NewDeferredServer creates a new DeferredServer that will connect lazily.
func NewDeferredServer(config ServerConfig) *DeferredServer {
	return &DeferredServer{
		config: config,
	}
}

// EnsureLoaded connects to the server and discovers tools on first use.
// Subsequent calls return immediately using cached results.
func (d *DeferredServer) EnsureLoaded(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.loaded {
		return d.err
	}

	d.loaded = true

	// Connect based on transport type
	var conn Connection
	var err error

	switch d.config.Transport {
	case TransportSTDIO:
		conn, err = newSTDIOConnection(ctx, d.config)
	case TransportHTTP:
		conn, err = newHTTPConnection(ctx, d.config)
	default:
		err = fmt.Errorf("unsupported transport: %s", d.config.Transport)
	}

	if err != nil {
		d.err = fmt.Errorf("deferred connect to %s failed: %w", d.config.Name, err)
		return d.err
	}

	d.conn = conn

	// Discover tools
	tools, err := conn.ListTools(ctx)
	if err != nil {
		d.err = fmt.Errorf("deferred tool discovery for %s failed: %w", d.config.Name, err)
		return d.err
	}

	for i := range tools {
		tools[i].ServerName = d.config.Name
	}
	d.tools = tools
	return nil
}

// Tools returns cached tools after first load. Returns nil if not yet loaded.
func (d *DeferredServer) Tools() []ToolDefinition {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.tools
}

// IsLoaded returns whether the server has been connected and tools discovered.
func (d *DeferredServer) IsLoaded() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loaded
}

// Config returns the server configuration.
func (d *DeferredServer) Config() ServerConfig {
	return d.config
}

// Close closes the underlying connection if loaded.
func (d *DeferredServer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

// CallTool invokes a tool, ensuring the server is loaded first.
func (d *DeferredServer) CallTool(ctx context.Context, req ToolCallRequest) (*ToolCallResponse, error) {
	if err := d.EnsureLoaded(ctx); err != nil {
		return nil, err
	}

	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("deferred server %s has no connection", d.config.Name)
	}

	return conn.CallTool(ctx, req)
}
