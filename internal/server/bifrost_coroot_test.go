package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/coroot"
)

type bifrostRuntimeStubProvider struct {
	chatFn   func(ctx context.Context, req bifrost.ChatRequest) (*bifrost.ChatResponse, error)
	streamFn func(ctx context.Context, req bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func (p *bifrostRuntimeStubProvider) Name() string { return "stub" }

func (p *bifrostRuntimeStubProvider) ChatCompletion(ctx context.Context, req bifrost.ChatRequest) (*bifrost.ChatResponse, error) {
	return p.chatFn(ctx, req)
}

func (p *bifrostRuntimeStubProvider) StreamChatCompletion(ctx context.Context, req bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
	return p.streamFn(ctx, req)
}

func (p *bifrostRuntimeStubProvider) SupportsToolCalling() bool { return true }
func (p *bifrostRuntimeStubProvider) Capabilities() bifrost.ProviderCapabilities {
	return bifrost.ProviderCapabilities{ToolCallingFormat: "openai_function"}
}

func TestBifrostToolNamesFromDynamicToolsMapsCorootTools(t *testing.T) {
	names := bifrostToolNamesFromDynamicTools([]map[string]any{
		{"name": corootToolListServices},
		{"name": corootToolServiceOverview},
		{"name": corootToolServiceMetrics},
		{"name": corootToolServiceAlerts},
		{"name": corootToolTopology},
		{"name": corootToolIncidentTime},
		{"name": corootToolRCAReport},
	})

	expected := []string{
		corootToolListServices,
		corootToolServiceOverview,
		corootToolServiceMetrics,
		corootToolServiceAlerts,
		corootToolTopology,
		corootToolIncidentTime,
		corootToolRCAReport,
	}
	if len(names) != len(expected) {
		t.Fatalf("expected %d mapped tool names, got %d: %#v", len(expected), len(names), names)
	}
	for i, want := range expected {
		if names[i] != want {
			t.Fatalf("names[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestSingleHostBifrostCorootToolCallCreatesResultCard(t *testing.T) {
	payload, err := json.Marshal([]map[string]any{
		{"id": "svc-api", "name": "api", "status": "healthy"},
		{"id": "svc-worker", "name": "worker", "status": "warning"},
	})
	if err != nil {
		t.Fatalf("marshal services payload: %v", err)
	}
	corootHTTPClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/v1/services" {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"not found"}`)),
					Request:    req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body:    io.NopCloser(strings.NewReader(string(payload))),
				Request: req,
			}, nil
		}),
	}

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
	app.corootClient = coroot.NewClientWithHTTPClient("http://coroot.internal", "test-token", time.Second, corootHTTPClient)
	if err := app.initBifrostRuntime(); err != nil {
		t.Fatalf("init bifrost runtime: %v", err)
	}

	callCount := 0
	app.bifrostGateway.RegisterProvider("openai", &workspaceFakeBifrostProvider{
		onRequest: func(req bifrost.ChatRequest) {
			if callCount == 0 {
				if got := strings.Join(workspaceBifrostToolNames(req), ","); !strings.Contains(got, corootToolListServices) {
					t.Fatalf("expected single-host Bifrost tools to include %s, got %q", corootToolListServices, got)
				}
			}
		},
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			switch callCount {
			case 1:
				return makeWorkspaceBifrostStream([]bifrost.StreamEvent{
					{
						Type:       "tool_call_delta",
						ToolIndex:  0,
						ToolCallID: "call-coroot-services",
						FuncName:   corootToolListServices,
						FuncArgs:   `{"reason":"list affected services"}`,
					},
					{Type: "done"},
				}), nil
			default:
				return makeWorkspaceBifrostStream([]bifrost.StreamEvent{
					{Type: "content_delta", Delta: "Coroot 服务已汇总"},
					{Type: "done"},
				}), nil
			}
		},
	})

	sessionID := "single-host-bifrost-coroot"
	app.store.EnsureSession(sessionID)
	app.startRuntimeTurn(sessionID, "server-local")

	if err := app.runBifrostTurn(context.Background(), sessionID, chatRequest{Message: "列出 Coroot 服务"}); err != nil {
		t.Fatalf("run bifrost turn: %v", err)
	}

	card := app.cardByID(sessionID, dynamicToolCardID("call-coroot-services"))
	if card == nil {
		t.Fatalf("expected coroot result card, cards=%#v", app.store.Session(sessionID).Cards)
	}
	if card.Type != "ResultSummaryCard" {
		t.Fatalf("expected ResultSummaryCard, got %#v", card)
	}
	if !strings.Contains(card.Text, `"uiKind":"readonly_summary"`) {
		t.Fatalf("expected readonly_summary payload in card text, got %q", card.Text)
	}
	if !strings.Contains(card.Text, "服务健康概览") {
		t.Fatalf("expected coroot summary payload, got %q", card.Text)
	}
	if session := app.store.Session(sessionID); session == nil || session.Runtime.Turn.Phase != "completed" {
		t.Fatalf("expected completed runtime turn, got %#v", session)
	}
}

func TestInitBifrostRuntimeSupportsAnthropicWithOllamaFallback(t *testing.T) {
	app := New(config.Config{
		UseBifrost:          true,
		LLMProvider:         "anthropic",
		LLMModel:            "claude-sonnet-4-20250514",
		LLMAPIKey:           "anthropic-key",
		LLMFallbackProvider: "ollama",
		LLMFallbackModel:    "qwen3:latest",
		DefaultWorkspace:    filepath.Join(t.TempDir(), "workspace"),
		StatePath:           filepath.Join(t.TempDir(), "ai-server-state.json"),
		AuditLogPath:        filepath.Join(t.TempDir(), "audit.log"),
	})
	if err := app.initBifrostRuntime(); err != nil {
		t.Fatalf("init bifrost runtime: %v", err)
	}

	app.bifrostGateway.RegisterProvider("anthropic", &bifrostRuntimeStubProvider{
		chatFn: func(context.Context, bifrost.ChatRequest) (*bifrost.ChatResponse, error) {
			return nil, &bifrost.APIError{StatusCode: 500, Message: "anthropic unavailable"}
		},
		streamFn: func(context.Context, bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			return nil, nil
		},
	})
	app.bifrostGateway.RegisterProvider("ollama", &bifrostRuntimeStubProvider{
		chatFn: func(_ context.Context, _ bifrost.ChatRequest) (*bifrost.ChatResponse, error) {
			return &bifrost.ChatResponse{
				Message: bifrost.Message{Role: "assistant", Content: "fallback ok"},
			}, nil
		},
		streamFn: func(context.Context, bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			return nil, nil
		},
	})

	resp, err := app.bifrostGateway.ChatCompletion(context.Background(), bifrost.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []bifrost.Message{{
			Role:    "user",
			Content: "test fallback",
		}},
	})
	if err != nil {
		t.Fatalf("chat completion with fallback: %v", err)
	}
	if got := resp.Message.Content; got != "fallback ok" {
		t.Fatalf("expected fallback response, got %#v", got)
	}
}
