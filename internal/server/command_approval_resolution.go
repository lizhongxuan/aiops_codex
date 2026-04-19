package server

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func commandApprovalToolName(explicitTool string, readonly bool) string {
	toolName := strings.TrimSpace(explicitTool)
	if readonly && toolName == "readonly_host_inspect" {
		return toolName
	}
	return "execute_command"
}

func approvalWithResolution(approval model.ApprovalRequest, resolution ApprovalResolution) model.ApprovalRequest {
	if id := strings.TrimSpace(resolution.ApprovalID); id != "" {
		approval.ID = id
	}
	if !resolution.RequestedAt.IsZero() {
		approval.RequestedAt = resolution.RequestedAt.Format(time.RFC3339)
	} else if strings.TrimSpace(approval.RequestedAt) == "" {
		approval.RequestedAt = model.NowString()
	}
	if !resolution.ResolvedAt.IsZero() {
		approval.ResolvedAt = resolution.ResolvedAt.Format(time.RFC3339)
	}
	return approval
}

func (a *App) requestCommandApprovalResolution(ctx context.Context, sessionID, toolName string, approval model.ApprovalRequest, allowByPolicy, readonly bool) ApprovalResolution {
	req := buildToolApprovalRequestForExistingApproval(sessionID, toolName, approval, allowByPolicy, readonly)
	fallback := ApprovalResolution{
		ApprovalID:             firstNonEmptyValue(strings.TrimSpace(approval.ID), model.NewID("approval")),
		Status:                 ApprovalResolutionStatusPending,
		SessionID:              sessionID,
		HostID:                 approval.HostID,
		ToolName:               toolName,
		Reason:                 firstNonEmptyValue(strings.TrimSpace(approval.Reason), "manual approval required"),
		RequiresManualApproval: true,
		RequestedAt:            time.Now(),
		Request:                req.Clone(),
		Metadata:               map[string]any{},
	}

	if a == nil || a.toolApprovalCoordinator == nil {
		return fallback
	}

	resolution, err := a.toolApprovalCoordinator.Request(ctx, req)
	if err != nil {
		log.Printf("command approval coordinator request failed session=%s tool=%s approval=%s err=%v", sessionID, toolName, approval.ID, err)
		return fallback
	}
	resolution.Normalize()
	if resolution.ApprovalID == "" {
		resolution.ApprovalID = fallback.ApprovalID
	}
	if resolution.SessionID == "" {
		resolution.SessionID = fallback.SessionID
	}
	if resolution.HostID == "" {
		resolution.HostID = fallback.HostID
	}
	if resolution.ToolName == "" {
		resolution.ToolName = fallback.ToolName
	}
	if resolution.RequestedAt.IsZero() {
		resolution.RequestedAt = fallback.RequestedAt
	}
	if resolution.Request.Invocation.ToolName == "" {
		resolution.Request = req.Clone()
	}
	return resolution
}

func localCommandAutoApprovalPresentation(approval model.ApprovalRequest, ruleName string) (status, title, text, decision string) {
	status, title, text, decision = approvalAutoApprovalPresentation(approval, ruleName)
	if strings.TrimSpace(ruleName) == toolApprovalRuleProfilePolicy {
		status = "accepted_by_profile_auto"
	}
	return status, title, text, decision
}
