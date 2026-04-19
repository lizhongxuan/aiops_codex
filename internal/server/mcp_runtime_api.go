package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/mcphost"
)

type mcpServerRuntimeItem struct {
	Name          string              `json:"name"`
	Transport     mcphost.Transport   `json:"transport"`
	Command       string              `json:"command,omitempty"`
	Args          []string            `json:"args,omitempty"`
	URL           string              `json:"url,omitempty"`
	Env           map[string]string   `json:"env,omitempty"`
	Disabled      bool                `json:"disabled"`
	AutoApprove   []string            `json:"autoApprove,omitempty"`
	TimeoutMillis int64               `json:"timeoutMillis,omitempty"`
	Status        mcphost.ServerStatus `json:"status"`
	Error         string              `json:"error,omitempty"`
	ToolCount     int                 `json:"toolCount"`
	ResourceCount int                 `json:"resourceCount"`
}

func (a *App) ensureConcreteMCPManager() (*mcphost.Manager, error) {
	if a == nil {
		return nil, nil
	}
	if manager, ok := a.mcpManager.(*mcphost.Manager); ok && manager != nil {
		return manager, nil
	}
	runtime, err := a.ensureMCPRuntime()
	if err != nil {
		return nil, err
	}
	manager, ok := runtime.(*mcphost.Manager)
	if !ok || manager == nil {
		return nil, fmt.Errorf("mcp runtime does not support management")
	}
	return manager, nil
}

func (a *App) writableMCPConfigPath() string {
	if len(a.cfg.MCPConfigPaths) >= 2 {
		return a.cfg.MCPConfigPaths[1]
	}
	if len(a.cfg.MCPConfigPaths) > 0 {
		return a.cfg.MCPConfigPaths[len(a.cfg.MCPConfigPaths)-1]
	}
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, ".kiro", "settings", "mcp.json")
}

func buildMCPRuntimeItems(manager *mcphost.Manager) []mcpServerRuntimeItem {
	if manager == nil {
		return nil
	}
	infos := manager.ServerInfos()
	configs := manager.ServerConfigs()
	infoByName := make(map[string]mcphost.ServerInfo, len(infos))
	for _, item := range infos {
		infoByName[item.Name] = item
	}
	items := make([]mcpServerRuntimeItem, 0, len(configs))
	for _, cfg := range configs {
		info := infoByName[cfg.Name]
		items = append(items, mcpServerRuntimeItem{
			Name:          cfg.Name,
			Transport:     cfg.Transport,
			Command:       cfg.Command,
			Args:          append([]string(nil), cfg.Args...),
			URL:           cfg.URL,
			Env:           cloneMCPServerEnvMap(cfg.Env),
			Disabled:      cfg.Disabled,
			AutoApprove:   append([]string(nil), cfg.AutoApprove...),
			TimeoutMillis: cfg.Timeout.Milliseconds(),
			Status:        info.Status,
			Error:         info.Error,
			ToolCount:     len(info.Tools),
			ResourceCount: len(info.Resources),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.TrimSpace(items[i].Name) < strings.TrimSpace(items[j].Name)
	})
	return items
}

func normalizeMCPServerConfig(cfg mcphost.ServerConfig) (mcphost.ServerConfig, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		return mcphost.ServerConfig{}, fmt.Errorf("mcp name is required")
	}
	cfg.Transport = mcphost.Transport(strings.TrimSpace(string(cfg.Transport)))
	switch cfg.Transport {
	case mcphost.TransportSTDIO, mcphost.TransportHTTP:
	default:
		return mcphost.ServerConfig{}, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
	cfg.Command = strings.TrimSpace(cfg.Command)
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.Transport == mcphost.TransportSTDIO && cfg.Command == "" {
		return mcphost.ServerConfig{}, fmt.Errorf("stdio transport requires command")
	}
	if cfg.Transport == mcphost.TransportHTTP && cfg.URL == "" {
		return mcphost.ServerConfig{}, fmt.Errorf("http transport requires url")
	}
	args := make([]string, 0, len(cfg.Args))
	for _, item := range cfg.Args {
		if text := strings.TrimSpace(item); text != "" {
			args = append(args, text)
		}
	}
	cfg.Args = args
	if cfg.Timeout < 0 {
		cfg.Timeout = 0
	}
	if len(cfg.Env) == 0 {
		cfg.Env = nil
	}
	return cfg, nil
}

func cloneMCPServerEnvMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (a *App) handleMCPServers(w http.ResponseWriter, r *http.Request, sessionID string) {
	manager, err := a.ensureConcreteMCPManager()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"items":      buildMCPRuntimeItems(manager),
			"configPath": a.writableMCPConfigPath(),
		})
	case http.MethodPost:
		var cfg mcphost.ServerConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		cfg, err = normalizeMCPServerConfig(cfg)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		manager.UpsertServer(cfg)
		if err := manager.SaveConfig(a.writableMCPConfigPath()); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if !cfg.Disabled {
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			if err := manager.ConnectServer(ctx, cfg.Name); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
		}
		a.audit("mcp_runtime.upserted", map[string]any{
			"sessionId": sessionID,
			"operator":  a.auditOperator(sessionID),
			"name":      cfg.Name,
			"transport": cfg.Transport,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"items": buildMCPRuntimeItems(manager),
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleMCPServerByName(w http.ResponseWriter, r *http.Request, sessionID string) {
	manager, err := a.ensureConcreteMCPManager()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/mcp/servers/"), "/")
	if path == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "mcp server not found"})
		return
	}
	if path == "refresh" {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := manager.Reload(ctx); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"items": buildMCPRuntimeItems(manager),
		})
		return
	}
	parts := strings.Split(path, "/")
	name := strings.TrimSpace(parts[0])
	if name == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "mcp server not found"})
		return
	}
	if len(parts) == 2 {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		switch parts[1] {
		case "open":
			cfgs := manager.ServerConfigs()
			var target *mcphost.ServerConfig
			for i := range cfgs {
				if strings.TrimSpace(cfgs[i].Name) == name {
					target = &cfgs[i]
					break
				}
			}
			if target == nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "mcp server not found"})
				return
			}
			target.Disabled = false
			manager.UpsertServer(*target)
			if err := manager.SaveConfig(a.writableMCPConfigPath()); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			if err := manager.ConnectServer(ctx, name); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
		case "close":
			cfgs := manager.ServerConfigs()
			var target *mcphost.ServerConfig
			for i := range cfgs {
				if strings.TrimSpace(cfgs[i].Name) == name {
					target = &cfgs[i]
					break
				}
			}
			if target == nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "mcp server not found"})
				return
			}
			target.Disabled = true
			manager.UpsertServer(*target)
			if err := manager.SaveConfig(a.writableMCPConfigPath()); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
		case "refresh":
			if err := manager.DisconnectServer(name); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			if err := manager.ConnectServer(ctx, name); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "mcp action not found"})
			return
		}
		a.audit("mcp_runtime.action", map[string]any{
			"sessionId": sessionID,
			"operator":  a.auditOperator(sessionID),
			"name":      name,
			"action":    parts[1],
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"items": buildMCPRuntimeItems(manager),
		})
		return
	}
	switch r.Method {
	case http.MethodGet:
		for _, item := range buildMCPRuntimeItems(manager) {
			if item.Name == name {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "mcp server not found"})
	case http.MethodPut:
		var cfg mcphost.ServerConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		cfg.Name = name
		cfg, err = normalizeMCPServerConfig(cfg)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		manager.UpsertServer(cfg)
		if err := manager.SaveConfig(a.writableMCPConfigPath()); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if !cfg.Disabled {
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			if err := manager.ConnectServer(ctx, cfg.Name); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"items": buildMCPRuntimeItems(manager),
		})
	case http.MethodDelete:
		if err := manager.RemoveServer(name); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		if err := manager.SaveConfig(a.writableMCPConfigPath()); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		a.audit("mcp_runtime.deleted", map[string]any{
			"sessionId": sessionID,
			"operator":  a.auditOperator(sessionID),
			"name":      name,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"items": buildMCPRuntimeItems(manager),
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
