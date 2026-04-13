package guardian

import (
	"sync"
)

// SessionConfig holds configuration for a guardian review session.
type SessionConfig struct {
	// NetworkProxy is the proxy URL inherited from the parent session.
	NetworkProxy string
	// AllowedHosts is the allowlist of hosts the session can access.
	AllowedHosts []string
	// ParentSessionID links back to the originating agent session.
	ParentSessionID string
}

// reviewSession is an internal session used for guardian model calls.
type reviewSession struct {
	ID     string
	Config SessionConfig
}

// ReviewSessionManager manages guardian review sessions with thread-safe access.
type ReviewSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*reviewSession
	counter  int
}

// NewReviewSessionManager creates a new ReviewSessionManager.
func NewReviewSessionManager() *ReviewSessionManager {
	return &ReviewSessionManager{
		sessions: make(map[string]*reviewSession),
	}
}

// CreateSession creates a new review session that clones the parent config,
// inheriting network proxy and allowlist settings.
func (m *ReviewSessionManager) CreateSession(parentConfig SessionConfig) *reviewSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.counter++
	id := generateSessionID(m.counter, parentConfig.ParentSessionID)

	// Clone allowlist to avoid shared mutation.
	allowedHosts := make([]string, len(parentConfig.AllowedHosts))
	copy(allowedHosts, parentConfig.AllowedHosts)

	session := &reviewSession{
		ID: id,
		Config: SessionConfig{
			NetworkProxy:    parentConfig.NetworkProxy,
			AllowedHosts:    allowedHosts,
			ParentSessionID: parentConfig.ParentSessionID,
		},
	}

	m.sessions[id] = session
	return session
}

// GetSession retrieves a review session by ID. Returns nil if not found.
func (m *ReviewSessionManager) GetSession(id string) *reviewSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// CleanupSession releases resources for the given review session.
func (m *ReviewSessionManager) CleanupSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// ActiveCount returns the number of active review sessions.
func (m *ReviewSessionManager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// generateSessionID creates a unique session ID from counter and parent.
func generateSessionID(counter int, parentID string) string {
	if parentID == "" {
		parentID = "orphan"
	}
	return parentID + "-guardian-" + itoa(counter)
}

// itoa is a simple int-to-string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
