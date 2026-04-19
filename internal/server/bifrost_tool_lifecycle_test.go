package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestWorkspaceOnToolStartEmitsLifecycleEventForReadonlyTool(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-workspace-tool-start"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)

	subscriber := &collectingToolSubscriber{}
	app.toolEventBus.Subscribe(subscriber)

	app.OnToolStart(context.Background(), &agentloop.Session{ID: sessionID}, "read_file", map[string]interface{}{
		"path": "/etc/hosts",
	})

	if len(subscriber.events) != 1 {
		t.Fatalf("expected one lifecycle event, got %d", len(subscriber.events))
	}
	event := subscriber.events[0]
	if event.Type != ToolLifecycleEventStarted {
		t.Fatalf("expected started event, got %#v", event)
	}
	if event.ToolName != "read_file" {
		t.Fatalf("expected read_file event, got %#v", event)
	}

	rawCardID, ok := app.bifrostToolCards.Load(sessionID + ":read_file")
	if !ok {
		t.Fatal("expected bifrost tool card tracking entry")
	}
	cardID, _ := rawCardID.(string)
	if cardID == "" || cardID != event.CardID {
		t.Fatalf("expected tracked card id to match event card id, got tracked=%q event=%q", cardID, event.CardID)
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session %q", sessionID)
	}
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected normalized thinking phase, got %q", session.Runtime.Turn.Phase)
	}
	if session.Runtime.Activity.CurrentReadingFile != "/etc/hosts" {
		t.Fatalf("expected current reading file to be set, got %#v", session.Runtime.Activity)
	}
	card := app.cardByID(sessionID, cardID)
	if card == nil || card.Status != "inProgress" {
		t.Fatalf("expected process card to be created by projection, got %#v", card)
	}
}

func TestWorkspaceOnToolCompleteEmitsLifecycleEventForReadonlyTool(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-workspace-tool-complete"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)

	subscriber := &collectingToolSubscriber{}
	app.toolEventBus.Subscribe(subscriber)

	args := map[string]interface{}{
		"query": "server_name",
		"path":  "/etc/nginx",
	}
	app.OnToolStart(context.Background(), &agentloop.Session{ID: sessionID}, "search_files", args)
	app.OnToolComplete(context.Background(), &agentloop.Session{ID: sessionID}, "search_files", args, `{"matches":2}`, nil)

	if len(subscriber.events) != 2 {
		t.Fatalf("expected started and completed events, got %d", len(subscriber.events))
	}
	if subscriber.events[1].Type != ToolLifecycleEventCompleted {
		t.Fatalf("expected completed event, got %#v", subscriber.events[1])
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session %q", sessionID)
	}
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected turn phase to return to thinking, got %q", session.Runtime.Turn.Phase)
	}
	if session.Runtime.Activity.CurrentSearchKind != "" || session.Runtime.Activity.CurrentSearchQuery != "" {
		t.Fatalf("expected active search state to clear, got %#v", session.Runtime.Activity)
	}
	if session.Runtime.Activity.SearchCount != 1 {
		t.Fatalf("expected search count to increment, got %#v", session.Runtime.Activity)
	}
	if len(session.Runtime.Activity.SearchedContentQueries) != 1 || session.Runtime.Activity.SearchedContentQueries[0].Query != "server_name" {
		t.Fatalf("expected searched content query to be recorded, got %#v", session.Runtime.Activity.SearchedContentQueries)
	}

	rawCardID, ok := app.bifrostToolCards.Load(sessionID + ":search_files")
	if ok {
		t.Fatalf("expected completion to clear bifrost card tracking, got %v", rawCardID)
	}

	cardID := subscriber.events[0].CardID
	card := app.cardByID(sessionID, cardID)
	if card == nil {
		t.Fatalf("expected process card %q to exist", cardID)
	}
	if card.Status != "completed" {
		t.Fatalf("expected process card to complete, got %#v", card)
	}
	if card.Text != "搜索文件：server_name" {
		t.Fatalf("expected process card text to preserve tool label, got %#v", card)
	}
}

func TestWorkspaceOnCommandLifecycleUsesUnifiedEventsWithoutChangingFileActivity(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-workspace-command-lifecycle"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)

	subscriber := &collectingToolSubscriber{}
	app.toolEventBus.Subscribe(subscriber)

	args := map[string]interface{}{"command": "pwd"}
	app.OnToolStart(context.Background(), &agentloop.Session{ID: sessionID}, "execute_command", args)

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session %q", sessionID)
	}
	if session.Runtime.Activity.CurrentChangingFile != "" {
		t.Fatalf("expected command start to keep currentChangingFile empty, got %#v", session.Runtime.Activity)
	}

	app.OnToolComplete(context.Background(), &agentloop.Session{ID: sessionID}, "execute_command", args, "pwd output", nil)

	if len(subscriber.events) != 2 {
		t.Fatalf("expected command lifecycle to emit two events, got %d", len(subscriber.events))
	}
	if subscriber.events[1].Type != ToolLifecycleEventCompleted {
		t.Fatalf("expected command completion event, got %#v", subscriber.events[1])
	}
	if session = app.store.Session(sessionID); session.Runtime.Activity.CommandsRun != 1 {
		t.Fatalf("expected command count to increment, got %#v", session.Runtime.Activity)
	}
	card := app.cardByID(sessionID, subscriber.events[0].CardID)
	if card == nil || card.Status != "completed" {
		t.Fatalf("expected command process card to complete, got %#v", card)
	}
}

func TestWorkspaceOnWriteFileLifecycleUsesUnifiedEvents(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-workspace-write-lifecycle"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)

	subscriber := &collectingToolSubscriber{}
	app.toolEventBus.Subscribe(subscriber)

	args := map[string]interface{}{"path": "/tmp/demo.txt"}
	app.OnToolStart(context.Background(), &agentloop.Session{ID: sessionID}, "write_file", args)

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session %q", sessionID)
	}
	if session.Runtime.Activity.CurrentChangingFile != "" {
		t.Fatalf("expected write_file start to preserve legacy empty change tracking, got %#v", session.Runtime.Activity)
	}

	app.OnToolComplete(context.Background(), &agentloop.Session{ID: sessionID}, "write_file", args, "write ok", nil)

	if len(subscriber.events) != 2 {
		t.Fatalf("expected write lifecycle to emit two events, got %d", len(subscriber.events))
	}
	if subscriber.events[1].Type != ToolLifecycleEventCompleted {
		t.Fatalf("expected write completion event, got %#v", subscriber.events[1])
	}
	if session = app.store.Session(sessionID); session.Runtime.Activity.FilesChanged != 1 {
		t.Fatalf("expected file change count to increment, got %#v", session.Runtime.Activity)
	}
	card := app.cardByID(sessionID, subscriber.events[0].CardID)
	if card == nil || card.Status != "completed" {
		t.Fatalf("expected write process card to complete, got %#v", card)
	}
}

func TestBifrostLifecycleHelperCoverageForRemainingTools(t *testing.T) {
	cases := []struct {
		tool           string
		wantLifecycle  bool
		wantReadOnly   bool
		wantTrackStart bool
	}{
		{tool: "execute_readonly_query", wantLifecycle: true, wantReadOnly: true, wantTrackStart: false},
		{tool: "query_ai_server_state", wantLifecycle: true, wantReadOnly: true, wantTrackStart: false},
		{tool: "execute_command", wantLifecycle: true, wantReadOnly: false, wantTrackStart: false},
		{tool: "readonly_host_inspect", wantLifecycle: true, wantReadOnly: true, wantTrackStart: false},
		{tool: "write_file", wantLifecycle: true, wantReadOnly: false, wantTrackStart: false},
		{tool: "apply_patch", wantLifecycle: true, wantReadOnly: false, wantTrackStart: false},
	}

	for _, tc := range cases {
		if got := isBifrostLifecycleProjectionTool(tc.tool); got != tc.wantLifecycle {
			t.Fatalf("tool %s lifecycle coverage mismatch: got %v want %v", tc.tool, got, tc.wantLifecycle)
		}
		if got := isBifrostLifecycleReadOnlyTool(tc.tool); got != tc.wantReadOnly {
			t.Fatalf("tool %s readonly mismatch: got %v want %v", tc.tool, got, tc.wantReadOnly)
		}
		if got := shouldBifrostLifecycleTrackActivityStart(tc.tool); got != tc.wantTrackStart {
			t.Fatalf("tool %s activity start mismatch: got %v want %v", tc.tool, got, tc.wantTrackStart)
		}
	}
}

func TestWorkspaceReadonlyQueryLifecycleSkipsProcessCardAndKeepsCommandCard(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-workspace-readonly-query-lifecycle"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)

	subscriber := &collectingToolSubscriber{}
	app.toolEventBus.Subscribe(subscriber)

	loopSession := &agentloop.Session{ID: sessionID}
	args := map[string]any{"command": "pwd"}
	app.OnToolStart(context.Background(), loopSession, "execute_readonly_query", args)
	result, err := app.bifrostExecuteReadonlyQuery(context.Background(), loopSession, bifrost.ToolCall{
		ID:   "call-readonly-query",
		Type: "function",
		Function: bifrost.FunctionCall{
			Name:      "execute_readonly_query",
			Arguments: `{"command":"pwd"}`,
		},
	}, args)
	if err != nil {
		t.Fatalf("bifrost readonly query failed: %v", err)
	}
	app.OnToolComplete(context.Background(), loopSession, "execute_readonly_query", args, result, nil)

	if len(subscriber.events) != 2 {
		t.Fatalf("expected started and completed events, got %d", len(subscriber.events))
	}
	if subscriber.events[0].Type != ToolLifecycleEventStarted || subscriber.events[1].Type != ToolLifecycleEventCompleted {
		t.Fatalf("unexpected lifecycle events: %#v", subscriber.events)
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session %q", sessionID)
	}
	for _, card := range session.Cards {
		if card.Type == "ProcessLineCard" {
			t.Fatalf("expected readonly query to avoid extra process card, got %#v", card)
		}
	}
	commandCardFound := false
	for _, card := range session.Cards {
		if card.Type != "CommandCard" {
			continue
		}
		commandCardFound = true
		if card.Status != "completed" {
			t.Fatalf("expected command card to complete, got %#v", card)
		}
		if strings.TrimSpace(card.Output) == "" {
			t.Fatalf("expected command card output to remain populated, got %#v", card)
		}
	}
	if !commandCardFound {
		t.Fatalf("expected readonly query command card, cards=%#v", session.Cards)
	}
}

func TestWorkspaceQueryAIServerStateLifecycleSkipsProcessCardAndKeepsResultCard(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-workspace-query-state-lifecycle"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)

	subscriber := &collectingToolSubscriber{}
	app.toolEventBus.Subscribe(subscriber)

	loopSession := &agentloop.Session{ID: sessionID}
	args := map[string]any{"focus": "hosts"}
	app.OnToolStart(context.Background(), loopSession, "query_ai_server_state", args)
	result, err := app.bifrostQueryAIServerState(context.Background(), loopSession, bifrost.ToolCall{
		ID:   "call-query-state",
		Type: "function",
		Function: bifrost.FunctionCall{
			Name:      "query_ai_server_state",
			Arguments: `{"focus":"hosts"}`,
		},
	}, args)
	if err != nil {
		t.Fatalf("bifrost query ai server state failed: %v", err)
	}
	app.OnToolComplete(context.Background(), loopSession, "query_ai_server_state", args, result, nil)

	if len(subscriber.events) != 2 {
		t.Fatalf("expected started and completed events, got %d", len(subscriber.events))
	}
	if subscriber.events[0].Type != ToolLifecycleEventStarted || subscriber.events[1].Type != ToolLifecycleEventCompleted {
		t.Fatalf("unexpected lifecycle events: %#v", subscriber.events)
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session %q", sessionID)
	}
	for _, card := range session.Cards {
		if card.Type == "ProcessLineCard" {
			t.Fatalf("expected query_ai_server_state to avoid process cards, got %#v", card)
		}
	}
	resultCard := app.cardByID(sessionID, dynamicToolCardID("call-query-state"))
	if resultCard == nil {
		t.Fatalf("expected workspace result card to exist, cards=%#v", session.Cards)
	}
	if resultCard.Type != "WorkspaceResultCard" || resultCard.Status != "completed" {
		t.Fatalf("unexpected state query card: %#v", resultCard)
	}
	if !strings.Contains(resultCard.Text, "AI Server State") {
		t.Fatalf("expected query card text to contain state payload, got %#v", resultCard)
	}
}

func TestAssistantStreamAndToolLifecycleStayOnSeparateCards(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-assistant-tool-boundary"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)

	subscriber := &collectingToolSubscriber{}
	app.toolEventBus.Subscribe(subscriber)

	loopSession := agentloop.NewSession(sessionID, agentloop.SessionSpec{Model: "test"})
	if err := app.OnAssistantDelta(context.Background(), loopSession, "assistant draft"); err != nil {
		t.Fatalf("assistant delta failed: %v", err)
	}
	assistantCardID := strings.TrimSpace(loopSession.CurrentCardID())
	if assistantCardID == "" {
		t.Fatal("expected current assistant card id")
	}

	toolArgs := map[string]any{"focus": "runtime"}
	app.OnToolStart(context.Background(), loopSession, "query_ai_server_state", toolArgs)
	if _, err := app.bifrostQueryAIServerState(context.Background(), loopSession, bifrost.ToolCall{
		ID:   "call-assistant-boundary",
		Type: "function",
		Function: bifrost.FunctionCall{
			Name:      "query_ai_server_state",
			Arguments: `{"focus":"runtime"}`,
		},
	}, toolArgs); err != nil {
		t.Fatalf("query ai server state failed: %v", err)
	}
	app.OnToolComplete(context.Background(), loopSession, "query_ai_server_state", toolArgs, "done", nil)
	if err := app.OnStreamComplete(context.Background(), loopSession, &agentloop.StreamResult{
		Content:   "assistant draft",
		ToolCalls: []bifrost.ToolCall{{ID: "call-assistant-boundary"}},
	}); err != nil {
		t.Fatalf("stream complete failed: %v", err)
	}

	assistantCard := app.cardByID(sessionID, assistantCardID)
	if assistantCard == nil {
		t.Fatalf("expected assistant card %q", assistantCardID)
	}
	if assistantCard.Type != "AssistantMessageCard" || assistantCard.Status != "completed" {
		t.Fatalf("expected completed assistant card, got %#v", assistantCard)
	}
	if strings.TrimSpace(assistantCard.Text) != "assistant draft" {
		t.Fatalf("expected assistant text to remain isolated, got %#v", assistantCard)
	}
	if resultCard := app.cardByID(sessionID, dynamicToolCardID("call-assistant-boundary")); resultCard == nil || resultCard.ID == assistantCardID {
		t.Fatalf("expected separate query result card, got assistant=%#v result=%#v", assistantCard, resultCard)
	}
	if len(subscriber.events) != 2 {
		t.Fatalf("expected only tool lifecycle events, got %#v", subscriber.events)
	}
	if phase := app.store.Session(sessionID).Runtime.Turn.Phase; phase == "finalizing" {
		t.Fatalf("expected tool-call stream completion to avoid finalizing phase, got %q", phase)
	}
}

func TestBifrostReadonlyRemoteHandlersDoNotDuplicateLifecycleProjection(t *testing.T) {
	type expectations struct {
		processTextContains string
		assertRuntime       func(*testing.T, *model.RuntimeState)
	}
	tests := []struct {
		name       string
		toolName   string
		callID     string
		args       map[string]any
		invoke     func(context.Context, *App, *agentloop.Session, bifrost.ToolCall, map[string]any) (string, error)
		onSend     func(*testing.T, *App, *agentrpc.Envelope)
		expect     expectations
	}{
		{
			name:     "list_files",
			toolName: "list_files",
			callID:   "call-bifrost-list",
			args: map[string]any{
				"host":        "linux-01",
				"path":        "/etc/nginx",
				"recursive":   true,
				"max_entries": 10,
				"reason":      "inspect nginx files",
			},
			invoke: func(ctx context.Context, app *App, session *agentloop.Session, call bifrost.ToolCall, args map[string]any) (string, error) {
				return app.bifrostListRemoteFiles(ctx, session, call, args)
			},
			onSend: func(t *testing.T, app *App, msg *agentrpc.Envelope) {
				t.Helper()
				if msg.Kind != "file/list" || msg.FileListRequest == nil {
					t.Fatalf("expected file/list request, got %#v", msg)
				}
				app.handleAgentFileListResult("linux-01", &agentrpc.FileListResult{
					RequestID: msg.FileListRequest.RequestID,
					Path:      "/etc/nginx",
					Entries: []agentrpc.FileEntry{
						{Name: "nginx.conf", Path: "/etc/nginx/nginx.conf", Kind: "file", Size: 1024},
					},
				})
			},
			expect: expectations{
				processTextContains: "已列出 /etc/nginx",
				assertRuntime: func(t *testing.T, rt *model.RuntimeState) {
					t.Helper()
					if rt.Activity.CurrentListingPath != "" || rt.Activity.ListCount != 1 {
						t.Fatalf("expected one list completion, got %#v", rt.Activity)
					}
				},
			},
		},
		{
			name:     "read_file",
			toolName: "read_file",
			callID:   "call-bifrost-read",
			args: map[string]any{
				"host":      "linux-01",
				"path":      "/etc/nginx/nginx.conf",
				"max_bytes": 4096,
				"reason":    "inspect nginx config",
			},
			invoke: func(ctx context.Context, app *App, session *agentloop.Session, call bifrost.ToolCall, args map[string]any) (string, error) {
				return app.bifrostReadRemoteFile(ctx, session, call, args)
			},
			onSend: func(t *testing.T, app *App, msg *agentrpc.Envelope) {
				t.Helper()
				if msg.Kind != "file/read" || msg.FileReadRequest == nil {
					t.Fatalf("expected file/read request, got %#v", msg)
				}
				app.handleAgentFileReadResult("linux-01", &agentrpc.FileReadResult{
					RequestID: msg.FileReadRequest.RequestID,
					Path:      "/etc/nginx/nginx.conf",
					Content:   "worker_processes auto;\n",
				})
			},
			expect: expectations{
				processTextContains: "已浏览 /etc/nginx/nginx.conf",
				assertRuntime: func(t *testing.T, rt *model.RuntimeState) {
					t.Helper()
					if rt.Activity.CurrentReadingFile != "" || rt.Activity.FilesViewed != 1 {
						t.Fatalf("expected one file view completion, got %#v", rt.Activity)
					}
					if len(rt.Activity.ViewedFiles) != 1 || rt.Activity.ViewedFiles[0].Path != "/etc/nginx/nginx.conf" {
						t.Fatalf("expected viewed file entry, got %#v", rt.Activity.ViewedFiles)
					}
				},
			},
		},
		{
			name:     "search_files",
			toolName: "search_files",
			callID:   "call-bifrost-search",
			args: map[string]any{
				"host":        "linux-01",
				"path":        "/etc/nginx",
				"query":       "server_name",
				"max_matches": 10,
				"reason":      "find vhost references",
			},
			invoke: func(ctx context.Context, app *App, session *agentloop.Session, call bifrost.ToolCall, args map[string]any) (string, error) {
				return app.bifrostSearchRemoteFiles(ctx, session, call, args)
			},
			onSend: func(t *testing.T, app *App, msg *agentrpc.Envelope) {
				t.Helper()
				if msg.Kind != "file/search" || msg.FileSearchRequest == nil {
					t.Fatalf("expected file/search request, got %#v", msg)
				}
				app.handleAgentFileSearchResult("linux-01", &agentrpc.FileSearchResult{
					RequestID: msg.FileSearchRequest.RequestID,
					Path:      "/etc/nginx",
					Query:     "server_name",
					Matches: []agentrpc.FileMatch{
						{Path: "/etc/nginx/nginx.conf", Line: 10, Preview: "server_name localhost;"},
						{Path: "/etc/nginx/sites-enabled/default", Line: 2, Preview: "server_name example.com;"},
					},
				})
			},
			expect: expectations{
				processTextContains: "已搜索内容",
				assertRuntime: func(t *testing.T, rt *model.RuntimeState) {
					t.Helper()
					if rt.Activity.CurrentSearchKind != "" || rt.Activity.CurrentSearchQuery != "" {
						t.Fatalf("expected search activity to clear, got %#v", rt.Activity)
					}
					if rt.Activity.SearchCount != 1 || rt.Activity.SearchLocationCount != 2 {
						t.Fatalf("expected one search and two hit locations, got %#v", rt.Activity)
					}
					if len(rt.Activity.SearchedContentQueries) != 1 || rt.Activity.SearchedContentQueries[0].Query != "server_name" {
						t.Fatalf("expected searched content query entry, got %#v", rt.Activity.SearchedContentQueries)
					}
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := New(config.Config{})
			sessionID := "sess-bifrost-" + tc.name
			app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
				Kind:               model.SessionKindWorkspace,
				Visible:            true,
				WorkspaceSessionID: sessionID,
				RuntimePreset:      model.SessionRuntimePresetWorkspace,
			})
			app.store.SetSelectedHost(sessionID, "linux-01")
			app.store.UpsertHost(model.Host{
				ID:         "linux-01",
				Name:       "linux-01",
				Kind:       "agent",
				Status:     "online",
				Executable: true,
			})

			stream := &remoteStatusUnifyAgentStream{
				onSend: func(msg *agentrpc.Envelope) error {
					tc.onSend(t, app, msg)
					return nil
				},
			}
			app.setAgentConnection("linux-01", &agentConnection{hostID: "linux-01", stream: stream})

			loopSession := &agentloop.Session{ID: sessionID}
			call := bifrost.ToolCall{
				ID:   tc.callID,
				Type: "function",
				Function: bifrost.FunctionCall{
					Name:      tc.toolName,
					Arguments: "{}",
				},
			}

			app.OnToolStart(context.Background(), loopSession, tc.toolName, tc.args)
			result, err := tc.invoke(context.Background(), app, loopSession, call, tc.args)
			app.OnToolComplete(context.Background(), loopSession, tc.toolName, tc.args, result, err)
			if err != nil {
				t.Fatalf("unexpected bifrost handler error: %v", err)
			}

			session := app.store.Session(sessionID)
			if session == nil {
				t.Fatalf("expected session %q", sessionID)
			}
			var processLineCards []*model.Card
			for _, card := range session.Cards {
				if card.Type != "ProcessLineCard" {
					continue
				}
				cardCopy := card
				processLineCards = append(processLineCards, &cardCopy)
			}
			if len(processLineCards) != 1 {
				t.Fatalf("expected exactly one process card, got %d with cards=%#v", len(processLineCards), session.Cards)
			}
			if processLineCards[0].Status != "completed" {
				t.Fatalf("expected completed process card, got %#v", processLineCards[0])
			}
			if !strings.Contains(processLineCards[0].Text, tc.expect.processTextContains) {
				t.Fatalf("expected process text to contain %q, got %#v", tc.expect.processTextContains, processLineCards[0])
			}
			tc.expect.assertRuntime(t, &session.Runtime)
			if phase := session.Runtime.Turn.Phase; phase != "thinking" {
				t.Fatalf("expected turn phase to return to thinking, got %q", phase)
			}
			if _, ok := app.bifrostToolCards.Load(sessionID + ":" + tc.toolName); ok {
				t.Fatalf("expected bifrost tool card tracking to be cleared for %s", tc.toolName)
			}
		})
	}
}

func TestWorkspaceLifecycleDoesNotFallbackToLegacyProcessCardsWhenSubscriberFails(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-workspace-lifecycle-no-fallback"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)
	app.toolEventBus.Subscribe(ToolLifecycleSubscriberFunc(func(context.Context, ToolLifecycleEvent) error {
		return errors.New("projection sidecar failed")
	}))

	session := &agentloop.Session{ID: sessionID}
	args := map[string]any{"path": "/etc/hosts"}
	app.OnToolStart(context.Background(), session, "read_file", args)
	app.OnToolComplete(context.Background(), session, "read_file", args, "127.0.0.1 localhost\n", nil)

	storeSession := app.store.Session(sessionID)
	if storeSession == nil {
		t.Fatalf("expected session %q", sessionID)
	}
	processCards := 0
	for _, card := range storeSession.Cards {
		if card.Type == "ProcessLineCard" {
			processCards++
		}
	}
	if processCards != 1 {
		t.Fatalf("expected exactly one process card despite subscriber failure, got %d with cards=%#v", processCards, storeSession.Cards)
	}
	if storeSession.Runtime.Activity.FilesViewed != 1 {
		t.Fatalf("expected one file view completion, got %#v", storeSession.Runtime.Activity)
	}
}
