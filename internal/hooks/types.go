package hooks

// HookEvent represents the lifecycle event that triggers a hook.
type HookEvent string

const (
	// EventSessionStart fires when a new session begins.
	EventSessionStart HookEvent = "session_start"
	// EventPreToolUse fires before a tool invocation is executed.
	EventPreToolUse HookEvent = "pre_tool_use"
	// EventPostToolUse fires after a tool invocation completes.
	EventPostToolUse HookEvent = "post_tool_use"
	// EventPromptSubmit fires when the user submits a prompt.
	EventPromptSubmit HookEvent = "prompt_submit"
)

// HookOutcome is the result returned by a hook handler.
type HookOutcome struct {
	// Block indicates whether the action should be blocked.
	Block bool
	// BlockReason explains why the action was blocked.
	BlockReason string
	// AdditionalContexts holds extra context strings to inject into the prompt.
	AdditionalContexts []string
	// ModifiedInput holds the rewritten user input (prompt_submit only).
	ModifiedInput string
}

// PreToolUseRequest is the payload passed to pre_tool_use hooks.
type PreToolUseRequest struct {
	// ToolName is the name of the tool about to be invoked.
	ToolName string
	// ToolInput is the raw input arguments for the tool.
	ToolInput map[string]interface{}
}

// PostToolUseRequest is the payload passed to post_tool_use hooks.
type PostToolUseRequest struct {
	// ToolName is the name of the tool that was invoked.
	ToolName string
	// ToolInput is the raw input arguments that were passed to the tool.
	ToolInput map[string]interface{}
	// ToolResult is the output returned by the tool.
	ToolResult interface{}
}

// HookHandler is the function signature for hook implementations.
// It receives a generic payload and returns an outcome or error.
type HookHandler func(payload interface{}) (HookOutcome, error)

// Hook represents a registered hook with its metadata.
type Hook struct {
	// Name identifies this hook for logging and debugging.
	Name string
	// Event is the lifecycle event this hook listens to.
	Event HookEvent
	// Handler is the function invoked when the event fires.
	Handler HookHandler
}
