package guardian

import (
	"context"
	"errors"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

const (
	// DefaultGuardianModel is the preferred model for guardian reviews.
	DefaultGuardianModel = "gpt-5.4"
	// DefaultGuardianTimeout is the maximum time allowed for a guardian review.
	DefaultGuardianTimeout = 90 * time.Second
)

// Guardian performs security reviews of tool invocations using a dedicated
// LLM model to assess risk and authorization.
type Guardian struct {
	gateway        *bifrost.Gateway
	preferredModel string
	timeout        time.Duration
	sessionMgr     *ReviewSessionManager
}

// NewGuardian creates a Guardian with default settings.
func NewGuardian(gateway *bifrost.Gateway) *Guardian {
	return &Guardian{
		gateway:        gateway,
		preferredModel: DefaultGuardianModel,
		timeout:        DefaultGuardianTimeout,
		sessionMgr:     NewReviewSessionManager(),
	}
}

// WithModel sets a custom model for the guardian.
func (g *Guardian) WithModel(model string) *Guardian {
	g.preferredModel = model
	return g
}

// WithTimeout sets a custom timeout for guardian reviews.
func (g *Guardian) WithTimeout(d time.Duration) *Guardian {
	g.timeout = d
	return g
}

// SessionManager returns the guardian's review session manager.
func (g *Guardian) SessionManager() *ReviewSessionManager {
	return g.sessionMgr
}

// ReviewApproval sends a compact transcript to the guardian model for risk
// assessment. It fails closed on timeout, execution failure, or malformed output.
func (g *Guardian) ReviewApproval(ctx context.Context, messages []bifrost.Message, request GuardianApprovalRequest) (*GuardianAssessment, error) {
	// Apply guardian timeout.
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	// Build compact transcript for the guardian model.
	transcript := BuildTranscript(messages, request)

	// Convert transcript to bifrost messages for the LLM call.
	llmMessages := buildGuardianMessages(transcript)

	// Make the LLM call.
	req := bifrost.ChatRequest{
		Model:       g.preferredModel,
		Messages:    llmMessages,
		MaxTokens:   512,
		Temperature: 0.0,
	}

	resp, err := g.gateway.ChatCompletion(ctx, req)
	if err != nil {
		// Fail closed on execution failure or timeout.
		if errors.Is(err, context.DeadlineExceeded) {
			return failClosed("guardian review timed out"), err
		}
		return failClosed("guardian execution failed: " + err.Error()), err
	}

	// Extract response content.
	content := extractResponseContent(resp)

	// Parse and validate the assessment.
	assessment, parseErr := ParseAssessment([]byte(content))
	if parseErr != nil {
		return assessment, parseErr
	}

	return assessment, nil
}

// buildGuardianMessages converts transcript entries into bifrost messages
// with a system prompt instructing the guardian model.
func buildGuardianMessages(transcript []TranscriptEntry) []bifrost.Message {
	systemPrompt := `You are a security guardian. Analyze the conversation and pending tool invocation.
Respond with a JSON object containing:
- "risk_level": one of "low", "medium", "high", "critical"
- "user_authorization": one of "explicit", "implied", "none"
- "outcome": one of "allow", "deny"
- "rationale": brief explanation of your decision

Only output valid JSON. No other text.`

	messages := make([]bifrost.Message, 0, len(transcript)+1)
	messages = append(messages, bifrost.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	// Add transcript entries as user context.
	var contextContent string
	for _, entry := range transcript {
		contextContent += "[" + entry.Role + "]: " + entry.Content + "\n"
	}

	messages = append(messages, bifrost.Message{
		Role:    "user",
		Content: contextContent,
	})

	return messages
}

// extractResponseContent gets the string content from a ChatResponse.
func extractResponseContent(resp *bifrost.ChatResponse) string {
	if resp == nil {
		return ""
	}
	switch v := resp.Message.Content.(type) {
	case string:
		return v
	default:
		return ""
	}
}
