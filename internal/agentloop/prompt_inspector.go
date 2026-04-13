package agentloop

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// PromptState represents the current state of a session's prompt.
type PromptState struct {
	SystemPrompt string                   `json:"system_prompt"`
	Messages     []bifrost.Message        `json:"messages"`
	Tools        []bifrost.ToolDefinition `json:"tools"`
	TokenCount   int                      `json:"token_count"`
}

// PromptInspector provides prompt debugging and inspection capabilities.
type PromptInspector struct {
	debugMode bool
	mu        sync.RWMutex
	logs      []PromptLog
}

// PromptLog records a logged prompt request.
type PromptLog struct {
	Model      string            `json:"model"`
	Messages   []bifrost.Message `json:"messages"`
	TokenCount int               `json:"token_count"`
}

// NewPromptInspector creates a new PromptInspector.
func NewPromptInspector(debugMode bool) *PromptInspector {
	return &PromptInspector{
		debugMode: debugMode,
	}
}

// SetDebugMode enables or disables debug mode.
func (p *PromptInspector) SetDebugMode(enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.debugMode = enabled
}

// IsDebugMode returns whether debug mode is enabled.
func (p *PromptInspector) IsDebugMode() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.debugMode
}

// LogPrompt logs the complete prompt when debug mode is enabled.
func (p *PromptInspector) LogPrompt(req bifrost.ChatRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.debugMode {
		return
	}

	entry := PromptLog{
		Model:    req.Model,
		Messages: req.Messages,
	}

	// Estimate token count
	for _, msg := range req.Messages {
		entry.TokenCount += estimateTokens(msg.Content)
	}

	p.logs = append(p.logs, entry)

	// Also log to standard logger in debug mode
	data, _ := json.MarshalIndent(entry, "", "  ")
	log.Printf("[prompt-inspector] LLM request:\n%s", string(data))
}

// InspectCurrentPrompt returns the current session prompt state.
func (p *PromptInspector) InspectCurrentPrompt(session *Session) PromptState {
	if session == nil {
		return PromptState{}
	}

	ctxMgr := session.ContextManager()
	if ctxMgr == nil {
		return PromptState{
			SystemPrompt: session.SystemPrompt(),
		}
	}

	messages := ctxMgr.Messages()
	tokenCount := 0
	for _, msg := range messages {
		tokenCount += estimateTokens(msg.Content)
	}

	return PromptState{
		SystemPrompt: session.SystemPrompt(),
		Messages:     messages,
		TokenCount:   tokenCount + estimateTokens(session.SystemPrompt()),
	}
}

// DryRun builds the prompt without executing the LLM call.
// Returns the complete prompt state that would be sent.
func (p *PromptInspector) DryRun(session *Session) (*PromptState, error) {
	state := p.InspectCurrentPrompt(session)
	return &state, nil
}

// Logs returns all recorded prompt logs (for testing/debugging).
func (p *PromptInspector) Logs() []PromptLog {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]PromptLog, len(p.logs))
	copy(out, p.logs)
	return out
}

// ClearLogs clears all recorded prompt logs.
func (p *PromptInspector) ClearLogs() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logs = nil
}

// estimateTokens provides a rough token count estimate (4 chars per token).
func estimateTokens(content interface{}) int {
	if content == nil {
		return 0
	}
	switch v := content.(type) {
	case string:
		if v == "" {
			return 0
		}
		return len(v) / 4
	default:
		data, _ := json.Marshal(v)
		return len(data) / 4
	}
}
