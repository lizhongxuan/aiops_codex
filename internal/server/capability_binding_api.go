package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// handleCapabilityBindings handles GET (list) and POST (create) on
// /api/v1/capability-bindings.
func (a *App) handleCapabilityBindings(w http.ResponseWriter, r *http.Request, _ string) {
	if a.capabilityBindingStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "capability binding store is not initialized"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		sourceType := strings.TrimSpace(r.URL.Query().Get("sourceType"))
		sourceID := strings.TrimSpace(r.URL.Query().Get("sourceId"))
		var items []model.CapabilityBinding
		if sourceType != "" && sourceID != "" {
			items = a.capabilityBindingStore.ListBySource(sourceType, sourceID)
		} else {
			items = a.capabilityBindingStore.List()
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})

	case http.MethodPost:
		var binding model.CapabilityBinding
		if err := json.NewDecoder(r.Body).Decode(&binding); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if binding.ID == "" {
			binding.ID = model.NewID("cbind")
		}
		if err := a.capabilityBindingStore.Add(binding); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, binding)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleCapabilityBindingByID handles GET, PUT, DELETE on
// /api/v1/capability-bindings/{id}.
func (a *App) handleCapabilityBindingByID(w http.ResponseWriter, r *http.Request, _ string) {
	if a.capabilityBindingStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "capability binding store is not initialized"})
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/capability-bindings/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "binding not found"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		binding, ok := a.capabilityBindingStore.Get(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "binding not found"})
			return
		}
		writeJSON(w, http.StatusOK, binding)

	case http.MethodPut:
		var binding model.CapabilityBinding
		if err := json.NewDecoder(r.Body).Decode(&binding); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		binding.ID = id
		if err := a.capabilityBindingStore.Update(binding); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		updated, _ := a.capabilityBindingStore.Get(id)
		writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		if err := a.capabilityBindingStore.Delete(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
