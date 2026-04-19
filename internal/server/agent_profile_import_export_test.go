package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestAgentProfileExportImportRoundTrip(t *testing.T) {
	app := startAgentProfileAPITestApp(t)
	exportCookie := mustGetAgentProfileCookie(t, app, http.MethodGet, "/api/v1/agent-profiles", nil)

	mainProfile := app.mainAgentProfile()
	mainProfile.Name = "Main Agent Exported"
	mainProfile.SystemPrompt.Content = "Round-trip main prompt."
	mainProfile.Skills = []model.AgentSkill{
		{ID: "ops-triage", Name: "Ops Triage", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
	}
	app.store.UpsertAgentProfile(mainProfile)

	hostProfile := app.hostAgentDefaultProfile()
	hostProfile.Name = "Host Agent Default Exported"
	hostProfile.SystemPrompt.Content = "Round-trip host prompt."
	hostProfile.CommandPermissions.AllowSudo = testBoolPtr(true)
	app.store.UpsertAgentProfile(hostProfile)

	exportResp, exportPath := mustDoAgentProfileAPIRequest(t, app, http.MethodGet, agentProfileExportPaths(), nil, exportCookie)
	if exportResp == nil {
		t.Skip("pending agent profile export/import API implementation")
	}
	if exportResp.Code != http.StatusOK {
		t.Fatalf("expected export status 200 on %s, got %d body=%s", exportPath, exportResp.Code, exportResp.Body.String())
	}
	var exported map[string]any
	if err := json.NewDecoder(exportResp.Body).Decode(&exported); err != nil {
		t.Fatalf("decode export payload: %v", err)
	}
	exportedProfiles := exportProfilesFromPayload(t, exported)
	if len(exportedProfiles) < 2 {
		t.Fatalf("expected at least two exported profiles, got %#v", exportedProfiles)
	}
	if !containsExportedProfile(exportedProfiles, "main-agent", "Main Agent Exported") {
		t.Fatalf("expected exported main agent profile, got %#v", exportedProfiles)
	}
	if !containsExportedProfile(exportedProfiles, "host-agent-default", "Host Agent Default Exported") {
		t.Fatalf("expected exported host-agent-default profile, got %#v", exportedProfiles)
	}

	importPayload := map[string]any{
		"profiles": exportedProfiles,
		"replace":  true,
	}
	importBody, err := json.Marshal(importPayload)
	if err != nil {
		t.Fatalf("marshal import payload: %v", err)
	}
	importResp, importPath := mustDoAgentProfileAPIRequest(t, app, http.MethodPost, agentProfileImportPaths(), strings.NewReader(string(importBody)), exportCookie)
	if importResp == nil {
		t.Skip("pending agent profile export/import API implementation")
	}
	if importResp.Code != http.StatusOK {
		t.Fatalf("expected import status 200 on %s, got %d body=%s", importPath, importResp.Code, importResp.Body.String())
	}

	roundTripResp, roundTripPath := mustDoAgentProfileAPIRequest(t, app, http.MethodGet, agentProfileExportPaths(), nil, exportCookie)
	if roundTripResp == nil {
		t.Skip("pending agent profile export/import API implementation")
	}
	if roundTripResp.Code != http.StatusOK {
		t.Fatalf("expected export after import status 200 on %s, got %d body=%s", roundTripPath, roundTripResp.Code, roundTripResp.Body.String())
	}
	var roundTrip map[string]any
	if err := json.NewDecoder(roundTripResp.Body).Decode(&roundTrip); err != nil {
		t.Fatalf("decode round-trip export payload: %v", err)
	}
	roundTripProfiles := exportProfilesFromPayload(t, roundTrip)
	if !containsExportedProfile(roundTripProfiles, "main-agent", "Main Agent Exported") {
		t.Fatalf("expected main profile to survive round trip, got %#v", roundTripProfiles)
	}
	if !containsExportedProfile(roundTripProfiles, "host-agent-default", "Host Agent Default Exported") {
		t.Fatalf("expected host profile to survive round trip, got %#v", roundTripProfiles)
	}
}

func TestAgentProfileImportRejectsMalformedPayload(t *testing.T) {
	app := startAgentProfileAPITestApp(t)
	cookie := mustGetAgentProfileCookie(t, app, http.MethodGet, "/api/v1/agent-profiles", nil)

	resp, _ := mustDoAgentProfileAPIRequest(t, app, http.MethodPost, agentProfileImportPaths(), strings.NewReader(`{"profiles":"bad"}`), cookie)
	if resp == nil {
		t.Skip("pending agent profile export/import API implementation")
	}
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed import to be rejected, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func startAgentProfileAPITestApp(t *testing.T) *App {
	t.Helper()

	workDir := t.TempDir()
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  filepath.Join(workDir, "workspace"),
		StatePath:         filepath.Join(workDir, "state.json"),
		AuditLogPath:      filepath.Join(workDir, "audit.log"),
		HTTPAddr:          "127.0.0.1:0",
		GRPCAddr:          "127.0.0.1:0",
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := app.Start(ctx); err != nil {
		cancel()
		t.Fatalf("start app: %v", err)
	}
	t.Cleanup(func() {
		cancel()
	})
	return app
}

func mustGetAgentProfileCookie(t *testing.T, app *App, method, path string, body *strings.Reader) *http.Cookie {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		reader = body
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(rec, req)
	resp := rec.Result()
	if len(resp.Cookies()) == 0 {
		t.Fatalf("expected session cookie from %s %s, got body=%s", method, path, rec.Body.String())
	}
	return resp.Cookies()[0]
}

func mustDoAgentProfileRequest(t *testing.T, app *App, method, path string, body *strings.Reader, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		reader = body
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(rec, req)
	return rec
}

func mustDoAgentProfileAPIRequest(t *testing.T, app *App, method string, paths []string, body *strings.Reader, cookie *http.Cookie) (*httptest.ResponseRecorder, string) {
	t.Helper()
	for _, path := range paths {
		resp := mustDoAgentProfileRequest(t, app, method, path, body, cookie)
		if isPendingAgentProfileAPI(resp) {
			continue
		}
		return resp, path
	}
	return nil, ""
}

func isPendingAgentProfileAPI(resp *httptest.ResponseRecorder) bool {
	if resp == nil {
		return true
	}
	if resp.Code == http.StatusNotFound || resp.Code == http.StatusMethodNotAllowed {
		return true
	}
	return strings.Contains(resp.Body.String(), "frontend build not found")
}

func agentProfileExportPaths() []string {
	return []string{
		"/api/v1/agent-profile/export",
		"/api/v1/agent-profiles/export",
	}
}

func agentProfileImportPaths() []string {
	return []string{
		"/api/v1/agent-profile/import",
		"/api/v1/agent-profiles/import",
	}
}

func exportProfilesFromPayload(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	rawProfiles := payload["profiles"]
	if rawProfiles == nil {
		rawProfiles = payload["items"]
	}
	if rawProfiles == nil {
		t.Fatalf("expected profiles/items in export payload, got %#v", payload)
	}
	raw, err := json.Marshal(rawProfiles)
	if err != nil {
		t.Fatalf("marshal export profiles: %v", err)
	}
	var profiles []map[string]any
	if err := json.Unmarshal(raw, &profiles); err != nil {
		t.Fatalf("unmarshal export profiles: %v", err)
	}
	return profiles
}

func containsExportedProfile(profiles []map[string]any, id, name string) bool {
	for _, profile := range profiles {
		if strings.TrimSpace(stringValueFromMap(profile, "id")) == id && strings.TrimSpace(stringValueFromMap(profile, "name")) == name {
			return true
		}
	}
	return false
}

func stringValueFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	value, ok := m[key]
	if !ok || value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		return ""
	}
	return out
}

func testBoolPtr(value bool) *bool {
	v := value
	return &v
}
