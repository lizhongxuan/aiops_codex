package server

import (
	"context"
	"log"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func (a *App) emitChoiceRequestedEvent(ctx context.Context, sessionID string, choice model.ChoiceRequest, card model.Card) bool {
	if a == nil || a.toolEventBus == nil {
		return false
	}
	event := newChoiceRequestedLifecycleEvent(sessionID, choice, card)
	if err := a.toolEventBus.Emit(ctx, event); err != nil {
		log.Printf("failed to emit choice requested event session=%s choice=%s err=%v", sessionID, choice.ID, err)
		return false
	}
	return true
}

func (a *App) emitChoiceResolvedEvent(ctx context.Context, sessionID, phase string, choice model.ChoiceRequest, card model.Card) bool {
	if a == nil || a.toolEventBus == nil {
		return false
	}
	event := newChoiceResolvedLifecycleEvent(sessionID, phase, choice, card)
	if err := a.toolEventBus.Emit(ctx, event); err != nil {
		log.Printf("failed to emit choice resolved event session=%s choice=%s err=%v", sessionID, choice.ID, err)
		return false
	}
	return true
}

func newChoiceRequestedLifecycleEvent(sessionID string, choice model.ChoiceRequest, card model.Card) ToolLifecycleEvent {
	return ToolLifecycleEvent{
		EventID:   model.NewID("toolevent"),
		SessionID: sessionID,
		ToolName:  "ask_user_question",
		Type:      ToolLifecycleEventChoiceRequested,
		Phase:     "waiting_input",
		CardID:    card.ID,
		Label: firstNonEmptyValue(
			strings.TrimSpace(card.Title),
			choiceCardTitle(choice.Questions),
			"需要用户选择",
		),
		Message: firstNonEmptyValue(
			strings.TrimSpace(card.Question),
			strings.TrimSpace(card.Title),
			"需要用户选择",
		),
		CreatedAt: firstNonEmptyValue(strings.TrimSpace(choice.RequestedAt), strings.TrimSpace(card.CreatedAt), model.NowString()),
		Payload: map[string]any{
			"choice": choiceLifecyclePayload(choice),
			"card":   choiceLifecycleCardPayload(card),
		},
	}
}

func newChoiceResolvedLifecycleEvent(sessionID, phase string, choice model.ChoiceRequest, card model.Card) ToolLifecycleEvent {
	return ToolLifecycleEvent{
		EventID:   model.NewID("toolevent"),
		SessionID: sessionID,
		ToolName:  "ask_user_question",
		Type:      ToolLifecycleEventChoiceResolved,
		Phase:     firstNonEmptyValue(strings.TrimSpace(phase), "thinking"),
		CardID:    card.ID,
		Label: firstNonEmptyValue(
			strings.TrimSpace(card.Title),
			choiceCardTitle(choice.Questions),
			"已收到用户选择",
		),
		Message: firstNonEmptyValue(
			strings.Join(card.AnswerSummary, "; "),
			strings.TrimSpace(card.Title),
			"已收到用户选择",
		),
		CreatedAt: firstNonEmptyValue(strings.TrimSpace(choice.ResolvedAt), strings.TrimSpace(card.UpdatedAt), model.NowString()),
		Payload: map[string]any{
			"choice": choiceLifecyclePayload(choice),
			"card":   choiceLifecycleCardPayload(card),
		},
	}
}

func choiceLifecyclePayload(choice model.ChoiceRequest) map[string]any {
	return map[string]any{
		"choiceId":     choice.ID,
		"requestIdRaw": choice.RequestIDRaw,
		"threadId":     choice.ThreadID,
		"turnId":       choice.TurnID,
		"itemId":       choice.ItemID,
		"status":       choice.Status,
		"questions":    append([]model.ChoiceQuestion(nil), choice.Questions...),
		"answers":      append([]model.ChoiceAnswer(nil), choice.Answers...),
		"requestedAt":  choice.RequestedAt,
		"resolvedAt":   choice.ResolvedAt,
	}
}

func choiceLifecycleCardPayload(card model.Card) map[string]any {
	return map[string]any{
		"cardId":        card.ID,
		"cardType":      card.Type,
		"title":         card.Title,
		"requestId":     card.RequestID,
		"question":      card.Question,
		"options":       append([]model.ChoiceOption(nil), card.Options...),
		"questions":     append([]model.ChoiceQuestion(nil), card.Questions...),
		"answerSummary": append([]string(nil), card.AnswerSummary...),
		"status":        card.Status,
		"createdAt":     card.CreatedAt,
		"updatedAt":     card.UpdatedAt,
	}
}
