package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type SessionState struct {
	ID             string
	AuthSessionID  string
	ThreadID       string
	TurnID         string
	SelectedHostID string
	Cards          []model.Card
	Approvals      map[string]model.ApprovalRequest
	Choices        map[string]model.ChoiceRequest
	ApprovalGrants []model.ApprovalGrant
	ItemCache      map[string]map[string]any
	Runtime        model.RuntimeState
	Auth           model.AuthState
	Tokens         model.ExternalAuthTokens
	CreatedAt      string
	LastActivityAt string
}

type BrowserSessionState struct {
	ID              string
	ActiveSessionID string
	SessionIDs      []string
	CreatedAt       string
	UpdatedAt       string
}

type AuthSessionState struct {
	ID            string
	Auth          model.AuthState
	Tokens        model.ExternalAuthTokens
	WebSessionIDs map[string]struct{}
	CreatedAt     string
	UpdatedAt     string
}

type Store struct {
	mu              sync.RWMutex
	browserSessions map[string]*BrowserSessionState
	sessions        map[string]*SessionState
	authSessions    map[string]*AuthSessionState
	threadToSession map[string]string
	turnToSession   map[string]string
	loginToSession  map[string]string
	lastAuthSession string
	hosts           map[string]model.Host
	statePath       string
	persistTimers   map[string]*time.Timer
}

type persistentSessionState struct {
	ID             string                       `json:"id"`
	AuthSessionID  string                       `json:"authSessionId,omitempty"`
	ThreadID       string                       `json:"threadId,omitempty"`
	SelectedHostID string                       `json:"selectedHostId,omitempty"`
	Auth           model.AuthState              `json:"auth"`
	Tokens         persistentExternalAuthTokens `json:"tokens"`
	ApprovalGrants []model.ApprovalGrant        `json:"approvalGrants,omitempty"`
	CreatedAt      string                       `json:"createdAt,omitempty"`
	LastActivityAt string                       `json:"lastActivityAt,omitempty"`
}

type persistentAuthSessionState struct {
	ID            string                       `json:"id"`
	Auth          model.AuthState              `json:"auth"`
	Tokens        persistentExternalAuthTokens `json:"tokens"`
	WebSessionIDs map[string]struct{}          `json:"webSessionIds,omitempty"`
	CreatedAt     string                       `json:"createdAt,omitempty"`
	UpdatedAt     string                       `json:"updatedAt,omitempty"`
}

type persistentBrowserSessionState struct {
	ID              string   `json:"id"`
	ActiveSessionID string   `json:"activeSessionId,omitempty"`
	SessionIDs      []string `json:"sessionIds,omitempty"`
	CreatedAt       string   `json:"createdAt,omitempty"`
	UpdatedAt       string   `json:"updatedAt,omitempty"`
}

type persistentExternalAuthTokens struct {
	IDToken          string `json:"idToken,omitempty"`
	AccessToken      string `json:"accessToken,omitempty"`
	ChatGPTAccountID string `json:"chatgptAccountId,omitempty"`
	ChatGPTPlanType  string `json:"chatgptPlanType,omitempty"`
	Email            string `json:"email,omitempty"`
}

type persistentState struct {
	BrowserSessions map[string]*persistentBrowserSessionState `json:"browserSessions"`
	Sessions        map[string]*persistentSessionState        `json:"sessions"`
	AuthSessions    map[string]*persistentAuthSessionState    `json:"authSessions"`
	ThreadToSession map[string]string                         `json:"threadToSession"`
	LoginToSession  map[string]string                         `json:"loginToSession"`
	LastAuthSession string                                    `json:"lastAuthSession,omitempty"`
	Hosts           map[string]model.Host                     `json:"hosts"`
}

func New() *Store {
	return &Store{
		browserSessions: make(map[string]*BrowserSessionState),
		sessions:        make(map[string]*SessionState),
		authSessions:    make(map[string]*AuthSessionState),
		threadToSession: make(map[string]string),
		turnToSession:   make(map[string]string),
		loginToSession:  make(map[string]string),
		persistTimers:   make(map[string]*time.Timer),
		hosts: map[string]model.Host{
			model.ServerLocalHostID: {
				ID:         model.ServerLocalHostID,
				Name:       "server-local",
				Kind:       "server_local",
				Status:     "online",
				Executable: true,
			},
		},
	}
}

func (s *Store) SetStatePath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statePath = path
}

func (s *Store) EnsureSession(sessionID string) *SessionState {
	s.mu.Lock()
	session, created := s.ensureSessionLocked(sessionID)
	cloned := cloneSession(session)
	s.mu.Unlock()
	if created {
		s.SaveStableState("")
	}
	return cloned
}

func (s *Store) Session(sessionID string) *SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSession(s.sessions[sessionID])
}

func (s *Store) SessionIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func (s *Store) TouchSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, _ := s.ensureSessionLocked(sessionID)
	session.LastActivityAt = model.NowString()
}

func (s *Store) SetSelectedHost(sessionID, hostID string) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	session.SelectedHostID = hostID
	session.Runtime.Turn.HostID = defaultHostID(hostID)
	session.LastActivityAt = model.NowString()
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) SetThread(sessionID, threadID string) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	if session.ThreadID != "" && session.ThreadID != threadID {
		delete(s.threadToSession, session.ThreadID)
	}
	session.ThreadID = threadID
	session.LastActivityAt = model.NowString()
	s.threadToSession[threadID] = sessionID
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) ClearThread(sessionID string) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	if session.ThreadID != "" {
		delete(s.threadToSession, session.ThreadID)
	}
	session.ThreadID = ""
	session.LastActivityAt = model.NowString()
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) SetTurn(sessionID, turnID string) {
	if turnID == "" {
		return
	}
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	if session.TurnID != "" && session.TurnID != turnID {
		delete(s.turnToSession, session.TurnID)
	}
	session.TurnID = turnID
	session.LastActivityAt = model.NowString()
	s.turnToSession[turnID] = sessionID
	s.mu.Unlock()
}

func (s *Store) ClearTurn(sessionID string) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	if session.TurnID != "" {
		delete(s.turnToSession, session.TurnID)
	}
	session.TurnID = ""
	session.LastActivityAt = model.NowString()
	s.mu.Unlock()
}

func (s *Store) ResetConversation(sessionID string) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	if session.ThreadID != "" {
		delete(s.threadToSession, session.ThreadID)
	}
	if session.TurnID != "" {
		delete(s.turnToSession, session.TurnID)
	}
	session.ThreadID = ""
	session.TurnID = ""
	session.Cards = nil
	session.Approvals = make(map[string]model.ApprovalRequest)
	session.Choices = make(map[string]model.ChoiceRequest)
	session.ApprovalGrants = make([]model.ApprovalGrant, 0)
	session.ItemCache = make(map[string]map[string]any)
	session.Runtime = defaultRuntime(session.SelectedHostID)
	session.LastActivityAt = model.NowString()
	s.mu.Unlock()
	s.SaveStableState("")
	_ = s.SaveSessionTranscript(sessionID)
}

func (s *Store) SessionIDByThread(threadID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.threadToSession[threadID]
}

func (s *Store) SessionIDByTurn(turnID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.turnToSession[turnID]
}

func (s *Store) SetPendingLogin(sessionID, loginID string) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	authSession := s.ensureAuthSessionLocked(sessionID, session)
	session.Auth.Pending = true
	session.Auth.PendingLoginID = loginID
	authSession.Auth = session.Auth
	now := model.NowString()
	authSession.UpdatedAt = now
	session.LastActivityAt = now
	s.loginToSession[loginID] = sessionID
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) SessionIDByLogin(loginID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loginToSession[loginID]
}

func (s *Store) SetAuth(sessionID string, auth model.AuthState, tokens model.ExternalAuthTokens) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	authSession := s.ensureAuthSessionLocked(sessionID, session)
	session.Auth = auth
	authSession.Auth = auth
	if tokens.AccessToken != "" || tokens.ChatGPTAccountID != "" || tokens.Email != "" {
		session.Tokens = tokens
		authSession.Tokens = tokens
		s.lastAuthSession = authSession.ID
	}
	now := model.NowString()
	authSession.UpdatedAt = now
	session.LastActivityAt = now
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) UpdateAuth(sessionID string, fn func(*model.AuthState, *model.ExternalAuthTokens)) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	authSession := s.ensureAuthSessionLocked(sessionID, session)
	fn(&session.Auth, &session.Tokens)
	authSession.Auth = session.Auth
	authSession.Tokens = session.Tokens
	now := model.NowString()
	authSession.UpdatedAt = now
	session.LastActivityAt = now
	if session.Tokens.AccessToken != "" || session.Tokens.ChatGPTAccountID != "" {
		s.lastAuthSession = authSession.ID
	}
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) Auth(sessionID string) model.AuthState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[sessionID]
	if session == nil {
		return model.AuthState{}
	}
	return session.Auth
}

func (s *Store) Tokens(sessionID string) model.ExternalAuthTokens {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[sessionID]
	if session == nil {
		return model.ExternalAuthTokens{}
	}
	return session.Tokens
}

func (s *Store) TokensForRefresh() model.ExternalAuthTokens {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastAuthSession != "" {
		if authSession := s.authSessions[s.lastAuthSession]; authSession != nil {
			return authSession.Tokens
		}
	}
	for _, session := range s.sessions {
		if session.Tokens.AccessToken != "" && session.Tokens.ChatGPTAccountID != "" {
			return session.Tokens
		}
	}
	return model.ExternalAuthTokens{}
}

func (s *Store) ClearAuth(sessionID string) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	authSession := s.ensureAuthSessionLocked(sessionID, session)
	session.Auth = model.AuthState{}
	session.Tokens = model.ExternalAuthTokens{}
	authSession.Auth = model.AuthState{}
	authSession.Tokens = model.ExternalAuthTokens{}
	now := model.NowString()
	authSession.UpdatedAt = now
	session.LastActivityAt = now
	if s.lastAuthSession == authSession.ID {
		s.lastAuthSession = ""
	}
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) SyncAuthFromCodex(sessionID string, auth model.AuthState) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	authSession := s.ensureAuthSessionLocked(sessionID, session)
	if auth.Mode == "" && session.Auth.Mode != "" {
		auth.Mode = session.Auth.Mode
	}
	if auth.Email == "" && session.Auth.Email != "" {
		auth.Email = session.Auth.Email
	}
	session.Auth = auth
	authSession.Auth = auth
	now := model.NowString()
	authSession.UpdatedAt = now
	session.LastActivityAt = now
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) SessionsNeedingAuthSync() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0)
	for id, session := range s.sessions {
		if session.Auth.Pending || session.Auth.Connected {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func (s *Store) PendingSessionIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0)
	for id, session := range s.sessions {
		if session.Auth.Pending {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func (s *Store) UpsertCard(sessionID string, card model.Card) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	for i := range session.Cards {
		if session.Cards[i].ID == card.ID {
			session.Cards[i] = card
			session.LastActivityAt = model.NowString()
			s.mu.Unlock()
			s.scheduleSessionPersistence(sessionID)
			return
		}
	}
	session.Cards = append(session.Cards, card)
	session.LastActivityAt = model.NowString()
	s.mu.Unlock()
	s.scheduleSessionPersistence(sessionID)
}

func (s *Store) UpdateCard(sessionID, cardID string, fn func(*model.Card)) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	for i := range session.Cards {
		if session.Cards[i].ID == cardID {
			fn(&session.Cards[i])
			session.LastActivityAt = model.NowString()
			s.mu.Unlock()
			s.scheduleSessionPersistence(sessionID)
			return
		}
	}
	s.mu.Unlock()
}

func (s *Store) RememberItem(sessionID, itemID string, item map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, _ := s.ensureSessionLocked(sessionID)
	session.ItemCache[itemID] = item
	session.LastActivityAt = model.NowString()
}

func (s *Store) Item(sessionID, itemID string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[sessionID]
	if session == nil {
		return nil
	}
	item := session.ItemCache[itemID]
	if item == nil {
		return nil
	}
	copyMap := make(map[string]any, len(item))
	for k, v := range item {
		copyMap[k] = v
	}
	return copyMap
}

func (s *Store) UpdateRuntime(sessionID string, fn func(*model.RuntimeState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, _ := s.ensureSessionLocked(sessionID)
	fn(&session.Runtime)
	session.LastActivityAt = model.NowString()
}

func (s *Store) AddApproval(sessionID string, approval model.ApprovalRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, _ := s.ensureSessionLocked(sessionID)
	session.Approvals[approval.ID] = approval
	session.LastActivityAt = model.NowString()
}

func (s *Store) AddChoice(sessionID string, choice model.ChoiceRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, _ := s.ensureSessionLocked(sessionID)
	session.Choices[choice.ID] = choice
	session.LastActivityAt = model.NowString()
}

func (s *Store) Choice(sessionID, choiceID string) (model.ChoiceRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[sessionID]
	if session == nil {
		return model.ChoiceRequest{}, false
	}
	choice, ok := session.Choices[choiceID]
	return choice, ok
}

func (s *Store) ResolveChoice(sessionID, choiceID, status, resolvedAt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, _ := s.ensureSessionLocked(sessionID)
	choice, ok := session.Choices[choiceID]
	if !ok {
		return
	}
	choice.Status = status
	choice.ResolvedAt = resolvedAt
	session.Choices[choiceID] = choice
	session.LastActivityAt = model.NowString()
}

func (s *Store) AddApprovalGrant(sessionID string, grant model.ApprovalGrant) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	for i := range session.ApprovalGrants {
		if session.ApprovalGrants[i].Fingerprint == grant.Fingerprint {
			session.ApprovalGrants[i] = grant
			session.LastActivityAt = model.NowString()
			s.mu.Unlock()
			s.SaveStableState("")
			return
		}
	}
	session.ApprovalGrants = append(session.ApprovalGrants, grant)
	session.LastActivityAt = model.NowString()
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) ApprovalGrant(sessionID, fingerprint string) (model.ApprovalGrant, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[sessionID]
	if session == nil {
		return model.ApprovalGrant{}, false
	}
	for _, grant := range session.ApprovalGrants {
		if grant.Fingerprint == fingerprint {
			return grant, true
		}
	}
	return model.ApprovalGrant{}, false
}

func (s *Store) Approval(sessionID, approvalID string) (model.ApprovalRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[sessionID]
	if session == nil {
		return model.ApprovalRequest{}, false
	}
	approval, ok := session.Approvals[approvalID]
	return approval, ok
}

func (s *Store) ResolveApproval(sessionID, approvalID, status, resolvedAt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, _ := s.ensureSessionLocked(sessionID)
	approval, ok := session.Approvals[approvalID]
	if !ok {
		return
	}
	approval.Status = status
	approval.ResolvedAt = resolvedAt
	session.Approvals[approvalID] = approval
	session.LastActivityAt = model.NowString()
}

func (s *Store) UpsertHost(host model.Host) {
	s.mu.Lock()
	s.hosts[host.ID] = host
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) MarkHostOffline(hostID string) {
	s.mu.Lock()
	host, ok := s.hosts[hostID]
	if !ok {
		s.mu.Unlock()
		return
	}
	host.Status = "offline"
	s.hosts[hostID] = host
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) MarkStaleHosts(timeout time.Duration) []string {
	s.mu.Lock()
	now := time.Now()
	changed := make([]string, 0)
	for id, host := range s.hosts {
		if id == model.ServerLocalHostID || host.Kind != "agent" || host.LastHeartbeat == "" {
			continue
		}
		lastHeartbeat, err := time.Parse(time.RFC3339, host.LastHeartbeat)
		if err != nil {
			continue
		}
		if now.Sub(lastHeartbeat) > timeout && host.Status != "offline" {
			host.Status = "offline"
			s.hosts[id] = host
			changed = append(changed, id)
		}
	}
	s.mu.Unlock()
	if len(changed) > 0 {
		s.SaveStableState("")
	}
	return changed
}

func (s *Store) Hosts() []model.Host {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Host, 0, len(s.hosts))
	for _, host := range s.hosts {
		out = append(out, host)
	}
	model.SortHosts(out)
	return out
}

func (s *Store) Snapshot(sessionID string, cfg model.UIConfig) model.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session := s.sessions[sessionID]
	if session == nil {
		session = defaultSession(sessionID)
	}

	approvals := make([]model.ApprovalRequest, 0, len(session.Approvals))
	for _, approval := range session.Approvals {
		approvals = append(approvals, approval)
	}
	slices.SortFunc(approvals, func(a, b model.ApprovalRequest) int {
		switch {
		case a.RequestedAt < b.RequestedAt:
			return -1
		case a.RequestedAt > b.RequestedAt:
			return 1
		default:
			return 0
		}
	})

	hosts := make([]model.Host, 0, len(s.hosts))
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	model.SortHosts(hosts)

	cards := append([]model.Card(nil), session.Cards...)
	return model.Snapshot{
		SessionID:      session.ID,
		SelectedHostID: session.SelectedHostID,
		Auth:           session.Auth,
		Hosts:          hosts,
		Cards:          cards,
		Approvals:      approvals,
		Runtime:        cloneRuntime(session.Runtime),
		LastActivityAt: session.LastActivityAt,
		Config:         cfg,
	}
}

func (s *Store) ensureSessionLocked(sessionID string) (*SessionState, bool) {
	session, ok := s.sessions[sessionID]
	if ok {
		return session, false
	}
	session = defaultSession(sessionID)
	s.sessions[sessionID] = session
	return session, true
}

func defaultSession(sessionID string) *SessionState {
	now := model.NowString()
	return &SessionState{
		ID:             sessionID,
		SelectedHostID: model.ServerLocalHostID,
		Approvals:      make(map[string]model.ApprovalRequest),
		Choices:        make(map[string]model.ChoiceRequest),
		ApprovalGrants: make([]model.ApprovalGrant, 0),
		ItemCache:      make(map[string]map[string]any),
		Runtime:        defaultRuntime(model.ServerLocalHostID),
		CreatedAt:      now,
		LastActivityAt: now,
	}
}

func (s *Store) ensureAuthSessionLocked(sessionID string, session *SessionState) *AuthSessionState {
	if session.AuthSessionID != "" {
		authSession := s.authSessions[session.AuthSessionID]
		if authSession != nil {
			if authSession.WebSessionIDs == nil {
				authSession.WebSessionIDs = make(map[string]struct{})
			}
			authSession.WebSessionIDs[sessionID] = struct{}{}
			return authSession
		}
	}

	now := model.NowString()
	authSessionID := "auth-" + sessionID
	authSession := &AuthSessionState{
		ID:            authSessionID,
		WebSessionIDs: map[string]struct{}{sessionID: {}},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	session.AuthSessionID = authSessionID
	s.authSessions[authSessionID] = authSession
	return authSession
}

func (s *Store) LoadStableState(path string) error {
	if path == "" {
		s.mu.RLock()
		path = s.statePath
		s.mu.RUnlock()
	}
	if path == "" {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var state persistentState
	if err := json.Unmarshal(content, &state); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.browserSessions = make(map[string]*BrowserSessionState, len(state.BrowserSessions))
	s.sessions = make(map[string]*SessionState, len(state.Sessions))
	for id, session := range state.Sessions {
		if session == nil {
			continue
		}
		s.sessions[id] = &SessionState{
			ID:             session.ID,
			AuthSessionID:  session.AuthSessionID,
			ThreadID:       "",
			TurnID:         "",
			SelectedHostID: defaultHostID(session.SelectedHostID),
			Approvals:      make(map[string]model.ApprovalRequest),
			Choices:        make(map[string]model.ChoiceRequest),
			ApprovalGrants: append([]model.ApprovalGrant(nil), session.ApprovalGrants...),
			ItemCache:      make(map[string]map[string]any),
			Auth:           session.Auth,
			Tokens:         fromPersistentTokens(session.Tokens),
			Runtime:        defaultRuntime(session.SelectedHostID),
			CreatedAt:      session.CreatedAt,
			LastActivityAt: session.LastActivityAt,
		}
		if s.sessions[id].ID == "" {
			s.sessions[id].ID = id
		}
		if s.sessions[id].CreatedAt == "" {
			s.sessions[id].CreatedAt = model.NowString()
		}
		if s.sessions[id].LastActivityAt == "" {
			s.sessions[id].LastActivityAt = s.sessions[id].CreatedAt
		}
		if err := s.loadSessionTranscriptLocked(path, id); err != nil {
			return err
		}
	}

	for id, browserSession := range state.BrowserSessions {
		if browserSession == nil {
			continue
		}
		s.browserSessions[id] = &BrowserSessionState{
			ID:              browserSession.ID,
			ActiveSessionID: browserSession.ActiveSessionID,
			SessionIDs:      append([]string(nil), browserSession.SessionIDs...),
			CreatedAt:       browserSession.CreatedAt,
			UpdatedAt:       browserSession.UpdatedAt,
		}
		if s.browserSessions[id].ID == "" {
			s.browserSessions[id].ID = id
		}
		if s.browserSessions[id].CreatedAt == "" {
			s.browserSessions[id].CreatedAt = model.NowString()
		}
		if s.browserSessions[id].UpdatedAt == "" {
			s.browserSessions[id].UpdatedAt = s.browserSessions[id].CreatedAt
		}
	}

	s.authSessions = make(map[string]*AuthSessionState, len(state.AuthSessions))
	for id, authSession := range state.AuthSessions {
		if authSession == nil {
			continue
		}
		webSessionIDs := authSession.WebSessionIDs
		if webSessionIDs == nil {
			webSessionIDs = make(map[string]struct{})
		}
		s.authSessions[id] = &AuthSessionState{
			ID:            authSession.ID,
			Auth:          authSession.Auth,
			Tokens:        fromPersistentTokens(authSession.Tokens),
			WebSessionIDs: webSessionIDs,
			CreatedAt:     authSession.CreatedAt,
			UpdatedAt:     authSession.UpdatedAt,
		}
		if s.authSessions[id].ID == "" {
			s.authSessions[id].ID = id
		}
	}

	s.threadToSession = make(map[string]string)
	s.turnToSession = make(map[string]string)
	s.loginToSession = cloneStringMap(state.LoginToSession)
	s.lastAuthSession = state.LastAuthSession
	s.persistTimers = make(map[string]*time.Timer)
	s.hosts = cloneHostMap(state.Hosts)
	if s.hosts == nil {
		s.hosts = make(map[string]model.Host)
	}
	s.hosts[model.ServerLocalHostID] = serverLocalHost()

	return nil
}

func (s *Store) SaveStableState(path string) error {
	if path == "" {
		s.mu.RLock()
		path = s.statePath
		s.mu.RUnlock()
	}
	if path == "" {
		return nil
	}

	s.mu.RLock()
	state := persistentState{
		BrowserSessions: make(map[string]*persistentBrowserSessionState, len(s.browserSessions)),
		Sessions:        make(map[string]*persistentSessionState, len(s.sessions)),
		AuthSessions:    make(map[string]*persistentAuthSessionState, len(s.authSessions)),
		ThreadToSession: make(map[string]string),
		LoginToSession:  cloneStringMap(s.loginToSession),
		LastAuthSession: s.lastAuthSession,
		Hosts:           cloneHostMap(s.hosts),
	}
	for id, browserSession := range s.browserSessions {
		if browserSession == nil {
			continue
		}
		state.BrowserSessions[id] = &persistentBrowserSessionState{
			ID:              browserSession.ID,
			ActiveSessionID: browserSession.ActiveSessionID,
			SessionIDs:      append([]string(nil), browserSession.SessionIDs...),
			CreatedAt:       browserSession.CreatedAt,
			UpdatedAt:       browserSession.UpdatedAt,
		}
	}
	for id, session := range s.sessions {
		if session == nil {
			continue
		}
		state.Sessions[id] = &persistentSessionState{
			ID:             session.ID,
			AuthSessionID:  session.AuthSessionID,
			ThreadID:       "",
			SelectedHostID: session.SelectedHostID,
			Auth:           session.Auth,
			Tokens:         toPersistentTokens(session.Tokens),
			ApprovalGrants: append([]model.ApprovalGrant(nil), session.ApprovalGrants...),
			CreatedAt:      session.CreatedAt,
			LastActivityAt: session.LastActivityAt,
		}
	}
	for id, authSession := range s.authSessions {
		if authSession == nil {
			continue
		}
		state.AuthSessions[id] = &persistentAuthSessionState{
			ID:            authSession.ID,
			Auth:          authSession.Auth,
			Tokens:        toPersistentTokens(authSession.Tokens),
			WebSessionIDs: cloneStructMap(authSession.WebSessionIDs),
			CreatedAt:     authSession.CreatedAt,
			UpdatedAt:     authSession.UpdatedAt,
		}
	}
	s.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func cloneSession(session *SessionState) *SessionState {
	if session == nil {
		return nil
	}
	out := *session
	out.Cards = append([]model.Card(nil), session.Cards...)
	out.Approvals = make(map[string]model.ApprovalRequest, len(session.Approvals))
	out.Choices = make(map[string]model.ChoiceRequest, len(session.Choices))
	out.ApprovalGrants = append([]model.ApprovalGrant(nil), session.ApprovalGrants...)
	out.Runtime = cloneRuntime(session.Runtime)
	for k, v := range session.Approvals {
		out.Approvals[k] = v
	}
	for k, v := range session.Choices {
		out.Choices[k] = v
	}
	out.ItemCache = make(map[string]map[string]any, len(session.ItemCache))
	for k, v := range session.ItemCache {
		copyMap := make(map[string]any, len(v))
		for kk, vv := range v {
			copyMap[kk] = vv
		}
		out.ItemCache[k] = copyMap
	}
	return &out
}

func defaultRuntime(hostID string) model.RuntimeState {
	hostID = defaultHostID(hostID)
	return model.RuntimeState{
		Turn: model.TurnRuntime{
			Phase:  "idle",
			HostID: hostID,
		},
		Codex: model.CodexRuntime{
			Status:   "connected",
			RetryMax: 5,
		},
		Activity: model.ActivityRuntime{
			ViewedFiles:            make([]model.ActivityEntry, 0),
			SearchedWebQueries:     make([]model.ActivityEntry, 0),
			SearchedContentQueries: make([]model.ActivityEntry, 0),
		},
	}
}

func cloneRuntime(runtime model.RuntimeState) model.RuntimeState {
	out := runtime
	out.Activity.ViewedFiles = append([]model.ActivityEntry(nil), runtime.Activity.ViewedFiles...)
	out.Activity.SearchedWebQueries = append([]model.ActivityEntry(nil), runtime.Activity.SearchedWebQueries...)
	out.Activity.SearchedContentQueries = append([]model.ActivityEntry(nil), runtime.Activity.SearchedContentQueries...)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return make(map[string]string)
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStructMap(in map[string]struct{}) map[string]struct{} {
	if len(in) == 0 {
		return make(map[string]struct{})
	}
	out := make(map[string]struct{}, len(in))
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}

func cloneHostMap(in map[string]model.Host) map[string]model.Host {
	if len(in) == 0 {
		return make(map[string]model.Host)
	}
	out := make(map[string]model.Host, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func defaultHostID(hostID string) string {
	if hostID == "" {
		return model.ServerLocalHostID
	}
	return hostID
}

func serverLocalHost() model.Host {
	return model.Host{
		ID:         model.ServerLocalHostID,
		Name:       "server-local",
		Kind:       "server_local",
		Status:     "online",
		Executable: true,
	}
}

func toPersistentTokens(tokens model.ExternalAuthTokens) persistentExternalAuthTokens {
	return persistentExternalAuthTokens{
		IDToken:          tokens.IDToken,
		AccessToken:      tokens.AccessToken,
		ChatGPTAccountID: tokens.ChatGPTAccountID,
		ChatGPTPlanType:  tokens.ChatGPTPlanType,
		Email:            tokens.Email,
	}
}

func fromPersistentTokens(tokens persistentExternalAuthTokens) model.ExternalAuthTokens {
	return model.ExternalAuthTokens{
		IDToken:          tokens.IDToken,
		AccessToken:      tokens.AccessToken,
		ChatGPTAccountID: tokens.ChatGPTAccountID,
		ChatGPTPlanType:  tokens.ChatGPTPlanType,
		Email:            tokens.Email,
	}
}
