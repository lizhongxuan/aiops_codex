package bifrost

import (
	"context"
)

const defaultDeepSeekBaseURL = "https://api.deepseek.com/v1"

// DeepSeekProvider implements Provider by delegating to an embedded OpenAIProvider.
// DeepSeek exposes an OpenAI-compatible /v1/chat/completions endpoint, so we
// simply wrap OpenAIProvider with the correct defaults and capability overrides.
type DeepSeekProvider struct {
	openai *OpenAIProvider
}

// NewDeepSeekProvider creates a DeepSeekProvider. If baseURL is empty the
// default DeepSeek endpoint (https://api.deepseek.com/v1) is used.
func NewDeepSeekProvider(apiKey, baseURL string) *DeepSeekProvider {
	if baseURL == "" {
		baseURL = defaultDeepSeekBaseURL
	}
	return &DeepSeekProvider{
		openai: NewOpenAIProvider(apiKey, baseURL),
	}
}

// Name returns the provider identifier.
func (p *DeepSeekProvider) Name() string { return "deepseek" }

// SupportsToolCalling returns true — DeepSeek models support tool calling.
func (p *DeepSeekProvider) SupportsToolCalling() bool { return true }

// Capabilities returns the feature set supported by the DeepSeek provider.
func (p *DeepSeekProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		SupportsNativeSearch:       false,
		SupportsReasoningContent:   true,
		SupportsStreamingToolCalls: true,
		SupportsToolUseFormat:      false,
		ToolCallingFormat:          "openai_function",
	}
}

// ChatCompletion delegates to the embedded OpenAI-compatible provider.
func (p *DeepSeekProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.openai.ChatCompletion(ctx, req)
}

// StreamChatCompletion delegates to the embedded OpenAI-compatible provider.
func (p *DeepSeekProvider) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	return p.openai.StreamChatCompletion(ctx, req)
}
