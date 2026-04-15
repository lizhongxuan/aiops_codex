package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// DynamicToolSpec describes a tool to be registered dynamically at runtime.
type DynamicToolSpec struct {
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Parameters       map[string]interface{} `json:"parameters,omitempty"`
	RequiresApproval bool                   `json:"requires_approval,omitempty"`
	IsReadOnly       bool                   `json:"is_read_only,omitempty"`
	// Handler is set programmatically; not serialized.
	Handler ToolHandler `json:"-"`
}

// RegisterDynamic adds a dynamically-specified tool to the registry.
// It validates the spec and returns an error if the spec is invalid.
func (r *ToolRegistry) RegisterDynamic(spec DynamicToolSpec) error {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return fmt.Errorf("dynamic tool registration: name is required")
	}
	if strings.TrimSpace(spec.Description) == "" {
		return fmt.Errorf("dynamic tool registration: description is required for %q", name)
	}

	params := spec.Parameters
	if params == nil {
		params = map[string]interface{}{
			"type":                 "object",
			"properties":          map[string]interface{}{},
			"additionalProperties": true,
		}
	}

	handler := spec.Handler
	if handler == nil {
		handler = func(ctx context.Context, tc ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
			argsJSON, _ := json.Marshal(args)
			return fmt.Sprintf("Dynamic tool %q called with args: %s (no handler wired)", name, string(argsJSON)), nil
		}
	}

	r.Register(ToolEntry{
		Name:             name,
		Description:      spec.Description,
		Parameters:       params,
		Handler:          handler,
		RequiresApproval: spec.RequiresApproval,
		IsReadOnly:       spec.IsReadOnly,
	})

	return nil
}

// UnregisterDynamic removes a dynamically registered tool by name.
func (r *ToolRegistry) UnregisterDynamic(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; ok {
		delete(r.tools, name)
		return true
	}
	return false
}
