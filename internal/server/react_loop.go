package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

const (
	reActLoopKindSingleHost = "single_host"
	reActLoopKindWorkspace  = "workspace"
	reActLoopVersion        = "react-loop-v1"
)

const (
	reActStageContextPreprocess = "context_preprocess"
	reActStageAttachmentInject  = "attachment_injection"
	reActStageModelStreamCall   = "model_stream_call"
	reActStageErrorRecovery     = "error_recovery"
	reActStageToolExecution     = "tool_execution"
	reActStagePostprocess       = "postprocess"
	reActStageLoopDecision      = "loop_decision"
)

const (
	reActModeAnswer   = "answer"
	reActModeReadonly  = "readonly"
	reActModePlan     = "plan"
	reActModeExecute  = "execute"
)

const (
	stopReasonEndTurn         = "end_turn"
	stopReasonToolUse         = "tool_use"
	stopReasonMaxTokens       = "max_tokens"
	stopReasonWaitingUser     = "waiting_user"
	stopReasonWaitingApproval = "waiting_approval"
	stopReasonFailed          = "failed"
	stopReasonCanceled        = "canceled"
)

type reActLoopStage interface {
	Name() string
	Run(context.Context, *reActLoopState) error
}

type reActLoopRequest struct {
	SessionID        string
	Kind             string
	HostID           string
	Message          string
	MonitorContext   *model.MonitorContext
	RequestID        string
	RequestStartedAt time.Time
}

type reActLoopState struct {
	Request            reActLoopRequest
	Messages           []map[string]any
	Attachments        []string
	AvailableTools     []string
	PermissionMode     string
	PlanMode           bool
	Mode               string
	RunID              string
	Iteration          int
	MaxIterations      int
	ThreadSpec         threadStartSpec
	TurnSpec           turnStartSpec
	ThreadID           string
	LastError          error
	NeedsFollowUp     bool
	RecoveryCount      int
	Checkpoints        []string
	StopReason         string
	AbortSignal        context.CancelFunc
	IterationStartedAt time.Time
}

type reActLoop struct {
	stages []reActLoopStage
}

func newDefaultReActLoop(app *App) reActLoop {
	return reActLoop{
		stages: []reActLoopStage{
			reActContextPreprocessStage{app: app},
			reActAttachmentInjectStage{app: app},
			reActModelStreamCallStage{app: app},
			reActErrorRecoveryStage{app: app},
			reActToolExecutionStage{app: app},
			reActPostprocessStage{app: app},
			reActLoopDecisionStage{app: app},
		},
	}
}

func defaultReActStageNames() []string {
	stages := newDefaultReActLoop(nil).stages
	names := make([]string, 0, len(stages))
	for _, stage := range stages {
		names = append(names, stage.Name())
	}
	return names
}

func (loop reActLoop) Run(ctx context.Context, state *reActLoopState) error {
	if state == nil {
		return errors.New("react loop state is nil")
	}
	if state.MaxIterations <= 0 {
		state.MaxIterations = 3
	}
	if state.Mode == "" {
		state.Mode = reActModeAnswer
	}
	for {
		// Check for cancellation at the start of each iteration.
		if ctx.Err() != nil {
			state.StopReason = stopReasonCanceled
			return ctx.Err()
		}
		if state.Iteration >= state.MaxIterations {
			if state.LastError != nil {
				return fmt.Errorf("react loop exceeded max iterations after recovery: %w", state.LastError)
			}
			return fmt.Errorf("react loop exceeded max iterations")
		}
		state.Iteration++
		state.NeedsFollowUp = false
		state.LastError = nil
		state.IterationStartedAt = time.Now()
		log.Printf("react loop iteration started session=%s iteration=%d mode=%s", state.Request.SessionID, state.Iteration, state.Mode)
		for _, stage := range loop.stages {
			if stage == nil {
				continue
			}
			state.Checkpoints = append(state.Checkpoints, stage.Name())
			if err := stage.Run(ctx, state); err != nil {
				return err
			}
		}
		iterationElapsed := time.Since(state.IterationStartedAt)
		log.Printf("react loop iteration completed session=%s iteration=%d elapsed=%s stop_reason=%s needs_followup=%v", state.Request.SessionID, state.Iteration, iterationElapsed, state.StopReason, state.NeedsFollowUp)
		if state.NeedsFollowUp {
			continue
		}
		return state.LastError
	}
}

type reActContextPreprocessStage struct {
	app *App
}

func (reActContextPreprocessStage) Name() string { return reActStageContextPreprocess }

func (stage reActContextPreprocessStage) Run(_ context.Context, state *reActLoopState) error {
	if strings.TrimSpace(state.Request.SessionID) == "" {
		return errors.New("react loop session id is required")
	}
	if strings.TrimSpace(state.Request.Message) == "" {
		return errors.New("react loop message is required")
	}
	if state.Request.HostID == "" {
		state.Request.HostID = model.ServerLocalHostID
	}
	if state.Request.Kind == "" {
		state.Request.Kind = reActLoopKindSingleHost
	}
	if state.RunID == "" {
		state.RunID = "loop-" + state.Request.SessionID
	}
	if state.PermissionMode == "" {
		state.PermissionMode = "normal"
	}
	if state.Mode == "" {
		if state.PlanMode {
			state.Mode = reActModePlan
		} else {
			state.Mode = reActModeAnswer
		}
	}
	if len(state.Messages) == 0 {
		state.Messages = []map[string]any{{
			"role":    "user",
			"content": state.Request.Message,
		}}
	}
	return nil
}

type reActAttachmentInjectStage struct {
	app *App
}

func (reActAttachmentInjectStage) Name() string { return reActStageAttachmentInject }

func (stage reActAttachmentInjectStage) Run(ctx context.Context, state *reActLoopState) error {
	if stage.app == nil {
		return errors.New("react loop attachment stage app is nil")
	}
	switch state.Request.Kind {
	case reActLoopKindWorkspace:
		state.ThreadSpec = stage.app.buildWorkspaceReActThreadStartSpec(ctx, state.Request.SessionID, state.Request.HostID)
		state.TurnSpec = stage.app.buildWorkspaceReActTurnStartSpec(ctx, state.Request.SessionID, state.Request.HostID, state.Request.Message)
		state.Attachments = append(state.Attachments, stage.app.buildReActLoopInstructions(reActLoopKindWorkspace, state.Request.SessionID, state.Request.HostID, true))
	default:
		req := chatRequest{
			Message:        state.Request.Message,
			HostID:         state.Request.HostID,
			MonitorContext: state.Request.MonitorContext,
		}
		state.ThreadSpec = stage.app.buildSingleHostReActThreadStartSpec(ctx, state.Request.SessionID)
		state.TurnSpec = stage.app.buildSingleHostReActTurnStartSpec(ctx, state.Request.SessionID, req)
		state.Attachments = append(state.Attachments, stage.app.buildReActLoopInstructions(reActLoopKindSingleHost, state.Request.SessionID, state.Request.HostID, true))
	}
	state.AvailableTools = dynamicToolNames(state.ThreadSpec.DynamicTools)
	return nil
}

type reActModelStreamCallStage struct {
	app *App
}

func (reActModelStreamCallStage) Name() string { return reActStageModelStreamCall }

func (stage reActModelStreamCallStage) Run(ctx context.Context, state *reActLoopState) error {
	if stage.app == nil {
		return errors.New("react loop model call stage app is nil")
	}
	session := stage.app.store.EnsureSession(state.Request.SessionID)
	if session.ThreadID != "" && strings.TrimSpace(session.ThreadConfigHash) != strings.TrimSpace(state.ThreadSpec.ThreadConfigHash) {
		stage.app.clearSessionThreadBinding(state.Request.SessionID)
		session.ThreadID = ""
	}
	threadID, err := stage.app.ensureThreadWithSpec(ctx, state.Request.SessionID, state.ThreadSpec)
	if err != nil {
		state.LastError = err
		return nil
	}
	state.ThreadID = threadID
	if err := stage.app.requestTurnWithSpec(ctx, state.Request.SessionID, threadID, state.TurnSpec); err != nil {
		state.LastError = err
		return nil
	}
	return nil
}

type reActErrorRecoveryStage struct {
	app *App
}

func (reActErrorRecoveryStage) Name() string { return reActStageErrorRecovery }

func (stage reActErrorRecoveryStage) Run(_ context.Context, state *reActLoopState) error {
	if state.LastError == nil {
		return nil
	}
	if stage.app != nil && isThreadNotFoundError(state.LastError) && state.RecoveryCount == 0 {
		log.Printf("react loop stale codex thread detected session=%s thread=%s err=%s", state.Request.SessionID, state.ThreadID, truncate(state.LastError.Error(), 200))
		stage.app.store.ClearThread(state.Request.SessionID)
		stage.app.appendThreadResetCard(state.Request.SessionID)
		stage.app.broadcastSnapshot(state.Request.SessionID)
		state.RecoveryCount++
		state.LastError = nil
		state.NeedsFollowUp = true
		return nil
	}
	return state.LastError
}

type reActToolExecutionStage struct {
	app *App
}

func (reActToolExecutionStage) Name() string { return reActStageToolExecution }

func (stage reActToolExecutionStage) Run(_ context.Context, state *reActLoopState) error {
	// Codex app-server streams tool requests back through handleCodexServerRequest.
	// This stage is kept explicit so the execution strategy can later be replaced
	// with an in-process StreamingToolExecutor without changing callers.
	return nil
}

type reActPostprocessStage struct {
	app *App
}

func (reActPostprocessStage) Name() string { return reActStagePostprocess }

func (stage reActPostprocessStage) Run(_ context.Context, state *reActLoopState) error {
	if stage.app == nil || state.NeedsFollowUp {
		return nil
	}
	stage.app.broadcastSnapshot(state.Request.SessionID)
	return nil
}

type reActLoopDecisionStage struct {
	app *App
}

func (reActLoopDecisionStage) Name() string { return reActStageLoopDecision }

func (reActLoopDecisionStage) Run(_ context.Context, state *reActLoopState) error {
	switch state.StopReason {
	case stopReasonToolUse:
		// Tools completed — schedule a follow-up iteration so the model can
		// observe tool results and decide the next action.
		state.NeedsFollowUp = true
		log.Printf("react loop decision: tool_use follow-up session=%s iteration=%d", state.Request.SessionID, state.Iteration)

	case stopReasonWaitingUser, stopReasonWaitingApproval:
		// The model is waiting on external input. Don't mark the run as
		// completed but also don't loop — the caller will resume later.
		state.NeedsFollowUp = false
		log.Printf("react loop decision: waiting session=%s iteration=%d reason=%s", state.Request.SessionID, state.Iteration, state.StopReason)

	case stopReasonEndTurn:
		// Model finished its turn with no pending tool calls — we're done.
		state.NeedsFollowUp = false
		log.Printf("react loop decision: end_turn completed session=%s iteration=%d", state.Request.SessionID, state.Iteration)

	case stopReasonMaxTokens:
		// Output was truncated. Attempt recovery if we haven't exhausted the
		// recovery budget (reuse the existing RecoveryCount threshold).
		const maxTokenRecoveryThreshold = 2
		if state.RecoveryCount < maxTokenRecoveryThreshold {
			state.RecoveryCount++
			state.NeedsFollowUp = true
			log.Printf("react loop decision: max_tokens recovery session=%s iteration=%d recovery=%d", state.Request.SessionID, state.Iteration, state.RecoveryCount)
		} else {
			state.NeedsFollowUp = false
			state.LastError = fmt.Errorf("react loop max_tokens exceeded recovery threshold session=%s", state.Request.SessionID)
			log.Printf("react loop decision: max_tokens threshold reached session=%s iteration=%d", state.Request.SessionID, state.Iteration)
		}

	default:
		// No explicit stop reason (legacy path) — fall through to the existing
		// behaviour where NeedsFollowUp is only set by recovery stages.
		log.Printf("react loop decision: default pass-through session=%s iteration=%d stop_reason=%s", state.Request.SessionID, state.Iteration, state.StopReason)
	}
	return nil
}

func (a *App) runReActAgentLoop(ctx context.Context, req reActLoopRequest) error {
	state := &reActLoopState{
		Request:        req,
		PermissionMode: "normal",
		Mode:           reActModeAnswer,
		MaxIterations:  3,
	}
	return newDefaultReActLoop(a).Run(ctx, state)
}

func dynamicToolNames(tools []map[string]any) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(getStringAny(tool, "name"))
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
