package server

import (
	"context"
	"testing"
	"time"
)

func TestToolApprovalCoordinatorAutoApprovesWhenApprovalNotRequired(t *testing.T) {
	coord := NewToolApprovalCoordinator()
	fixedNow := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	coord.now = func() time.Time { return fixedNow }
	coord.nextID = func(prefix string) string { return prefix + "-001" }

	req := ToolApprovalRequest{
		SessionID: "sess-1",
		HostID:    "host-1",
		ToolName:  "read_file",
		Reason:    "inspect config",
		Invocation: ToolInvocation{
			ToolName:         "read_file",
			RequiresApproval: false,
			Arguments: map[string]any{
				"path": "/etc/hosts",
			},
		},
		Metadata: map[string]any{
			"source": "test",
		},
	}

	resolution, err := coord.Request(context.Background(), req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !resolution.IsApproved() {
		t.Fatalf("expected approved resolution, got %#v", resolution)
	}
	if !resolution.AutoApproved {
		t.Fatalf("expected auto approved resolution, got %#v", resolution)
	}
	if resolution.RuleName != "tool_requires_no_approval" {
		t.Fatalf("expected default rule name, got %q", resolution.RuleName)
	}
	if resolution.ApprovalID != "approval-001" {
		t.Fatalf("expected deterministic approval id, got %q", resolution.ApprovalID)
	}
	if resolution.RequestedAt != fixedNow || resolution.ResolvedAt != fixedNow {
		t.Fatalf("expected fixed timestamps, got %#v", resolution)
	}
	if resolution.Request.Metadata["source"] != "test" {
		t.Fatalf("expected request metadata to be preserved, got %#v", resolution.Request.Metadata)
	}

	req.Metadata["source"] = "mutated"
	if resolution.Request.Metadata["source"] != "test" {
		t.Fatalf("expected request clone to be isolated, got %#v", resolution.Request.Metadata)
	}
}

func TestToolApprovalCoordinatorRequestReturnsPendingSkeleton(t *testing.T) {
	coord := NewToolApprovalCoordinator()
	fixedNow := time.Date(2026, 4, 17, 11, 0, 0, 0, time.UTC)
	coord.now = func() time.Time { return fixedNow }
	coord.nextID = func(prefix string) string { return prefix + "-002" }

	resolution, err := coord.Request(context.Background(), ToolApprovalRequest{
		SessionID: "sess-2",
		HostID:    "host-2",
		ToolName:  "write_file",
		Reason:    "manual review needed",
		Invocation: ToolInvocation{
			ToolName:         "write_file",
			RequiresApproval: true,
		},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !resolution.IsPending() {
		t.Fatalf("expected pending resolution, got %#v", resolution)
	}
	if !resolution.RequiresManualApproval {
		t.Fatalf("expected manual approval requirement, got %#v", resolution)
	}
	if resolution.ApprovalID != "approval-002" {
		t.Fatalf("expected deterministic approval id, got %q", resolution.ApprovalID)
	}
	if resolution.RequestedAt != fixedNow {
		t.Fatalf("expected fixed request timestamp, got %v", resolution.RequestedAt)
	}
	if !resolution.ResolvedAt.IsZero() {
		t.Fatalf("expected pending resolution to remain unresolved, got %v", resolution.ResolvedAt)
	}
	if resolution.Reason != "manual review needed" {
		t.Fatalf("expected request reason to be preserved, got %q", resolution.Reason)
	}
}

func TestToolApprovalCoordinatorCustomRuleMatchesFirst(t *testing.T) {
	coord := NewToolApprovalCoordinator(ToolApprovalRuleFunc{
		RuleName: "session-policy",
		Fn: func(_ context.Context, req ToolApprovalRequest) (ApprovalResolution, bool) {
			if req.SessionID != "sess-3" || req.ToolName != "write_file" {
				return ApprovalResolution{}, false
			}
			return ApprovalResolution{
				RuleName: "session-policy",
				Reason:   "session policy allows this tool",
				Metadata: map[string]any{"policy": "allow"},
			}, true
		},
	})
	coord.now = func() time.Time {
		return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	}
	coord.nextID = func(prefix string) string { return prefix + "-003" }

	resolution, ok := coord.AutoApprove(context.Background(), ToolApprovalRequest{
		SessionID: "sess-3",
		HostID:    "host-3",
		ToolName:  "write_file",
		Invocation: ToolInvocation{
			ToolName:         "write_file",
			RequiresApproval: true,
		},
	})
	if !ok {
		t.Fatal("expected rule to match")
	}
	if !resolution.IsApproved() {
		t.Fatalf("expected approved resolution, got %#v", resolution)
	}
	if resolution.RuleName != "session-policy" {
		t.Fatalf("expected rule name to be preserved, got %q", resolution.RuleName)
	}
	if resolution.ApprovalID != "approval-003" {
		t.Fatalf("expected approval id from coordinator, got %q", resolution.ApprovalID)
	}
	if resolution.RequestedAt.IsZero() || resolution.ResolvedAt.IsZero() {
		t.Fatalf("expected timestamps to be set, got %#v", resolution)
	}
	if resolution.Metadata["policy"] != "allow" {
		t.Fatalf("expected rule metadata to be preserved, got %#v", resolution.Metadata)
	}
}

func TestApprovalResolutionCloneCopiesNestedMaps(t *testing.T) {
	resolution := ApprovalResolution{
		ApprovalID: "approval-4",
		Status:     ApprovalResolutionStatusApproved,
		Request: ToolApprovalRequest{
			ToolName: "read_file",
			Metadata: map[string]any{"request": "meta"},
		},
		Metadata: map[string]any{"resolution": "meta"},
	}

	cloned := resolution.Clone()
	cloned.Metadata["resolution"] = "changed"
	cloned.Request.Metadata["request"] = "changed"

	if resolution.Metadata["resolution"] != "meta" {
		t.Fatalf("expected resolution metadata to be isolated, got %#v", resolution.Metadata)
	}
	if resolution.Request.Metadata["request"] != "meta" {
		t.Fatalf("expected request metadata to be isolated, got %#v", resolution.Request.Metadata)
	}
}

func TestApprovalStatusToCardStatusMapsAutoApprovedStatuses(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{status: "accepted_for_host_auto", want: "completed"},
		{status: "accepted_by_policy_auto", want: "completed"},
	}

	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			if got := approvalStatusToCardStatus(tc.status); got != tc.want {
				t.Fatalf("expected %q to map to %q, got %q", tc.status, tc.want, got)
			}
		})
	}
}
