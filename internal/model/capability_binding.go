package model

// CapabilityBinding represents a directional binding between a source entity
// (e.g. an agent profile) and a target capability (e.g. a skill or MCP server).
type CapabilityBinding struct {
	ID         string `json:"id"`
	SourceType string `json:"sourceType"`           // e.g. "profile"
	SourceID   string `json:"sourceId"`
	TargetType string `json:"targetType"`            // e.g. "skill", "mcp"
	TargetID   string `json:"targetId"`
	Status     string `json:"status,omitempty"`       // e.g. "active", "disabled"
	CreatedAt  string `json:"createdAt,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}
