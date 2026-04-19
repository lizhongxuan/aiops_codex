package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/mcphost"
)

type fakeMCPRuntime struct {
	tools            []mcphost.ToolDefinition
	autoApproved     map[string]bool
	readResourceResp map[string]*mcphost.ToolCallResponse
}

func (f *fakeMCPRuntime) LoadConfig(_ ...string) error { return nil }

func (f *fakeMCPRuntime) ConnectAll(_ context.Context) {}

func (f *fakeMCPRuntime) AllTools() []mcphost.ToolDefinition {
	out := make([]mcphost.ToolDefinition, len(f.tools))
	copy(out, f.tools)
	return out
}

func (f *fakeMCPRuntime) IsAutoApproved(serverName, toolName string) bool {
	if f.autoApproved == nil {
		return false
	}
	return f.autoApproved[serverName+":"+toolName]
}

func (f *fakeMCPRuntime) CallTool(_ context.Context, _ string, _ mcphost.ToolCallRequest) (*mcphost.ToolCallResponse, error) {
	return &mcphost.ToolCallResponse{
		Content: []mcphost.ContentBlock{{Type: "text", Text: "ok"}},
	}, nil
}

func (f *fakeMCPRuntime) ReadResource(_ context.Context, _ string, uri string) (*mcphost.ToolCallResponse, error) {
	if f.readResourceResp == nil {
		return nil, nil
	}
	return f.readResourceResp[uri], nil
}

func (f *fakeMCPRuntime) DisconnectAll() {}

func TestInitBifrostRuntimeRegistersMCPDynamicTools(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "aiops_codex_session",
		SessionSecret:     "test-session-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  filepath.Join(t.TempDir(), "workspace"),
		StatePath:         filepath.Join(t.TempDir(), "ai-server-state.json"),
		AuditLogPath:      filepath.Join(t.TempDir(), "audit.log"),
		UseBifrost:        true,
		LLMProvider:       "openai",
		LLMModel:          "test-model",
		LLMAPIKey:         "test-key",
	})
	app.mcpManager = &fakeMCPRuntime{
		tools: []mcphost.ToolDefinition{
			{
				Name:        "topology",
				Description: "Render topology",
				ServerName:  "coroot-rca",
				InputSchema: map[string]any{"type": "object"},
				Meta: map[string]any{
					"ui": map[string]any{"resourceUri": "ui://coroot-rca/topology"},
				},
			},
		},
	}

	if err := app.initBifrostRuntime(); err != nil {
		t.Fatalf("init bifrost runtime: %v", err)
	}

	spec := app.buildSingleHostReActThreadStartSpec(context.Background(), "mcp-single-host")
	found := false
	expectedName := registeredMCPToolName("coroot-rca", "topology")
	for _, tool := range spec.DynamicTools {
		if tool["name"] == expectedName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected dynamic tools to include %q, got %#v", expectedName, spec.DynamicTools)
	}
}

func TestOnToolCompleteCreatesAssistantCardWithMCPAppHTML(t *testing.T) {
	app := New(config.Config{})
	app.mcpManager = &fakeMCPRuntime{
		readResourceResp: map[string]*mcphost.ToolCallResponse{
			"ui://coroot-rca/topology": {
				Contents: []mcphost.ResourceContent{
					{
						URI:      "ui://coroot-rca/topology",
						MimeType: "text/html;profile=mcp-app",
						Text:     "<!DOCTYPE html><html><body><h1>Coroot 拓扑图</h1></body></html>",
					},
				},
			},
		},
	}
	toolName := registeredMCPToolName("coroot-rca", "topology")
	app.mcpToolBindings = map[string]mcpToolBinding{
		toolName: {
			RegisteredName: toolName,
			ServerName:     "coroot-rca",
			ToolName:       "topology",
			ToolDef: mcphost.ToolDefinition{
				Name:       "topology",
				ServerName: "coroot-rca",
				Meta: map[string]any{
					"ui": map[string]any{"resourceUri": "ui://coroot-rca/topology"},
				},
			},
		},
	}

	sessionID := "mcp-tool-result"
	app.store.EnsureSession(sessionID)
	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session %q", sessionID)
	}

	app.OnToolComplete(context.Background(), &agentloop.Session{ID: sessionID}, toolName, nil, "Coroot 拓扑图已生成。", nil)

	storeSession := app.store.Session(sessionID)
	if storeSession == nil || len(storeSession.Cards) == 0 {
		t.Fatalf("expected assistant result card, got %#v", storeSession)
	}
	card := storeSession.Cards[len(storeSession.Cards)-1]
	if card.Type != "AssistantMessageCard" {
		t.Fatalf("expected AssistantMessageCard, got %#v", card)
	}
	if card.Detail["source"] != "mcp" {
		t.Fatalf("expected mcp detail source, got %#v", card.Detail)
	}
	mcpApp, _ := card.Detail["mcpApp"].(map[string]any)
	if mcpApp["resourceUri"] != "ui://coroot-rca/topology" {
		t.Fatalf("expected resource uri in detail, got %#v", mcpApp)
	}
	if mcpApp["html"] != "<!DOCTYPE html><html><body><h1>Coroot 拓扑图</h1></body></html>" {
		t.Fatalf("expected html payload in detail, got %#v", mcpApp)
	}
}
