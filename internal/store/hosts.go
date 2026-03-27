package store

import (
	"sort"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

const hostSessionMessageLimit = 4

func (s *Store) Host(hostID string) (model.Host, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	host, ok := s.hosts[hostID]
	if !ok {
		return model.Host{}, false
	}
	return normalizeStoredHost(host), true
}

func (s *Store) DeleteHost(hostID string) bool {
	hostID = strings.TrimSpace(hostID)
	if hostID == "" || hostID == model.ServerLocalHostID {
		return false
	}

	s.mu.Lock()
	if _, ok := s.hosts[hostID]; !ok {
		s.mu.Unlock()
		return false
	}
	delete(s.hosts, hostID)
	s.mu.Unlock()
	s.SaveStableState("")
	return true
}

func (s *Store) BatchUpdateHostLabels(hostIDs []string, add map[string]string, remove []string) []model.Host {
	if len(hostIDs) == 0 {
		return nil
	}

	removeKeys := make([]string, 0, len(remove))
	for _, item := range remove {
		key := strings.TrimSpace(item)
		if key == "" {
			continue
		}
		if before, _, ok := strings.Cut(key, "="); ok {
			key = strings.TrimSpace(before)
		}
		if key != "" {
			removeKeys = append(removeKeys, key)
		}
	}

	s.mu.Lock()
	updated := make([]model.Host, 0, len(hostIDs))
	for _, rawID := range hostIDs {
		hostID := strings.TrimSpace(rawID)
		host, ok := s.hosts[hostID]
		if !ok {
			continue
		}
		labels := cloneHostLabels(host.Labels)
		if labels == nil {
			labels = make(map[string]string)
		}
		for key, value := range add {
			k := strings.TrimSpace(key)
			if k == "" {
				continue
			}
			labels[k] = strings.TrimSpace(value)
		}
		for _, key := range removeKeys {
			delete(labels, key)
		}
		host.Labels = labels
		s.hosts[hostID] = normalizeStoredHost(host)
		updated = append(updated, s.hosts[hostID])
	}
	s.mu.Unlock()
	if len(updated) > 0 {
		s.SaveStableState("")
	}
	return updated
}

func (s *Store) HostSessions(hostID string, limit int) []model.HostSessionSummary {
	hostID = defaultHostID(strings.TrimSpace(hostID))
	if hostID == "" {
		return nil
	}

	s.mu.RLock()
	out := make([]model.HostSessionSummary, 0)
	for _, session := range s.sessions {
		if session == nil || defaultHostID(session.SelectedHostID) != hostID {
			continue
		}
		out = append(out, summarizeHostSession(session))
	}
	s.mu.RUnlock()

	sort.SliceStable(out, func(i, j int) bool {
		switch {
		case out[i].LastActivityAt > out[j].LastActivityAt:
			return true
		case out[i].LastActivityAt < out[j].LastActivityAt:
			return false
		default:
			return out[i].SessionID > out[j].SessionID
		}
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func summarizeHostSession(session *SessionState) model.HostSessionSummary {
	base := summarizeSession(session)
	taskSummary := ""
	replySummary := ""
	messages := make([]model.SessionMessageExcerpt, 0, hostSessionMessageLimit)

	for _, card := range session.Cards {
		if !isUserCard(card) {
			continue
		}
		if text := summarizeCardText(card); text != "" {
			taskSummary = truncateRunes(text, 120)
			break
		}
	}

	for i := len(session.Cards) - 1; i >= 0; i-- {
		card := session.Cards[i]
		if text := hostReplySummary(card); text != "" {
			replySummary = truncateRunes(text, 140)
			break
		}
	}

	for i := len(session.Cards) - 1; i >= 0 && len(messages) < hostSessionMessageLimit; i-- {
		card := session.Cards[i]
		role, text, ok := hostSessionMessage(card)
		if !ok {
			continue
		}
		messages = append(messages, model.SessionMessageExcerpt{
			Role:      role,
			Text:      truncateRunes(text, 180),
			CreatedAt: card.CreatedAt,
		})
	}
	reverseMessageExcerpts(messages)

	return model.HostSessionSummary{
		SessionID:      base.ID,
		Title:          base.Title,
		Status:         base.Status,
		LastActivityAt: base.LastActivityAt,
		MessageCount:   base.MessageCount,
		TaskSummary:    taskSummary,
		ReplySummary:   replySummary,
		Messages:       messages,
	}
}

func hostReplySummary(card model.Card) string {
	switch {
	case isAssistantCard(card):
		return summarizeCardText(card)
	case card.Type == "ResultSummaryCard":
		return summarizeCardText(card)
	case card.Type == "NoticeCard":
		return summarizeCardText(card)
	case card.Type == "ErrorCard":
		return summarizeCardText(card)
	default:
		return ""
	}
}

func hostSessionMessage(card model.Card) (string, string, bool) {
	switch {
	case isUserCard(card):
		if text := summarizeCardText(card); text != "" {
			return "user", text, true
		}
	case isAssistantCard(card):
		if text := summarizeCardText(card); text != "" {
			return "assistant", text, true
		}
	case card.Type == "ResultSummaryCard" || card.Type == "NoticeCard" || card.Type == "ErrorCard":
		if text := summarizeCardText(card); text != "" {
			return "system", text, true
		}
	}
	return "", "", false
}

func reverseMessageExcerpts(items []model.SessionMessageExcerpt) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}

func normalizeStoredHost(host model.Host) model.Host {
	if host.ID == model.ServerLocalHostID {
		local := serverLocalHost()
		if host.OS != "" {
			local.OS = host.OS
		}
		if host.Arch != "" {
			local.Arch = host.Arch
		}
		if len(host.Labels) > 0 {
			local.Labels = cloneHostLabels(host.Labels)
		}
		return local
	}

	host.ID = strings.TrimSpace(host.ID)
	host.Name = strings.TrimSpace(host.Name)
	if host.Name == "" {
		host.Name = host.ID
	}
	host.Kind = strings.TrimSpace(host.Kind)
	if host.Kind == "" {
		host.Kind = "inventory"
	}
	host.Address = strings.TrimSpace(host.Address)
	host.Transport = strings.TrimSpace(host.Transport)
	host.Status = strings.TrimSpace(host.Status)
	host.AgentVersion = strings.TrimSpace(host.AgentVersion)
	host.LastHeartbeat = strings.TrimSpace(host.LastHeartbeat)
	host.LastError = strings.TrimSpace(host.LastError)
	host.SSHUser = strings.TrimSpace(host.SSHUser)
	host.InstallState = strings.TrimSpace(host.InstallState)
	host.ControlMode = strings.TrimSpace(host.ControlMode)
	host.Labels = cloneHostLabels(host.Labels)

	switch {
	case host.Kind == "agent" || host.Executable || host.TerminalCapable:
		host.Transport = "grpc_reverse"
	case host.Transport == "" && host.Address != "":
		host.Transport = "ssh_bootstrap"
	case host.Transport == "":
		host.Transport = "inventory"
	}
	if host.Status == "" {
		if host.Executable || host.TerminalCapable || host.LastHeartbeat != "" {
			host.Status = "offline"
		} else {
			host.Status = "pending_install"
		}
	}
	if host.InstallState == "" {
		switch {
		case host.Executable || host.TerminalCapable || host.Kind == "agent":
			host.InstallState = "installed"
		case host.Address != "":
			host.InstallState = "pending_install"
		default:
			host.InstallState = "inventory"
		}
	}
	if host.ControlMode == "" {
		switch host.Transport {
		case "grpc_reverse":
			host.ControlMode = "persistent_stream"
		case "ssh_bootstrap":
			host.ControlMode = "bootstrap_over_ssh"
		default:
			host.ControlMode = "inventory"
		}
	}
	if host.SSHPort <= 0 {
		host.SSHPort = 22
	}

	return host
}

func mergeStoredHost(existing, next model.Host) model.Host {
	merged := next
	if merged.Name == "" {
		merged.Name = existing.Name
	}
	if merged.Kind == "" {
		merged.Kind = existing.Kind
	}
	if merged.Address == "" {
		merged.Address = existing.Address
	}
	if merged.Transport == "" {
		merged.Transport = existing.Transport
	}
	if merged.Status == "" {
		merged.Status = existing.Status
	}
	if merged.OS == "" {
		merged.OS = existing.OS
	}
	if merged.Arch == "" {
		merged.Arch = existing.Arch
	}
	if merged.AgentVersion == "" {
		merged.AgentVersion = existing.AgentVersion
	}
	if len(merged.Labels) == 0 {
		merged.Labels = cloneHostLabels(existing.Labels)
	}
	if merged.LastHeartbeat == "" {
		merged.LastHeartbeat = existing.LastHeartbeat
	}
	if merged.LastError == "" {
		merged.LastError = existing.LastError
	}
	if merged.SSHUser == "" {
		merged.SSHUser = existing.SSHUser
	}
	if merged.SSHPort == 0 {
		merged.SSHPort = existing.SSHPort
	}
	if merged.InstallState == "" {
		merged.InstallState = existing.InstallState
	}
	if merged.ControlMode == "" {
		merged.ControlMode = existing.ControlMode
	}
	return merged
}

func cloneHostLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		out[trimmedKey] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
