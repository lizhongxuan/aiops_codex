package server

import (
	"testing"
)

func TestToolInvocationNormalizeAndClone(t *testing.T) {
	inv := ToolInvocation{
		InvocationID: "  inv-1  ",
		SessionID:    "  sess-1 ",
		ThreadID:     " thread-1 ",
		TurnID:       " turn-1 ",
		ToolName:     "  read_file ",
		ToolKind:     "  readonly ",
		HostID:       " host-1 ",
		WorkspaceID:  " ws-1 ",
		CallID:       " call-1 ",
		RawArguments: " {\"path\":\"/tmp/a\"} ",
		Arguments: map[string]any{
			"path": "/tmp/a",
		},
	}

	inv.Normalize()

	if inv.InvocationID != "inv-1" || inv.ToolName != "read_file" || inv.RawArguments != "{\"path\":\"/tmp/a\"}" {
		t.Fatalf("normalize trimmed fields incorrectly: %#v", inv)
	}
	if inv.Arguments == nil {
		t.Fatalf("normalize should keep arguments non-nil")
	}

	cloned := inv.Clone()
	cloned.Arguments["path"] = "/tmp/b"
	if inv.Arguments["path"] != "/tmp/a" {
		t.Fatalf("clone should not share argument map: %#v", inv.Arguments)
	}
}

func TestToolExecutionResultClone(t *testing.T) {
	res := ToolExecutionResult{
		InvocationID: "inv-1",
		Status:       ToolRunStatusCompleted,
		OutputData: map[string]any{
			"ok": true,
		},
		EvidenceRefs: []string{"e1", "e2"},
	}

	cloned := res.Clone()
	cloned.OutputData["ok"] = false
	cloned.EvidenceRefs[0] = "changed"

	if got := res.OutputData["ok"]; got != true {
		t.Fatalf("clone should not share output data map: %#v", res.OutputData)
	}
	if got := res.EvidenceRefs[0]; got != "e1" {
		t.Fatalf("clone should not share evidence slice: %#v", res.EvidenceRefs)
	}
}
