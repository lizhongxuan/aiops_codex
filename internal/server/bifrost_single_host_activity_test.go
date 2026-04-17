package server

import (
	"context"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/config"
)

func TestSingleHostOnToolStartUpdatesRuntimeActivityWithoutProcessCards(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-single-host-tool-start"
	app.store.EnsureSession(sessionID)

	app.OnToolStart(context.Background(), &agentloop.Session{ID: sessionID}, "web_search", map[string]interface{}{
		"query": "BTC price now",
	})

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if session.Runtime.Activity.CurrentSearchKind != "web" {
		t.Fatalf("expected currentSearchKind=web, got %q", session.Runtime.Activity.CurrentSearchKind)
	}
	if session.Runtime.Activity.CurrentSearchQuery != "BTC price now" {
		t.Fatalf("expected currentSearchQuery to be set, got %q", session.Runtime.Activity.CurrentSearchQuery)
	}
	if session.Runtime.Activity.CurrentWebSearchQuery != "BTC price now" {
		t.Fatalf("expected currentWebSearchQuery to be set, got %q", session.Runtime.Activity.CurrentWebSearchQuery)
	}
	if len(session.Cards) != 0 {
		t.Fatalf("expected no process cards for single_host session, got %d cards", len(session.Cards))
	}
}

func TestSingleHostOnToolCompleteUpdatesRuntimeActivityWithoutProcessCards(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-single-host-tool-complete"
	app.store.EnsureSession(sessionID)

	args := map[string]interface{}{
		"query": "A股 指数 快照",
	}
	app.OnToolStart(context.Background(), &agentloop.Session{ID: sessionID}, "web_search", args)
	app.OnToolComplete(context.Background(), &agentloop.Session{ID: sessionID}, "web_search", args, `{"ok":true}`, nil)

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if session.Runtime.Activity.CurrentSearchKind != "" || session.Runtime.Activity.CurrentSearchQuery != "" || session.Runtime.Activity.CurrentWebSearchQuery != "" {
		t.Fatalf("expected live search state to clear, got %#v", session.Runtime.Activity)
	}
	if session.Runtime.Activity.SearchCount != 1 {
		t.Fatalf("expected search count to increment, got %d", session.Runtime.Activity.SearchCount)
	}
	if len(session.Runtime.Activity.SearchedWebQueries) != 1 {
		t.Fatalf("expected one searched web query entry, got %d", len(session.Runtime.Activity.SearchedWebQueries))
	}
	if session.Runtime.Activity.SearchedWebQueries[0].Query != "A股 指数 快照" {
		t.Fatalf("expected searched query to be recorded, got %#v", session.Runtime.Activity.SearchedWebQueries[0])
	}
	if len(session.Cards) != 0 {
		t.Fatalf("expected no process cards for single_host session, got %d cards", len(session.Cards))
	}
}
