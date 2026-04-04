package model

// LabTopologyNode represents a single node in a lab topology.
type LabTopologyNode struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Role     string            `json:"role,omitempty"`     // e.g. "web", "db", "cache", "lb"
	OS       string            `json:"os,omitempty"`       // e.g. "linux", "windows"
	Services []string          `json:"services,omitempty"` // e.g. ["nginx", "mysql"]
	Labels   map[string]string `json:"labels,omitempty"`
}

// LabTopologyLink represents a connection between two topology nodes.
type LabTopologyLink struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Protocol string `json:"protocol,omitempty"` // e.g. "tcp", "http", "grpc"
	Port     int    `json:"port,omitempty"`
}

// LabTopology describes the virtual network topology of a lab environment.
type LabTopology struct {
	Nodes []LabTopologyNode `json:"nodes,omitempty"`
	Links []LabTopologyLink `json:"links,omitempty"`
}

// LabEnvironment represents a sandboxed lab environment for fault injection
// and chaos engineering exercises.
type LabEnvironment struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Status      string            `json:"status"`      // draft, running, stopped, error
	Scenario    string            `json:"scenario,omitempty"` // scenario template name
	Topology    LabTopology       `json:"topology"`
	MockHostIDs []string          `json:"mockHostIds,omitempty"` // registered mock host IDs
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedAt   string            `json:"createdAt"`
	UpdatedAt   string            `json:"updatedAt"`
}
