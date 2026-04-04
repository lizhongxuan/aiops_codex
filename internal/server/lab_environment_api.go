package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func (a *App) handleLabEnvironments(w http.ResponseWriter, r *http.Request, _ string) {
	if a.labEnvironmentStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "lab environment store is not initialized"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		items := a.labEnvironmentStore.List()
		result := make([]any, len(items))
		for i, item := range items {
			result[i] = item
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": result,
			"stats": a.labEnvironmentStore.Stats(),
		})

	case http.MethodPost:
		var record model.LabEnvironment
		if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if strings.TrimSpace(record.Name) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if record.ID == "" {
			record.ID = model.NewID("lab")
		}
		if err := a.labEnvironmentStore.Add(record); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		saved, _ := a.labEnvironmentStore.Get(record.ID)
		writeJSON(w, http.StatusCreated, saved)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleLabEnvironmentByID(w http.ResponseWriter, r *http.Request, _ string) {
	if a.labEnvironmentStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "lab environment store is not initialized"})
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/lab-environments/"), "/")
	if path == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "lab environment not found"})
		return
	}

	parts := strings.SplitN(path, "/", 2)
	id := strings.TrimSpace(parts[0])
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "lab environment not found"})
		return
	}

	// Handle action sub-routes: POST /api/v1/lab-environments/{id}/{action}
	if len(parts) == 2 {
		switch parts[1] {
		case "start":
			a.handleLabEnvironmentStart(w, r, id)
		case "stop":
			a.handleLabEnvironmentStop(w, r, id)
		case "inject":
			a.handleLabEnvironmentInject(w, r, id)
		case "reset":
			a.handleLabEnvironmentReset(w, r, id)
		case "exec":
			a.handleLabMockExec(w, r, id)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown action"})
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		record, ok := a.labEnvironmentStore.Get(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "lab environment not found"})
			return
		}
		writeJSON(w, http.StatusOK, record)

	case http.MethodPut:
		var record model.LabEnvironment
		if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		record.ID = id
		if err := a.labEnvironmentStore.Update(record); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		saved, _ := a.labEnvironmentStore.Get(id)
		writeJSON(w, http.StatusOK, saved)

	case http.MethodDelete:
		if err := a.labEnvironmentStore.Delete(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "deleted"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleLabEnvironmentStart(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	env, err := a.labEnvironmentStore.Start(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (a *App) handleLabEnvironmentStop(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	env, err := a.labEnvironmentStore.Stop(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, env)
}

type labInjectRequest struct {
	NodeIDs   []string `json:"nodeIds"`
	FaultType string   `json:"faultType"`
}

func (a *App) handleLabEnvironmentInject(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req labInjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(req.NodeIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nodeIds is required"})
		return
	}
	if strings.TrimSpace(req.FaultType) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "faultType is required"})
		return
	}
	env, err := a.labEnvironmentStore.InjectFault(id, req.NodeIDs, req.FaultType)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (a *App) handleLabEnvironmentReset(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	env, err := a.labEnvironmentStore.Reset(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// labMockCommandResult returns a simulated command result for lab mock hosts.
// This is the v1 mock mode: commands targeting lab hosts return synthetic output
// instead of being dispatched to real agents.
func labMockCommandResult(hostID, command string) map[string]any {
	return map[string]any{
		"hostId":  hostID,
		"command": command,
		"mock":    true,
		"exitCode": 0,
		"stdout":  "[lab-mock] simulated output for: " + command,
		"stderr":  "",
	}
}

// handleLabMockExec handles POST /api/v1/lab-environments/{id}/exec
// to execute a mock command against a lab environment host.
func (a *App) handleLabMockExec(w http.ResponseWriter, r *http.Request, envID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	env, ok := a.labEnvironmentStore.Get(envID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "lab environment not found"})
		return
	}
	if env.Status != "running" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "lab environment is not running"})
		return
	}

	var req struct {
		HostID  string `json:"hostId"`
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}

	// If no hostId specified, use the first mock host.
	targetHostID := req.HostID
	if targetHostID == "" && len(env.MockHostIDs) > 0 {
		targetHostID = env.MockHostIDs[0]
	}

	// Verify the target host belongs to this environment.
	found := false
	for _, hid := range env.MockHostIDs {
		if hid == targetHostID {
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "host not found in this lab environment"})
		return
	}

	writeJSON(w, http.StatusOK, labMockCommandResult(targetHostID, req.Command))
}
