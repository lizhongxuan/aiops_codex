package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu    sync.RWMutex
	path  string
	state persistedState
}

type persistedState struct {
	Version            int                        `json:"version"`
	Missions           map[string]*Mission        `json:"missions"`
	Sessions           map[string]SessionMeta     `json:"sessions"`
	SeenBySession      map[string]WorkerSeenState `json:"seenBySession"`
	MissionByWorkspace map[string]string          `json:"missionByWorkspace"`
	MissionByPlanner   map[string]string          `json:"missionByPlanner"`
	MissionByWorker    map[string]string          `json:"missionByWorker"`
	WorkerByHost       map[string]string          `json:"workerByHost"`
	ApprovalToWorker   map[string]string          `json:"approvalToWorker"`
	ChoiceToSession    map[string]string          `json:"choiceToSession"`
}

func NewStore(path string) *Store {
	s := &Store{path: path}
	s.state = persistedState{
		Version:            1,
		Missions:           make(map[string]*Mission),
		Sessions:           make(map[string]SessionMeta),
		SeenBySession:      make(map[string]WorkerSeenState),
		MissionByWorkspace: make(map[string]string),
		MissionByPlanner:   make(map[string]string),
		MissionByWorker:    make(map[string]string),
		WorkerByHost:       make(map[string]string),
		ApprovalToWorker:   make(map[string]string),
		ChoiceToSession:    make(map[string]string),
	}
	return s
}

func (s *Store) Path() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.path
}

func (s *Store) SetPath(path string) {
	s.mu.Lock()
	s.path = path
	s.mu.Unlock()
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil
	}
	content, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.state = newPersistedState()
			return nil
		}
		return err
	}
	var state persistedState
	if err := json.Unmarshal(content, &state); err != nil {
		return err
	}
	state.normalize()
	s.state = state
	return nil
}

func (s *Store) Save() error {
	s.mu.RLock()
	if s.path == "" {
		s.mu.RUnlock()
		return nil
	}
	payload := s.state.clone()
	path := s.path
	s.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (s *Store) Mission(missionID string) (*Mission, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mission, ok := s.state.Missions[missionID]
	return cloneMission(mission), ok
}

func (s *Store) Missions() []*Mission {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Mission, 0, len(s.state.Missions))
	for _, mission := range s.state.Missions {
		out = append(out, cloneMission(mission))
	}
	slices.SortFunc(out, func(a, b *Mission) int {
		switch {
		case a == nil && b == nil:
			return 0
		case a == nil:
			return 1
		case b == nil:
			return -1
		case a.UpdatedAt > b.UpdatedAt:
			return -1
		case a.UpdatedAt < b.UpdatedAt:
			return 1
		default:
			return 0
		}
	})
	return out
}

func (s *Store) UpsertMission(mission *Mission) (*Mission, error) {
	if mission == nil {
		return nil, errors.New("mission is nil")
	}
	if mission.ID == "" {
		return nil, errors.New("mission id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Missions == nil {
		s.state = newPersistedState()
	}
	mission = cloneMission(mission)
	if mission.Status == "" {
		mission.Status = MissionStatusPending
	}
	if mission.ProjectionMode == "" {
		mission.ProjectionMode = "front_projection"
	}
	if mission.Workers == nil {
		mission.Workers = make(map[string]*HostWorker)
	}
	if mission.Tasks == nil {
		mission.Tasks = make(map[string]*TaskRun)
	}
	if mission.Workspaces == nil {
		mission.Workspaces = make(map[string]*WorkspaceLease)
	}
	s.state.Missions[mission.ID] = mission
	return cloneMission(mission), nil
}

func (s *Store) UpdateMission(missionID string, fn func(*Mission) error) (*Mission, error) {
	if missionID == "" {
		return nil, errors.New("mission id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	mission, ok := s.state.Missions[missionID]
	if !ok {
		return nil, fmt.Errorf("mission %q not found", missionID)
	}
	working := cloneMission(mission)
	if working.Workers == nil {
		working.Workers = make(map[string]*HostWorker)
	}
	if working.Tasks == nil {
		working.Tasks = make(map[string]*TaskRun)
	}
	if working.Workspaces == nil {
		working.Workspaces = make(map[string]*WorkspaceLease)
	}
	if err := fn(working); err != nil {
		return nil, err
	}
	s.state.Missions[missionID] = working
	return cloneMission(working), nil
}

func (s *Store) DeleteMission(missionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Missions, missionID)
	for sessionID, id := range s.state.MissionByWorkspace {
		if id == missionID {
			delete(s.state.MissionByWorkspace, sessionID)
		}
	}
	for sessionID, id := range s.state.MissionByPlanner {
		if id == missionID {
			delete(s.state.MissionByPlanner, sessionID)
		}
	}
	for sessionID, id := range s.state.MissionByWorker {
		if id == missionID {
			delete(s.state.MissionByWorker, sessionID)
		}
	}
}

func (s *Store) UpsertSessionMeta(sessionID string, meta SessionMeta) SessionMeta {
	now := nowString()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Sessions == nil {
		s.state = newPersistedState()
	}
	meta = normalizeSessionMeta(meta)
	meta.UpdatedAt = now
	if meta.CreatedAt == "" {
		meta.CreatedAt = now
	} else if existing, ok := s.state.Sessions[sessionID]; ok && existing.CreatedAt != "" {
		meta.CreatedAt = existing.CreatedAt
	}
	s.state.Sessions[sessionID] = meta
	s.indexSessionMetaLocked(sessionID, meta)
	return meta
}

func (s *Store) SessionMeta(sessionID string) (SessionMeta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	meta, ok := s.state.Sessions[sessionID]
	return normalizeSessionMeta(meta), ok
}

func (s *Store) SessionSeenState(sessionID string) (WorkerSeenState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.state.SeenBySession[sessionID]
	return normalizeSeenState(state), ok
}

func (s *Store) UpdateSessionSeenState(sessionID string, fn func(*WorkerSeenState) bool) (WorkerSeenState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.SeenBySession == nil {
		s.state.SeenBySession = make(map[string]WorkerSeenState)
	}
	state := normalizeSeenState(s.state.SeenBySession[sessionID])
	if fn == nil {
		return state, false
	}
	changed := fn(&state)
	state = normalizeSeenState(state)
	if changed {
		s.state.SeenBySession[sessionID] = state
	}
	return state, changed
}

func (s *Store) SessionIDsByKind(kind SessionKind) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0)
	for sessionID, meta := range s.state.Sessions {
		if meta.Kind == kind {
			out = append(out, sessionID)
		}
	}
	slices.Sort(out)
	return out
}

func (s *Store) MissionIDBySession(sessionID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if missionID, ok := s.state.MissionByWorkspace[sessionID]; ok {
		return missionID, true
	}
	if missionID, ok := s.state.MissionByPlanner[sessionID]; ok {
		return missionID, true
	}
	if missionID, ok := s.state.MissionByWorker[sessionID]; ok {
		return missionID, true
	}
	meta, ok := s.state.Sessions[sessionID]
	if !ok {
		return "", false
	}
	return meta.MissionID, meta.MissionID != ""
}

func (s *Store) MissionByWorkspaceSession(sessionID string) (*Mission, bool) {
	missionID, ok := s.MissionIDByWorkspaceSession(sessionID)
	if !ok {
		return nil, false
	}
	return s.Mission(missionID)
}

func (s *Store) MissionIDByWorkspaceSession(sessionID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	missionID, ok := s.state.MissionByWorkspace[sessionID]
	return missionID, ok
}

func (s *Store) MissionIDByPlannerSession(sessionID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	missionID, ok := s.state.MissionByPlanner[sessionID]
	return missionID, ok
}

func (s *Store) MissionIDByWorkerSession(sessionID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	missionID, ok := s.state.MissionByWorker[sessionID]
	return missionID, ok
}

func (s *Store) LinkApprovalToWorker(approvalID, workerSessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.ApprovalToWorker == nil {
		s.state.ApprovalToWorker = make(map[string]string)
	}
	s.state.ApprovalToWorker[approvalID] = workerSessionID
}

func (s *Store) ResolveApprovalRoute(approvalID string) (ApprovalRoute, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	workerSessionID, ok := s.state.ApprovalToWorker[approvalID]
	if !ok || workerSessionID == "" {
		return ApprovalRoute{}, false
	}
	workerMeta, ok := s.state.Sessions[workerSessionID]
	if !ok {
		return ApprovalRoute{ApprovalID: approvalID, WorkerSessionID: workerSessionID, OK: true}, true
	}
	return ApprovalRoute{
		MissionID:       workerMeta.MissionID,
		WorkerSessionID: workerSessionID,
		WorkerHostID:    workerMeta.WorkerHostID,
		ApprovalID:      approvalID,
		OK:              true,
	}, true
}

func (s *Store) LinkChoiceToSession(choiceID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.ChoiceToSession == nil {
		s.state.ChoiceToSession = make(map[string]string)
	}
	s.state.ChoiceToSession[choiceID] = sessionID
}

func (s *Store) ResolveChoiceRoute(choiceID string) (ChoiceRoute, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionID, ok := s.state.ChoiceToSession[choiceID]
	if !ok || sessionID == "" {
		return ChoiceRoute{}, false
	}
	meta, ok := s.state.Sessions[sessionID]
	if !ok {
		return ChoiceRoute{SessionID: sessionID, ChoiceID: choiceID, OK: true}, true
	}
	return ChoiceRoute{
		MissionID: meta.MissionID,
		SessionID: sessionID,
		ChoiceID:  choiceID,
		OK:        true,
	}, true
}

func (s *Store) WorkerSessionIDByApproval(approvalID string) (string, bool) {
	route, ok := s.ResolveApprovalRoute(approvalID)
	if !ok {
		return "", false
	}
	return route.WorkerSessionID, true
}

func (s *Store) SessionIDByChoice(choiceID string) (string, bool) {
	route, ok := s.ResolveChoiceRoute(choiceID)
	if !ok {
		return "", false
	}
	return route.SessionID, true
}

func (s *Store) normalizeMissionState(mission *Mission) *Mission {
	if mission == nil {
		return nil
	}
	if mission.Workers == nil {
		mission.Workers = make(map[string]*HostWorker)
	}
	if mission.Tasks == nil {
		mission.Tasks = make(map[string]*TaskRun)
	}
	if mission.Workspaces == nil {
		mission.Workspaces = make(map[string]*WorkspaceLease)
	}
	if mission.Events == nil {
		mission.Events = make([]RelayEvent, 0)
	}
	if mission.Status == "" {
		mission.Status = MissionStatusPending
	}
	if mission.ProjectionMode == "" {
		mission.ProjectionMode = "front_projection"
	}
	if mission.GlobalActiveBudget <= 0 {
		mission.GlobalActiveBudget = DefaultGlobalActiveBudget
	}
	if mission.MissionActiveBudget <= 0 {
		mission.MissionActiveBudget = DefaultMissionActiveBudget
	}
	return mission
}

func (s *Store) appendMissionEventLocked(missionID string, event RelayEvent) {
	mission, ok := s.state.Missions[missionID]
	if !ok || mission == nil {
		return
	}
	mission = s.normalizeMissionState(mission)
	mission.Events = append(mission.Events, event)
	if len(mission.Events) > DefaultEventWindowSize {
		mission.Events = append([]RelayEvent(nil), mission.Events[len(mission.Events)-DefaultEventWindowSize:]...)
	}
	mission.UpdatedAt = event.CreatedAt
	s.state.Missions[missionID] = mission
}

func (s *Store) indexSessionMetaLocked(sessionID string, meta SessionMeta) {
	delete(s.state.MissionByWorkspace, sessionID)
	delete(s.state.MissionByPlanner, sessionID)
	delete(s.state.MissionByWorker, sessionID)
	switch meta.Kind {
	case SessionKindWorkspace:
		if meta.MissionID != "" {
			s.state.MissionByWorkspace[sessionID] = meta.MissionID
		}
	case SessionKindPlanner:
		if meta.MissionID != "" {
			s.state.MissionByPlanner[sessionID] = meta.MissionID
		}
	case SessionKindWorker:
		if meta.MissionID != "" {
			s.state.MissionByWorker[sessionID] = meta.MissionID
		}
		if meta.WorkerHostID != "" {
			if s.state.WorkerByHost == nil {
				s.state.WorkerByHost = make(map[string]string)
			}
			s.state.WorkerByHost[meta.WorkerHostID] = sessionID
		}
	}
}

func newPersistedState() persistedState {
	return persistedState{
		Version:            1,
		Missions:           make(map[string]*Mission),
		Sessions:           make(map[string]SessionMeta),
		SeenBySession:      make(map[string]WorkerSeenState),
		MissionByWorkspace: make(map[string]string),
		MissionByPlanner:   make(map[string]string),
		MissionByWorker:    make(map[string]string),
		WorkerByHost:       make(map[string]string),
		ApprovalToWorker:   make(map[string]string),
		ChoiceToSession:    make(map[string]string),
	}
}

func (s persistedState) clone() persistedState {
	payload, _ := json.Marshal(s)
	var out persistedState
	_ = json.Unmarshal(payload, &out)
	out.normalize()
	return out
}

func (s *persistedState) normalize() {
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Missions == nil {
		s.Missions = make(map[string]*Mission)
	}
	if s.Sessions == nil {
		s.Sessions = make(map[string]SessionMeta)
	}
	if s.SeenBySession == nil {
		s.SeenBySession = make(map[string]WorkerSeenState)
	}
	if s.MissionByWorkspace == nil {
		s.MissionByWorkspace = make(map[string]string)
	}
	if s.MissionByPlanner == nil {
		s.MissionByPlanner = make(map[string]string)
	}
	if s.MissionByWorker == nil {
		s.MissionByWorker = make(map[string]string)
	}
	if s.WorkerByHost == nil {
		s.WorkerByHost = make(map[string]string)
	}
	if s.ApprovalToWorker == nil {
		s.ApprovalToWorker = make(map[string]string)
	}
	if s.ChoiceToSession == nil {
		s.ChoiceToSession = make(map[string]string)
	}
	for id, meta := range s.Sessions {
		s.Sessions[id] = normalizeSessionMeta(meta)
	}
	for id, state := range s.SeenBySession {
		s.SeenBySession[id] = normalizeSeenState(state)
	}
	s.MissionByWorkspace = make(map[string]string)
	s.MissionByPlanner = make(map[string]string)
	s.MissionByWorker = make(map[string]string)
	s.WorkerByHost = make(map[string]string)
	for id, mission := range s.Missions {
		normalizedMission := normalizeLoadedMission(cloneMission(mission))
		s.Missions[id] = normalizedMission
		s.backfillMissionSessionMeta(normalizedMission)
	}
	for sessionID, meta := range s.Sessions {
		s.indexSessionMeta(sessionID, meta)
	}
}

func (s *persistedState) backfillMissionSessionMeta(mission *Mission) {
	if mission == nil {
		return
	}
	if workspaceSessionID := strings.TrimSpace(mission.WorkspaceSessionID); workspaceSessionID != "" {
		s.Sessions[workspaceSessionID] = mergeSessionMeta(s.Sessions[workspaceSessionID], SessionMeta{
			Kind:               SessionKindWorkspace,
			Visible:            true,
			MissionID:          mission.ID,
			WorkspaceSessionID: workspaceSessionID,
			RuntimePreset:      RuntimePresetWorkspaceFront,
		})
	}
	if plannerSessionID := strings.TrimSpace(mission.PlannerSessionID); plannerSessionID != "" {
		s.Sessions[plannerSessionID] = mergeSessionMeta(s.Sessions[plannerSessionID], SessionMeta{
			Kind:               SessionKindPlanner,
			Visible:            false,
			MissionID:          mission.ID,
			WorkspaceSessionID: mission.WorkspaceSessionID,
			RuntimePreset:      RuntimePresetPlannerInternal,
			PlannerThreadID:    mission.PlannerThreadID,
		})
	}
	for hostID, worker := range mission.Workers {
		if worker == nil || strings.TrimSpace(worker.SessionID) == "" {
			continue
		}
		s.Sessions[worker.SessionID] = mergeSessionMeta(s.Sessions[worker.SessionID], SessionMeta{
			Kind:               SessionKindWorker,
			Visible:            false,
			MissionID:          mission.ID,
			WorkspaceSessionID: mission.WorkspaceSessionID,
			WorkerHostID:       firstNonEmpty(strings.TrimSpace(worker.HostID), strings.TrimSpace(hostID)),
			RuntimePreset:      RuntimePresetWorkerInternal,
			WorkerThreadID:     worker.ThreadID,
		})
	}
}

func (s *persistedState) indexSessionMeta(sessionID string, meta SessionMeta) {
	switch meta.Kind {
	case SessionKindWorkspace:
		if meta.MissionID != "" {
			s.MissionByWorkspace[sessionID] = meta.MissionID
		}
	case SessionKindPlanner:
		if meta.MissionID != "" {
			s.MissionByPlanner[sessionID] = meta.MissionID
		}
	case SessionKindWorker:
		if meta.MissionID != "" {
			s.MissionByWorker[sessionID] = meta.MissionID
		}
		if meta.WorkerHostID != "" {
			s.WorkerByHost[meta.WorkerHostID] = sessionID
		}
	}
}

func normalizeLoadedMission(mission *Mission) *Mission {
	if mission == nil {
		return nil
	}
	if mission.Workers == nil {
		mission.Workers = make(map[string]*HostWorker)
	}
	if mission.Tasks == nil {
		mission.Tasks = make(map[string]*TaskRun)
	}
	if mission.Workspaces == nil {
		mission.Workspaces = make(map[string]*WorkspaceLease)
	}
	if mission.Events == nil {
		mission.Events = make([]RelayEvent, 0)
	}
	if mission.Status == "" {
		mission.Status = MissionStatusPending
	}
	if mission.ProjectionMode == "" {
		mission.ProjectionMode = "front_projection"
	}
	if mission.GlobalActiveBudget <= 0 {
		mission.GlobalActiveBudget = DefaultGlobalActiveBudget
	}
	if mission.MissionActiveBudget <= 0 {
		mission.MissionActiveBudget = DefaultMissionActiveBudget
	}
	return mission
}

func mergeSessionMeta(existing, recovered SessionMeta) SessionMeta {
	meta := normalizeSessionMeta(existing)
	meta.Kind = recovered.Kind
	if recovered.Kind == SessionKindWorkspace {
		meta.Visible = true
	}
	if meta.MissionID == "" {
		meta.MissionID = recovered.MissionID
	}
	if meta.WorkspaceSessionID == "" {
		meta.WorkspaceSessionID = recovered.WorkspaceSessionID
	}
	if meta.WorkerHostID == "" {
		meta.WorkerHostID = recovered.WorkerHostID
	}
	if meta.RuntimePreset == "" {
		meta.RuntimePreset = recovered.RuntimePreset
	}
	if meta.PlannerThreadID == "" {
		meta.PlannerThreadID = recovered.PlannerThreadID
	}
	if meta.WorkerThreadID == "" {
		meta.WorkerThreadID = recovered.WorkerThreadID
	}
	return normalizeSessionMeta(meta)
}

func cloneMission(in *Mission) *Mission {
	if in == nil {
		return nil
	}
	payload, _ := json.Marshal(in)
	var out Mission
	_ = json.Unmarshal(payload, &out)
	if out.Workers == nil {
		out.Workers = make(map[string]*HostWorker)
	}
	if out.Tasks == nil {
		out.Tasks = make(map[string]*TaskRun)
	}
	if out.Workspaces == nil {
		out.Workspaces = make(map[string]*WorkspaceLease)
	}
	if out.Events == nil {
		out.Events = make([]RelayEvent, 0)
	}
	return &out
}

func cloneWorker(in *HostWorker) *HostWorker {
	if in == nil {
		return nil
	}
	payload, _ := json.Marshal(in)
	var out HostWorker
	_ = json.Unmarshal(payload, &out)
	if out.QueueTaskIDs == nil {
		out.QueueTaskIDs = make([]string, 0)
	}
	return &out
}

func cloneTask(in *TaskRun) *TaskRun {
	if in == nil {
		return nil
	}
	payload, _ := json.Marshal(in)
	var out TaskRun
	_ = json.Unmarshal(payload, &out)
	if out.Constraints == nil {
		out.Constraints = make([]string, 0)
	}
	return &out
}

func cloneWorkspaceLease(in *WorkspaceLease) *WorkspaceLease {
	if in == nil {
		return nil
	}
	payload, _ := json.Marshal(in)
	var out WorkspaceLease
	_ = json.Unmarshal(payload, &out)
	return &out
}

func normalizeSeenState(state WorkerSeenState) WorkerSeenState {
	if state.SeenCardIDs == nil {
		state.SeenCardIDs = make(map[string]string)
	}
	if state.SeenApprovalStatus == nil {
		state.SeenApprovalStatus = make(map[string]string)
	}
	if state.SeenChoiceStatus == nil {
		state.SeenChoiceStatus = make(map[string]string)
	}
	if state.SeenExecStatus == nil {
		state.SeenExecStatus = make(map[string]string)
	}
	return state
}

func normalizeSessionMeta(meta SessionMeta) SessionMeta {
	if meta.Kind == "" {
		meta.Kind = SessionKindSingleHost
	}
	if meta.Kind == SessionKindWorkspace && !meta.Visible {
		meta.Visible = true
	}
	if meta.Kind == SessionKindSingleHost && !meta.Visible {
		meta.Visible = true
	}
	if meta.RuntimePreset == "" {
		switch meta.Kind {
		case SessionKindWorkspace:
			meta.RuntimePreset = RuntimePresetWorkspaceFront
		case SessionKindPlanner:
			meta.RuntimePreset = RuntimePresetPlannerInternal
		case SessionKindWorker:
			meta.RuntimePreset = RuntimePresetWorkerInternal
		default:
			meta.RuntimePreset = RuntimePresetSingleHostDefault
		}
	}
	return meta
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
