package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type remoteExecSession struct {
	ID        string
	SessionID string
	HostID    string
	CardID    string
	ToolName  string
	Command   string
	Cwd       string
	Shell     string
	StartedAt string
	Approval  string

	done     chan remoteExecResult
	doneOnce sync.Once

	mu     sync.Mutex
	output string
}

type remoteExecResult struct {
	Output   string
	ExitCode int
	Status   string
	Message  string
}

type execSpec struct {
	Command    string
	Cwd        string
	Shell      string
	TimeoutSec int
	Readonly   bool
	Approval   string
}

func (e *remoteExecSession) appendOutput(chunk string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.output += chunk
	return e.output
}

func (e *remoteExecSession) snapshotOutput() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.output
}

func (e *remoteExecSession) finish(result remoteExecResult) bool {
	finished := false
	e.doneOnce.Do(func() {
		finished = true
		e.done <- result
	})
	return finished
}

func (a *App) setExecSession(exec *remoteExecSession) {
	a.execMu.Lock()
	defer a.execMu.Unlock()
	a.execs[exec.ID] = exec
}

func (a *App) execSession(execID string) (*remoteExecSession, bool) {
	a.execMu.Lock()
	defer a.execMu.Unlock()
	exec, ok := a.execs[execID]
	return exec, ok
}

func (a *App) clearExecSession(execID string) {
	a.execMu.Lock()
	defer a.execMu.Unlock()
	delete(a.execs, execID)
}

func (a *App) failRemoteExecsForHost(hostID, message string) {
	a.execMu.Lock()
	sessions := make([]*remoteExecSession, 0, len(a.execs))
	for _, exec := range a.execs {
		if exec.HostID == hostID {
			sessions = append(sessions, exec)
		}
	}
	a.execMu.Unlock()

	for _, exec := range sessions {
		output := exec.snapshotOutput()
		if message != "" {
			if output != "" && !strings.HasSuffix(output, "\n") {
				output += "\n"
			}
			output += message
		}
		exec.finish(remoteExecResult{
			Output:   output,
			ExitCode: 255,
			Status:   "failed",
			Message:  message,
		})
	}
}

func (a *App) cancelRemoteExecsForSession(sessionID, message string) {
	a.execMu.Lock()
	sessions := make([]*remoteExecSession, 0, len(a.execs))
	for _, exec := range a.execs {
		if exec.SessionID == sessionID {
			sessions = append(sessions, exec)
		}
	}
	a.execMu.Unlock()

	for _, exec := range sessions {
		_ = a.sendAgentEnvelope(exec.HostID, &agentrpc.Envelope{
			Kind: "exec/cancel",
			ExecCancel: &agentrpc.ExecCancel{
				ExecID: exec.ID,
			},
		})
		output := exec.snapshotOutput()
		if message != "" {
			if output != "" && !strings.HasSuffix(output, "\n") {
				output += "\n"
			}
			output += message
		}
		exec.finish(remoteExecResult{
			Output:   output,
			ExitCode: 130,
			Status:   "cancelled",
			Message:  message,
		})
	}
}

func (a *App) handleAgentExecOutput(hostID string, payload *agentrpc.ExecOutput) {
	if payload == nil || strings.TrimSpace(payload.ExecID) == "" {
		return
	}
	exec, ok := a.execSession(payload.ExecID)
	if !ok || exec.HostID != hostID {
		return
	}

	chunk := sanitizeTerminalChunk(payload.Data)
	if chunk == "" {
		return
	}
	exec.appendOutput(chunk)
	a.store.UpdateCard(exec.SessionID, exec.CardID, func(card *model.Card) {
		card.Output += chunk
		card.UpdatedAt = model.NowString()
	})
	a.broadcastSnapshot(exec.SessionID)
}

func (a *App) handleAgentExecExit(hostID string, payload *agentrpc.ExecExit) {
	if payload == nil || strings.TrimSpace(payload.ExecID) == "" {
		return
	}
	exec, ok := a.execSession(payload.ExecID)
	if !ok || exec.HostID != hostID {
		return
	}

	output := exec.snapshotOutput()
	if payload.Message != "" {
		if output != "" && !strings.HasSuffix(output, "\n") {
			output += "\n"
		}
		output += payload.Message
	}

	status := strings.TrimSpace(payload.Status)
	if status == "" {
		if payload.Code == 0 {
			status = "completed"
		} else {
			status = "failed"
		}
	}

	exec.finish(remoteExecResult{
		Output:   output,
		ExitCode: payload.Code,
		Status:   status,
		Message:  payload.Message,
	})
}

func (a *App) runRemoteExec(ctx context.Context, sessionID, hostID, cardID string, spec execSpec) (remoteExecResult, error) {
	if hostID == model.ServerLocalHostID {
		return remoteExecResult{}, errors.New("server-local should use built-in Codex tools instead of remote execute_* tools")
	}

	host := a.findHost(hostID)
	if host.Status != "online" || !host.Executable {
		return remoteExecResult{}, errors.New("selected remote host is offline or not executable")
	}

	now := model.NowString()
	createdAt := now
	if existing := a.cardByID(sessionID, cardID); existing != nil && existing.CreatedAt != "" {
		createdAt = existing.CreatedAt
	}

	a.setRuntimeTurnPhase(sessionID, "executing")
	a.incrementCommandCount(sessionID)
	a.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "CommandCard",
		Title:     "Command execution",
		Command:   spec.Command,
		Cwd:       spec.Cwd,
		Status:    "inProgress",
		CreatedAt: createdAt,
		UpdatedAt: now,
	})
	a.broadcastSnapshot(sessionID)
	a.auditRemoteToolEvent("remote.exec.started", sessionID, hostID, func() string {
		if spec.Readonly {
			return "execute_readonly_query"
		}
		return "execute_system_mutation"
	}(), map[string]any{
		"command":          spec.Command,
		"cwd":              spec.Cwd,
		"startedAt":        now,
		"approvalDecision": spec.Approval,
	})

	exec := &remoteExecSession{
		ID:        model.NewID("exec"),
		SessionID: sessionID,
		HostID:    hostID,
		CardID:    cardID,
		ToolName: func() string {
			if spec.Readonly {
				return "execute_readonly_query"
			}
			return "execute_system_mutation"
		}(),
		Command:   spec.Command,
		Cwd:       spec.Cwd,
		Shell:     spec.Shell,
		StartedAt: now,
		Approval:  spec.Approval,
		done:      make(chan remoteExecResult, 1),
	}
	a.setExecSession(exec)

	if err := a.sendAgentEnvelope(hostID, &agentrpc.Envelope{
		Kind: "exec/start",
		ExecStart: &agentrpc.ExecStart{
			ExecID:     exec.ID,
			Command:    spec.Command,
			Cwd:        spec.Cwd,
			Shell:      spec.Shell,
			TimeoutSec: clampExecTimeout(spec.TimeoutSec, spec.Readonly),
			Readonly:   spec.Readonly,
		},
	}); err != nil {
		a.clearExecSession(exec.ID)
		a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
			card.Status = "failed"
			card.Output = err.Error()
			card.DurationMS = durationBetween(createdAt, model.NowString())
			card.UpdatedAt = model.NowString()
		})
		a.resumeThinkingAfterExecution(sessionID)
		a.broadcastSnapshot(sessionID)
		return remoteExecResult{}, err
	}

	select {
	case <-ctx.Done():
		_ = a.sendAgentEnvelope(hostID, &agentrpc.Envelope{
			Kind: "exec/cancel",
			ExecCancel: &agentrpc.ExecCancel{
				ExecID: exec.ID,
			},
		})
		status := "cancelled"
		message := "command cancelled"
		exitCode := 130
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = "timeout"
			message = "command timed out"
			exitCode = 124
		}
		exec.finish(remoteExecResult{
			Output:   exec.snapshotOutput(),
			ExitCode: exitCode,
			Status:   status,
			Message:  message,
		})
		result := <-exec.done
		a.clearExecSession(exec.ID)
		a.finalizeExecCard(exec, createdAt, result)
		return result, ctx.Err()
	case result := <-exec.done:
		a.clearExecSession(exec.ID)
		a.finalizeExecCard(exec, createdAt, result)
		return result, nil
	}
}

func (a *App) finalizeExecCard(exec *remoteExecSession, createdAt string, result remoteExecResult) {
	now := model.NowString()
	finalStatus := execResultCardStatus(result)
	a.store.UpdateCard(exec.SessionID, exec.CardID, func(card *model.Card) {
		card.Output = result.Output
		card.Status = finalStatus
		card.DurationMS = durationBetween(createdAt, now)
		card.UpdatedAt = now
	})
	a.resumeThinkingAfterExecution(exec.SessionID)
	a.broadcastSnapshot(exec.SessionID)
	a.auditRemoteToolEvent("remote.exec.finished", exec.SessionID, exec.HostID, exec.ToolName, map[string]any{
		"command":          exec.Command,
		"cwd":              exec.Cwd,
		"startedAt":        exec.StartedAt,
		"endedAt":          now,
		"status":           finalStatus,
		"exitCode":         result.ExitCode,
		"approvalDecision": exec.Approval,
	})
}

func execResultCardStatus(result remoteExecResult) string {
	errorText := strings.ToLower(strings.TrimSpace(result.Message + "\n" + result.Output))
	switch result.Status {
	case "cancelled":
		return "cancelled"
	case "timeout":
		return "timeout"
	case "completed":
		if result.ExitCode == 0 && !commandOutputLooksFailed(result.Output) {
			return "completed"
		}
	default:
	}
	switch {
	case strings.Contains(errorText, "heartbeat timed out"):
		return "host_timeout"
	case strings.Contains(errorText, "remote host disconnected"):
		return "disconnected"
	case strings.Contains(errorText, "permission denied") || strings.Contains(errorText, "operation not permitted"):
		return "permission_denied"
	}
	return "failed"
}

func clampExecTimeout(timeoutSec int, readonly bool) int {
	if readonly {
		if timeoutSec <= 0 {
			return 60
		}
		if timeoutSec > 120 {
			return 120
		}
		return timeoutSec
	}
	if timeoutSec <= 0 {
		return 180
	}
	if timeoutSec > 600 {
		return 600
	}
	return timeoutSec
}

func toolResponse(text string, success bool) map[string]any {
	return map[string]any{
		"contentItems": []map[string]any{
			{
				"type": "inputText",
				"text": text,
			},
		},
		"success": success,
	}
}

func formatExecToolResult(command string, result remoteExecResult) string {
	var builder strings.Builder
	statusLabel := "completed"
	if result.Status != "" {
		statusLabel = result.Status
	}
	builder.WriteString(fmt.Sprintf("Host command `%s` %s with exit code %d.", truncate(command, 180), statusLabel, result.ExitCode))
	if strings.TrimSpace(result.Output) != "" {
		builder.WriteString("\n\nOutput:\n```text\n")
		builder.WriteString(truncateToolOutput(result.Output, 16000))
		if !strings.HasSuffix(result.Output, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("```")
	}
	return builder.String()
}

func truncateToolOutput(output string, max int) string {
	if max <= 0 || len(output) <= max {
		return output
	}
	return output[:max] + "\n...[truncated]..."
}
