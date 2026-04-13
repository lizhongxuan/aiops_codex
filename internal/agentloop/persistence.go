package agentloop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ---------- Persisted types ----------

// PersistedMessage is the on-disk representation of a bifrost.Message.
type PersistedMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCalls  []bifrost.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Timestamp  time.Time   `json:"timestamp"`
}

// PersistedSession is the on-disk representation of a Session.
type PersistedSession struct {
	ID            string             `json:"id"`
	Model         string             `json:"model"`
	SystemPrompt  string             `json:"system_prompt"`
	EnabledTools  []string           `json:"enabled_tools"`
	MaxIterations int                `json:"max_iterations"`
	ContextWindow int                `json:"context_window"`
	Messages      []PersistedMessage `json:"messages"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
	TurnCount     int                `json:"turn_count"`
	Metadata      map[string]string  `json:"metadata,omitempty"`
}

// PersistedSessionSummary is a lightweight listing entry.
type PersistedSessionSummary struct {
	ID        string    `json:"id"`
	Model     string    `json:"model"`
	TurnCount int       `json:"turn_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Preview   string    `json:"preview,omitempty"`
}

// ---------- SessionStore ----------

// SessionStore handles persistence of agent loop sessions to disk.
// It stores each session as a JSON file in a configurable directory.
type SessionStore struct {
	mu      sync.Mutex
	rootDir string
}

// NewSessionStore creates a new SessionStore rooted at the given directory.
func NewSessionStore(rootDir string) *SessionStore {
	return &SessionStore{rootDir: rootDir}
}

func (s *SessionStore) sessionsDir() string {
	return filepath.Join(s.rootDir, "sessions")
}

func (s *SessionStore) sessionPath(id string) string {
	safe := strings.ReplaceAll(id, "/", "_")
	safe = strings.ReplaceAll(safe, "..", "_")
	return filepath.Join(s.sessionsDir(), safe+".json")
}

// Save persists a Session to disk.
func (s *SessionStore) Save(session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.sessionsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}

	msgs := session.ContextManager().Messages()
	persisted := make([]PersistedMessage, len(msgs))
	for i, m := range msgs {
		persisted[i] = PersistedMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
			Timestamp:  time.Now(),
		}
	}

	ps := PersistedSession{
		ID:            session.ID,
		Model:         session.Model(),
		SystemPrompt:  session.SystemPrompt(),
		EnabledTools:  session.EnabledTools(),
		MaxIterations: session.MaxIterations(),
		ContextWindow: session.ctxMgr.contextWindow,
		Messages:      persisted,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		TurnCount:     countUserMessages(msgs),
	}

	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return os.WriteFile(s.sessionPath(session.ID), data, 0o644)
}

// Load restores a Session from disk.
func (s *SessionStore) Load(id string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.sessionPath(id))
	if err != nil {
		return nil, fmt.Errorf("read session %s: %w", id, err)
	}

	var ps PersistedSession
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("unmarshal session %s: %w", id, err)
	}

	spec := SessionSpec{
		Model:         ps.Model,
		MaxIterations: ps.MaxIterations,
		ContextWindow: ps.ContextWindow,
		DynamicTools:  ps.EnabledTools,
	}
	session := NewSession(ps.ID, spec)
	// Override the system prompt with the persisted one.
	session.systemPrompt = ps.SystemPrompt

	// Restore messages.
	for _, pm := range ps.Messages {
		session.ctxMgr.Append(bifrost.Message{
			Role:       pm.Role,
			Content:    pm.Content,
			ToolCalls:  pm.ToolCalls,
			ToolCallID: pm.ToolCallID,
		})
	}

	return session, nil
}

// List returns summaries of all persisted sessions, sorted by UpdatedAt desc.
func (s *SessionStore) List() ([]PersistedSessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.sessionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var summaries []PersistedSessionSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var ps PersistedSession
		if err := json.Unmarshal(data, &ps); err != nil {
			continue
		}
		preview := ""
		for _, m := range ps.Messages {
			if m.Role == "user" {
				if s, ok := m.Content.(string); ok && s != "" {
					preview = s
					if len(preview) > 100 {
						preview = preview[:100] + "..."
					}
				}
			}
		}
		summaries = append(summaries, PersistedSessionSummary{
			ID:        ps.ID,
			Model:     ps.Model,
			TurnCount: ps.TurnCount,
			CreatedAt: ps.CreatedAt,
			UpdatedAt: ps.UpdatedAt,
			Preview:   preview,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

// Delete removes a persisted session.
func (s *SessionStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(s.sessionPath(id))
}

// Fork creates a copy of a session with a new ID, preserving the conversation
// history up to the current point.
func (s *SessionStore) Fork(sourceID, newID string) (*Session, error) {
	source, err := s.Load(sourceID)
	if err != nil {
		return nil, fmt.Errorf("fork source: %w", err)
	}

	// Create a new session with the same spec but new ID.
	forked := &Session{
		ID:            newID,
		ctxMgr:        NewContextManager(source.ctxMgr.contextWindow),
		model:         source.model,
		systemPrompt:  source.systemPrompt,
		enabledTools:  append([]string(nil), source.enabledTools...),
		maxIterations: source.maxIterations,
		approvalCh:    make(chan ApprovalDecision, 1),
	}

	// Copy messages.
	for _, m := range source.ContextManager().Messages() {
		forked.ctxMgr.Append(m)
	}

	// Persist the fork.
	if err := s.Save(forked); err != nil {
		return nil, fmt.Errorf("save fork: %w", err)
	}
	return forked, nil
}

func countUserMessages(msgs []bifrost.Message) int {
	count := 0
	for _, m := range msgs {
		if m.Role == "user" {
			count++
		}
	}
	return count
}
