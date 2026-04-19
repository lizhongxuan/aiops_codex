package server

import (
	"context"
	"io"
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

type remoteStatusUnifyAgentStream struct {
	mu       sync.Mutex
	messages []*agentrpc.Envelope
	onSend   func(*agentrpc.Envelope) error
}

func (s *remoteStatusUnifyAgentStream) SetHeader(_ metadata.MD) error { return nil }

func (s *remoteStatusUnifyAgentStream) SendHeader(_ metadata.MD) error { return nil }

func (s *remoteStatusUnifyAgentStream) SetTrailer(_ metadata.MD) {}

func (s *remoteStatusUnifyAgentStream) Context() context.Context { return context.Background() }

func (s *remoteStatusUnifyAgentStream) Send(msg *agentrpc.Envelope) error {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.mu.Unlock()
	if s.onSend != nil {
		return s.onSend(msg)
	}
	return nil
}

func (s *remoteStatusUnifyAgentStream) Recv() (*agentrpc.Envelope, error) { return nil, io.EOF }

func (s *remoteStatusUnifyAgentStream) SendMsg(any) error { return nil }

func (s *remoteStatusUnifyAgentStream) RecvMsg(any) error { return io.EOF }

func newRemoteStatusUnifyApp(t *testing.T, sessionID, hostID string) *App {
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

func assertNoResultSummaryCard(t *testing.T, session *store.SessionState) {
	t.Helper()
	for _, card := range session.Cards {
		if card.Type == "ResultSummaryCard" {
			t.Fatalf("expected no ResultSummaryCard, got %#v", card)
		}
	}
}

func assertToolResponseSucceeded(t *testing.T, result any) string {
	t.Helper()
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected tool response map, got %#v", result)
	}
	if payload["success"] != true {
		t.Fatalf("expected tool response success=true, got %#v", payload)
	}
	items, ok := payload["contentItems"].([]map[string]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected content items in tool response, got %#v", payload)
	}
	text, _ := items[0]["text"].(string)
	if strings.TrimSpace(text) == "" {
		t.Fatalf("expected non-empty tool response text, got %#v", payload)
	}
	return text
}

func TestRemoteListFilesKeepsProcessStateButNoResultSummaryCard(t *testing.T) {
	app := newRemoteStatusUnifyApp(t, "sess-remote-list", "linux-01")
	responded := make(chan any, 1)

	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-remote-list" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	stream := &remoteStatusUnifyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind != "file/list" || msg.FileListRequest == nil {
				t.Fatalf("expected file/list request, got %#v", msg)
			}
			app.handleAgentFileListResult("linux-01", &agentrpc.FileListResult{
				RequestID: msg.FileListRequest.RequestID,
				Path:      "/etc/nginx",
				Entries: []agentrpc.FileEntry{
					{Name: "nginx.conf", Path: "/etc/nginx/nginx.conf", Kind: "file", Size: 2048},
					{Name: "conf.d", Path: "/etc/nginx/conf.d", Kind: "dir"},
				},
			})
			return nil
		},
	}
	app.setAgentConnection("linux-01", &agentConnection{hostID: "linux-01", stream: stream})

	app.handleDynamicToolCall("raw-remote-list", map[string]any{
		"threadId": "thread-sess-remote-list",
		"turnId":   "turn-sess-remote-list",
		"callId":   "call-remote-list",
		"tool":     "list_remote_files",
		"arguments": map[string]any{
			"host":        "linux-01",
			"path":        "/etc/nginx",
			"recursive":   true,
			"max_entries": 10,
			"reason":      "inspect nginx files",
		},
	})

	select {
	case result := <-responded:
		text := assertToolResponseSucceeded(t, result)
		if !strings.Contains(text, "目录 `/etc/nginx`") {
			t.Fatalf("expected list response text, got %q", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for list response")
	}

	session := app.store.Session("sess-remote-list")
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Cards) != 1 {
		t.Fatalf("expected only process card, got %#v", session.Cards)
	}
	card := app.cardByID("sess-remote-list", "process-toolcmd-call-remote-list")
	if card == nil {
		t.Fatalf("expected process card to exist")
	}
	if card.Status != "completed" {
		t.Fatalf("expected completed process card, got %q", card.Status)
	}
	if !strings.Contains(card.Text, "已列出 /etc/nginx") {
		t.Fatalf("expected completion text, got %q", card.Text)
	}
	display, ok := card.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display, got %#v", card.Detail)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) < 2 {
		t.Fatalf("expected structured blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[0], "kind") != ToolDisplayBlockResultStats || getStringAny(blocks[1], "kind") != ToolDisplayBlockLinkList {
		t.Fatalf("expected result_stats + link_list blocks, got %#v", blocks)
	}
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected turn to return to thinking, got %q", session.Runtime.Turn.Phase)
	}
	if session.Runtime.Activity.CurrentListingPath != "" || session.Runtime.Activity.ListCount != 1 {
		t.Fatalf("expected listing activity to clear and count once, got %#v", session.Runtime.Activity)
	}
	events := app.toolEventStore.SessionEvents("sess-remote-list")
	if len(events) < 2 {
		t.Fatalf("expected lifecycle events for remote list tool, got %#v", events)
	}
	if events[0].Type != string(ToolLifecycleEventStarted) || events[len(events)-1].Type != string(ToolLifecycleEventCompleted) {
		t.Fatalf("expected started/completed lifecycle events, got %#v", events)
	}
	assertNoResultSummaryCard(t, session)
}

func TestRemoteReadFileKeepsProcessStateButNoResultSummaryCard(t *testing.T) {
	app := newRemoteStatusUnifyApp(t, "sess-remote-read", "linux-02")
	responded := make(chan any, 1)

	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-remote-read" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	stream := &remoteStatusUnifyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind != "file/read" || msg.FileReadRequest == nil {
				t.Fatalf("expected file/read request, got %#v", msg)
			}
			app.handleAgentFileReadResult("linux-02", &agentrpc.FileReadResult{
				RequestID: msg.FileReadRequest.RequestID,
				Path:      "/etc/nginx/nginx.conf",
				Content:   "user nginx;\nworker_processes auto;\n",
			})
			return nil
		},
	}
	app.setAgentConnection("linux-02", &agentConnection{hostID: "linux-02", stream: stream})

	app.handleDynamicToolCall("raw-remote-read", map[string]any{
		"threadId": "thread-sess-remote-read",
		"turnId":   "turn-sess-remote-read",
		"callId":   "call-remote-read",
		"tool":     "read_remote_file",
		"arguments": map[string]any{
			"host":      "linux-02",
			"path":      "/etc/nginx/nginx.conf",
			"max_bytes": 4096,
			"reason":    "inspect config",
		},
	})

	select {
	case result := <-responded:
		text := assertToolResponseSucceeded(t, result)
		if !strings.Contains(text, "Read file /etc/nginx/nginx.conf") {
			t.Fatalf("expected read response text, got %q", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for read response")
	}

	session := app.store.Session("sess-remote-read")
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Cards) != 1 {
		t.Fatalf("expected only process card, got %#v", session.Cards)
	}
	card := app.cardByID("sess-remote-read", "process-toolcmd-call-remote-read")
	if card == nil {
		t.Fatalf("expected process card to exist")
	}
	if card.Status != "completed" {
		t.Fatalf("expected completed process card, got %q", card.Status)
	}
	if !strings.Contains(card.Text, "已浏览 /etc/nginx/nginx.conf") {
		t.Fatalf("expected completion text, got %q", card.Text)
	}
	display, ok := card.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display, got %#v", card.Detail)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) < 2 {
		t.Fatalf("expected structured blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[1], "kind") != ToolDisplayBlockFilePreview {
		t.Fatalf("expected file_preview block, got %#v", blocks)
	}
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected turn to return to thinking, got %q", session.Runtime.Turn.Phase)
	}
	if session.Runtime.Activity.CurrentReadingFile != "" || session.Runtime.Activity.FilesViewed != 1 {
		t.Fatalf("expected reading activity to clear and count once, got %#v", session.Runtime.Activity)
	}
	if len(session.Runtime.Activity.ViewedFiles) != 1 || session.Runtime.Activity.ViewedFiles[0].Path != "/etc/nginx/nginx.conf" {
		t.Fatalf("expected viewed file activity to update, got %#v", session.Runtime.Activity.ViewedFiles)
	}
	events := app.toolEventStore.SessionEvents("sess-remote-read")
	if len(events) < 2 {
		t.Fatalf("expected lifecycle events for remote read tool, got %#v", events)
	}
	if events[0].Type != string(ToolLifecycleEventStarted) || events[len(events)-1].Type != string(ToolLifecycleEventCompleted) {
		t.Fatalf("expected started/completed lifecycle events, got %#v", events)
	}
	assertNoResultSummaryCard(t, session)
}

func TestRemoteSearchFilesKeepsProcessStateButNoResultSummaryCard(t *testing.T) {
	app := newRemoteStatusUnifyApp(t, "sess-remote-search", "linux-03")
	responded := make(chan any, 1)

	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-remote-search" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	stream := &remoteStatusUnifyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind != "file/search" || msg.FileSearchRequest == nil {
				t.Fatalf("expected file/search request, got %#v", msg)
			}
			app.handleAgentFileSearchResult("linux-03", &agentrpc.FileSearchResult{
				RequestID: msg.FileSearchRequest.RequestID,
				Path:      "/etc/nginx",
				Query:     "server_name",
				Matches: []agentrpc.FileMatch{
					{Path: "/etc/nginx/nginx.conf", Line: 12, Preview: "server_name example.com;"},
					{Path: "/etc/nginx/conf.d/app.conf", Line: 7, Preview: "server_name app.example.com;"},
				},
			})
			return nil
		},
	}
	app.setAgentConnection("linux-03", &agentConnection{hostID: "linux-03", stream: stream})

	app.handleDynamicToolCall("raw-remote-search", map[string]any{
		"threadId": "thread-sess-remote-search",
		"turnId":   "turn-sess-remote-search",
		"callId":   "call-remote-search",
		"tool":     "search_remote_files",
		"arguments": map[string]any{
			"host":        "linux-03",
			"path":        "/etc/nginx",
			"query":       "server_name",
			"max_matches": 20,
			"reason":      "inspect search hits",
		},
	})

	select {
	case result := <-responded:
		text := assertToolResponseSucceeded(t, result)
		if !strings.Contains(text, "在 `/etc/nginx` 中搜索 `server_name`") {
			t.Fatalf("expected search response text, got %q", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for search response")
	}

	session := app.store.Session("sess-remote-search")
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Cards) != 1 {
		t.Fatalf("expected only process card, got %#v", session.Cards)
	}
	card := app.cardByID("sess-remote-search", "process-toolcmd-call-remote-search")
	if card == nil {
		t.Fatalf("expected process card to exist")
	}
	if card.Status != "completed" {
		t.Fatalf("expected completed process card, got %q", card.Status)
	}
	if !strings.Contains(card.Text, "已搜索内容（命中 2 个位置）") {
		t.Fatalf("expected completion text, got %q", card.Text)
	}
	display, ok := card.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display, got %#v", card.Detail)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) < 3 {
		t.Fatalf("expected structured blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[0], "kind") != ToolDisplayBlockSearchQueries || getStringAny(blocks[2], "kind") != ToolDisplayBlockLinkList {
		t.Fatalf("expected search_queries + link_list blocks, got %#v", blocks)
	}
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected turn to return to thinking, got %q", session.Runtime.Turn.Phase)
	}
	if session.Runtime.Activity.CurrentSearchKind != "" || session.Runtime.Activity.CurrentSearchQuery != "" {
		t.Fatalf("expected search activity to clear, got %#v", session.Runtime.Activity)
	}
	if session.Runtime.Activity.SearchCount != 1 || session.Runtime.Activity.SearchLocationCount != 2 {
		t.Fatalf("expected search counters to update, got %#v", session.Runtime.Activity)
	}
	if len(session.Runtime.Activity.SearchedContentQueries) != 1 {
		t.Fatalf("expected one search activity entry, got %#v", session.Runtime.Activity.SearchedContentQueries)
	}
	events := app.toolEventStore.SessionEvents("sess-remote-search")
	if len(events) < 2 {
		t.Fatalf("expected lifecycle events for remote search tool, got %#v", events)
	}
	if events[0].Type != string(ToolLifecycleEventStarted) || events[len(events)-1].Type != string(ToolLifecycleEventCompleted) {
		t.Fatalf("expected started/completed lifecycle events, got %#v", events)
	}
	assertNoResultSummaryCard(t, session)
}
