package server

import (
	"encoding/json"
	"net/http"

	"github.com/lizhongxuan/aiops-codex/internal/generator"
)

// corootSkillsRequest is the optional JSON body for the coroot-skills
// generator endpoint. When Tools is empty the handler falls back to
// DefaultCorootTools().
type corootSkillsRequest struct {
	Tools []generator.CorootToolMeta `json:"tools,omitempty"`
}

// corootSkillsResponse is returned by POST /api/v1/generator/coroot-skills.
type corootSkillsResponse struct {
	Skills []corootSkillSummary   `json:"skills"`
	Lint   []generator.LintIssue  `json:"lint"`
	Count  int                    `json:"count"`
}

// corootSkillSummary is a lightweight view of a generated skill plus its
// per-skill lint issues.
type corootSkillSummary struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Description  string                `json:"description"`
	Category     string                `json:"category"`
	Status       string                `json:"status"`
	Dependencies []string              `json:"dependencies,omitempty"`
	LintIssues   []generator.LintIssue `json:"lintIssues,omitempty"`
}

// handleCorootSkillsGenerate handles POST /api/v1/generator/coroot-skills.
// It accepts an optional JSON body with a tools array; when absent or empty
// the default Coroot tool definitions are used.
func (a *App) handleCorootSkillsGenerate(w http.ResponseWriter, r *http.Request, svc *generator.GeneratorService) {
	var req corootSkillsRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
	}

	tools := req.Tools
	if len(tools) == 0 {
		tools = generator.DefaultCorootTools()
	}

	skills, err := svc.GenerateSkillsFromCoroot(tools)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Collect per-skill lint results and an aggregate list.
	var allIssues []generator.LintIssue
	summaries := make([]corootSkillSummary, 0, len(skills))
	for i := range skills {
		issues := svc.Lint(&skills[i])
		allIssues = append(allIssues, issues...)
		summaries = append(summaries, corootSkillSummary{
			ID:           skills[i].ID,
			Name:         skills[i].Name,
			Description:  skills[i].Description,
			Category:     skills[i].Category,
			Status:       skills[i].Status,
			Dependencies: skills[i].Dependencies,
			LintIssues:   issues,
		})
	}

	writeJSON(w, http.StatusOK, corootSkillsResponse{
		Skills: summaries,
		Lint:   allIssues,
		Count:  len(summaries),
	})
}
