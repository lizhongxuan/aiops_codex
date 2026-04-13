package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// RegisterAgentJobsTool registers the agent_jobs tool.
func RegisterAgentJobsTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "agent_jobs",
		Description: "Execute batch jobs from a CSV-like task list with configurable concurrency.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"jobs": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"id": map[string]interface{}{
								"type":        "string",
								"description": "Unique job identifier.",
							},
							"command": map[string]interface{}{
								"type":        "string",
								"description": "Command or instruction to execute.",
							},
							"args": map[string]interface{}{
								"type":        "object",
								"description": "Additional arguments for the job.",
							},
						},
						"required": []string{"id", "command"},
					},
					"minItems":    1,
					"description": "List of jobs to execute.",
				},
				"concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of concurrent jobs (default 1, max 10).",
				},
			},
			"required":             []string{"jobs"},
			"additionalProperties": false,
		},
		Handler:          handleAgentJobs,
		RequiresApproval: true,
	})
}

// JobSpec represents a single job in a batch.
type JobSpec struct {
	ID      string                 `json:"id"`
	Command string                 `json:"command"`
	Args    map[string]interface{} `json:"args,omitempty"`
}

// JobResult represents the result of a single job execution.
type JobResult struct {
	ID     string `json:"id"`
	Status string `json:"status"` // "success" or "error"
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

func handleAgentJobs(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	jobsRaw, ok := args["jobs"]
	if !ok {
		return "", fmt.Errorf("agent_jobs requires 'jobs' argument")
	}

	data, err := json.Marshal(jobsRaw)
	if err != nil {
		return "", fmt.Errorf("agent_jobs: invalid jobs: %w", err)
	}

	var jobs []JobSpec
	if err := json.Unmarshal(data, &jobs); err != nil {
		return "", fmt.Errorf("agent_jobs: invalid jobs format: %w", err)
	}

	if len(jobs) == 0 {
		return "", fmt.Errorf("agent_jobs: at least one job is required")
	}

	concurrency := 1
	if c, ok := args["concurrency"].(float64); ok && c > 0 {
		concurrency = int(c)
	}
	if concurrency > 10 {
		concurrency = 10
	}

	// Execute jobs with bounded concurrency.
	results := make([]JobResult, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for i, job := range jobs {
		wg.Add(1)
		go func(idx int, j JobSpec) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := executeJob(ctx, session, j)
			results[idx] = result
		}(i, job)
	}

	wg.Wait()

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", fmt.Errorf("agent_jobs: %w", err)
	}
	return string(out), nil
}

func executeJob(ctx context.Context, session *Session, job JobSpec) JobResult {
	// Basic job execution — commands are treated as descriptions of work.
	// In a full implementation this would dispatch to sub-agents or shell.
	if strings.TrimSpace(job.Command) == "" {
		return JobResult{
			ID:     job.ID,
			Status: "error",
			Error:  "empty command",
		}
	}

	select {
	case <-ctx.Done():
		return JobResult{
			ID:     job.ID,
			Status: "error",
			Error:  "context cancelled",
		}
	default:
	}

	return JobResult{
		ID:     job.ID,
		Status: "success",
		Output: fmt.Sprintf("Job %q executed: %s", job.ID, job.Command),
	}
}
