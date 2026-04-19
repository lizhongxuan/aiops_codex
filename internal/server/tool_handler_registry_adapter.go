package server

import (
	"context"
	"fmt"
	"strings"
)

type unifiedToolHandlerAdapter struct {
	tool UnifiedTool
	desc ToolDescriptor
}

func newToolHandlerAdapter(tool UnifiedTool, desc ToolDescriptor) ToolHandler {
	if tool == nil {
		return nil
	}
	return unifiedToolHandlerAdapter{
		tool: tool,
		desc: desc.Clone(),
	}
}

func (h unifiedToolHandlerAdapter) Descriptor() ToolDescriptor {
	return h.desc.Clone()
}

func (h unifiedToolHandlerAdapter) Execute(ctx context.Context, inv ToolInvocation) (ToolExecutionResult, error) {
	if h.tool == nil {
		return ToolExecutionResult{}, fmt.Errorf("unified tool is nil")
	}

	req := ToolCallRequest{Invocation: inv.Clone()}
	req.Normalize()
	if len(req.Input) == 0 && len(inv.Arguments) > 0 {
		req.Input = cloneNestedAnyMap(inv.Arguments)
	}

	result, err := h.tool.Call(ctx, req)
	if err != nil {
		return ToolExecutionResult{}, err
	}

	display := result.DisplayOutput
	if display == nil && h.tool.Display() != nil {
		display = h.tool.Display().RenderResult(result)
	}
	return toolExecutionResultFromCallResult(inv, result, display), nil
}

type legacyToolUnifiedAdapter struct {
	handler ToolHandler
	desc    ToolDescriptor
}

func newUnifiedToolAdapter(handler ToolHandler, desc ToolDescriptor) UnifiedTool {
	if handler == nil {
		return nil
	}
	return legacyToolUnifiedAdapter{
		handler: handler,
		desc:    desc.Clone(),
	}
}

func (u legacyToolUnifiedAdapter) Name() string {
	return u.desc.Name
}

func (u legacyToolUnifiedAdapter) Aliases() []string {
	return nil
}

func (u legacyToolUnifiedAdapter) Description(ToolDescriptionContext) string {
	if u.desc.DisplayLabel != "" {
		return u.desc.DisplayLabel
	}
	return u.desc.Name
}

func (u legacyToolUnifiedAdapter) InputSchema() map[string]any {
	return map[string]any{}
}

func (u legacyToolUnifiedAdapter) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	if u.handler == nil {
		return ToolCallResult{}, fmt.Errorf("tool handler is nil")
	}

	req.Normalize()
	inv := req.Invocation.Clone()
	if len(req.Input) > 0 {
		inv.Arguments = cloneNestedAnyMap(req.Input)
	}

	result, err := u.handler.Execute(ctx, inv)
	if err != nil {
		return ToolCallResult{}, err
	}
	return toolCallResultFromExecutionResult(result), nil
}

func (u legacyToolUnifiedAdapter) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	approvalType := strings.TrimSpace(u.desc.Kind)
	if u.desc.IsReadOnly {
		approvalType = "readonly"
	}
	allowed := !u.desc.RequiresApproval
	return PermissionResult{
		Allowed:          allowed,
		RequiresApproval: u.desc.RequiresApproval,
		ApprovalType:     approvalType,
	}, nil
}

func (u legacyToolUnifiedAdapter) IsConcurrencySafe(ToolCallRequest) bool {
	return u.desc.IsReadOnly
}

func (u legacyToolUnifiedAdapter) IsReadOnly(ToolCallRequest) bool {
	return u.desc.IsReadOnly
}

func (u legacyToolUnifiedAdapter) IsDestructive(ToolCallRequest) bool {
	return !u.desc.IsReadOnly
}

func (u legacyToolUnifiedAdapter) Display() ToolDisplayAdapter {
	return nil
}

func toolCallResultFromExecutionResult(result ToolExecutionResult) ToolCallResult {
	out := ToolCallResult{
		Metadata: cloneNestedAnyMap(result.ProjectionPayload),
	}
	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}
	if status := strings.TrimSpace(string(result.Status)); status != "" && status != string(ToolRunStatusCompleted) {
		out.Metadata["status"] = status
	}
	if message := strings.TrimSpace(result.ErrorText); message != "" {
		out.Metadata["errorText"] = message
	}
	if lifecycle := strings.TrimSpace(result.LifecycleMessage); lifecycle != "" {
		out.Metadata["lifecycleMessage"] = lifecycle
	}
	if result.OutputText != "" {
		out.Output = result.OutputText
	}
	if len(result.OutputData) > 0 {
		out.StructuredContent = cloneNestedAnyMap(result.OutputData)
	}
	if len(result.EvidenceRefs) > 0 {
		out.EvidenceRefs = append([]string(nil), result.EvidenceRefs...)
	}
	return out
}

func toolExecutionResultFromCallResult(inv ToolInvocation, result ToolCallResult, display *ToolDisplayPayload) ToolExecutionResult {
	status := ToolRunStatusCompleted
	switch strings.ToLower(strings.TrimSpace(getStringAny(result.Metadata, "status", "resultStatus"))) {
	case string(ToolRunStatusFailed), "failure", "error":
		status = ToolRunStatusFailed
	case string(ToolRunStatusCancelled), "canceled":
		status = ToolRunStatusCancelled
	}
	exec := ToolExecutionResult{
		InvocationID: inv.InvocationID,
		Status:       status,
	}
	switch output := result.Output.(type) {
	case string:
		exec.OutputText = output
	case fmt.Stringer:
		exec.OutputText = output.String()
	case nil:
	default:
		exec.OutputText = fmt.Sprint(output)
	}
	if exec.OutputText == "" && result.DisplayOutput != nil {
		exec.OutputText = result.DisplayOutput.Summary
	}
	exec.OutputData = cloneNestedAnyMap(result.StructuredContent)
	exec.ProjectionPayload = cloneNestedAnyMap(result.Metadata)
	if exec.ProjectionPayload == nil {
		exec.ProjectionPayload = map[string]any{}
	}
	exec.LifecycleMessage = strings.TrimSpace(getStringAny(result.Metadata, "lifecycleMessage", "displayLabel"))
	if exec.ErrorText == "" && status != ToolRunStatusCompleted {
		exec.ErrorText = firstNonEmptyValue(
			strings.TrimSpace(getStringAny(result.Metadata, "errorText", "error", "message")),
			strings.TrimSpace(exec.OutputText),
		)
	}
	if display != nil {
		exec.ProjectionPayload["display"] = toolDisplayPayloadToProjectionMap(display)
		if display.FinalCard != nil {
			exec.ProjectionPayload["finalCard"] = toolFinalCardDescriptorToProjectionMap(display.FinalCard)
		}
	}
	if len(result.EvidenceRefs) > 0 {
		exec.EvidenceRefs = append([]string(nil), result.EvidenceRefs...)
	}
	if exec.ErrorText == "" && status != ToolRunStatusCompleted {
		exec.ErrorText = firstNonEmptyValue(strings.TrimSpace(exec.OutputText), string(status))
	}
	return exec
}

func toolDisplayPayloadToProjectionMap(payload *ToolDisplayPayload) map[string]any {
	if payload == nil {
		return nil
	}
	cloned := payload.Clone()
	out := map[string]any{
		"summary":   cloned.Summary,
		"activity":  cloned.Activity,
		"skipCards": cloned.SkipCards,
	}
	if len(cloned.Blocks) > 0 {
		blocks := make([]map[string]any, 0, len(cloned.Blocks))
		for _, block := range cloned.Blocks {
			blocks = append(blocks, toolDisplayBlockToProjectionMap(block))
		}
		out["blocks"] = blocks
	}
	if cloned.FinalCard != nil {
		out["finalCard"] = toolFinalCardDescriptorToProjectionMap(cloned.FinalCard)
	}
	if len(cloned.Metadata) > 0 {
		out["metadata"] = cloned.Metadata
	}
	return out
}

func toolDisplayBlockToProjectionMap(block ToolDisplayBlock) map[string]any {
	cloned := block.Clone()
	out := map[string]any{
		"kind":  cloned.Kind,
		"title": cloned.Title,
		"text":  cloned.Text,
	}
	if len(cloned.Items) > 0 {
		out["items"] = cloned.Items
	}
	if len(cloned.Metadata) > 0 {
		out["metadata"] = cloned.Metadata
	}
	return out
}

func toolFinalCardDescriptorToProjectionMap(card *ToolFinalCardDescriptor) map[string]any {
	if card == nil {
		return nil
	}
	cloned := card.Clone()
	out := map[string]any{
		"cardId":    cloned.CardID,
		"cardType":  cloned.CardType,
		"title":     cloned.Title,
		"text":      cloned.Text,
		"summary":   cloned.Summary,
		"status":    cloned.Status,
		"command":   cloned.Command,
		"cwd":       cloned.Cwd,
		"hostId":    cloned.HostID,
		"hostName":  cloned.HostName,
		"createdAt": cloned.CreatedAt,
		"updatedAt": cloned.UpdatedAt,
	}
	if len(cloned.Detail) > 0 {
		out["detail"] = cloned.Detail
	}
	return out
}
