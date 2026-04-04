package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// handleUICards handles GET (list) and POST (create) on /api/v1/ui-cards.
func (a *App) handleUICards(w http.ResponseWriter, r *http.Request, _ string) {
	if a.uiCardStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui card store is not initialized"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		kind := strings.TrimSpace(r.URL.Query().Get("kind"))
		var items []model.UICardDefinition
		if kind != "" {
			items = a.uiCardStore.ListByKind(kind)
		} else {
			items = a.uiCardStore.List()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": items,
			"stats": a.uiCardStore.Stats(),
			"total": len(items),
		})

	case http.MethodPost:
		var card model.UICardDefinition
		if err := json.NewDecoder(r.Body).Decode(&card); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if card.ID == "" {
			card.ID = model.NewID("uicard")
		}
		if err := a.uiCardStore.Add(card); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, card)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleUICardByID handles GET, PUT, DELETE on /api/v1/ui-cards/{id} and
// POST on /api/v1/ui-cards/{id}/preview.
func (a *App) handleUICardByID(w http.ResponseWriter, r *http.Request, _ string) {
	if a.uiCardStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui card store is not initialized"})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/ui-cards/")
	path = strings.Trim(path, "/")
	if path == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "card definition not found"})
		return
	}

	parts := strings.SplitN(path, "/", 2)
	id := parts[0]

	// Handle /api/v1/ui-cards/{id}/preview
	if len(parts) == 2 && parts[1] == "preview" {
		a.handleUICardPreview(w, r, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		card, ok := a.uiCardStore.Get(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "card definition not found"})
			return
		}
		writeJSON(w, http.StatusOK, card)

	case http.MethodPut:
		var card model.UICardDefinition
		if err := json.NewDecoder(r.Body).Decode(&card); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		card.ID = id
		if err := a.uiCardStore.Update(card); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		updated, _ := a.uiCardStore.Get(id)
		writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		if err := a.uiCardStore.Delete(id); err != nil {
			status := http.StatusNotFound
			if strings.Contains(err.Error(), "built-in") {
				status = http.StatusForbidden
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleUICardPreview returns a mock preview payload for the given card
// definition, useful for the frontend trigger debugger.
func (a *App) handleUICardPreview(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	card, ok := a.uiCardStore.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "card definition not found"})
		return
	}

	// Parse optional input payload for preview.
	var input map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&input)
	}

	preview := map[string]any{
		"cardId":   card.ID,
		"name":     card.Name,
		"kind":     card.Kind,
		"renderer": card.Renderer,
		"status":   card.Status,
		"input":    input,
		"mockData": generateMockPreviewData(card),
	}
	writeJSON(w, http.StatusOK, preview)
}

// generateMockPreviewData produces sample data for a card preview based on
// its kind.
func generateMockPreviewData(card model.UICardDefinition) map[string]any {
	switch card.Kind {
	case "readonly_summary":
		return map[string]any{
			"title":   card.Name + " — 预览",
			"kvRows":  []map[string]string{{"key": "示例指标", "value": "42"}},
			"summary": "这是一条预览摘要。",
		}
	case "readonly_chart":
		return map[string]any{
			"title":      card.Name + " — 预览",
			"dataPoints": []map[string]any{{"ts": "2024-01-01T00:00:00Z", "value": 1.0}},
		}
	case "action_panel":
		return map[string]any{
			"title":   card.Name + " — 预览",
			"actions": []map[string]string{{"label": "示例操作", "action": "noop"}},
		}
	case "form_panel":
		return map[string]any{
			"title":  card.Name + " — 预览",
			"fields": []map[string]string{{"name": "param1", "type": "text", "label": "参数1"}},
		}
	case "monitor_bundle", "remediation_bundle":
		return map[string]any{
			"title":    card.Name + " — 预览",
			"subCards": []string{},
		}
	default:
		return map[string]any{"title": card.Name + " — 预览"}
	}
}
