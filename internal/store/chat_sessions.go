package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

const sessionPersistDebounce = 350 * time.Millisecond

type sessionTranscript struct {
	Version             int                        `json:"version"`
	SessionID           string                     `json:"sessionId"`
	Cards               []model.Card               `json:"cards"`
	IncidentEvents      []model.IncidentEvent      `json:"incidentEvents,omitempty"`
	VerificationRecords []model.VerificationRecord `json:"verificationRecords,omitempty"`
	TurnPolicy          *model.TurnPolicy          `json:"turnPolicy,omitempty"`
	PromptEnvelope      *model.PromptEnvelope      `json:"promptEnvelope,omitempty"`
}

func (s *Store) BrowserSessionExists(browserID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.browserSessions[browserID]
	return ok
}

func (s *Store) SessionExists(sessionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.sessions[sessionID]
	return ok
}

func (s *Store) BrowserSession(browserID string) *BrowserSessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneBrowserSession(s.browserSessions[browserID])
}

func (s *Store) BrowserOwnsSession(browserID, sessionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.browserOwnsSessionLocked(s.browserSessions[browserID], sessionID)
}

func (s *Store) EnsureBrowserSession(browserID string) *BrowserSessionState {
	s.mu.Lock()
	browser, created := s.ensureBrowserSessionLocked(browserID)
	cloned := cloneBrowserSession(browser)
	s.mu.Unlock()
	if created {
		s.SaveStableState("")
	}
	return cloned
}

func (s *Store) AttachLegacySessionToBrowser(browserID, sessionID string) *BrowserSessionState {
	s.mu.Lock()
	browser, created := s.ensureBrowserSessionLocked(browserID)
	if _, ok := s.sessions[sessionID]; ok && !slices.Contains(browser.SessionIDs, sessionID) {
		browser.SessionIDs = append(browser.SessionIDs, sessionID)
	}
	if browser.ActiveSessionID == "" {
		browser.ActiveSessionID = sessionID
	}
	browser.UpdatedAt = model.NowString()
	cloned := cloneBrowserSession(browser)
	s.mu.Unlock()
	if created || cloned.ActiveSessionID == sessionID {
		s.SaveStableState("")
	}
	return cloned
}

func (s *Store) EnsureActiveSession(browserID string) string {
	s.mu.Lock()
	browser, browserCreated := s.ensureBrowserSessionLocked(browserID)
	if browser.ActiveSessionID != "" {
		if session := s.sessions[browser.ActiveSessionID]; session != nil {
			sessionID := browser.ActiveSessionID
			s.mu.Unlock()
			if browserCreated {
				s.SaveStableState("")
			}
			return sessionID
		}
	}
	session := s.createSessionLocked(browser)
	sessionID := session.ID
	s.mu.Unlock()
	s.SaveStableState("")
	return sessionID
}

func (s *Store) CreateSession(browserID string) *SessionState {
	return s.CreateSessionWithMeta(browserID, model.DefaultSessionMeta(), true)
}

func (s *Store) CreateSessionWithMeta(browserID string, meta model.SessionMeta, attachToBrowser bool) *SessionState {
	s.mu.Lock()
	var browser *BrowserSessionState
	if browserID != "" {
		browser, _ = s.ensureBrowserSessionLocked(browserID)
	}
	session := s.createSessionLockedWithMeta(browser, meta, attachToBrowser)
	cloned := cloneSession(session)
	s.mu.Unlock()
	s.SaveStableState("")
	_ = s.SaveSessionTranscript(cloned.ID)
	return cloned
}

func (s *Store) ActivateSession(browserID, sessionID string) error {
	s.mu.Lock()
	browser, _ := s.ensureBrowserSessionLocked(browserID)
	if !s.browserOwnsSessionLocked(browser, sessionID) {
		s.mu.Unlock()
		return errors.New("session not found")
	}
	browser.ActiveSessionID = sessionID
	browser.UpdatedAt = model.NowString()
	s.mu.Unlock()
	s.SaveStableState("")
	return nil
}

func (s *Store) SessionSummaries(browserID string) []model.SessionSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	browser := s.browserSessions[browserID]
	if browser == nil {
		return []model.SessionSummary{}
	}
	out := make([]model.SessionSummary, 0, len(browser.SessionIDs))
	for _, sessionID := range browser.SessionIDs {
		session := s.sessions[sessionID]
		if session == nil || !session.Meta.Visible {
			continue
		}
		out = append(out, summarizeSession(session))
	}
	slices.SortFunc(out, func(a, b model.SessionSummary) int {
		switch {
		case a.LastActivityAt > b.LastActivityAt:
			return -1
		case a.LastActivityAt < b.LastActivityAt:
			return 1
		case a.CreatedAt > b.CreatedAt:
			return -1
		case a.CreatedAt < b.CreatedAt:
			return 1
		default:
			return strings.Compare(a.ID, b.ID)
		}
	})
	return out
}

func (s *Store) SaveSessionTranscript(sessionID string) error {
	s.mu.RLock()
	statePath := s.statePath
	session := s.sessions[sessionID]
	var cards []model.Card
	var incidentEvents []model.IncidentEvent
	var verificationRecords []model.VerificationRecord
	var turnPolicy *model.TurnPolicy
	var promptEnvelope *model.PromptEnvelope
	if session != nil {
		cards = append([]model.Card(nil), session.Cards...)
		incidentEvents = cloneIncidentEvents(session.IncidentEvents)
		verificationRecords = cloneVerificationRecords(session.VerificationRecords)
		turnPolicy = cloneTurnPolicyPtr(session.Runtime.TurnPolicy)
		promptEnvelope = clonePromptEnvelope(session.Runtime.PromptEnvelope)
	}
	s.mu.RUnlock()
	if session == nil || statePath == "" {
		return nil
	}

	payload := sessionTranscript{
		Version:             2,
		SessionID:           sessionID,
		Cards:               cards,
		IncidentEvents:      incidentEvents,
		VerificationRecords: verificationRecords,
		TurnPolicy:          turnPolicy,
		PromptEnvelope:      promptEnvelope,
	}
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	path := transcriptPath(statePath, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Store) scheduleSessionPersistence(sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	if timer := s.persistTimers[sessionID]; timer != nil {
		timer.Stop()
	}
	s.persistTimers[sessionID] = time.AfterFunc(sessionPersistDebounce, func() {
		s.flushSessionPersistence(sessionID)
	})
	s.mu.Unlock()
}

func (s *Store) flushSessionPersistence(sessionID string) {
	s.mu.Lock()
	if timer := s.persistTimers[sessionID]; timer != nil {
		timer.Stop()
		delete(s.persistTimers, sessionID)
	}
	s.mu.Unlock()
	_ = s.SaveStableState("")
	_ = s.SaveSessionTranscript(sessionID)
}

func (s *Store) ensureBrowserSessionLocked(browserID string) (*BrowserSessionState, bool) {
	browser, ok := s.browserSessions[browserID]
	if ok {
		return browser, false
	}
	now := model.NowString()
	browser = &BrowserSessionState{
		ID:         browserID,
		SessionIDs: make([]string, 0),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	s.browserSessions[browserID] = browser
	return browser, true
}

func (s *Store) createSessionLocked(browser *BrowserSessionState) *SessionState {
	return s.createSessionLockedWithMeta(browser, model.DefaultSessionMeta(), true)
}

func (s *Store) createSessionLockedWithMeta(browser *BrowserSessionState, meta model.SessionMeta, attachToBrowser bool) *SessionState {
	now := model.NowString()
	sessionID := model.NewID("sess")
	session := defaultSession(sessionID)
	session.Meta = normalizeSessionMetaForCreate(meta)
	session.CreatedAt = now
	session.LastActivityAt = now

	if browser != nil {
		if active := s.sessions[browser.ActiveSessionID]; active != nil {
			session.SelectedHostID = defaultHostID(active.SelectedHostID)
			session.Runtime = defaultRuntime(session.SelectedHostID)
			session.AuthSessionID = active.AuthSessionID
			session.Auth = active.Auth
			session.Tokens = active.Tokens
			s.linkAuthSessionLocked(sessionID, session.AuthSessionID)
		} else if s.lastAuthSession != "" {
			if authSession := s.authSessions[s.lastAuthSession]; authSession != nil {
				session.AuthSessionID = authSession.ID
				session.Auth = authSession.Auth
				session.Tokens = authSession.Tokens
				s.linkAuthSessionLocked(sessionID, session.AuthSessionID)
			}
		}
		if attachToBrowser {
			browser.SessionIDs = append(browser.SessionIDs, sessionID)
			browser.ActiveSessionID = sessionID
			browser.UpdatedAt = now
		}
	}
	s.sessions[sessionID] = session
	return session
}

func (s *Store) browserOwnsSessionLocked(browser *BrowserSessionState, sessionID string) bool {
	if browser == nil {
		return false
	}
	for _, existing := range browser.SessionIDs {
		if existing == sessionID {
			return true
		}
	}
	return false
}

func (s *Store) linkAuthSessionLocked(sessionID, authSessionID string) {
	if authSessionID == "" {
		return
	}
	authSession := s.authSessions[authSessionID]
	if authSession == nil {
		return
	}
	if authSession.WebSessionIDs == nil {
		authSession.WebSessionIDs = make(map[string]struct{})
	}
	authSession.WebSessionIDs[sessionID] = struct{}{}
}

func (s *Store) loadSessionTranscriptLocked(statePath, sessionID string) error {
	session := s.sessions[sessionID]
	if session == nil || statePath == "" {
		return nil
	}
	content, err := os.ReadFile(transcriptPath(statePath, sessionID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var transcript sessionTranscript
	if err := json.Unmarshal(content, &transcript); err != nil {
		return err
	}
	session.Cards = append([]model.Card(nil), transcript.Cards...)
	session.IncidentEvents = cloneIncidentEvents(transcript.IncidentEvents)
	session.VerificationRecords = cloneVerificationRecords(transcript.VerificationRecords)
	if transcript.TurnPolicy != nil {
		session.Runtime.TurnPolicy = cloneTurnPolicy(*transcript.TurnPolicy)
	}
	session.Runtime.PromptEnvelope = clonePromptEnvelope(transcript.PromptEnvelope)
	return nil
}

func summarizeSession(session *SessionState) model.SessionSummary {
	title := "新建会话"
	preview := "暂无消息"
	messageCount := 0
	for _, card := range session.Cards {
		if isConversationCard(card) {
			messageCount++
		}
		if title == "新建会话" && isUserCard(card) {
			if text := summarizeCardText(card); text != "" {
				title = truncateRunes(text, 24)
			}
		}
	}
	for i := len(session.Cards) - 1; i >= 0; i-- {
		if text := summarizeCardText(session.Cards[i]); text != "" {
			preview = truncateRunes(text, 60)
			break
		}
	}
	return model.SessionSummary{
		ID:             session.ID,
		Kind:           session.Meta.Kind,
		Title:          title,
		Preview:        preview,
		SelectedHostID: session.SelectedHostID,
		Status:         summarizeSessionStatus(session),
		MessageCount:   messageCount,
		CreatedAt:      session.CreatedAt,
		LastActivityAt: session.LastActivityAt,
	}
}

func summarizeSessionStatus(session *SessionState) string {
	if session == nil {
		return "empty"
	}
	if session.Runtime.Turn.Active {
		if session.Runtime.Turn.Phase == "waiting_approval" {
			return "waiting_approval"
		}
		return "running"
	}
	if len(session.Cards) == 0 {
		return "empty"
	}
	for i := len(session.Cards) - 1; i >= 0; i-- {
		card := session.Cards[i]
		if card.Type == "ErrorCard" || card.Status == "failed" {
			return "failed"
		}
		if isConversationCard(card) || card.Type == "NoticeCard" || card.Type == "ResultSummaryCard" {
			break
		}
	}
	return "completed"
}

func summarizeCardText(card model.Card) string {
	for _, candidate := range []string{card.Text, card.Message, card.Summary, card.Title} {
		text := strings.TrimSpace(candidate)
		if text == "" {
			continue
		}
		text = strings.ReplaceAll(text, "\n", " ")
		text = strings.Join(strings.Fields(text), " ")
		if text != "" {
			return text
		}
	}
	return ""
}

func isUserCard(card model.Card) bool {
	return card.Type == "UserMessageCard" || (card.Type == "MessageCard" && card.Role == "user")
}

func isAssistantCard(card model.Card) bool {
	return card.Type == "AssistantMessageCard" || (card.Type == "MessageCard" && card.Role == "assistant")
}

func isConversationCard(card model.Card) bool {
	return isUserCard(card) || isAssistantCard(card)
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "..."
}

func transcriptPath(statePath, sessionID string) string {
	return filepath.Join(filepath.Dir(statePath), "sessions", sessionID+".json")
}

func cloneBrowserSession(session *BrowserSessionState) *BrowserSessionState {
	if session == nil {
		return nil
	}
	out := *session
	out.SessionIDs = append([]string(nil), session.SessionIDs...)
	return &out
}
