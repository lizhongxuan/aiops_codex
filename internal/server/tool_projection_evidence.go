package server

import (
	"context"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type evidenceToolProjection struct {
	app *App
}

func NewEvidenceToolProjection(app *App) ToolLifecycleSubscriber {
	return evidenceToolProjection{app: app}
}

func (p evidenceToolProjection) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	if p.app == nil {
		return nil
	}
	p.app.projectToolLifecycleEvidence(event.SessionID, event)
	return nil
}

func (a *App) projectToolLifecycleEvidence(sessionID string, event ToolLifecycleEvent) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	if event.Type != ToolLifecycleEventCompleted && event.Type != ToolLifecycleEventFailed {
		return
	}
	if !toolProjectionBoolFromMaps([]map[string]any{event.Payload, event.Metadata}, "syncActionArtifacts", "sync_action_artifacts") {
		return
	}

	cardID := toolProjectionStringFromMaps([]map[string]any{
		projectionMapFromSource(event.Payload, "finalCard"),
		projectionMapFromSource(event.Metadata, "finalCard"),
	}, "cardId", "cardID", "card_id", "id")
	if strings.TrimSpace(cardID) == "" {
		return
	}

	card := a.cardByID(sessionID, cardID)
	if card == nil {
		return
	}
	a.syncActionArtifacts(sessionID, *card)
}

func (a *App) syncActionArtifacts(sessionID string, card model.Card) {
	if a == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(card.ID) == "" {
		return
	}

	switch card.Type {
	case "FileChangeCard":
		a.bindFileChangeCardEvidence(sessionID, card)
		if refreshed := a.cardByID(sessionID, card.ID); refreshed != nil {
			card = *refreshed
		}
		a.syncActionVerification(sessionID, card)
	}
}

func (a *App) bindFileChangeCardEvidence(sessionID string, card model.Card) string {
	if a == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(card.ID) == "" {
		return ""
	}

	target := strings.TrimSpace(card.ID)
	paths := changePaths(card.Changes)
	if len(paths) > 0 {
		target = paths[0]
	}
	summary := strings.TrimSpace(card.Summary)
	if summary == "" {
		summary = truncate(strings.Join(paths, ", "), 120)
	}
	if summary == "" {
		summary = firstNonEmptyValue(strings.TrimSpace(card.Status), strings.TrimSpace(card.Title))
	}

	return a.bindCardEvidence(sessionID, card.ID, evidenceArtifactInput{
		Kind:       "file_change",
		SourceKind: "config_diff",
		SourceRef:  target,
		Title:      firstNonEmptyValue(strings.TrimSpace(card.Title), "File change"),
		Summary:    summary,
		Content: stableCardJSON(map[string]any{
			"changes": card.Changes,
			"status":  card.Status,
		}),
		Raw: map[string]any{
			"id":      card.ID,
			"status":  card.Status,
			"title":   card.Title,
			"text":    card.Text,
			"summary": card.Summary,
			"hostId":  card.HostID,
			"detail":  cloneAnyMap(card.Detail),
			"changes": append([]model.FileChange(nil), card.Changes...),
		},
		Metadata: map[string]any{
			"status": card.Status,
			"paths":  paths,
		},
	})
}

func toolProjectionBoolFromMaps(sources []map[string]any, keys ...string) bool {
	for _, source := range sources {
		if source == nil {
			continue
		}
		for _, key := range keys {
			if value, ok := source[key]; ok {
				flag, ok := value.(bool)
				if ok {
					return flag
				}
			}
		}
	}
	return false
}
