package model

// MonitorContext carries monitoring context from the frontend (e.g. Coroot
// overview page) so the AI can reason about the current monitoring state.
type MonitorContext struct {
	Source       string         `json:"source"`                 // coroot
	ResourceType string        `json:"resourceType"`           // service | host | cluster
	ResourceID   string        `json:"resourceId"`
	HostIDs      []string      `json:"hostIds,omitempty"`
	TimeRange    string        `json:"timeRange"`
	Panels       []any         `json:"panels,omitempty"`
	Alerts       []any         `json:"alerts,omitempty"`
	Topology     map[string]any `json:"topology,omitempty"`
}

// MonitorContextPromptPrefix builds a short system-level preamble that
// describes the monitoring context so the LLM can ground its answers.
func MonitorContextPromptPrefix(mc MonitorContext) string {
	if mc.Source == "" && mc.ResourceType == "" && mc.ResourceID == "" {
		return ""
	}
	prefix := "[Monitor Context] source=" + mc.Source +
		" resourceType=" + mc.ResourceType +
		" resourceId=" + mc.ResourceID
	if mc.TimeRange != "" {
		prefix += " timeRange=" + mc.TimeRange
	}
	if len(mc.HostIDs) > 0 {
		prefix += " hostIds="
		for i, id := range mc.HostIDs {
			if i > 0 {
				prefix += ","
			}
			prefix += id
		}
	}
	return prefix
}
