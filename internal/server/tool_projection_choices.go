package server

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type choiceToolProjection struct {
	app *App
}

func NewChoiceToolProjection(app *App) ToolLifecycleSubscriber {
	return choiceToolProjection{app: app}
}

func (p choiceToolProjection) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	if p.app == nil {
		return nil
	}
	switch event.Type {
	case ToolLifecycleEventChoiceRequested:
		p.app.projectChoiceRequested(event.SessionID, event)
	case ToolLifecycleEventChoiceResolved:
		p.app.projectChoiceResolved(event.SessionID, event)
	}
	return nil
}

func (a *App) projectChoiceRequested(sessionID string, event ToolLifecycleEvent) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	choice, card := buildChoiceProjectionObjects(event)
	if strings.TrimSpace(choice.ID) == "" {
		return
	}
	if strings.TrimSpace(choice.Status) == "" {
		choice.Status = "pending"
	}
	if strings.TrimSpace(choice.RequestedAt) == "" {
		choice.RequestedAt = eventTimeString(event)
	}
	if strings.TrimSpace(choice.ItemID) == "" {
		choice.ItemID = firstNonEmptyValue(strings.TrimSpace(card.ID), strings.TrimSpace(event.CardID))
	}
	if strings.TrimSpace(card.ID) == "" {
		card.ID = firstNonEmptyValue(strings.TrimSpace(choice.ItemID), strings.TrimSpace(event.CardID))
	}
	if strings.TrimSpace(card.Type) == "" {
		card.Type = "ChoiceCard"
	}
	if strings.TrimSpace(card.Status) == "" {
		card.Status = "pending"
	}
	if strings.TrimSpace(card.Title) == "" {
		card.Title = choiceCardTitle(choice.Questions)
	}
	if strings.TrimSpace(card.Question) == "" && len(choice.Questions) > 0 {
		card.Question = choice.Questions[0].Question
	}
	if len(card.Questions) == 0 {
		card.Questions = append([]model.ChoiceQuestion(nil), choice.Questions...)
	}
	if len(card.Options) == 0 && len(card.Questions) > 0 {
		card.Options = append([]model.ChoiceOption(nil), card.Questions[0].Options...)
	}
	if strings.TrimSpace(card.RequestID) == "" {
		card.RequestID = choice.ID
	}
	if strings.TrimSpace(card.CreatedAt) == "" {
		card.CreatedAt = choice.RequestedAt
	}
	if strings.TrimSpace(card.UpdatedAt) == "" {
		card.UpdatedAt = choice.RequestedAt
	}

	a.recordOrchestratorChoiceRequested(sessionID, choice)
	a.projectMirroredWorkerChoiceRequested(sessionID, event, choice, card)
}

func (a *App) projectChoiceResolved(sessionID string, event ToolLifecycleEvent) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	choice, card := buildChoiceProjectionObjects(event)
	if strings.TrimSpace(choice.ID) == "" {
		return
	}
	if strings.TrimSpace(choice.Status) == "" {
		choice.Status = "completed"
	}
	if strings.TrimSpace(choice.ResolvedAt) == "" {
		choice.ResolvedAt = eventTimeString(event)
	}
	if strings.TrimSpace(choice.ItemID) == "" {
		choice.ItemID = firstNonEmptyValue(strings.TrimSpace(card.ID), strings.TrimSpace(event.CardID), strings.TrimSpace(choice.ID))
	}
	if strings.TrimSpace(card.ID) == "" {
		card.ID = firstNonEmptyValue(strings.TrimSpace(choice.ItemID), strings.TrimSpace(event.CardID), strings.TrimSpace(choice.ID))
	}
	if strings.TrimSpace(card.Status) == "" {
		card.Status = "completed"
	}
	if strings.TrimSpace(card.UpdatedAt) == "" {
		card.UpdatedAt = choice.ResolvedAt
	}
	if len(card.AnswerSummary) == 0 {
		card.AnswerSummary = choiceAnswerSummary(choice.Questions, choiceAnswerInputsFromModel(choice.Answers))
	}

	a.recordOrchestratorChoiceResolved(sessionID, choice, choiceAnswerInputsFromModel(choice.Answers))
	a.projectMirroredWorkerChoiceResolved(sessionID, event, choice, card)
}

func (a *App) projectMirroredWorkerChoiceRequested(sessionID string, event ToolLifecycleEvent, choice model.ChoiceRequest, card model.Card) {
	if a == nil || a.orchestrator == nil {
		return
	}
	meta := a.sessionMeta(sessionID)
	if meta.Kind != model.SessionKindWorker {
		return
	}
	phase := firstNonEmptyValue(strings.TrimSpace(event.Phase), "waiting_input")
	a.syncWorkerPhaseAndRefreshWorkspace(sessionID, phase)

	if err := a.orchestrator.RegisterChoiceRoute(choice.ID, sessionID); err != nil {
		log.Printf("orchestrator choice route failed choice=%s session=%s err=%v", choice.ID, sessionID, err)
	}

	workspaceSessionID := strings.TrimSpace(meta.WorkspaceSessionID)
	if workspaceSessionID == "" || workspaceSessionID == sessionID {
		return
	}
	a.store.AddChoice(workspaceSessionID, choice)
	a.store.UpsertCard(workspaceSessionID, card)
	a.setRuntimeTurnPhase(workspaceSessionID, phase)
	a.broadcastSnapshot(workspaceSessionID)
}

func (a *App) projectMirroredWorkerChoiceResolved(sessionID string, event ToolLifecycleEvent, choice model.ChoiceRequest, card model.Card) {
	if a == nil {
		return
	}
	meta := a.sessionMeta(sessionID)
	if meta.Kind != model.SessionKindWorker {
		return
	}
	phase := firstNonEmptyValue(strings.TrimSpace(event.Phase), "thinking")
	a.syncWorkerPhaseAndRefreshWorkspace(sessionID, phase)

	workspaceSessionID := strings.TrimSpace(meta.WorkspaceSessionID)
	if workspaceSessionID == "" || workspaceSessionID == sessionID {
		return
	}
	now := firstNonEmptyValue(strings.TrimSpace(choice.ResolvedAt), strings.TrimSpace(card.UpdatedAt), eventTimeString(event))
	a.store.ResolveChoiceWithAnswers(workspaceSessionID, choice.ID, "completed", now, append([]model.ChoiceAnswer(nil), choice.Answers...))
	a.store.UpdateCard(workspaceSessionID, firstNonEmptyValue(strings.TrimSpace(card.ID), strings.TrimSpace(choice.ItemID), strings.TrimSpace(choice.ID)), func(existing *model.Card) {
		existing.Status = "completed"
		existing.AnswerSummary = append([]string(nil), card.AnswerSummary...)
		existing.UpdatedAt = now
	})
	a.broadcastSnapshot(workspaceSessionID)
}

func buildChoiceProjectionObjects(event ToolLifecycleEvent) (model.ChoiceRequest, model.Card) {
	choiceSources := choiceProjectionSources(event)
	cardSources := choiceCardProjectionSources(event)

	questions := toProjectionChoiceQuestions(toolProjectionValueFromMaps(choiceSources, "questions"))
	cardQuestions := toProjectionChoiceQuestions(toolProjectionValueFromMaps(cardSources, "questions"))
	if len(cardQuestions) == 0 {
		cardQuestions = append([]model.ChoiceQuestion(nil), questions...)
	}

	choice := model.ChoiceRequest{
		ID:           toolProjectionStringFromMaps(choiceSources, "choiceId", "choiceID", "choice_id", "requestId", "requestID", "request_id", "id"),
		RequestIDRaw: toolProjectionStringFromMaps(choiceSources, "requestIdRaw", "requestIDRaw", "request_id_raw"),
		ThreadID:     toolProjectionStringFromMaps(choiceSources, "threadId", "threadID", "thread_id"),
		TurnID:       toolProjectionStringFromMaps(choiceSources, "turnId", "turnID", "turn_id"),
		ItemID:       toolProjectionStringFromMaps(choiceSources, "itemId", "itemID", "item_id", "cardId", "cardID", "card_id"),
		Status:       toolProjectionStringFromMaps(choiceSources, "status"),
		Questions:    questions,
		Answers:      toProjectionChoiceAnswers(toolProjectionValueFromMaps(choiceSources, "answers")),
		RequestedAt:  toolProjectionStringFromMaps(choiceSources, "requestedAt", "requested_at"),
		ResolvedAt:   toolProjectionStringFromMaps(choiceSources, "resolvedAt", "resolved_at"),
	}

	card := model.Card{
		ID:            firstNonEmptyValue(toolProjectionStringFromMaps(cardSources, "cardId", "cardID", "card_id", "itemId", "itemID", "item_id"), choice.ItemID, strings.TrimSpace(event.CardID)),
		Type:          firstNonEmptyValue(toolProjectionStringFromMaps(cardSources, "cardType", "card_type"), "ChoiceCard"),
		Title:         toolProjectionStringFromMaps(cardSources, "title"),
		RequestID:     firstNonEmptyValue(toolProjectionStringFromMaps(cardSources, "requestId", "requestID", "request_id"), choice.ID),
		Question:      toolProjectionStringFromMaps(cardSources, "question"),
		Options:       toProjectionChoiceOptions(toolProjectionValueFromMaps(cardSources, "options")),
		Questions:     cardQuestions,
		AnswerSummary: toProjectionStrings(toolProjectionValueFromMaps(cardSources, "answerSummary", "answer_summary")),
		Status:        toolProjectionStringFromMaps(cardSources, "status"),
		CreatedAt:     firstNonEmptyValue(toolProjectionStringFromMaps(cardSources, "createdAt", "created_at"), eventTimeString(event)),
		UpdatedAt:     firstNonEmptyValue(toolProjectionStringFromMaps(cardSources, "updatedAt", "updated_at"), eventTimeString(event)),
	}
	if len(card.Options) == 0 && len(card.Questions) > 0 {
		card.Options = append([]model.ChoiceOption(nil), card.Questions[0].Options...)
	}
	return choice, card
}

func choiceProjectionSources(event ToolLifecycleEvent) []map[string]any {
	return []map[string]any{
		projectionMapFromSource(event.Payload, "choice"),
		projectionMapFromSource(event.Metadata, "choice"),
		event.Payload,
		event.Metadata,
	}
}

func choiceCardProjectionSources(event ToolLifecycleEvent) []map[string]any {
	return []map[string]any{
		projectionMapFromSource(event.Payload, "card"),
		projectionMapFromSource(event.Metadata, "card"),
		event.Payload,
		event.Metadata,
	}
}

func toProjectionChoiceQuestions(raw any) []model.ChoiceQuestion {
	switch typed := raw.(type) {
	case []model.ChoiceQuestion:
		return append([]model.ChoiceQuestion(nil), typed...)
	default:
		return toChoiceQuestions(raw)
	}
}

func toProjectionChoiceOptions(raw any) []model.ChoiceOption {
	switch typed := raw.(type) {
	case []model.ChoiceOption:
		return append([]model.ChoiceOption(nil), typed...)
	default:
		return toChoiceOptions(raw)
	}
}

func toProjectionChoiceAnswers(raw any) []model.ChoiceAnswer {
	switch typed := raw.(type) {
	case []model.ChoiceAnswer:
		return append([]model.ChoiceAnswer(nil), typed...)
	case []any:
		answers := make([]model.ChoiceAnswer, 0, len(typed))
		for _, entry := range typed {
			value, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			answers = append(answers, model.ChoiceAnswer{
				Value:   getString(value, "value"),
				Label:   getString(value, "label"),
				IsOther: getBool(value, "isOther"),
				Note:    getString(value, "note"),
			})
		}
		return answers
	default:
		return nil
	}
}

func toProjectionStrings(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		items := make([]string, 0, len(typed))
		for _, entry := range typed {
			if text := strings.TrimSpace(fmt.Sprint(entry)); text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}
