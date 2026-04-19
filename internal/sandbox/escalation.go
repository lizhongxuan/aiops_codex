package sandbox

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// EscalationRequest represents a request to expand sandbox permissions.
type EscalationRequest struct {
	Operation string   `json:"operation"`
	Paths     []string `json:"paths,omitempty"`
	Hosts     []string `json:"hosts,omitempty"`
	Reason    string   `json:"reason"`
}

// EscalationEvent records a sandbox escalation for audit purposes.
type EscalationEvent struct {
	Request   EscalationRequest `json:"request"`
	Approved  bool              `json:"approved"`
	Timestamp time.Time         `json:"timestamp"`
	SessionID string            `json:"session_id"`
}

// EscalationManager handles sandbox escalation requests and audit logging.
type EscalationManager struct {
	mu     sync.Mutex
	events []EscalationEvent
	// ApprovalFunc is called to request user approval. If nil, escalation is denied.
	ApprovalFunc func(ctx context.Context, req EscalationRequest) (bool, error)
}

// NewEscalationManager creates a new EscalationManager.
func NewEscalationManager() *EscalationManager {
	return &EscalationManager{}
}

// RequestEscalation presents an escalation request to the user and temporarily
// expands sandbox permissions for the specific operation on approval.
// All escalation events are logged for audit.
func (em *EscalationManager) RequestEscalation(ctx context.Context, sessionID string, req EscalationRequest) (bool, error) {
	if req.Operation == "" {
		return false, fmt.Errorf("escalation request must specify an operation")
	}

	var approved bool
	var err error

	if em.ApprovalFunc != nil {
		approved, err = em.ApprovalFunc(ctx, req)
		if err != nil {
			// Log denial on error
			em.logEvent(sessionID, req, false)
			return false, fmt.Errorf("escalation approval failed: %w", err)
		}
	}

	em.logEvent(sessionID, req, approved)
	return approved, nil
}

// Events returns all recorded escalation events.
func (em *EscalationManager) Events() []EscalationEvent {
	em.mu.Lock()
	defer em.mu.Unlock()
	out := make([]EscalationEvent, len(em.events))
	copy(out, em.events)
	return out
}

// ClearEvents removes all recorded events.
func (em *EscalationManager) ClearEvents() {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.events = nil
}

func (em *EscalationManager) logEvent(sessionID string, req EscalationRequest, approved bool) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.events = append(em.events, EscalationEvent{
		Request:   req,
		Approved:  approved,
		Timestamp: time.Now(),
		SessionID: sessionID,
	})
}
