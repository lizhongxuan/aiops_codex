package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/generator"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// ---------- request / response types ----------

type generateRequest struct {
	Source      string         `json:"source"`      // mcp_tool | script_config | coroot
	ToolName    string         `json:"toolName,omitempty"`
	ToolDesc    string         `json:"toolDesc,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	// script_config source
	ScriptConfig *model.ScriptConfigProfile `json:"scriptConfig,omitempty"`
	// coroot source
	ServiceType string         `json:"serviceType,omitempty"`
	QuerySchema map[string]any `json:"querySchema,omitempty"`
}

type lintRequest struct {
	DraftType string `json:"draftType"` // skill | card
	// Exactly one of these should be populated.
	Skill *model.AgentSkill       `json:"skill,omitempty"`
	Card  *model.UICardDefinition `json:"card,omitempty"`
}

type previewRequest struct {
	DraftType string `json:"draftType"` // skill | card
	Skill     *model.AgentSkill       `json:"skill,omitempty"`
	Card      *model.UICardDefinition `json:"card,omitempty"`
}

type publishDraftRequest struct {
	DraftType string `json:"draftType"` // skill | card
	Skill     *model.AgentSkill       `json:"skill,omitempty"`
	Card      *model.UICardDefinition `json:"card,omitempty"`
}

// ---------- handler ----------

// handleGenerator routes POST requests to the four generator sub-endpoints:
//   POST /api/v1/generator/generate
//   POST /api/v1/generator/lint
//   POST /api/v1/generator/preview
//   POST /api/v1/generator/publish-draft
func (a *App) handleGenerator(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	sub := strings.TrimPrefix(r.URL.Path, "/api/v1/generator/")
	sub = strings.Trim(sub, "/")

	svc := generator.NewGeneratorService()

	switch sub {
	case "generate":
		a.handleGeneratorGenerate(w, r, svc)
	case "lint":
		a.handleGeneratorLint(w, r, svc)
	case "preview":
		a.handleGeneratorPreview(w, r, svc)
	case "publish-draft":
		a.handleGeneratorPublishDraft(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown generator endpoint"})
	}
}

func (a *App) handleGeneratorGenerate(w http.ResponseWriter, r *http.Request, svc *generator.GeneratorService) {
	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	switch req.Source {
	case "mcp_tool":
		skill, err := svc.GenerateSkillFromMCP(req.ToolName, req.ToolDesc, req.InputSchema)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"draftType": "skill", "skill": skill})

	case "script_config":
		if req.ScriptConfig == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scriptConfig is required for source script_config"})
			return
		}
		card, err := svc.GenerateCardFromScript(*req.ScriptConfig)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"draftType": "card", "card": card})

	case "coroot":
		card, err := svc.GenerateBundleFromCoroot(req.ServiceType, req.QuerySchema)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"draftType": "card", "card": card})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported source: " + req.Source})
	}
}

func (a *App) handleGeneratorLint(w http.ResponseWriter, r *http.Request, svc *generator.GeneratorService) {
	var req lintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	var issues []generator.LintIssue
	switch req.DraftType {
	case "skill":
		if req.Skill == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "skill payload is required"})
			return
		}
		issues = svc.Lint(req.Skill)
	case "card":
		if req.Card == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "card payload is required"})
			return
		}
		issues = svc.Lint(req.Card)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "draftType must be skill or card"})
		return
	}

	hasErrors := false
	for _, issue := range issues {
		if issue.Level == "error" {
			hasErrors = true
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": issues, "valid": !hasErrors})
}

func (a *App) handleGeneratorPreview(w http.ResponseWriter, r *http.Request, _ *generator.GeneratorService) {
	var req previewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	switch req.DraftType {
	case "skill":
		if req.Skill == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "skill payload is required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"draftType": "skill",
			"preview": map[string]any{
				"id":             req.Skill.ID,
				"name":           req.Skill.Name,
				"description":    req.Skill.Description,
				"category":       req.Skill.Category,
				"activationMode": req.Skill.ActivationMode,
				"status":         req.Skill.Status,
				"dependencies":   req.Skill.Dependencies,
			},
		})
	case "card":
		if req.Card == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "card payload is required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"draftType": "card",
			"preview": map[string]any{
				"id":           req.Card.ID,
				"name":         req.Card.Name,
				"kind":         req.Card.Kind,
				"renderer":     req.Card.Renderer,
				"status":       req.Card.Status,
				"capabilities": req.Card.Capabilities,
				"triggerTypes": req.Card.TriggerTypes,
				"mockData":     generateMockPreviewData(*req.Card),
			},
		})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "draftType must be skill or card"})
	}
}

func (a *App) handleGeneratorPublishDraft(w http.ResponseWriter, r *http.Request) {
	var req publishDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	switch req.DraftType {
	case "skill":
		if req.Skill == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "skill payload is required"})
			return
		}
		req.Skill.Status = "active"
		writeJSON(w, http.StatusOK, map[string]any{"published": true, "draftType": "skill", "skill": req.Skill})

	case "card":
		if req.Card == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "card payload is required"})
			return
		}
		req.Card.Status = "active"
		req.Card.UpdatedAt = model.NowString()
		// Persist to the UI card store if available.
		if a.uiCardStore != nil {
			if _, exists := a.uiCardStore.Get(req.Card.ID); exists {
				_ = a.uiCardStore.Update(*req.Card)
			} else {
				_ = a.uiCardStore.Add(*req.Card)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"published": true, "draftType": "card", "card": req.Card})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "draftType must be skill or card"})
	}
}
