package server

import (
	"context"
	"errors"
	"fmt"
	stdstrings "strings"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// ApprovalResolutionStatus captures the outcome of an approval request.
type ApprovalResolutionStatus string

const (
	ApprovalResolutionStatusPending  ApprovalResolutionStatus = "pending"
	ApprovalResolutionStatusApproved ApprovalResolutionStatus = "approved"
	ApprovalResolutionStatusDeclined ApprovalResolutionStatus = "declined"
)

const (
	toolApprovalRuleSessionGrant      = "session_grant"
	toolApprovalRuleHostGrant         = "host_grant"
	toolApprovalRuleProfilePolicy     = "profile_policy"
	toolApprovalMetadataApproval      = "approval"
	toolApprovalMetadataPolicyAllowed = "policyAllowsAutoApprove"
)

// ToolApprovalRequest is the source-agnostic payload used to evaluate approvals.
type ToolApprovalRequest struct {
	SessionID string
	HostID    string
	ToolName  string
	Reason    string

	Invocation ToolInvocation
	Metadata   map[string]any
}

// Clone returns a shallow copy with copied map fields.
func (r ToolApprovalRequest) Clone() ToolApprovalRequest {
	r.Invocation = r.Invocation.Clone()
	if r.Metadata != nil {
		meta := make(map[string]any, len(r.Metadata))
		for k, v := range r.Metadata {
			meta[k] = v
		}
		r.Metadata = meta
	}
	return r
}

// Normalize trims stable string fields and guarantees a non-nil metadata map.
func (r *ToolApprovalRequest) Normalize() {
	if r == nil {
		return
	}
	r.SessionID = stdstrings.TrimSpace(r.SessionID)
	r.HostID = stdstrings.TrimSpace(r.HostID)
	r.ToolName = stdstrings.TrimSpace(r.ToolName)
	r.Reason = stdstrings.TrimSpace(r.Reason)
	r.Invocation.Normalize()
	if r.Metadata == nil {
		r.Metadata = make(map[string]any)
	}
}

// ApprovalResolution summarizes the outcome of an approval decision.
type ApprovalResolution struct {
	ApprovalID   string
	Status       ApprovalResolutionStatus
	RuleName     string
	Reason       string
	AutoApproved bool

	SessionID              string
	HostID                 string
	ToolName               string
	RequiresManualApproval bool
	RequestedAt            time.Time
	ResolvedAt             time.Time

	Request  ToolApprovalRequest
	Metadata map[string]any
}

// Clone returns a shallow copy with copied map fields and nested request.
func (r ApprovalResolution) Clone() ApprovalResolution {
	r.Request = r.Request.Clone()
	if r.Metadata != nil {
		meta := make(map[string]any, len(r.Metadata))
		for k, v := range r.Metadata {
			meta[k] = v
		}
		r.Metadata = meta
	}
	return r
}

// Normalize trims stable fields and ensures maps exist.
func (r *ApprovalResolution) Normalize() {
	if r == nil {
		return
	}
	r.ApprovalID = stdstrings.TrimSpace(r.ApprovalID)
	r.RuleName = stdstrings.TrimSpace(r.RuleName)
	r.Reason = stdstrings.TrimSpace(r.Reason)
	r.SessionID = stdstrings.TrimSpace(r.SessionID)
	r.HostID = stdstrings.TrimSpace(r.HostID)
	r.ToolName = stdstrings.TrimSpace(r.ToolName)
	r.Request.Normalize()
	if r.Metadata == nil {
		r.Metadata = make(map[string]any)
	}
}

// IsApproved reports whether the resolution is approved.
func (r ApprovalResolution) IsApproved() bool {
	return r.Status == ApprovalResolutionStatusApproved
}

// IsPending reports whether the resolution still requires manual action.
func (r ApprovalResolution) IsPending() bool {
	return r.Status == ApprovalResolutionStatusPending
}

// ToolApprovalRule evaluates a request and optionally returns an auto-approved resolution.
type ToolApprovalRule interface {
	Name() string
	Evaluate(context.Context, ToolApprovalRequest) (ApprovalResolution, bool)
}

// ToolApprovalRuleFunc adapts a function into a ToolApprovalRule.
type ToolApprovalRuleFunc struct {
	RuleName string
	Fn       func(context.Context, ToolApprovalRequest) (ApprovalResolution, bool)
}

// Name returns the configured rule name.
func (r ToolApprovalRuleFunc) Name() string {
	return stdstrings.TrimSpace(r.RuleName)
}

// Evaluate invokes the wrapped rule function.
func (r ToolApprovalRuleFunc) Evaluate(ctx context.Context, req ToolApprovalRequest) (ApprovalResolution, bool) {
	if r.Fn == nil {
		return ApprovalResolution{}, false
	}
	return r.Fn(ctx, req)
}

// ToolApprovalCoordinator evaluates auto-approve rules and produces approval requests.
type ToolApprovalCoordinator interface {
	AutoApprove(context.Context, ToolApprovalRequest) (ApprovalResolution, bool)
	Request(context.Context, ToolApprovalRequest) (ApprovalResolution, error)
}

// DefaultToolApprovalCoordinator is a minimal in-process implementation.
type DefaultToolApprovalCoordinator struct {
	mu     sync.RWMutex
	rules  []ToolApprovalRule
	now    func() time.Time
	nextID func(prefix string) string
}

// NewToolApprovalCoordinator creates a default coordinator with optional rules.
func NewToolApprovalCoordinator(rules ...ToolApprovalRule) *DefaultToolApprovalCoordinator {
	return &DefaultToolApprovalCoordinator{
		rules:  append([]ToolApprovalRule(nil), rules...),
		now:    time.Now,
		nextID: func(prefix string) string { return model.NewID(prefix) },
	}
}

func (a *App) registerDefaultToolApprovalRules() {
	if a == nil {
		return
	}
	coord, ok := a.toolApprovalCoordinator.(*DefaultToolApprovalCoordinator)
	if !ok || coord == nil {
		return
	}
	coord.AddRule(ToolApprovalRuleFunc{
		RuleName: toolApprovalRuleSessionGrant,
		Fn:       a.evaluateSessionGrantToolApproval,
	})
	coord.AddRule(ToolApprovalRuleFunc{
		RuleName: toolApprovalRuleHostGrant,
		Fn:       a.evaluateHostGrantToolApproval,
	})
	coord.AddRule(ToolApprovalRuleFunc{
		RuleName: toolApprovalRuleProfilePolicy,
		Fn:       a.evaluatePolicyToolApproval,
	})
}

// AddRule registers an approval rule after construction.
func (c *DefaultToolApprovalCoordinator) AddRule(rule ToolApprovalRule) {
	if c == nil || rule == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules = append(c.rules, rule)
}

// AutoApprove evaluates the default rule chain and returns the first matching resolution.
func (c *DefaultToolApprovalCoordinator) AutoApprove(ctx context.Context, req ToolApprovalRequest) (ApprovalResolution, bool) {
	if c == nil {
		return ApprovalResolution{}, false
	}

	req.Normalize()
	if req.ToolName == "" {
		return ApprovalResolution{}, false
	}

	if !req.Invocation.RequiresApproval {
		return c.autoApprovedResolution(req, "tool_requires_no_approval", "tool does not require approval"), true
	}

	c.mu.RLock()
	rules := append([]ToolApprovalRule(nil), c.rules...)
	c.mu.RUnlock()

	for _, rule := range rules {
		if rule == nil {
			continue
		}
		resolution, ok := rule.Evaluate(ctx, req)
		if !ok {
			continue
		}
		resolution.Normalize()
		if resolution.Status == "" {
			resolution.Status = ApprovalResolutionStatusApproved
		}
		if resolution.ApprovalID == "" {
			resolution.ApprovalID = c.newID("approval")
		}
		resolution.AutoApproved = true
		resolution.Request = req.Clone()
		if resolution.RequestedAt.IsZero() {
			resolution.RequestedAt = c.nowTime()
		}
		if resolution.ResolvedAt.IsZero() {
			resolution.ResolvedAt = resolution.RequestedAt
		}
		if resolution.SessionID == "" {
			resolution.SessionID = req.SessionID
		}
		if resolution.HostID == "" {
			resolution.HostID = req.HostID
		}
		if resolution.ToolName == "" {
			resolution.ToolName = req.ToolName
		}
		if resolution.Reason == "" {
			resolution.Reason = "auto-approved by rule"
		}
		return resolution, true
	}

	return ApprovalResolution{}, false
}

// Request produces either an auto-approved resolution or a pending approval skeleton.
func (c *DefaultToolApprovalCoordinator) Request(ctx context.Context, req ToolApprovalRequest) (ApprovalResolution, error) {
	if c == nil {
		return ApprovalResolution{}, errors.New("approval coordinator is nil")
	}

	req.Normalize()
	if req.ToolName == "" {
		return ApprovalResolution{}, fmt.Errorf("tool approval request tool name is required")
	}

	if resolution, ok := c.AutoApprove(ctx, req); ok {
		return resolution, nil
	}

	now := c.nowTime()
	resolution := ApprovalResolution{
		ApprovalID:             c.newID("approval"),
		Status:                 ApprovalResolutionStatusPending,
		SessionID:              req.SessionID,
		HostID:                 req.HostID,
		ToolName:               req.ToolName,
		Reason:                 firstNonEmptyValue(req.Reason, "manual approval required"),
		RequiresManualApproval: true,
		RequestedAt:            now,
		Request:                req.Clone(),
		Metadata:               map[string]any{},
	}
	resolution.Normalize()
	resolution.RequestedAt = now
	return resolution, nil
}

func (c *DefaultToolApprovalCoordinator) autoApprovedResolution(req ToolApprovalRequest, ruleName, reason string) ApprovalResolution {
	now := c.nowTime()
	return ApprovalResolution{
		ApprovalID:   c.newID("approval"),
		Status:       ApprovalResolutionStatusApproved,
		RuleName:     ruleName,
		Reason:       reason,
		AutoApproved: true,
		SessionID:    req.SessionID,
		HostID:       req.HostID,
		ToolName:     req.ToolName,
		RequestedAt:  now,
		ResolvedAt:   now,
		Request:      req.Clone(),
		Metadata:     map[string]any{},
	}
}

func (c *DefaultToolApprovalCoordinator) nowTime() time.Time {
	if c != nil && c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *DefaultToolApprovalCoordinator) newID(prefix string) string {
	if c != nil && c.nextID != nil {
		return c.nextID(prefix)
	}
	return model.NewID(prefix)
}

func buildToolApprovalRequestForExistingApproval(sessionID, toolName string, approval model.ApprovalRequest, allowByPolicy, readonly bool) ToolApprovalRequest {
	toolName = firstNonEmptyValue(stdstrings.TrimSpace(toolName), stdstrings.TrimSpace(approval.Type))
	arguments := make(map[string]any, 4)
	if command := stdstrings.TrimSpace(approval.Command); command != "" {
		arguments["command"] = command
	}
	if cwd := stdstrings.TrimSpace(approval.Cwd); cwd != "" {
		arguments["cwd"] = cwd
	}
	if grantRoot := stdstrings.TrimSpace(approval.GrantRoot); grantRoot != "" {
		arguments["grantRoot"] = grantRoot
	}
	if len(approval.Changes) > 0 {
		arguments["changes"] = append([]model.FileChange(nil), approval.Changes...)
	}
	return ToolApprovalRequest{
		SessionID: sessionID,
		HostID:    approval.HostID,
		ToolName:  toolName,
		Reason:    approval.Reason,
		Invocation: ToolInvocation{
			SessionID:        sessionID,
			HostID:           approval.HostID,
			ToolName:         toolName,
			RequiresApproval: true,
			ReadOnly:         readonly,
			Arguments:        arguments,
		},
		Metadata: map[string]any{
			toolApprovalMetadataApproval:      approval,
			toolApprovalMetadataPolicyAllowed: allowByPolicy,
		},
	}
}

func (a *App) matchToolApprovalRule(ctx context.Context, req ToolApprovalRequest, ruleName string) (ApprovalResolution, bool) {
	if a == nil || a.toolApprovalCoordinator == nil {
		return ApprovalResolution{}, false
	}
	resolution, ok := a.toolApprovalCoordinator.AutoApprove(ctx, req)
	if !ok {
		return ApprovalResolution{}, false
	}
	if expected := stdstrings.TrimSpace(ruleName); expected != "" && resolution.RuleName != expected {
		return ApprovalResolution{}, false
	}
	return resolution, true
}

func (a *App) evaluateSessionGrantToolApproval(_ context.Context, req ToolApprovalRequest) (ApprovalResolution, bool) {
	if a == nil {
		return ApprovalResolution{}, false
	}
	approval := approvalRequestFromToolApprovalRequest(req)
	if approval.Fingerprint == "" {
		return ApprovalResolution{}, false
	}
	if _, ok := a.store.ApprovalGrant(req.SessionID, approval.Fingerprint); !ok {
		return ApprovalResolution{}, false
	}
	return ApprovalResolution{
		RuleName: toolApprovalRuleSessionGrant,
		Reason:   "matched session approval grant",
	}, true
}

func (a *App) evaluateHostGrantToolApproval(_ context.Context, req ToolApprovalRequest) (ApprovalResolution, bool) {
	if a == nil {
		return ApprovalResolution{}, false
	}
	approval := approvalRequestFromToolApprovalRequest(req)
	if approval.Fingerprint == "" || approval.HostID == "" || a.approvalGrantStore == nil {
		return ApprovalResolution{}, false
	}
	if _, ok := a.approvalGrantStore.MatchFingerprint(approval.HostID, approval.Fingerprint); !ok {
		return ApprovalResolution{}, false
	}
	return ApprovalResolution{
		RuleName: toolApprovalRuleHostGrant,
		Reason:   "matched host approval grant",
	}, true
}

func (a *App) evaluatePolicyToolApproval(_ context.Context, req ToolApprovalRequest) (ApprovalResolution, bool) {
	if !toolApprovalPolicyAllowsAutoApprove(req) {
		return ApprovalResolution{}, false
	}
	return ApprovalResolution{
		RuleName: toolApprovalRuleProfilePolicy,
		Reason:   "effective profile allows direct execution",
	}, true
}

func approvalRequestFromToolApprovalRequest(req ToolApprovalRequest) model.ApprovalRequest {
	if raw, ok := req.Metadata[toolApprovalMetadataApproval]; ok {
		switch typed := raw.(type) {
		case model.ApprovalRequest:
			return typed
		case *model.ApprovalRequest:
			if typed != nil {
				return *typed
			}
		}
	}

	approval := model.ApprovalRequest{
		HostID: req.HostID,
		Reason: req.Reason,
		Type:   req.ToolName,
	}
	if command, _ := req.Invocation.Arguments["command"].(string); command != "" {
		approval.Command = command
	}
	if cwd, _ := req.Invocation.Arguments["cwd"].(string); cwd != "" {
		approval.Cwd = cwd
	}
	if grantRoot, _ := req.Invocation.Arguments["grantRoot"].(string); grantRoot != "" {
		approval.GrantRoot = grantRoot
	}
	if changes, ok := req.Invocation.Arguments["changes"].([]model.FileChange); ok {
		approval.Changes = append([]model.FileChange(nil), changes...)
	}
	return approval
}

func toolApprovalPolicyAllowsAutoApprove(req ToolApprovalRequest) bool {
	raw, ok := req.Metadata[toolApprovalMetadataPolicyAllowed]
	if !ok {
		return false
	}
	allowed, _ := raw.(bool)
	return allowed
}
