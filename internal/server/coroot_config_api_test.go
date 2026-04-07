package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/coroot"
)

// ---------------------------------------------------------------------------
// Unit tests: handleCorootConfig handler
// **Validates: Requirements 8.2**
// ---------------------------------------------------------------------------

func TestHandleCorootConfig_Configured(t *testing.T) {
	// Start a dummy upstream so the coroot client has a valid base URL.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	app := &App{
		corootClient: coroot.NewClient(upstream.URL, "test-token", 5*time.Second),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coroot/config", nil)
	rec := httptest.NewRecorder()
	app.handleCorootConfig(rec, req, "test-session")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp CorootConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Configured {
		t.Error("expected configured=true, got false")
	}
	if resp.BaseURL == "" {
		t.Error("expected non-empty baseUrl when configured")
	}
	if !resp.IframeMode {
		t.Error("expected iframeMode=true when configured")
	}
}

func TestHandleCorootConfig_NotConfigured(t *testing.T) {
	app := &App{corootClient: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coroot/config", nil)
	rec := httptest.NewRecorder()
	app.handleCorootConfig(rec, req, "test-session")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp CorootConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Configured {
		t.Error("expected configured=false, got true")
	}
	if resp.BaseURL != "" {
		t.Errorf("expected empty baseUrl, got %q", resp.BaseURL)
	}
	if resp.IframeMode {
		t.Error("expected iframeMode=false, got true")
	}
}

func TestHandleCorootConfig_NonGETMethodRejected(t *testing.T) {
	app := &App{corootClient: nil}

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/coroot/config", nil)
			rec := httptest.NewRecorder()
			app.handleCorootConfig(rec, req, "test-session")

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s: expected status 405, got %d", method, rec.Code)
			}
		})
	}
}

func TestHandleCorootConfig_SanitizesBaseURL(t *testing.T) {
	// Use a base URL with a path component to verify it gets stripped.
	app := &App{
		corootClient: coroot.NewClient("http://coroot.internal:9090/some/path?token=secret", "test-token", 5*time.Second),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coroot/config", nil)
	rec := httptest.NewRecorder()
	app.handleCorootConfig(rec, req, "test-session")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp CorootConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	expected := "http://coroot.internal:9090"
	if resp.BaseURL != expected {
		t.Errorf("expected sanitized baseUrl %q, got %q", expected, resp.BaseURL)
	}
}
