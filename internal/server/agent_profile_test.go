package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestHandleAgentProfilesReturnsDefaults(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	handler := app.withSession(app.handleAgentProfiles)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-profiles", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var payload struct {
		Items        []model.AgentProfile `json:"items"`
		SkillCatalog []model.AgentSkill   `json:"skillCatalog"`
		MCPCatalog   []model.AgentMCP     `json:"mcpCatalog"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) < 2 {
		t.Fatalf("expected default profiles, got %#v", payload.Items)
	}
	if payload.Items[0].Runtime.Model == "" {
		t.Fatalf("expected runtime model to be set, got %#v", payload.Items[0])
	}
	if len(payload.SkillCatalog) == 0 {
		t.Fatalf("expected skill catalog to be returned")
	}
	if len(payload.MCPCatalog) == 0 {
		t.Fatalf("expected mcp catalog to be returned")
	}
}

func TestHandleAgentProfileUpdateAndPreview(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	updateHandler := app.withSession(app.handleAgentProfile)
	previewHandler := app.withSession(app.handleAgentProfilePreview)

	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/agent-profile", strings.NewReader(`{
		"id":"main-agent",
		"type":"main-agent",
		"name":"Primary Agent",
		"description":"profile under test",
		"runtime":{
			"model":"gpt-5.4-mini",
			"reasoningEffort":"high",
			"approvalPolicy":"untrusted",
			"sandboxMode":"workspace-write"
		},
		"systemPrompt":{"content":"Keep plans short and safe."},
		"commandPermissions":{
			"enabled":false,
			"defaultMode":"deny",
			"allowShellWrapper":false,
			"allowSudo":false,
			"defaultTimeoutSeconds":45,
			"allowedWritableRoots":["/tmp"],
			"categoryPolicies":{
				"system_inspection":"allow",
				"service_read":"allow",
				"network_read":"approval_required",
				"file_read":"allow",
				"service_mutation":"deny",
				"filesystem_mutation":"deny",
				"package_mutation":"deny"
			}
		},
		"capabilityPermissions":{
			"commandExecution":"disabled",
			"fileRead":"enabled",
			"fileSearch":"enabled",
			"fileChange":"approval_required",
			"terminal":"enabled",
			"webSearch":"enabled",
			"webOpen":"approval_required",
			"approval":"enabled",
			"multiAgent":"enabled",
			"plan":"enabled",
			"summary":"enabled"
		}
	}`))
	updateRec := httptest.NewRecorder()

	updateHandler(updateRec, updateReq)

	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	sessionID := app.store.SessionIDs()[0]
	profile, ok := app.store.AgentProfile(string(model.AgentProfileTypeMainAgent))
	if !ok {
		t.Fatalf("expected main-agent profile to exist")
	}
	if profile.Runtime.Model != "gpt-5.4-mini" {
		t.Fatalf("expected model to persist, got %#v", profile.Runtime)
	}
	if got := boolValue(profile.CommandPermissions.Enabled, true); got {
		t.Fatalf("expected command execution to stay disabled, got %#v", profile.CommandPermissions.Enabled)
	}
	if got := boolValue(profile.CommandPermissions.AllowShellWrapper, true); got {
		t.Fatalf("expected shell wrapper to stay disabled, got %#v", profile.CommandPermissions.AllowShellWrapper)
	}

	previewReq := httptest.NewRequest(http.MethodGet, "/api/v1/agent-profile/preview?profileId=main-agent&hostId=linux-01", nil)
	for _, cookie := range updateRec.Result().Cookies() {
		previewReq.AddCookie(cookie)
	}
	previewRec := httptest.NewRecorder()
	previewHandler(previewRec, previewReq)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("expected preview status 200, got %d body=%s", previewRec.Code, previewRec.Body.String())
	}
	var preview agentProfilePreviewResponse
	if err := json.NewDecoder(previewRec.Body).Decode(&preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !strings.Contains(preview.SystemPrompt, "Keep plans short and safe.") {
		t.Fatalf("expected preview to include saved system prompt, got %q", preview.SystemPrompt)
	}
	if !strings.Contains(preview.SystemPrompt, "Operate only on the selected host") {
		t.Fatalf("expected preview to include rendered host instruction, got %q", preview.SystemPrompt)
	}
	if preview.Runtime.Model != "gpt-5.4-mini" {
		t.Fatalf("expected preview runtime to match saved model, got %#v", preview.Runtime)
	}
	if sessionID == "" {
		t.Fatalf("expected session to be created")
	}
}

func TestHandleAgentProfileRejectsInvalidCapabilityState(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
	})
	handler := app.withSession(app.handleAgentProfile)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent-profile", strings.NewReader(`{
		"id":"main-agent",
		"type":"main-agent",
		"name":"Primary Agent",
		"riskConfirmed":true,
		"systemPrompt":{"content":"safe prompt"},
		"commandPermissions":{
			"enabled":true,
			"defaultMode":"approval_required",
			"allowShellWrapper":true,
			"allowSudo":false,
			"defaultTimeoutSeconds":60,
			"categoryPolicies":{
				"system_inspection":"allow",
				"service_read":"allow",
				"network_read":"allow",
				"file_read":"allow",
				"service_mutation":"approval_required",
				"filesystem_mutation":"approval_required",
				"package_mutation":"deny"
			}
		},
		"capabilityPermissions":{"commandExecution":"sometimes"}
	}`))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload agentProfileErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode validation error: %v", err)
	}
	if payload.FieldErrors["capabilityPermissions.commandExecution"] == "" {
		t.Fatalf("expected field error for capabilityPermissions.commandExecution, got %#v", payload)
	}
}

func TestHandleAgentProfileByIDSupportsGetPutAndReset(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	handler := app.withSession(app.handleAgentProfileByID)

	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/agent-profiles/host-agent-default", strings.NewReader(`{
		"riskConfirmed":true,
		"name":"Host Agent Default",
		"type":"host-agent-default",
		"description":"updated host agent profile",
		"runtime":{
			"model":"gpt-5.4-mini",
			"reasoningEffort":"low",
			"approvalPolicy":"untrusted",
			"sandboxMode":"workspace-write"
		},
		"systemPrompt":{"content":"Host agent default prompt"},
		"commandPermissions":{
			"enabled":true,
			"defaultMode":"approval_required",
			"allowShellWrapper":false,
			"allowSudo":false,
			"defaultTimeoutSeconds":90,
			"allowedWritableRoots":["/srv/app"],
			"categoryPolicies":{
				"system_inspection":"allow",
				"service_read":"allow",
				"network_read":"allow",
				"file_read":"allow",
				"service_mutation":"approval_required",
				"filesystem_mutation":"approval_required",
				"package_mutation":"deny"
			}
		},
		"capabilityPermissions":{
			"commandExecution":"enabled",
			"fileRead":"enabled",
			"fileSearch":"enabled",
			"fileChange":"approval_required",
			"terminal":"disabled",
			"webSearch":"disabled",
			"webOpen":"disabled",
			"approval":"enabled",
			"multiAgent":"disabled",
			"plan":"enabled",
			"summary":"enabled"
		}
	}`))
	updateRec := httptest.NewRecorder()
	handler(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected update status 200, got %d body=%s", updateRec.Code, updateRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/agent-profiles/host-agent-default", nil)
	getRec := httptest.NewRecorder()
	handler(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get status 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}
	var profile model.AgentProfile
	if err := json.NewDecoder(getRec.Body).Decode(&profile); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if profile.Description != "updated host agent profile" {
		t.Fatalf("expected updated description, got %#v", profile.Description)
	}

	resetReq := httptest.NewRequest(http.MethodPost, "/api/v1/agent-profiles/host-agent-default/reset", nil)
	resetRec := httptest.NewRecorder()
	handler(resetRec, resetReq)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("expected reset status 200, got %d body=%s", resetRec.Code, resetRec.Body.String())
	}
	var resetProfile model.AgentProfile
	if err := json.NewDecoder(resetRec.Body).Decode(&resetProfile); err != nil {
		t.Fatalf("decode reset response: %v", err)
	}
	if resetProfile.Description != model.DefaultAgentProfile(string(model.AgentProfileTypeHostAgentDefault)).Description {
		t.Fatalf("expected reset description, got %#v", resetProfile.Description)
	}
}

func TestHandleAgentSkillCatalogCRUD(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
	})
	createHandler := app.withSession(app.handleAgentSkills)
	itemHandler := app.withSession(app.handleAgentSkillByID)

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/agent-skills", strings.NewReader(`{
		"id":"custom-skill",
		"name":"Custom Skill",
		"description":"custom skill",
		"source":"local",
		"defaultEnabled":false,
		"defaultActivationMode":"explicit_only"
	}`))
	createRec := httptest.NewRecorder()
	createHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createPayload struct {
		Items []model.AgentSkill `json:"items"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create payload: %v", err)
	}
	if !containsSkillCatalog(createPayload.Items, "custom-skill") {
		t.Fatalf("expected custom skill in catalog, got %#v", createPayload.Items)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/agent-skills/custom-skill", nil)
	for _, cookie := range createRec.Result().Cookies() {
		deleteReq.AddCookie(cookie)
	}
	deleteRec := httptest.NewRecorder()
	itemHandler(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete status 200, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	for _, item := range app.store.SkillCatalog() {
		if item.ID == "custom-skill" {
			t.Fatalf("expected custom skill to be deleted")
		}
	}
}

func TestHandleAgentSkillCatalogAcceptsEnabledAliasFields(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
	})
	handler := app.withSession(app.handleAgentSkills)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent-skills", strings.NewReader(`{
		"id":"alias-skill",
		"name":"Alias Skill",
		"description":"created from UI alias fields",
		"source":"local",
		"enabled":true,
		"activationMode":"default_enabled"
	}`))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	item, ok := findSkillCatalogItem(app.store.SkillCatalog(), "alias-skill")
	if !ok {
		t.Fatalf("expected alias skill to persist")
	}
	if !item.DefaultEnabled {
		t.Fatalf("expected alias skill defaultEnabled=true, got %#v", item)
	}
	if item.DefaultActivationMode != model.AgentSkillActivationDefault {
		t.Fatalf("expected alias skill default activation mode to normalize, got %#v", item)
	}
}

func TestHandleAgentMCPCatalogAcceptsEnabledAliasFields(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
	})
	handler := app.withSession(app.handleAgentMCPs)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent-mcps", strings.NewReader(`{
		"id":"alias-mcp",
		"name":"Alias MCP",
		"type":"http",
		"source":"local",
		"enabled":true,
		"permission":"readwrite",
		"requiresExplicitUserApproval":true
	}`))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	item, ok := findMcpCatalogItem(app.store.MCPCatalog(), "alias-mcp")
	if !ok {
		t.Fatalf("expected alias mcp to persist")
	}
	if !item.DefaultEnabled {
		t.Fatalf("expected alias mcp defaultEnabled=true, got %#v", item)
	}
	if item.Permission != model.AgentMCPPermissionReadwrite {
		t.Fatalf("expected alias mcp permission to normalize, got %#v", item)
	}
}

func TestHandleAgentProfileAcceptsCatalogManagedSkillAndMCP(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	app.store.UpsertSkillCatalogItem(model.AgentSkill{
		ID:                    "custom-skill",
		Name:                  "Custom Skill",
		Source:                "local",
		DefaultEnabled:        false,
		DefaultActivationMode: model.AgentSkillActivationExplicit,
	})
	app.store.UpsertMCPCatalogItem(model.AgentMCP{
		ID:             "custom-mcp",
		Name:           "Custom MCP",
		Type:           "http",
		Source:         "local",
		DefaultEnabled: false,
		Permission:     model.AgentMCPPermissionReadonly,
	})
	handler := app.withSession(app.handleAgentProfile)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent-profile", strings.NewReader(`{
		"id":"main-agent",
		"type":"main-agent",
		"name":"Primary Agent",
		"riskConfirmed":true,
		"systemPrompt":{"content":"safe prompt"},
		"commandPermissions":{
			"enabled":true,
			"defaultMode":"approval_required",
			"allowShellWrapper":true,
			"allowSudo":false,
			"defaultTimeoutSeconds":60,
			"categoryPolicies":{
				"system_inspection":"allow",
				"service_read":"allow",
				"network_read":"allow",
				"file_read":"allow",
				"service_mutation":"approval_required",
				"filesystem_mutation":"approval_required",
				"package_mutation":"deny"
			}
		},
		"capabilityPermissions":{
			"commandExecution":"enabled",
			"fileRead":"enabled",
			"fileSearch":"enabled",
			"fileChange":"approval_required",
			"terminal":"enabled",
			"webSearch":"enabled",
			"webOpen":"approval_required",
			"approval":"enabled",
			"multiAgent":"enabled",
			"plan":"enabled",
			"summary":"enabled"
		},
		"skills":[
			{"id":"custom-skill","name":"Custom Skill","enabled":true,"activationMode":"explicit_only"}
		],
		"mcps":[
			{"id":"custom-mcp","name":"Custom MCP","enabled":true,"permission":"readonly"}
		]
	}`))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	profile, ok := app.store.AgentProfile(string(model.AgentProfileTypeMainAgent))
	if !ok {
		t.Fatalf("expected profile to exist")
	}
	if !containsSkillCatalog(profile.Skills, "custom-skill") {
		t.Fatalf("expected custom skill binding to persist, got %#v", profile.Skills)
	}
	if !containsMcpCatalog(profile.MCPs, "custom-mcp") {
		t.Fatalf("expected custom mcp binding to persist, got %#v", profile.MCPs)
	}
}

func containsSkillCatalog(items []model.AgentSkill, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func findSkillCatalogItem(items []model.AgentSkill, id string) (model.AgentSkill, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return model.AgentSkill{}, false
}

func containsMcpCatalog(items []model.AgentMCP, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func findMcpCatalogItem(items []model.AgentMCP, id string) (model.AgentMCP, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return model.AgentMCP{}, false
}

func TestHandleAgentProfileRejectsUnknownSkillAndMCPIDs(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
	})
	handler := app.withSession(app.handleAgentProfile)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent-profile", strings.NewReader(`{
		"id":"main-agent",
		"type":"main-agent",
		"name":"Primary Agent",
		"systemPrompt":{"content":"safe prompt"},
		"commandPermissions":{
			"enabled":true,
			"defaultMode":"approval_required",
			"allowShellWrapper":true,
			"allowSudo":false,
			"defaultTimeoutSeconds":60,
			"categoryPolicies":{
				"system_inspection":"allow",
				"service_read":"allow",
				"network_read":"allow",
				"file_read":"allow",
				"service_mutation":"approval_required",
				"filesystem_mutation":"approval_required",
				"package_mutation":"deny"
			}
		},
		"capabilityPermissions":{
			"commandExecution":"approval_required",
			"fileRead":"enabled",
			"fileSearch":"enabled",
			"fileChange":"approval_required",
			"terminal":"enabled",
			"webSearch":"enabled",
			"webOpen":"enabled",
			"approval":"enabled",
			"multiAgent":"enabled",
			"plan":"enabled",
			"summary":"enabled"
		},
		"skills":[{"id":"ghost-skill","name":"Ghost","enabled":true,"activationMode":"default_enabled"}],
		"mcps":[{"id":"ghost-mcp","name":"Ghost MCP","enabled":true,"permission":"readonly"}]
	}`))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload agentProfileErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode validation error: %v", err)
	}
	if !hasFieldErrorContaining(payload.FieldErrors, "unsupported skill id", "ghost-skill") {
		t.Fatalf("expected field error for unknown skill id, got %#v", payload)
	}
	if !hasFieldErrorContaining(payload.FieldErrors, "unsupported MCP id", "ghost-mcp") {
		t.Fatalf("expected field error for unknown mcp id, got %#v", payload)
	}
}

func TestHandleAgentProfileRequiresRiskConfirmationForHighRiskChanges(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
	})
	handler := app.withSession(app.handleAgentProfile)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent-profile", strings.NewReader(`{
		"id":"main-agent",
		"type":"main-agent",
		"name":"Primary Agent",
		"systemPrompt":{"content":"safe prompt"},
		"runtime":{"sandboxMode":"danger-full-access"},
		"commandPermissions":{
			"enabled":true,
			"defaultMode":"approval_required",
			"allowShellWrapper":true,
			"allowSudo":true,
			"defaultTimeoutSeconds":60,
			"categoryPolicies":{
				"system_inspection":"allow",
				"service_read":"allow",
				"network_read":"allow",
				"file_read":"allow",
				"service_mutation":"allow",
				"filesystem_mutation":"approval_required",
				"package_mutation":"deny"
			}
		},
		"capabilityPermissions":{
			"commandExecution":"enabled",
			"fileRead":"enabled",
			"fileSearch":"enabled",
			"fileChange":"approval_required",
			"terminal":"enabled",
			"webSearch":"enabled",
			"webOpen":"enabled",
			"approval":"enabled",
			"multiAgent":"enabled",
			"plan":"enabled",
			"summary":"enabled"
		}
	}`))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload agentProfileErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode validation error: %v", err)
	}
	if payload.FieldErrors["commandPermissions.allowSudo"] == "" {
		t.Fatalf("expected risk confirmation error for allowSudo, got %#v", payload)
	}
	if payload.FieldErrors["runtime.sandboxMode"] == "" {
		t.Fatalf("expected risk confirmation error for sandbox mode, got %#v", payload)
	}
}

func TestHandleAgentProfileAcceptsRiskConfirmationMarker(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
	})
	handler := app.withSession(app.handleAgentProfile)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent-profile", strings.NewReader(`{
		"id":"main-agent",
		"type":"main-agent",
		"name":"Primary Agent",
		"riskConfirmed":true,
		"systemPrompt":{"content":"safe prompt"},
		"runtime":{"sandboxMode":"danger-full-access"},
		"commandPermissions":{
			"enabled":true,
			"defaultMode":"approval_required",
			"allowShellWrapper":true,
			"allowSudo":true,
			"defaultTimeoutSeconds":60,
			"categoryPolicies":{
				"system_inspection":"allow",
				"service_read":"allow",
				"network_read":"allow",
				"file_read":"allow",
				"service_mutation":"allow",
				"filesystem_mutation":"approval_required",
				"package_mutation":"deny"
			}
		},
		"capabilityPermissions":{
			"commandExecution":"enabled",
			"fileRead":"enabled",
			"fileSearch":"enabled",
			"fileChange":"approval_required",
			"terminal":"enabled",
			"webSearch":"enabled",
			"webOpen":"enabled",
			"approval":"enabled",
			"multiAgent":"enabled",
			"plan":"enabled",
			"summary":"enabled"
		}
	}`))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEnsureThreadUsesUpdatedMainAgentProfile(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	sessionID := "sess-main-agent-thread"
	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, "linux-01")

	profile := app.mainAgentProfile()
	profile.Runtime.Model = "gpt-5.4-mini"
	profile.Runtime.ApprovalPolicy = "team-approved"
	profile.Runtime.SandboxMode = "workspace-write"
	profile.SystemPrompt.Content = "Keep changes tight and explain impact clearly."
	profile.Skills = []model.AgentSkill{
		{ID: "ops-triage", Name: "Ops Triage", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
		{ID: "safe-change-review", Name: "Safe Change Review", Enabled: false, ActivationMode: model.AgentSkillActivationDisabled},
	}
	profile.MCPs = []model.AgentMCP{
		{ID: "filesystem", Name: "Filesystem MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly},
		{ID: "docs", Name: "Docs MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly, RequiresExplicitUserApproval: true},
		{ID: "metrics", Name: "Metrics MCP", Enabled: false, Permission: model.AgentMCPPermissionReadwrite, RequiresExplicitUserApproval: true},
	}
	app.store.UpsertAgentProfile(profile)

	var captured map[string]any
	app.codexRequestFunc = func(_ context.Context, method string, params any, result any) error {
		switch method {
		case "skills/list":
			return json.Unmarshal([]byte(`{"data":[]}`), result)
		case "thread/start":
		default:
			t.Fatalf("expected skills/list or thread/start, got %s", method)
		}
		var ok bool
		captured, ok = params.(map[string]any)
		if !ok {
			t.Fatalf("expected params map, got %#v", params)
		}
		return json.Unmarshal([]byte(`{"thread":{"id":"thread-updated-main"}}`), result)
	}

	threadID, err := app.ensureThread(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ensureThread: %v", err)
	}
	if threadID != "thread-updated-main" {
		t.Fatalf("expected thread id to be returned, got %q", threadID)
	}
	if got := captured["model"]; got != "gpt-5.4-mini" {
		t.Fatalf("expected updated model, got %#v", got)
	}
	if got := captured["approvalPolicy"]; got != "team-approved" {
		t.Fatalf("expected updated approval policy, got %#v", got)
	}
	if got := captured["sandbox"]; got != "workspace-write" {
		t.Fatalf("expected updated sandbox, got %#v", got)
	}
	instructions := stringValue(captured["developerInstructions"])
	if !strings.Contains(instructions, "Keep changes tight and explain impact clearly.") {
		t.Fatalf("expected updated system prompt in developer instructions, got %q", instructions)
	}
	if !strings.Contains(instructions, "Operate only on the selected host \"linux-01\".") {
		t.Fatalf("expected selected host in developer instructions, got %q", instructions)
	}
	if !strings.Contains(instructions, "Default-enabled skills:") || !strings.Contains(instructions, "Ops Triage") {
		t.Fatalf("expected default-enabled skills in developer instructions, got %q", instructions)
	}
	if strings.Contains(instructions, "Safe Change Review") {
		t.Fatalf("expected disabled skill to be filtered out, got %q", instructions)
	}
	if !strings.Contains(instructions, "Enabled MCP connectors:") || !strings.Contains(instructions, "Filesystem MCP (readonly)") {
		t.Fatalf("expected enabled MCPs in developer instructions, got %q", instructions)
	}
	if !strings.Contains(instructions, "The following MCP connectors require explicit user approval") || !strings.Contains(instructions, "Docs MCP") {
		t.Fatalf("expected explicit approval MCPs in developer instructions, got %q", instructions)
	}
	if strings.Contains(instructions, "Metrics MCP") {
		t.Fatalf("expected disabled MCP to be filtered out, got %q", instructions)
	}
}

func TestRequestTurnUsesUpdatedMainAgentProfile(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	sessionID := "sess-main-agent-turn"
	threadID := "thread-main-agent-turn"
	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, "linux-02")
	app.store.SetThread(sessionID, threadID)

	profile := app.mainAgentProfile()
	profile.Runtime.Model = "gpt-5.4-pro"
	profile.Runtime.ReasoningEffort = "high"
	profile.Runtime.ApprovalPolicy = "strict"
	profile.Runtime.SandboxMode = "read-only"
	profile.SystemPrompt.Content = "Tune each turn for concise operational summaries."
	profile.Skills = []model.AgentSkill{
		{ID: "incident-summary", Name: "Incident Summary", Enabled: true, ActivationMode: model.AgentSkillActivationExplicit},
		{ID: "host-change-review", Name: "Host Change Review", Enabled: false, ActivationMode: model.AgentSkillActivationDisabled},
	}
	profile.MCPs = []model.AgentMCP{
		{ID: "host-files", Name: "Host Files MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly},
		{ID: "host-logs", Name: "Host Logs MCP", Enabled: true, Permission: model.AgentMCPPermissionReadwrite, RequiresExplicitUserApproval: true},
	}
	app.store.UpsertAgentProfile(profile)

	var captured map[string]any
	app.codexRequestFunc = func(_ context.Context, method string, params any, result any) error {
		switch method {
		case "skills/list":
			return json.Unmarshal([]byte(`{"data":[]}`), result)
		case "turn/start":
		default:
			t.Fatalf("expected skills/list or turn/start, got %s", method)
		}
		var ok bool
		captured, ok = params.(map[string]any)
		if !ok {
			t.Fatalf("expected params map, got %#v", params)
		}
		return json.Unmarshal([]byte(`{"turn":{"id":"turn-updated-main"}}`), result)
	}

	err := app.requestTurn(context.Background(), sessionID, threadID, chatRequest{
		Message: "Show the updated turn prompt.",
		HostID:  "linux-02",
	})
	if err != nil {
		t.Fatalf("requestTurn: %v", err)
	}
	if got := captured["threadId"]; got != threadID {
		t.Fatalf("expected thread id to be forwarded, got %#v", got)
	}
	if got := captured["approvalPolicy"]; got != "strict" {
		t.Fatalf("expected updated approval policy, got %#v", got)
	}
	if got := captured["reasoningEffort"]; got != "high" {
		t.Fatalf("expected updated reasoning effort, got %#v", got)
	}
	sandbox := mapValue(captured["sandboxPolicy"])
	if got := sandbox["type"]; got != "readOnly" {
		t.Fatalf("expected updated sandbox type, got %#v", got)
	}
	instructions := stringValue(captured["developerInstructions"])
	if !strings.Contains(instructions, "Tune each turn for concise operational summaries.") {
		t.Fatalf("expected updated system prompt in turn instructions, got %q", instructions)
	}
	if !strings.Contains(instructions, "Summarize execution results clearly for the web UI.") {
		t.Fatalf("expected turn-scoped instruction, got %q", instructions)
	}
	if !strings.Contains(instructions, "Incident Summary") {
		t.Fatalf("expected enabled skill in turn instructions, got %q", instructions)
	}
	if strings.Contains(instructions, "Host Change Review") {
		t.Fatalf("expected disabled skill to be filtered out, got %q", instructions)
	}
	if !strings.Contains(instructions, "Host Files MCP (readonly)") {
		t.Fatalf("expected enabled MCP in turn instructions, got %q", instructions)
	}
	if !strings.Contains(instructions, "Host Logs MCP") {
		t.Fatalf("expected explicit approval MCP in turn instructions, got %q", instructions)
	}
}

func TestAgentProfilePreviewShowsEnabledSkillsAndMCPsOnly(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	profile := app.mainAgentProfile()
	profile.SystemPrompt.Content = "Preview prompt for enabled items."
	profile.Skills = []model.AgentSkill{
		{ID: "ops-triage", Name: "Ops Triage", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
		{ID: "safe-change-review", Name: "Safe Change Review", Enabled: false, ActivationMode: model.AgentSkillActivationDisabled},
	}
	profile.MCPs = []model.AgentMCP{
		{ID: "filesystem", Name: "Filesystem MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly},
		{ID: "docs", Name: "Docs MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly, RequiresExplicitUserApproval: true},
		{ID: "metrics", Name: "Metrics MCP", Enabled: false, Permission: model.AgentMCPPermissionReadwrite, RequiresExplicitUserApproval: true},
	}
	app.store.UpsertAgentProfile(profile)

	handler := app.withSession(app.handleAgentProfilePreview)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-profile/preview?profileId=main-agent&hostId=linux-03", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected preview status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var preview agentProfilePreviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&preview); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	if len(preview.EnabledSkills) != 1 {
		t.Fatalf("expected one enabled skill, got %#v", preview.EnabledSkills)
	}
	if preview.EnabledSkills[0].ID != "ops-triage" {
		t.Fatalf("expected enabled skill to remain, got %#v", preview.EnabledSkills[0])
	}
	if len(preview.EnabledMCPs) != 2 {
		t.Fatalf("expected only enabled MCPs, got %#v", preview.EnabledMCPs)
	}
	if !strings.Contains(preview.SystemPrompt, "Ops Triage") {
		t.Fatalf("expected enabled skill in preview prompt, got %q", preview.SystemPrompt)
	}
	if strings.Contains(preview.SystemPrompt, "Safe Change Review") {
		t.Fatalf("expected disabled skill to be filtered from preview prompt, got %q", preview.SystemPrompt)
	}
	if !strings.Contains(preview.SystemPrompt, "Filesystem MCP (readonly)") {
		t.Fatalf("expected enabled MCP in preview prompt, got %q", preview.SystemPrompt)
	}
	if !strings.Contains(preview.SystemPrompt, "Docs MCP") {
		t.Fatalf("expected explicit approval MCP in preview prompt, got %q", preview.SystemPrompt)
	}
	if strings.Contains(preview.SystemPrompt, "Metrics MCP") {
		t.Fatalf("expected disabled MCP to be filtered from preview prompt, got %q", preview.SystemPrompt)
	}
}

func TestAgentProfilePreviewFiltersMCPWhenRequiredCapabilityDisabled(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.FileRead = model.AgentCapabilityDisabled
	profile.CapabilityPermissions.WebSearch = model.AgentCapabilityEnabled
	profile.MCPs = []model.AgentMCP{
		{ID: "filesystem", Name: "Filesystem MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly},
		{ID: "docs", Name: "Docs MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly, RequiresExplicitUserApproval: true},
	}
	app.store.UpsertAgentProfile(profile)

	handler := app.withSession(app.handleAgentProfilePreview)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-profile/preview?profileId=main-agent&hostId=server-local", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected preview status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var preview agentProfilePreviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&preview); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	if len(preview.EnabledMCPs) != 1 || preview.EnabledMCPs[0].ID != "docs" {
		t.Fatalf("expected fileRead-gated MCP to be removed, got %#v", preview.EnabledMCPs)
	}
	if strings.Contains(preview.SystemPrompt, "Filesystem MCP") {
		t.Fatalf("expected disabled-by-capability MCP to be filtered from preview prompt, got %q", preview.SystemPrompt)
	}
	if !strings.Contains(preview.SystemPrompt, "Docs MCP") {
		t.Fatalf("expected unrelated enabled MCP to remain, got %q", preview.SystemPrompt)
	}
}

func TestHostAgentProfileUpdatePushesToConnectedAgent(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	hostID := "linux-01"
	app.store.UpsertHost(model.Host{
		ID:              hostID,
		Name:            hostID,
		Kind:            "agent",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})
	stream := &fakeAgentConnectServer{}
	app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})

	handler := app.withSession(app.handleAgentProfile)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent-profile?profileId=host-agent-default", strings.NewReader(`{
		"id":"host-agent-default",
		"type":"host-agent-default",
		"name":"Host Agent Default",
		"systemPrompt":{"content":"Only run approved host actions."},
		"commandPermissions":{
			"enabled":true,
			"defaultMode":"approval_required",
			"allowShellWrapper":false,
			"allowSudo":false,
			"defaultTimeoutSeconds":90,
			"allowedWritableRoots":["/workspace"],
			"categoryPolicies":{
				"system_inspection":"allow",
				"service_read":"allow",
				"network_read":"allow",
				"file_read":"allow",
				"service_mutation":"approval_required",
				"filesystem_mutation":"approval_required",
				"package_mutation":"deny"
			}
		},
		"capabilityPermissions":{
			"commandExecution":"approval_required",
			"fileRead":"enabled",
			"fileSearch":"enabled",
			"fileChange":"approval_required",
			"terminal":"enabled",
			"webSearch":"disabled",
			"webOpen":"disabled",
			"approval":"enabled",
			"multiAgent":"disabled",
			"plan":"disabled",
			"summary":"enabled"
		},
		"skills":[{"id":"host-diagnostics","name":"Host Diagnostics","enabled":true,"activationMode":"default_enabled"}],
		"mcps":[{"id":"host-files","name":"Host Files MCP","enabled":true,"permission":"readonly"}]
	}`))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected update status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	msg := stream.waitForKind(t, "profile/update", time.Second)
	if msg.ProfileUpdate == nil {
		t.Fatalf("expected profile update payload, got %#v", msg)
	}
	if msg.ProfileUpdate.Profile.ID != string(model.AgentProfileTypeHostAgentDefault) {
		t.Fatalf("expected host-agent-default payload, got %#v", msg.ProfileUpdate.Profile)
	}
	if got := boolValue(msg.ProfileUpdate.Profile.CommandPermissions.AllowShellWrapper, true); got {
		t.Fatalf("expected pushed profile to disable shell wrapper, got %#v", msg.ProfileUpdate.Profile.CommandPermissions)
	}
	if msg.ProfileUpdate.ProfileHash == "" {
		t.Fatalf("expected non-empty pushed profile hash")
	}
}

func hasFieldErrorContaining(fieldErrors map[string]string, prefixes ...string) bool {
	for _, message := range fieldErrors {
		matched := true
		for _, prefix := range prefixes {
			if !strings.Contains(message, prefix) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func mapValue(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
