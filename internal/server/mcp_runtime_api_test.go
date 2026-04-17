package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/config"
)

func TestHandleMCPServersCanCreateAndListWorkspaceServers(t *testing.T) {
	tempDir := t.TempDir()
	workspaceConfig := filepath.Join(tempDir, ".kiro", "settings", "mcp.json")
	app := New(config.Config{
		SessionCookieName: "mcp-runtime-test",
		SessionSecret:     "mcp-runtime-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  tempDir,
		MCPConfigPaths:    []string{filepath.Join(tempDir, "user-mcp.json"), workspaceConfig},
	})

	postHandler := app.withSession(app.handleMCPServers)
	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", strings.NewReader(`{
		"name":"coroot-rca",
		"transport":"http",
		"url":"http://127.0.0.1:8088/mcp",
		"disabled":true
	}`))
	postRec := httptest.NewRecorder()
	postHandler(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", postRec.Code, postRec.Body.String())
	}
	if _, err := os.Stat(workspaceConfig); err != nil {
		t.Fatalf("expected workspace mcp config to be written: %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers", nil)
	getRec := httptest.NewRecorder()
	postHandler(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}
	var payload struct {
		Items []struct {
			Name     string `json:"name"`
			URL      string `json:"url"`
			Disabled bool   `json:"disabled"`
		} `json:"items"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("expected 1 mcp server, got %#v", payload.Items)
	}
	if payload.Items[0].Name != "coroot-rca" || payload.Items[0].URL != "http://127.0.0.1:8088/mcp" || !payload.Items[0].Disabled {
		t.Fatalf("unexpected mcp server payload: %#v", payload.Items[0])
	}
}

func TestHandleMCPServerByNameCanCloseServer(t *testing.T) {
	tempDir := t.TempDir()
	workspaceConfig := filepath.Join(tempDir, ".kiro", "settings", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(workspaceConfig), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(workspaceConfig, []byte(`{
  "mcpServers": {
    "near-market": {
      "transport": "http",
      "url": "http://127.0.0.1:8088/mcp"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	app := New(config.Config{
		SessionCookieName: "mcp-runtime-test",
		SessionSecret:     "mcp-runtime-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  tempDir,
		MCPConfigPaths:    []string{filepath.Join(tempDir, "user-mcp.json"), workspaceConfig},
	})

	// Warm the manager once so close operates on a loaded runtime.
	rootHandler := app.withSession(app.handleMCPServers)
	rootRec := httptest.NewRecorder()
	rootHandler(rootRec, httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers", nil))
	if rootRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rootRec.Code, rootRec.Body.String())
	}

	handler := app.withSession(app.handleMCPServerByName)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/near-market/close", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(workspaceConfig)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"disabled": true`) {
		t.Fatalf("expected disabled flag to persist, got %s", string(data))
	}
}
