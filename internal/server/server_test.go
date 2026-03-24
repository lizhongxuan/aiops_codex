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
