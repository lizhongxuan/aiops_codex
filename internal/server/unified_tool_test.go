package server

import "testing"

func TestToolCallRequestCloneCopiesContainers(t *testing.T) {
	req := ToolCallRequest{
		Invocation: ToolInvocation{
			InvocationID: "inv-1",
			SessionID:    "session-1",
			ToolName:     "web_search",
			Arguments: map[string]any{
				"query": "btc price",
			},
		},
		Input: map[string]any{
			"query": "btc price",
		},
		Metadata: map[string]any{
			"source": "test",
		},
	}

	cloned := req.Clone()
	cloned.Invocation.Arguments["query"] = "eth price"
	cloned.Input["query"] = "eth price"
	cloned.Metadata["source"] = "changed"

	if got := req.Invocation.Arguments["query"]; got != "btc price" {
		t.Fatalf("expected original invocation args to remain unchanged, got %#v", got)
	}
	if got := req.Input["query"]; got != "btc price" {
		t.Fatalf("expected original input to remain unchanged, got %#v", got)
	}
	if got := req.Metadata["source"]; got != "test" {
		t.Fatalf("expected original metadata to remain unchanged, got %#v", got)
	}
}

func TestToolCallRequestNormalizeDefaultsInputFromInvocation(t *testing.T) {
	req := ToolCallRequest{
		Invocation: ToolInvocation{
			InvocationID: " inv-1 ",
			SessionID:    " session-1 ",
			ToolName:     " web_search ",
			Arguments: map[string]any{
				"query": "btc price",
			},
		},
	}

	req.Normalize()

	if req.Invocation.InvocationID != "inv-1" || req.Invocation.SessionID != "session-1" || req.Invocation.ToolName != "web_search" {
		t.Fatalf("expected normalized invocation fields, got %#v", req.Invocation)
	}
	if got := req.Input["query"]; got != "btc price" {
		t.Fatalf("expected input to default from invocation arguments, got %#v", got)
	}
}

func TestPermissionResultCloneCopiesSlicesAndMaps(t *testing.T) {
	result := PermissionResult{
		Allowed:           false,
		RequiresApproval:  true,
		ApprovalType:      "command",
		ApprovalDecisions: []string{"approve_once"},
		Metadata: map[string]any{
			"reason": "needs approval",
		},
	}

	cloned := result.Clone()
	cloned.ApprovalDecisions[0] = "deny"
	cloned.Metadata["reason"] = "changed"

	if got := result.ApprovalDecisions[0]; got != "approve_once" {
		t.Fatalf("expected original approval decision to remain unchanged, got %#v", got)
	}
	if got := result.Metadata["reason"]; got != "needs approval" {
		t.Fatalf("expected original metadata to remain unchanged, got %#v", got)
	}
}

func TestToolCallResultCloneCopiesNestedDisplayPayload(t *testing.T) {
	result := ToolCallResult{
		Output: "done",
		DisplayOutput: &ToolDisplayPayload{
			Summary: "Did 1 search",
			Blocks: []ToolDisplayBlock{
				{
					Kind: ToolDisplayBlockSearchQueries,
					Items: []map[string]any{
						{"query": "btc price"},
					},
				},
			},
			Metadata: map[string]any{
				"phase": "completed",
			},
		},
		StructuredContent: map[string]any{
			"count": 1,
		},
		Metadata: map[string]any{
			"source": "test",
		},
	}

	cloned := result.Clone()
	cloned.DisplayOutput.Summary = "Changed"
	cloned.DisplayOutput.Blocks[0].Items[0]["query"] = "eth price"
	cloned.StructuredContent["count"] = 2
	cloned.Metadata["source"] = "changed"

	if result.DisplayOutput.Summary != "Did 1 search" {
		t.Fatalf("expected original display summary to remain unchanged, got %#v", result.DisplayOutput.Summary)
	}
	if got := result.DisplayOutput.Blocks[0].Items[0]["query"]; got != "btc price" {
		t.Fatalf("expected original display block item to remain unchanged, got %#v", got)
	}
	if got := result.StructuredContent["count"]; got != 1 {
		t.Fatalf("expected original structured content to remain unchanged, got %#v", got)
	}
	if got := result.Metadata["source"]; got != "test" {
		t.Fatalf("expected original metadata to remain unchanged, got %#v", got)
	}
}
