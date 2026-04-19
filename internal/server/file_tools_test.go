package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestRegisterDefaultToolHandlersRegistersUnifiedFileTools(t *testing.T) {
	app := &App{toolHandlerRegistry: NewToolHandlerRegistry()}

	app.registerDefaultToolHandlers()

	for _, name := range []string{
		"list_remote_files",
		"read_remote_file",
		"search_remote_files",
		"host_file_read",
		"host_file_search",
	} {
		desc, unified, ok := app.toolHandlerRegistry.LookupUnified(name)
		if !ok || unified == nil {
			t.Fatalf("expected unified tool %q to be registered", name)
		}
		if !desc.IsReadOnly {
			t.Fatalf("expected %q to remain readonly, got %#v", name, desc)
		}
		if desc.Kind != "unified" {
			t.Fatalf("expected %q descriptor kind unified, got %#v", name, desc)
		}
	}

	desc, unified, ok := app.toolHandlerRegistry.LookupUnified("write_file")
	if !ok || unified == nil {
		t.Fatalf("expected unified tool %q to be registered", "write_file")
	}
	if desc.IsReadOnly {
		t.Fatalf("expected %q to remain mutable, got %#v", "write_file", desc)
	}
	if desc.Kind != "unified" {
		t.Fatalf("expected %q descriptor kind unified, got %#v", "write_file", desc)
	}
}

func TestFileUnifiedToolsUsePromptRegistryDescriptions(t *testing.T) {
	if got := (remoteFileListUnifiedTool{}).Description(ToolDescriptionContext{}); got != remoteToolPromptDescription("list_remote_files") {
		t.Fatalf("expected list_remote_files prompt description, got %q", got)
	}
	if got := (remoteFileReadUnifiedTool{}).Description(ToolDescriptionContext{}); got != remoteToolPromptDescription("read_remote_file") {
		t.Fatalf("expected read_remote_file prompt description, got %q", got)
	}
	if got := (remoteFileSearchUnifiedTool{}).Description(ToolDescriptionContext{}); got != remoteToolPromptDescription("search_remote_files") {
		t.Fatalf("expected search_remote_files prompt description, got %q", got)
	}
	if got := (remoteFileWriteUnifiedTool{}).Description(ToolDescriptionContext{}); got != remoteToolPromptDescription("write_file") {
		t.Fatalf("expected write_file prompt description, got %q", got)
	}
	if got := (hostFileReadUnifiedTool{}).Description(ToolDescriptionContext{}); got != toolPromptDescription("host_file_read") {
		t.Fatalf("expected host_file_read prompt description, got %q", got)
	}
	if got := (hostFileSearchUnifiedTool{}).Description(ToolDescriptionContext{}); got != toolPromptDescription("host_file_search") {
		t.Fatalf("expected host_file_search prompt description, got %q", got)
	}
}

func TestRemoteFileListUnifiedToolProjectsStructuredDirectoryDisplay(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.MustRegisterUnifiedTool(remoteFileListUnifiedTool{
		listFiles: func(context.Context, string, string, bool, int) (*agentrpc.FileListResult, error) {
			return &agentrpc.FileListResult{
				Path: "/etc/nginx",
				Entries: []agentrpc.FileEntry{
					{Name: "nginx.conf", Path: "/etc/nginx/nginx.conf", Kind: "file", Size: 2048},
					{Name: "conf.d", Path: "/etc/nginx/conf.d", Kind: "dir"},
				},
			}, nil
		},
	})

	bus := NewInProcessToolEventBus()
	bus.Subscribe(NewProductProjectionSubscriber(app))

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "sess-file-list",
		CallID:    "call-file-list",
		ToolName:  "list_remote_files",
		HostID:    "linux-01",
		Arguments: map[string]any{
			"host":        "linux-01",
			"path":        "/etc/nginx",
			"recursive":   true,
			"max_entries": 20,
			"reason":      "inspect nginx directory",
		},
	})
	if err != nil {
		t.Fatalf("dispatch list_remote_files: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if got := getStringAny(result.OutputData, "path"); got != "/etc/nginx" {
		t.Fatalf("expected path to be preserved, got %#v", result.OutputData)
	}
	if got, _ := getIntAny(result.OutputData, "entryCount"); got != 2 {
		t.Fatalf("expected entryCount=2, got %#v", result.OutputData)
	}

	process := app.cardByID("sess-file-list", "proc-call-file-list")
	if process == nil {
		t.Fatal("expected process card to be projected")
	}
	display, ok := process.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display on process card, got %#v", process.Detail)
	}
	if got := getStringAny(display, "summary"); got == "" || !containsAll(got, "/etc/nginx", "2") {
		t.Fatalf("expected directory summary with path and entry count, got %#v", display)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) < 2 {
		t.Fatalf("expected structured blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[0], "kind") != ToolDisplayBlockResultStats {
		t.Fatalf("expected result_stats block first, got %#v", blocks)
	}
	if getStringAny(blocks[1], "kind") != ToolDisplayBlockLinkList {
		t.Fatalf("expected link_list block second, got %#v", blocks)
	}
}

func TestRemoteFileReadUnifiedToolProjectsFilePreviewDisplay(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.MustRegisterUnifiedTool(remoteFileReadUnifiedTool{
		readFile: func(context.Context, string, string, int) (*agentrpc.FileReadResult, error) {
			return &agentrpc.FileReadResult{
				Path:      "/etc/nginx/nginx.conf",
				Content:   "user nginx;\nworker_processes auto;\nserver_name example.com;\n",
				Truncated: true,
			}, nil
		},
	})

	bus := NewInProcessToolEventBus()
	bus.Subscribe(NewProductProjectionSubscriber(app))

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "sess-file-read",
		CallID:    "call-file-read",
		ToolName:  "read_remote_file",
		HostID:    "linux-02",
		Arguments: map[string]any{
			"host":      "linux-02",
			"path":      "/etc/nginx/nginx.conf",
			"max_bytes": 4096,
			"reason":    "inspect config",
		},
	})
	if err != nil {
		t.Fatalf("dispatch read_remote_file: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if got := getStringAny(result.OutputData, "path"); got != "/etc/nginx/nginx.conf" {
		t.Fatalf("expected path to be preserved, got %#v", result.OutputData)
	}

	process := app.cardByID("sess-file-read", "proc-call-file-read")
	if process == nil {
		t.Fatal("expected process card to be projected")
	}
	display, ok := process.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display on process card, got %#v", process.Detail)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) < 2 {
		t.Fatalf("expected structured blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[0], "kind") != ToolDisplayBlockResultStats {
		t.Fatalf("expected result_stats block first, got %#v", blocks)
	}
	if getStringAny(blocks[1], "kind") != ToolDisplayBlockFilePreview {
		t.Fatalf("expected file_preview block second, got %#v", blocks)
	}
}

func TestRemoteFileSearchUnifiedToolProjectsMatchBlocks(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.MustRegisterUnifiedTool(remoteFileSearchUnifiedTool{
		searchFiles: func(context.Context, string, string, string, int) (*agentrpc.FileSearchResult, error) {
			return &agentrpc.FileSearchResult{
				Path:  "/etc/nginx",
				Query: "server_name",
				Matches: []agentrpc.FileMatch{
					{Path: "/etc/nginx/nginx.conf", Line: 12, Preview: "server_name example.com;"},
					{Path: "/etc/nginx/conf.d/app.conf", Line: 7, Preview: "server_name app.example.com;"},
				},
			}, nil
		},
	})

	bus := NewInProcessToolEventBus()
	bus.Subscribe(NewProductProjectionSubscriber(app))

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "sess-file-search",
		CallID:    "call-file-search",
		ToolName:  "search_remote_files",
		HostID:    "linux-03",
		Arguments: map[string]any{
			"host":        "linux-03",
			"path":        "/etc/nginx",
			"query":       "server_name",
			"max_matches": 20,
			"reason":      "inspect search hits",
		},
	})
	if err != nil {
		t.Fatalf("dispatch search_remote_files: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if got, _ := getIntAny(result.OutputData, "matchCount"); got != 2 {
		t.Fatalf("expected matchCount=2, got %#v", result.OutputData)
	}

	process := app.cardByID("sess-file-search", "proc-call-file-search")
	if process == nil {
		t.Fatal("expected process card to be projected")
	}
	display, ok := process.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display on process card, got %#v", process.Detail)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) < 3 {
		t.Fatalf("expected structured blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[0], "kind") != ToolDisplayBlockSearchQueries {
		t.Fatalf("expected search_queries block first, got %#v", blocks)
	}
	if getStringAny(blocks[1], "kind") != ToolDisplayBlockResultStats {
		t.Fatalf("expected result_stats block second, got %#v", blocks)
	}
	if getStringAny(blocks[2], "kind") != ToolDisplayBlockLinkList {
		t.Fatalf("expected link_list block third, got %#v", blocks)
	}
}

func TestRemoteFileWriteUnifiedToolProjectsWriteSummaryAndArtifacts(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.MustRegisterUnifiedTool(remoteFileWriteUnifiedTool{
		app: app,
		writeFile: func(context.Context, string, string, string, string) (*agentrpc.FileWriteResult, error) {
			return &agentrpc.FileWriteResult{
				Path:       "/etc/nginx/nginx.conf",
				OldContent: "user nginx;\n",
				NewContent: "user nginx;\nworker_processes auto;\n",
				Created:    false,
				WriteMode:  "overwrite",
				Cancelable: true,
			}, nil
		},
	})

	app.store.EnsureSession("sess-file-write")
	app.store.UpsertHost(model.Host{ID: "linux-04", Name: "linux-04", Kind: "agent", Status: "online", Executable: true})

	bus := NewInProcessToolEventBus()
	bus.Subscribe(NewProductProjectionSubscriber(app))

	coord := NewToolApprovalCoordinator(ToolApprovalRuleFunc{
		RuleName: "session-allow",
		Fn: func(_ context.Context, req ToolApprovalRequest) (ApprovalResolution, bool) {
			if req.SessionID != "sess-file-write" || req.ToolName != "write_file" {
				return ApprovalResolution{}, false
			}
			return ApprovalResolution{
				Status:   ApprovalResolutionStatusApproved,
				RuleName: "session-allow",
				Reason:   "write policy allows execution",
			}, true
		},
	})
	coord.now = func() time.Time { return time.Date(2026, 4, 19, 8, 0, 0, 0, time.UTC) }
	coord.nextID = func(prefix string) string { return prefix + "-write" }

	dispatcher := newToolDispatcher(app, registry, bus, coord)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "sess-file-write",
		CallID:    "call-file-write",
		ToolName:  "write_file",
		HostID:    "linux-04",
		Arguments: map[string]any{
			"host":    "linux-04",
			"path":    "/etc/nginx/nginx.conf",
			"content": "user nginx;\nworker_processes auto;\n",
			"reason":  "update nginx worker count",
		},
	})
	if err != nil {
		t.Fatalf("dispatch write_file: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if got := getStringAny(result.OutputData, "changeKind"); got != "update" {
		t.Fatalf("expected changeKind=update, got %#v", result.OutputData)
	}
	if got := getStringAny(result.OutputData, "writeMode"); got != "overwrite" {
		t.Fatalf("expected writeMode=overwrite, got %#v", result.OutputData)
	}

	process := app.cardByID("sess-file-write", "proc-call-file-write")
	if process == nil {
		t.Fatal("expected process card to be projected")
	}
	display := toolProjectionDisplayMapFromDetail(process.Detail)
	if got := getStringAny(display, "summary"); got == "" || !strings.Contains(got, "/etc/nginx/nginx.conf") {
		t.Fatalf("expected process display summary to mention file path, got %#v", display)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) < 2 {
		t.Fatalf("expected write summary blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[0], "kind") != ToolDisplayBlockResultStats {
		t.Fatalf("expected first block result_stats, got %#v", blocks)
	}
	if getStringAny(blocks[1], "kind") != ToolDisplayBlockFileDiffSummary {
		t.Fatalf("expected second block file_diff_summary, got %#v", blocks)
	}

	card := app.cardByID("sess-file-write", dynamicToolCardID("call-file-write"))
	if card == nil {
		t.Fatal("expected file change card to be projected")
	}
	if card.Type != "FileChangeCard" || card.Status != "completed" {
		t.Fatalf("unexpected file change card: %#v", card)
	}
	if card.Summary == "" || !strings.Contains(card.Summary, "/etc/nginx/nginx.conf") {
		t.Fatalf("expected card summary to mention file path, got %#v", card)
	}
	if len(card.Changes) != 1 || card.Changes[0].Kind != "update" {
		t.Fatalf("expected update change entry, got %#v", card.Changes)
	}
	if got := getStringAny(card.Detail, "dryRunSummary"); !strings.Contains(got, "@@") {
		t.Fatalf("expected dryRunSummary diff preview, got %#v", card.Detail)
	}
	if got := getStringAny(card.Detail, "changeKind"); got != "update" {
		t.Fatalf("expected detail changeKind=update, got %#v", card.Detail)
	}
	if got := getStringAny(card.Detail, "writeMode"); got != "overwrite" {
		t.Fatalf("expected detail writeMode=overwrite, got %#v", card.Detail)
	}
	evidenceID := getStringAny(card.Detail, "evidenceId")
	if evidenceID == "" {
		t.Fatalf("expected evidenceId on file change card, got %#v", card.Detail)
	}
	if item := app.store.Item("sess-file-write", evidenceID); item == nil {
		t.Fatalf("expected evidence artifact %q", evidenceID)
	}
	record := findVerificationRecord(app.snapshot("sess-file-write").VerificationRecords, "verify-"+dynamicToolCardID("call-file-write"))
	if record == nil {
		t.Fatalf("expected verification record for file write, got %#v", app.snapshot("sess-file-write").VerificationRecords)
	}
	if got := anyToString(record.Metadata["evidenceId"]); got != evidenceID {
		t.Fatalf("expected verification evidenceId %q, got %#v", evidenceID, record.Metadata)
	}
}

func TestHostFileReadUnifiedToolProjectsFilePreviewDisplay(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.MustRegisterUnifiedTool(hostFileReadUnifiedTool{
		execute: func(context.Context, ToolInvocation, execSpec) (remoteExecResult, error) {
			return remoteExecResult{
				Status:   "completed",
				ExitCode: 0,
				Stdout:   "user nginx;\nworker_processes auto;\nserver_name example.com;\n",
			}, nil
		},
	})

	bus := NewInProcessToolEventBus()
	bus.Subscribe(NewProductProjectionSubscriber(app))

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	_, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "sess-host-file-read",
		CallID:    "call-host-file-read",
		ToolName:  "host_file_read",
		HostID:    "linux-04",
		Arguments: map[string]any{
			"host":      "linux-04",
			"path":      "/etc/nginx/nginx.conf",
			"max_lines": 40,
			"reason":    "inspect config",
		},
	})
	if err != nil {
		t.Fatalf("dispatch host_file_read: %v", err)
	}

	process := app.cardByID("sess-host-file-read", "proc-call-host-file-read")
	if process == nil {
		t.Fatal("expected process card to be projected")
	}
	display, ok := process.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display on process card, got %#v", process.Detail)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) < 2 {
		t.Fatalf("expected structured blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[1], "kind") != ToolDisplayBlockFilePreview {
		t.Fatalf("expected file_preview block, got %#v", blocks)
	}
}

func TestHostFileSearchUnifiedToolProjectsMatchBlocks(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.MustRegisterUnifiedTool(hostFileSearchUnifiedTool{
		execute: func(context.Context, ToolInvocation, execSpec) (remoteExecResult, error) {
			return remoteExecResult{
				Status:   "completed",
				ExitCode: 0,
				Stdout: "/etc/nginx/nginx.conf:12:server_name example.com;\n" +
					"/etc/nginx/conf.d/app.conf:7:server_name app.example.com;\n",
			}, nil
		},
	})

	bus := NewInProcessToolEventBus()
	bus.Subscribe(NewProductProjectionSubscriber(app))

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	_, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "sess-host-file-search",
		CallID:    "call-host-file-search",
		ToolName:  "host_file_search",
		HostID:    "linux-05",
		Arguments: map[string]any{
			"host":        "linux-05",
			"path":        "/etc/nginx",
			"pattern":     "server_name",
			"max_matches": 20,
			"reason":      "inspect search hits",
		},
	})
	if err != nil {
		t.Fatalf("dispatch host_file_search: %v", err)
	}

	process := app.cardByID("sess-host-file-search", "proc-call-host-file-search")
	if process == nil {
		t.Fatal("expected process card to be projected")
	}
	display, ok := process.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display on process card, got %#v", process.Detail)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) < 3 {
		t.Fatalf("expected structured blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[0], "kind") != ToolDisplayBlockSearchQueries {
		t.Fatalf("expected search_queries block first, got %#v", blocks)
	}
	if getStringAny(blocks[2], "kind") != ToolDisplayBlockLinkList {
		t.Fatalf("expected link_list block third, got %#v", blocks)
	}
}
