package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func (a *App) handleScriptConfigs(w http.ResponseWriter, r *http.Request, _ string) {
	if a.scriptConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "script config store is not initialized"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		scriptName := strings.TrimSpace(r.URL.Query().Get("scriptName"))
		var items []model.ScriptConfigProfile
		if scriptName != "" {
			items = a.scriptConfigStore.ListByScript(scriptName)
		} else {
			items = a.scriptConfigStore.List()
		}
		// Ensure JSON always emits [] instead of null.
		result := make([]any, len(items))
		for i, item := range items {
			result[i] = item
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": result,
			"stats": a.scriptConfigStore.Stats(),
		})

	case http.MethodPost:
		var record model.ScriptConfigProfile
		if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if strings.TrimSpace(record.ScriptName) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scriptName is required"})
			return
		}
		if record.ID == "" {
			record.ID = model.NewID("sc")
		}
		if err := a.scriptConfigStore.Add(record); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		saved, _ := a.scriptConfigStore.Get(record.ID)
		writeJSON(w, http.StatusCreated, saved)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleScriptConfigByID(w http.ResponseWriter, r *http.Request, _ string) {
	if a.scriptConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "script config store is not initialized"})
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/script-configs/"), "/")
	if path == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "script config not found"})
		return
	}

	parts := strings.SplitN(path, "/", 2)
	id := strings.TrimSpace(parts[0])
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "script config not found"})
		return
	}

	// Handle dry-run sub-route: POST /api/v1/script-configs/{id}/dry-run
	if len(parts) == 2 && parts[1] == "dry-run" {
		a.handleScriptConfigDryRun(w, r, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		record, ok := a.scriptConfigStore.Get(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "script config not found"})
			return
		}
		writeJSON(w, http.StatusOK, record)

	case http.MethodPut:
		var record model.ScriptConfigProfile
		if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		record.ID = id
		if err := a.scriptConfigStore.Update(record); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		saved, _ := a.scriptConfigStore.Get(id)
		writeJSON(w, http.StatusOK, saved)

	case http.MethodDelete:
		if err := a.scriptConfigStore.Delete(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "deleted"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleScriptConfigDryRun renders the script template with the config
// profile defaults merged with user-supplied parameters and returns the
// command preview without executing.
func (a *App) handleScriptConfigDryRun(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	record, ok := a.scriptConfigStore.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "script config not found"})
		return
	}

	// Parse optional user-supplied parameter overrides.
	var userParams map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&userParams)
	}

	// Merge defaults with user params (user params take precedence).
	merged := make(map[string]any)
	for k, v := range record.Defaults {
		merged[k] = v
	}
	for k, v := range userParams {
		merged[k] = v
	}

	// Build a simple command preview from the script name and merged params.
	preview := record.ScriptName
	for k, v := range merged {
		preview += fmt.Sprintf(" --%s=%v", k, v)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"configId":       record.ID,
		"scriptName":     record.ScriptName,
		"mergedParams":   merged,
		"commandPreview": preview,
		"approvalPolicy": record.ApprovalPolicy,
		"environmentRef": record.EnvironmentRef,
		"dryRun":         true,
	})
}
