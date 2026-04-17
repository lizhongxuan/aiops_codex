package server

import (
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
)

func TestBuildFileListCardProducesStructuredCounts(t *testing.T) {
	card := buildFileListCardWithRecursive("toolmsg-1", "linux-01", &agentrpc.FileListResult{
		Path: "/etc/nginx",
		Entries: []agentrpc.FileEntry{
			{Name: "nginx.conf", Path: "/etc/nginx/nginx.conf", Kind: "file", Size: 2048},
			{Name: "conf.d", Path: "/etc/nginx/conf.d", Kind: "dir"},
		},
		Truncated: true,
	}, true, "2026-03-25T00:00:00Z")

	if card.Type != "ResultSummaryCard" {
		t.Fatalf("expected ResultSummaryCard, got %q", card.Type)
	}
	if got, ok := card.Detail["cancelable"].(bool); !ok || got {
		t.Fatalf("expected file list card cancelable=false, got %#v", card.Detail)
	}
	if card.Summary == "" || card.Text == "" {
		t.Fatalf("expected structured summary and note, got summary=%q text=%q", card.Summary, card.Text)
	}
	if len(card.KVRows) < 5 {
		t.Fatalf("expected structured kv rows, got %#v", card.KVRows)
	}
	if len(card.FileItems) != 2 {
		t.Fatalf("expected 2 file items, got %d", len(card.FileItems))
	}
	if len(card.Highlights) == 0 {
		t.Fatalf("expected highlights to be populated")
	}
}

func TestBuildFileReadCardProducesStructuredPreview(t *testing.T) {
	card := buildFileReadCard("toolpreview-1", "linux-01", &agentrpc.FileReadResult{
		Path:      "/etc/nginx/nginx.conf",
		Content:   "user nginx;\nworker_processes auto;\nserver_name example.com;\n",
		Truncated: true,
	}, "2026-03-25T00:00:00Z")

	if card.Type != "ResultSummaryCard" {
		t.Fatalf("expected ResultSummaryCard, got %q", card.Type)
	}
	if got, ok := card.Detail["cancelable"].(bool); !ok || got {
		t.Fatalf("expected file read card cancelable=false, got %#v", card.Detail)
	}
	if len(card.KVRows) < 4 {
		t.Fatalf("expected structured kv rows, got %#v", card.KVRows)
	}
	if len(card.FileItems) != 1 {
		t.Fatalf("expected one file item, got %d", len(card.FileItems))
	}
	if card.FileItems[0].Preview == "" {
		t.Fatalf("expected file preview to be populated")
	}
	if len(card.Highlights) == 0 {
		t.Fatalf("expected highlights to be populated")
	}
	if card.Text == "" {
		t.Fatalf("expected truncation note to be populated")
	}
}

func TestBuildFileSearchCardProducesStructuredMatches(t *testing.T) {
	card := buildFileSearchCard("toolmsg-2", "linux-01", &agentrpc.FileSearchResult{
		Path:  "/etc/nginx",
		Query: "server_name",
		Matches: []agentrpc.FileMatch{
			{Path: "/etc/nginx/nginx.conf", Line: 12, Preview: "server_name example.com;"},
			{Path: "/etc/nginx/conf.d/app.conf", Line: 7, Preview: "server_name app.example.com;"},
		},
		Truncated: true,
	}, "2026-03-25T00:00:00Z")

	if card.Type != "ResultSummaryCard" {
		t.Fatalf("expected ResultSummaryCard, got %q", card.Type)
	}
	if got, ok := card.Detail["cancelable"].(bool); !ok || got {
		t.Fatalf("expected file search card cancelable=false, got %#v", card.Detail)
	}
	if len(card.KVRows) < 6 {
		t.Fatalf("expected structured kv rows, got %#v", card.KVRows)
	}
	if len(card.FileItems) != 2 {
		t.Fatalf("expected 2 file items, got %d", len(card.FileItems))
	}
	if card.FileItems[0].Kind != "match" {
		t.Fatalf("expected match kind, got %q", card.FileItems[0].Kind)
	}
	if card.FileItems[0].Meta != "第 12 行" {
		t.Fatalf("unexpected meta %q", card.FileItems[0].Meta)
	}
	if card.FileItems[0].Preview == "" {
		t.Fatalf("expected match meta and preview to be populated")
	}
	if len(card.Highlights) == 0 {
		t.Fatalf("expected highlights to be populated")
	}
	if card.Text == "" {
		t.Fatalf("expected truncation note to be populated")
	}
}
