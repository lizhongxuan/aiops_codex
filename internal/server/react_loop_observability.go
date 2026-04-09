package server

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// loopLogFields builds a structured log prefix for ReAct loop events.
func loopLogFields(runID, sessionID string, iteration int) string {
	return fmt.Sprintf("runId=%s sessionId=%s iteration=%d", runID, sessionID, iteration)
}

// logLoopEvent logs a ReAct loop lifecycle event with structured fields.
func logLoopEvent(event, runID, sessionID string, iteration int, extra ...string) {
	fields := loopLogFields(runID, sessionID, iteration)
	if len(extra) > 0 {
		fields += " " + strings.Join(extra, " ")
	}
	log.Printf("[react_loop] %s %s", event, fields)
}

// logToolInvocation logs a tool invocation event.
func logToolInvocation(event, runID, sessionID, toolName, invocationID string) {
	log.Printf("[react_loop] tool_%s runId=%s sessionId=%s tool=%s invocationId=%s",
		event, runID, sessionID, toolName, invocationID)
}

// logErrorRecovery logs an error recovery attempt.
func logErrorRecovery(runID, sessionID string, iteration, recoveryCount int, errorType, action string) {
	log.Printf("[react_loop] error_recovery runId=%s sessionId=%s iteration=%d recovery=%d errorType=%s action=%s",
		runID, sessionID, iteration, recoveryCount, errorType, action)
}

// loopMetrics tracks aggregate metrics for a single loop run.
type loopMetrics struct {
	RunID                string
	SessionID            string
	StartedAt            time.Time
	CompletedAt          time.Time
	IterationCount       int
	ToolCount            int
	WaitingUserCount     int
	WaitingApprovalCount int
	CompactAttempts      int
	CompactFailures      int
	FallbackModelCount   int
	MaxTokenRecoveries   int
	AuthorizationBlocks  int
	FinalStatus          string
}

// newLoopMetrics creates a new metrics tracker for a loop run.
func newLoopMetrics(runID, sessionID string) *loopMetrics {
	return &loopMetrics{
		RunID:     runID,
		SessionID: sessionID,
		StartedAt: time.Now(),
	}
}

// logSummary logs the final metrics summary for the loop run.
func (m *loopMetrics) logSummary() {
	if m.CompletedAt.IsZero() {
		m.CompletedAt = time.Now()
	}
	duration := m.CompletedAt.Sub(m.StartedAt)
	log.Printf("[react_loop] run_summary runId=%s sessionId=%s status=%s duration=%s iterations=%d tools=%d waiting_user=%d waiting_approval=%d compact_attempts=%d compact_failures=%d fallback_model=%d max_token_recovery=%d auth_blocks=%d",
		m.RunID, m.SessionID, m.FinalStatus, duration,
		m.IterationCount, m.ToolCount, m.WaitingUserCount, m.WaitingApprovalCount,
		m.CompactAttempts, m.CompactFailures, m.FallbackModelCount, m.MaxTokenRecoveries,
		m.AuthorizationBlocks)
}

// classifyLoopError categorizes an error for logging and metrics.
func classifyLoopError(err error) string {
	if err == nil {
		return "none"
	}
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "context canceled"):
		return "user_cancel"
	case strings.Contains(errStr, "context deadline exceeded"):
		return "timeout"
	case strings.Contains(errStr, "thread not found"):
		return "stale_thread"
	case strings.Contains(errStr, "max_tokens"):
		return "max_tokens"
	case strings.Contains(errStr, "prompt_too_long"):
		return "prompt_too_long"
	case strings.Contains(errStr, "overloaded"):
		return "model_overload"
	case strings.Contains(errStr, "authorization"):
		return "authorization"
	case strings.Contains(errStr, "app-server"):
		return "app_server_disconnect"
	default:
		return "unknown"
	}
}
