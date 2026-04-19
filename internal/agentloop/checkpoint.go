package agentloop

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// IterationCheckpoint captures the state after each loop iteration.
type IterationCheckpoint struct {
	SessionID   string             `json:"session_id"`
	Iteration   int                `json:"iteration"`
	Messages    []bifrost.Message  `json:"messages"`
	Phase       string             `json:"phase"` // "llm_call", "tool_exec", "completed"
	ToolCalls   []bifrost.ToolCall `json:"tool_calls,omitempty"`
	ToolResults []toolResultEntry  `json:"tool_results,omitempty"`
}

type toolResultEntry struct {
	CallID string `json:"call_id"`
	Result string `json:"result"`
}

// CheckpointStore manages iteration-level checkpoints for crash recovery.
type CheckpointStore struct {
	mu      sync.Mutex
	baseDir string
}

// NewCheckpointStore creates a checkpoint store. If baseDir is empty, checkpointing is disabled.
func NewCheckpointStore(baseDir string) *CheckpointStore {
	return &CheckpointStore{baseDir: baseDir}
}

// Save persists a checkpoint to disk. Returns silently on error (fail-open).
func (cs *CheckpointStore) Save(cp IterationCheckpoint) {
	if cs.baseDir == "" {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()

	dir := filepath.Join(cs.baseDir, cp.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[checkpoint] mkdir failed: %v", err)
		return
	}

	path := filepath.Join(dir, fmt.Sprintf("iter-%03d.json", cp.Iteration))
	data, err := json.Marshal(cp)
	if err != nil {
		log.Printf("[checkpoint] marshal failed: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("[checkpoint] write failed: %v", err)
	}
}

// LoadLatest loads the most recent checkpoint for a session. Returns nil if none found.
func (cs *CheckpointStore) LoadLatest(sessionID string) *IterationCheckpoint {
	if cs.baseDir == "" {
		return nil
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()

	dir := filepath.Join(cs.baseDir, sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var latest *IterationCheckpoint
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cp IterationCheckpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			continue
		}
		if latest == nil || cp.Iteration > latest.Iteration {
			latest = &cp
		}
	}
	return latest
}

// Clear removes all checkpoints for a session (called when turn completes successfully).
func (cs *CheckpointStore) Clear(sessionID string) {
	if cs.baseDir == "" {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()

	dir := filepath.Join(cs.baseDir, sessionID)
	os.RemoveAll(dir)
}
