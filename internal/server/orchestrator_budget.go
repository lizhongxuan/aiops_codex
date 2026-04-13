package server

import (
	"context"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

const (
	orchestratorThreadCreateRateLimit = 4
	orchestratorTurnStartRateLimit    = 8
	orchestratorPendingRequestBudget  = 24
	orchestratorRateWindow            = time.Second
	orchestratorBudgetPollInterval    = 150 * time.Millisecond
)

func (a *App) acquireOrchestratorPermit(ctx context.Context, missionID, sessionID string, globalBudget, missionBudget int, needThread bool) error {
	if globalBudget <= 0 {
		globalBudget = 32
	}
	if missionBudget <= 0 {
		missionBudget = 8
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if a.tryReserveOrchestratorPermit(missionID, sessionID, globalBudget, missionBudget, needThread) {
			return nil
		}
		timer := time.NewTimer(orchestratorBudgetPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (a *App) tryReserveOrchestratorPermit(missionID, sessionID string, globalBudget, missionBudget int, needThread bool) bool {
	a.orchestratorMu.Lock()
	defer a.orchestratorMu.Unlock()

	now := time.Now().UTC()
	a.threadStarts = pruneStartTimes(a.threadStarts, now)
	a.turnStarts = pruneStartTimes(a.turnStarts, now)

	a.turnMu.Lock()
	pendingTurns := len(a.turnCancels)
	a.turnMu.Unlock()
	if pendingTurns >= orchestratorPendingRequestBudget {
		return false
	}
	if needThread && len(a.threadStarts) >= orchestratorThreadCreateRateLimit {
		return false
	}
	if len(a.turnStarts) >= orchestratorTurnStartRateLimit {
		return false
	}

	globalActive, missionActive := a.countOrchestratorActiveLocked(missionID, sessionID)
	if globalActive >= globalBudget || missionActive >= missionBudget {
		return false
	}

	a.orchestratorJobs[sessionID] = missionID
	if needThread {
		a.threadStarts = append(a.threadStarts, now)
	}
	a.turnStarts = append(a.turnStarts, now)
	return true
}

func (a *App) releaseOrchestratorPermit(sessionID string) {
	a.orchestratorMu.Lock()
	defer a.orchestratorMu.Unlock()
	delete(a.orchestratorJobs, sessionID)
}

func (a *App) countOrchestratorActiveLocked(missionID, excludeSessionID string) (global int, mission int) {
	for _, sessionID := range a.store.SessionIDs() {
		if sessionID == excludeSessionID {
			continue
		}
		session := a.store.Session(sessionID)
		if session == nil || !session.Runtime.Turn.Active {
			continue
		}
		switch strings.TrimSpace(session.Runtime.Turn.Phase) {
		case "", "completed", "failed", "aborted", "waiting_approval", "waiting_input":
			continue
		}
		meta := a.sessionMeta(sessionID)
		if meta.Kind != model.SessionKindWorkspace && meta.Kind != model.SessionKindWorker {
			continue
		}
		global++
		if meta.MissionID == missionID {
			mission++
		}
	}
	for sessionID, reservedMissionID := range a.orchestratorJobs {
		if sessionID == excludeSessionID {
			continue
		}
		global++
		if reservedMissionID == missionID {
			mission++
		}
	}
	return global, mission
}

func pruneStartTimes(items []time.Time, now time.Time) []time.Time {
	if len(items) == 0 {
		return nil
	}
	cutoff := now.Add(-orchestratorRateWindow)
	out := items[:0]
	for _, item := range items {
		if item.After(cutoff) {
			out = append(out, item)
		}
	}
	return out
}
