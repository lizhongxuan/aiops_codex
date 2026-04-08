package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type SessionState struct {
	ID               string
	AuthSessionID    string
	ThreadID         string
	ThreadConfigHash string
	TurnID           string
	SelectedHostID   string
	Meta             model.SessionMeta
	Cards            []model.Card
	Approvals        map[string]model.ApprovalRequest
	Choices          map[string]model.ChoiceRequest
	ApprovalGrants   []model.ApprovalGrant
	ItemCache        map[string]map[string]any
	Runtime          model.RuntimeState
	Auth             model.AuthState
	Tokens           model.ExternalAuthTokens
	CreatedAt        string
	LastActivityAt   string
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
	agentProfiles   map[string]model.AgentProfile
	skillCatalog    []model.AgentSkill
	mcpCatalog      []model.AgentMCP
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
	Meta           model.SessionMeta            `json:"meta,omitempty"`
	Auth           model.AuthState              `json:"auth"`
	Tokens         persistentExternalAuthTokens `json:"tokens"`
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
	BrowserSessions     map[string]*persistentBrowserSessionState `json:"browserSessions"`
	Sessions            map[string]*persistentSessionState        `json:"sessions"`
	AuthSessions        map[string]*persistentAuthSessionState    `json:"authSessions"`
	AgentProfiles       map[string]model.AgentProfile             `json:"agentProfiles"`
	AgentProfileVersion int                                       `json:"agentProfileVersion,omitempty"`
	SkillCatalog        []model.AgentSkill                        `json:"skillCatalog,omitempty"`
	MCPCatalog          []model.AgentMCP                          `json:"mcpCatalog,omitempty"`
	ThreadToSession     map[string]string                         `json:"threadToSession"`
	LoginToSession      map[string]string                         `json:"loginToSession"`
	LastAuthSession     string                                    `json:"lastAuthSession,omitempty"`
	Hosts               map[string]model.Host                     `json:"hosts"`
}

func New() *Store {
	return &Store{
		browserSessions: make(map[string]*BrowserSessionState),
		sessions:        make(map[string]*SessionState),
		authSessions:    make(map[string]*AuthSessionState),
		agentProfiles:   defaultAgentProfileMap(),
		skillCatalog:    cloneSkillCatalog(model.SupportedAgentSkills()),
		mcpCatalog:      cloneMCPCatalog(model.SupportedAgentMCPs()),
		threadToSession: make(map[string]string),
		turnToSession:   make(map[string]string),
		loginToSession:  make(map[string]string),
		persistTimers:   make(map[string]*time.Timer),
		hosts: map[string]model.Host{
			model.ServerLocalHostID: serverLocalHost(),
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

func (s *Store) SessionMeta(sessionID string) model.SessionMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[sessionID]
	if session == nil {
		return model.DefaultSessionMeta()
	}
	if isZeroSessionMeta(session.Meta) {
		return model.DefaultSessionMeta()
	}
	return model.NormalizeSessionMeta(session.Meta)
}

func (s *Store) EnsureSessionWithMeta(sessionID string, meta model.SessionMeta) *SessionState {
	s.mu.Lock()
	session, created := s.ensureSessionLocked(sessionID)
	if created || isZeroSessionMeta(session.Meta) {
		session.Meta = normalizeSessionMetaForCreate(meta)
	}
	cloned := cloneSession(session)
	s.mu.Unlock()
	if created {
		s.SaveStableState("")
	}
	return cloned
}

func (s *Store) UpdateSessionMeta(sessionID string, fn func(*model.SessionMeta)) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	meta := session.Meta
	if isZeroSessionMeta(meta) {
		meta = model.DefaultSessionMeta()
	}
	meta.RuntimePreset = ""
	fn(&meta)
	session.Meta = normalizeSessionMetaForCreate(meta)
	session.LastActivityAt = model.NowString()
	s.mu.Unlock()
	s.SaveStableState("")
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
	hostID = defaultHostID(hostID)
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	previousHostID := defaultHostID(session.SelectedHostID)
	if previousHostID != hostID {
		session.ApprovalGrants = nil
	}
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

func (s *Store) SetThreadConfigHash(sessionID, hash string) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	session.ThreadConfigHash = hash
	session.LastActivityAt = model.NowString()
	s.mu.Unlock()
}

func (s *Store) ClearThread(sessionID string) {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	if session.ThreadID != "" {
		delete(s.threadToSession, session.ThreadID)
	}
	session.ThreadID = ""
	session.ThreadConfigHash = ""
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
	session.ThreadConfigHash = ""
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

func (s *Store) AgentProfiles() []model.AgentProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profiles := make([]model.AgentProfile, 0, len(s.agentProfiles))
	for _, profile := range s.agentProfiles {
		profiles = append(profiles, cloneAgentProfile(profile))
	}
	model.SortAgentProfiles(profiles)
	return profiles
}

func (s *Store) SkillCatalog() []model.AgentSkill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSkillCatalog(s.skillCatalog)
}

func (s *Store) MCPCatalog() []model.AgentMCP {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneMCPCatalog(s.mcpCatalog)
}

func (s *Store) AgentProfile(profileID string) (model.AgentProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, ok := s.agentProfiles[profileID]
	if !ok {
		return model.AgentProfile{}, false
	}
	return cloneAgentProfile(profile), true
}

func (s *Store) UpsertAgentProfile(profile model.AgentProfile) {
	s.mu.Lock()
	normalized := model.CompleteAgentProfile(profile)
	if s.agentProfiles == nil {
		s.agentProfiles = defaultAgentProfileMap()
	}
	s.agentProfiles[normalized.ID] = normalized
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) UpsertSkillCatalogItem(item model.AgentSkill) {
	s.mu.Lock()
	s.ensureDefaultAgentCatalogsLocked()
	normalized := item
	found := false
	for index := range s.skillCatalog {
		if s.skillCatalog[index].ID != normalized.ID {
			continue
		}
		s.skillCatalog[index] = normalized
		found = true
		break
	}
	if !found {
		s.skillCatalog = append(s.skillCatalog, normalized)
	}
	slices.SortFunc(s.skillCatalog, func(left, right model.AgentSkill) int {
		return strings.Compare(left.ID, right.ID)
	})
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) DeleteSkillCatalogItem(skillID string) {
	s.mu.Lock()
	s.ensureDefaultAgentCatalogsLocked()
	filtered := s.skillCatalog[:0]
	for _, item := range s.skillCatalog {
		if item.ID == skillID {
			continue
		}
		filtered = append(filtered, item)
	}
	s.skillCatalog = filtered
	for profileID, profile := range s.agentProfiles {
		nextSkills := make([]model.AgentSkill, 0, len(profile.Skills))
		for _, skill := range profile.Skills {
			if skill.ID == skillID {
				continue
			}
			nextSkills = append(nextSkills, skill)
		}
		profile.Skills = nextSkills
		s.agentProfiles[profileID] = profile
	}
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) UpsertMCPCatalogItem(item model.AgentMCP) {
	s.mu.Lock()
	s.ensureDefaultAgentCatalogsLocked()
	normalized := item
	found := false
	for index := range s.mcpCatalog {
		if s.mcpCatalog[index].ID != normalized.ID {
			continue
		}
		s.mcpCatalog[index] = normalized
		found = true
		break
	}
	if !found {
		s.mcpCatalog = append(s.mcpCatalog, normalized)
	}
	slices.SortFunc(s.mcpCatalog, func(left, right model.AgentMCP) int {
		return strings.Compare(left.ID, right.ID)
	})
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) DeleteMCPCatalogItem(mcpID string) {
	s.mu.Lock()
	s.ensureDefaultAgentCatalogsLocked()
	filtered := s.mcpCatalog[:0]
	for _, item := range s.mcpCatalog {
		if item.ID == mcpID {
			continue
		}
		filtered = append(filtered, item)
	}
	s.mcpCatalog = filtered
	for profileID, profile := range s.agentProfiles {
		nextMCPs := make([]model.AgentMCP, 0, len(profile.MCPs))
		for _, item := range profile.MCPs {
			if item.ID == mcpID {
				continue
			}
			nextMCPs = append(nextMCPs, item)
		}
		profile.MCPs = nextMCPs
		s.agentProfiles[profileID] = profile
	}
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) ResetAgentProfile(profileID string) {
	s.mu.Lock()
	if s.agentProfiles == nil {
		s.agentProfiles = defaultAgentProfileMap()
	}
	s.ensureDefaultAgentCatalogsLocked()
	switch profileID {
	case string(model.AgentProfileTypeMainAgent), string(model.AgentProfileTypeHostAgentDefault), string(model.AgentProfileTypeHostAgentOverride):
		s.agentProfiles[profileID] = filteredDefaultAgentProfile(profileID, s.skillCatalog, s.mcpCatalog)
	default:
		delete(s.agentProfiles, profileID)
	}
	s.ensureDefaultAgentProfilesLocked()
	s.mu.Unlock()
	s.SaveStableState("")
}

func (s *Store) ResetAgentProfiles() {
	s.mu.Lock()
	s.ensureDefaultAgentCatalogsLocked()
	s.agentProfiles = defaultAgentProfileMapForCatalogs(s.skillCatalog, s.mcpCatalog)
	s.mu.Unlock()
	s.SaveStableState("")
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

func (s *Store) RemoveCard(sessionID, cardID string) bool {
	s.mu.Lock()
	session, _ := s.ensureSessionLocked(sessionID)
	for i := range session.Cards {
		if session.Cards[i].ID != cardID {
			continue
		}
		session.Cards = append(session.Cards[:i], session.Cards[i+1:]...)
		session.LastActivityAt = model.NowString()
		s.mu.Unlock()
		s.scheduleSessionPersistence(sessionID)
		return true
	}
	s.mu.Unlock()
	return false
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
	s.ResolveChoiceWithAnswers(sessionID, choiceID, status, resolvedAt, nil)
}

func (s *Store) ResolveChoiceWithAnswers(sessionID, choiceID, status, resolvedAt string, answers []model.ChoiceAnswer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, _ := s.ensureSessionLocked(sessionID)
	choice, ok := session.Choices[choiceID]
	if !ok {
		return
	}
	choice.Status = status
	if answers != nil {
		choice.Answers = append([]model.ChoiceAnswer(nil), answers...)
	}
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
	host.ID = defaultHostID(host.ID)
	if existing, ok := s.hosts[host.ID]; ok {
		host = mergeStoredHost(existing, host)
	}
	s.hosts[host.ID] = normalizeStoredHost(host)
	s.mu.Unlock()
	s.SaveStableState("")
}

// RemoveHost deletes a host entry from the in-memory store and persists.
func (s *Store) RemoveHost(hostID string) {
	s.mu.Lock()
	delete(s.hosts, hostID)
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
	if host.InstallState == "" {
		host.InstallState = "installed"
	}
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
		out = append(out, normalizeStoredHost(host))
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
	agentLoop, iterations, invocations, evidence := buildAgentLoopProjection(session, cards)
	return model.Snapshot{
		SessionID:           session.ID,
		Kind:                session.Meta.Kind,
		SelectedHostID:      session.SelectedHostID,
		Auth:                session.Auth,
		Hosts:               hosts,
		Cards:               cards,
		Approvals:           approvals,
		Runtime:             cloneRuntime(session.Runtime),
		AgentLoop:           agentLoop,
		AgentLoopIterations: iterations,
		ToolInvocations:     invocations,
		EvidenceSummaries:   evidence,
		LastActivityAt:      session.LastActivityAt,
		Config:              cfg,
	}
}

func buildAgentLoopProjection(session *SessionState, cards []model.Card) (*model.AgentLoopRun, []model.AgentLoopIteration, []model.ToolInvocation, []model.EvidenceRecord) {
	if session == nil {
		return nil, nil, nil, nil
	}
	runID := "loop-" + session.ID
	iterationID := "iter-" + runID
	if strings.TrimSpace(session.TurnID) != "" {
		iterationID = "iter-" + session.TurnID
	}
	phase := strings.TrimSpace(session.Runtime.Turn.Phase)
	if phase == "" {
		phase = "idle"
	}
	invocations, evidence := buildToolInvocationProjection(runID, iterationID, cards)
	if !session.Runtime.Turn.Active && phase == "idle" && len(invocations) == 0 {
		return nil, nil, nil, nil
	}

	status := "completed"
	switch phase {
	case "waiting_input":
		status = "waiting_user"
	case "waiting_approval":
		status = "waiting_approval"
	case "failed":
		status = "failed"
	case "aborted":
		status = "canceled"
	case "idle", "completed":
		status = "completed"
	default:
		if session.Runtime.Turn.Active {
			status = "running"
		}
	}
	mode := "answer"
	switch phase {
	case "planning":
		mode = "plan"
	case "executing", "finalizing":
		mode = "execute"
	case "waiting_input":
		mode = "answer"
	case "waiting_approval":
		mode = "plan"
	}
	run := &model.AgentLoopRun{
		ID:                runID,
		SessionID:         session.ID,
		Status:            status,
		Mode:              mode,
		Kind:              session.Meta.Kind,
		ActiveIterationID: iterationID,
		CreatedAt:         firstNonEmptyString(session.Runtime.Turn.StartedAt, session.CreatedAt),
		UpdatedAt:         session.LastActivityAt,
	}
	if !session.Runtime.Turn.Active {
		run.ActiveIterationID = ""
	}
	iteration := model.AgentLoopIteration{
		ID:            iterationID,
		RunID:         runID,
		Index:         1,
		StopReason:    agentLoopStopReason(phase, session.Runtime.Turn.Active),
		NeedsFollowUp: session.Runtime.Turn.Active && phase != "waiting_input" && phase != "waiting_approval",
		ModelAttempt:  1,
		StartedAt:     firstNonEmptyString(session.Runtime.Turn.StartedAt, session.CreatedAt),
		CompletedAt:   "",
	}
	if !session.Runtime.Turn.Active {
		iteration.CompletedAt = session.LastActivityAt
	}
	return run, []model.AgentLoopIteration{iteration}, invocations, evidence
}

func buildToolInvocationProjection(runID, iterationID string, cards []model.Card) ([]model.ToolInvocation, []model.EvidenceRecord) {
	invocations := make([]model.ToolInvocation, 0)
	evidence := make([]model.EvidenceRecord, 0)
	for _, card := range cards {
		name := toolInvocationNameForCard(card)
		if name == "" {
			continue
		}
		invocationID := "tool-" + card.ID
		evidenceID := "evidence-" + card.ID
		input := toolInvocationInputForCard(card)
		output := toolInvocationOutputForCard(card)
		invocations = append(invocations, model.ToolInvocation{
			ID:            invocationID,
			RunID:         runID,
			IterationID:   iterationID,
			Name:          name,
			Status:        toolInvocationStatusForCard(card),
			InputJSON:     stableJSON(input),
			OutputJSON:    stableJSON(output),
			InputSummary:  toolInvocationInputSummary(card),
			OutputSummary: toolInvocationOutputSummary(card),
			EvidenceID:    evidenceID,
			StartedAt:     card.CreatedAt,
			CompletedAt:   toolInvocationCompletedAt(card),
		})
		evidence = append(evidence, model.EvidenceRecord{
			ID:           evidenceID,
			RunID:        runID,
			InvocationID: invocationID,
			Kind:         name,
			Title:        firstNonEmptyString(card.Title, toolInvocationInputSummary(card), name),
			Summary:      toolInvocationOutputSummary(card),
			Content:      toolEvidenceContent(card),
			Metadata: map[string]any{
				"cardId": card.ID,
				"type":   card.Type,
				"status": card.Status,
				"hostId": card.HostID,
			},
			CreatedAt: firstNonEmptyString(card.CreatedAt, card.UpdatedAt),
		})
	}
	return invocations, evidence
}

func toolInvocationNameForCard(card model.Card) string {
	if tool := cardDetailString(card, "tool", "toolName"); tool != "" {
		return tool
	}
	switch card.Type {
	case "ChoiceCard":
		return "ask_user_question"
	case "CommandCard":
		return "command"
	case "CommandApprovalCard", "FileChangeApprovalCard":
		return "request_approval"
	case "DispatchSummaryCard":
		return "orchestrator_dispatch_tasks"
	case "PlanCard":
		return "update_plan"
	case "PlanApprovalCard":
		return "exit_plan_mode"
	default:
		return ""
	}
}

func toolInvocationStatusForCard(card model.Card) string {
	status := strings.TrimSpace(card.Status)
	if card.Type == "ChoiceCard" && status == "pending" {
		return "waiting_user"
	}
	if (card.Type == "CommandApprovalCard" || card.Type == "FileChangeApprovalCard" || card.Type == "PlanApprovalCard") && status == "pending" {
		return "waiting_approval"
	}
	if status == "" {
		return "completed"
	}
	return status
}

func toolInvocationInputForCard(card model.Card) map[string]any {
	switch card.Type {
	case "ChoiceCard":
		return map[string]any{"questions": card.Questions, "question": card.Question, "options": card.Options}
	case "CommandCard", "CommandApprovalCard":
		return map[string]any{"hostId": card.HostID, "cwd": card.Cwd, "command": card.Command}
	case "FileChangeApprovalCard":
		return map[string]any{"hostId": card.HostID, "changes": card.Changes, "reason": card.Text}
	case "DispatchSummaryCard", "PlanCard", "PlanApprovalCard":
		return map[string]any{"title": card.Title, "text": card.Text, "items": card.Items, "detail": card.Detail}
	default:
		return map[string]any{"title": card.Title, "text": card.Text}
	}
}

func toolInvocationOutputForCard(card model.Card) map[string]any {
	return map[string]any{
		"status":        card.Status,
		"summary":       card.Summary,
		"text":          card.Text,
		"message":       card.Message,
		"output":        card.Output,
		"stdout":        card.Stdout,
		"stderr":        card.Stderr,
		"exitCode":      card.ExitCode,
		"answerSummary": card.AnswerSummary,
		"durationMs":    card.DurationMS,
		"approval":      card.Approval,
	}
}

func toolInvocationInputSummary(card model.Card) string {
	switch card.Type {
	case "ChoiceCard":
		return firstNonEmptyString(card.Question, card.Title, "澄清用户意图")
	case "CommandCard", "CommandApprovalCard":
		return strings.TrimSpace(card.Command)
	case "FileChangeApprovalCard":
		return firstNonEmptyString(card.Text, "文件变更审批")
	case "PlanCard":
		return firstNonEmptyString(card.Title, card.Text, "计划更新")
	case "PlanApprovalCard":
		return firstNonEmptyString(card.Title, card.Summary, card.Text, "计划审批")
	default:
		return firstNonEmptyString(card.Title, card.Text)
	}
}

func toolInvocationOutputSummary(card model.Card) string {
	switch card.Type {
	case "ChoiceCard":
		if len(card.AnswerSummary) > 0 {
			return strings.Join(card.AnswerSummary, "; ")
		}
		if strings.TrimSpace(card.Status) == "pending" {
			return "等待用户回答"
		}
	case "CommandCard":
		return compactProjectionText(firstNonEmptyString(card.Output, card.Stdout, card.Stderr, card.Text, card.Summary, card.Status), 240)
	case "CommandApprovalCard", "FileChangeApprovalCard", "PlanApprovalCard":
		return firstNonEmptyString(card.Text, card.Summary, card.Status)
	}
	return compactProjectionText(firstNonEmptyString(card.Summary, card.Text, card.Message, card.Status), 240)
}

func toolEvidenceContent(card model.Card) string {
	switch card.Type {
	case "CommandCard":
		return firstNonEmptyString(card.Output, strings.TrimSpace(strings.Join([]string{card.Stdout, card.Stderr}, "\n")), card.Text, card.Summary)
	case "ChoiceCard":
		return strings.Join(append([]string{card.Question}, card.AnswerSummary...), "\n")
	case "PlanCard", "PlanApprovalCard":
		return firstNonEmptyString(stableJSON(card.Detail), card.Text, card.Summary, card.Message)
	default:
		return firstNonEmptyString(card.Output, card.Text, card.Summary, card.Message)
	}
}

func cardDetailString(card model.Card, keys ...string) string {
	if card.Detail == nil {
		return ""
	}
	for _, key := range keys {
		value, ok := card.Detail[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func toolInvocationCompletedAt(card model.Card) string {
	switch toolInvocationStatusForCard(card) {
	case "pending", "running", "waiting_user", "waiting_approval":
		return ""
	default:
		return firstNonEmptyString(card.UpdatedAt, card.CreatedAt)
	}
}

func agentLoopStopReason(phase string, active bool) string {
	switch phase {
	case "waiting_input":
		return "waiting_user"
	case "waiting_approval":
		return "waiting_approval"
	case "failed":
		return "failed"
	case "aborted":
		return "canceled"
	case "completed", "idle":
		return "end_turn"
	default:
		if active {
			return "tool_use"
		}
		return "end_turn"
	}
}

func stableJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

func compactProjectionText(value string, maxLen int) string {
	text := strings.Join(strings.Fields(value), " ")
	if maxLen > 0 && len(text) > maxLen {
		return text[:maxLen] + "..."
	}
	return text
}

func (s *Store) ensureSessionLocked(sessionID string) (*SessionState, bool) {
	session, ok := s.sessions[sessionID]
	if ok {
		if isZeroSessionMeta(session.Meta) {
			session.Meta = model.DefaultSessionMeta()
		} else {
			session.Meta = normalizeSessionMetaForLoad(session.Meta)
		}
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
		Meta:           model.DefaultSessionMeta(),
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
		meta := normalizeSessionMetaForLoad(session.Meta)
		s.sessions[id] = &SessionState{
			ID:             session.ID,
			AuthSessionID:  session.AuthSessionID,
			ThreadID:       "",
			TurnID:         "",
			SelectedHostID: defaultHostID(session.SelectedHostID),
			Meta:           meta,
			Approvals:      make(map[string]model.ApprovalRequest),
			Choices:        make(map[string]model.ChoiceRequest),
			ApprovalGrants: make([]model.ApprovalGrant, 0),
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
	s.agentProfiles = cloneAgentProfileMap(state.AgentProfiles)
	s.skillCatalog = cloneSkillCatalog(state.SkillCatalog)
	s.mcpCatalog = cloneMCPCatalog(state.MCPCatalog)
	s.ensureDefaultAgentCatalogsLocked()
	s.ensureDefaultAgentProfilesLocked()

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
		BrowserSessions:     make(map[string]*persistentBrowserSessionState, len(s.browserSessions)),
		Sessions:            make(map[string]*persistentSessionState, len(s.sessions)),
		AuthSessions:        make(map[string]*persistentAuthSessionState, len(s.authSessions)),
		AgentProfiles:       cloneAgentProfileMap(s.agentProfiles),
		AgentProfileVersion: model.AgentProfileConfigVersion,
		SkillCatalog:        cloneSkillCatalog(s.skillCatalog),
		MCPCatalog:          cloneMCPCatalog(s.mcpCatalog),
		ThreadToSession:     make(map[string]string),
		LoginToSession:      cloneStringMap(s.loginToSession),
		LastAuthSession:     s.lastAuthSession,
		Hosts:               cloneHostMap(s.hosts),
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
			Meta:           normalizeSessionMetaForPersist(session.Meta),
			Auth:           session.Auth,
			Tokens:         toPersistentTokens(session.Tokens),
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

func isZeroSessionMeta(meta model.SessionMeta) bool {
	return meta.Kind == "" && !meta.Visible && meta.MissionID == "" && meta.WorkspaceSessionID == "" && meta.WorkerHostID == "" && meta.RuntimePreset == ""
}

func normalizeSessionMetaForCreate(meta model.SessionMeta) model.SessionMeta {
	if isZeroSessionMeta(meta) {
		return model.DefaultSessionMeta()
	}
	meta = model.NormalizeSessionMeta(meta)
	if meta.Kind == "" {
		meta.Kind = model.SessionKindSingleHost
	}
	if meta.RuntimePreset == "" {
		meta = model.NormalizeSessionMeta(meta)
	}
	return meta
}

func normalizeSessionMetaForLoad(meta model.SessionMeta) model.SessionMeta {
	if isZeroSessionMeta(meta) {
		return model.DefaultSessionMeta()
	}
	meta = model.NormalizeSessionMeta(meta)
	if meta.Kind == "" {
		meta.Kind = model.SessionKindSingleHost
	}
	if meta.RuntimePreset == "" {
		meta = model.NormalizeSessionMeta(meta)
	}
	return meta
}

func normalizeSessionMetaForPersist(meta model.SessionMeta) model.SessionMeta {
	if isZeroSessionMeta(meta) {
		return model.DefaultSessionMeta()
	}
	return normalizeSessionMetaForCreate(meta)
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

func cloneAgentProfile(profile model.AgentProfile) model.AgentProfile {
	out := profile
	out.CommandPermissions.Enabled = cloneBoolPtr(profile.CommandPermissions.Enabled)
	out.CommandPermissions.AllowShellWrapper = cloneBoolPtr(profile.CommandPermissions.AllowShellWrapper)
	out.CommandPermissions.AllowSudo = cloneBoolPtr(profile.CommandPermissions.AllowSudo)
	out.CommandPermissions.AllowedWritableRoots = append([]string(nil), profile.CommandPermissions.AllowedWritableRoots...)
	out.CommandPermissions.CategoryPolicies = cloneStringMap(profile.CommandPermissions.CategoryPolicies)
	out.Skills = append([]model.AgentSkill(nil), profile.Skills...)
	out.MCPs = append([]model.AgentMCP(nil), profile.MCPs...)
	return out
}

func cloneSkillCatalog(in []model.AgentSkill) []model.AgentSkill {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.AgentSkill, 0, len(in))
	for _, item := range in {
		out = append(out, item)
	}
	return out
}

func cloneMCPCatalog(in []model.AgentMCP) []model.AgentMCP {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.AgentMCP, 0, len(in))
	for _, item := range in {
		out = append(out, item)
	}
	return out
}

func cloneAgentProfileMap(in map[string]model.AgentProfile) map[string]model.AgentProfile {
	if len(in) == 0 {
		return make(map[string]model.AgentProfile)
	}
	out := make(map[string]model.AgentProfile, len(in))
	for k, v := range in {
		out[k] = cloneAgentProfile(v)
	}
	return out
}

func defaultAgentProfileMap() map[string]model.AgentProfile {
	profiles := model.DefaultAgentProfiles()
	out := make(map[string]model.AgentProfile, len(profiles))
	for _, profile := range profiles {
		out[profile.ID] = cloneAgentProfile(profile)
	}
	return out
}

func filteredDefaultAgentProfile(profileID string, skillCatalog []model.AgentSkill, mcpCatalog []model.AgentMCP) model.AgentProfile {
	profile := model.DefaultAgentProfile(profileID)
	allowedSkills := make(map[string]struct{}, len(skillCatalog))
	for _, item := range skillCatalog {
		if item.ID == "" {
			continue
		}
		allowedSkills[item.ID] = struct{}{}
	}
	filteredSkills := make([]model.AgentSkill, 0, len(profile.Skills))
	for _, item := range profile.Skills {
		if _, ok := allowedSkills[item.ID]; !ok {
			continue
		}
		filteredSkills = append(filteredSkills, item)
	}
	profile.Skills = filteredSkills

	allowedMCPs := make(map[string]struct{}, len(mcpCatalog))
	for _, item := range mcpCatalog {
		if item.ID == "" {
			continue
		}
		allowedMCPs[item.ID] = struct{}{}
	}
	filteredMCPs := make([]model.AgentMCP, 0, len(profile.MCPs))
	for _, item := range profile.MCPs {
		if _, ok := allowedMCPs[item.ID]; !ok {
			continue
		}
		filteredMCPs = append(filteredMCPs, item)
	}
	profile.MCPs = filteredMCPs
	return profile
}

func defaultAgentProfileMapForCatalogs(skillCatalog []model.AgentSkill, mcpCatalog []model.AgentMCP) map[string]model.AgentProfile {
	out := make(map[string]model.AgentProfile, len(model.DefaultAgentProfiles()))
	for _, profileID := range model.DefaultAgentProfileIDs() {
		profile := filteredDefaultAgentProfile(profileID, skillCatalog, mcpCatalog)
		out[profile.ID] = cloneAgentProfile(profile)
	}
	return out
}

func cloneBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func (s *Store) ensureDefaultAgentProfilesLocked() {
	if s.agentProfiles == nil {
		s.agentProfiles = defaultAgentProfileMapForCatalogs(s.skillCatalog, s.mcpCatalog)
		return
	}
	for _, profileID := range model.DefaultAgentProfileIDs() {
		profile := filteredDefaultAgentProfile(profileID, s.skillCatalog, s.mcpCatalog)
		if _, ok := s.agentProfiles[profile.ID]; !ok {
			s.agentProfiles[profile.ID] = cloneAgentProfile(profile)
			continue
		}
		s.agentProfiles[profile.ID] = model.CompleteAgentProfile(s.agentProfiles[profile.ID])
	}
}

func (s *Store) ensureDefaultAgentCatalogsLocked() {
	if len(s.skillCatalog) == 0 {
		s.skillCatalog = cloneSkillCatalog(model.SupportedAgentSkills())
	}
	if len(s.mcpCatalog) == 0 {
		s.mcpCatalog = cloneMCPCatalog(model.SupportedAgentMCPs())
	}
}

func defaultHostID(hostID string) string {
	if hostID == "" {
		return model.ServerLocalHostID
	}
	return hostID
}

func serverLocalHost() model.Host {
	return model.Host{
		ID:              model.ServerLocalHostID,
		Name:            "server-local",
		Kind:            "server_local",
		Address:         "127.0.0.1",
		Transport:       "local",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
		InstallState:    "installed",
		ControlMode:     "local",
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
