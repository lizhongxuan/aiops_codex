package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// RegisterSubagentTools registers the subagent management tools into the
// ToolRegistry. These tools allow the main agent to spawn, communicate with,
// wait for, and close subagents — mirroring Codex's multi_agents handler.
func RegisterSubagentTools(reg *ToolRegistry, ac *AgentControl) {
	reg.Register(ToolEntry{
		Name:        "spawn_agent",
		Description: "Spawn a new subagent to handle a specific subtask in parallel. The subagent runs independently with its own context and tools.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The task instruction for the subagent.",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Optional model override for the subagent.",
				},
				"tools": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional tool subset for the subagent. Empty inherits parent tools.",
				},
				"max_iterations": map[string]interface{}{
					"type":        "integer",
					"minimum":     1,
					"maximum":     100,
					"description": "Optional iteration budget override.",
				},
			},
			"required":             []string{"prompt"},
			"additionalProperties": false,
		},
		Handler: func(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
			prompt, _ := args["prompt"].(string)
			model, _ := args["model"].(string)
			maxIter := 0
			if v, ok := args["max_iterations"].(float64); ok {
				maxIter = int(v)
			}
			var tools []string
			if t, ok := args["tools"].([]interface{}); ok {
				for _, item := range t {
					if s, ok := item.(string); ok {
						tools = append(tools, s)
					}
				}
			}

			agent, err := ac.SpawnAgent(ctx, session, SpawnAgentRequest{
				Prompt:        prompt,
				Model:         model,
				Tools:         tools,
				MaxIterations: maxIter,
				ParentID:      AgentID(session.ID),
			})
			if err != nil {
				return fmt.Sprintf("Failed to spawn agent: %v", err), nil
			}
			return fmt.Sprintf("Subagent spawned: %s", agent.ID), nil
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "wait_agent",
		Description: "Wait for one or more subagents to complete and return their results.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_ids": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "List of subagent IDs to wait for.",
					"minItems":    1,
				},
			},
			"required":             []string{"agent_ids"},
			"additionalProperties": false,
		},
		Handler: func(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
			rawIDs, ok := args["agent_ids"].([]interface{})
			if !ok || len(rawIDs) == 0 {
				return "Error: agent_ids must be a non-empty array", nil
			}
			ids := make([]AgentID, 0, len(rawIDs))
			for _, raw := range rawIDs {
				if s, ok := raw.(string); ok {
					ids = append(ids, AgentID(s))
				}
			}
			results, err := ac.WaitMultiple(ctx, ids)
			if err != nil {
				return fmt.Sprintf("Error waiting for agents: %v", err), nil
			}
			out, _ := json.Marshal(results)
			return string(out), nil
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "send_agent_input",
		Description: "Send additional input or instructions to a running subagent.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "The subagent ID to send input to.",
				},
				"input": map[string]interface{}{
					"type":        "string",
					"description": "The message to send to the subagent.",
				},
			},
			"required":             []string{"agent_id", "input"},
			"additionalProperties": false,
		},
		Handler: func(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
			agentID, _ := args["agent_id"].(string)
			input, _ := args["input"].(string)
			if err := ac.SendInput(ctx, AgentID(agentID), input); err != nil {
				return fmt.Sprintf("Error: %v", err), nil
			}
			return fmt.Sprintf("Input sent to agent %s", agentID), nil
		},
	})

	reg.Register(ToolEntry{
		Name:        "close_agent",
		Description: "Cancel and close a subagent and all its descendants.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "The subagent ID to close.",
				},
			},
			"required":             []string{"agent_id"},
			"additionalProperties": false,
		},
		Handler: func(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
			agentID, _ := args["agent_id"].(string)
			if err := ac.CloseAgent(AgentID(agentID)); err != nil {
				return fmt.Sprintf("Error: %v", err), nil
			}
			return fmt.Sprintf("Agent %s closed", agentID), nil
		},
	})

	reg.Register(ToolEntry{
		Name:        "list_agents",
		Description: "List all active subagents and their current status.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Why you need to list agents.",
				},
			},
			"additionalProperties": false,
		},
		Handler: func(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
			parentID := AgentID(session.ID)
			agents := ac.ListAgents(&parentID)
			if len(agents) == 0 {
				return "No active subagents.", nil
			}
			var sb strings.Builder
			for _, a := range agents {
				sb.WriteString(fmt.Sprintf("- %s: status=%s, created=%s\n", a.ID, a.Status, a.CreatedAt.Format("15:04:05")))
			}
			return sb.String(), nil
		},
		IsReadOnly: true,
	})
}
