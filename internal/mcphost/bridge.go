package mcphost

import (
	"context"
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// RegisterMCPTools discovers all tools from connected MCP servers and registers
// them into the agentloop ToolRegistry. This bridges the MCP world into the
// agent loop's tool dispatch system.
func RegisterMCPTools(reg *agentloop.ToolRegistry, mgr *Manager) {
	tools := mgr.AllTools()
	for _, tool := range tools {
		// Capture loop variables.
		td := tool
		serverName := td.ServerName
		toolName := td.Name

		// Namespace MCP tools to avoid collisions: "mcp_<server>_<tool>".
		registeredName := fmt.Sprintf("mcp_%s_%s", serverName, toolName)

		params := td.InputSchema
		if params == nil {
			params = map[string]interface{}{
				"type":                 "object",
				"properties":          map[string]interface{}{},
				"additionalProperties": true,
			}
		}

		isAutoApproved := mgr.IsAutoApproved(serverName, toolName)

		reg.Register(agentloop.ToolEntry{
			Name:        registeredName,
			Description: fmt.Sprintf("[MCP:%s] %s", serverName, td.Description),
			Parameters:  params,
			IsReadOnly:  isAutoApproved,
			RequiresApproval: !isAutoApproved,
			Handler: func(ctx context.Context, tc agentloop.ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
				resp, err := mgr.CallTool(ctx, serverName, ToolCallRequest{
					Name:      toolName,
					Arguments: args,
				})
				if err != nil {
					return fmt.Sprintf("MCP tool %s error: %v", toolName, err), nil
				}
				if resp.IsError {
					var sb strings.Builder
					sb.WriteString("MCP tool returned error:\n")
					for _, block := range resp.Content {
						if block.Text != "" {
							sb.WriteString(block.Text)
							sb.WriteString("\n")
						}
					}
					return sb.String(), nil
				}
				var sb strings.Builder
				for _, block := range resp.Content {
					if block.Text != "" {
						sb.WriteString(block.Text)
						sb.WriteString("\n")
					}
				}
				return sb.String(), nil
			},
		})
	}
}
