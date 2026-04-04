package model

// ScriptConfigProfile describes a reusable configuration instance for a
// Runner script. It captures the parameter schema, default values,
// environment binding, approval policy and runner profile so that script
// execution is reproducible, auditable and rollback-friendly.
type ScriptConfigProfile struct {
	ID              string         `json:"id"`
	ScriptName      string         `json:"scriptName"`
	Description     string         `json:"description,omitempty"`
	ArgSchema       map[string]any `json:"argSchema,omitempty"`
	Defaults        map[string]any `json:"defaults,omitempty"`
	EnvironmentRef  string         `json:"environmentRef,omitempty"`
	InventoryPreset string         `json:"inventoryPreset,omitempty"`
	ApprovalPolicy  string         `json:"approvalPolicy,omitempty"` // none | required | auto
	RunnerProfile   string         `json:"runnerProfile,omitempty"`
	Status          string         `json:"status"`                   // active | draft | disabled
	CreatedAt       string         `json:"createdAt"`
	UpdatedAt       string         `json:"updatedAt"`
}
