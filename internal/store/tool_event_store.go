package store

import "sync"

const defaultToolEventStoreLimit = 256

// ToolEventRecord stores a projected copy of a tool lifecycle event for replay
// and debugging flows.
type ToolEventRecord struct {
	Sequence       uint64         `json:"sequence"`
	EventID        string         `json:"eventId"`
	InvocationID   string         `json:"invocationId,omitempty"`
	Type           string         `json:"type"`
	SessionID      string         `json:"sessionId"`
	ToolName       string         `json:"toolName,omitempty"`
	HostID         string         `json:"hostId,omitempty"`
	CallID         string         `json:"callId,omitempty"`
	CardID         string         `json:"cardId,omitempty"`
	ApprovalID     string         `json:"approvalId,omitempty"`
	Phase          string         `json:"phase,omitempty"`
	Label          string         `json:"label,omitempty"`
	Message        string         `json:"message,omitempty"`
	Error          string         `json:"error,omitempty"`
	ActivityKind   string         `json:"activityKind,omitempty"`
	ActivityTarget string         `json:"activityTarget,omitempty"`
	ActivityQuery  string         `json:"activityQuery,omitempty"`
	CreatedAt      string         `json:"createdAt,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// ToolEventStore keeps an in-memory ring buffer of recent tool lifecycle
// events. It is intentionally session-query oriented so timeline/debug APIs can
// replay the exact event order seen by projections.
type ToolEventStore struct {
	mu           sync.RWMutex
	limit        int
	nextSequence uint64
	start        int
	count        int
	events       []ToolEventRecord
}

func NewToolEventStore(limit int) *ToolEventStore {
	if limit <= 0 {
		limit = defaultToolEventStoreLimit
	}
	return &ToolEventStore{
		limit:  limit,
		events: make([]ToolEventRecord, limit),
	}
}

func (s *ToolEventStore) Append(record ToolEventRecord) ToolEventRecord {
	if s == nil {
		return cloneToolEventRecord(record)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextSequence++
	stored := cloneToolEventRecord(record)
	stored.Sequence = s.nextSequence

	index := 0
	if s.count < s.limit {
		index = (s.start + s.count) % s.limit
		s.count++
	} else {
		index = s.start
		s.start = (s.start + 1) % s.limit
	}
	s.events[index] = stored
	return cloneToolEventRecord(stored)
}

func (s *ToolEventStore) SessionEvents(sessionID string) []ToolEventRecord {
	if s == nil || sessionID == "" {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ToolEventRecord, 0, s.count)
	for i := 0; i < s.count; i++ {
		index := (s.start + i) % s.limit
		record := s.events[index]
		if record.SessionID != sessionID {
			continue
		}
		out = append(out, cloneToolEventRecord(record))
	}
	return out
}

func cloneToolEventRecord(record ToolEventRecord) ToolEventRecord {
	if record.Payload != nil {
		payload := make(map[string]any, len(record.Payload))
		for key, value := range record.Payload {
			payload[key] = value
		}
		record.Payload = payload
	}
	if record.Metadata != nil {
		metadata := make(map[string]any, len(record.Metadata))
		for key, value := range record.Metadata {
			metadata[key] = value
		}
		record.Metadata = metadata
	}
	return record
}
