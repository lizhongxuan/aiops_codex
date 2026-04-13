package hooks

import "log"

// Runtime is the hook execution engine. It holds a registry and provides
// typed execution methods for each hook event.
type Runtime struct {
	registry *Registry
}

// NewRuntime creates a new hook Runtime with an empty registry.
func NewRuntime() *Runtime {
	return &Runtime{
		registry: NewRegistry(),
	}
}

// Register adds a hook to the runtime's internal registry.
func (rt *Runtime) Register(hook Hook) {
	rt.registry.Register(hook)
}

// SessionStartResult holds the aggregated outcome of session_start hooks.
type SessionStartResult struct {
	AdditionalContexts []string
}

// ExecuteSessionStart runs all session_start hooks. Errors are logged and
// execution continues (fail-open). Additional contexts are aggregated.
func (rt *Runtime) ExecuteSessionStart() SessionStartResult {
	var result SessionStartResult
	hooks := rt.registry.Get(EventSessionStart)
	for _, h := range hooks {
		outcome, err := h.Handler(nil)
		if err != nil {
			log.Printf("hooks: session_start hook %q failed: %v", h.Name, err)
			continue
		}
		result.AdditionalContexts = append(result.AdditionalContexts, outcome.AdditionalContexts...)
	}
	return result
}

// PreToolUseResult holds the aggregated outcome of pre_tool_use hooks.
type PreToolUseResult struct {
	Block              bool
	BlockReason        string
	AdditionalContexts []string
}

// ExecutePreToolUse runs all pre_tool_use hooks. If any hook blocks, execution
// stops and the block reason is returned. Additional contexts are aggregated
// from hooks that ran before the block.
func (rt *Runtime) ExecutePreToolUse(req PreToolUseRequest) PreToolUseResult {
	var result PreToolUseResult
	hooks := rt.registry.Get(EventPreToolUse)
	for _, h := range hooks {
		outcome, err := h.Handler(req)
		if err != nil {
			log.Printf("hooks: pre_tool_use hook %q failed: %v", h.Name, err)
			continue
		}
		result.AdditionalContexts = append(result.AdditionalContexts, outcome.AdditionalContexts...)
		if outcome.Block {
			result.Block = true
			result.BlockReason = outcome.BlockReason
			return result
		}
	}
	return result
}

// PostToolUseResult holds the aggregated outcome of post_tool_use hooks.
type PostToolUseResult struct {
	AdditionalContexts []string
}

// ExecutePostToolUse runs all post_tool_use hooks. The tool result is passed
// to each handler. Errors are logged and execution continues.
func (rt *Runtime) ExecutePostToolUse(req PostToolUseRequest) PostToolUseResult {
	var result PostToolUseResult
	hooks := rt.registry.Get(EventPostToolUse)
	for _, h := range hooks {
		outcome, err := h.Handler(req)
		if err != nil {
			log.Printf("hooks: post_tool_use hook %q failed: %v", h.Name, err)
			continue
		}
		result.AdditionalContexts = append(result.AdditionalContexts, outcome.AdditionalContexts...)
	}
	return result
}

// PromptSubmitResult holds the aggregated outcome of prompt_submit hooks.
type PromptSubmitResult struct {
	Block         bool
	BlockReason   string
	ModifiedInput string
}

// ExecutePromptSubmit runs all prompt_submit hooks. If any hook blocks,
// execution stops. The last non-empty ModifiedInput wins.
func (rt *Runtime) ExecutePromptSubmit(userInput string) PromptSubmitResult {
	var result PromptSubmitResult
	result.ModifiedInput = userInput

	hooks := rt.registry.Get(EventPromptSubmit)
	for _, h := range hooks {
		outcome, err := h.Handler(userInput)
		if err != nil {
			log.Printf("hooks: prompt_submit hook %q failed: %v", h.Name, err)
			continue
		}
		if outcome.Block {
			result.Block = true
			result.BlockReason = outcome.BlockReason
			return result
		}
		if outcome.ModifiedInput != "" {
			result.ModifiedInput = outcome.ModifiedInput
			// Pass modified input to subsequent hooks.
			userInput = outcome.ModifiedInput
		}
	}
	return result
}
