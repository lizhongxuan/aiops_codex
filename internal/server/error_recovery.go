package server

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// errorRecoveryConfig holds configuration for error recovery.
type errorRecoveryConfig struct {
	MaxCompactRetries     int
	MaxTokenRecoveries    int
	MaxOverloadRetries    int
	ToolTimeoutDuration   time.Duration
	DisconnectGracePeriod time.Duration
}

// defaultErrorRecoveryConfig returns the default error recovery configuration.
func defaultErrorRecoveryConfig() errorRecoveryConfig {
	return errorRecoveryConfig{
		MaxCompactRetries:     3,
		MaxTokenRecoveries:    2,
		MaxOverloadRetries:    1,
		ToolTimeoutDuration:   30 * time.Second,
		DisconnectGracePeriod: 5 * time.Second,
	}
}

// errorRecoveryAction represents the action to take after error recovery.
type errorRecoveryAction struct {
	Action        string // "retry", "compact_retry", "inject_message", "fallback_model", "terminate", "circuit_break"
	Message       string // recovery message to inject
	FallbackModel string // model to switch to
	Evidence      string // evidence description
}

// recoverFromError attempts to recover from a loop error.
// Returns the recovery action to take.
func recoverFromError(err error, state *reActLoopState, cfg errorRecoveryConfig) errorRecoveryAction {
	if err == nil {
		return errorRecoveryAction{Action: "none"}
	}

	errStr := err.Error()

	// Context canceled — user stopped the loop
	if strings.Contains(errStr, "context canceled") || strings.Contains(errStr, "context deadline exceeded") {
		return errorRecoveryAction{
			Action:   "terminate",
			Evidence: fmt.Sprintf("Loop canceled: %s", errStr),
		}
	}

	// Prompt too long — try compaction
	if strings.Contains(errStr, "prompt_too_long") || strings.Contains(errStr, "context_length") {
		if state.RecoveryCount >= cfg.MaxCompactRetries {
			return errorRecoveryAction{
				Action:   "circuit_break",
				Evidence: fmt.Sprintf("Compaction failed after %d attempts", state.RecoveryCount),
			}
		}
		return errorRecoveryAction{
			Action:   "compact_retry",
			Evidence: "Context too long, attempting compaction",
		}
	}

	// Max output tokens — inject recovery message
	if strings.Contains(errStr, "max_tokens") || strings.Contains(errStr, "output_limit") {
		if state.RecoveryCount >= cfg.MaxTokenRecoveries {
			return errorRecoveryAction{
				Action:   "terminate",
				Evidence: fmt.Sprintf("Max token recovery exhausted after %d attempts", state.RecoveryCount),
			}
		}
		return errorRecoveryAction{
			Action:   "inject_message",
			Message:  recoveryMessageForMaxTokens(),
			Evidence: "Output truncated, injecting recovery message",
		}
	}

	// Model overload — try fallback model
	if strings.Contains(errStr, "overloaded") || strings.Contains(errStr, "rate_limit") {
		if state.RecoveryCount >= cfg.MaxOverloadRetries {
			return errorRecoveryAction{
				Action:   "terminate",
				Evidence: "Model overload recovery exhausted",
			}
		}
		return errorRecoveryAction{
			Action:        "fallback_model",
			FallbackModel: "claude-3-5-sonnet-20241022",
			Evidence:      "Model overloaded, switching to fallback",
		}
	}

	// App-server disconnect
	if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "EOF") || strings.Contains(errStr, "broken pipe") {
		return errorRecoveryAction{
			Action:   "terminate",
			Evidence: fmt.Sprintf("App-server disconnected: %s", truncate(errStr, 100)),
		}
	}

	// Tool timeout
	if strings.Contains(errStr, "tool timeout") || strings.Contains(errStr, "execution timeout") {
		return errorRecoveryAction{
			Action:   "inject_message",
			Message:  fmt.Sprintf("Tool execution timed out: %s. Consider breaking the task into smaller steps or using a different approach.", truncate(errStr, 100)),
			Evidence: fmt.Sprintf("Tool timeout: %s", truncate(errStr, 100)),
		}
	}

	// Unknown error — terminate
	return errorRecoveryAction{
		Action:   "terminate",
		Evidence: fmt.Sprintf("Unrecoverable error: %s", truncate(errStr, 200)),
	}
}

// applyRecoveryAction applies the recovery action to the loop state.
func applyRecoveryAction(action errorRecoveryAction, state *reActLoopState, app *App) {
	log.Printf("[error_recovery] action=%s session=%s iteration=%d recovery=%d evidence=%s",
		action.Action, state.Request.SessionID, state.Iteration, state.RecoveryCount, truncate(action.Evidence, 100))

	switch action.Action {
	case "compact_retry":
		state.RecoveryCount++
		state.NeedsFollowUp = true
		state.LastError = nil
		// Compaction will be handled by the context preprocess stage on next iteration

	case "inject_message":
		state.RecoveryCount++
		state.NeedsFollowUp = true
		state.LastError = nil
		state.Messages = append(state.Messages, map[string]any{
			"role":    "system",
			"content": action.Message,
		})

	case "fallback_model":
		state.RecoveryCount++
		state.NeedsFollowUp = true
		state.LastError = nil
		// The fallback model will be picked up by the model stream call stage

	case "circuit_break":
		state.NeedsFollowUp = false
		if app != nil {
			createRecoveryEvidenceCard(app, state.Request.SessionID, "circuit_break", action.Evidence)
		}

	case "terminate":
		state.NeedsFollowUp = false
		if app != nil {
			createRecoveryEvidenceCard(app, state.Request.SessionID, "terminated", action.Evidence)
		}
	}
}

// createRecoveryEvidenceCard creates an error card with evidence for recovery events.
func createRecoveryEvidenceCard(app *App, sessionID, status, evidence string) {
	now := model.NowString()
	evidenceID := model.NewID("ev")

	app.store.RememberItem(sessionID, evidenceID, map[string]any{
		"kind":     "error_recovery",
		"status":   status,
		"evidence": evidence,
	})

	app.store.UpsertCard(sessionID, model.Card{
		ID:      model.NewID("recovery"),
		Type:    "ErrorCard",
		Title:   "错误恢复",
		Message: evidence,
		Status:  status,
		Detail: map[string]any{
			"evidenceId": evidenceID,
			"recovery":   status,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})

	app.broadcastSnapshot(sessionID)
}
