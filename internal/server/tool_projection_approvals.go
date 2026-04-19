package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type approvalToolProjection struct {
	app *App
}

func NewApprovalToolProjection(app *App) ToolLifecycleSubscriber {
	return approvalToolProjection{app: app}
}

func (p approvalToolProjection) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	if p.app == nil {
		return nil
	}
	p.app.projectToolLifecycleApprovals(event.SessionID, event)
	return nil
}

func (a *App) projectToolLifecycleApprovals(sessionID string, event ToolLifecycleEvent) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}

	switch event.Type {
	case ToolLifecycleEventApprovalRequested:
		a.projectApprovalRequested(sessionID, event)
	case ToolLifecycleEventApprovalResolved:
		a.projectApprovalResolved(sessionID, event)
	}
}

func (a *App) projectApprovalRequested(sessionID string, event ToolLifecycleEvent) {
	approval, card := buildApprovalProjectionObjects(event, true)
	if approval.ID == "" {
		return
	}

	if approval.Status == "" {
		approval.Status = "pending"
	}
	if approval.RequestedAt == "" {
		approval.RequestedAt = eventTimeString(event)
	}
	if approval.Decisions == nil {
		approval.Decisions = []string{"accept", "decline"}
	}
	if approval.ItemID == "" {
		approval.ItemID = card.ID
	}
	if card.ID == "" {
		card.ID = firstNonEmptyValue(approval.ItemID, model.NewID("approval"))
	}
	if card.Type == "" {
		card.Type = "ApprovalCard"
	}
	if card.Status == "" {
		card.Status = "pending"
	}
	if card.Title == "" {
		card.Title = approvalTitleFromEvent(event, approval)
	}
	if card.Text == "" {
		card.Text = approvalTextFromEvent(event, approval)
	}
	card.Approval = &model.ApprovalRef{
		RequestID: approval.ID,
		Type:      approval.Type,
		Decisions: append([]string(nil), approval.Decisions...),
	}
	if card.Detail == nil {
		card.Detail = mergedProjectionDetail(event)
	}

	a.store.AddApproval(sessionID, approval)
	a.store.UpsertCard(sessionID, card)
}

func (a *App) projectApprovalResolved(sessionID string, event ToolLifecycleEvent) {
	approvalProjection, projectedCard := buildApprovalProjectionObjects(event, false)
	approvalID := firstNonEmptyValue(
		strings.TrimSpace(approvalProjection.ID),
		toolProjectionStringFromMaps(approvalProjectionSources(event), "approvalId", "approvalID", "approval_id", "requestId", "requestID", "request_id", "id"),
	)
	cardID := firstNonEmptyValue(
		strings.TrimSpace(projectedCard.ID),
		toolProjectionStringFromMaps(cardProjectionSources(event), "cardId", "cardID", "card_id", "itemId", "itemID", "item_id"),
	)
	status := firstNonEmptyValue(
		toolProjectionStringFromMaps(approvalProjectionSources(event), "status", "approvalStatus", "approval_status", "decision"),
		"accepted",
	)
	resolvedAt := firstNonEmptyValue(toolProjectionStringFromMaps(approvalProjectionSources(event), "resolvedAt", "resolved_at"), eventTimeString(event))

	if approvalID == "" && cardID == "" {
		return
	}

	var approval model.ApprovalRequest
	var ok bool
	if approvalID != "" {
		approval, ok = a.store.Approval(sessionID, approvalID)
		if !ok && approvalProjection.ID != "" {
			approval = approvalProjection
			if approval.Status == "" {
				approval.Status = status
			}
			if approval.RequestedAt == "" {
				approval.RequestedAt = firstNonEmptyValue(approvalProjection.RequestedAt, resolvedAt)
			}
			if approval.ResolvedAt == "" {
				approval.ResolvedAt = resolvedAt
			}
			a.store.AddApproval(sessionID, approval)
			ok = true
		}
		if ok {
			if cardID == "" {
				cardID = approval.ItemID
			}
			if approval.ItemID == "" && approvalProjection.ItemID != "" {
				approval.ItemID = approvalProjection.ItemID
			}
		}
	}

	if approvalID != "" {
		a.store.ResolveApproval(sessionID, approvalID, status, resolvedAt)
	}
	if approval.ID != "" {
		approval.Status = status
		approval.ResolvedAt = resolvedAt
	}

	cardStatus := firstNonEmptyValue(strings.TrimSpace(projectedCard.Status), approvalStatusToCardStatus(status))
	if cardID == "" {
		if approval.ItemID != "" {
			cardID = approval.ItemID
		}
	}
	if cardID == "" {
		return
	}

	title := firstNonEmptyValue(strings.TrimSpace(projectedCard.Title), approvalTitleFromEvent(event, approval))
	text := firstNonEmptyValue(strings.TrimSpace(projectedCard.Text), approvalTextFromEvent(event, approval))
	summary := firstNonEmptyValue(strings.TrimSpace(projectedCard.Summary), toolProjectionStringFromMaps(cardProjectionSources(event), "summary", "message"))
	cardType := firstNonEmptyValue(strings.TrimSpace(projectedCard.Type), toolProjectionStringFromMaps(cardProjectionSources(event), "cardType", "card_type"), "ApprovalCard")
	cardDetail := projectedCard.Detail
	originalApprovalCardID := ""
	if approval.ID != "" {
		originalApprovalCardID = strings.TrimSpace(approval.ItemID)
	} else {
		originalApprovalCardID = strings.TrimSpace(approvalProjection.ItemID)
	}
	if originalApprovalCardID != "" && originalApprovalCardID != cardID {
		a.store.UpdateCard(sessionID, originalApprovalCardID, func(card *model.Card) {
			card.Status = approvalStatusToCardStatus(status)
			card.UpdatedAt = resolvedAt
		})
	}

	updated := false
	a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
		card.Type = firstNonEmptyValue(card.Type, cardType)
		card.Status = cardStatus
		if title != "" {
			card.Title = title
		}
		if text != "" {
			card.Text = text
		}
		if summary != "" {
			card.Summary = summary
		}
		if projectedCard.Command != "" {
			card.Command = projectedCard.Command
		}
		if projectedCard.Cwd != "" {
			card.Cwd = projectedCard.Cwd
		}
		if projectedCard.HostID != "" {
			card.HostID = projectedCard.HostID
		}
		if projectedCard.HostName != "" {
			card.HostName = projectedCard.HostName
		}
		if len(projectedCard.Changes) > 0 {
			card.Changes = append([]model.FileChange(nil), projectedCard.Changes...)
		}
		if cardDetail != nil {
			card.Detail = cloneAnyMap(cardDetail)
		} else if card.Detail == nil {
			card.Detail = mergedProjectionDetail(event)
		}
		card.UpdatedAt = resolvedAt
		updated = true
	})
	if updated {
		return
	}

	card := model.Card{
		ID:       cardID,
		Type:     cardType,
		Title:    title,
		Text:     text,
		Summary:  summary,
		Status:   cardStatus,
		Command:  projectedCard.Command,
		Cwd:      projectedCard.Cwd,
		HostID:   projectedCard.HostID,
		HostName: projectedCard.HostName,
		Changes:  append([]model.FileChange(nil), projectedCard.Changes...),
		Detail: func() map[string]any {
			if cardDetail != nil {
				return cloneAnyMap(cardDetail)
			}
			return mergedProjectionDetail(event)
		}(),
		UpdatedAt: resolvedAt,
		CreatedAt: resolvedAt,
	}
	if approval.ID != "" {
		card.Approval = &model.ApprovalRef{
			RequestID: approval.ID,
			Type:      approval.Type,
			Decisions: append([]string(nil), approval.Decisions...),
		}
	}
	a.store.UpsertCard(sessionID, card)
}

func buildApprovalProjectionObjects(event ToolLifecycleEvent, requested bool) (model.ApprovalRequest, model.Card) {
	approval := model.ApprovalRequest{
		ID:           firstNonEmptyValue(toolProjectionStringFromMaps(approvalProjectionSources(event), "approvalId", "approvalID", "approval_id", "requestId", "requestID", "request_id", "id"), ""),
		RequestIDRaw: toolProjectionStringFromMaps(approvalProjectionSources(event), "requestIdRaw", "requestIDRaw", "request_id_raw"),
		HostID:       toolProjectionStringFromMaps(approvalProjectionSources(event), "hostId", "hostID", "host_id"),
		Fingerprint:  toolProjectionStringFromMaps(approvalProjectionSources(event), "fingerprint"),
		Type:         toolProjectionStringFromMaps(approvalProjectionSources(event), "approvalType", "approval_type", "approvalKind", "approval_kind"),
		Status:       toolProjectionStringFromMaps(approvalProjectionSources(event), "status", "approvalStatus", "approval_status"),
		ThreadID:     toolProjectionStringFromMaps(approvalProjectionSources(event), "threadId", "threadID", "thread_id"),
		TurnID:       toolProjectionStringFromMaps(approvalProjectionSources(event), "turnId", "turnID", "turn_id"),
		ItemID:       toolProjectionStringFromMaps(approvalProjectionSources(event), "cardId", "cardID", "card_id", "itemId", "itemID", "item_id"),
		Command:      toolProjectionStringFromMaps(approvalProjectionSources(event), "command"),
		Cwd:          toolProjectionStringFromMaps(approvalProjectionSources(event), "cwd"),
		Reason:       toolProjectionStringFromMaps(approvalProjectionSources(event), "reason", "message", "text", "summary"),
		GrantRoot:    toolProjectionStringFromMaps(approvalProjectionSources(event), "grantRoot", "grant_root"),
		RequestedAt:  toolProjectionStringFromMaps(approvalProjectionSources(event), "requestedAt", "requested_at"),
		ResolvedAt:   toolProjectionStringFromMaps(approvalProjectionSources(event), "resolvedAt", "resolved_at"),
		Changes:      toChanges(toolProjectionValueFromMaps(approvalProjectionSources(event), "changes")),
	}
	if approval.ID == "" && requested {
		approval.ID = firstNonEmptyValue(approval.ItemID, model.NewID("approval"))
	}
	approval.Decisions = toolProjectionStringsFromMaps(approvalProjectionSources(event), "decisions", "options")

	card := model.Card{
		ID:        firstNonEmptyValue(toolProjectionStringFromMaps(cardProjectionSources(event), "cardId", "cardID", "card_id", "itemId", "itemID", "item_id"), approval.ItemID),
		Type:      toolProjectionStringFromMaps(cardProjectionSources(event), "cardType", "card_type", "type"),
		Role:      toolProjectionStringFromMaps(cardProjectionSources(event), "role"),
		Title:     toolProjectionStringFromMaps(cardProjectionSources(event), "title", "label"),
		Text:      toolProjectionStringFromMaps(cardProjectionSources(event), "text", "message", "summary", "reason"),
		Summary:   toolProjectionStringFromMaps(cardProjectionSources(event), "summary"),
		Status:    toolProjectionStringFromMaps(cardProjectionSources(event), "status"),
		Command:   toolProjectionStringFromMaps(cardProjectionSources(event), "command"),
		Cwd:       toolProjectionStringFromMaps(cardProjectionSources(event), "cwd"),
		HostID:    toolProjectionStringFromMaps(cardProjectionSources(event), "hostId", "hostID", "host_id"),
		HostName:  toolProjectionStringFromMaps(cardProjectionSources(event), "hostName", "host_name"),
		Changes:   toChanges(toolProjectionValueFromMaps(cardProjectionSources(event), "changes")),
		Detail:    explicitProjectionDetail(event),
		CreatedAt: firstNonEmptyValue(toolProjectionStringFromMaps(cardProjectionSources(event), "createdAt", "created_at"), eventTimeString(event)),
		UpdatedAt: firstNonEmptyValue(toolProjectionStringFromMaps(cardProjectionSources(event), "updatedAt", "updated_at"), eventTimeString(event)),
	}
	if card.Command == "" {
		card.Command = approval.Command
	}
	if card.Cwd == "" {
		card.Cwd = approval.Cwd
	}
	if card.HostID == "" {
		card.HostID = approval.HostID
	}
	if len(card.Changes) == 0 && len(approval.Changes) > 0 {
		card.Changes = append([]model.FileChange(nil), approval.Changes...)
	}
	if requested && card.Status == "" {
		card.Status = "pending"
	}
	if card.Type == "" && requested {
		card.Type = "ApprovalCard"
	}
	if len(approval.Decisions) == 0 {
		approval.Decisions = []string{"accept", "decline"}
	}
	if approval.ID == "" && requested {
		approval.ID = firstNonEmptyValue(approval.ItemID, card.ID, model.NewID("approval"))
	}
	if approval.ItemID == "" {
		approval.ItemID = firstNonEmptyValue(card.ID, approval.ID)
	}
	if card.ID == "" {
		card.ID = firstNonEmptyValue(approval.ItemID, approval.ID)
	}
	if card.Approval == nil && approval.ID != "" {
		card.Approval = &model.ApprovalRef{
			RequestID: approval.ID,
			Type:      approval.Type,
			Decisions: append([]string(nil), approval.Decisions...),
		}
	}
	if card.Detail == nil {
		card.Detail = mergedProjectionDetail(event)
	}
	return approval, card
}

func approvalTitleFromEvent(event ToolLifecycleEvent, approval model.ApprovalRequest) string {
	return firstNonEmptyValue(
		toolProjectionStringFromMaps(approvalProjectionSources(event), "title", "label", "name"),
		approval.Reason,
		approval.Command,
		approval.Type,
		"Approval",
	)
}

func approvalTextFromEvent(event ToolLifecycleEvent, approval model.ApprovalRequest) string {
	return firstNonEmptyValue(
		toolProjectionStringFromMaps(approvalProjectionSources(event), "text", "message", "summary", "reason"),
		approval.Reason,
		approval.Command,
	)
}

func approvalStatusToCardStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "accepted", "accept", "approved", "resolved", "accepted_by_profile_auto", "accepted_by_policy_auto", "accepted_for_session_auto", "accepted_for_session", "accepted_for_host_auto":
		return "completed"
	case "declined", "decline", "rejected":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return firstNonEmptyValue(strings.TrimSpace(status), "completed")
	}
}

func eventTimeString(event ToolLifecycleEvent) string {
	if strings.TrimSpace(event.CreatedAt) != "" {
		return strings.TrimSpace(event.CreatedAt)
	}
	if !event.Timestamp.IsZero() {
		return event.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return model.NowString()
}

func approvalProjectionSources(event ToolLifecycleEvent) []map[string]any {
	return []map[string]any{
		projectionMapFromSource(event.Payload, "approval"),
		projectionMapFromSource(event.Metadata, "approval"),
		event.Payload,
		event.Metadata,
	}
}

func cardProjectionSources(event ToolLifecycleEvent) []map[string]any {
	return []map[string]any{
		projectionMapFromSource(event.Payload, "card"),
		projectionMapFromSource(event.Metadata, "card"),
		event.Payload,
		event.Metadata,
	}
}

func projectionMapFromSource(source map[string]any, key string) map[string]any {
	if source == nil {
		return nil
	}
	if value, ok := source[key]; ok {
		if m, ok := asStringAnyMap(value); ok {
			return m
		}
	}
	return nil
}

func toolProjectionStringFromMaps(sources []map[string]any, keys ...string) string {
	for _, source := range sources {
		if source == nil {
			continue
		}
		for _, key := range keys {
			if value, ok := toolProjectionLookup(source, key); ok {
				if s := strings.TrimSpace(fmt.Sprint(value)); s != "" && s != "<nil>" {
					return s
				}
			}
		}
	}
	return ""
}

func toolProjectionStringsFromMaps(sources []map[string]any, keys ...string) []string {
	for _, source := range sources {
		if source == nil {
			continue
		}
		for _, key := range keys {
			value, ok := toolProjectionLookup(source, key)
			if !ok {
				continue
			}
			switch v := value.(type) {
			case []string:
				return append([]string(nil), v...)
			case []any:
				out := make([]string, 0, len(v))
				for _, item := range v {
					if s := strings.TrimSpace(fmt.Sprint(item)); s != "" && s != "<nil>" {
						out = append(out, s)
					}
				}
				if len(out) > 0 {
					return out
				}
			case string:
				if s := strings.TrimSpace(v); s != "" {
					return []string{s}
				}
			}
		}
	}
	return nil
}

func toolProjectionValueFromMaps(sources []map[string]any, keys ...string) any {
	for _, source := range sources {
		if source == nil {
			continue
		}
		for _, key := range keys {
			if value, ok := toolProjectionLookup(source, key); ok {
				return value
			}
		}
	}
	return nil
}

func toolProjectionLookup(source map[string]any, key string) (any, bool) {
	if source == nil {
		return nil, false
	}
	if value, ok := source[key]; ok {
		return value, true
	}
	normalizedKey := normalizeProjectionKey(key)
	for currentKey, value := range source {
		if normalizeProjectionKey(currentKey) == normalizedKey {
			return value, true
		}
	}
	return nil, false
}

func normalizeProjectionKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func asStringAnyMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		copyMap := make(map[string]any, len(typed))
		for k, v := range typed {
			copyMap[k] = v
		}
		return copyMap, true
	default:
		return nil, false
	}
}

func explicitProjectionDetail(event ToolLifecycleEvent) map[string]any {
	for _, source := range []map[string]any{projectionMapFromSource(event.Payload, "card"), projectionMapFromSource(event.Metadata, "card"), event.Payload, event.Metadata} {
		if source == nil {
			continue
		}
		if value, ok := toolProjectionLookup(source, "detail"); ok {
			if detail, ok := asStringAnyMap(value); ok {
				return detail
			}
		}
	}
	return nil
}

func mergedProjectionDetail(event ToolLifecycleEvent) map[string]any {
	merged := make(map[string]any)
	for _, source := range []map[string]any{event.Metadata, event.Payload} {
		for key, value := range source {
			merged[key] = value
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}
