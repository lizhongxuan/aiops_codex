package server

import "testing"

func TestCompletedCommandStatusTreatsShellErrorsAsFailed(t *testing.T) {
	item := map[string]any{
		"status":   "completed",
		"exitCode": 0,
	}
	output := "zsh:1: operation not permitted: ps"

	if got := completedCommandStatus(item, output); got != "failed" {
		t.Fatalf("expected failed, got %q", got)
	}
}

func TestCompletedCommandStatusUsesExitCodeAndNormalSuccess(t *testing.T) {
	failedItem := map[string]any{
		"status":   "completed",
		"exitCode": 1,
	}
	if got := completedCommandStatus(failedItem, ""); got != "failed" {
		t.Fatalf("expected non-zero exit code to fail, got %q", got)
	}

	completedItem := map[string]any{
		"status":   "completed",
		"exitCode": 0,
	}
	if got := completedCommandStatus(completedItem, "load averages: 1.23 1.11 1.05"); got != "completed" {
		t.Fatalf("expected successful output to stay completed, got %q", got)
	}
}

func TestDetectActivitySignalForWebSearchThreadItem(t *testing.T) {
	item := map[string]any{
		"type":  "webSearch",
		"query": "2026-03-25 A股 主要指数",
		"action": map[string]any{
			"type":  "search",
			"query": "2026-03-25 A股 主要指数",
		},
	}

	kind, entry, currentLabel, ok := detectActivitySignal(item)
	if !ok {
		t.Fatalf("expected web search signal to be detected")
	}
	if kind != "web_search" {
		t.Fatalf("expected web_search kind, got %q", kind)
	}
	if currentLabel != "2026-03-25 A股 主要指数" {
		t.Fatalf("unexpected currentLabel %q", currentLabel)
	}
	if entry.Query != "2026-03-25 A股 主要指数" {
		t.Fatalf("unexpected entry.Query %q", entry.Query)
	}
}

func TestDetectActivitySignalForWebOpenPageThreadItem(t *testing.T) {
	item := map[string]any{
		"type": "webSearch",
		"action": map[string]any{
			"type": "openPage",
			"url":  "https://finance.example.com/market/a-share",
		},
	}

	kind, _, currentLabel, ok := detectActivitySignal(item)
	if !ok {
		t.Fatalf("expected open page signal to be detected")
	}
	if kind != "web_open" {
		t.Fatalf("expected web_open kind, got %q", kind)
	}
	if currentLabel != "finance.example.com/market/a-share" {
		t.Fatalf("unexpected currentLabel %q", currentLabel)
	}
}
