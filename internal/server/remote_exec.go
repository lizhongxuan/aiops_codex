package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

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

	mu                sync.Mutex
	output            string
	stdout            string
	stderr            string
	cancelRequested   bool
	cancelMessage     string
	cancelRequestedAt time.Time
}

type remoteExecResult struct {
	Output    string
	Stdout    string
	Stderr    string
	ExitCode  int
	Status    string
	Message   string
	Error     string
	Timeout   bool
	Cancelled bool
}

type execSpec struct {
	Command    string
	Cwd        string
	Shell      string
	TimeoutSec int
	Readonly   bool
	Approval   string
	ToolName   string
}

func defaultRemoteExecCwd(host model.Host) string {
	if strings.EqualFold(strings.TrimSpace(host.OS), "windows") {
		return `C:\Windows\Temp`
	}
	return "/tmp"
}

func defaultRemoteExecShell(host model.Host) string {
	if strings.EqualFold(strings.TrimSpace(host.OS), "windows") {
		return ""
	}
	return "/bin/sh"
}

func normalizeRemoteExecSpec(host model.Host, spec execSpec) execSpec {
	spec.Cwd = strings.TrimSpace(spec.Cwd)
	spec.Shell = strings.TrimSpace(spec.Shell)
	if spec.Cwd == "" {
		spec.Cwd = defaultRemoteExecCwd(host)
	}
	if spec.Shell == "" {
		spec.Shell = defaultRemoteExecShell(host)
	}
	return spec
}

func (e *remoteExecSession) appendOutput(stream, chunk string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.output += chunk
	if stream == "stderr" {
		e.stderr += chunk
	} else {
		e.stdout += chunk
	}
	return e.output
}

func (e *remoteExecSession) snapshotResult() remoteExecResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	return remoteExecResult{
		Output:    e.output,
		Stdout:    e.stdout,
		Stderr:    e.stderr,
		Cancelled: e.cancelRequested,
		Message:   e.cancelMessage,
	}
}

func (e *remoteExecSession) requestCancel(message string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancelRequested {
		return false
	}
	e.cancelRequested = true
	e.cancelMessage = strings.TrimSpace(message)
	e.cancelRequestedAt = time.Now()
	return true
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
		result := exec.snapshotResult()
		result.Output = appendExecMessage(result.Output, message)
		result.ExitCode = 255
		result.Status = "failed"
		result.Message = strings.TrimSpace(message)
		result.Error = strings.TrimSpace(message)
		exec.finish(result)
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
		if err := a.sendAgentEnvelope(exec.HostID, &agentrpc.Envelope{
			Kind: "exec/cancel",
			ExecCancel: &agentrpc.ExecCancel{
				ExecID: exec.ID,
			},
		}); err != nil {
			a.appendIncidentEvent(exec.SessionID, "cancel.signal_failed", "warning", "取消信号发送失败", fmt.Sprintf("未能向 %s 发送取消信号，步骤 %s 可能仍在执行", exec.HostID, exec.CardID), map[string]any{
				"execId": exec.ID,
				"cardId": exec.CardID,
				"hostId": exec.HostID,
				"error":  err.Error(),
			})
		}
		if !exec.requestCancel(message) {
			continue
		}
		now := model.NowString()
		a.store.UpdateCard(exec.SessionID, exec.CardID, func(card *model.Card) {
			card.Status = "cancelled"
			card.ExitCode = 130
			card.Cancelled = true
			card.UpdatedAt = now
		})
		go a.forceCancelRemoteExec(exec.ID, 3*time.Second)
	}
	a.broadcastSnapshot(sessionID)
}

func (a *App) forceCancelRemoteExec(execID string, delay time.Duration) {
	if delay <= 0 {
		delay = 3 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C

	exec, ok := a.execSession(execID)
	if !ok {
		return
	}
	result := exec.snapshotResult()
	if !result.Cancelled {
		return
	}
	result.Output = appendExecMessage(result.Output, result.Message)
	result.ExitCode = 130
	result.Status = "cancelled"
	result.Cancelled = true
	a.appendIncidentEvent(exec.SessionID, "cancel.partial_failure", "warning", "取消未获远端确认", fmt.Sprintf("步骤 %s 未返回取消确认，已在本地强制标记为 cancelled", exec.CardID), map[string]any{
		"execId":  exec.ID,
		"cardId":  exec.CardID,
		"hostId":  exec.HostID,
		"message": result.Message,
	})
	exec.finish(result)
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
	stream := strings.ToLower(strings.TrimSpace(payload.Stream))
	if stream != "stderr" {
		stream = "stdout"
	}
	exec.appendOutput(stream, chunk)
	a.store.UpdateCard(exec.SessionID, exec.CardID, func(card *model.Card) {
		card.Output += chunk
		if stream == "stderr" {
			card.Stderr += chunk
		} else {
			card.Stdout += chunk
		}
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

	result := exec.snapshotResult()
	if payload.Stdout != "" && len(payload.Stdout) >= len(result.Stdout) {
		result.Stdout = payload.Stdout
	}
	if payload.Stderr != "" && len(payload.Stderr) >= len(result.Stderr) {
		result.Stderr = payload.Stderr
	}
	if len(result.Output) < len(result.Stdout)+len(result.Stderr) {
		result.Output = result.Stdout + result.Stderr
	}

	status := strings.TrimSpace(payload.Status)
	if status == "" {
		if payload.Cancelled {
			status = "cancelled"
		} else if payload.Timeout {
			status = "timeout"
		} else if execCancelRequested(exec) {
			status = "cancelled"
		} else if execExitCode(payload) == 0 {
			status = "completed"
		} else {
			status = "failed"
		}
	}
	result.Output = appendExecMessage(result.Output, defaultString(strings.TrimSpace(payload.Message), strings.TrimSpace(payload.Error)))
	result.ExitCode = execExitCode(payload)
	result.Status = status
	result.Message = strings.TrimSpace(payload.Message)
	result.Error = strings.TrimSpace(payload.Error)
	result.Timeout = payload.Timeout || status == "timeout"
	result.Cancelled = payload.Cancelled || status == "cancelled"
	exec.finish(result)
}

func (a *App) runRemoteExec(ctx context.Context, sessionID, hostID, cardID string, spec execSpec) (remoteExecResult, error) {
	if hostID == model.ServerLocalHostID {
		return remoteExecResult{}, errors.New("server-local should use built-in Codex tools instead of remote execute_* tools")
	}

	host := a.findHost(hostID)
	if host.Status != "online" || !host.Executable {
		return remoteExecResult{}, errors.New("selected remote host is offline or not executable")
	}
	spec = normalizeRemoteExecSpec(host, spec)

	now := model.NowString()
	createdAt := now
	cardDetail := map[string]any{}
	if existing := a.cardByID(sessionID, cardID); existing != nil {
		if existing.CreatedAt != "" {
			createdAt = existing.CreatedAt
		}
		if len(existing.Detail) > 0 {
			cardDetail = cloneAnyMap(existing.Detail)
		}
	}

	a.setRuntimeTurnPhase(sessionID, "executing")
	a.incrementCommandCount(sessionID)
	cardDetail["readonly"] = spec.Readonly
	cardDetail["cancelable"] = true
	if toolName := strings.TrimSpace(spec.ToolName); toolName != "" {
		cardDetail["tool"] = toolName
	}
	card := model.Card{
		ID:        cardID,
		Type:      "CommandCard",
		Title:     "Command execution",
		Command:   spec.Command,
		Cwd:       spec.Cwd,
		Status:    "inProgress",
		Detail:    cardDetail,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	applyCardHost(&card, host)
	a.store.UpsertCard(sessionID, card)
	a.broadcastSnapshot(sessionID)
	a.recordOrchestratorRemoteExecStarted(sessionID, hostID, cardID, spec.Command)
	a.auditRemoteToolEvent("remote.exec.started", sessionID, hostID, func() string {
		return execSpecToolName(spec)
	}(), map[string]any{
		"command":          spec.Command,
		"cwd":              spec.Cwd,
		"startedAt":        now,
		"endedAt":          nil,
		"status":           "inProgress",
		"exitCode":         nil,
		"approvalDecision": spec.Approval,
	})

	exec := &remoteExecSession{
		ID:        model.NewID("exec"),
		SessionID: sessionID,
		HostID:    hostID,
		CardID:    cardID,
		ToolName:  execSpecToolName(spec),
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
			Cancelable: true,
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
		status := "cancelled"
		message := "command cancelled"
		exitCode := 130
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = "timeout"
			message = "command timed out"
			exitCode = 124
		}
		_ = a.sendAgentEnvelope(hostID, &agentrpc.Envelope{
			Kind: "exec/cancel",
			ExecCancel: &agentrpc.ExecCancel{
				ExecID: exec.ID,
			},
		})
		exec.requestCancel(message)
		go a.forceCancelRemoteExec(exec.ID, 2*time.Second)
		a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
			card.Status = status
			card.ExitCode = exitCode
			card.Cancelled = status == "cancelled"
			card.Timeout = status == "timeout"
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(sessionID)
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

func (a *App) runRemoteExecWithoutCard(ctx context.Context, sessionID, hostID string, spec execSpec) (remoteExecResult, error) {
	if hostID == model.ServerLocalHostID {
		return remoteExecResult{}, errors.New("server-local should use built-in Codex tools instead of remote execute_* tools")
	}

	host := a.findHost(hostID)
	if host.Status != "online" || !host.Executable {
		return remoteExecResult{}, errors.New("selected remote host is offline or not executable")
	}
	spec = normalizeRemoteExecSpec(host, spec)

	exec := &remoteExecSession{
		ID:        model.NewID("exec"),
		SessionID: sessionID,
		HostID:    hostID,
		ToolName:  execSpecToolName(spec),
		Command:   spec.Command,
		Cwd:       spec.Cwd,
		Shell:     spec.Shell,
		StartedAt: model.NowString(),
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
			Cancelable: true,
		},
	}); err != nil {
		a.clearExecSession(exec.ID)
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
		exec.requestCancel("command cancelled")
		go a.forceCancelRemoteExec(exec.ID, 2*time.Second)
		result := <-exec.done
		a.clearExecSession(exec.ID)
		return result, ctx.Err()
	case result := <-exec.done:
		a.clearExecSession(exec.ID)
		return result, nil
	}
}

func (a *App) executeLocalReadonlyHostInspect(sessionID, rawID string, params dynamicToolCallParams, args execToolArgs) {
	cardID := dynamicToolCardID(params.CallID)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(clampExecTimeout(args.TimeoutSec, true)+5)*time.Second)
	defer cancel()

	result, err := a.runLocalReadonlyExec(ctx, sessionID, cardID, execSpec{
		Command:    args.Command,
		Cwd:        args.Cwd,
		TimeoutSec: args.TimeoutSec,
		Readonly:   true,
		ToolName:   "readonly_host_inspect",
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}
	if a.turnWasInterrupted(sessionID) {
		return
	}

	success := execResultCardStatus(result) == "completed"
	_ = a.respondCodex(context.Background(), rawID, toolResponse(formatExecToolResult(args.Command, result), success))
}

func (a *App) runLocalReadonlyExec(ctx context.Context, sessionID, cardID string, spec execSpec) (remoteExecResult, error) {
	cwd := strings.TrimSpace(spec.Cwd)
	if cwd == "" {
		cwd = strings.TrimSpace(a.cfg.DefaultWorkspace)
	}
	if cwd != "" {
		if info, err := os.Stat(cwd); err != nil {
			return remoteExecResult{}, fmt.Errorf("local readonly cwd %q is not accessible: %w", cwd, err)
		} else if !info.IsDir() {
			return remoteExecResult{}, fmt.Errorf("local readonly cwd %q is not a directory", cwd)
		}
	}

	now := model.NowString()
	createdAt := now
	if existing := a.cardByID(sessionID, cardID); existing != nil && existing.CreatedAt != "" {
		createdAt = existing.CreatedAt
	}
	host := a.findHost(model.ServerLocalHostID)
	a.setRuntimeTurnPhase(sessionID, "executing")
	a.incrementCommandCount(sessionID)
	card := model.Card{
		ID:      cardID,
		Type:    "CommandCard",
		Title:   "Readonly host inspection",
		Command: spec.Command,
		Cwd:     cwd,
		Status:  "inProgress",
		Detail: map[string]any{
			"tool":       execSpecToolName(spec),
			"readonly":   true,
			"cancelable": true,
		},
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	applyCardHost(&card, host)
	a.store.UpsertCard(sessionID, card)
	a.broadcastSnapshot(sessionID)
	a.audit("local.readonly_exec.started", map[string]any{
		"sessionId": sessionID,
		"hostId":    model.ServerLocalHostID,
		"toolName":  execSpecToolName(spec),
		"command":   spec.Command,
		"cwd":       cwd,
		"startedAt": now,
	})

	shell := strings.TrimSpace(spec.Shell)
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-lc", spec.Command)
	if cwd != "" {
		cmd.Dir = cwd
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	result := remoteExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Output: stdout.String() + stderr.String(),
		Status: "completed",
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if runErr != nil {
		result.Status = "failed"
		result.Error = strings.TrimSpace(runErr.Error())
		if result.ExitCode == 0 {
			result.ExitCode = 1
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.Status = "timeout"
		result.Timeout = true
		result.Error = "command timed out"
		result.ExitCode = 124
		runErr = ctx.Err()
	} else if errors.Is(ctx.Err(), context.Canceled) {
		result.Status = "cancelled"
		result.Cancelled = true
		result.Error = "command cancelled"
		result.ExitCode = 130
		runErr = ctx.Err()
	}
	if result.ExitCode != 0 && result.Status == "completed" {
		result.Status = "failed"
	}

	execSession := &remoteExecSession{
		SessionID: sessionID,
		HostID:    model.ServerLocalHostID,
		CardID:    cardID,
		ToolName:  execSpecToolName(spec),
		Command:   spec.Command,
		Cwd:       cwd,
		Shell:     shell,
		StartedAt: now,
	}
	a.finalizeExecCard(execSession, createdAt, result)
	a.audit("local.readonly_exec.finished", map[string]any{
		"sessionId": sessionID,
		"hostId":    model.ServerLocalHostID,
		"toolName":  execSpecToolName(spec),
		"command":   spec.Command,
		"cwd":       cwd,
		"status":    execResultCardStatus(result),
		"exitCode":  result.ExitCode,
		"timeout":   result.Timeout,
		"cancelled": result.Cancelled,
		"error":     truncate(result.Error, 200),
		"endedAt":   model.NowString(),
	})
	return result, runErr
}

func execSpecToolName(spec execSpec) string {
	if toolName := strings.TrimSpace(spec.ToolName); toolName != "" {
		return toolName
	}
	if spec.Readonly {
		return "execute_readonly_query"
	}
	return "execute_system_mutation"
}

func readonlyHostInspectToolName(toolName string) string {
	if strings.TrimSpace(toolName) == "readonly_host_inspect" {
		return "readonly_host_inspect"
	}
	return ""
}

func (a *App) finalizeExecCard(exec *remoteExecSession, createdAt string, result remoteExecResult) {
	now := model.NowString()
	finalStatus := execResultCardStatus(result)
	summary, highlights, kvRows := buildExecCardPresentation(exec, result, finalStatus)
	display := toolDisplayPayloadToProjectionMap(commandResultDisplayPayload(exec.Command, exec.Cwd, result, finalStatus))
	a.store.UpdateCard(exec.SessionID, exec.CardID, func(card *model.Card) {
		card.Output = result.Output
		card.Stdout = result.Stdout
		card.Stderr = result.Stderr
		card.ExitCode = result.ExitCode
		card.Timeout = result.Timeout
		card.Cancelled = result.Cancelled
		card.Error = result.Error
		card.Summary = summary
		card.Highlights = highlights
		card.KVRows = kvRows
		card.Status = finalStatus
		card.Detail = toolProjectionDisplayDetailMap(display, card.Detail)
		card.DurationMS = durationBetween(createdAt, now)
		card.UpdatedAt = now
	})
	a.bindCardEvidence(exec.SessionID, exec.CardID, evidenceArtifactInput{
		Kind:       exec.ToolName,
		SourceKind: "command",
		SourceRef:  firstNonEmptyValue(exec.HostID, exec.Command),
		Title:      "Command execution",
		Summary:    summary,
		Content:    firstNonEmptyValue(result.Output, strings.TrimSpace(strings.Join([]string{result.Stdout, result.Stderr}, "\n")), result.Error, result.Message),
		Raw: map[string]any{
			"command":    exec.Command,
			"cwd":        exec.Cwd,
			"hostId":     exec.HostID,
			"status":     finalStatus,
			"exitCode":   result.ExitCode,
			"stdout":     result.Stdout,
			"stderr":     result.Stderr,
			"output":     result.Output,
			"error":      result.Error,
			"message":    result.Message,
			"timeout":    result.Timeout,
			"cancelled":  result.Cancelled,
			"durationMs": durationBetween(createdAt, now),
		},
	})
	if card := a.cardByID(exec.SessionID, exec.CardID); card != nil {
		a.syncActionVerification(exec.SessionID, *card)
	}
	a.resumeThinkingAfterExecution(exec.SessionID)
	a.broadcastSnapshot(exec.SessionID)
	a.auditRemoteToolEvent("remote.exec.finished", exec.SessionID, exec.HostID, exec.ToolName, map[string]any{
		"command":          exec.Command,
		"cwd":              exec.Cwd,
		"startedAt":        exec.StartedAt,
		"endedAt":          now,
		"status":           finalStatus,
		"exitCode":         result.ExitCode,
		"timeout":          result.Timeout,
		"cancelled":        result.Cancelled,
		"error":            truncate(result.Error, 200),
		"approvalDecision": exec.Approval,
	})
	a.recordOrchestratorRemoteExecFinished(exec.SessionID, exec.HostID, exec.CardID, finalStatus, exec.Command, firstNonEmptyString([]string{result.Error, result.Message, result.Output}))
}

func buildExecCardPresentation(exec *remoteExecSession, result remoteExecResult, finalStatus string) (string, []string, []model.KeyValueRow) {
	summary := execFailureSummary(finalStatus, result)
	if finalStatus == "completed" {
		summary = execSuccessSummary(exec, result)
	}
	highlights := execSummaryHighlights(finalStatus, summary, result)
	return summary, highlights, execResultKVRows(result)
}

func buildLocalCommandCardPresentation(item map[string]any, output string) (remoteExecResult, string, string, []string, []model.KeyValueRow) {
	result := localCommandCardResult(item, output)
	finalStatus := execResultCardStatus(result)
	summary, highlights, kvRows := buildExecCardPresentation(&remoteExecSession{ToolName: "commandExecution"}, result, finalStatus)
	return result, finalStatus, summary, highlights, kvRows
}

func localCommandCardResult(item map[string]any, output string) remoteExecResult {
	status := normalizeCardStatus(getString(item, "status"))
	if status == "inProgress" {
		status = "completed"
	}

	exitCode, _ := getIntAny(item, "exitCode", "exit_code")
	stdout := getStringAny(item, "stdout", "stdoutText", "stdout_text")
	stderr := getStringAny(item, "stderr", "stderrText", "stderr_text")
	errorText := strings.TrimSpace(getStringAny(item, "error", "errorMessage", "error_message"))
	messageText := strings.TrimSpace(getStringAny(item, "message", "statusMessage", "status_message"))

	result := remoteExecResult{
		Output:    output,
		Stdout:    stdout,
		Stderr:    stderr,
		ExitCode:  exitCode,
		Status:    status,
		Message:   messageText,
		Error:     errorText,
		Timeout:   getBool(item, "timeout"),
		Cancelled: getBool(item, "cancelled") || status == "cancelled",
	}
	if result.Timeout && result.Status == "completed" {
		result.Status = "timeout"
	}
	if result.Cancelled && result.Status == "completed" {
		result.Status = "cancelled"
	}

	if strings.TrimSpace(result.Stdout) == "" && strings.TrimSpace(result.Stderr) == "" {
		if result.ExitCode != 0 || commandOutputLooksFailed(output) || strings.TrimSpace(result.Error) != "" {
			result.Stderr = output
		} else {
			result.Stdout = output
		}
	}

	return result
}

func execSuccessSummary(exec *remoteExecSession, result remoteExecResult) string {
	if line := firstMeaningfulExecLine(result.Stdout, stripShellInitNoise(result.Output), stripShellInitNoise(result.Stderr)); line != "" {
		return truncate(line, 140)
	}
	if exec != nil {
		switch exec.ToolName {
		case "execute_system_mutation":
			return "远程变更命令已执行成功"
		case "readonly_host_inspect":
			return "只读主机检查已完成"
		case "execute_readonly_query":
			return "远程只读检查已完成"
		}
	}
	return "命令已执行完成"
}

func execFailureSummary(finalStatus string, result remoteExecResult) string {
	switch finalStatus {
	case "cancelled":
		return fmt.Sprintf("命令已取消（退出码 %d）", result.ExitCode)
	case "timeout":
		return fmt.Sprintf("命令已超时（退出码 %d）", result.ExitCode)
	case "permission_denied":
		return fmt.Sprintf("执行失败：权限不足（退出码 %d）", result.ExitCode)
	case "disconnected":
		return "执行失败：远程主机已断连"
	case "host_timeout":
		return "执行失败：远程主机心跳超时"
	}

	detail := firstMeaningfulExecLine(stripShellInitNoise(result.Stderr), result.Error, result.Message, stripShellInitNoise(result.Output))
	if detail == "" {
		if result.ExitCode != 0 {
			return fmt.Sprintf("执行失败（退出码 %d）", result.ExitCode)
		}
		return "执行失败"
	}
	if result.ExitCode != 0 {
		return fmt.Sprintf("执行失败（退出码 %d）：%s", result.ExitCode, truncate(detail, 120))
	}
	return "执行失败：" + truncate(detail, 120)
}

func execSummaryHighlights(finalStatus, summary string, result remoteExecResult) []string {
	source := result.Stdout
	if finalStatus != "completed" || strings.TrimSpace(source) == "" {
		source = stripShellInitNoise(result.Stderr)
	}
	if strings.TrimSpace(source) == "" {
		source = stripShellInitNoise(result.Output)
	}
	lines := meaningfulExecLines(source, 4)
	if len(lines) == 0 {
		return nil
	}
	if summary != "" && lines[0] == summary {
		lines = lines[1:]
	}
	if len(lines) > 3 {
		lines = lines[:3]
	}
	return lines
}

func execResultKVRows(result remoteExecResult) []model.KeyValueRow {
	rows := []model.KeyValueRow{{
		Key:   "退出码",
		Value: fmt.Sprintf("%d", result.ExitCode),
	}}
	if stdoutLines := countMeaningfulExecLines(result.Stdout); stdoutLines > 0 {
		rows = append(rows, model.KeyValueRow{Key: "标准输出", Value: fmt.Sprintf("%d 行", stdoutLines)})
	}
	if stderrLines := countMeaningfulExecLines(result.Stderr); stderrLines > 0 {
		rows = append(rows, model.KeyValueRow{Key: "标准错误", Value: fmt.Sprintf("%d 行", stderrLines)})
	}
	if result.Timeout {
		rows = append(rows, model.KeyValueRow{Key: "超时", Value: "是"})
	}
	if result.Cancelled {
		rows = append(rows, model.KeyValueRow{Key: "已取消", Value: "是"})
	}
	return rows
}

func firstMeaningfulExecLine(texts ...string) string {
	for _, text := range texts {
		lines := meaningfulExecLines(text, 1)
		if len(lines) > 0 {
			return lines[0]
		}
	}
	return ""
}

func countMeaningfulExecLines(text string) int {
	return len(meaningfulExecLines(text, 0))
}

func meaningfulExecLines(text string, limit int) []string {
	lines := make([]string, 0, 4)
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lines = append(lines, truncate(line, 140))
		if limit > 0 && len(lines) >= limit {
			return lines
		}
	}
	return lines
}

func shellInitNoiseOnly(text string) bool {
	lines := meaningfulExecLines(text, 0)
	if len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		if !shellInitNoiseLine(line) {
			return false
		}
	}
	return true
}

func shellInitNoiseLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}

	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "cargo/env") && !strings.Contains(lower, "no such file or directory") && !strings.Contains(lower, "command not found") && !strings.Contains(lower, "permission denied") {
		return false
	}

	markers := []string{
		".bashrc: line ",
		".bash_profile: line ",
		".bash_login: line ",
		".profile: line ",
		".zshrc: line ",
		".zprofile: line ",
		".zlogin: line ",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return strings.HasPrefix(lower, "bash:") || strings.HasPrefix(lower, "zsh:") || strings.HasPrefix(lower, "sh:")
}

func stripShellInitNoise(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, raw := range lines {
		if shellInitNoiseLine(raw) {
			continue
		}
		kept = append(kept, raw)
	}
	return strings.Join(kept, "\n")
}

func execResultCardStatus(result remoteExecResult) string {
	errorText := strings.ToLower(strings.TrimSpace(result.Error + "\n" + result.Message + "\n" + result.Stderr + "\n" + result.Output))
	switch {
	case result.Cancelled || result.Status == "cancelled":
		return "cancelled"
	case result.Timeout || result.Status == "timeout":
		return "timeout"
	case result.Status == "completed":
		if result.ExitCode == 0 && !commandOutputLooksFailed(result.Output) {
			return "completed"
		}
		if result.ExitCode == 0 && shellInitNoiseOnly(result.Stderr) && !commandOutputLooksFailed(stripShellInitNoise(result.Output)) {
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
	if result.Timeout {
		builder.WriteString("\nTimeout: true")
	}
	if result.Cancelled {
		builder.WriteString("\nCancelled: true")
	}
	if strings.TrimSpace(result.Error) != "" {
		builder.WriteString("\nError: ")
		builder.WriteString(truncate(result.Error, 400))
	}
	if strings.TrimSpace(result.Stdout) != "" {
		builder.WriteString("\n\nStdout:\n```text\n")
		builder.WriteString(truncateToolOutput(result.Stdout, 12000))
		if !strings.HasSuffix(result.Stdout, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("```")
	}
	if strings.TrimSpace(result.Stderr) != "" {
		builder.WriteString("\n\nStderr:\n```text\n")
		builder.WriteString(truncateToolOutput(result.Stderr, 8000))
		if !strings.HasSuffix(result.Stderr, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("```")
	}
	if strings.TrimSpace(result.Stdout) == "" && strings.TrimSpace(result.Stderr) == "" && strings.TrimSpace(result.Output) != "" {
		builder.WriteString("\n\nOutput:\n```text\n")
		builder.WriteString(truncateToolOutput(result.Output, 16000))
		if !strings.HasSuffix(result.Output, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("```")
	}
	return builder.String()
}

func execExitCode(payload *agentrpc.ExecExit) int {
	if payload == nil {
		return 0
	}
	if payload.ExitCode != 0 || payload.Code == 0 {
		return payload.ExitCode
	}
	return payload.Code
}

func execCancelRequested(exec *remoteExecSession) bool {
	if exec == nil {
		return false
	}
	exec.mu.Lock()
	defer exec.mu.Unlock()
	return exec.cancelRequested
}

func appendExecMessage(output, message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return output
	}
	if strings.TrimSpace(output) == "" {
		return message
	}
	if strings.Contains(output, message) {
		return output
	}
	if strings.HasSuffix(output, "\n") {
		return output + message
	}
	return output + "\n" + message
}

func truncateToolOutput(output string, max int) string {
	if max <= 0 || len(output) <= max {
		return output
	}
	return output[:max] + "\n...[truncated]..."
}
