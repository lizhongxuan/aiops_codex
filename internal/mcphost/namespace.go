package mcphost

import (
	"fmt"
	"strings"
)

const namespaceSeparator = "__"

// NamespacedTool represents a tool with server namespace prefix.
type NamespacedTool struct {
	CallableName string `json:"callable_name"` // "server__tool_name"
	OriginalName string `json:"original_name"`
	ServerName   string `json:"server_name"`
	Description  string `json:"description"`
}

// NamespaceTools prefixes tool names with server namespace to create unique callable names.
func NamespaceTools(serverName string, tools []ToolDefinition) []NamespacedTool {
	result := make([]NamespacedTool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, NamespacedTool{
			CallableName: serverName + namespaceSeparator + tool.Name,
			OriginalName: tool.Name,
			ServerName:   serverName,
			Description:  fmt.Sprintf("[%s] %s", serverName, tool.Description),
		})
	}
	return result
}

// ResolveNamespacedTool extracts server and original tool name from a callable name.
func ResolveNamespacedTool(callableName string) (serverName, toolName string, err error) {
	idx := strings.Index(callableName, namespaceSeparator)
	if idx < 0 {
		return "", "", fmt.Errorf("invalid namespaced tool name %q: missing separator %q", callableName, namespaceSeparator)
	}

	serverName = callableName[:idx]
	toolName = callableName[idx+len(namespaceSeparator):]

	if serverName == "" {
		return "", "", fmt.Errorf("invalid namespaced tool name %q: empty server name", callableName)
	}
	if toolName == "" {
		return "", "", fmt.Errorf("invalid namespaced tool name %q: empty tool name", callableName)
	}

	return serverName, toolName, nil
}
