package server

import (
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

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

func TestParseExecToolArgsSupportsCommandAndProgramArgs(t *testing.T) {
	t.Run("command", func(t *testing.T) {
		args, err := parseExecToolArgs(map[string]any{
			"command": "uptime",
			"reason":  "check load",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if args.Command != "uptime" {
			t.Fatalf("unexpected command %q", args.Command)
		}
	})

	t.Run("programArgs", func(t *testing.T) {
		args, err := parseExecToolArgs(map[string]any{
			"program": "systemctl",
			"args":    []any{"status", "nginx.service"},
			"reason":  "check service",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if args.Command != "systemctl status nginx.service" {
			t.Fatalf("unexpected command %q", args.Command)
		}
	})
}

func TestValidateReadonlyCommand(t *testing.T) {
	if err := validateReadonlyCommand("systemctl status nginx"); err != nil {
		t.Fatalf("expected status command to pass, got %v", err)
	}
	if err := validateReadonlyCommand("ps -ef | head -20"); err != nil {
		t.Fatalf("expected simple pipeline to pass, got %v", err)
	}
	if err := validateReadonlyCommand("systemctl restart nginx"); err == nil {
		t.Fatalf("expected restart to be rejected")
	}
	if err := validateReadonlyCommand("rm -rf /tmp/demo"); err == nil {
		t.Fatalf("expected rm to be rejected")
	}
	if err := validateReadonlyCommand("docker compose ps"); err != nil {
		t.Fatalf("expected docker compose ps to pass, got %v", err)
	}
	if err := validateReadonlyCommand("docker compose ls"); err != nil {
		t.Fatalf("expected docker compose ls to pass, got %v", err)
	}
	if err := validateReadonlyCommand("docker compose up -d"); err == nil {
		t.Fatalf("expected docker compose up to be rejected")
	}
	if err := validateReadonlyCommand("kubectl --context prod config view"); err != nil {
		t.Fatalf("expected kubectl config view to pass, got %v", err)
	}
	if err := validateReadonlyCommand("kubectl auth can-i get pods"); err != nil {
		t.Fatalf("expected kubectl auth can-i to pass, got %v", err)
	}
	if err := validateReadonlyCommand("kubectl rollout restart deploy/nginx"); err == nil {
		t.Fatalf("expected kubectl rollout restart to be rejected")
	}
	if err := validateReadonlyCommand("git -C /repo config --get remote.origin.url"); err != nil {
		t.Fatalf("expected git config --get to pass, got %v", err)
	}
	if err := validateReadonlyCommand("git checkout main"); err == nil {
		t.Fatalf("expected git checkout to be rejected")
	}
}

func TestExecResultCardStatusPreservesTimeoutAndCancelled(t *testing.T) {
	if got := execResultCardStatus(remoteExecResult{Status: "timeout", ExitCode: 124}); got != "timeout" {
		t.Fatalf("expected timeout, got %q", got)
	}
	if got := execResultCardStatus(remoteExecResult{Status: "cancelled", ExitCode: 130}); got != "cancelled" {
		t.Fatalf("expected cancelled, got %q", got)
	}
	if got := execResultCardStatus(remoteExecResult{Status: "failed", Message: "remote host disconnected"}); got != "disconnected" {
		t.Fatalf("expected disconnected, got %q", got)
	}
	if got := execResultCardStatus(remoteExecResult{Status: "failed", Output: "zsh:1: operation not permitted: ps"}); got != "permission_denied" {
		t.Fatalf("expected permission_denied, got %q", got)
	}
}

func TestParseRemoteFileChangeArgsDefaultsAndAppend(t *testing.T) {
	args, err := parseRemoteFileChangeArgs(map[string]any{
		"mode":    "file_change",
		"path":    "/etc/nginx/nginx.conf",
		"content": "worker_processes auto;\n",
		"reason":  "update config",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args.WriteMode != "overwrite" {
		t.Fatalf("expected default overwrite mode, got %q", args.WriteMode)
	}

	appendArgs, err := parseRemoteFileChangeArgs(map[string]any{
		"mode":       "file_change",
		"path":       "/etc/profile",
		"content":    "\nexport DEMO=1\n",
		"write_mode": "append",
		"reason":     "append env",
	})
	if err != nil {
		t.Fatalf("unexpected append error: %v", err)
	}
	if appendArgs.WriteMode != "append" {
		t.Fatalf("expected append mode, got %q", appendArgs.WriteMode)
	}
}

func TestApprovalMemoTextForRemoteDecisions(t *testing.T) {
	host := model.Host{ID: "linux-01", Name: "linux-01"}
	commandApproval := model.ApprovalRequest{
		Type:    "remote_command",
		HostID:  "linux-01",
		Command: "systemctl restart nginx",
	}
	if got := approvalMemoText(host, commandApproval, "accept"); got != "已同意在 linux-01 执行：systemctl restart nginx" {
		t.Fatalf("unexpected command memo: %q", got)
	}

	fileApproval := model.ApprovalRequest{
		Type:   "remote_file_change",
		HostID: "linux-01",
		Changes: []model.FileChange{
			{Path: "/etc/nginx/nginx.conf"},
		},
	}
	if got := approvalMemoText(host, fileApproval, "accept_session"); got != "已同意并记住在 linux-01 修改文件：/etc/nginx/nginx.conf" {
		t.Fatalf("unexpected file memo: %q", got)
	}
}

func TestBuildFileSearchCardProducesStructuredEntries(t *testing.T) {
	card := buildFileSearchCard("toolmsg-1", "linux-01", &agentrpc.FileSearchResult{
		Path:  "/etc/nginx",
		Query: "server_name",
		Matches: []agentrpc.FileMatch{
			{Path: "/etc/nginx/nginx.conf", Line: 12, Preview: "server_name example.com;"},
		},
	}, "2026-03-25T00:00:00Z")

	if card.Type != "ResultSummaryCard" {
		t.Fatalf("expected ResultSummaryCard, got %q", card.Type)
	}
	if len(card.FileItems) != 1 {
		t.Fatalf("expected 1 file item, got %d", len(card.FileItems))
	}
	if card.FileItems[0].Meta != "第 12 行" {
		t.Fatalf("unexpected meta %q", card.FileItems[0].Meta)
	}
	if card.FileItems[0].Preview != "server_name example.com;" {
		t.Fatalf("unexpected preview %q", card.FileItems[0].Preview)
	}
}

func TestShouldIgnoreTurnPayloadForFailedTurn(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-failed"
	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, "thread-1")
	app.store.SetTurn(sessionID, "turn-1")
	app.finishRuntimeTurn(sessionID, "failed")

	if !app.shouldIgnoreTurnPayload(sessionID, map[string]any{
		"threadId": "thread-1",
		"turnId":   "turn-1",
	}) {
		t.Fatalf("expected failed turn payload to be ignored")
	}
}

func TestFailStalledTurnMarksSessionFailed(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-stalled"
	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, "thread-1")
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)
	app.store.SetTurn(sessionID, "turn-1")

	app.failStalledTurn(sessionID, "turn-1", 45*time.Second)

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if session.Runtime.Turn.Active {
		t.Fatalf("expected stalled turn to become inactive")
	}
	if session.Runtime.Turn.Phase != "failed" {
		t.Fatalf("expected stalled turn phase to be failed, got %q", session.Runtime.Turn.Phase)
	}

	found := false
	for _, card := range session.Cards {
		if card.Type == "ErrorCard" && strings.Contains(card.Message, "45 秒内没有返回任何进展") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected stalled turn error card to be recorded")
	}
}
