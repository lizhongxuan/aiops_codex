package server

import (
	"strings"
	"sync"
	"time"
)

const (
	// maxCardOutputInSnapshot is the max chars of Card.Output included in snapshots.
	// Larger outputs are truncated with an evidence reference.
	maxCardOutputInSnapshot = 2000

	// snapshotThrottleInterval is the minimum interval between snapshot broadcasts.
	snapshotThrottleInterval = 100 * time.Millisecond
)

// truncateCardOutputForSnapshot truncates a card's output for snapshot transmission.
// Returns the truncated output and whether truncation occurred.
func truncateCardOutputForSnapshot(output string, evidenceID string) (string, bool) {
	if len(output) <= maxCardOutputInSnapshot {
		return output, false
	}

	// Keep first portion and add truncation notice
	truncated := output[:maxCardOutputInSnapshot]
	if evidenceID != "" {
		truncated += "\n\n[... output truncated, full content in evidence " + evidenceID + " ...]"
	} else {
		truncated += "\n\n[... output truncated ...]"
	}
	return truncated, true
}

// snapshotThrottler manages throttled snapshot broadcasts per session.
type snapshotThrottler struct {
	mu       sync.Mutex
	pending  map[string]bool
	timers   map[string]*time.Timer
	interval time.Duration
}

// newSnapshotThrottler creates a new throttler with the given interval.
func newSnapshotThrottler(interval time.Duration) *snapshotThrottler {
	return &snapshotThrottler{
		pending:  make(map[string]bool),
		timers:   make(map[string]*time.Timer),
		interval: interval,
	}
}


// schedule schedules a throttled broadcast for the given session.
// If a broadcast is already pending, it's a no-op.
// The callback will be called after the throttle interval.
func (st *snapshotThrottler) schedule(sessionID string, broadcast func(string)) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.pending[sessionID] {
		return // already scheduled
	}

	st.pending[sessionID] = true
	timer := time.AfterFunc(st.interval, func() {
		st.mu.Lock()
		delete(st.pending, sessionID)
		delete(st.timers, sessionID)
		st.mu.Unlock()

		broadcast(sessionID)
	})
	st.timers[sessionID] = timer
}

// flush immediately broadcasts for the given session if pending.
func (st *snapshotThrottler) flush(sessionID string) {
	st.mu.Lock()
	timer, ok := st.timers[sessionID]
	if ok {
		timer.Stop()
		delete(st.pending, sessionID)
		delete(st.timers, sessionID)
	}
	st.mu.Unlock()
}

// isEvidenceOnlyOutput checks if an output should be stored as evidence only,
// not included in the snapshot.
func isEvidenceOnlyOutput(output string) bool {
	return len(output) > maxCardOutputInSnapshot
}

// extractEvidenceIDFromOutput tries to find an evidence ID in output text.
func extractEvidenceIDFromOutput(output string) string {
	const marker = "evidence "
	idx := strings.Index(output, marker)
	if idx == -1 {
		return ""
	}
	start := idx + len(marker)
	end := start
	for end < len(output) && output[end] != ' ' && output[end] != '\n' && output[end] != ']' && output[end] != ')' {
		end++
	}
	if end > start {
		return output[start:end]
	}
	return ""
}
