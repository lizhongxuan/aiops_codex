package server

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"google.golang.org/grpc/metadata"
)

type dynamicToolPolicyAgentStream struct {
	mu       sync.Mutex
	messages []*agentrpc.Envelope
	onSend   func(*agentrpc.Envelope) error
}

func (s *dynamicToolPolicyAgentStream) SetHeader(_ metadata.MD) error { return nil }

func (s *dynamicToolPolicyAgentStream) SendHeader(_ metadata.MD) error { return nil }

func (s *dynamicToolPolicyAgentStream) SetTrailer(_ metadata.MD) {}

func (s *dynamicToolPolicyAgentStream) Context() context.Context { return context.Background() }

func (s *dynamicToolPolicyAgentStream) Send(msg *agentrpc.Envelope) error {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.mu.Unlock()
	if s.onSend != nil {
		return s.onSend(msg)
	}
	return nil
}

func (s *dynamicToolPolicyAgentStream) Recv() (*agentrpc.Envelope, error) { return nil, io.EOF }

func (s *dynamicToolPolicyAgentStream) SendMsg(any) error { return nil }

func (s *dynamicToolPolicyAgentStream) RecvMsg(any) error { return io.EOF }

func (s *dynamicToolPolicyAgentStream) kinds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	kinds := make([]string, 0, len(s.messages))
	for _, msg := range s.messages {
		kinds = append(kinds, msg.Kind)
	}
	return kinds
}

func newRemoteDynamicToolPolicyApp(t *testing.T, sessionID, hostID string) *App {
	t.Helper()

	app := New(config.Config{})
	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, hostID)
	app.store.SetThread(sessionID, "thread-"+sessionID)
	app.store.SetTurn(sessionID, "turn-"+sessionID)
	app.store.UpsertHost(model.Host{
		ID:         hostID,
		Name:       hostID,
		Kind:       "agent",
		Status:     "online",
		Executable: true,
	})
	return app
}

func TestRemoteDynamicToolReadOnlyQueryDoesNotCreateApproval(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-readonly-policy", "linux-01")
	responded := make(chan any, 1)

	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-readonly-policy" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	stream := &dynamicToolPolicyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind != "exec/start" || msg.ExecStart == nil {
				t.Fatalf("expected exec/start envelope, got %#v", msg)
			}
			if !msg.ExecStart.Readonly {
				t.Fatalf("expected readonly exec start")
			}
			if msg.ExecStart.Command != "uptime" {
				t.Fatalf("expected uptime command, got %q", msg.ExecStart.Command)
			}
			app.handleAgentExecOutput("linux-01", &agentrpc.ExecOutput{
				ExecID: msg.ExecStart.ExecID,
				Stream: "stdout",
				Data:   "load average: 0.20 0.15 0.10\n",
			})
			app.handleAgentExecExit("linux-01", &agentrpc.ExecExit{
				ExecID:   msg.ExecStart.ExecID,
				ExitCode: 0,
				Status:   "completed",
				Stdout:   "load average: 0.20 0.15 0.10\n",
			})
			return nil
		},
	}
	app.setAgentConnection("linux-01", &agentConnection{hostID: "linux-01", stream: stream})

	app.handleDynamicToolCall("raw-readonly-policy", map[string]any{
		"threadId": "thread-sess-readonly-policy",
		"turnId":   "turn-sess-readonly-policy",
		"callId":   "call-readonly-policy",
		"tool":     "execute_readonly_query",
		"arguments": map[string]any{
			"host":    "linux-01",
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
		if payload["success"] != true {
			t.Fatalf("expected readonly query to succeed, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for readonly query response")
	}

	session := app.store.Session("sess-readonly-policy")
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Approvals) != 0 {
		t.Fatalf("expected no approvals for readonly query, got %#v", session.Approvals)
	}
	if len(session.Cards) != 1 {
		t.Fatalf("expected only command card for readonly query, got %#v", session.Cards)
	}
	card := app.cardByID("sess-readonly-policy", dynamicToolCardID("call-readonly-policy"))
	if card == nil {
		t.Fatalf("expected command card to exist")
	}
	if card.Type != "CommandCard" || card.Status != "completed" {
		t.Fatalf("expected completed CommandCard, got %#v", card)
	}
	if !strings.Contains(card.Output, "load average") {
		t.Fatalf("expected command output to be preserved on card, got %#v", card)
	}
	events := app.toolEventStore.SessionEvents("sess-readonly-policy")
	if len(events) < 2 {
		t.Fatalf("expected lifecycle events for readonly query, got %#v", events)
	}
	if events[0].Type != string(ToolLifecycleEventStarted) || events[len(events)-1].Type != string(ToolLifecycleEventCompleted) {
		t.Fatalf("expected started/completed lifecycle events, got %#v", events)
	}
	if len(stream.kinds()) != 1 || stream.kinds()[0] != "exec/start" {
		t.Fatalf("expected one exec/start envelope, got %#v", stream.kinds())
	}
}

func TestQueryAIServerStateUsesLifecycleWithoutExtraProcessCard(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "sess-state-policy"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-"+sessionID)
	app.store.SetTurn(sessionID, "turn-"+sessionID)
	app.store.UpsertHost(model.Host{ID: "db-01", Name: "db-01", Kind: "remote", Status: "online", Executable: true})

	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-state-policy" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	app.handleDynamicToolCall("raw-state-policy", map[string]any{
		"threadId": "thread-" + sessionID,
		"turnId":   "turn-" + sessionID,
		"callId":   "call-state-policy",
		"tool":     "query_ai_server_state",
		"arguments": map[string]any{
			"focus": "runtime",
		},
	})

	select {
	case result := <-responded:
		payload, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected tool response map, got %#v", result)
		}
		if payload["success"] != true {
			t.Fatalf("expected state query to succeed, got %#v", payload)
		}
		text := toolResponseText(t, payload)
		if strings.HasPrefix(text, "{") {
			var structured map[string]any
			if err := json.Unmarshal([]byte(text), &structured); err != nil {
				t.Fatalf("expected JSON payload or readable text, got %q: %v", text, err)
			}
			if got := getStringAny(structured, "sessionId"); got != sessionID {
				t.Fatalf("expected session id %q, got %#v", sessionID, structured)
			}
			if got, _ := getIntAny(structured, "hostCount"); got != 2 {
				t.Fatalf("expected host count 2 including server-local, got %#v", structured)
			}
		} else {
			if !strings.Contains(text, sessionID) || !strings.Contains(text, "hostCount") {
				t.Fatalf("expected readable state payload, got %q", text)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for state query response")
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Cards) != 1 {
		t.Fatalf("expected only workspace result card, got %#v", session.Cards)
	}
	card := app.cardByID(sessionID, dynamicToolCardID("raw-state-policy"))
	if card == nil {
		t.Fatalf("expected workspace result card to exist")
	}
	if card.Type != "WorkspaceResultCard" || card.Status != "completed" {
		t.Fatalf("expected completed workspace result card, got %#v", card)
	}
	events := app.toolEventStore.SessionEvents(sessionID)
	if len(events) < 2 {
		t.Fatalf("expected lifecycle events for state query, got %#v", events)
	}
	if events[0].Type != string(ToolLifecycleEventStarted) || events[len(events)-1].Type != string(ToolLifecycleEventCompleted) {
		t.Fatalf("expected started/completed lifecycle events, got %#v", events)
	}
}

func TestRemoteDynamicToolMutationCommandCreatesPendingApproval(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-command-policy", "linux-02")
	responded := make(chan any, 1)

	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		responded <- map[string]any{
			"rawID":  rawID,
			"result": result,
		}
		return nil
	}

	app.handleDynamicToolCall("raw-command-policy", map[string]any{
		"threadId": "thread-sess-command-policy",
		"turnId":   "turn-sess-command-policy",
		"callId":   "call-command-policy",
		"tool":     "execute_system_mutation",
		"arguments": map[string]any{
			"host":    "linux-02",
			"mode":    "command",
			"command": "systemctl restart nginx",
			"cwd":     "/etc/nginx",
			"reason":  "restart nginx",
		},
	})

	select {
	case got := <-responded:
		t.Fatalf("expected no immediate tool response for pending approval, got %#v", got)
	default:
	}

	session := app.store.Session("sess-command-policy")
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Approvals) != 1 {
		t.Fatalf("expected one approval, got %#v", session.Approvals)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
		break
	}
	if approval.Type != "remote_command" {
		t.Fatalf("expected remote_command approval, got %#v", approval.Type)
	}
	if approval.Status != "pending" {
		t.Fatalf("expected pending approval, got %#v", approval.Status)
	}
	if approval.Command != "systemctl restart nginx" {
		t.Fatalf("unexpected approval command %#v", approval.Command)
	}
	if approval.Cwd != "/etc/nginx" {
		t.Fatalf("unexpected approval cwd %#v", approval.Cwd)
	}
	if len(session.Cards) != 1 {
		t.Fatalf("expected one approval card, got %#v", session.Cards)
	}
	card := session.Cards[0]
	if card.Type != "CommandApprovalCard" {
		t.Fatalf("expected CommandApprovalCard, got %#v", card.Type)
	}
	if card.Status != "pending" {
		t.Fatalf("expected pending card, got %#v", card.Status)
	}
}

func TestWorkspaceRiskGateRejectsMutationBeforePlanApproval(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "sess-risk-gate-plan"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-"+sessionID)
	app.store.SetTurn(sessionID, "turn-"+sessionID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "plan-card-risk-gate",
		Type:      "PlanCard",
		Status:    "inProgress",
		CreatedAt: "2026-04-15T12:00:00Z",
		UpdatedAt: "2026-04-15T12:00:00Z",
		Detail: map[string]any{
			"tool": "update_plan",
		},
	})

	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-risk-gate-plan" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		respondedPayload, _ = result.(map[string]any)
		return nil
	}

	app.handleDynamicToolCall("raw-risk-gate-plan", map[string]any{
		"threadId": "thread-" + sessionID,
		"turnId":   "turn-" + sessionID,
		"callId":   "call-risk-gate-plan",
		"tool":     "execute_system_mutation",
		"arguments": map[string]any{
			"host":    model.ServerLocalHostID,
			"mode":    "command",
			"command": "systemctl restart nginx",
			"reason":  "restart nginx",
		},
	})

	if respondedPayload["success"] != false {
		t.Fatalf("expected mutation to be rejected before plan approval, got %#v", respondedPayload)
	}
	if text := toolResponseText(t, respondedPayload); !strings.Contains(text, "计划审批通过前不能执行变更命令") {
		t.Fatalf("expected explainable risk gate error, got %q", text)
	}
	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Approvals) != 0 {
		t.Fatalf("expected no approvals to be created before plan approval, got %#v", session.Approvals)
	}
}

func TestRemoteDynamicToolMutationFileChangeCreatesPendingApproval(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-file-policy", "linux-03")
	responded := make(chan any, 1)
	readCount := 0
	writeCount := 0

	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		responded <- map[string]any{
			"rawID":  rawID,
			"result": result,
		}
		return nil
	}

	stream := &dynamicToolPolicyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			switch {
			case msg.FileReadRequest != nil:
				readCount++
				app.handleAgentFileReadResult("linux-03", &agentrpc.FileReadResult{
					RequestID: msg.FileReadRequest.RequestID,
					Path:      msg.FileReadRequest.Path,
					Content:   "old-value\n",
				})
			case msg.FileWriteRequest != nil:
				writeCount++
			}
			return nil
		},
	}
	app.setAgentConnection("linux-03", &agentConnection{hostID: "linux-03", stream: stream})

	app.handleDynamicToolCall("raw-file-policy", map[string]any{
		"threadId": "thread-sess-file-policy",
		"turnId":   "turn-sess-file-policy",
		"callId":   "call-file-policy",
		"tool":     "execute_system_mutation",
		"arguments": map[string]any{
			"host":       "linux-03",
			"mode":       "file_change",
			"path":       "/etc/app.conf",
			"content":    "new-value\n",
			"write_mode": "overwrite",
			"reason":     "update config",
		},
	})

	select {
	case got := <-responded:
		t.Fatalf("expected no immediate tool response for pending approval, got %#v", got)
	default:
	}

	if readCount != 1 {
		t.Fatalf("expected one file read while preparing approval, got %d", readCount)
	}
	if writeCount != 0 {
		t.Fatalf("expected no file write before approval, got %d", writeCount)
	}
	if kinds := stream.kinds(); len(kinds) != 1 || kinds[0] != "file/read" {
		t.Fatalf("expected one file/read envelope, got %#v", kinds)
	}

	session := app.store.Session("sess-file-policy")
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Approvals) != 1 {
		t.Fatalf("expected one approval, got %#v", session.Approvals)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
		break
	}
	if approval.Type != "remote_file_change" {
		t.Fatalf("expected remote_file_change approval, got %#v", approval.Type)
	}
	if approval.Status != "pending" {
		t.Fatalf("expected pending approval, got %#v", approval.Status)
	}
	if approval.Changes == nil || len(approval.Changes) != 1 {
		t.Fatalf("expected one file change in approval, got %#v", approval.Changes)
	}
	if approval.Changes[0].Path != "/etc/app.conf" {
		t.Fatalf("unexpected file change path %#v", approval.Changes[0].Path)
	}
}
