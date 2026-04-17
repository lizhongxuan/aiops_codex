package agentloop

import (
	"fmt"
	"strings"
)

type IncidentMode string

const (
	IncidentModeAnalysis IncidentMode = "analysis"
	IncidentModeExecute  IncidentMode = "execute"
)

type IncidentStage string

const (
	IncidentStageUnderstanding       IncidentStage = "understanding"
	IncidentStagePlanning            IncidentStage = "planning"
	IncidentStageCollectingEvidence  IncidentStage = "collecting_evidence"
	IncidentStageAnalyzing           IncidentStage = "analyzing"
	IncidentStageWaitingPlanApproval IncidentStage = "waiting_plan_approval"
	IncidentStageExecuting           IncidentStage = "executing"
	IncidentStageWaitingActionReview IncidentStage = "waiting_action_approval"
	IncidentStageVerifying           IncidentStage = "verifying"
	IncidentStageRollbackSuggested   IncidentStage = "rollback_suggested"
	IncidentStageCompleted           IncidentStage = "completed"
	IncidentStageFailed              IncidentStage = "failed"
	IncidentStageCanceled            IncidentStage = "canceled"
)

var incidentAllowedStagesByMode = map[IncidentMode]map[IncidentStage]struct{}{
	IncidentModeAnalysis: {
		IncidentStageUnderstanding:       {},
		IncidentStagePlanning:            {},
		IncidentStageCollectingEvidence:  {},
		IncidentStageAnalyzing:           {},
		IncidentStageWaitingPlanApproval: {},
		IncidentStageCompleted:           {},
		IncidentStageFailed:              {},
		IncidentStageCanceled:            {},
	},
	IncidentModeExecute: {
		IncidentStageExecuting:           {},
		IncidentStageWaitingActionReview: {},
		IncidentStageVerifying:           {},
		IncidentStageRollbackSuggested:   {},
		IncidentStageCompleted:           {},
		IncidentStageFailed:              {},
		IncidentStageCanceled:            {},
	},
}

var incidentAllowedModeTransitions = map[IncidentMode]map[IncidentMode]struct{}{
	IncidentModeAnalysis: {
		IncidentModeAnalysis: {},
		IncidentModeExecute:  {},
	},
	IncidentModeExecute: {
		IncidentModeAnalysis: {},
		IncidentModeExecute:  {},
	},
}

var incidentAllowedStageTransitions = map[IncidentStage]map[IncidentStage]struct{}{
	IncidentStageUnderstanding: {
		IncidentStageUnderstanding:       {},
		IncidentStageCollectingEvidence:  {},
		IncidentStageAnalyzing:           {},
		IncidentStagePlanning:            {},
		IncidentStageWaitingPlanApproval: {},
		IncidentStageExecuting:           {},
		IncidentStageCompleted:           {},
		IncidentStageFailed:              {},
		IncidentStageCanceled:            {},
	},
	IncidentStageCollectingEvidence: {
		IncidentStageCollectingEvidence:  {},
		IncidentStageAnalyzing:           {},
		IncidentStagePlanning:            {},
		IncidentStageWaitingPlanApproval: {},
		IncidentStageCompleted:           {},
		IncidentStageFailed:              {},
		IncidentStageCanceled:            {},
	},
	IncidentStageAnalyzing: {
		IncidentStageCollectingEvidence:  {},
		IncidentStageAnalyzing:           {},
		IncidentStagePlanning:            {},
		IncidentStageWaitingPlanApproval: {},
		IncidentStageCompleted:           {},
		IncidentStageFailed:              {},
		IncidentStageCanceled:            {},
	},
	IncidentStagePlanning: {
		IncidentStagePlanning:            {},
		IncidentStageCollectingEvidence:  {},
		IncidentStageAnalyzing:           {},
		IncidentStageWaitingPlanApproval: {},
		IncidentStageCompleted:           {},
		IncidentStageFailed:              {},
		IncidentStageCanceled:            {},
	},
	IncidentStageWaitingPlanApproval: {
		IncidentStagePlanning:            {},
		IncidentStageWaitingPlanApproval: {},
		IncidentStageExecuting:           {},
		IncidentStageCompleted:           {},
		IncidentStageFailed:              {},
		IncidentStageCanceled:            {},
	},
	IncidentStageExecuting: {
		IncidentStageExecuting:           {},
		IncidentStageUnderstanding:       {},
		IncidentStageAnalyzing:           {},
		IncidentStageWaitingActionReview: {},
		IncidentStageVerifying:           {},
		IncidentStageRollbackSuggested:   {},
		IncidentStageCompleted:           {},
		IncidentStageFailed:              {},
		IncidentStageCanceled:            {},
	},
	IncidentStageWaitingActionReview: {
		IncidentStageWaitingActionReview: {},
		IncidentStageExecuting:           {},
		IncidentStagePlanning:            {},
		IncidentStageAnalyzing:           {},
		IncidentStageVerifying:           {},
		IncidentStageRollbackSuggested:   {},
		IncidentStageCompleted:           {},
		IncidentStageFailed:              {},
		IncidentStageCanceled:            {},
	},
	IncidentStageVerifying: {
		IncidentStageVerifying:         {},
		IncidentStageRollbackSuggested: {},
		IncidentStageExecuting:         {},
		IncidentStageCompleted:         {},
		IncidentStageFailed:            {},
		IncidentStageCanceled:          {},
	},
	IncidentStageRollbackSuggested: {
		IncidentStageRollbackSuggested: {},
		IncidentStagePlanning:          {},
		IncidentStageExecuting:         {},
		IncidentStageCompleted:         {},
		IncidentStageFailed:            {},
		IncidentStageCanceled:          {},
	},
	IncidentStageCompleted: {
		IncidentStageCompleted:          {},
		IncidentStageUnderstanding:      {},
		IncidentStageCollectingEvidence: {},
		IncidentStageAnalyzing:          {},
		IncidentStagePlanning:           {},
	},
	IncidentStageFailed: {
		IncidentStageFailed:             {},
		IncidentStageUnderstanding:      {},
		IncidentStageCollectingEvidence: {},
		IncidentStageAnalyzing:          {},
		IncidentStagePlanning:           {},
	},
	IncidentStageCanceled: {
		IncidentStageCanceled:           {},
		IncidentStageUnderstanding:      {},
		IncidentStageCollectingEvidence: {},
		IncidentStageAnalyzing:          {},
		IncidentStagePlanning:           {},
	},
}

func NormalizeIncidentMode(value string) IncidentMode {
	switch strings.TrimSpace(value) {
	case string(IncidentModeExecute):
		return IncidentModeExecute
	default:
		return IncidentModeAnalysis
	}
}

func NormalizeIncidentStage(value string) IncidentStage {
	switch strings.TrimSpace(value) {
	case string(IncidentStagePlanning):
		return IncidentStagePlanning
	case string(IncidentStageCollectingEvidence):
		return IncidentStageCollectingEvidence
	case string(IncidentStageAnalyzing):
		return IncidentStageAnalyzing
	case string(IncidentStageWaitingPlanApproval):
		return IncidentStageWaitingPlanApproval
	case string(IncidentStageExecuting):
		return IncidentStageExecuting
	case string(IncidentStageWaitingActionReview):
		return IncidentStageWaitingActionReview
	case string(IncidentStageVerifying):
		return IncidentStageVerifying
	case string(IncidentStageRollbackSuggested):
		return IncidentStageRollbackSuggested
	case string(IncidentStageCompleted):
		return IncidentStageCompleted
	case string(IncidentStageFailed):
		return IncidentStageFailed
	case string(IncidentStageCanceled):
		return IncidentStageCanceled
	default:
		return IncidentStageUnderstanding
	}
}

func CanTransitionIncidentMode(from, to IncidentMode) bool {
	if to == "" {
		return false
	}
	if from == "" {
		return true
	}
	allowed, ok := incidentAllowedModeTransitions[from]
	if !ok {
		return false
	}
	_, ok = allowed[to]
	return ok
}

func IncidentStageAllowedForMode(mode IncidentMode, stage IncidentStage) bool {
	if mode == "" || stage == "" {
		return false
	}
	allowed, ok := incidentAllowedStagesByMode[mode]
	if !ok {
		return false
	}
	_, ok = allowed[stage]
	return ok
}

func CanTransitionIncidentStage(from, to IncidentStage) bool {
	if to == "" {
		return false
	}
	if from == "" {
		return true
	}
	allowed, ok := incidentAllowedStageTransitions[from]
	if !ok {
		return false
	}
	_, ok = allowed[to]
	return ok
}

func ValidateIncidentTransition(prevMode, nextMode, prevStage, nextStage string) error {
	rawNextStage := strings.TrimSpace(nextStage)
	if rawNextStage == "" {
		return fmt.Errorf("incident transition missing target stage")
	}

	normalizedNextMode := NormalizeIncidentMode(nextMode)
	normalizedNextStage := NormalizeIncidentStage(nextStage)
	if !IncidentStageAllowedForMode(normalizedNextMode, normalizedNextStage) {
		return fmt.Errorf("incident stage %q is not allowed in mode %q", normalizedNextStage, normalizedNextMode)
	}

	if strings.TrimSpace(prevStage) == "" && strings.TrimSpace(prevMode) == "" {
		return nil
	}

	normalizedPrevMode := NormalizeIncidentMode(prevMode)
	if !CanTransitionIncidentMode(normalizedPrevMode, normalizedNextMode) {
		return fmt.Errorf("incident mode transition %q -> %q is not allowed", normalizedPrevMode, normalizedNextMode)
	}

	normalizedPrevStage := NormalizeIncidentStage(prevStage)
	if !CanTransitionIncidentStage(normalizedPrevStage, normalizedNextStage) {
		return fmt.Errorf("incident stage transition %q -> %q is not allowed", normalizedPrevStage, normalizedNextStage)
	}
	return nil
}
