package server

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// toolInvocationTracker manages ToolInvocation lifecycle for a session.
type toolInvocationTracker struct {
	app       *App
	sessionID string
	runID     string
}

// newToolInvocationTracker creates a tracker for the given session and run.
func newToolInvocationTracker(app *App, sessionID, runID string) *toolInvocationTracker {
	return &toolInvocationTracker{
		app:       app,
		sessionID: sessionID,
		runID:     runID,
	}
}

// beginInvocation creates a ToolInvocation record with status=running.
func (t *toolInvocationTracker) beginInvocation(toolName string, input map[string]any) string {
	invocationID := model.NewID("inv")
	now := model.NowString()

	inputJSON, _ := json.Marshal(input)
	inputSummary := summarizeToolInput(toolName, input)

	invocation := model.ToolInvocation{
		ID:           invocationID,
		RunID:        t.runID,
		Name:         toolName,
		Status:       "running",
		InputJSON:    string(inputJSON),
		InputSummary: inputSummary,
		StartedAt:    now,
	}

	t.app.store.RememberItem(t.sessionID, invocationID, map[string]any{
		"kind":         "tool_invocation",
		"invocationId": invocationID,
		"name":         toolName,
		"status":       "running",
		"inputJson":    string(inputJSON),
		"inputSummary": inputSummary,
		"startedAt":    now,
	})

	log.Printf("[tool_tracker] begin invocation=%s tool=%s session=%s run=%s", invocationID, toolName, t.sessionID, t.runID)
	_ = invocation // suppress unused warning if needed
	return invocationID
}

// completeInvocation updates a ToolInvocation with output and completed status.
func (t *toolInvocationTracker) completeInvocation(invocationID, toolName string, output map[string]any, evidenceID string) {
	now := model.NowString()
	outputJSON, _ := json.Marshal(output)
	outputSummary := summarizeToolOutput(toolName, output)

	t.app.store.RememberItem(t.sessionID, invocationID, map[string]any{
		"kind":          "tool_invocation",
		"invocationId":  invocationID,
		"name":          toolName,
		"status":        "completed",
		"outputJson":    string(outputJSON),
		"outputSummary": outputSummary,
		"evidenceId":    evidenceID,
		"completedAt":   now,
	})

	log.Printf("[tool_tracker] complete invocation=%s tool=%s session=%s evidence=%s", invocationID, toolName, t.sessionID, evidenceID)
}

// failInvocation marks a ToolInvocation as failed.
func (t *toolInvocationTracker) failInvocation(invocationID, toolName, errorMsg string) {
	now := model.NowString()

	t.app.store.RememberItem(t.sessionID, invocationID, map[string]any{
		"kind":          "tool_invocation",
		"invocationId":  invocationID,
		"name":          toolName,
		"status":        "failed",
		"outputSummary": fmt.Sprintf("Error: %s", truncate(errorMsg, 200)),
		"completedAt":   now,
	})

	log.Printf("[tool_tracker] fail invocation=%s tool=%s session=%s error=%s", invocationID, toolName, t.sessionID, truncate(errorMsg, 100))
}

// waitingInvocation marks a ToolInvocation as waiting for user or approval.
func (t *toolInvocationTracker) waitingInvocation(invocationID, toolName, waitType string) {
	t.app.store.RememberItem(t.sessionID, invocationID, map[string]any{
		"kind":         "tool_invocation",
		"invocationId": invocationID,
		"name":         toolName,
		"status":       waitType,
	})

	log.Printf("[tool_tracker] waiting invocation=%s tool=%s session=%s type=%s", invocationID, toolName, t.sessionID, waitType)
}

// summarizeToolInput creates a brief summary of tool input for display.
func summarizeToolInput(toolName string, input map[string]any) string {
	switch toolName {
	case "ask_user_question":
		if questions, ok := input["questions"].([]any); ok && len(questions) > 0 {
			if q, ok := questions[0].(map[string]any); ok {
				return fmt.Sprintf("问题: %s", truncate(fmt.Sprint(q["question"]), 80))
			}
		}
		return "澄清问题"
	case "readonly_host_inspect":
		cmd := fmt.Sprint(input["command"])
		host := fmt.Sprint(input["hostId"])
		return fmt.Sprintf("Host: %s | Command: %s", host, truncate(cmd, 60))
	case "enter_plan_mode":
		return fmt.Sprintf("目标: %s", truncate(fmt.Sprint(input["goal"]), 80))
	case "update_plan":
		return fmt.Sprintf("计划: %s", truncate(fmt.Sprint(input["title"]), 80))
	case "exit_plan_mode":
		return fmt.Sprintf("审批: %s", truncate(fmt.Sprint(input["title"]), 80))
	case "orchestrator_dispatch_tasks":
		if tasks, ok := input["tasks"].([]any); ok {
			return fmt.Sprintf("派发 %d 个任务", len(tasks))
		}
		return "派发任务"
	case "query_ai_server_state":
		return fmt.Sprintf("查询: %s", truncate(fmt.Sprint(input["focus"]), 80))
	case "request_approval":
		return fmt.Sprintf("审批: %s", truncate(fmt.Sprint(input["command"]), 80))
	default:
		return toolName
	}
}

// summarizeToolOutput creates a brief summary of tool output for display.
func summarizeToolOutput(toolName string, output map[string]any) string {
	if errMsg, ok := output["error"].(string); ok {
		return fmt.Sprintf("Error: %s", truncate(errMsg, 100))
	}
	success, _ := output["success"].(bool)
	if !success {
		return "执行失败"
	}
	switch toolName {
	case "query_ai_server_state":
		if hosts, ok := output["hostCount"]; ok {
			return fmt.Sprintf("Hosts: %v | 状态查询完成", hosts)
		}
		return "状态查询完成"
	default:
		return "执行完成"
	}
}
