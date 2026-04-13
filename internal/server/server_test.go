package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/store"
	"google.golang.org/grpc/metadata"
)

type fakeAgentConnectServer struct {
	mu       sync.Mutex
	messages []*agentrpc.Envelope
}

func (f *fakeAgentConnectServer) SetHeader(metadata.MD) error {
	return nil
}

func (f *fakeAgentConnectServer) SendHeader(metadata.MD) error {
	return nil
}

func (f *fakeAgentConnectServer) SetTrailer(metadata.MD) {}

func (f *fakeAgentConnectServer) Context() context.Context {
	return context.Background()
}

func (f *fakeAgentConnectServer) Send(msg *agentrpc.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeAgentConnectServer) Recv() (*agentrpc.Envelope, error) {
	return nil, io.EOF
}

func (f *fakeAgentConnectServer) SendMsg(any) error {
	return nil
}

func (f *fakeAgentConnectServer) RecvMsg(any) error {
	return io.EOF
}

func (f *fakeAgentConnectServer) snapshotMessages() []*agentrpc.Envelope {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*agentrpc.Envelope(nil), f.messages...)
}

func (f *fakeAgentConnectServer) waitForKind(t *testing.T, kind string, timeout time.Duration) *agentrpc.Envelope {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, msg := range f.snapshotMessages() {
			if msg.Kind == kind {
				return msg
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for envelope kind %q", kind)
	return nil
}

func readAuditRecords(t *testing.T, path string) []map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode audit record: %v", err)
		}
		records = append(records, record)
	}
	return records
}

func findAuditRecord(records []map[string]any, event string) map[string]any {
	for _, record := range records {
		if record["event"] == event {
			return record
		}
	}
	return nil
}

func mustAuditString(t *testing.T, record map[string]any, key string) string {
	t.Helper()
	value, ok := record[key]
	if !ok {
		t.Fatalf("expected audit field %s", key)
	}
	str, ok := value.(string)
	if !ok {
		t.Fatalf("expected audit field %s to be string, got %#v", key, value)
	}
	return str
}

func mustAuditNil(t *testing.T, record map[string]any, key string) {
	t.Helper()
	value, ok := record[key]
	if !ok {
		t.Fatalf("expected audit field %s", key)
	}
	if value != nil {
		t.Fatalf("expected audit field %s to be nil, got %#v", key, value)
	}
}

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

func TestSetRuntimeTurnPhaseNormalizesAndSkipsDuplicateUpdates(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-turn-phase-normalize"

	app.store.EnsureSession(sessionID)
	app.setRuntimeTurnPhase(sessionID, "unknown-phase")

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected unknown phase to normalize to thinking, got %q", session.Runtime.Turn.Phase)
	}
	firstActivityAt := session.LastActivityAt

	time.Sleep(1100 * time.Millisecond)
	app.setRuntimeTurnPhase(sessionID, "thinking")

	session = app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected duplicate thinking phase to stay thinking, got %q", session.Runtime.Turn.Phase)
	}
	if session.LastActivityAt != firstActivityAt {
		t.Fatalf("expected duplicate phase update to be ignored, got lastActivityAt %q -> %q", firstActivityAt, session.LastActivityAt)
	}
}

func TestClearRemoteHostUnavailableCardsRemovesStaleError(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-remote-host-recovery"
	hostID := "host-a"
	cardID := "remote-host-error-" + hostID

	app.store.EnsureSession(sessionID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "ErrorCard",
		Title:     "远程主机连接超时",
		Message:   "test",
		Status:    "failed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})

	app.clearRemoteHostUnavailableCards(hostID)

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	for _, card := range session.Cards {
		if card.ID == cardID {
			t.Fatalf("expected stale remote host error card to be removed")
		}
	}
}

func TestScheduleSilentTurnCompletionCheckCompletesFinalizingTurn(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-silent-finalizing"

	app.store.EnsureSession(sessionID)
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)
	app.setRuntimeTurnPhase(sessionID, "finalizing")
	app.store.SetTurn(sessionID, "turn-silent-finalizing")
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "assistant-final",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "done",
		Status:    "completed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})

	time.Sleep(25 * time.Millisecond)
	app.scheduleSilentTurnCompletionCheck(sessionID, 20*time.Millisecond)

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		session := app.store.Session(sessionID)
		if session != nil && !session.Runtime.Turn.Active && session.Runtime.Turn.Phase == "completed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	t.Fatalf("expected finalizing turn to auto-complete, got active=%t phase=%q", session.Runtime.Turn.Active, session.Runtime.Turn.Phase)
}

func TestTurnTraceLifecycleLogsStageSummary(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-turn-trace"

	app.store.EnsureSession(sessionID)

	var buf strings.Builder
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	}()

	app.beginTurnTraceRequest(sessionID, "host-a")
	app.startRuntimeTurn(sessionID, "host-a")
	app.markTurnTraceThreadStartBegin(sessionID)
	app.markTurnTraceThreadStarted(sessionID, "thread-1")
	app.markTurnTraceTurnStartBegin(sessionID, "thread-1")
	app.markTurnTraceTurnStarted(sessionID, "thread-1", "turn-1")
	app.markTurnTraceFirstItem(sessionID, "item-1", "commandExecution")
	app.markTurnTraceFirstAssistant(sessionID, "item-2", "delta")
	app.completeTurnTrace(sessionID, "completed")

	output := buf.String()
	if !strings.Contains(output, "turn first progress session=sess-turn-trace") {
		t.Fatalf("expected first progress log, got %q", output)
	}
	if !strings.Contains(output, "turn first assistant session=sess-turn-trace") {
		t.Fatalf("expected first assistant log, got %q", output)
	}
	if !strings.Contains(output, "turn trace complete session=sess-turn-trace") {
		t.Fatalf("expected final trace summary log, got %q", output)
	}
	if !strings.Contains(output, "request=req-") {
		t.Fatalf("expected request id in trace logs, got %q", output)
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
	if err := validateReadonlyCommand("journalctl -u nginx -n 50"); err != nil {
		t.Fatalf("expected journalctl logs to pass, got %v", err)
	}
	if err := validateReadonlyCommand("grep -R \"server_name\" /etc/nginx"); err != nil {
		t.Fatalf("expected grep search to pass, got %v", err)
	}
	if err := validateReadonlyCommand("tail -n 100 /var/log/syslog"); err != nil {
		t.Fatalf("expected tail to pass, got %v", err)
	}
	if err := validateReadonlyCommand("find /var/log -type f -name '*.log'"); err != nil {
		t.Fatalf("expected read-only find to pass, got %v", err)
	}
	if err := validateReadonlyCommand("ps -ef | head -20"); err != nil {
		t.Fatalf("expected simple pipeline to pass, got %v", err)
	}
	if err := validateReadonlyCommand("pgrep -lf '[p]ostgres|[p]ostmaster'"); err != nil {
		t.Fatalf("expected quoted regex alternation to pass, got %v", err)
	}
	if err := validateReadonlyCommand("ps aux | grep -E '[p]ostgres|[p]ostmaster'"); err != nil {
		t.Fatalf("expected quoted regex alternation in pipeline to pass, got %v", err)
	}
	if err := validateReadonlyCommand("command -v psql"); err != nil {
		t.Fatalf("expected command -v lookup to pass, got %v", err)
	}
	if err := validateReadonlyCommand("lsof -nP -iTCP:5432 -sTCP:LISTEN"); err != nil {
		t.Fatalf("expected lsof read-only inspection to pass, got %v", err)
	}
	if err := validateReadonlyCommand("launchctl list | grep -i postgres"); err != nil {
		t.Fatalf("expected launchctl list to pass, got %v", err)
	}
	if err := validateReadonlyCommand("brew services list | grep -i postgres"); err != nil {
		t.Fatalf("expected brew services list to pass, got %v", err)
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
	if err := validateReadonlyCommand("journalctl --vacuum-time=1d"); err == nil {
		t.Fatalf("expected journalctl vacuum to be rejected")
	}
	if err := validateReadonlyCommand("find /tmp -delete"); err == nil {
		t.Fatalf("expected find -delete to be rejected")
	}
	if err := validateReadonlyCommand("sed -i 's/old/new/' /tmp/demo"); err == nil {
		t.Fatalf("expected sed -i to be rejected")
	}
	if err := validateReadonlyCommand("hostname prod-web-01"); err == nil {
		t.Fatalf("expected hostname change to be rejected")
	}
}

func TestExecResultCardStatusPreservesTimeoutAndCancelled(t *testing.T) {
	if got := execResultCardStatus(remoteExecResult{Status: "timeout", ExitCode: 124, Timeout: true}); got != "timeout" {
		t.Fatalf("expected timeout, got %q", got)
	}
	if got := execResultCardStatus(remoteExecResult{Status: "cancelled", ExitCode: 130, Cancelled: true}); got != "cancelled" {
		t.Fatalf("expected cancelled, got %q", got)
	}
	if got := execResultCardStatus(remoteExecResult{Status: "failed", Message: "remote host disconnected"}); got != "disconnected" {
		t.Fatalf("expected disconnected, got %q", got)
	}
	if got := execResultCardStatus(remoteExecResult{Status: "failed", Output: "zsh:1: operation not permitted: ps"}); got != "permission_denied" {
		t.Fatalf("expected permission_denied, got %q", got)
	}
}

func TestHandleAgentExecExitKeepsStructuredRemoteResult(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-structured-exec"
	hostID := "linux-01"
	cardID := "card-structured-exec"
	now := model.NowString()

	app.store.EnsureSession(sessionID)
	app.store.UpsertHost(model.Host{ID: hostID, Name: "linux-01", Kind: "agent", Status: "online", Executable: true})
	app.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "CommandCard",
		Status:    "inProgress",
		CreatedAt: now,
		UpdatedAt: now,
	})

	exec := &remoteExecSession{
		ID:        "exec-structured",
		SessionID: sessionID,
		HostID:    hostID,
		CardID:    cardID,
		done:      make(chan remoteExecResult, 1),
	}
	app.setExecSession(exec)

	app.handleAgentExecOutput(hostID, &agentrpc.ExecOutput{ExecID: exec.ID, Stream: "stdout", Data: "cpu ok\n"})
	app.handleAgentExecOutput(hostID, &agentrpc.ExecOutput{ExecID: exec.ID, Stream: "stderr", Data: "warn\n"})
	app.handleAgentExecExit(hostID, &agentrpc.ExecExit{
		ExecID:   exec.ID,
		ExitCode: 2,
		Status:   "failed",
		Stdout:   "cpu ok\n",
		Stderr:   "warn\n",
		Error:    "exit status 2",
	})

	result := <-exec.done
	if result.Stdout != "cpu ok\n" {
		t.Fatalf("unexpected stdout %q", result.Stdout)
	}
	if result.Stderr != "warn\n" {
		t.Fatalf("unexpected stderr %q", result.Stderr)
	}
	if result.ExitCode != 2 {
		t.Fatalf("expected exitCode 2, got %d", result.ExitCode)
	}
	if result.Error != "exit status 2" {
		t.Fatalf("unexpected error %q", result.Error)
	}

	card := app.cardByID(sessionID, cardID)
	if card == nil {
		t.Fatalf("expected card to exist")
	}
	if card.Stdout != "cpu ok\n" {
		t.Fatalf("expected card stdout to be updated, got %q", card.Stdout)
	}
	if card.Stderr != "warn\n" {
		t.Fatalf("expected card stderr to be updated, got %q", card.Stderr)
	}
}

func TestMarkTurnInterruptedSendsRemoteCancelAndKeepsCancelledCard(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-stop-remote"
	hostID := "linux-01"
	cardID := "card-stop-remote"
	now := model.NowString()

	app.store.EnsureSession(sessionID)
	app.store.UpsertHost(model.Host{ID: hostID, Name: "linux-01", Kind: "agent", Status: "online", Executable: true})
	app.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "CommandCard",
		Status:    "inProgress",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.startRuntimeTurn(sessionID, hostID)
	app.store.SetTurn(sessionID, "turn-stop-1")

	stream := &fakeAgentConnectServer{}
	app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})

	exec := &remoteExecSession{
		ID:        "exec-stop-1",
		SessionID: sessionID,
		HostID:    hostID,
		CardID:    cardID,
		done:      make(chan remoteExecResult, 1),
	}
	app.setExecSession(exec)

	app.markTurnInterrupted(sessionID, "turn-stop-1")

	messages := stream.snapshotMessages()
	if len(messages) != 1 || messages[0].Kind != "exec/cancel" {
		t.Fatalf("expected exec/cancel to be sent, got %#v", messages)
	}

	card := app.cardByID(sessionID, cardID)
	if card == nil {
		t.Fatalf("expected cancelled card to exist")
	}
	if card.Status != "cancelled" {
		t.Fatalf("expected card to stay cancelled after stop, got %q", card.Status)
	}

	session := app.store.Session(sessionID)
	if session == nil || session.Runtime.Turn.Phase != "aborted" {
		t.Fatalf("expected runtime turn to be aborted, got %#v", session)
	}

	app.handleAgentExecExit(hostID, &agentrpc.ExecExit{
		ExecID:    exec.ID,
		ExitCode:  130,
		Status:    "cancelled",
		Cancelled: true,
		Message:   "command cancelled",
		Stderr:    "command cancelled",
	})
	result := <-exec.done
	if !result.Cancelled {
		t.Fatalf("expected cancelled result, got %#v", result)
	}
	if result.Status != "cancelled" {
		t.Fatalf("expected cancelled status, got %q", result.Status)
	}
}

func TestFinalizeExecCardAddsReadonlySummary(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-readonly-summary"
	cardID := "card-readonly-summary"
	createdAt := model.NowString()
	app.store.EnsureSession(sessionID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "CommandCard",
		Status:    "inProgress",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})

	exec := &remoteExecSession{
		ID:        "exec-readonly-summary",
		SessionID: sessionID,
		HostID:    "linux-01",
		CardID:    cardID,
		ToolName:  "execute_readonly_query",
		Command:   "uptime",
	}
	app.finalizeExecCard(exec, createdAt, remoteExecResult{
		Output:   "load average: 0.12 0.18 0.22\nusers: 2\n",
		Stdout:   "load average: 0.12 0.18 0.22\nusers: 2\n",
		ExitCode: 0,
		Status:   "completed",
	})

	card := app.cardByID(sessionID, cardID)
	if card == nil {
		t.Fatalf("expected card to exist")
	}
	if card.Summary == "" {
		t.Fatalf("expected readonly summary to be populated")
	}
	if len(card.KVRows) == 0 || card.KVRows[0].Key != "退出码" {
		t.Fatalf("expected kv rows to include exit code, got %#v", card.KVRows)
	}
}

func TestFinalizeExecCardKeepsFailedMutationOutputAndExitCode(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-mutation-failed"
	cardID := "card-mutation-failed"
	createdAt := model.NowString()
	app.store.EnsureSession(sessionID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "CommandCard",
		Status:    "inProgress",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})

	exec := &remoteExecSession{
		ID:        "exec-mutation-failed",
		SessionID: sessionID,
		HostID:    "linux-01",
		CardID:    cardID,
		ToolName:  "execute_system_mutation",
		Command:   "systemctl restart nginx",
	}
	app.finalizeExecCard(exec, createdAt, remoteExecResult{
		Output:   "permission denied\nfull stderr\n",
		Stderr:   "permission denied\nfull stderr\n",
		ExitCode: 1,
		Status:   "failed",
	})

	card := app.cardByID(sessionID, cardID)
	if card == nil {
		t.Fatalf("expected card to exist")
	}
	if card.Status != "permission_denied" {
		t.Fatalf("expected permission_denied status, got %q", card.Status)
	}
	if card.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", card.ExitCode)
	}
	if !strings.Contains(card.Output, "full stderr") {
		t.Fatalf("expected full output to be retained, got %q", card.Output)
	}
	if !strings.Contains(card.Summary, "退出码 1") {
		t.Fatalf("expected failed summary to retain exit code, got %q", card.Summary)
	}
}

func TestHandleRemoteApprovalRejectAliasRespondsAndResolves(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-remote-reject"
	hostID := "linux-01"
	now := model.NowString()
	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-remote-reject" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	app.store.EnsureSession(sessionID)
	app.store.UpsertHost(model.Host{ID: hostID, Name: hostID, Kind: "agent", Status: "online", Executable: true})
	app.store.AddApproval(sessionID, model.ApprovalRequest{
		ID:           "approval-remote-reject",
		RequestIDRaw: "raw-remote-reject",
		HostID:       hostID,
		Type:         "remote_command",
		Status:       "pending",
		ItemID:       "card-remote-reject",
		Command:      "systemctl restart nginx",
		RequestedAt:  now,
	})
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "card-remote-reject",
		Type:      "CommandApprovalCard",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/approval-remote-reject/decision", strings.NewReader(`{"decision":"reject"}`))
	recorder := httptest.NewRecorder()
	app.handleApprovalDecision(recorder, req, sessionID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	select {
	case <-responded:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected codex response after reject alias")
	}
	approval, ok := app.store.Approval(sessionID, "approval-remote-reject")
	if !ok || approval.Status != "decline" {
		t.Fatalf("expected approval to resolve as decline, got %#v", approval)
	}
}

func TestHandleRemoteApprovalAcceptSessionContinuesExecution(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	app := New(config.Config{AuditLogPath: auditPath})
	sessionID := "sess-remote-accept-session"
	hostID := "linux-01"
	now := model.NowString()
	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-remote-accept-session" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	app.store.EnsureSession(sessionID)
	app.store.UpdateAuth(sessionID, func(auth *model.AuthState, _ *model.ExternalAuthTokens) {
		auth.Email = "operator@example.com"
		auth.Mode = "chatgpt"
	})
	app.store.SetThread(sessionID, "thread-remote-accept-session")
	app.store.SetTurn(sessionID, "turn-remote-accept-session")
	app.store.UpsertHost(model.Host{ID: hostID, Name: hostID, Kind: "agent", Status: "online", Executable: true})
	app.store.AddApproval(sessionID, model.ApprovalRequest{
		ID:           "approval-remote-accept-session",
		RequestIDRaw: "raw-remote-accept-session",
		HostID:       hostID,
		Fingerprint:  approvalFingerprintForCommand(hostID, "echo smoke-ok", "/tmp"),
		Type:         "remote_command",
		Status:       "pending",
		ThreadID:     "thread-remote-accept-session",
		TurnID:       "turn-remote-accept-session",
		ItemID:       "card-remote-accept-session",
		Command:      "echo smoke-ok",
		Cwd:          "/tmp",
		RequestedAt:  now,
	})
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "card-remote-accept-session",
		Type:      "CommandApprovalCard",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.RememberItem(sessionID, "card-remote-accept-session", map[string]any{
		"host":       hostID,
		"command":    "echo smoke-ok",
		"cwd":        "/tmp",
		"reason":     "smoke",
		"timeoutSec": 5,
	})

	stream := &fakeAgentConnectServer{}
	app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/approval-remote-accept-session/decision", strings.NewReader(`{"decision":"accept_session"}`))
	recorder := httptest.NewRecorder()
	app.handleApprovalDecision(recorder, req, sessionID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	startMsg := stream.waitForKind(t, "exec/start", 2*time.Second)
	if startMsg.ExecStart == nil {
		t.Fatalf("expected exec start payload")
	}
	app.handleAgentExecOutput(hostID, &agentrpc.ExecOutput{
		ExecID: startMsg.ExecStart.ExecID,
		Stream: "stdout",
		Data:   "smoke-ok\n",
	})
	app.handleAgentExecExit(hostID, &agentrpc.ExecExit{
		ExecID:   startMsg.ExecStart.ExecID,
		ExitCode: 0,
		Status:   "completed",
		Stdout:   "smoke-ok\n",
	})

	select {
	case result := <-responded:
		payload, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected tool response map, got %#v", result)
		}
		if payload["success"] != true {
			t.Fatalf("expected successful response payload, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected codex response after accepted remote execution")
	}

	card := app.cardByID(sessionID, "card-remote-accept-session")
	if card == nil || card.Status != "completed" {
		t.Fatalf("expected completed command card, got %#v", card)
	}
	if card.Summary == "" {
		t.Fatalf("expected completed command card summary")
	}
	if _, ok := app.store.ApprovalGrant(sessionID, approvalFingerprintForCommand(hostID, "echo smoke-ok", "/tmp")); !ok {
		t.Fatalf("expected approval grant to be remembered for session")
	}

	records := readAuditRecords(t, auditPath)
	decision := findAuditRecord(records, "approval.decision")
	if decision == nil {
		t.Fatalf("expected approval.decision audit record")
	}
	if got := mustAuditString(t, decision, "sessionId"); got != sessionID {
		t.Fatalf("expected sessionId %q, got %q", sessionID, got)
	}
	if got := mustAuditString(t, decision, "threadId"); got != "thread-remote-accept-session" {
		t.Fatalf("expected threadId thread-remote-accept-session, got %q", got)
	}
	if got := mustAuditString(t, decision, "turnId"); got != "turn-remote-accept-session" {
		t.Fatalf("expected turnId turn-remote-accept-session, got %q", got)
	}
	if got := mustAuditString(t, decision, "hostId"); got != hostID {
		t.Fatalf("expected hostId %q, got %q", hostID, got)
	}
	if got := mustAuditString(t, decision, "hostName"); got != hostID {
		t.Fatalf("expected hostName %q, got %q", hostID, got)
	}
	if got := mustAuditString(t, decision, "operator"); got != "operator@example.com" {
		t.Fatalf("expected operator email, got %q", got)
	}
	if got := mustAuditString(t, decision, "toolName"); got != "execute_system_mutation" {
		t.Fatalf("expected toolName execute_system_mutation, got %q", got)
	}
	if got := mustAuditString(t, decision, "command"); got != "echo smoke-ok" {
		t.Fatalf("expected command echo smoke-ok, got %q", got)
	}
	if got := mustAuditString(t, decision, "cwd"); got != "/tmp" {
		t.Fatalf("expected cwd /tmp, got %q", got)
	}
	if got := mustAuditString(t, decision, "approvalDecision"); got != "accept_session" {
		t.Fatalf("expected approvalDecision accept_session, got %q", got)
	}
	if got := mustAuditString(t, decision, "status"); got != "accepted_for_session" {
		t.Fatalf("expected status accepted_for_session, got %q", got)
	}
	if got := mustAuditString(t, decision, "startedAt"); got != now {
		t.Fatalf("expected startedAt %q, got %q", now, got)
	}
	if got := mustAuditString(t, decision, "endedAt"); got == "" {
		t.Fatalf("expected endedAt to be recorded")
	}
	mustAuditNil(t, decision, "exitCode")

	started := findAuditRecord(records, "remote.exec.started")
	if started == nil {
		t.Fatalf("expected remote.exec.started audit record")
	}
	if got := mustAuditString(t, started, "sessionId"); got != sessionID {
		t.Fatalf("expected started sessionId %q, got %q", sessionID, got)
	}
	if got := mustAuditString(t, started, "threadId"); got != "thread-remote-accept-session" {
		t.Fatalf("expected threadId thread-remote-accept-session, got %q", got)
	}
	if got := mustAuditString(t, started, "turnId"); got != "turn-remote-accept-session" {
		t.Fatalf("expected turnId turn-remote-accept-session, got %q", got)
	}
	if got := mustAuditString(t, started, "operator"); got != "operator@example.com" {
		t.Fatalf("expected operator email on remote.exec.started, got %q", got)
	}
	if got := mustAuditString(t, started, "approvalDecision"); got != "accepted_for_session" {
		t.Fatalf("expected approvalDecision accepted_for_session, got %q", got)
	}
	if got := mustAuditString(t, started, "status"); got != "inProgress" {
		t.Fatalf("expected started status inProgress, got %q", got)
	}
	mustAuditNil(t, started, "endedAt")
	mustAuditNil(t, started, "exitCode")

	finished := findAuditRecord(records, "remote.exec.finished")
	if finished == nil {
		t.Fatalf("expected remote.exec.finished audit record")
	}
	if got := mustAuditString(t, finished, "sessionId"); got != sessionID {
		t.Fatalf("expected finished sessionId %q, got %q", sessionID, got)
	}
	if got := mustAuditString(t, finished, "operator"); got != "operator@example.com" {
		t.Fatalf("expected operator email on remote.exec.finished, got %q", got)
	}
	if got := mustAuditString(t, finished, "command"); got != "echo smoke-ok" {
		t.Fatalf("expected finished command echo smoke-ok, got %q", got)
	}
	if got := mustAuditString(t, finished, "cwd"); got != "/tmp" {
		t.Fatalf("expected finished cwd /tmp, got %q", got)
	}
	if got := mustAuditString(t, finished, "status"); got != "completed" {
		t.Fatalf("expected finished status completed, got %q", got)
	}
	if got := mustAuditString(t, finished, "approvalDecision"); got != "accepted_for_session" {
		t.Fatalf("expected finished approvalDecision accepted_for_session, got %q", got)
	}
	if got, ok := finished["exitCode"].(float64); !ok || got != 0 {
		t.Fatalf("expected finished exitCode 0, got %#v", finished["exitCode"])
	}
}

func TestRunRemoteExecDefaultsSafeCwdAndShellForRemoteHosts(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-remote-default-cwd-shell"
	hostID := "linux-01"
	cardID := "card-remote-default-cwd-shell"
	app.store.EnsureSession(sessionID)
	app.store.UpsertHost(model.Host{
		ID:         hostID,
		Name:       hostID,
		Kind:       "agent",
		Status:     "online",
		Executable: true,
		OS:         "linux",
	})

	stream := &fakeAgentConnectServer{}
	app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})

	done := make(chan struct{})
	go func() {
		msg := stream.waitForKind(t, "exec/start", 2*time.Second)
		if msg.ExecStart == nil {
			t.Errorf("expected exec start payload")
			close(done)
			return
		}
		if got := msg.ExecStart.Cwd; got != "/tmp" {
			t.Errorf("expected default cwd /tmp, got %q", got)
		}
		if got := msg.ExecStart.Shell; got != "/bin/sh" {
			t.Errorf("expected default shell /bin/sh, got %q", got)
		}
		app.handleAgentExecExit(hostID, &agentrpc.ExecExit{
			ExecID:   msg.ExecStart.ExecID,
			ExitCode: 0,
			Status:   "completed",
		})
		close(done)
	}()

	result, err := app.runRemoteExec(context.Background(), sessionID, hostID, cardID, execSpec{
		Command:  "uptime",
		Readonly: true,
		ToolName: "readonly_host_inspect",
	})
	if err != nil {
		t.Fatalf("runRemoteExec: %v", err)
	}
	<-done

	if result.Status != "completed" {
		t.Fatalf("expected completed status, got %#v", result)
	}
	card := app.cardByID(sessionID, cardID)
	if card == nil {
		t.Fatalf("expected command card to exist")
	}
	if card.Cwd != "/tmp" {
		t.Fatalf("expected card cwd /tmp, got %q", card.Cwd)
	}
}

func TestRemoteFileChangeAuditLifecycleIncludesStableFields(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	app := New(config.Config{AuditLogPath: auditPath})
	sessionID := "sess-remote-file-change"
	hostID := "linux-01"
	now := model.NowString()
	responded := make(chan any, 1)

	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-remote-file-change" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	app.store.EnsureSession(sessionID)
	app.store.UpdateAuth(sessionID, func(auth *model.AuthState, _ *model.ExternalAuthTokens) {
		auth.Email = "operator@example.com"
		auth.Mode = "chatgpt"
	})
	app.store.SetThread(sessionID, "thread-remote-file-change")
	app.store.SetTurn(sessionID, "turn-remote-file-change")
	app.store.UpsertHost(model.Host{ID: hostID, Name: "db-prod-01", Kind: "agent", Status: "online", Executable: true})

	stream := &fileChangeAgentStream{}
	stream.onSend = func(msg *agentrpc.Envelope) error {
		switch msg.Kind {
		case "file/read":
			app.handleAgentFileReadResult(hostID, &agentrpc.FileReadResult{
				RequestID: msg.FileReadRequest.RequestID,
				Path:      msg.FileReadRequest.Path,
				Content:   "worker_processes 1;\n",
			})
		case "file/write":
			app.handleAgentFileWriteResult(hostID, &agentrpc.FileWriteResult{
				RequestID:  msg.FileWriteRequest.RequestID,
				Path:       msg.FileWriteRequest.Path,
				OldContent: "worker_processes 1;\n",
				NewContent: "worker_processes auto;\n",
				Created:    false,
				WriteMode:  msg.FileWriteRequest.WriteMode,
			})
		}
		return nil
	}
	app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})

	app.requestRemoteFileChangeApproval(sessionID, hostID, "raw-remote-file-change", dynamicToolCallParams{
		ThreadID: "thread-remote-file-change",
		TurnID:   "turn-remote-file-change",
		CallID:   "call-remote-file-change",
		Tool:     "execute_system_mutation",
	}, remoteFileChangeArgs{
		Mode:      "file_change",
		Path:      "/etc/nginx/nginx.conf",
		Content:   "worker_processes auto;\n",
		WriteMode: "overwrite",
		Reason:    "update nginx config",
	})

	session := app.store.Session(sessionID)
	if session == nil || len(session.Approvals) != 1 {
		t.Fatalf("expected one pending approval, got %#v", session)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approval.ID+"/decision", strings.NewReader(`{"decision":"accept_session"}`))
	recorder := httptest.NewRecorder()
	app.handleApprovalDecision(recorder, req, sessionID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	select {
	case result := <-responded:
		payload, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected tool response map, got %#v", result)
		}
		if payload["success"] != true {
			t.Fatalf("expected successful remote file change response, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected codex response after accepted remote file change")
	}

	records := readAuditRecords(t, auditPath)
	requested := findAuditRecord(records, "approval.requested")
	if requested == nil {
		t.Fatalf("expected approval.requested audit record")
	}
	if got := requested["filePath"]; got != "/etc/nginx/nginx.conf" {
		t.Fatalf("expected requested filePath, got %#v", got)
	}
	if got := requested["threadId"]; got != "thread-remote-file-change" {
		t.Fatalf("expected requested threadId, got %#v", got)
	}
	if got := requested["turnId"]; got != "turn-remote-file-change" {
		t.Fatalf("expected requested turnId, got %#v", got)
	}
	if got := requested["hostName"]; got != "db-prod-01" {
		t.Fatalf("expected requested hostName db-prod-01, got %#v", got)
	}
	if got := requested["operator"]; got != "operator@example.com" {
		t.Fatalf("expected requested operator email, got %#v", got)
	}
	if got := requested["cwd"]; got != "/etc/nginx" {
		t.Fatalf("expected requested cwd /etc/nginx, got %#v", got)
	}
	if got := requested["toolName"]; got != "execute_system_mutation" {
		t.Fatalf("expected requested toolName execute_system_mutation, got %#v", got)
	}
	if got := requested["status"]; got != "pending" {
		t.Fatalf("expected requested status pending, got %#v", got)
	}
	if requested["approvalDecision"] != nil {
		t.Fatalf("expected requested approvalDecision nil, got %#v", requested["approvalDecision"])
	}
	if requested["endedAt"] != nil {
		t.Fatalf("expected requested endedAt nil, got %#v", requested["endedAt"])
	}
	if requested["exitCode"] != nil {
		t.Fatalf("expected requested exitCode nil, got %#v", requested["exitCode"])
	}

	decision := findAuditRecord(records, "approval.decision")
	if decision == nil {
		t.Fatalf("expected approval.decision audit record")
	}
	if got := mustAuditString(t, decision, "approvalDecision"); got != "accept_session" {
		t.Fatalf("expected approvalDecision accept_session, got %q", got)
	}
	if got := mustAuditString(t, decision, "status"); got != "accepted_for_session" {
		t.Fatalf("expected decision status accepted_for_session, got %q", got)
	}
	if got := mustAuditString(t, decision, "filePath"); got != "/etc/nginx/nginx.conf" {
		t.Fatalf("expected decision filePath, got %q", got)
	}
	if got := mustAuditString(t, decision, "cwd"); got != "/etc/nginx" {
		t.Fatalf("expected decision cwd /etc/nginx, got %q", got)
	}
	if got := mustAuditString(t, decision, "operator"); got != "operator@example.com" {
		t.Fatalf("expected decision operator email, got %q", got)
	}
	if got := mustAuditString(t, decision, "startedAt"); got != now {
		t.Fatalf("expected decision startedAt %q, got %q", now, got)
	}
	if got := mustAuditString(t, decision, "endedAt"); got == "" {
		t.Fatalf("expected decision endedAt to be recorded")
	}
	mustAuditNil(t, decision, "exitCode")

	started := findAuditRecord(records, "remote.file_change.started")
	if started == nil {
		t.Fatalf("expected remote.file_change.started audit record")
	}
	if got := mustAuditString(t, started, "filePath"); got != "/etc/nginx/nginx.conf" {
		t.Fatalf("expected started filePath, got %q", got)
	}
	if got := mustAuditString(t, started, "cwd"); got != "/etc/nginx" {
		t.Fatalf("expected started cwd /etc/nginx, got %q", got)
	}
	if got := mustAuditString(t, started, "operator"); got != "operator@example.com" {
		t.Fatalf("expected started operator email, got %q", got)
	}
	if got := mustAuditString(t, started, "approvalDecision"); got != "accepted_for_session" {
		t.Fatalf("expected started approvalDecision accepted_for_session, got %q", got)
	}
	if got := mustAuditString(t, started, "status"); got != "inProgress" {
		t.Fatalf("expected started status inProgress, got %q", got)
	}
	mustAuditNil(t, started, "endedAt")
	mustAuditNil(t, started, "exitCode")

	finished := findAuditRecord(records, "remote.file_change.finished")
	if finished == nil {
		t.Fatalf("expected remote.file_change.finished audit record")
	}
	if got := mustAuditString(t, finished, "filePath"); got != "/etc/nginx/nginx.conf" {
		t.Fatalf("expected finished filePath, got %q", got)
	}
	if got := mustAuditString(t, finished, "cwd"); got != "/etc/nginx" {
		t.Fatalf("expected finished cwd /etc/nginx, got %q", got)
	}
	if got := mustAuditString(t, finished, "operator"); got != "operator@example.com" {
		t.Fatalf("expected finished operator email, got %q", got)
	}
	if got := mustAuditString(t, finished, "status"); got != "completed" {
		t.Fatalf("expected finished status completed, got %q", got)
	}
	if got := mustAuditString(t, finished, "approvalDecision"); got != "accepted_for_session" {
		t.Fatalf("expected finished approvalDecision accepted_for_session, got %q", got)
	}
	if got := mustAuditString(t, finished, "startedAt"); got != now {
		t.Fatalf("expected finished startedAt %q, got %q", now, got)
	}
	if got := mustAuditString(t, finished, "endedAt"); got == "" {
		t.Fatalf("expected finished endedAt to be recorded")
	}
	mustAuditNil(t, finished, "exitCode")
}

func TestHandleDynamicToolCallRejectsHostMismatch(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-dynamic-tool-host-mismatch"
	hostID := "linux-01"
	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-tool-host-mismatch" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, hostID)
	app.store.SetThread(sessionID, "thread-tool-host-mismatch")
	app.store.UpsertHost(model.Host{ID: hostID, Name: hostID, Kind: "agent", Status: "online", Executable: true})

	app.handleDynamicToolCall("raw-tool-host-mismatch", map[string]any{
		"threadId": "thread-tool-host-mismatch",
		"turnId":   "turn-tool-host-mismatch",
		"callId":   "call-tool-host-mismatch",
		"tool":     "execute_readonly_query",
		"arguments": map[string]any{
			"host":    "linux-02",
			"command": "uptime",
			"reason":  "check load",
		},
	})

	select {
	case result := <-responded:
		payload, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected tool response map, got %#v", result)
		}
		if payload["success"] != false {
			t.Fatalf("expected failed tool response, got %#v", payload)
		}
		items, _ := payload["contentItems"].([]map[string]any)
		if len(items) == 0 || !strings.Contains(getString(items[0], "text"), "does not match selected host") {
			t.Fatalf("expected mismatch text in response, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected tool response for host mismatch")
	}
}

func TestHandleCommandApprovalRequestBlocksLocalFallbackOnRemoteHost(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-local-approval-blocked"
	hostID := "linux-01"
	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "1" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, hostID)
	app.store.SetThread(sessionID, "thread-local-approval-blocked")
	app.store.UpsertHost(model.Host{ID: hostID, Name: hostID, Kind: "agent", Status: "online", Executable: true})

	payload := map[string]any{
		"threadId": "thread-local-approval-blocked",
		"turnId":   "turn-local-approval-blocked",
		"itemId":   "cmd-local-approval-blocked",
		"command":  "pwd",
		"cwd":      "/tmp",
		"reason":   "debug",
	}
	app.handleLocalCommandApprovalRequest("1", payload)

	select {
	case result := <-responded:
		payload, ok := result.(map[string]any)
		if !ok || payload["decision"] != "decline" {
			t.Fatalf("expected decline response, got %#v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected decline response for blocked local approval")
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected turn to stay thinking, got %q", session.Runtime.Turn.Phase)
	}
	found := false
	for _, card := range session.Cards {
		if card.Type != "ErrorCard" {
			continue
		}
		if card.HostID != hostID {
			t.Fatalf("expected error card host id %q, got %#v", hostID, card)
		}
		if !strings.Contains(card.Message, "不会静默回退到 server-local") {
			t.Fatalf("expected no-fallback text, got %#v", card)
		}
		found = true
	}
	if !found {
		t.Fatalf("expected error card for blocked local fallback")
	}
}

func TestHandleItemStartedBlocksUnexpectedLocalCommandOnRemoteHost(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-local-command-blocked"
	hostID := "linux-01"

	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, hostID)
	app.store.SetThread(sessionID, "thread-local-command-blocked")
	app.store.SetTurn(sessionID, "turn-local-command-blocked")
	app.startRuntimeTurn(sessionID, hostID)
	app.store.UpsertHost(model.Host{ID: hostID, Name: hostID, Kind: "agent", Status: "online", Executable: true})

	app.handleItemStarted(map[string]any{
		"threadId": "thread-local-command-blocked",
		"turnId":   "turn-local-command-blocked",
		"item": map[string]any{
			"id":      "cmd-local-command-blocked",
			"type":    "commandExecution",
			"command": "pwd",
			"status":  "in_progress",
		},
	})

	session := app.store.Session(sessionID)
	if session == nil || session.Runtime.Turn.Phase != "failed" {
		t.Fatalf("expected turn to fail after local fallback block, got %#v", session)
	}

	card := app.cardByID(sessionID, "cmd-local-command-blocked")
	if card == nil {
		t.Fatalf("expected failed command card to exist")
	}
	if card.Status != "failed" {
		t.Fatalf("expected failed command card, got %#v", card)
	}
	if !strings.Contains(card.Output, "已阻止本地") {
		t.Fatalf("expected fallback message in command card, got %#v", card)
	}
}

func TestProcessLineCardsTrackCommandAndFileSearchLifecycle(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-process-line-lifecycle"
	threadID := "thread-process-line"

	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, threadID)

	app.handleItemStarted(map[string]any{
		"threadId": threadID,
		"turnId":   "turn-process-line",
		"item": map[string]any{
			"id":      "cmd-process-line",
			"type":    "commandExecution",
			"command": "systemctl status nginx",
			"status":  "in_progress",
		},
	})

	card := app.cardByID(sessionID, "process-cmd-process-line")
	if card == nil {
		t.Fatalf("expected command process card to exist")
	}
	if card.Status != "inProgress" {
		t.Fatalf("expected command process card to be in progress, got %q", card.Status)
	}
	if !strings.Contains(card.Text, "现在执行命令") {
		t.Fatalf("expected command process card text to mention execution, got %q", card.Text)
	}

	app.handleItemCompleted(map[string]any{
		"threadId": threadID,
		"turnId":   "turn-process-line",
		"item": map[string]any{
			"id":       "cmd-process-line",
			"type":     "commandExecution",
			"command":  "systemctl status nginx",
			"status":   "completed",
			"exitCode": 0,
			"output":   "nginx active",
		},
	})

	card = app.cardByID(sessionID, "process-cmd-process-line")
	if card == nil {
		t.Fatalf("expected completed command process card to exist")
	}
	if card.Status != "completed" {
		t.Fatalf("expected completed command process card, got %q", card.Status)
	}
	if !strings.Contains(card.Text, "已处理 1 个命令") {
		t.Fatalf("expected command completion text, got %q", card.Text)
	}

	app.handleItemStarted(map[string]any{
		"threadId": threadID,
		"turnId":   "turn-process-line",
		"item": map[string]any{
			"id":     "search-process-line",
			"type":   "fileSearch",
			"path":   "/etc/nginx",
			"query":  "server_name",
			"status": "in_progress",
		},
	})

	card = app.cardByID(sessionID, "process-search-process-line")
	if card == nil {
		t.Fatalf("expected file search process card to exist")
	}
	if card.Status != "inProgress" {
		t.Fatalf("expected file search process card to be in progress, got %q", card.Status)
	}
	if !strings.Contains(card.Text, "现在搜索文件") {
		t.Fatalf("expected file search start text, got %q", card.Text)
	}

	app.handleItemCompleted(map[string]any{
		"threadId": threadID,
		"turnId":   "turn-process-line",
		"item": map[string]any{
			"id":     "search-process-line",
			"type":   "fileSearch",
			"path":   "/etc/nginx",
			"query":  "server_name",
			"status": "completed",
		},
	})

	card = app.cardByID(sessionID, "process-search-process-line")
	if card == nil {
		t.Fatalf("expected completed file search process card to exist")
	}
	if card.Status != "completed" {
		t.Fatalf("expected completed file search process card, got %q", card.Status)
	}
	if !strings.Contains(card.Text, "已搜索文件") {
		t.Fatalf("expected file search completion text, got %q", card.Text)
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if session.Runtime.Activity.CommandsRun != 1 {
		t.Fatalf("expected 1 command run, got %d", session.Runtime.Activity.CommandsRun)
	}
	if session.Runtime.Activity.SearchCount != 1 {
		t.Fatalf("expected 1 search, got %d", session.Runtime.Activity.SearchCount)
	}
	if len(session.Runtime.Activity.SearchedContentQueries) != 1 {
		t.Fatalf("expected 1 content search entry, got %d", len(session.Runtime.Activity.SearchedContentQueries))
	}
	if session.Runtime.Activity.CurrentSearchKind != "" || session.Runtime.Activity.CurrentSearchQuery != "" {
		t.Fatalf("expected search activity to clear, got %#v", session.Runtime.Activity)
	}
}

func TestExpireConnectingTerminalSessionTimesOutAndReclaimsSession(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-terminal-timeout"
	terminalID := "term-timeout"

	app.store.EnsureSession(sessionID)
	app.terminals[terminalID] = &terminalSession{
		ID:             terminalID,
		OwnerSessionID: sessionID,
		HostID:         "linux-01",
		Status:         "connecting",
		Remote:         true,
	}

	app.expireConnectingTerminalSession(terminalID, 5*time.Millisecond)

	if _, ok := app.terminalSession(terminalID); ok {
		t.Fatalf("expected connecting terminal to be reclaimed after timeout")
	}
}

func TestReapExitedTerminalSessionRemovesExitedSession(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-terminal-reap"
	terminalID := "term-reap"

	app.store.EnsureSession(sessionID)
	app.terminals[terminalID] = &terminalSession{
		ID:             terminalID,
		OwnerSessionID: sessionID,
		HostID:         model.ServerLocalHostID,
		Status:         "disconnected",
		exited:         true,
	}

	app.reapExitedTerminalSession(terminalID, 5*time.Millisecond)

	if _, ok := app.terminalSession(terminalID); ok {
		t.Fatalf("expected exited terminal to be reclaimed")
	}
}

func TestParseRemoteFileChangeArgsDefaultsAndAppend(t *testing.T) {
	args, err := parseRemoteFileChangeArgs(map[string]any{
		"host":    "linux-01",
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
		"host":       "linux-01",
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

func TestHostSelectionPersistsHostAndClearsThreadBinding(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "aiops_codex_session",
		SessionSecret:     "test-session-secret",
		SessionCookieTTL:  time.Hour,
	})
	stateHandler := app.withSession(app.handleState)
	selectHandler := app.withSession(app.handleHostSelection)

	app.store.UpsertHost(model.Host{
		ID:              "linux-01",
		Name:            "linux-01",
		Kind:            "agent",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})

	stateReq := httptest.NewRequest(http.MethodGet, "/api/v1/state", nil)
	stateRecorder := httptest.NewRecorder()
	stateHandler(stateRecorder, stateReq)
	if stateRecorder.Code != http.StatusOK {
		t.Fatalf("expected initial state request to succeed, got %d", stateRecorder.Code)
	}

	var snapshot model.Snapshot
	if err := json.NewDecoder(stateRecorder.Result().Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode initial snapshot: %v", err)
	}

	var sessionCookie *http.Cookie
	for _, cookie := range stateRecorder.Result().Cookies() {
		if cookie.Name == app.cfg.SessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected session cookie %q to be set", app.cfg.SessionCookieName)
	}

	app.store.SetSelectedHost(snapshot.SessionID, model.ServerLocalHostID)
	app.store.SetThread(snapshot.SessionID, "thread-1")
	app.store.SetTurn(snapshot.SessionID, "turn-1")

	selectReq := httptest.NewRequest(http.MethodPost, "/api/v1/host/select", strings.NewReader(`{"hostId":"linux-01"}`))
	selectReq.AddCookie(sessionCookie)
	selectRecorder := httptest.NewRecorder()
	selectHandler(selectRecorder, selectReq)
	if selectRecorder.Code != http.StatusOK {
		t.Fatalf("expected host selection to succeed, got %d", selectRecorder.Code)
	}

	var selected model.Snapshot
	if err := json.NewDecoder(selectRecorder.Result().Body).Decode(&selected); err != nil {
		t.Fatalf("decode selected snapshot: %v", err)
	}
	if selected.SelectedHostID != "linux-01" {
		t.Fatalf("expected selected host to be linux-01, got %q", selected.SelectedHostID)
	}

	session := app.store.Session(snapshot.SessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if session.SelectedHostID != "linux-01" {
		t.Fatalf("expected persisted host to be linux-01, got %q", session.SelectedHostID)
	}
	if session.ThreadID != "" {
		t.Fatalf("expected thread binding to be cleared, got %q", session.ThreadID)
	}
	if session.TurnID != "" {
		t.Fatalf("expected turn binding to be cleared, got %q", session.TurnID)
	}
}

func TestRequestRemoteCommandApprovalIncludesHostMetadata(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	app := New(config.Config{AuditLogPath: auditPath})
	sessionID := "sess-remote-approval"
	app.store.EnsureSession(sessionID)
	app.store.UpdateAuth(sessionID, func(auth *model.AuthState, _ *model.ExternalAuthTokens) {
		auth.Email = "operator@example.com"
		auth.Mode = "chatgpt"
	})
	app.store.UpsertHost(model.Host{
		ID:              "linux-01",
		Name:            "db-prod-01",
		Kind:            "agent",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})

	app.requestRemoteCommandApproval(sessionID, "linux-01", "raw-1", dynamicToolCallParams{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		CallID:   "call-1",
		Tool:     "execute_system_mutation",
	}, execToolArgs{
		Command: "systemctl restart nginx",
		Cwd:     "/etc/nginx",
		Reason:  "restart nginx",
	}, false)

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Cards) != 1 {
		t.Fatalf("expected 1 approval card, got %d", len(session.Cards))
	}
	card := session.Cards[0]
	if card.HostID != "linux-01" {
		t.Fatalf("expected host id linux-01, got %q", card.HostID)
	}
	if card.HostName != "db-prod-01" {
		t.Fatalf("expected host name db-prod-01, got %q", card.HostName)
	}

	content, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) == 0 {
		t.Fatalf("expected audit log to contain approval record")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &record); err != nil {
		t.Fatalf("decode audit record: %v", err)
	}
	if record["event"] != "approval.requested" {
		t.Fatalf("expected approval.requested event, got %#v", record["event"])
	}
	if record["reason"] != "restart nginx" {
		t.Fatalf("expected reason to be recorded, got %#v", record["reason"])
	}
	if record["sessionId"] != sessionID {
		t.Fatalf("expected sessionId to be recorded, got %#v", record["sessionId"])
	}
	if record["threadId"] != "thread-1" {
		t.Fatalf("expected threadId to be recorded, got %#v", record["threadId"])
	}
	if record["turnId"] != "turn-1" {
		t.Fatalf("expected turnId to be recorded, got %#v", record["turnId"])
	}
	if record["hostId"] != "linux-01" {
		t.Fatalf("expected hostId to be recorded, got %#v", record["hostId"])
	}
	if record["hostName"] != "db-prod-01" {
		t.Fatalf("expected hostName to be recorded, got %#v", record["hostName"])
	}
	if record["operator"] != "operator@example.com" {
		t.Fatalf("expected operator to be recorded, got %#v", record["operator"])
	}
	if record["toolName"] != "execute_system_mutation" {
		t.Fatalf("expected toolName to be recorded, got %#v", record["toolName"])
	}
	if record["command"] != "systemctl restart nginx" {
		t.Fatalf("expected command to be recorded, got %#v", record["command"])
	}
	if record["cwd"] != "/etc/nginx" {
		t.Fatalf("expected cwd to be recorded, got %#v", record["cwd"])
	}
	if record["approvalDecision"] != nil {
		t.Fatalf("expected approvalDecision to be nil, got %#v", record["approvalDecision"])
	}
	if record["startedAt"] == nil || record["startedAt"] == "" {
		t.Fatalf("expected startedAt to be recorded, got %#v", record["startedAt"])
	}
	if record["endedAt"] != nil {
		t.Fatalf("expected endedAt to be nil, got %#v", record["endedAt"])
	}
	if record["status"] != "pending" {
		t.Fatalf("expected status pending, got %#v", record["status"])
	}
	if record["exitCode"] != nil {
		t.Fatalf("expected exitCode to be nil, got %#v", record["exitCode"])
	}
	if record["fingerprint"] == nil || record["fingerprint"] == "" {
		t.Fatalf("expected fingerprint to be recorded, got %#v", record["fingerprint"])
	}
}

func TestAuditApprovalLifecycleAutoAcceptedIncludesStableFields(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	app := New(config.Config{AuditLogPath: auditPath})
	sessionID := "sess-auto-accepted"
	now := model.NowString()
	app.store.EnsureSession(sessionID)
	app.store.UpdateAuth(sessionID, func(auth *model.AuthState, _ *model.ExternalAuthTokens) {
		auth.Email = "operator@example.com"
		auth.Mode = "chatgpt"
	})
	app.store.SetThread(sessionID, "thread-auto-accepted")
	app.store.SetTurn(sessionID, "turn-auto-accepted")
	app.store.UpsertHost(model.Host{ID: "linux-01", Name: "db-prod-01", Kind: "agent", Status: "online", Executable: true})

	approval := model.ApprovalRequest{
		ID:          "approval-auto-accepted",
		HostID:      "linux-01",
		Type:        "remote_file_change",
		Status:      "accepted_for_session_auto",
		ThreadID:    "thread-auto-accepted",
		TurnID:      "turn-auto-accepted",
		RequestedAt: now,
		GrantRoot:   "/etc/nginx",
		Changes: []model.FileChange{
			{Path: "/etc/nginx/nginx.conf", Kind: "modified"},
		},
		Command:     "",
		Fingerprint: "fp-auto-accepted",
	}

	app.auditApprovalLifecycleEvent("approval.auto_accepted", sessionID, approval, "accept_session", approval.Status, approval.RequestedAt, now, map[string]any{
		"fingerprint": approval.Fingerprint,
	})

	records := readAuditRecords(t, auditPath)
	record := findAuditRecord(records, "approval.auto_accepted")
	if record == nil {
		t.Fatalf("expected approval.auto_accepted audit record")
	}
	if record["sessionId"] != sessionID {
		t.Fatalf("expected sessionId %q, got %#v", sessionID, record["sessionId"])
	}
	if record["threadId"] != "thread-auto-accepted" {
		t.Fatalf("expected threadId thread-auto-accepted, got %#v", record["threadId"])
	}
	if record["turnId"] != "turn-auto-accepted" {
		t.Fatalf("expected turnId turn-auto-accepted, got %#v", record["turnId"])
	}
	if record["hostId"] != "linux-01" {
		t.Fatalf("expected hostId linux-01, got %#v", record["hostId"])
	}
	if record["hostName"] != "db-prod-01" {
		t.Fatalf("expected hostName db-prod-01, got %#v", record["hostName"])
	}
	if record["operator"] != "operator@example.com" {
		t.Fatalf("expected operator email, got %#v", record["operator"])
	}
	if record["toolName"] != "execute_system_mutation" {
		t.Fatalf("expected toolName execute_system_mutation, got %#v", record["toolName"])
	}
	if record["filePath"] != "/etc/nginx/nginx.conf" {
		t.Fatalf("expected filePath /etc/nginx/nginx.conf, got %#v", record["filePath"])
	}
	if record["cwd"] != "/etc/nginx" {
		t.Fatalf("expected cwd /etc/nginx, got %#v", record["cwd"])
	}
	if record["approvalDecision"] != "accept_session" {
		t.Fatalf("expected approvalDecision accept_session, got %#v", record["approvalDecision"])
	}
	if record["startedAt"] != now {
		t.Fatalf("expected startedAt %q, got %#v", now, record["startedAt"])
	}
	if record["endedAt"] != now {
		t.Fatalf("expected endedAt %q, got %#v", now, record["endedAt"])
	}
	if record["status"] != "accepted_for_session_auto" {
		t.Fatalf("expected status accepted_for_session_auto, got %#v", record["status"])
	}
	if record["exitCode"] != nil {
		t.Fatalf("expected exitCode nil, got %#v", record["exitCode"])
	}
	if record["fingerprint"] != "fp-auto-accepted" {
		t.Fatalf("expected fingerprint fp-auto-accepted, got %#v", record["fingerprint"])
	}
}

func TestThreadResetOnlyAffectsActiveSession(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "aiops_codex_session",
		SessionSecret:     "test-session-secret",
		SessionCookieTTL:  time.Hour,
	})
	sessionsHandler := app.withBrowserSession(app.handleSessions)
	activateHandler := app.withBrowserSession(app.handleSessionActivation)
	resetHandler := app.withSession(app.handleThreadReset)

	decodeResponse := func(t *testing.T, recorder *httptest.ResponseRecorder, out any) {
		t.Helper()
		if err := json.NewDecoder(recorder.Result().Body).Decode(out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}

	getSessionCookie := func(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
		t.Helper()
		for _, cookie := range recorder.Result().Cookies() {
			if cookie.Name == app.cfg.SessionCookieName {
				return cookie
			}
		}
		t.Fatalf("expected session cookie %q to be set", app.cfg.SessionCookieName)
		return nil
	}

	type sessionsResponse struct {
		ActiveSessionID string                 `json:"activeSessionId"`
		Sessions        []model.SessionSummary `json:"sessions"`
	}

	initialReq := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	initialRecorder := httptest.NewRecorder()
	sessionsHandler(initialRecorder, initialReq)
	if initialRecorder.Code != http.StatusOK {
		t.Fatalf("expected initial sessions request to succeed, got %d", initialRecorder.Code)
	}

	var initial sessionsResponse
	decodeResponse(t, initialRecorder, &initial)
	cookie := getSessionCookie(t, initialRecorder)
	firstSessionID := initial.ActiveSessionID
	if firstSessionID == "" {
		t.Fatalf("expected initial active session to be created")
	}

	app.store.UpsertCard(firstSessionID, model.Card{
		ID:        "card-first",
		Type:      "UserMessageCard",
		Text:      "first session history",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", nil)
	createReq.AddCookie(cookie)
	createRecorder := httptest.NewRecorder()
	sessionsHandler(createRecorder, createReq)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("expected session creation to succeed, got %d", createRecorder.Code)
	}

	var created sessionsResponse
	decodeResponse(t, createRecorder, &created)
	secondSessionID := created.ActiveSessionID
	if secondSessionID == "" || secondSessionID == firstSessionID {
		t.Fatalf("expected a new active session, got %q", secondSessionID)
	}

	app.store.UpsertCard(secondSessionID, model.Card{
		ID:        "card-second",
		Type:      "AssistantMessageCard",
		Text:      "second session keeps history",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})

	activateReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+firstSessionID+"/activate", nil)
	activateReq.AddCookie(cookie)
	activateRecorder := httptest.NewRecorder()
	activateHandler(activateRecorder, activateReq)
	if activateRecorder.Code != http.StatusOK {
		t.Fatalf("expected activation to succeed, got %d", activateRecorder.Code)
	}

	resetReq := httptest.NewRequest(http.MethodPost, "/api/v1/thread/reset", nil)
	resetReq.AddCookie(cookie)
	resetRecorder := httptest.NewRecorder()
	resetHandler(resetRecorder, resetReq)
	if resetRecorder.Code != http.StatusOK {
		t.Fatalf("expected reset to succeed, got %d", resetRecorder.Code)
	}

	firstSession := app.store.Session(firstSessionID)
	if firstSession == nil {
		t.Fatalf("expected first session to exist")
	}
	if len(firstSession.Cards) != 0 {
		t.Fatalf("expected active session cards to be cleared, got %d", len(firstSession.Cards))
	}

	secondSession := app.store.Session(secondSessionID)
	if secondSession == nil {
		t.Fatalf("expected second session to exist")
	}
	if len(secondSession.Cards) != 1 {
		t.Fatalf("expected inactive session history to remain, got %d cards", len(secondSession.Cards))
	}
	if secondSession.Cards[0].Text != "second session keeps history" {
		t.Fatalf("unexpected inactive session card: %#v", secondSession.Cards[0])
	}

	browserID, ok := app.verifySessionCookie(cookie.Value)
	if !ok {
		t.Fatalf("expected session cookie to be valid")
	}
	browser := app.store.BrowserSession(browserID)
	if browser == nil {
		t.Fatalf("expected browser session to exist")
	}
	if browser.ActiveSessionID != firstSessionID {
		t.Fatalf("expected active session to remain %q after reset, got %q", firstSessionID, browser.ActiveSessionID)
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

func TestShouldAutoResetThread(t *testing.T) {
	app := New(config.Config{})

	t.Run("resets idle thread", func(t *testing.T) {
		session := &store.SessionState{
			ThreadID:       "thread-1",
			LastActivityAt: time.Now().Add(-(autoThreadResetIdleThreshold + time.Minute)).Format(time.RFC3339),
		}

		if !app.shouldAutoResetThread(session, "你好") {
			t.Fatalf("expected idle thread to reset")
		}
	})

	t.Run("resets short prompt on long conversation", func(t *testing.T) {
		session := &store.SessionState{
			ThreadID:       "thread-2",
			LastActivityAt: time.Now().Format(time.RFC3339),
			Cards:          make([]model.Card, 0, autoThreadResetConversationThreshold),
		}
		for i := 0; i < autoThreadResetConversationThreshold; i++ {
			session.Cards = append(session.Cards, model.Card{ID: model.NewID("msg"), Type: "UserMessageCard"})
		}

		if !app.shouldAutoResetThread(session, "你好") {
			t.Fatalf("expected long conversation short prompt to reset")
		}
		if app.shouldAutoResetThread(session, "继续按刚才的方案把第 3 步展开说清楚") {
			t.Fatalf("expected richer follow-up to keep current thread")
		}
	})
}

func TestAutoApproveByHostGrantAcceptsMatchingFingerprint(t *testing.T) {
	dir := t.TempDir()
	app := New(config.Config{})
	app.approvalGrantStore = store.NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	sessionID := "sess-host-grant"
	hostID := "linux-01"
	now := model.NowString()
	fingerprint := "command|linux-01|/tmp|ls -la"

	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		responded <- result
		return nil
	}

	// Add a matching host-level grant
	_ = app.approvalGrantStore.Add(model.ApprovalGrantRecord{
		ID:          "grant-1",
		HostID:      hostID,
		HostScope:   "host",
		GrantType:   "command",
		Fingerprint: fingerprint,
		Command:     "ls -la",
		CreatedBy:   "test",
		Status:      "active",
	})

	app.store.EnsureSession(sessionID)

	approval := model.ApprovalRequest{
		ID:           "approval-host-grant-1",
		RequestIDRaw: "raw-host-grant-1",
		HostID:       hostID,
		Fingerprint:  fingerprint,
		Type:         "command",
		Status:       "pending",
		ItemID:       "card-host-grant-1",
		Command:      "ls -la",
		Cwd:          "/tmp",
		RequestedAt:  now,
	}

	result := app.autoApproveByHostGrant(sessionID, approval)
	if !result {
		t.Fatalf("expected autoApproveByHostGrant to return true")
	}

	select {
	case resp := <-responded:
		m, ok := resp.(map[string]any)
		if !ok {
			t.Fatalf("expected map response, got %T", resp)
		}
		if m["decision"] != "accept" {
			t.Fatalf("expected accept decision, got %v", m["decision"])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected codex response")
	}

	resolved, ok := app.store.Approval(sessionID, "approval-host-grant-1")
	if !ok {
		t.Fatalf("expected approval to be stored")
	}
	if resolved.Status != "accepted_for_host_auto" {
		t.Fatalf("expected status accepted_for_host_auto, got %q", resolved.Status)
	}
}

func TestAutoApproveByHostGrantSkipsWhenNoMatch(t *testing.T) {
	dir := t.TempDir()
	app := New(config.Config{})
	app.approvalGrantStore = store.NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	sessionID := "sess-host-grant-nomatch"
	hostID := "linux-01"
	now := model.NowString()

	app.store.EnsureSession(sessionID)

	approval := model.ApprovalRequest{
		ID:           "approval-host-grant-2",
		RequestIDRaw: "raw-host-grant-2",
		HostID:       hostID,
		Fingerprint:  "command|linux-01|/tmp|rm -rf /",
		Type:         "command",
		Status:       "pending",
		ItemID:       "card-host-grant-2",
		Command:      "rm -rf /",
		Cwd:          "/tmp",
		RequestedAt:  now,
	}

	result := app.autoApproveByHostGrant(sessionID, approval)
	if result {
		t.Fatalf("expected autoApproveByHostGrant to return false when no matching grant")
	}
}

func TestAutoApproveByHostGrantSkipsExpiredGrant(t *testing.T) {
	dir := t.TempDir()
	app := New(config.Config{})
	app.approvalGrantStore = store.NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	sessionID := "sess-host-grant-expired"
	hostID := "linux-01"
	now := model.NowString()
	fingerprint := "command|linux-01|/tmp|ls -la"

	app.store.EnsureSession(sessionID)

	// Add an expired grant
	_ = app.approvalGrantStore.Add(model.ApprovalGrantRecord{
		ID:          "grant-expired",
		HostID:      hostID,
		HostScope:   "host",
		GrantType:   "command",
		Fingerprint: fingerprint,
		Command:     "ls -la",
		CreatedBy:   "test",
		ExpiresAt:   "2020-01-01T00:00:00Z",
		Status:      "active",
	})

	approval := model.ApprovalRequest{
		ID:           "approval-host-grant-3",
		RequestIDRaw: "raw-host-grant-3",
		HostID:       hostID,
		Fingerprint:  fingerprint,
		Type:         "command",
		Status:       "pending",
		ItemID:       "card-host-grant-3",
		Command:      "ls -la",
		Cwd:          "/tmp",
		RequestedAt:  now,
	}

	result := app.autoApproveByHostGrant(sessionID, approval)
	if result {
		t.Fatalf("expected autoApproveByHostGrant to return false for expired grant")
	}
}
