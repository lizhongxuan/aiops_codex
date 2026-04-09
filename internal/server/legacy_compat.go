package server

import (
	"log"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// isLegacyRouteCard checks if a card was created by the old route-based system.
func isLegacyRouteCard(card model.Card) bool {
	if card.Detail == nil {
		return false
	}
	route, _ := card.Detail["route"].(string)
	return route == "direct_answer" || route == "state_query" || route == "host_readonly" || route == "complex_task"
}

// isLegacyPlannerCard checks if a card references the old PlannerSession.
func isLegacyPlannerCard(card model.Card) bool {
	text := strings.ToLower(card.Text + " " + card.Title + " " + card.Summary)
	return strings.Contains(text, "plannersession") || strings.Contains(text, "planner session")
}

// normalizeLegacyCardForDisplay cleans up legacy card text for display.
// Replaces internal implementation details with user-friendly terms.
func normalizeLegacyCardForDisplay(card *model.Card) {
	card.Text = normalizeLegacyText(card.Text)
	card.Title = normalizeLegacyText(card.Title)
	card.Summary = normalizeLegacyText(card.Summary)
	if card.Message != "" {
		card.Message = normalizeLegacyText(card.Message)
	}
}

// normalizeLegacyText replaces internal terms with user-friendly equivalents.
func normalizeLegacyText(text string) string {
	if text == "" {
		return text
	}
	replacements := []struct{ old, new string }{
		{"PlannerSession", "主 Agent Session"},
		{"plannerSession", "主 Agent Session"},
		{"Planner trace", "执行记录"},
		{"planner trace", "执行记录"},
		{"Planner", "主 Agent"},
		{"planner", "主 Agent"},
		{"影子 session", "内部会话"},
		{"shadow session", "内部会话"},
		{"route thread", "主链路"},
		{"Route Thread", "主链路"},
	}
	result := text
	for _, r := range replacements {
		result = strings.ReplaceAll(result, r.old, r.new)
	}
	return result
}

// ensureLegacyCardsDisplayable processes all cards in a session to ensure
// legacy cards are displayable without crashing due to missing loop fields.
func ensureLegacyCardsDisplayable(cards []model.Card) []model.Card {
	result := make([]model.Card, len(cards))
	for i, card := range cards {
		result[i] = card
		if isLegacyRouteCard(card) || isLegacyPlannerCard(card) {
			normalizeLegacyCardForDisplay(&result[i])
		}
	}
	return result
}

// shouldUseReActLoop determines if a session should use the ReAct loop
// for new messages. Returns true for all new sessions and for old sessions
// that receive new input.
func shouldUseReActLoop(session *model.SessionMeta) bool {
	// All sessions use the ReAct loop for new messages.
	// Legacy route code is only for historical data display.
	return true
}

// logLegacySessionAccess logs when a legacy session is accessed for debugging.
func logLegacySessionAccess(sessionID string, hasRouteCards bool) {
	if hasRouteCards {
		log.Printf("[legacy_compat] session %s has legacy route cards, displaying in compatibility mode", sessionID)
	}
}
