package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/coroot"
)

func TestIsAllowedCorootPath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		iframeMode bool
		want       bool
	}{
		// Original API paths – always allowed regardless of iframe mode.
		{"api services", "/api/v1/services", false, true},
		{"api services iframe", "/api/v1/services", true, true},
		{"api topology", "/api/v1/topology", false, true},
		{"api incidents", "/api/v1/incidents", false, true},
		{"api metrics", "/api/v1/metrics", false, true},
		{"api status", "/api/v1/status", false, true},
		{"api sub-path", "/api/v1/services/foo", false, true},

		// Iframe paths – only allowed when iframeMode is true.
		{"root non-iframe", "/", false, false},
		{"root iframe", "/", true, true},
		{"static non-iframe", "/static/js/main.js", false, false},
		{"static iframe", "/static/js/main.js", true, true},
		{"assets non-iframe", "/assets/logo.png", false, false},
		{"assets iframe", "/assets/logo.png", true, true},
		{"project non-iframe", "/p/default", false, false},
		{"project iframe", "/p/default", true, true},

		// Disallowed paths – rejected in non-iframe mode; in iframe mode the
		// "/" prefix matches everything so these are allowed.
		{"admin non-iframe", "/admin", false, false},
		{"admin iframe", "/admin", true, true},
		{"random path non-iframe", "/foo/bar", false, false},
		{"random path iframe", "/foo/bar", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAllowedCorootPath(tt.path, tt.iframeMode)
			if got != tt.want {
				t.Errorf("isAllowedCorootPath(%q, %v) = %v, want %v",
					tt.path, tt.iframeMode, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Property-based tests using testing/quick
// Feature: coroot-monitor-embed, Property 5: 代理路径转发正确性（iframe 模式）
// **Validates: Requirements 8.1, 8.3**
// ---------------------------------------------------------------------------

// hasAllowedReadPrefix returns true when path starts with any allowedCorootReadPaths prefix.
// This is the reference oracle for non-iframe mode.
func hasAllowedReadPrefix(path string) bool {
	for _, prefix := range allowedCorootReadPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// TestPropertyIframeModeAllowsAllSlashPaths verifies that in iframe mode,
// every path starting with "/" is allowed. Since "/" is in iframeCorootPaths,
// any absolute path (starting with "/") must be accepted.
func TestPropertyIframeModeAllowsAllSlashPaths(t *testing.T) {
	f := func(suffix string) bool {
		path := "/" + suffix // ensure path starts with "/"
		return isAllowedCorootPath(path, true)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("Property violated: iframe mode should allow all /-prefixed paths: %v", err)
	}
}

// TestPropertyNonIframeModeMatchesReadPrefixesOnly verifies that in non-iframe
// mode, isAllowedCorootPath returns true if and only if the path starts with
// one of the allowedCorootReadPaths prefixes.
func TestPropertyNonIframeModeMatchesReadPrefixesOnly(t *testing.T) {
	f := func(suffix string) bool {
		path := "/" + suffix // ensure path starts with "/"
		got := isAllowedCorootPath(path, false)
		want := hasAllowedReadPrefix(path)
		return got == want
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("Property violated: non-iframe mode should match only read prefixes: %v", err)
	}
}

// TestPropertyIframeSuperset verifies that iframe mode is a superset of
// non-iframe mode: any path allowed without iframe must also be allowed with
// iframe enabled.
func TestPropertyIframeSuperset(t *testing.T) {
	f := func(suffix string) bool {
		path := "/" + suffix
		allowedNonIframe := isAllowedCorootPath(path, false)
		allowedIframe := isAllowedCorootPath(path, true)
		// If allowed in non-iframe, must be allowed in iframe.
		if allowedNonIframe && !allowedIframe {
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("Property violated: iframe mode must be a superset of non-iframe mode: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Unit tests: handleCorootProxy handler-level method rejection
// **Validates: Requirements 8.1, 8.3**
// ---------------------------------------------------------------------------

// newTestAppWithCoroot creates a minimal App with a real coroot.Client pointing
// at the given upstream URL. This is sufficient for testing the handler's early
// validation logic (method check, path filter) without needing the full App
// dependency graph.
func newTestAppWithCoroot(upstreamURL string) *App {
	app := &App{
		corootClient: coroot.NewClient(upstreamURL, "test-token", 5*time.Second),
	}
	return app
}

func TestHandleCorootProxy_NonGETMethodsRejected(t *testing.T) {
	// Start a dummy upstream so the coroot client has a valid base URL.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	app := newTestAppWithCoroot(upstream.URL)

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/coroot/api/v1/services", nil)
			rec := httptest.NewRecorder()
			app.handleCorootProxy(rec, req, "test-session")

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s: expected status %d, got %d", method, http.StatusMethodNotAllowed, rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "read-only mode") {
				t.Errorf("%s: expected body to contain 'read-only mode', got %q", method, body)
			}
		})
	}
}

func TestHandleCorootProxy_GETAllowedPath(t *testing.T) {
	// Upstream echoes back a 200 for any request that reaches it.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	app := newTestAppWithCoroot(upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coroot/api/v1/services", nil)
	rec := httptest.NewRecorder()
	app.handleCorootProxy(rec, req, "test-session")

	if rec.Code != http.StatusOK {
		t.Errorf("GET allowed path: expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleCorootProxy_GETForbiddenPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	app := newTestAppWithCoroot(upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coroot/admin/settings", nil)
	rec := httptest.NewRecorder()
	app.handleCorootProxy(rec, req, "test-session")

	if rec.Code != http.StatusForbidden {
		t.Errorf("GET forbidden path: expected 403, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "path not allowed") {
		t.Errorf("expected body to contain 'path not allowed', got %q", body)
	}
}

func TestHandleCorootProxy_NilCorootClient(t *testing.T) {
	app := &App{corootClient: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coroot/api/v1/services", nil)
	rec := httptest.NewRecorder()
	app.handleCorootProxy(rec, req, "test-session")

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil client: expected 503, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "coroot not configured") {
		t.Errorf("expected body to contain 'coroot not configured', got %q", body)
	}
}
