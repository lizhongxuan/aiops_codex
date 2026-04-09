package server

import (
	"fmt"
	"log"
	"sync"
)

// toolDispatchCategory classifies a tool for dispatch scheduling.
type toolDispatchCategory string

const (
	toolCategoryReadonly toolDispatchCategory = "readonly"
	toolCategoryMutation toolDispatchCategory = "mutation"
	toolCategoryBlocking toolDispatchCategory = "blocking"
	toolCategoryApproval toolDispatchCategory = "approval"
)

// toolDispatchRequest represents a single tool to be dispatched.
type toolDispatchRequest struct {
	CallID   string
	ToolName string
	Input    map[string]any
	HostID   string
	Category toolDispatchCategory
}

// toolDispatchResult represents the result of a tool dispatch.
type toolDispatchResult struct {
	CallID   string
	ToolName string
	Output   map[string]any
	Error    error
	Blocking bool // true if the tool paused the loop
}

// categorizeToolForDispatch determines the dispatch category of a tool.
func categorizeToolForDispatch(toolName string) toolDispatchCategory {
	switch toolName {
	case "ask_user_question":
		return toolCategoryBlocking
	case "exit_plan_mode", "request_approval":
		return toolCategoryApproval
	case "query_ai_server_state", "readonly_host_inspect",
		"execute_readonly_query", "list_remote_files",
		"read_remote_file", "search_remote_files":
		return toolCategoryReadonly
	case "orchestrator_dispatch_tasks", "execute_system_mutation":
		return toolCategoryMutation
	case "enter_plan_mode", "update_plan":
		return toolCategoryReadonly // plan tools are non-destructive
	default:
		return toolCategoryMutation // default to mutation for safety
	}
}

// toolDispatcher manages the execution of tool batches.
type toolDispatcher struct {
	app *App
}

// newToolDispatcher creates a new tool dispatcher.
func newToolDispatcher(app *App) *toolDispatcher {
	return &toolDispatcher{app: app}
}

// dispatchBatch dispatches a batch of tool requests according to parallelism rules.
// Returns results and whether any tool caused the loop to block.
func (d *toolDispatcher) dispatchBatch(requests []toolDispatchRequest) ([]toolDispatchResult, bool) {
	if len(requests) == 0 {
		return nil, false
	}

	// Separate tools by category
	var readonlyTools []toolDispatchRequest
	var mutationTools []toolDispatchRequest
	var blockingTools []toolDispatchRequest
	var approvalTools []toolDispatchRequest

	for _, req := range requests {
		switch req.Category {
		case toolCategoryReadonly:
			readonlyTools = append(readonlyTools, req)
		case toolCategoryMutation:
			mutationTools = append(mutationTools, req)
		case toolCategoryBlocking:
			blockingTools = append(blockingTools, req)
		case toolCategoryApproval:
			approvalTools = append(approvalTools, req)
		}
	}

	var allResults []toolDispatchResult
	loopBlocked := false

	// 1. Execute blocking tools first (they pause the loop)
	if len(blockingTools) > 0 {
		for _, req := range blockingTools {
			result := toolDispatchResult{
				CallID:   req.CallID,
				ToolName: req.ToolName,
				Blocking: true,
			}
			allResults = append(allResults, result)
			loopBlocked = true
			log.Printf("[tool_dispatcher] blocking tool=%s call=%s", req.ToolName, req.CallID)
		}
		return allResults, loopBlocked
	}

	// 2. Execute approval tools (they also pause the loop)
	if len(approvalTools) > 0 {
		for _, req := range approvalTools {
			result := toolDispatchResult{
				CallID:   req.CallID,
				ToolName: req.ToolName,
				Blocking: true,
			}
			allResults = append(allResults, result)
			loopBlocked = true
			log.Printf("[tool_dispatcher] approval tool=%s call=%s", req.ToolName, req.CallID)
		}
		return allResults, loopBlocked
	}

	// 3. Execute readonly tools in parallel
	if len(readonlyTools) > 0 {
		readonlyResults := d.executeParallel(readonlyTools)
		allResults = append(allResults, readonlyResults...)
	}

	// 4. Execute mutation tools serially (grouped by host)
	if len(mutationTools) > 0 {
		mutationResults := d.executeSerial(mutationTools)
		allResults = append(allResults, mutationResults...)
	}

	return allResults, loopBlocked
}

// executeParallel executes readonly tools concurrently.
func (d *toolDispatcher) executeParallel(requests []toolDispatchRequest) []toolDispatchResult {
	results := make([]toolDispatchResult, len(requests))
	var wg sync.WaitGroup

	for i, req := range requests {
		wg.Add(1)
		go func(idx int, r toolDispatchRequest) {
			defer wg.Done()
			log.Printf("[tool_dispatcher] parallel exec tool=%s call=%s", r.ToolName, r.CallID)
			results[idx] = toolDispatchResult{
				CallID:   r.CallID,
				ToolName: r.ToolName,
				// Actual execution is handled by handleDynamicToolCall
			}
		}(i, req)
	}

	wg.Wait()
	return results
}

// executeSerial executes mutation tools one at a time, grouped by host.
func (d *toolDispatcher) executeSerial(requests []toolDispatchRequest) []toolDispatchResult {
	// Group by host
	hostGroups := make(map[string][]toolDispatchRequest)
	for _, req := range requests {
		hostID := req.HostID
		if hostID == "" {
			hostID = "default"
		}
		hostGroups[hostID] = append(hostGroups[hostID], req)
	}

	var results []toolDispatchResult
	for hostID, group := range hostGroups {
		for _, req := range group {
			log.Printf("[tool_dispatcher] serial exec tool=%s call=%s host=%s", req.ToolName, req.CallID, hostID)
			results = append(results, toolDispatchResult{
				CallID:   req.CallID,
				ToolName: req.ToolName,
			})
		}
	}

	return results
}

// buildDispatchRequests converts tool call data into dispatch requests.
func buildDispatchRequests(toolCalls []map[string]any) []toolDispatchRequest {
	requests := make([]toolDispatchRequest, 0, len(toolCalls))
	for _, call := range toolCalls {
		name := getStringAny(call, "name", "tool")
		callID := getStringAny(call, "id", "callId")
		input, _ := call["input"].(map[string]any)
		if input == nil {
			input, _ = call["arguments"].(map[string]any)
		}
		hostID := getStringAny(input, "hostId", "host_id")

		requests = append(requests, toolDispatchRequest{
			CallID:   callID,
			ToolName: name,
			Input:    input,
			HostID:   hostID,
			Category: categorizeToolForDispatch(name),
		})
	}
	return requests
}

// validateToolPermission checks if a tool is allowed given the current permission mode.
func validateToolPermission(toolName, permissionMode string, planMode bool) error {
	category := categorizeToolForDispatch(toolName)

	if planMode && category == toolCategoryMutation {
		return fmt.Errorf("tool %q is not allowed in plan mode", toolName)
	}

	if permissionMode == "readonly" && category == toolCategoryMutation {
		return fmt.Errorf("tool %q requires mutation permission, current mode is readonly", toolName)
	}

	return nil
}
