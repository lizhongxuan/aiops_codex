package server

import (
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/store"
)

func TestToolProjectionRuntimeTracksPhaseAndActivity(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-runtime-projection"

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:           ToolLifecycleEventStarted,
		SessionID:      sessionID,
		ToolName:       "read_file",
		ActivityTarget: "/etc/hosts",
		Phase:          "executing",
	})

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatal("expected session to exist")
	}
	if session.Runtime.Turn.Phase != "executing" {
		t.Fatalf("expected executing phase, got %q", session.Runtime.Turn.Phase)
	}
	if session.Runtime.Activity.CurrentReadingFile != "/etc/hosts" {
		t.Fatalf("expected current reading file to be tracked, got %#v", session.Runtime.Activity)
	}

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
	})

	session = app.store.Session(sessionID)
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected completed tool to return to thinking, got %q", session.Runtime.Turn.Phase)
	}
	if session.Runtime.Activity.CurrentReadingFile != "" {
		t.Fatalf("expected current activity to be cleared, got %#v", session.Runtime.Activity)
	}
}

func TestToolProjectionRuntimePrefersDisplayActivity(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-runtime-display-activity"

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:           ToolLifecycleEventStarted,
		SessionID:      sessionID,
		ToolName:       "read_file",
		CardID:         "proc-display",
		ActivityTarget: "/tmp/heuristic.txt",
		Phase:          "executing",
		Payload: map[string]any{
			"display": map[string]any{
				"activity": "/tmp/display.txt",
			},
		},
	})

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatal("expected session to exist")
	}
	if session.Runtime.Activity.CurrentReadingFile != "/tmp/display.txt" {
		t.Fatalf("expected display activity to win over heuristic target, got %#v", session.Runtime.Activity)
	}

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
		ToolName:  "read_file",
		Payload: map[string]any{
			"display": map[string]any{
				"activity": "/tmp/display.txt",
			},
			"trackActivityCompletion": true,
		},
	})

	session = app.store.Session(sessionID)
	if session.Runtime.Activity.CurrentReadingFile != "" {
		t.Fatalf("expected display-driven activity to clear on completion, got %#v", session.Runtime.Activity)
	}
}

func TestToolProjectionRuntimeHandlesApprovalTransitions(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-approval-projection"

	app.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
		rt.Activity.CurrentChangingFile = "/tmp/a.txt"
	})

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventApprovalRequested,
		SessionID: sessionID,
	})

	session := app.store.Session(sessionID)
	if session.Runtime.Turn.Phase != "waiting_approval" {
		t.Fatalf("expected waiting_approval phase, got %q", session.Runtime.Turn.Phase)
	}
	if session.Runtime.Activity.CurrentChangingFile != "" {
		t.Fatalf("expected approval request to clear current activity, got %#v", session.Runtime.Activity)
	}

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventApprovalResolved,
		SessionID: sessionID,
	})

	session = app.store.Session(sessionID)
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected approval resolution to return to thinking, got %q", session.Runtime.Turn.Phase)
	}
}

func TestToolProjectionRuntimeTracksCompletionCountersWhenRequested(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-runtime-complete-projection"

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:           ToolLifecycleEventStarted,
		SessionID:      sessionID,
		ToolName:       "search_files",
		ActivityQuery:  "server_name",
		ActivityTarget: "/etc/nginx",
		Phase:          "searching",
	})

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
		ToolName:  "search_files",
		Payload: map[string]any{
			"trackActivityCompletion": true,
			"arguments": map[string]any{
				"query": "server_name",
				"path":  "/etc/nginx",
			},
		},
	})

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatal("expected session to exist")
	}
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected completed tool to return to thinking, got %q", session.Runtime.Turn.Phase)
	}
	if session.Runtime.Activity.CurrentSearchKind != "" || session.Runtime.Activity.CurrentSearchQuery != "" {
		t.Fatalf("expected live search activity to clear, got %#v", session.Runtime.Activity)
	}
	if session.Runtime.Activity.SearchCount != 1 {
		t.Fatalf("expected search count to increment, got %#v", session.Runtime.Activity)
	}
	if len(session.Runtime.Activity.SearchedContentQueries) != 1 || session.Runtime.Activity.SearchedContentQueries[0].Query != "server_name" {
		t.Fatalf("expected searched content query to be recorded, got %#v", session.Runtime.Activity.SearchedContentQueries)
	}
}

func TestToolProjectionRuntimeTracksRemoteFileAliasesWhenRequested(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-runtime-remote-aliases"

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:           ToolLifecycleEventStarted,
		SessionID:      sessionID,
		ToolName:       "list_remote_files",
		ActivityTarget: "/etc/nginx",
		Phase:          "browsing",
	})

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
		ToolName:  "list_remote_files",
		Payload: map[string]any{
			"trackActivityCompletion": true,
			"arguments": map[string]any{
				"path": "/etc/nginx",
			},
		},
	})

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatal("expected session to exist")
	}
	if session.Runtime.Activity.ListCount != 1 {
		t.Fatalf("expected list count to increment for list_remote_files, got %#v", session.Runtime.Activity)
	}
	if session.Runtime.Activity.CurrentListingPath != "" {
		t.Fatalf("expected listing activity to clear, got %#v", session.Runtime.Activity)
	}
}

func TestToolProjectionRuntimeTracksRemoteReadAliasViewedFiles(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-runtime-remote-read-alias"

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:           ToolLifecycleEventStarted,
		SessionID:      sessionID,
		ToolName:       "read_remote_file",
		ActivityTarget: "/etc/nginx/nginx.conf",
		Phase:          "browsing",
	})

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
		ToolName:  "read_remote_file",
		Payload: map[string]any{
			"trackActivityCompletion": true,
			"arguments": map[string]any{
				"path": "/etc/nginx/nginx.conf",
			},
			"outputData": map[string]any{
				"path": "/etc/nginx/nginx.conf",
			},
		},
	})

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatal("expected session to exist")
	}
	if session.Runtime.Activity.CurrentReadingFile != "" {
		t.Fatalf("expected reading activity to clear, got %#v", session.Runtime.Activity)
	}
	if session.Runtime.Activity.FilesViewed != 1 {
		t.Fatalf("expected viewed file count to increment, got %#v", session.Runtime.Activity)
	}
	if len(session.Runtime.Activity.ViewedFiles) != 1 || session.Runtime.Activity.ViewedFiles[0].Path != "/etc/nginx/nginx.conf" {
		t.Fatalf("expected viewed file entry to be recorded, got %#v", session.Runtime.Activity.ViewedFiles)
	}
}

func TestToolProjectionRuntimeTracksRemoteSearchAliasLocationCount(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-runtime-remote-search-alias"

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:          ToolLifecycleEventStarted,
		SessionID:     sessionID,
		ToolName:      "search_remote_files",
		ActivityQuery: "server_name",
		Phase:         "searching",
	})

	app.projectToolLifecycleRuntime(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
		ToolName:  "search_remote_files",
		Payload: map[string]any{
			"trackActivityCompletion": true,
			"arguments": map[string]any{
				"path":  "/etc/nginx",
				"query": "server_name",
			},
			"outputData": map[string]any{
				"path":       "/etc/nginx",
				"query":      "server_name",
				"matchCount": 2,
			},
		},
	})

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatal("expected session to exist")
	}
	if session.Runtime.Activity.CurrentSearchKind != "" || session.Runtime.Activity.CurrentSearchQuery != "" {
		t.Fatalf("expected live search activity to clear, got %#v", session.Runtime.Activity)
	}
	if session.Runtime.Activity.SearchCount != 1 || session.Runtime.Activity.SearchLocationCount != 2 {
		t.Fatalf("expected remote search counters to update, got %#v", session.Runtime.Activity)
	}
	if len(session.Runtime.Activity.SearchedContentQueries) != 1 {
		t.Fatalf("expected searched content query entry, got %#v", session.Runtime.Activity.SearchedContentQueries)
	}
	if got := session.Runtime.Activity.SearchedContentQueries[0]; got.Path != "/etc/nginx" || got.Query != "server_name" {
		t.Fatalf("expected searched content query to preserve path/query, got %#v", got)
	}
}
