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
	Request        reActLoopRequest
	Messages       []map[string]any
	Attachments    []string
	AvailableTools []string
	PermissionMode string
	PlanMode       bool
	RunID          string
	Iteration      int
	MaxIterations  int
	ThreadSpec     threadStartSpec
	TurnSpec       turnStartSpec
	ThreadID       string
	LastError      error
	NeedsFollowUp  bool
	RecoveryCount  int
	Checkpoints    []string
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
	for {
		if state.Iteration >= state.MaxIterations {
			if state.LastError != nil {
				return fmt.Errorf("react loop exceeded max iterations after recovery: %w", state.LastError)
			}
			return fmt.Errorf("react loop exceeded max iterations")
		}
		state.Iteration++
		state.NeedsFollowUp = false
		state.LastError = nil
		for _, stage := range loop.stages {
			if stage == nil {
				continue
			}
			state.Checkpoints = append(state.Checkpoints, stage.Name())
			if err := stage.Run(ctx, state); err != nil {
				return err
			}
		}
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
	// A successful turn/start hands the streaming ReAct continuation to the Codex
	// app-server. Follow-up iterations inside the same turn are driven by tool
	// result notifications; synchronous follow-up is only used for recovery.
	return nil
}

func (a *App) runReActAgentLoop(ctx context.Context, req reActLoopRequest) error {
	state := &reActLoopState{
		Request:        req,
		PermissionMode: "normal",
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
