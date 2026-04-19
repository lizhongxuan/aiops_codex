package server

import (
	"context"
	"strings"
	"testing"
)

func TestRegisterDefaultToolHandlersRegistersUnifiedWebTools(t *testing.T) {
	app := &App{toolHandlerRegistry: NewToolHandlerRegistry()}

	app.registerDefaultToolHandlers()

	for _, name := range []string{"web_search", "open_page", "find_in_page"} {
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
		if !desc.SupportsStreamingProgress {
			t.Fatalf("expected %q to support streaming progress, got %#v", name, desc)
		}
	}
}

func TestWebUnifiedToolsUsePromptRegistryDescriptions(t *testing.T) {
	if got := (webSearchUnifiedTool{}).Description(ToolDescriptionContext{}); got != toolPromptDescription("web_search") {
		t.Fatalf("expected web_search prompt description, got %q", got)
	}
	if got := (webFetchUnifiedTool{}).Description(ToolDescriptionContext{}); got != toolPromptDescription("open_page") {
		t.Fatalf("expected open_page prompt description, got %q", got)
	}
	if got := (findInPageUnifiedTool{}).Description(ToolDescriptionContext{}); got != toolPromptDescription("find_in_page") {
		t.Fatalf("expected find_in_page prompt description, got %q", got)
	}
}

func TestWebSearchUnifiedToolProjectsStructuredSearchDisplay(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.MustRegisterUnifiedTool(webSearchUnifiedTool{
		app: app,
		execute: func(context.Context, map[string]any) (string, error) {
			return `[{"title":"OpenAI docs","url":"https://platform.openai.com/docs","snippet":"Latest API docs"},{"title":"Release notes","url":"https://platform.openai.com/docs/release-notes","snippet":"Recent updates"}]`, nil
		},
	})

	bus := NewInProcessToolEventBus()
	bus.Subscribe(NewProductProjectionSubscriber(app))

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "sess-web-search",
		CallID:    "call-web-search",
		ToolName:  "web_search",
		Arguments: map[string]any{
			"query": "OpenAI latest docs",
		},
	})
	if err != nil {
		t.Fatalf("dispatch web_search: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if got := getStringAny(result.OutputData, "query"); got != "OpenAI latest docs" {
		t.Fatalf("expected query to be preserved, got %#v", result.OutputData)
	}
	if got, _ := getIntAny(result.OutputData, "resultCount"); got != 2 {
		t.Fatalf("expected resultCount=2, got %#v", result.OutputData)
	}

	process := app.cardByID("sess-web-search", "proc-call-web-search")
	if process == nil {
		t.Fatal("expected process card to be projected")
	}
	display, ok := process.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display on process card, got %#v", process.Detail)
	}
	if got := getStringAny(display, "summary"); got == "" || !containsAll(got, "OpenAI latest docs", "2") {
		t.Fatalf("expected search summary with query and result count, got %#v", display)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) != 3 {
		t.Fatalf("expected 3 display blocks, got %#v", display["blocks"])
	}
	kinds := []string{
		getStringAny(blocks[0], "kind"),
		getStringAny(blocks[1], "kind"),
		getStringAny(blocks[2], "kind"),
	}
	wantKinds := []string{ToolDisplayBlockSearchQueries, ToolDisplayBlockLinkList, ToolDisplayBlockResultStats}
	for i, want := range wantKinds {
		if kinds[i] != want {
			t.Fatalf("expected block %d kind %q, got %#v", i, want, blocks)
		}
	}

	session := app.store.Session("sess-web-search")
	if session == nil {
		t.Fatal("expected session runtime to exist")
	}
	if session.Runtime.Activity.SearchCount != 1 {
		t.Fatalf("expected search count=1, got %#v", session.Runtime.Activity)
	}
	if len(session.Runtime.Activity.SearchedWebQueries) != 1 || session.Runtime.Activity.SearchedWebQueries[0].Query != "OpenAI latest docs" {
		t.Fatalf("expected searched query to be recorded, got %#v", session.Runtime.Activity)
	}
}

func TestWebFetchUnifiedToolProjectsFetchedPageDescriptor(t *testing.T) {
	app := newTestApp(t)
	registry := NewToolHandlerRegistry()
	registry.MustRegisterUnifiedTool(webFetchUnifiedTool{
		execute: func(context.Context, map[string]any) (string, error) {
			return `<html><head><title>Example Domain</title></head><body><main><h1>Example Domain</h1><p>This domain is for use in illustrative examples in documents.</p></main></body></html>`, nil
		},
	})

	bus := NewInProcessToolEventBus()
	bus.Subscribe(NewProductProjectionSubscriber(app))

	dispatcher := newToolDispatcher(app, registry, bus, nil)
	result, err := dispatcher.Dispatch(context.Background(), ToolInvocation{
		SessionID: "sess-open-page",
		CallID:    "call-open-page",
		ToolName:  "open_page",
		Arguments: map[string]any{
			"url": "https://example.com",
		},
	})
	if err != nil {
		t.Fatalf("dispatch open_page: %v", err)
	}
	if result.Status != ToolRunStatusCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if got := getStringAny(result.OutputData, "title"); got != "Example Domain" {
		t.Fatalf("expected title to be extracted, got %#v", result.OutputData)
	}
	if got := getStringAny(result.OutputData, "url"); got != "https://example.com" {
		t.Fatalf("expected url to be preserved, got %#v", result.OutputData)
	}
	if got := getStringAny(result.OutputData, "contentSummary"); got == "" || !containsAll(got, "illustrative", "documents") {
		t.Fatalf("expected content summary to be extracted, got %#v", result.OutputData)
	}

	process := app.cardByID("sess-open-page", "proc-call-open-page")
	if process == nil {
		t.Fatal("expected process card to be projected")
	}
	display, ok := process.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display on process card, got %#v", process.Detail)
	}
	if got := getStringAny(display, "summary"); got != "已抓取页面：Example Domain" {
		t.Fatalf("expected fetched page summary, got %#v", display)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("expected 2 display blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[0], "kind") != ToolDisplayBlockKVList || getStringAny(blocks[1], "kind") != ToolDisplayBlockText {
		t.Fatalf("expected kv_list + text blocks, got %#v", blocks)
	}
	if process.Text == "已抓取页面：Example Domain" && process.Detail == nil {
		t.Fatalf("expected fetched page to stay structured instead of plain text only, got %#v", process)
	}

	session := app.store.Session("sess-open-page")
	if session == nil {
		t.Fatal("expected session runtime to exist")
	}
	if session.Runtime.Activity.FilesViewed != 1 {
		t.Fatalf("expected open_page completion to increment viewed files, got %#v", session.Runtime.Activity)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
