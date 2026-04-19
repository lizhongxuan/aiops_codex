package memory

import (
	"context"
	"fmt"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// MemorySystem orchestrates the full memory pipeline (Phase 1 → Phase 2).
type MemorySystem struct {
	gateway *bifrost.Gateway
	storage *Storage
	lock    *ConsolidationLock
}

// NewMemorySystem creates a new MemorySystem with the given gateway and storage directory.
func NewMemorySystem(gateway *bifrost.Gateway, storageDir string) *MemorySystem {
	return &MemorySystem{
		gateway: gateway,
		storage: NewStorage(storageDir),
		lock:    NewConsolidationLock(),
	}
}

// StartMemoryPipeline runs the full Phase 1 → Phase 2 pipeline at session start.
// It extracts raw memories from past rollouts, then consolidates them.
func (ms *MemorySystem) StartMemoryPipeline(ctx context.Context, rollouts [][]MemoryTrace) error {
	// Phase 1: Extract raw memories
	config := DefaultPhase1Config()
	rawMemories, err := ExtractPhase1(ctx, ms.gateway, config, rollouts)
	if err != nil {
		return fmt.Errorf("memory/system: phase1: %w", err)
	}

	// Persist raw memories
	if err := ms.storage.SaveRawMemories(rawMemories); err != nil {
		return fmt.Errorf("memory/system: save raw: %w", err)
	}

	// Phase 2: Consolidate
	if !ms.lock.Acquire("pipeline") {
		return fmt.Errorf("memory/system: consolidation lock held by %s", ms.lock.Holder())
	}
	defer ms.lock.Release("pipeline")

	consolidated, err := ConsolidatePhase2(ctx, ms.gateway, rawMemories)
	if err != nil {
		return fmt.Errorf("memory/system: phase2: %w", err)
	}

	// Persist consolidated memory
	if err := ms.storage.SaveConsolidated(consolidated); err != nil {
		return fmt.Errorf("memory/system: save consolidated: %w", err)
	}

	return nil
}

// InjectMemoryContext returns the consolidated memory summary for injection into a new session.
func (ms *MemorySystem) InjectMemoryContext() (string, error) {
	consolidated, err := ms.storage.LoadConsolidated()
	if err != nil {
		return "", fmt.Errorf("memory/system: load consolidated: %w", err)
	}
	if consolidated == nil {
		return "", nil
	}
	return consolidated.Summary, nil
}

// Storage returns the underlying storage instance.
func (ms *MemorySystem) Storage() *Storage {
	return ms.storage
}

// Lock returns the underlying consolidation lock.
func (ms *MemorySystem) Lock() *ConsolidationLock {
	return ms.lock
}
