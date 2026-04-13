package mcphost

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Manager manages the lifecycle of multiple MCP servers. It handles
// configuration loading, connection management, tool discovery, and
// hot-reload when config files change.
type Manager struct {
	mu          sync.RWMutex
	servers     map[string]*managedServer
	configPaths []string
}

type managedServer struct {
	config ServerConfig
	conn   Connection
	info   ServerInfo
}

// NewManager creates a new MCP Manager.
func NewManager() *Manager {
	return &Manager{
		servers: make(map[string]*managedServer),
	}
}

// ---------- Configuration ----------

// MCPConfigFile represents the JSON structure of an mcp.json config file.
type MCPConfigFile struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// LoadConfig loads MCP server configurations from one or more JSON files.
// Later files override earlier ones (workspace > user).
func (m *Manager) LoadConfig(paths ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configPaths = paths

	merged := make(map[string]ServerConfig)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read mcp config %s: %w", path, err)
		}
		var cfg MCPConfigFile
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parse mcp config %s: %w", path, err)
		}
		for name, sc := range cfg.MCPServers {
			sc.Name = name
			merged[name] = sc
		}
	}

	// Reconcile: add new, update changed, remove deleted.
	for name, cfg := range merged {
		existing, ok := m.servers[name]
		if !ok {
			m.servers[name] = &managedServer{
				config: cfg,
				info: ServerInfo{
					Name:      name,
					Transport: cfg.Transport,
					Status:    ServerStatusDisconnected,
				},
			}
		} else {
			existing.config = cfg
		}
	}
	// Remove servers no longer in config.
	for name, srv := range m.servers {
		if _, ok := merged[name]; !ok {
			if srv.conn != nil {
				_ = srv.conn.Close()
			}
			delete(m.servers, name)
		}
	}
	return nil
}

// SaveConfig writes the current server configurations to the specified path.
func (m *Manager) SaveConfig(path string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg := MCPConfigFile{MCPServers: make(map[string]ServerConfig)}
	for name, srv := range m.servers {
		cfg.MCPServers[name] = srv.config
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ---------- Server Management ----------

// AddServer adds a new MCP server configuration.
func (m *Manager) AddServer(cfg ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers[cfg.Name] = &managedServer{
		config: cfg,
		info: ServerInfo{
			Name:      cfg.Name,
			Transport: cfg.Transport,
			Status:    ServerStatusDisconnected,
		},
	}
}

// RemoveServer disconnects and removes an MCP server.
func (m *Manager) RemoveServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	srv, ok := m.servers[name]
	if !ok {
		return fmt.Errorf("mcp server %q not found", name)
	}
	if srv.conn != nil {
		_ = srv.conn.Close()
	}
	delete(m.servers, name)
	return nil
}

// ConnectAll connects to all configured, non-disabled servers.
func (m *Manager) ConnectAll(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, srv := range m.servers {
		if srv.config.Disabled || srv.conn != nil {
			continue
		}
		m.connectServerLocked(ctx, name, srv)
	}
}

// ConnectServer connects to a specific server by name.
func (m *Manager) ConnectServer(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	srv, ok := m.servers[name]
	if !ok {
		return fmt.Errorf("mcp server %q not found", name)
	}
	m.connectServerLocked(ctx, name, srv)
	if srv.info.Status == ServerStatusError {
		return fmt.Errorf("failed to connect to %s: %s", name, srv.info.Error)
	}
	return nil
}

func (m *Manager) connectServerLocked(ctx context.Context, name string, srv *managedServer) {
	srv.info.Status = ServerStatusConnecting
	var conn Connection
	var err error

	switch srv.config.Transport {
	case TransportSTDIO:
		conn, err = newSTDIOConnection(ctx, srv.config)
	case TransportHTTP:
		conn, err = newHTTPConnection(ctx, srv.config)
	default:
		err = fmt.Errorf("unsupported transport: %s", srv.config.Transport)
	}

	if err != nil {
		srv.info.Status = ServerStatusError
		srv.info.Error = err.Error()
		log.Printf("[mcphost] failed to connect to %s: %v", name, err)
		return
	}

	srv.conn = conn
	srv.info.Status = ServerStatusConnected
	srv.info.Error = ""

	// Discover tools.
	tools, err := conn.ListTools(ctx)
	if err != nil {
		log.Printf("[mcphost] failed to list tools from %s: %v", name, err)
	} else {
		for i := range tools {
			tools[i].ServerName = name
		}
		srv.info.Tools = tools
	}

	log.Printf("[mcphost] connected to %s (%s), %d tools discovered", name, srv.config.Transport, len(srv.info.Tools))
}

// DisconnectAll disconnects all servers.
func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, srv := range m.servers {
		if srv.conn != nil {
			_ = srv.conn.Close()
			srv.conn = nil
			srv.info.Status = ServerStatusDisconnected
		}
	}
}

// ---------- Tool Operations ----------

// AllTools returns all tools from all connected servers.
func (m *Manager) AllTools() []ToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var all []ToolDefinition
	for _, srv := range m.servers {
		if srv.info.Status == ServerStatusConnected {
			all = append(all, srv.info.Tools...)
		}
	}
	return all
}

// CallTool routes a tool call to the appropriate server.
func (m *Manager) CallTool(ctx context.Context, serverName string, req ToolCallRequest) (*ToolCallResponse, error) {
	m.mu.RLock()
	srv, ok := m.servers[serverName]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("mcp server %q not found", serverName)
	}
	if srv.conn == nil {
		return nil, fmt.Errorf("mcp server %q is not connected", serverName)
	}
	return srv.conn.CallTool(ctx, req)
}

// ReadResource reads a resource from the specified MCP server by URI.
func (m *Manager) ReadResource(ctx context.Context, serverName string, uri string) (*ToolCallResponse, error) {
	m.mu.RLock()
	srv, ok := m.servers[serverName]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("mcp server %q not found", serverName)
	}
	if srv.conn == nil {
		return nil, fmt.Errorf("mcp server %q is not connected", serverName)
	}
	return srv.conn.ReadResource(ctx, uri)
}

// FindResourceServer finds which server provides a given resource URI.
func (m *Manager) FindResourceServer(uri string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, srv := range m.servers {
		for _, r := range srv.info.Resources {
			if r.URI == uri {
				return srv.config.Name, true
			}
		}
	}
	return "", false
}

// FindToolServer finds which server provides a given tool name.
func (m *Manager) FindToolServer(toolName string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, srv := range m.servers {
		for _, t := range srv.info.Tools {
			if t.Name == toolName {
				return srv.config.Name, true
			}
		}
	}
	return "", false
}

// IsAutoApproved checks if a tool is in the auto-approve list for its server.
func (m *Manager) IsAutoApproved(serverName, toolName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	srv, ok := m.servers[serverName]
	if !ok {
		return false
	}
	for _, name := range srv.config.AutoApprove {
		if name == toolName || name == "*" {
			return true
		}
	}
	return false
}

// ServerInfos returns status information for all managed servers.
func (m *Manager) ServerInfos() []ServerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []ServerInfo
	for _, srv := range m.servers {
		out = append(out, srv.info)
	}
	return out
}

// Reload re-reads config files and reconnects changed servers.
func (m *Manager) Reload(ctx context.Context) error {
	if err := m.LoadConfig(m.configPaths...); err != nil {
		return err
	}
	m.ConnectAll(ctx)
	return nil
}
