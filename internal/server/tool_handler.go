package server

import "context"

// ToolDescriptor describes the static capabilities of a tool handler.
type ToolDescriptor struct {
	Name                      string
	Domain                    string
	Kind                      string
	DisplayLabel              string
	StartPhase                string
	RequiresApproval          bool
	IsReadOnly                bool
	SupportsStreamingProgress bool
	ProjectionHints           []string
}

// Clone returns a shallow copy of the descriptor with copied slice fields.
func (d ToolDescriptor) Clone() ToolDescriptor {
	if d.ProjectionHints != nil {
		d.ProjectionHints = append([]string(nil), d.ProjectionHints...)
	}
	return d
}

// ToolHandler executes a single tool invocation.
type ToolHandler interface {
	Descriptor() ToolDescriptor
	Execute(ctx context.Context, inv ToolInvocation) (ToolExecutionResult, error)
}

// ToolHandlerFunc adapts a function into a ToolHandler.
type ToolHandlerFunc struct {
	Desc ToolDescriptor
	Fn   func(ctx context.Context, inv ToolInvocation) (ToolExecutionResult, error)
}

// Descriptor returns the static metadata for the handler.
func (h ToolHandlerFunc) Descriptor() ToolDescriptor {
	return h.Desc.Clone()
}

// Execute invokes the wrapped function.
func (h ToolHandlerFunc) Execute(ctx context.Context, inv ToolInvocation) (ToolExecutionResult, error) {
	if h.Fn == nil {
		return ToolExecutionResult{}, nil
	}
	return h.Fn(ctx, inv)
}
