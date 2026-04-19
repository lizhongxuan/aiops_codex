package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
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

type resolvedDispatchTool struct {
	descriptor      ToolDescriptor
	handler         ToolHandler
	unified         UnifiedTool
	request         ToolCallRequest
	permission      PermissionResult
	readOnly        bool
	destructive     bool
	concurrencySafe bool
}

func categorizeToolForDispatch(toolName string) toolDispatchCategory {
	meta := lookupToolRiskMetadata(toolName)
	if meta.DispatchCategory == "" {
		return toolCategoryMutation
	}
	return meta.DispatchCategory
}

// toolDispatcher manages the execution of tool batches.
type toolDispatcher struct {
	app       *App
	registry  *ToolHandlerRegistry
	emitter   ToolEventEmitter
	approvals ToolApprovalCoordinator
}

// newToolDispatcher creates a new tool dispatcher.
func newToolDispatcher(app *App, registry *ToolHandlerRegistry, emitter ToolEventEmitter, approvals ToolApprovalCoordinator) *toolDispatcher {
	return &toolDispatcher{
		app:       app,
		registry:  registry,
		emitter:   emitter,
		approvals: approvals,
	}
}

// Dispatch executes a single invocation through the unified handler registry and
// emits lifecycle events that downstream projection subscribers can consume.
func (d *toolDispatcher) Dispatch(ctx context.Context, invocation ToolInvocation) (ToolExecutionResult, error) {
	if d == nil {
		return ToolExecutionResult{}, errors.New("tool dispatcher is nil")
	}
	if d.registry == nil {
		return ToolExecutionResult{}, errors.New("tool handler registry is nil")
	}
	if invocation.InvocationID == "" {
		invocation.InvocationID = model.NewID("toolinv")
	}
	if invocation.SessionID == "" {
		return ToolExecutionResult{}, errors.New("tool invocation session id is required")
	}
	if invocation.ToolName == "" {
		return ToolExecutionResult{}, errors.New("tool invocation tool name is required")
	}
	if invocation.StartedAt.IsZero() {
		invocation.StartedAt = time.Now()
	}

	resolved, err := d.resolveToolExecution(ctx, invocation)
	if err != nil {
		_ = d.emit(ctx, newToolFailedEvent(invocation, err))
		return ToolExecutionResult{}, err
	}

	invocation.ToolKind = firstNonEmptyValue(invocation.ToolKind, resolved.descriptor.Kind)
	invocation.RequiresApproval = resolved.permission.RequiresApproval
	invocation.ReadOnly = resolved.readOnly

	if invocation.RequiresApproval {
		resolution, err := d.coordinateApproval(ctx, invocation, resolved.descriptor, resolved.permission, resolved.destructive)
		if err != nil {
			emitErr := d.emit(ctx, newToolFailedEvent(invocation, err))
			if emitErr != nil {
				log.Printf("[tool_dispatcher] failed to emit approval failure event tool=%s invocation=%s err=%v", invocation.ToolName, invocation.InvocationID, emitErr)
			}
			return ToolExecutionResult{}, err
		}
		if resolution.IsPending() {
			return newToolWaitingApprovalResult(invocation, resolution), nil
		}
		if !resolution.IsApproved() {
			return newToolApprovalDeclinedResult(invocation, resolution), nil
		}
	} else if !resolved.permission.Allowed {
		reason := firstNonEmptyValue(strings.TrimSpace(resolved.permission.Reason), "tool execution is not allowed")
		err := errors.New(reason)
		emitErr := d.emit(ctx, newToolFailedEvent(invocation, err))
		if emitErr != nil {
			log.Printf("[tool_dispatcher] failed to emit denied event tool=%s invocation=%s err=%v", invocation.ToolName, invocation.InvocationID, emitErr)
		}
		return ToolExecutionResult{}, err
	}

	startedEvent := newToolStartedEvent(invocation, resolved.descriptor)
	injectToolDisplayPayload(&startedEvent, renderToolUseDisplay(resolved.unified, resolved.request))
	if err := d.emit(ctx, startedEvent); err != nil {
		return ToolExecutionResult{}, err
	}

	execCtx := ctx
	if resolved.descriptor.SupportsStreamingProgress {
		execCtx = withToolProgressReporter(ctx, d.progressReporter(ctx, invocation, resolved.descriptor, resolved.unified))
	}

	var (
		result  ToolExecutionResult
		execErr error
	)
	if resolved.unified != nil {
		callResult, callErr := resolved.unified.Call(execCtx, resolved.request)
		if callErr != nil {
			emitErr := d.emit(ctx, newToolFailedEvent(invocation, callErr))
			if emitErr != nil {
				log.Printf("[tool_dispatcher] failed to emit failed event tool=%s invocation=%s err=%v", invocation.ToolName, invocation.InvocationID, emitErr)
			}
			return ToolExecutionResult{}, callErr
		}
		display := callResult.DisplayOutput
		if display == nil && resolved.unified.Display() != nil {
			display = resolved.unified.Display().RenderResult(callResult)
		}
		result = toolExecutionResultFromCallResult(invocation, callResult, display)
	} else {
		result, execErr = resolved.handler.Execute(execCtx, invocation)
		if execErr != nil {
			emitErr := d.emit(ctx, newToolFailedEvent(invocation, execErr))
			if emitErr != nil {
				log.Printf("[tool_dispatcher] failed to emit failed event tool=%s invocation=%s err=%v", invocation.ToolName, invocation.InvocationID, emitErr)
			}
			return ToolExecutionResult{}, execErr
		}
	}

	if result.InvocationID == "" {
		result.InvocationID = invocation.InvocationID
	}
	if result.FinishedAt.IsZero() {
		result.FinishedAt = time.Now()
	}
	if result.Status == "" {
		result.Status = ToolRunStatusCompleted
	}

	event, emitErr := d.resultEvent(invocation, result)
	if emitErr != nil {
		return result, emitErr
	}
	if err := d.emit(ctx, event); err != nil {
		return result, err
	}
	return result, nil
}

func (d *toolDispatcher) emit(ctx context.Context, event ToolLifecycleEvent) error {
	if d == nil || d.emitter == nil {
		return nil
	}
	return d.emitter.Emit(ctx, event)
}

func (d *toolDispatcher) progressReporter(ctx context.Context, invocation ToolInvocation, descriptor ToolDescriptor, unified UnifiedTool) toolProgressReporter {
	if d == nil || d.emitter == nil {
		return nil
	}
	return func(update ToolProgressUpdate) error {
		if display := renderToolProgressDisplay(unified, invocation, update); display != nil {
			update.Payload = mergeToolLifecyclePayload(update.Payload, map[string]any{
				"display": toolDisplayPayloadToProjectionMap(display),
			})
			if display.FinalCard != nil {
				if update.Payload == nil {
					update.Payload = map[string]any{}
				}
				update.Payload["finalCard"] = toolFinalCardDescriptorToProjectionMap(display.FinalCard)
			}
		}
		return d.emit(ctx, newToolProgressEvent(invocation, descriptor, update))
	}
}

func (d *toolDispatcher) coordinateApproval(ctx context.Context, invocation ToolInvocation, descriptor ToolDescriptor, permission PermissionResult, destructive bool) (ApprovalResolution, error) {
	if d == nil {
		return ApprovalResolution{}, errors.New("tool dispatcher is nil")
	}
	if !permission.RequiresApproval {
		return ApprovalResolution{}, nil
	}
	if d.approvals == nil {
		return ApprovalResolution{}, fmt.Errorf("tool %q requires approval but no approval coordinator is configured", invocation.ToolName)
	}

	metadata := cloneNestedAnyMap(permission.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["toolKind"] = firstNonEmptyValue(invocation.ToolKind, descriptor.Kind)
	metadata["displayLabel"] = firstNonEmptyValue(strings.TrimSpace(descriptor.DisplayLabel), strings.TrimSpace(defaultToolLifecycleLabel(invocation.ToolName, invocation.Arguments)))
	metadata["isReadOnly"] = invocation.ReadOnly
	metadata["isDestructive"] = destructive
	if approvalType := strings.TrimSpace(permission.ApprovalType); approvalType != "" {
		metadata["approvalType"] = approvalType
	}
	if len(permission.ApprovalDecisions) > 0 {
		metadata["decisions"] = append([]string(nil), permission.ApprovalDecisions...)
	}

	resolution, err := d.approvals.Request(ctx, ToolApprovalRequest{
		SessionID: invocation.SessionID,
		HostID:    invocation.HostID,
		ToolName:  invocation.ToolName,
		Reason: firstNonEmptyValue(
			strings.TrimSpace(permission.Reason),
			strings.TrimSpace(descriptor.DisplayLabel),
			strings.TrimSpace(defaultToolLifecycleLabel(invocation.ToolName, invocation.Arguments)),
			strings.TrimSpace(invocation.ToolName),
		),
		Invocation: invocation.Clone(),
		Metadata:   metadata,
	})
	if err != nil {
		return ApprovalResolution{}, err
	}

	if err := d.emit(ctx, newToolApprovalRequestedEvent(invocation, descriptor, resolution)); err != nil {
		return ApprovalResolution{}, err
	}
	if resolution.IsPending() {
		return resolution, nil
	}
	if err := d.emit(ctx, newToolApprovalResolvedEvent(invocation, descriptor, resolution)); err != nil {
		return ApprovalResolution{}, err
	}
	return resolution, nil
}

func (d *toolDispatcher) resultEvent(invocation ToolInvocation, result ToolExecutionResult) (ToolLifecycleEvent, error) {
	switch result.Status {
	case ToolRunStatusCompleted:
		return newToolCompletedEvent(invocation, result), nil
	case ToolRunStatusFailed:
		return newToolFailedResultEvent(invocation, result), nil
	case ToolRunStatusCancelled:
		return newToolCancelledEvent(invocation, result), nil
	default:
		return ToolLifecycleEvent{}, fmt.Errorf("unsupported tool execution status %q", result.Status)
	}
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
		category := d.dispatchCategoryForRequest(req)
		switch category {
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

func (d *toolDispatcher) resolveToolExecution(ctx context.Context, invocation ToolInvocation) (resolvedDispatchTool, error) {
	req := ToolCallRequest{
		Invocation: invocation.Clone(),
		Input:      cloneNestedAnyMap(invocation.Arguments),
		RawInput:   invocation.RawArguments,
	}
	req.Normalize()

	resolved := resolvedDispatchTool{
		request:    req,
		permission: PermissionResult{Allowed: true, RequiresApproval: invocation.RequiresApproval},
	}

	if unifiedDescriptor, unified, ok := d.registry.LookupUnified(invocation.ToolName); ok && unified != nil {
		resolved.descriptor = unifiedDescriptor
		resolved.unified = unified
		permission, err := unified.CheckPermissions(ctx, req)
		if err != nil {
			return resolvedDispatchTool{}, err
		}
		resolved.permission = permission.Clone()
		resolved.readOnly = unified.IsReadOnly(req)
		resolved.destructive = unified.IsDestructive(req)
		resolved.concurrencySafe = unified.IsConcurrencySafe(req)
	} else {
		descriptor, handler, ok := d.registry.Lookup(invocation.ToolName)
		if !ok || handler == nil {
			return resolvedDispatchTool{}, fmt.Errorf("tool handler %q is not registered", invocation.ToolName)
		}
		resolved.descriptor = descriptor
		resolved.handler = handler
		resolved.permission = PermissionResult{Allowed: true, RequiresApproval: invocation.RequiresApproval || descriptor.RequiresApproval}
		resolved.readOnly = invocation.ReadOnly || descriptor.IsReadOnly
		resolved.destructive = !descriptor.IsReadOnly
		resolved.concurrencySafe = descriptor.IsReadOnly
	}

	if resolved.permission.RequiresApproval {
		resolved.permission.Allowed = false
	}

	return resolved, nil
}

func (d *toolDispatcher) dispatchCategoryForRequest(req toolDispatchRequest) toolDispatchCategory {
	switch req.Category {
	case toolCategoryBlocking, toolCategoryApproval:
		return req.Category
	}

	if d != nil && d.registry != nil {
		callReq := ToolCallRequest{
			Invocation: ToolInvocation{
				ToolName:  req.ToolName,
				Arguments: cloneNestedAnyMap(req.Input),
			},
			Input: cloneNestedAnyMap(req.Input),
		}
		callReq.Normalize()
		if _, unified, ok := d.registry.LookupUnified(req.ToolName); ok && unified != nil {
			permission, err := unified.CheckPermissions(context.Background(), callReq)
			if err == nil && permission.RequiresApproval {
				return toolCategoryApproval
			}
			if unified.IsReadOnly(callReq) && unified.IsConcurrencySafe(callReq) && !unified.IsDestructive(callReq) {
				return toolCategoryReadonly
			}
			return toolCategoryMutation
		}
		return d.registry.DispatchCategory(req.ToolName, req.Category)
	}

	if req.Category != "" {
		return req.Category
	}
	return categorizeToolForDispatch(req.ToolName)
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
		})
	}
	return requests
}

// validateToolPermission checks if a tool is allowed given the current permission mode.
func validateToolPermission(toolName, permissionMode string, planMode bool) error {
	meta := lookupToolRiskMetadata(toolName)
	category := meta.DispatchCategory
	if category == "" {
		category = toolCategoryMutation
	}

	if planMode && !meta.AllowedInPlanMode {
		return fmt.Errorf("tool %q is not allowed in plan mode", toolName)
	}

	if permissionMode == "readonly" && category == toolCategoryMutation {
		return fmt.Errorf("tool %q requires mutation permission, current mode is readonly", toolName)
	}

	return nil
}

func newToolStartedEvent(invocation ToolInvocation, descriptor ToolDescriptor) ToolLifecycleEvent {
	label := firstNonEmptyValue(strings.TrimSpace(descriptor.DisplayLabel), strings.TrimSpace(defaultToolLifecycleLabel(invocation.ToolName, invocation.Arguments)))
	now := invocation.StartedAt
	if now.IsZero() {
		now = time.Now()
	}
	payload := map[string]any{
		"arguments":          cloneToolPayload(invocation.Arguments),
		"toolKind":           firstNonEmptyValue(invocation.ToolKind, descriptor.Kind),
		"requiresApproval":   invocation.RequiresApproval,
		"isReadOnly":         invocation.ReadOnly,
		"displayLabel":       label,
		"source":             string(invocation.Source),
		"workspaceSessionId": strings.TrimSpace(invocation.WorkspaceID),
	}
	inheritToolLifecycleBooleanFlag(payload, invocation.Arguments, "trackActivityStart")
	inheritToolLifecycleBooleanFlag(payload, invocation.Arguments, "skipCardProjection")
	return ToolLifecycleEvent{
		EventID:      model.NewID("toolevent"),
		InvocationID: invocation.InvocationID,
		SessionID:    invocation.SessionID,
		ToolName:     invocation.ToolName,
		Type:         ToolLifecycleEventStarted,
		Phase:        firstNonEmptyValue(strings.TrimSpace(descriptor.StartPhase), strings.TrimSpace(defaultToolLifecyclePhase(invocation.ToolName, invocation.Arguments))),
		HostID:       defaultHostID(invocation.HostID),
		CallID:       strings.TrimSpace(invocation.CallID),
		CardID:       toolLifecycleCardID(invocation),
		Label:        label,
		Message:      label,
		ActivityKind: defaultToolLifecycleActivityKind(invocation.ToolName),
		ActivityTarget: defaultToolLifecycleActivityTarget(
			invocation.ToolName,
			invocation.Arguments,
			label,
		),
		ActivityQuery: defaultToolLifecycleActivityQuery(invocation.ToolName, invocation.Arguments),
		Timestamp:     now,
		CreatedAt:     model.NowString(),
		Payload:       payload,
	}
}

func newToolCompletedEvent(invocation ToolInvocation, result ToolExecutionResult) ToolLifecycleEvent {
	label := firstNonEmptyValue(
		strings.TrimSpace(result.LifecycleMessage),
		strings.TrimSpace(defaultToolLifecycleCompletedLabel(invocation.ToolName, invocation.Arguments)),
		strings.TrimSpace(defaultToolLifecycleLabel(invocation.ToolName, invocation.Arguments)),
	)
	payload := map[string]any{
		"arguments":    cloneToolPayload(invocation.Arguments),
		"outputText":   result.OutputText,
		"outputData":   cloneToolPayload(result.OutputData),
		"evidenceRefs": append([]string(nil), result.EvidenceRefs...),
		"displayLabel": label,
	}
	payload = mergeToolLifecyclePayload(payload, result.ProjectionPayload)
	return ToolLifecycleEvent{
		EventID:      model.NewID("toolevent"),
		InvocationID: invocation.InvocationID,
		SessionID:    invocation.SessionID,
		ToolName:     invocation.ToolName,
		Type:         ToolLifecycleEventCompleted,
		Phase:        "thinking",
		HostID:       defaultHostID(invocation.HostID),
		CallID:       strings.TrimSpace(invocation.CallID),
		CardID:       toolLifecycleCardID(invocation),
		Label:        label,
		Message:      label,
		Timestamp:    result.FinishedAt,
		CreatedAt:    model.NowString(),
		Payload:      payload,
	}
}

func newToolProgressEvent(invocation ToolInvocation, descriptor ToolDescriptor, update ToolProgressUpdate) ToolLifecycleEvent {
	label := firstNonEmptyValue(
		strings.TrimSpace(update.Label),
		strings.TrimSpace(update.Message),
		strings.TrimSpace(descriptor.DisplayLabel),
		strings.TrimSpace(defaultToolLifecycleLabel(invocation.ToolName, invocation.Arguments)),
	)
	message := firstNonEmptyValue(strings.TrimSpace(update.Message), strings.TrimSpace(update.Label), label)
	now := update.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	payload := map[string]any{
		"arguments":    cloneToolPayload(invocation.Arguments),
		"displayLabel": label,
	}
	payload = mergeToolLifecyclePayload(payload, update.Payload)
	inheritToolLifecycleBooleanFlag(payload, invocation.Arguments, "skipCardProjection")

	return ToolLifecycleEvent{
		EventID:        model.NewID("toolevent"),
		InvocationID:   invocation.InvocationID,
		SessionID:      invocation.SessionID,
		ToolName:       invocation.ToolName,
		Type:           ToolLifecycleEventProgress,
		Phase:          firstNonEmptyValue(strings.TrimSpace(update.Phase), strings.TrimSpace(descriptor.StartPhase), strings.TrimSpace(defaultToolLifecyclePhase(invocation.ToolName, invocation.Arguments))),
		HostID:         defaultHostID(invocation.HostID),
		CallID:         strings.TrimSpace(invocation.CallID),
		CardID:         toolLifecycleCardID(invocation),
		Label:          label,
		Message:        message,
		ActivityKind:   firstNonEmptyValue(strings.TrimSpace(update.ActivityKind), defaultToolLifecycleActivityKind(invocation.ToolName)),
		ActivityTarget: firstNonEmptyValue(strings.TrimSpace(update.ActivityTarget), defaultToolLifecycleActivityTarget(invocation.ToolName, invocation.Arguments, label)),
		ActivityQuery:  firstNonEmptyValue(strings.TrimSpace(update.ActivityQuery), defaultToolLifecycleActivityQuery(invocation.ToolName, invocation.Arguments)),
		Timestamp:      now,
		CreatedAt:      firstNonEmptyValue(strings.TrimSpace(update.CreatedAt), toolLifecycleEventTimeString(now)),
		Payload:        payload,
		Metadata:       cloneToolPayload(update.Metadata),
	}
}

func newToolFailedEvent(invocation ToolInvocation, err error) ToolLifecycleEvent {
	cardID := toolLifecycleCardID(invocation)
	errorText := firstNonEmptyValue(errorString(err), "tool execution failed")
	return ToolLifecycleEvent{
		EventID:      model.NewID("toolevent"),
		InvocationID: invocation.InvocationID,
		SessionID:    invocation.SessionID,
		ToolName:     invocation.ToolName,
		Type:         ToolLifecycleEventFailed,
		Phase:        "thinking",
		HostID:       defaultHostID(invocation.HostID),
		CallID:       strings.TrimSpace(invocation.CallID),
		CardID:       cardID,
		Label:        errorText,
		Message:      errorText,
		Error:        errorText,
		Timestamp:    time.Now(),
		CreatedAt:    model.NowString(),
		Payload: map[string]any{
			"arguments": cloneToolPayload(invocation.Arguments),
			"error":     errorText,
		},
	}
}

func newToolFailedResultEvent(invocation ToolInvocation, result ToolExecutionResult) ToolLifecycleEvent {
	errorText := firstNonEmptyValue(strings.TrimSpace(result.ErrorText), "tool execution failed")
	payload := map[string]any{
		"arguments":  cloneToolPayload(invocation.Arguments),
		"error":      errorText,
		"outputData": cloneToolPayload(result.OutputData),
	}
	payload = mergeToolLifecyclePayload(payload, result.ProjectionPayload)
	return ToolLifecycleEvent{
		EventID:      model.NewID("toolevent"),
		InvocationID: invocation.InvocationID,
		SessionID:    invocation.SessionID,
		ToolName:     invocation.ToolName,
		Type:         ToolLifecycleEventFailed,
		Phase:        "thinking",
		HostID:       defaultHostID(invocation.HostID),
		CallID:       strings.TrimSpace(invocation.CallID),
		CardID:       toolLifecycleCardID(invocation),
		Label:        errorText,
		Message:      errorText,
		Error:        errorText,
		Timestamp:    result.FinishedAt,
		CreatedAt:    model.NowString(),
		Payload:      payload,
	}
}

func newToolCancelledEvent(invocation ToolInvocation, result ToolExecutionResult) ToolLifecycleEvent {
	label := firstNonEmptyValue(strings.TrimSpace(result.LifecycleMessage), strings.TrimSpace(result.ErrorText), "tool execution cancelled")
	payload := map[string]any{
		"arguments": cloneToolPayload(invocation.Arguments),
		"error":     strings.TrimSpace(result.ErrorText),
	}
	payload = mergeToolLifecyclePayload(payload, result.ProjectionPayload)
	return ToolLifecycleEvent{
		EventID:      model.NewID("toolevent"),
		InvocationID: invocation.InvocationID,
		SessionID:    invocation.SessionID,
		ToolName:     invocation.ToolName,
		Type:         ToolLifecycleEventCancelled,
		Phase:        "thinking",
		HostID:       defaultHostID(invocation.HostID),
		CallID:       strings.TrimSpace(invocation.CallID),
		CardID:       toolLifecycleCardID(invocation),
		Label:        label,
		Message:      label,
		Error:        strings.TrimSpace(result.ErrorText),
		Timestamp:    result.FinishedAt,
		CreatedAt:    model.NowString(),
		Payload:      payload,
	}
}

func newToolApprovalRequestedEvent(invocation ToolInvocation, descriptor ToolDescriptor, resolution ApprovalResolution) ToolLifecycleEvent {
	label := firstNonEmptyValue(
		strings.TrimSpace(resolution.Reason),
		strings.TrimSpace(descriptor.DisplayLabel),
		strings.TrimSpace(defaultToolLifecycleLabel(invocation.ToolName, invocation.Arguments)),
		"需要审批",
	)
	now := resolution.RequestedAt
	if now.IsZero() {
		now = time.Now()
	}
	approvalID := firstNonEmptyValue(strings.TrimSpace(resolution.ApprovalID), model.NewID("approval"))
	cardID := toolApprovalCardID(invocation, resolution)
	decisions := toolApprovalDecisions(resolution)
	return ToolLifecycleEvent{
		EventID:      model.NewID("toolevent"),
		InvocationID: invocation.InvocationID,
		SessionID:    invocation.SessionID,
		ToolName:     invocation.ToolName,
		Type:         ToolLifecycleEventApprovalRequested,
		Phase:        "waiting_approval",
		HostID:       defaultHostID(invocation.HostID),
		CallID:       strings.TrimSpace(invocation.CallID),
		CardID:       cardID,
		ApprovalID:   approvalID,
		Label:        label,
		Message:      label,
		Timestamp:    now,
		CreatedAt:    toolLifecycleEventTimeString(now),
		Payload: map[string]any{
			"approval": map[string]any{
				"approvalId":   approvalID,
				"cardId":       cardID,
				"approvalType": toolApprovalType(invocation, descriptor, resolution),
				"status":       string(ApprovalResolutionStatusPending),
				"hostId":       defaultHostID(invocation.HostID),
				"threadId":     strings.TrimSpace(invocation.ThreadID),
				"turnId":       strings.TrimSpace(invocation.TurnID),
				"command":      defaultToolLifecycleActivityTarget(invocation.ToolName, invocation.Arguments, label),
				"reason":       label,
				"requestedAt":  toolLifecycleEventTimeString(now),
				"decisions":    decisions,
			},
			"card": map[string]any{
				"cardId":    cardID,
				"cardType":  "ApprovalCard",
				"status":    "pending",
				"title":     label,
				"text":      label,
				"summary":   label,
				"createdAt": toolLifecycleEventTimeString(now),
				"updatedAt": toolLifecycleEventTimeString(now),
			},
		},
		Metadata: map[string]any{
			"approval": map[string]any{
				"ruleName":               strings.TrimSpace(resolution.RuleName),
				"autoApproved":           resolution.AutoApproved,
				"requiresManualApproval": resolution.RequiresManualApproval,
			},
		},
	}
}

func newToolApprovalResolvedEvent(invocation ToolInvocation, descriptor ToolDescriptor, resolution ApprovalResolution) ToolLifecycleEvent {
	label := firstNonEmptyValue(
		strings.TrimSpace(resolution.Reason),
		strings.TrimSpace(descriptor.DisplayLabel),
		strings.TrimSpace(defaultToolLifecycleLabel(invocation.ToolName, invocation.Arguments)),
		"审批已处理",
	)
	now := resolution.ResolvedAt
	if now.IsZero() {
		now = time.Now()
	}
	approvalID := firstNonEmptyValue(strings.TrimSpace(resolution.ApprovalID), model.NewID("approval"))
	cardID := toolApprovalCardID(invocation, resolution)
	status := toolApprovalResolutionValue(resolution.Status)
	return ToolLifecycleEvent{
		EventID:      model.NewID("toolevent"),
		InvocationID: invocation.InvocationID,
		SessionID:    invocation.SessionID,
		ToolName:     invocation.ToolName,
		Type:         ToolLifecycleEventApprovalResolved,
		Phase:        "thinking",
		HostID:       defaultHostID(invocation.HostID),
		CallID:       strings.TrimSpace(invocation.CallID),
		CardID:       cardID,
		ApprovalID:   approvalID,
		Label:        label,
		Message:      label,
		Timestamp:    now,
		CreatedAt:    toolLifecycleEventTimeString(now),
		Payload: map[string]any{
			"approval": map[string]any{
				"approvalId": approvalID,
				"cardId":     cardID,
				"status":     status,
				"decision":   status,
				"summary":    label,
				"resolvedAt": toolLifecycleEventTimeString(now),
			},
			"card": map[string]any{
				"cardId":    cardID,
				"cardType":  "ApprovalCard",
				"summary":   label,
				"text":      label,
				"updatedAt": toolLifecycleEventTimeString(now),
			},
		},
		Metadata: map[string]any{
			"approval": map[string]any{
				"ruleName":     strings.TrimSpace(resolution.RuleName),
				"autoApproved": resolution.AutoApproved,
			},
		},
	}
}

func newToolWaitingApprovalResult(invocation ToolInvocation, resolution ApprovalResolution) ToolExecutionResult {
	finishedAt := resolution.RequestedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	return ToolExecutionResult{
		InvocationID: invocation.InvocationID,
		Status:       ToolRunStatusWaitingApproval,
		OutputData: map[string]any{
			"approvalId":     strings.TrimSpace(resolution.ApprovalID),
			"approvalStatus": toolApprovalResolutionValue(resolution.Status),
		},
		FinishedAt: finishedAt,
	}
}

func newToolApprovalDeclinedResult(invocation ToolInvocation, resolution ApprovalResolution) ToolExecutionResult {
	finishedAt := resolution.ResolvedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	return ToolExecutionResult{
		InvocationID: invocation.InvocationID,
		Status:       ToolRunStatusCancelled,
		ErrorText:    firstNonEmptyValue(strings.TrimSpace(resolution.Reason), "tool execution was not approved"),
		OutputData: map[string]any{
			"approvalId":     strings.TrimSpace(resolution.ApprovalID),
			"approvalStatus": toolApprovalResolutionValue(resolution.Status),
		},
		FinishedAt: finishedAt,
	}
}

func defaultToolLifecyclePhase(toolName string, args map[string]any) string {
	phase, _ := toolPhaseAndLabel(toolName, args)
	switch normalizeRuntimeTurnPhase(phase) {
	case "thinking", "planning", "waiting_approval", "waiting_input", "executing", "finalizing":
		return phase
	default:
		return "thinking"
	}
}

func defaultToolLifecycleLabel(toolName string, args map[string]any) string {
	_, label := toolPhaseAndLabel(toolName, args)
	return label
}

func defaultToolLifecycleCompletedLabel(toolName string, args map[string]any) string {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "list_files", "list_dir", "list_remote_files":
		path, _ := args["path"].(string)
		if path != "" {
			return "已列出 " + path
		}
	case "read_file", "read_remote_file", "host_file_read":
		path, _ := args["path"].(string)
		if path != "" {
			return "已浏览 " + path
		}
	case "host_file_search":
		pattern := getStringAny(args, "pattern", "query")
		if pattern != "" {
			return "已搜索内容（" + pattern + "）"
		}
	case "search_files":
		query, _ := args["query"].(string)
		if query != "" {
			return "已搜索内容（" + query + "）"
		}
	case "search_remote_files":
		query, _ := args["query"].(string)
		if query != "" {
			return "已搜索内容（" + query + "）"
		}
	case "web_search":
		query, _ := args["query"].(string)
		if query != "" {
			return "已搜索网页（" + query + "）"
		}
	case "write_file", "apply_patch":
		path, _ := args["path"].(string)
		if path != "" {
			return "已修改 " + path
		}
	case "execute_readonly_query":
		command, _ := args["command"].(string)
		if command != "" {
			return "已执行只读命令：" + command
		}
	case "readonly_host_inspect":
		command, _ := args["command"].(string)
		if command != "" {
			return "已完成只读巡检：" + command
		}
	}
	return ""
}

func defaultToolLifecycleActivityKind(toolName string) string {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "web_search", "search_files", "search_remote_files", "host_file_search", "find_in_page":
		return "search"
	case "read_file", "read_remote_file", "host_file_read", "open_page", "list_files", "list_dir", "list_remote_files":
		return "browse"
	case "write_file", "apply_patch", "execute_command", "execute_readonly_query", "readonly_host_inspect", "shell_command":
		return "execute"
	default:
		return "tool"
	}
}

func defaultToolLifecycleActivityTarget(toolName string, args map[string]any, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "read_file", "read_remote_file", "host_file_read", "write_file", "list_files", "list_dir", "list_remote_files":
		if path, _ := args["path"].(string); strings.TrimSpace(path) != "" {
			return strings.TrimSpace(path)
		}
	case "open_page":
		if url, _ := args["url"].(string); strings.TrimSpace(url) != "" {
			return strings.TrimSpace(url)
		}
	case "execute_command", "execute_readonly_query", "readonly_host_inspect", "shell_command":
		if command, _ := args["command"].(string); strings.TrimSpace(command) != "" {
			return strings.TrimSpace(command)
		}
	}
	return strings.TrimSpace(fallback)
}

func defaultToolLifecycleActivityQuery(toolName string, args map[string]any) string {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "web_search", "search_files", "search_remote_files", "host_file_search", "find_in_page":
		if query, _ := args["query"].(string); strings.TrimSpace(query) != "" {
			return strings.TrimSpace(query)
		}
		if pattern, _ := args["pattern"].(string); strings.TrimSpace(pattern) != "" {
			return strings.TrimSpace(pattern)
		}
	}
	return ""
}

func toolLifecycleCardID(invocation ToolInvocation) string {
	if args := invocation.Arguments; args != nil {
		if processCardID, _ := args["processCardId"].(string); strings.TrimSpace(processCardID) != "" {
			return strings.TrimSpace(processCardID)
		}
	}
	if callID := strings.TrimSpace(invocation.CallID); callID != "" {
		return "proc-" + callID
	}
	if invocationID := strings.TrimSpace(invocation.InvocationID); invocationID != "" {
		return "proc-" + invocationID
	}
	return model.NewID("proc")
}

func toolApprovalCardID(invocation ToolInvocation, resolution ApprovalResolution) string {
	if cardID := strings.TrimSpace(getStringAny(resolution.Request.Metadata, "cardId", "cardID", "card_id")); cardID != "" {
		return cardID
	}
	if approvalID := strings.TrimSpace(resolution.ApprovalID); approvalID != "" {
		return "approval-card-" + approvalID
	}
	if callID := strings.TrimSpace(invocation.CallID); callID != "" {
		return "approval-card-" + callID
	}
	if invocationID := strings.TrimSpace(invocation.InvocationID); invocationID != "" {
		return "approval-card-" + invocationID
	}
	return model.NewID("approval-card")
}

func toolApprovalType(invocation ToolInvocation, descriptor ToolDescriptor, resolution ApprovalResolution) string {
	if approvalType := strings.TrimSpace(getStringAny(resolution.Request.Metadata, "approvalType", "approval_type")); approvalType != "" {
		return approvalType
	}
	switch {
	case invocation.ReadOnly || descriptor.IsReadOnly:
		return "readonly"
	case strings.TrimSpace(descriptor.Kind) != "":
		return strings.TrimSpace(descriptor.Kind)
	default:
		return "tool"
	}
}

func toolApprovalDecisions(resolution ApprovalResolution) []string {
	if decisions := stringsFromAny(resolution.Request.Metadata["decisions"]); len(decisions) > 0 {
		return decisions
	}
	return []string{"accept", "decline"}
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func renderToolUseDisplay(tool UnifiedTool, req ToolCallRequest) *ToolDisplayPayload {
	if tool == nil || tool.Display() == nil {
		return nil
	}
	return tool.Display().RenderUse(req)
}

func renderToolProgressDisplay(tool UnifiedTool, invocation ToolInvocation, update ToolProgressUpdate) *ToolDisplayPayload {
	if tool == nil || tool.Display() == nil {
		return nil
	}
	return tool.Display().RenderProgress(ToolProgressEvent{
		Invocation: invocation.Clone(),
		Update:     update,
	})
}

func injectToolDisplayPayload(event *ToolLifecycleEvent, display *ToolDisplayPayload) {
	if event == nil || display == nil {
		return
	}
	payload := cloneToolPayload(event.Payload)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["display"] = toolDisplayPayloadToProjectionMap(display)
	if display.FinalCard != nil {
		payload["finalCard"] = toolFinalCardDescriptorToProjectionMap(display.FinalCard)
	}
	event.Payload = payload
}

func toolApprovalResolutionValue(status ApprovalResolutionStatus) string {
	switch status {
	case ApprovalResolutionStatusApproved:
		return "approved"
	case ApprovalResolutionStatusDeclined:
		return "declined"
	default:
		return "pending"
	}
}

func toolLifecycleEventTimeString(ts time.Time) string {
	if ts.IsZero() {
		return model.NowString()
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func cloneToolPayload(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func mergeToolLifecyclePayload(base, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	merged := cloneToolPayload(base)
	if merged == nil {
		merged = make(map[string]any)
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func inheritToolLifecycleBooleanFlag(dst, src map[string]any, key string) {
	if len(dst) == 0 || len(src) == 0 {
		return
	}
	value, ok := src[key].(bool)
	if !ok {
		return
	}
	dst[key] = value
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
