package server

import (
	"context"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type cardToolProjection struct {
	app *App
}

func NewCardToolProjection(app *App) ToolLifecycleSubscriber {
	return cardToolProjection{app: app}
}

func (p cardToolProjection) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	if p.app == nil {
		return nil
	}
	p.app.projectToolLifecycleCards(event.SessionID, event)
	return nil
}

func (a *App) projectToolLifecycleCards(sessionID string, event ToolLifecycleEvent) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	display := toolProjectionDisplayMapFromEvent(event)
	if shouldSkipToolLifecycleCardProjection(event, display) {
		return
	}

	switch event.Type {
	case ToolLifecycleEventStarted:
		cardID := strings.TrimSpace(event.CardID)
		if cardID == "" {
			cardID = model.NewID("proc")
		}
		phase := firstNonEmptyValue(strings.TrimSpace(event.Phase), "executing")
		text := firstNonEmptyValue(
			strings.TrimSpace(event.Label),
			strings.TrimSpace(event.Message),
			toolProjectionDisplaySummaryFromMap(display),
			toolProjectionDisplayActivityFromMap(display),
			strings.TrimSpace(event.ToolName),
		)
		if text == "" {
			text = "工具开始执行"
		}
		if display != nil {
			a.upsertToolProcessDisplay(sessionID, cardID, phase, text, display)
			return
		}
		a.beginToolProcess(sessionID, cardID, phase, text)
	case ToolLifecycleEventProgress:
		cardID := strings.TrimSpace(event.CardID)
		if cardID == "" {
			cardID = model.NewID("proc")
		}
		phase := firstNonEmptyValue(strings.TrimSpace(event.Phase), "executing")
		text := firstNonEmptyValue(
			strings.TrimSpace(event.Message),
			strings.TrimSpace(event.Label),
			toolProjectionDisplaySummaryFromMap(display),
			toolProjectionDisplayActivityFromMap(display),
			strings.TrimSpace(event.ToolName),
		)
		if text == "" {
			text = "工具执行中"
		}
		if !a.updateToolProcessProgress(sessionID, cardID, text, display) {
			if display != nil {
				a.upsertToolProcessDisplay(sessionID, cardID, phase, text, display)
			} else {
				a.beginToolProcess(sessionID, cardID, phase, text)
			}
			return
		}
	case ToolLifecycleEventCompleted:
		cardID := strings.TrimSpace(event.CardID)
		if cardID == "" {
			a.projectToolLifecycleFinalCard(sessionID, event)
			return
		}
		text := firstNonEmptyValue(
			strings.TrimSpace(event.Message),
			strings.TrimSpace(event.Label),
			toolProjectionDisplaySummaryFromMap(display),
			toolProjectionDisplayActivityFromMap(display),
			strings.TrimSpace(event.ToolName),
		)
		if text == "" {
			text = "工具已完成"
		}
		a.completeToolProcessDisplay(sessionID, cardID, text, display)
		a.projectToolLifecycleFinalCard(sessionID, event)
	case ToolLifecycleEventFailed:
		cardID := strings.TrimSpace(event.CardID)
		if cardID == "" {
			a.projectToolLifecycleFinalCard(sessionID, event)
			return
		}
		text := firstNonEmptyValue(
			strings.TrimSpace(event.Error),
			strings.TrimSpace(event.Message),
			strings.TrimSpace(event.Label),
			toolProjectionDisplaySummaryFromMap(display),
			toolProjectionDisplayActivityFromMap(display),
		)
		if text == "" {
			text = "工具执行失败"
		}
		a.failToolProcessDisplay(sessionID, cardID, text, display)
		a.projectToolLifecycleFinalCard(sessionID, event)
	}
}

func (a *App) updateToolProcessProgress(sessionID, cardID, text string, display map[string]any) bool {
	if a == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(cardID) == "" {
		return false
	}
	updated := false
	a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
		card.Text = text
		card.Status = "inProgress"
		card.Detail = toolProjectionDisplayDetailMap(display, card.Detail)
		card.UpdatedAt = model.NowString()
		updated = true
	})
	return updated
}

func (a *App) projectToolLifecycleFinalCard(sessionID string, event ToolLifecycleEvent) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}

	display := toolProjectionDisplayMapFromEvent(event)
	cardFields := toolProjectionFinalCardMapFromEvent(event)
	if cardFields == nil {
		return
	}

	sources := []map[string]any{cardFields}
	card := model.Card{
		ID:       toolProjectionStringFromMaps(sources, "cardId", "cardID", "card_id", "id"),
		Type:     toolProjectionStringFromMaps(sources, "cardType", "card_type", "type"),
		Title:    toolProjectionStringFromMaps(sources, "title", "label"),
		Text:     toolProjectionStringFromMaps(sources, "text", "message"),
		Summary:  toolProjectionStringFromMaps(sources, "summary"),
		Status:   toolProjectionStringFromMaps(sources, "status"),
		Command:  toolProjectionStringFromMaps(sources, "command"),
		Cwd:      toolProjectionStringFromMaps(sources, "cwd"),
		HostID:   toolProjectionStringFromMaps(sources, "hostId", "hostID", "host_id"),
		HostName: toolProjectionStringFromMaps(sources, "hostName", "host_name"),
		Changes:  toChanges(toolProjectionValueFromMaps(sources, "changes")),
		Detail: func() map[string]any {
			if detail, ok := toolProjectionValueFromMaps(sources, "detail").(map[string]any); ok {
				return toolProjectionDisplayDetailMap(display, detail)
			}
			return toolProjectionDisplayDetailMap(display, nil)
		}(),
		CreatedAt: toolProjectionStringFromMaps(sources, "createdAt", "created_at"),
		UpdatedAt: firstNonEmptyValue(toolProjectionStringFromMaps(sources, "updatedAt", "updated_at"), eventTimeString(event)),
	}
	if card.ID == "" {
		return
	}
	if card.Type == "" {
		card.Type = "ResultCard"
	}
	if card.Status == "" {
		switch event.Type {
		case ToolLifecycleEventCompleted:
			card.Status = "completed"
		case ToolLifecycleEventFailed:
			card.Status = "failed"
		default:
			card.Status = "completed"
		}
	}
	if card.HostID == "" {
		card.HostID = defaultHostID(event.HostID)
	}
	if card.HostName == "" && card.HostID != "" {
		card.HostName = hostNameOrID(a.findHost(card.HostID))
	}
	if existing := a.cardByID(sessionID, card.ID); existing != nil {
		if card.CreatedAt == "" {
			card.CreatedAt = existing.CreatedAt
		}
		if card.Type == "" {
			card.Type = existing.Type
		}
		if card.Title == "" {
			card.Title = existing.Title
		}
		if card.Text == "" {
			card.Text = existing.Text
		}
		if card.Summary == "" {
			card.Summary = existing.Summary
		}
		if card.Command == "" {
			card.Command = existing.Command
		}
		if card.Cwd == "" {
			card.Cwd = existing.Cwd
		}
		if card.HostID == "" {
			card.HostID = existing.HostID
		}
		if card.HostName == "" {
			card.HostName = existing.HostName
		}
		if len(card.Changes) == 0 {
			card.Changes = append([]model.FileChange(nil), existing.Changes...)
		}
		if card.Detail == nil && len(existing.Detail) > 0 {
			card.Detail = cloneAnyMap(existing.Detail)
		}
		if card.Detail != nil {
			card.Detail = toolProjectionDisplayDetailMap(display, card.Detail)
		}
		if existing.Approval != nil {
			card.Approval = &model.ApprovalRef{
				RequestID: existing.Approval.RequestID,
				Type:      existing.Approval.Type,
				Decisions: append([]string(nil), existing.Approval.Decisions...),
			}
		}
	}
	if card.CreatedAt == "" {
		card.CreatedAt = eventTimeString(event)
	}
	a.store.UpsertCard(sessionID, card)
}

func (a *App) upsertToolProcessDisplay(sessionID, cardID, phase, text string, display map[string]any) {
	if a == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(cardID) == "" {
		return
	}
	now := model.NowString()
	a.setRuntimeTurnPhase(sessionID, phase)
	a.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "ProcessLineCard",
		Text:      text,
		Status:    "inProgress",
		Detail:    toolProjectionDisplayDetailMap(display, nil),
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func shouldSkipToolLifecycleCardProjection(event ToolLifecycleEvent, display map[string]any) bool {
	for _, source := range []map[string]any{event.Payload, event.Metadata, display} {
		if len(source) == 0 {
			continue
		}
		for _, key := range []string{"skipCardProjection", "skipCards", "skip_cards"} {
			value, ok := source[key].(bool)
			if ok && value {
				return true
			}
		}
	}
	return false
}

func toolProjectionDisplayMapFromEvent(event ToolLifecycleEvent) map[string]any {
	for _, source := range []map[string]any{event.Payload, event.Metadata} {
		display := projectionMapFromSource(source, "display")
		if display != nil {
			return cloneNestedAnyMap(display)
		}
	}
	return nil
}

func toolProjectionFinalCardMapFromEvent(event ToolLifecycleEvent) map[string]any {
	for _, source := range []map[string]any{projectionMapFromSource(event.Payload, "finalCard"), projectionMapFromSource(event.Metadata, "finalCard")} {
		if source != nil {
			return source
		}
	}
	if display := toolProjectionDisplayMapFromEvent(event); display != nil {
		if finalCard, ok := asStringAnyMap(display["finalCard"]); ok {
			return finalCard
		}
	}
	return nil
}

func toolProjectionDisplaySummaryFromMap(display map[string]any) string {
	return toolProjectionStringFromMaps([]map[string]any{display}, "summary")
}

func toolProjectionDisplayActivityFromMap(display map[string]any) string {
	return toolProjectionStringFromMaps([]map[string]any{display}, "activity")
}

func toolProjectionDisplayDetailMap(display, existing map[string]any) map[string]any {
	if len(display) == 0 && len(existing) == 0 {
		return nil
	}

	detail := cloneAnyMap(existing)
	if len(detail) == 0 && len(display) == 0 {
		return nil
	}
	if len(detail) == 0 {
		detail = make(map[string]any)
	}
	if len(display) > 0 {
		detail["display"] = cloneNestedAnyMap(display)
	} else if existingDisplay := toolProjectionDisplayMapFromDetail(existing); len(existingDisplay) > 0 {
		detail["display"] = existingDisplay
	}
	if len(detail) == 0 {
		return nil
	}
	return detail
}

func toolProjectionDisplayMapFromDetail(detail map[string]any) map[string]any {
	if len(detail) == 0 {
		return nil
	}
	value, ok := detail["display"]
	if !ok {
		return nil
	}
	if display, ok := asStringAnyMap(value); ok {
		return cloneNestedAnyMap(display)
	}
	return nil
}
