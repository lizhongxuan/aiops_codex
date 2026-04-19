package server

import (
	"encoding/json"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type evidenceArtifactInput struct {
	ID                 string
	Kind               string
	SourceKind         string
	SourceRef          string
	Title              string
	Summary            string
	Content            string
	Raw                any
	Metadata           map[string]any
	RelatedEvidenceIDs []string
}

func stableEvidenceCitationKey(evidenceID string) string {
	trimmed := strings.TrimSpace(evidenceID)
	if trimmed == "" {
		return ""
	}
	replacer := strings.NewReplacer(" ", "-", "_", "-", "/", "-", "\\", "-", ":", "-", ".", "-", "[", "-", "]", "-", "(", "-", ")", "-")
	normalized := strings.Trim(replacer.Replace(strings.ToUpper(trimmed)), "-")
	return "E-" + normalized
}

func (a *App) rememberEvidenceArtifact(sessionID string, input evidenceArtifactInput) string {
	evidenceID := firstNonEmptyValue(strings.TrimSpace(input.ID), model.NewID("ev"))
	payload := map[string]any{
		"id":          evidenceID,
		"kind":        strings.TrimSpace(input.Kind),
		"sourceKind":  firstNonEmptyValue(strings.TrimSpace(input.SourceKind), strings.TrimSpace(input.Kind)),
		"sourceRef":   strings.TrimSpace(input.SourceRef),
		"citationKey": stableEvidenceCitationKey(evidenceID),
		"title":       strings.TrimSpace(input.Title),
		"summary":     strings.TrimSpace(input.Summary),
		"content":     strings.TrimSpace(input.Content),
		"raw":         input.Raw,
	}
	if len(input.Metadata) > 0 {
		payload["metadata"] = cloneAnyMap(input.Metadata)
	}
	if len(input.RelatedEvidenceIDs) > 0 {
		payload["relatedEvidenceIds"] = append([]string(nil), input.RelatedEvidenceIDs...)
	}
	a.store.RememberItem(sessionID, evidenceID, payload)
	return evidenceID
}

func (a *App) bindCardEvidence(sessionID, cardID string, input evidenceArtifactInput) string {
	evidenceID := input.ID
	if strings.TrimSpace(evidenceID) == "" && strings.TrimSpace(cardID) != "" {
		evidenceID = "evidence-" + strings.TrimSpace(cardID)
	}
	if strings.TrimSpace(cardID) != "" {
		if input.Metadata == nil {
			input.Metadata = make(map[string]any)
		}
		if strings.TrimSpace(getStringAny(input.Metadata, "cardId")) == "" {
			input.Metadata["cardId"] = cardID
		}
	}
	input.ID = evidenceID
	evidenceID = a.rememberEvidenceArtifact(sessionID, input)
	if strings.TrimSpace(cardID) == "" {
		return evidenceID
	}
	a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
		if card.Detail == nil {
			card.Detail = make(map[string]any)
		}
		card.Detail["evidenceId"] = evidenceID
		card.Detail["citationKey"] = stableEvidenceCitationKey(evidenceID)
		if summary := strings.TrimSpace(input.Summary); summary != "" {
			card.Detail["evidenceSummary"] = summary
		}
		if len(input.RelatedEvidenceIDs) > 0 {
			card.Detail["relatedEvidenceIds"] = append([]string(nil), input.RelatedEvidenceIDs...)
		}
		if card.UpdatedAt == "" {
			card.UpdatedAt = model.NowString()
		}
	})
	return evidenceID
}

func (a *App) buildEvidenceDetailPayload(sessionID, evidenceID string) map[string]any {
	snapshot := a.snapshot(sessionID)
	var record *model.EvidenceRecord
	for i := range snapshot.EvidenceSummaries {
		if strings.TrimSpace(snapshot.EvidenceSummaries[i].ID) == strings.TrimSpace(evidenceID) {
			copyRecord := snapshot.EvidenceSummaries[i]
			record = &copyRecord
			break
		}
	}
	if record == nil {
		return nil
	}

	payload := map[string]any{
		"id":                 record.ID,
		"runId":              record.RunID,
		"invocationId":       record.InvocationID,
		"kind":               record.Kind,
		"sourceKind":         record.SourceKind,
		"sourceRef":          record.SourceRef,
		"citationKey":        record.CitationKey,
		"relatedEvidenceIds": append([]string(nil), record.RelatedEvidenceIDs...),
		"title":              record.Title,
		"summary":            record.Summary,
		"content":            record.Content,
		"metadata":           cloneAnyMap(record.Metadata),
		"createdAt":          record.CreatedAt,
	}

	if item := a.store.Item(sessionID, evidenceID); item != nil {
		for _, key := range []string{"kind", "sourceKind", "sourceRef", "citationKey", "title", "summary", "content", "raw", "metadata", "relatedEvidenceIds"} {
			if value, ok := item[key]; ok && value != nil {
				payload[key] = value
			}
		}
	}

	metadata, _ := payload["metadata"].(map[string]any)
	cardID := getStringAny(metadata, "cardId")
	if cardID != "" {
		for _, card := range snapshot.Cards {
			if strings.TrimSpace(card.ID) != cardID {
				continue
			}
			if raw := fullEvidenceContentFromCard(card); raw != "" {
				payload["content"] = raw
			}
			payload["card"] = card
			break
		}
	}
	return payload
}

func fullEvidenceContentFromCard(card model.Card) string {
	switch card.Type {
	case "CommandCard":
		return firstNonEmptyValue(card.Output, strings.TrimSpace(strings.Join([]string{card.Stdout, card.Stderr}, "\n")), card.Text, card.Summary)
	case "PlanCard", "PlanApprovalCard":
		return firstNonEmptyValue(stableCardJSON(card.Detail), card.Text, card.Summary, card.Message)
	case "ResultSummaryCard", "WorkspaceResultCard":
		return firstNonEmptyValue(stableCardJSON(map[string]any{
			"summary":    card.Summary,
			"highlights": card.Highlights,
			"kvRows":     card.KVRows,
			"fileItems":  card.FileItems,
			"text":       card.Text,
			"detail":     card.Detail,
		}), card.Text, card.Summary)
	default:
		return firstNonEmptyValue(card.Output, card.Text, card.Summary, card.Message, stableCardJSON(card.Detail))
	}
}

func stableCardJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
