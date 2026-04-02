package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type ManagerConfig struct {
	Store                 *Store
	WorkspaceRoot         string
	Clock                 func() time.Time
	WorkspaceBootstrapper func(context.Context, string) error
	RelayEventSink        func(context.Context, RelayEvent) error
}

type ManagerOption func(*Manager)

func WithStore(store *Store) ManagerOption {
	return func(m *Manager) {
		m.store = store
	}
}

func WithWorkspaceRoot(root string) ManagerOption {
	return func(m *Manager) {
		m.workspaceRoot = strings.TrimSpace(root)
	}
}

func WithClock(clock func() time.Time) ManagerOption {
	return func(m *Manager) {
		m.clock = clock
	}
}

func WithWorkspaceBootstrapper(fn func(context.Context, string) error) ManagerOption {
	return func(m *Manager) {
		m.workspaceBootstrapper = fn
	}
}

func WithRelayEventSink(fn func(context.Context, RelayEvent) error) ManagerOption {
	return func(m *Manager) {
		m.relayEventSink = fn
	}
}

type StartMissionRequest struct {
	MissionID           string
	WorkspaceSessionID  string
	PlannerSessionID    string
	PlannerThreadID     string
	Title               string
	Summary             string
	ProjectionMode      string
	GlobalActiveBudget  int
	MissionActiveBudget int
}

type Manager struct {
	store                 *Store
	dispatcher            *Dispatcher
	collector             *Collector
	clock                 func() time.Time
	workspaceRoot         string
	workspaceBootstrapper func(context.Context, string) error
	relayEventSink        func(context.Context, RelayEvent) error
}

type WorkerTurnOutcome struct {
	MissionID           string
	WorkspaceSessionID  string
	WorkerSessionID     string
	WorkerHostID        string
	CompletedTaskID     string
	CompletedTaskStatus TaskStatus
	NextTask            *TaskRun
	MissionCompleted    bool
	MissionStatus       MissionStatus
}

type WorkerActivation struct {
	MissionID          string
	WorkspaceSessionID string
	WorkerSessionID    string
	WorkerHostID       string
	ActivatedTaskID    string
}

type SessionFailureOutcome struct {
	MissionID          string
	WorkspaceSessionID string
	SessionID          string
	HostID             string
	Kind               SessionKind
	Reason             string
	FailedTaskIDs      []string
	MissionStatus      MissionStatus
}

type RuntimeRecoveryProbe struct {
	SessionHasThread func(sessionID string) bool
	HostAvailable    func(hostID string) bool
}

type RuntimeRecoveryResult struct {
	Failures []*SessionFailureOutcome
}

func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		store:         NewStore(""),
		clock:         time.Now,
		workspaceRoot: "",
	}
	m.dispatcher = newDispatcher(m.store)
	m.collector = newCollector(m.store)
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	if m.store == nil {
		m.store = NewStore("")
	}
	if m.clock == nil {
		m.clock = time.Now
	}
	if m.dispatcher == nil {
		m.dispatcher = newDispatcher(m.store)
	} else {
		m.dispatcher.store = m.store
	}
	if m.collector == nil {
		m.collector = newCollector(m.store)
	} else {
		m.collector.store = m.store
	}
	return m
}

func NewManagerFromConfig(cfg ManagerConfig) *Manager {
	opts := []ManagerOption{}
	if cfg.Store != nil {
		opts = append(opts, WithStore(cfg.Store))
	}
	if cfg.WorkspaceRoot != "" {
		opts = append(opts, WithWorkspaceRoot(cfg.WorkspaceRoot))
	}
	if cfg.Clock != nil {
		opts = append(opts, WithClock(cfg.Clock))
	}
	if cfg.WorkspaceBootstrapper != nil {
		opts = append(opts, WithWorkspaceBootstrapper(cfg.WorkspaceBootstrapper))
	}
	if cfg.RelayEventSink != nil {
		opts = append(opts, WithRelayEventSink(cfg.RelayEventSink))
	}
	return NewManager(opts...)
}

func (m *Manager) Load() error {
	if m == nil || m.store == nil {
		return errors.New("manager is not ready")
	}
	return m.store.Load()
}

func (m *Manager) Save() error {
	if m == nil || m.store == nil {
		return errors.New("manager is not ready")
	}
	return m.store.Save()
}

func (m *Manager) StartMission(ctx context.Context, req StartMissionRequest) (*Mission, error) {
	_ = ctx
	if m == nil || m.store == nil {
		return nil, errors.New("manager is not ready")
	}
	now := m.nowString()

	missionID := strings.TrimSpace(req.MissionID)
	if missionID == "" {
		missionID = newSessionID("mission")
	}
	workspaceSessionID := strings.TrimSpace(req.WorkspaceSessionID)
	if workspaceSessionID == "" {
		workspaceSessionID = newSessionID("workspace")
	}
	// Legacy planner session IDs are caller-owned; the manager no longer allocates
	// planner sessions or planner leases for new missions.
	plannerSessionID := strings.TrimSpace(req.PlannerSessionID)

	mission := &Mission{
		ID:                  missionID,
		WorkspaceSessionID:  workspaceSessionID,
		PlannerSessionID:    plannerSessionID,
		PlannerThreadID:     strings.TrimSpace(req.PlannerThreadID),
		Title:               strings.TrimSpace(req.Title),
		Summary:             strings.TrimSpace(req.Summary),
		Status:              MissionStatusRunning,
		ProjectionMode:      firstNonEmpty(req.ProjectionMode, "front_projection"),
		CreatedAt:           now,
		UpdatedAt:           now,
		Workers:             make(map[string]*HostWorker),
		Tasks:               make(map[string]*TaskRun),
		Workspaces:          make(map[string]*WorkspaceLease),
		Events:              make([]RelayEvent, 0),
		GlobalActiveBudget:  req.GlobalActiveBudget,
		MissionActiveBudget: req.MissionActiveBudget,
	}
	if mission.GlobalActiveBudget <= 0 {
		mission.GlobalActiveBudget = DefaultGlobalActiveBudget
	}
	if mission.MissionActiveBudget <= 0 {
		mission.MissionActiveBudget = DefaultMissionActiveBudget
	}
	if _, err := m.store.UpsertMission(mission); err != nil {
		return nil, err
	}

	m.store.UpsertSessionMeta(workspaceSessionID, SessionMeta{
		Kind:               SessionKindWorkspace,
		Visible:            true,
		MissionID:          missionID,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      RuntimePresetWorkspaceFront,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	if err := m.Save(); err != nil {
		return nil, err
	}
	mission, ok := m.store.Mission(missionID)
	if !ok {
		return nil, fmt.Errorf("mission %q not found after create", missionID)
	}
	return mission, nil
}

func (m *Manager) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	if m == nil || m.dispatcher == nil {
		return nil, errors.New("manager is not ready")
	}
	result, err := m.dispatcher.Dispatch(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := m.Save(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) CancelMission(ctx context.Context, missionID string) error {
	_ = ctx
	if m == nil || m.dispatcher == nil {
		return errors.New("manager is not ready")
	}
	if err := m.dispatcher.CancelMission(missionID); err != nil {
		return err
	}
	return m.Save()
}

func (m *Manager) CancelTask(ctx context.Context, missionID, taskID string) (*TaskCancelResult, error) {
	_ = ctx
	if m == nil || m.dispatcher == nil {
		return nil, errors.New("manager is not ready")
	}
	result, err := m.dispatcher.CancelTask(missionID, taskID)
	if err != nil {
		return nil, err
	}
	if err := m.Save(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) CancelByWorkspaceSession(ctx context.Context, workspaceSessionID string) error {
	_ = ctx
	if m == nil || m.dispatcher == nil {
		return errors.New("manager is not ready")
	}
	if err := m.dispatcher.CancelByWorkspaceSession(workspaceSessionID); err != nil {
		return err
	}
	return m.Save()
}

func (m *Manager) OnSnapshot(sessionID string, snapshot Snapshot) error {
	if m == nil {
		return errors.New("manager is not ready")
	}
	sessionID = strings.TrimSpace(sessionID)
	if snapshot.SessionID == "" {
		snapshot.SessionID = sessionID
	}
	if snapshot.SessionID == "" {
		return nil
	}
	if err := m.recordCollectorEvent(func(collector *Collector) (bool, error) {
		return collector.OnSnapshot(snapshot.SessionID, snapshot)
	}); err != nil {
		return err
	}
	if m.relayEventSink != nil {
		event := RelayEvent{
			MissionID: snapshot.MissionID,
			SessionID: snapshot.SessionID,
			Type:      EventTypeSnapshot,
			Status:    snapshot.Status,
			Summary:   snapshot.Summary,
			Detail:    snapshot.Detail,
			CreatedAt: m.nowString(),
		}
		if err := m.relayEventSink(context.Background(), event); err != nil {
			return err
		}
	}
	if m.dispatcher != nil {
		if err := m.dispatcher.OnSnapshot(snapshot); err != nil {
			return err
		}
		return m.Save()
	}
	return m.Save()
}

func (m *Manager) RecordTurnPhase(sessionID, phase string) error {
	return m.recordCollectorEvent(func(collector *Collector) (bool, error) {
		return collector.OnTurnPhase(sessionID, phase)
	})
}

func (m *Manager) RecordReply(sessionID, reply string) error {
	return m.recordCollectorEvent(func(collector *Collector) (bool, error) {
		return collector.OnReply(sessionID, reply)
	})
}

func (m *Manager) RecordApprovalRequested(sessionID, approvalID, summary, detail string) error {
	return m.recordCollectorEvent(func(collector *Collector) (bool, error) {
		return collector.OnApprovalRequested(sessionID, approvalID, summary, detail)
	})
}

func (m *Manager) RecordApprovalResolved(sessionID, approvalID, status, summary string) error {
	return m.recordCollectorEvent(func(collector *Collector) (bool, error) {
		return collector.OnApprovalResolved(sessionID, approvalID, status, summary)
	})
}

func (m *Manager) RecordChoiceRequested(sessionID, choiceID, summary string) error {
	return m.recordCollectorEvent(func(collector *Collector) (bool, error) {
		return collector.OnChoiceRequested(sessionID, choiceID, summary)
	})
}

func (m *Manager) RecordChoiceResolved(sessionID, choiceID, summary string) error {
	return m.recordCollectorEvent(func(collector *Collector) (bool, error) {
		return collector.OnChoiceResolved(sessionID, choiceID, summary)
	})
}

func (m *Manager) RecordRemoteExecStarted(sessionID, hostID, cardID, command string) error {
	return m.recordCollectorEvent(func(collector *Collector) (bool, error) {
		return collector.OnRemoteExecStarted(sessionID, hostID, cardID, command)
	})
}

func (m *Manager) RecordRemoteExecFinished(sessionID, hostID, cardID, status, command, detail string) error {
	return m.recordCollectorEvent(func(collector *Collector) (bool, error) {
		return collector.OnRemoteExecFinished(sessionID, hostID, cardID, status, command, detail)
	})
}

func (m *Manager) MissionBySession(sessionID string) (*Mission, bool) {
	if m == nil || m.store == nil {
		return nil, false
	}
	missionID, ok := m.store.MissionIDBySession(sessionID)
	if !ok {
		return nil, false
	}
	return m.store.Mission(missionID)
}

func (m *Manager) MissionByWorkspaceSession(sessionID string) (*Mission, bool) {
	if m == nil || m.store == nil {
		return nil, false
	}
	return m.store.MissionByWorkspaceSession(sessionID)
}

func (m *Manager) Mission(missionID string) (*Mission, bool) {
	if m == nil || m.store == nil {
		return nil, false
	}
	return m.store.Mission(strings.TrimSpace(missionID))
}

func (m *Manager) Missions() []*Mission {
	if m == nil || m.store == nil {
		return nil
	}
	return m.store.Missions()
}

func (m *Manager) ResolveApprovalRoute(approvalID string) (ApprovalRoute, bool) {
	if m == nil || m.store == nil {
		return ApprovalRoute{}, false
	}
	return m.store.ResolveApprovalRoute(approvalID)
}

func (m *Manager) ResolveChoiceRoute(choiceID string) (ChoiceRoute, bool) {
	if m == nil || m.store == nil {
		return ChoiceRoute{}, false
	}
	return m.store.ResolveChoiceRoute(choiceID)
}

func (m *Manager) RegisterApprovalRoute(approvalID, workerSessionID string) error {
	if m == nil || m.store == nil {
		return errors.New("manager is not ready")
	}
	m.store.LinkApprovalToWorker(approvalID, workerSessionID)
	return m.Save()
}

func (m *Manager) RegisterChoiceRoute(choiceID, sessionID string) error {
	if m == nil || m.store == nil {
		return errors.New("manager is not ready")
	}
	m.store.LinkChoiceToSession(choiceID, sessionID)
	return m.Save()
}

func (m *Manager) WorkerTask(sessionID string) (*Mission, *HostWorker, *TaskRun, bool) {
	if m == nil || m.store == nil {
		return nil, nil, nil, false
	}
	mission, ok := m.MissionBySession(sessionID)
	if !ok || mission == nil {
		return nil, nil, nil, false
	}
	for _, worker := range mission.Workers {
		if worker == nil || worker.SessionID != sessionID {
			continue
		}
		return mission, cloneWorker(worker), cloneTask(mission.Tasks[worker.ActiveTaskID]), true
	}
	return mission, nil, nil, false
}

func (m *Manager) MarkWorkerDispatching(sessionID string) error {
	if m == nil || m.store == nil {
		return errors.New("manager is not ready")
	}
	missionID, ok := m.store.MissionIDByWorkerSession(sessionID)
	if !ok {
		return fmt.Errorf("worker session %q is not linked to a mission", sessionID)
	}
	_, err := m.store.UpdateMission(missionID, func(mission *Mission) error {
		var worker *HostWorker
		for _, candidate := range mission.Workers {
			if candidate != nil && candidate.SessionID == sessionID {
				worker = candidate
				break
			}
		}
		if worker == nil {
			return fmt.Errorf("worker session %q is not attached to mission %q", sessionID, missionID)
		}
		activeTaskID := strings.TrimSpace(worker.ActiveTaskID)
		if activeTaskID == "" {
			return nil
		}
		now := m.nowString()
		if task := mission.Tasks[activeTaskID]; task != nil && !isTaskTerminal(task.Status) {
			task.Status = TaskStatusDispatching
			task.UpdatedAt = now
		}
		worker.Status = WorkerStatusDispatching
		worker.IdleSince = ""
		worker.LastSeenAt = now
		worker.UpdatedAt = now
		mission.UpdatedAt = now
		return nil
	})
	if err != nil {
		return err
	}
	return m.Save()
}

func (m *Manager) CompleteWorkerTurn(sessionID string, phase string, reply string) (*WorkerTurnOutcome, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("manager is not ready")
	}
	missionID, ok := m.store.MissionIDByWorkerSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("worker session %q is not linked to a mission", sessionID)
	}
	outcome := &WorkerTurnOutcome{WorkerSessionID: sessionID}
	_, err := m.store.UpdateMission(missionID, func(mission *Mission) error {
		var worker *HostWorker
		for _, candidate := range mission.Workers {
			if candidate != nil && candidate.SessionID == sessionID {
				worker = candidate
				break
			}
		}
		if worker == nil {
			return fmt.Errorf("worker session %q is not attached to mission %q", sessionID, missionID)
		}

		now := m.nowString()
		outcome.MissionID = missionID
		outcome.WorkspaceSessionID = mission.WorkspaceSessionID
		outcome.WorkerHostID = worker.HostID
		outcome.MissionStatus = mission.Status

		completedTaskID := strings.TrimSpace(worker.ActiveTaskID)
		outcome.CompletedTaskID = completedTaskID
		if completedTaskID == "" {
			return nil
		}
		taskStatus := taskStatusFromTurnPhase(phase)
		outcome.CompletedTaskStatus = taskStatus
		if completedTaskID != "" {
			if task := mission.Tasks[completedTaskID]; task != nil {
				task.Status = taskStatus
				task.LastReply = strings.TrimSpace(reply)
				task.UpdatedAt = now
			}
		}

		worker.ActiveTaskID = ""
		worker.LastSeenAt = now
		worker.UpdatedAt = now
		worker.IdleSince = now

		if mission.Status == MissionStatusRunning {
			for len(worker.QueueTaskIDs) > 0 {
				nextTaskID := worker.QueueTaskIDs[0]
				worker.QueueTaskIDs = append([]string(nil), worker.QueueTaskIDs[1:]...)
				nextTask := mission.Tasks[nextTaskID]
				if nextTask == nil || isTaskTerminal(nextTask.Status) {
					continue
				}
				nextTask.Status = TaskStatusReady
				nextTask.UpdatedAt = now
				worker.ActiveTaskID = nextTask.ID
				worker.Status = WorkerStatusDispatching
				outcome.NextTask = cloneTask(nextTask)
				break
			}
		}
		if outcome.NextTask == nil {
			worker.Status = workerTerminalStatus(taskStatus)
		}

		mission.UpdatedAt = now
		if allMissionTasksTerminal(mission) && mission.Status == MissionStatusRunning {
			mission.Status = summarizeMissionTerminalStatus(mission)
			outcome.MissionCompleted = true
		}
		outcome.MissionStatus = mission.Status
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := m.Save(); err != nil {
		return nil, err
	}
	return outcome, nil
}

func (m *Manager) ActivateQueuedWorkers(workspaceSessionID string) ([]WorkerActivation, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("manager is not ready")
	}
	mission, ok := m.store.MissionByWorkspaceSession(strings.TrimSpace(workspaceSessionID))
	if !ok || mission == nil {
		return nil, fmt.Errorf("workspace session %q is not linked to a mission", workspaceSessionID)
	}
	activations := make([]WorkerActivation, 0)
	_, err := m.store.UpdateMission(mission.ID, func(mission *Mission) error {
		if mission.Status != MissionStatusRunning {
			return nil
		}
		activeBudget := mission.MissionActiveBudget
		if activeBudget <= 0 {
			activeBudget = DefaultMissionActiveBudget
		}
		activeWorkers := countActiveWorkers(mission)
		if activeWorkers >= activeBudget {
			return nil
		}

		hostIDs := make([]string, 0, len(mission.Workers))
		for hostID := range mission.Workers {
			hostIDs = append(hostIDs, hostID)
		}
		sort.Strings(hostIDs)

		now := m.nowString()
		for _, hostID := range hostIDs {
			if activeWorkers >= activeBudget {
				break
			}
			worker := mission.Workers[hostID]
			if worker == nil || worker.ActiveTaskID != "" {
				continue
			}
			switch worker.Status {
			case WorkerStatusRunning, WorkerStatusDispatching, WorkerStatusWaiting:
				continue
			}
			for len(worker.QueueTaskIDs) > 0 {
				nextTaskID := strings.TrimSpace(worker.QueueTaskIDs[0])
				worker.QueueTaskIDs = append([]string(nil), worker.QueueTaskIDs[1:]...)
				if nextTaskID == "" {
					continue
				}
				nextTask := mission.Tasks[nextTaskID]
				if nextTask == nil || isTaskTerminal(nextTask.Status) {
					continue
				}
				nextTask.Status = TaskStatusReady
				nextTask.UpdatedAt = now
				worker.ActiveTaskID = nextTask.ID
				worker.Status = WorkerStatusDispatching
				worker.LastSeenAt = now
				worker.UpdatedAt = now
				mission.UpdatedAt = now
				activations = append(activations, WorkerActivation{
					MissionID:          mission.ID,
					WorkspaceSessionID: mission.WorkspaceSessionID,
					WorkerSessionID:    worker.SessionID,
					WorkerHostID:       worker.HostID,
					ActivatedTaskID:    nextTask.ID,
				})
				activeWorkers++
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(activations) == 0 {
		return nil, nil
	}
	if err := m.Save(); err != nil {
		return nil, err
	}
	return activations, nil
}

func (m *Manager) SyncWorkerPhase(sessionID string, phase string) error {
	if m == nil || m.store == nil {
		return errors.New("manager is not ready")
	}
	missionID, ok := m.store.MissionIDByWorkerSession(sessionID)
	if !ok {
		return nil
	}
	_, err := m.store.UpdateMission(missionID, func(mission *Mission) error {
		var worker *HostWorker
		for _, candidate := range mission.Workers {
			if candidate != nil && candidate.SessionID == sessionID {
				worker = candidate
				break
			}
		}
		if worker == nil {
			return nil
		}
		now := m.nowString()
		if task := mission.Tasks[worker.ActiveTaskID]; task != nil && !isTaskTerminal(task.Status) {
			switch strings.TrimSpace(phase) {
			case "waiting_approval":
				task.Status = TaskStatusWaitingApproval
				worker.Status = WorkerStatusWaiting
			case "waiting_input":
				task.Status = TaskStatusWaitingInput
				worker.Status = WorkerStatusWaiting
			default:
				task.Status = TaskStatusRunning
				worker.Status = WorkerStatusRunning
			}
			task.UpdatedAt = now
			worker.IdleSince = ""
		}
		worker.LastSeenAt = now
		worker.UpdatedAt = now
		mission.UpdatedAt = now
		return nil
	})
	if err != nil {
		return err
	}
	return m.Save()
}

func (m *Manager) FailWorkerSession(sessionID, reason string) (*SessionFailureOutcome, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("manager is not ready")
	}
	outcome, changed, err := m.failWorkerSession(sessionID, reason)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := m.Save(); err != nil {
			return nil, err
		}
	}
	return outcome, nil
}

func (m *Manager) FailPlannerSession(sessionID, reason string) (*SessionFailureOutcome, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("manager is not ready")
	}
	outcome, changed, err := m.failPlannerSession(sessionID, reason)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := m.Save(); err != nil {
			return nil, err
		}
	}
	return outcome, nil
}

func (m *Manager) ReconcileSessionRuntime(sessionID, reason string) (*SessionFailureOutcome, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("manager is not ready")
	}
	meta, ok := m.store.SessionMeta(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %q is not linked to orchestrator state", sessionID)
	}
	switch meta.Kind {
	case SessionKindWorker:
		return m.FailWorkerSession(sessionID, reason)
	case SessionKindPlanner:
		return m.FailPlannerSession(sessionID, reason)
	default:
		return nil, fmt.Errorf("session %q kind %q does not support runtime reconcile", sessionID, meta.Kind)
	}
}

func (m *Manager) ReconcileAfterLoad(probe RuntimeRecoveryProbe) (*RuntimeRecoveryResult, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("manager is not ready")
	}
	result := &RuntimeRecoveryResult{
		Failures: make([]*SessionFailureOutcome, 0),
	}
	hostAvailable := probe.HostAvailable
	if hostAvailable == nil {
		hostAvailable = func(string) bool { return true }
	}
	sessionHasThread := probe.SessionHasThread
	if sessionHasThread == nil {
		sessionHasThread = func(string) bool { return false }
	}

	handledOfflineHosts := make(map[string]struct{})
	for _, mission := range m.Missions() {
		if mission == nil || mission.Status != MissionStatusRunning {
			continue
		}
		if mission.PlannerSessionID != "" && len(mission.Tasks) == 0 && !sessionHasThread(mission.PlannerSessionID) {
			outcome, err := m.FailPlannerSession(mission.PlannerSessionID, "legacy planner mission unsupported after planner removal")
			if err != nil {
				return nil, err
			}
			if outcome != nil {
				result.Failures = append(result.Failures, outcome)
			}
		}
		for _, worker := range mission.Workers {
			if worker == nil || isWorkerTerminal(worker.Status) {
				continue
			}
			if _, ok := handledOfflineHosts[worker.HostID]; !ok && !hostAvailable(worker.HostID) {
				outcomes, err := m.MarkHostUnavailable(worker.HostID, "remote host unavailable after restart")
				if err != nil {
					return nil, err
				}
				result.Failures = append(result.Failures, outcomes...)
				handledOfflineHosts[worker.HostID] = struct{}{}
				continue
			}
			if !sessionHasThread(worker.SessionID) {
				outcome, err := m.FailWorkerSession(worker.SessionID, "worker runtime lost after restart")
				if err != nil {
					return nil, err
				}
				if outcome != nil {
					result.Failures = append(result.Failures, outcome)
				}
			}
		}
	}
	return result, nil
}

func (m *Manager) MarkHostUnavailable(hostID, reason string) ([]*SessionFailureOutcome, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("manager is not ready")
	}
	hostID = strings.TrimSpace(hostID)
	if hostID == "" {
		return nil, errors.New("host id is required")
	}
	sessionIDs := m.store.SessionIDsByKind(SessionKindWorker)
	outcomes := make([]*SessionFailureOutcome, 0)
	changedAny := false
	for _, sessionID := range sessionIDs {
		meta, ok := m.store.SessionMeta(sessionID)
		if !ok || strings.TrimSpace(meta.WorkerHostID) != hostID {
			continue
		}
		outcome, changed, err := m.failWorkerSession(sessionID, reason)
		if err != nil {
			return nil, err
		}
		if outcome != nil {
			outcomes = append(outcomes, outcome)
		}
		changedAny = changedAny || changed
	}
	if changedAny {
		if err := m.Save(); err != nil {
			return nil, err
		}
	}
	return outcomes, nil
}

func (m *Manager) FailWorkersByHost(hostID string, reply string) ([]*WorkerTurnOutcome, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("manager is not ready")
	}
	hostID = strings.TrimSpace(hostID)
	reply = strings.TrimSpace(reply)
	if hostID == "" {
		return nil, nil
	}

	missions := m.store.Missions()
	outcomes := make([]*WorkerTurnOutcome, 0)
	changed := false

	for _, mission := range missions {
		if mission == nil {
			continue
		}

		var outcome *WorkerTurnOutcome
		_, err := m.store.UpdateMission(mission.ID, func(current *Mission) error {
			if current == nil || isMissionTerminal(current.Status) {
				return nil
			}
			worker := current.Workers[hostID]
			if worker == nil || isWorkerTerminal(worker.Status) {
				return nil
			}

			now := m.nowString()
			outcome = &WorkerTurnOutcome{
				MissionID:           current.ID,
				WorkspaceSessionID:  current.WorkspaceSessionID,
				WorkerSessionID:     worker.SessionID,
				WorkerHostID:        worker.HostID,
				CompletedTaskStatus: TaskStatusFailed,
			}

			activeTaskID := strings.TrimSpace(worker.ActiveTaskID)
			if activeTaskID != "" {
				outcome.CompletedTaskID = activeTaskID
				if task := current.Tasks[activeTaskID]; task != nil && !isTaskTerminal(task.Status) {
					task.Status = TaskStatusFailed
					task.LastReply = reply
					task.UpdatedAt = now
				}
			}

			for _, taskID := range worker.QueueTaskIDs {
				task := current.Tasks[taskID]
				if task == nil || isTaskTerminal(task.Status) {
					continue
				}
				task.Status = TaskStatusFailed
				task.LastReply = reply
				task.UpdatedAt = now
			}

			worker.ActiveTaskID = ""
			worker.QueueTaskIDs = nil
			worker.Status = WorkerStatusFailed
			worker.LastSeenAt = now
			worker.IdleSince = now
			worker.UpdatedAt = now
			current.UpdatedAt = now

			if allMissionTasksTerminal(current) {
				current.Status = summarizeMissionTerminalStatus(current)
				outcome.MissionCompleted = true
			}
			outcome.MissionStatus = current.Status
			changed = true
			return nil
		})
		if err != nil {
			return nil, err
		}
		if outcome != nil {
			outcomes = append(outcomes, outcome)
		}
	}

	if !changed {
		return outcomes, nil
	}
	if err := m.Save(); err != nil {
		return nil, err
	}
	return outcomes, nil
}

func (m *Manager) ProjectMission(sessionID string) (MissionCardView, bool) {
	mission, ok := m.MissionBySession(sessionID)
	if !ok {
		return MissionCardView{}, false
	}
	return ProjectMissionCard(mission), true
}

func (m *Manager) ProjectPlanSummary(sessionID string) (PlanSummaryView, bool) {
	mission, ok := m.MissionBySession(sessionID)
	if !ok {
		return PlanSummaryView{}, false
	}
	return ProjectPlanSummary(mission), true
}

func (m *Manager) ProjectPlanDetail(sessionID string) (PlanDetailView, bool) {
	mission, ok := m.MissionBySession(sessionID)
	if !ok {
		return PlanDetailView{}, false
	}
	return ProjectPlanDetail(mission), true
}

func (m *Manager) ProjectDispatch(sessionID string, result *DispatchResult) (DispatchSummaryView, bool) {
	mission, ok := m.MissionBySession(sessionID)
	if !ok {
		return DispatchSummaryView{}, false
	}
	return ProjectDispatchSummary(result, mission), true
}

func (m *Manager) ProjectDispatchHost(sessionID, hostID string) (DispatchHostDetailView, bool) {
	mission, ok := m.MissionBySession(sessionID)
	if !ok || mission == nil {
		return DispatchHostDetailView{}, false
	}
	worker := mission.Workers[strings.TrimSpace(hostID)]
	if worker == nil {
		return DispatchHostDetailView{}, false
	}
	task := mission.Tasks[worker.ActiveTaskID]
	if task == nil {
		for _, candidate := range mission.Tasks {
			if candidate != nil && candidate.HostID == worker.HostID {
				if task == nil || candidate.UpdatedAt > task.UpdatedAt {
					task = candidate
				}
			}
		}
	}
	return ProjectDispatchHostDetail(task, worker), true
}

func (m *Manager) ProjectWorkerReadonly(sessionID, hostID string) (WorkerReadonlyDetailView, bool) {
	mission, ok := m.MissionBySession(sessionID)
	if !ok || mission == nil {
		return WorkerReadonlyDetailView{}, false
	}
	worker := mission.Workers[strings.TrimSpace(hostID)]
	if worker == nil {
		return WorkerReadonlyDetailView{}, false
	}
	return ProjectWorkerReadonlyDetail(worker), true
}

func (m *Manager) CreateWorkspaceSnapshot(sessionID string, status string, summary string) Snapshot {
	if m == nil || m.store == nil {
		return Snapshot{SessionID: sessionID, Status: status, Summary: summary, UpdatedAt: time.Now().UTC()}
	}
	meta, _ := m.store.SessionMeta(sessionID)
	return Snapshot{
		SessionID: sessionID,
		Kind:      meta.Kind,
		Visible:   meta.Visible,
		MissionID: meta.MissionID,
		Status:    status,
		Summary:   summary,
		UpdatedAt: m.now(),
	}
}

func (m *Manager) bootstrapWorkspace(ctx context.Context, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if m != nil && m.workspaceBootstrapper != nil {
		return m.workspaceBootstrapper(ctx, path)
	}
	return EnsureWorkspace(path)
}

func (m *Manager) recordCollectorEvent(fn func(*Collector) (bool, error)) error {
	if m == nil || m.collector == nil || fn == nil {
		return nil
	}
	changed, err := fn(m.collector)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return m.Save()
}

func (m *Manager) now() time.Time {
	if m != nil && m.clock != nil {
		return m.clock().UTC()
	}
	return time.Now().UTC()
}

func (m *Manager) nowString() string {
	return m.now().Format(time.RFC3339Nano)
}

func newSessionID(prefix string) string {
	return model.NewID(prefix)
}

func newWorkspaceID(kind string, missionID string, hostID string) string {
	if hostID == "" {
		return fmt.Sprintf("%s:%s", kind, missionID)
	}
	return fmt.Sprintf("%s:%s:%s", kind, missionID, hostID)
}

func newEventID(prefix string) string {
	return model.NewID(prefix)
}

func (m *Manager) failWorkerSession(sessionID, reason string) (*SessionFailureOutcome, bool, error) {
	missionID, ok := m.store.MissionIDByWorkerSession(sessionID)
	if !ok {
		return nil, false, fmt.Errorf("worker session %q is not linked to a mission", sessionID)
	}
	outcome := &SessionFailureOutcome{
		SessionID: sessionID,
		Kind:      SessionKindWorker,
		Reason:    strings.TrimSpace(reason),
	}
	changed := false
	_, err := m.store.UpdateMission(missionID, func(mission *Mission) error {
		var worker *HostWorker
		for _, candidate := range mission.Workers {
			if candidate != nil && candidate.SessionID == sessionID {
				worker = candidate
				break
			}
		}
		if worker == nil {
			return fmt.Errorf("worker session %q is not attached to mission %q", sessionID, missionID)
		}
		outcome.MissionID = missionID
		outcome.WorkspaceSessionID = mission.WorkspaceSessionID
		outcome.HostID = worker.HostID
		outcome.MissionStatus = mission.Status

		if mission.Status == MissionStatusCompleted || mission.Status == MissionStatusCancelled {
			return nil
		}

		now := m.nowString()
		failedTaskIDs := make([]string, 0)
		for _, task := range mission.Tasks {
			if task == nil || task.HostID != worker.HostID || isTaskTerminal(task.Status) {
				continue
			}
			task.Status = TaskStatusFailed
			task.LastError = strings.TrimSpace(reason)
			task.UpdatedAt = now
			failedTaskIDs = append(failedTaskIDs, task.ID)
		}
		if len(failedTaskIDs) == 0 && worker.Status == WorkerStatusFailed && worker.ActiveTaskID == "" && len(worker.QueueTaskIDs) == 0 {
			outcome.FailedTaskIDs = failedTaskIDs
			outcome.MissionStatus = mission.Status
			return nil
		}
		changed = true
		worker.ActiveTaskID = ""
		worker.QueueTaskIDs = nil
		worker.Status = WorkerStatusFailed
		worker.LastSeenAt = now
		worker.IdleSince = now
		worker.UpdatedAt = now
		mission.UpdatedAt = now
		if len(failedTaskIDs) > 0 {
			mission.Events = append(mission.Events, RelayEvent{
				ID:        newEventID("failed"),
				MissionID: mission.ID,
				HostID:    worker.HostID,
				SessionID: sessionID,
				Type:      EventTypeFailed,
				Status:    string(WorkerStatusFailed),
				Summary:   firstNonEmpty(strings.TrimSpace(reason), fmt.Sprintf("host=%s worker failed", worker.HostID)),
				CreatedAt: now,
			})
			if len(mission.Events) > DefaultEventWindowSize {
				mission.Events = append([]RelayEvent(nil), mission.Events[len(mission.Events)-DefaultEventWindowSize:]...)
			}
		}
		if allMissionTasksTerminal(mission) && mission.Status == MissionStatusRunning {
			mission.Status = summarizeMissionTerminalStatus(mission)
		}
		outcome.FailedTaskIDs = append(outcome.FailedTaskIDs, failedTaskIDs...)
		outcome.MissionStatus = mission.Status
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return outcome, changed, nil
}

func (m *Manager) failPlannerSession(sessionID, reason string) (*SessionFailureOutcome, bool, error) {
	missionID, ok := m.store.MissionIDByPlannerSession(sessionID)
	if !ok {
		for _, mission := range m.store.Missions() {
			if mission != nil && strings.TrimSpace(mission.PlannerSessionID) == strings.TrimSpace(sessionID) {
				missionID = mission.ID
				ok = true
				break
			}
		}
		if !ok {
			return nil, false, fmt.Errorf("planner session %q is not linked to a mission", sessionID)
		}
	}
	outcome := &SessionFailureOutcome{
		SessionID: sessionID,
		Kind:      SessionKindPlanner,
		Reason:    strings.TrimSpace(reason),
	}
	changed := false
	_, err := m.store.UpdateMission(missionID, func(mission *Mission) error {
		outcome.MissionID = missionID
		outcome.WorkspaceSessionID = mission.WorkspaceSessionID
		outcome.MissionStatus = mission.Status
		if mission.Status != MissionStatusRunning || len(mission.Tasks) > 0 {
			return nil
		}
		now := m.nowString()
		mission.Status = MissionStatusFailed
		mission.UpdatedAt = now
		mission.Events = append(mission.Events, RelayEvent{
			ID:        newEventID("failed"),
			MissionID: mission.ID,
			SessionID: sessionID,
			Type:      EventTypeFailed,
			Status:    string(MissionStatusFailed),
			Summary:   firstNonEmpty(strings.TrimSpace(reason), "legacy planner mission unsupported after planner removal"),
			CreatedAt: now,
		})
		if len(mission.Events) > DefaultEventWindowSize {
			mission.Events = append([]RelayEvent(nil), mission.Events[len(mission.Events)-DefaultEventWindowSize:]...)
		}
		outcome.MissionStatus = mission.Status
		changed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return outcome, changed, nil
}

func taskStatusFromTurnPhase(phase string) TaskStatus {
	switch strings.TrimSpace(phase) {
	case "completed":
		return TaskStatusCompleted
	case "cancelled", "aborted":
		return TaskStatusCancelled
	default:
		return TaskStatusFailed
	}
}

func workerTerminalStatus(status TaskStatus) WorkerStatus {
	switch status {
	case TaskStatusCompleted:
		return WorkerStatusCompleted
	case TaskStatusCancelled:
		return WorkerStatusCancelled
	default:
		return WorkerStatusFailed
	}
}

func workerStatusForActiveTask(status TaskStatus) WorkerStatus {
	switch status {
	case TaskStatusReady, TaskStatusDispatching:
		return WorkerStatusDispatching
	case TaskStatusWaitingApproval, TaskStatusWaitingInput:
		return WorkerStatusWaiting
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return workerTerminalStatus(status)
	default:
		return WorkerStatusRunning
	}
}

func isWorkerTerminal(status WorkerStatus) bool {
	switch status {
	case WorkerStatusCompleted, WorkerStatusFailed, WorkerStatusCancelled:
		return true
	default:
		return false
	}
}

func isTaskTerminal(status TaskStatus) bool {
	switch status {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

func allMissionTasksTerminal(mission *Mission) bool {
	if mission == nil || len(mission.Tasks) == 0 {
		return false
	}
	for _, task := range mission.Tasks {
		if task == nil {
			continue
		}
		if !isTaskTerminal(task.Status) {
			return false
		}
	}
	return true
}

func summarizeMissionTerminalStatus(mission *Mission) MissionStatus {
	if mission == nil {
		return MissionStatusFailed
	}
	hasFailed := false
	hasCancelled := false
	for _, task := range mission.Tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case TaskStatusFailed:
			hasFailed = true
		case TaskStatusCancelled:
			hasCancelled = true
		}
	}
	switch {
	case hasFailed:
		return MissionStatusFailed
	case hasCancelled:
		return MissionStatusCancelled
	default:
		return MissionStatusCompleted
	}
}

func isMissionTerminal(status MissionStatus) bool {
	switch status {
	case MissionStatusCompleted, MissionStatusFailed, MissionStatusCancelled:
		return true
	default:
		return false
	}
}
