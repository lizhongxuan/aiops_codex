package server

import "testing"

func TestToolDisplayPayloadCloneCopiesNestedFields(t *testing.T) {
	payload := ToolDisplayPayload{
		Summary:  "Reading file",
		Activity: "Reading /tmp/a.txt",
		Blocks: []ToolDisplayBlock{
			{
				Kind:  ToolDisplayBlockFilePreview,
				Title: "Preview",
				Text:  "hello",
				Items: []map[string]any{
					{"path": "/tmp/a.txt"},
				},
				Metadata: map[string]any{
					"lines": 10,
				},
			},
		},
		FinalCard: &ToolFinalCardDescriptor{
			CardType: "ResultCard",
			Title:    "Done",
			Text:     "Read ok",
			Detail: map[string]any{
				"path": "/tmp/a.txt",
			},
		},
		Metadata: map[string]any{
			"phase": "completed",
		},
	}

	cloned := payload.Clone()
	cloned.Blocks[0].Items[0]["path"] = "/tmp/b.txt"
	cloned.Blocks[0].Metadata["lines"] = 20
	cloned.FinalCard.Detail["path"] = "/tmp/b.txt"
	cloned.Metadata["phase"] = "running"

	if got := payload.Blocks[0].Items[0]["path"]; got != "/tmp/a.txt" {
		t.Fatalf("expected original block item to remain unchanged, got %#v", got)
	}
	if got := payload.Blocks[0].Metadata["lines"]; got != 10 {
		t.Fatalf("expected original block metadata to remain unchanged, got %#v", got)
	}
	if got := payload.FinalCard.Detail["path"]; got != "/tmp/a.txt" {
		t.Fatalf("expected original final card detail to remain unchanged, got %#v", got)
	}
	if got := payload.Metadata["phase"]; got != "completed" {
		t.Fatalf("expected original payload metadata to remain unchanged, got %#v", got)
	}
}

func TestToolDisplayBlockKindsRemainStable(t *testing.T) {
	kinds := []string{
		ToolDisplayBlockText,
		ToolDisplayBlockKVList,
		ToolDisplayBlockCommand,
		ToolDisplayBlockFilePreview,
		ToolDisplayBlockFileDiffSummary,
		ToolDisplayBlockSearchQueries,
		ToolDisplayBlockLinkList,
		ToolDisplayBlockResultStats,
		ToolDisplayBlockWarning,
	}

	want := []string{
		"text",
		"kv_list",
		"command",
		"file_preview",
		"file_diff_summary",
		"search_queries",
		"link_list",
		"result_stats",
		"warning",
	}

	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("expected display kind %d to be %q, got %q", i, want[i], kinds[i])
		}
	}
}
