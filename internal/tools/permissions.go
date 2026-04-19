package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// RegisterRequestPermissionsTool registers the request_permissions tool.
func RegisterRequestPermissionsTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "request_permissions",
		Description: "Submit a structured permission request for filesystem paths and network hosts.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filesystem": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{
								"type":        "string",
								"description": "Filesystem path to request access to.",
							},
							"mode": map[string]interface{}{
								"type":        "string",
								"enum":        []string{"read", "write", "execute"},
								"description": "Access mode requested.",
							},
							"reason": map[string]interface{}{
								"type":        "string",
								"description": "Reason for requesting this permission.",
							},
						},
						"required": []string{"path", "mode"},
					},
					"description": "Filesystem permission requests.",
				},
				"network": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"host": map[string]interface{}{
								"type":        "string",
								"description": "Network host to request access to.",
							},
							"port": map[string]interface{}{
								"type":        "integer",
								"description": "Port number (0 for any).",
							},
							"reason": map[string]interface{}{
								"type":        "string",
								"description": "Reason for requesting this permission.",
							},
						},
						"required": []string{"host"},
					},
					"description": "Network permission requests.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Overall reason for the permission request.",
				},
			},
			"additionalProperties": false,
		},
		Handler:          handleRequestPermissions,
		RequiresApproval: true,
	})
}

// PermissionRequest represents a structured permission request.
type PermissionRequest struct {
	Filesystem []FilesystemPermission `json:"filesystem,omitempty"`
	Network    []NetworkPermission    `json:"network,omitempty"`
	Reason     string                 `json:"reason,omitempty"`
}

// FilesystemPermission represents a single filesystem permission request.
type FilesystemPermission struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Reason string `json:"reason,omitempty"`
}

// NetworkPermission represents a single network permission request.
type NetworkPermission struct {
	Host   string `json:"host"`
	Port   int    `json:"port,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func handleRequestPermissions(ctx context.Context, tc ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	req := PermissionRequest{}

	if reason, ok := args["reason"].(string); ok {
		req.Reason = reason
	}

	// Parse filesystem permissions.
	if fsRaw, ok := args["filesystem"]; ok {
		data, err := json.Marshal(fsRaw)
		if err == nil {
			var perms []FilesystemPermission
			if err := json.Unmarshal(data, &perms); err == nil {
				req.Filesystem = perms
			}
		}
	}

	// Parse network permissions.
	if netRaw, ok := args["network"]; ok {
		data, err := json.Marshal(netRaw)
		if err == nil {
			var perms []NetworkPermission
			if err := json.Unmarshal(data, &perms); err == nil {
				req.Network = perms
			}
		}
	}

	if len(req.Filesystem) == 0 && len(req.Network) == 0 {
		return "", fmt.Errorf("request_permissions: at least one filesystem or network permission must be specified")
	}

	out, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", fmt.Errorf("request_permissions: %w", err)
	}

	return fmt.Sprintf("Permission request submitted:\n%s", string(out)), nil
}
