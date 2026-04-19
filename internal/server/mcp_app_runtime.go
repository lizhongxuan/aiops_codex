package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/mcphost"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type mcpRuntime interface {
	LoadConfig(paths ...string) error
	ConnectAll(ctx context.Context)
	AllTools() []mcphost.ToolDefinition
	IsAutoApproved(serverName, toolName string) bool
	CallTool(ctx context.Context, serverName string, req mcphost.ToolCallRequest) (*mcphost.ToolCallResponse, error)
	ReadResource(ctx context.Context, serverName, uri string) (*mcphost.ToolCallResponse, error)
	DisconnectAll()
}

type mcpToolBinding struct {
	RegisteredName string
	ServerName     string
	ToolName       string
	ToolDef        mcphost.ToolDefinition
}

func (a *App) ensureMCPRuntime() (mcpRuntime, error) {
	if a == nil {
		return nil, nil
	}
	if a.mcpManager != nil {
		return a.mcpManager, nil
	}
	manager := mcphost.NewManager()
	if err := manager.LoadConfig(a.cfg.MCPConfigPaths...); err != nil {
		return nil, err
	}
	manager.ConnectAll(context.Background())
	a.mcpManager = manager
	return manager, nil
}

func (a *App) registerBifrostMCPTools(reg *agentloop.ToolRegistry) error {
	runtime, err := a.ensureMCPRuntime()
	if err != nil {
		return err
	}
	if runtime == nil {
		return nil
	}

	tools := runtime.AllTools()
	if len(tools) == 0 {
		a.mcpToolBindings = nil
		return nil
	}

	sort.Slice(tools, func(i, j int) bool {
		left := registeredMCPToolName(tools[i].ServerName, tools[i].Name)
		right := registeredMCPToolName(tools[j].ServerName, tools[j].Name)
		return left < right
	})

	bindings := make(map[string]mcpToolBinding, len(tools))
	for _, tool := range tools {
		registeredName := registeredMCPToolName(tool.ServerName, tool.Name)
		params := cloneAnyMap(tool.InputSchema)
		if len(params) == 0 {
			params = map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": true,
			}
		}
		binding := mcpToolBinding{
			RegisteredName: registeredName,
			ServerName:     tool.ServerName,
			ToolName:       tool.Name,
			ToolDef:        tool,
		}
		bindings[registeredName] = binding
		reg.Register(agentloop.ToolEntry{
			Name:             registeredName,
			Description:      formattedMCPToolDescription(binding),
			Parameters:       params,
			Handler:          agentloop.WrapSessionHandler(a.bifrostExecuteMCPTool(binding)),
			IsReadOnly:       runtime.IsAutoApproved(binding.ServerName, binding.ToolName),
			RequiresApproval: !runtime.IsAutoApproved(binding.ServerName, binding.ToolName),
		})
	}

	a.mcpToolBindings = bindings
	return nil
}

func registeredMCPToolName(serverName, toolName string) string {
	return fmt.Sprintf("mcp_%s_%s", strings.TrimSpace(serverName), strings.TrimSpace(toolName))
}

func formattedMCPToolDescription(binding mcpToolBinding) string {
	description := strings.TrimSpace(binding.ToolDef.Description)
	if description == "" {
		description = binding.ToolName
	}
	return fmt.Sprintf("[MCP:%s] %s", binding.ServerName, description)
}

func (a *App) bifrostExecuteMCPTool(binding mcpToolBinding) agentloop.SessionToolHandler {
	return func(ctx context.Context, _ *agentloop.Session, _ bifrost.ToolCall, arguments map[string]any) (string, error) {
		if a == nil || a.mcpManager == nil {
			return "MCP runtime is not initialized.", nil
		}
		resp, err := a.mcpManager.CallTool(ctx, binding.ServerName, mcphost.ToolCallRequest{
			Name:      binding.ToolName,
			Arguments: arguments,
		})
		if err != nil {
			return fmt.Sprintf("MCP tool %s error: %v", binding.ToolName, err), nil
		}
		return flattenMCPToolResponse(resp), nil
	}
}

func flattenMCPToolResponse(resp *mcphost.ToolCallResponse) string {
	if resp == nil {
		return ""
	}
	var builder strings.Builder
	if resp.IsError {
		builder.WriteString("MCP tool returned error:\n")
	}
	for _, block := range resp.Content {
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString(text)
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func (a *App) mcpDynamicTools() []map[string]any {
	if a == nil || len(a.mcpToolBindings) == 0 {
		return nil
	}
	names := make([]string, 0, len(a.mcpToolBindings))
	for name := range a.mcpToolBindings {
		names = append(names, name)
	}
	sort.Strings(names)

	tools := make([]map[string]any, 0, len(names))
	for _, name := range names {
		binding := a.mcpToolBindings[name]
		params := cloneAnyMap(binding.ToolDef.InputSchema)
		if len(params) == 0 {
			params = map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": true,
			}
		}
		tool := map[string]any{
			"name":        binding.RegisteredName,
			"description": formattedMCPToolDescription(binding),
			"inputSchema": params,
		}
		if len(binding.ToolDef.Meta) > 0 {
			tool["_meta"] = cloneAnyMap(binding.ToolDef.Meta)
		}
		tools = append(tools, tool)
	}
	return tools
}

func (a *App) mcpToolBinding(toolName string) (mcpToolBinding, bool) {
	if a == nil || len(a.mcpToolBindings) == 0 {
		return mcpToolBinding{}, false
	}
	binding, ok := a.mcpToolBindings[strings.TrimSpace(toolName)]
	return binding, ok
}

func mcpToolResourceURI(binding mcpToolBinding) string {
	meta := binding.ToolDef.Meta
	if len(meta) == 0 {
		return ""
	}
	ui, _ := meta["ui"].(map[string]any)
	if len(ui) == 0 {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(ui["resourceUri"]))
}

func extractMCPAppHTML(resp *mcphost.ToolCallResponse) (string, string) {
	if resp == nil {
		return "", ""
	}
	for _, item := range resp.Contents {
		html := strings.TrimSpace(item.Text)
		mimeType := strings.TrimSpace(item.MimeType)
		if html == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(mimeType), "text/html") {
			return html, mimeType
		}
	}
	return "", ""
}

func mcpToolResultCardID(processCardID string) string {
	if processCardID == "" {
		return model.NewID("msg")
	}
	return processCardID + "-result"
}

func mcpToolResultLooksLikeError(result string) bool {
	normalized := strings.ToLower(strings.TrimSpace(result))
	return strings.HasPrefix(normalized, "mcp tool returned error:") || strings.HasPrefix(normalized, "mcp tool ")
}

func (a *App) maybeCreateMCPResultCard(ctx context.Context, sessionID, toolName, processCardID, result string, execErr error) {
	binding, ok := a.mcpToolBinding(toolName)
	if !ok {
		return
	}

	detail := map[string]any{
		"tool":           binding.ToolName,
		"source":         "mcp",
		"mcpServer":      binding.ServerName,
		"registeredTool": binding.RegisteredName,
	}

	if resourceURI := mcpToolResourceURI(binding); resourceURI != "" && execErr == nil && !mcpToolResultLooksLikeError(result) && a.mcpManager != nil {
		if resourceResp, err := a.mcpManager.ReadResource(ctx, binding.ServerName, resourceURI); err == nil {
			if html, mimeType := extractMCPAppHTML(resourceResp); html != "" {
				detail["mcpApp"] = map[string]any{
					"serverName":  binding.ServerName,
					"toolName":    binding.ToolName,
					"resourceUri": resourceURI,
					"mimeType":    mimeType,
					"html":        html,
				}
			}
		}
	}

	text := strings.TrimSpace(result)
	if text == "" {
		text = fmt.Sprintf("MCP 工具 %s 已返回结果。", binding.ToolName)
	}
	status := "completed"
	if execErr != nil || mcpToolResultLooksLikeError(result) {
		status = "failed"
	}

	now := model.NowString()
	a.store.UpsertCard(sessionID, model.Card{
		ID:        mcpToolResultCardID(processCardID),
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      text,
		Status:    status,
		Detail:    detail,
		CreatedAt: now,
		UpdatedAt: now,
	})
}
