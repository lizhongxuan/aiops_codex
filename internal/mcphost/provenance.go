package mcphost

// ToolSource identifies where a tool was registered from.
type ToolSource string

const (
	SourceBuiltin ToolSource = "builtin"
	SourceMCP     ToolSource = "mcp"
	SourceDynamic ToolSource = "dynamic"
	SourceSkill   ToolSource = "skill"
)

// ToolProvenance records the origin of a registered tool.
type ToolProvenance struct {
	Source     ToolSource `json:"source"`      // "builtin", "mcp", "dynamic", "skill"
	SourceName string     `json:"source_name"` // Server name, skill name, etc.
}

// ToolEntryWithProvenance extends a ToolDefinition with provenance tracking.
type ToolEntryWithProvenance struct {
	ToolDefinition
	Provenance ToolProvenance `json:"provenance"`
}

// ProvenanceRegistry tracks provenance for all registered tools.
type ProvenanceRegistry struct {
	tools map[string]ToolProvenance // tool name → provenance
}

// NewProvenanceRegistry creates a new ProvenanceRegistry.
func NewProvenanceRegistry() *ProvenanceRegistry {
	return &ProvenanceRegistry{
		tools: make(map[string]ToolProvenance),
	}
}

// Register records provenance for a tool.
func (r *ProvenanceRegistry) Register(toolName string, provenance ToolProvenance) {
	r.tools[toolName] = provenance
}

// Get returns the provenance for a tool.
func (r *ProvenanceRegistry) Get(toolName string) (ToolProvenance, bool) {
	p, ok := r.tools[toolName]
	return p, ok
}

// Remove removes provenance tracking for a tool.
func (r *ProvenanceRegistry) Remove(toolName string) {
	delete(r.tools, toolName)
}

// All returns all tool provenance entries.
func (r *ProvenanceRegistry) All() map[string]ToolProvenance {
	out := make(map[string]ToolProvenance, len(r.tools))
	for k, v := range r.tools {
		out[k] = v
	}
	return out
}

// ToolsFromSource returns all tool names from a specific source.
func (r *ProvenanceRegistry) ToolsFromSource(source ToolSource) []string {
	var result []string
	for name, p := range r.tools {
		if p.Source == source {
			result = append(result, name)
		}
	}
	return result
}
