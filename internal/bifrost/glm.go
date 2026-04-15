package bifrost

import (
	"context"
)

const defaultGLMBaseURL = "https://open.bigmodel.cn/api/paas/v4"

// GLMProvider implements Provider by delegating to an embedded OpenAIProvider.
// GLM (Zhipu AI) exposes an OpenAI-compatible /v1/chat/completions endpoint,
// so we simply wrap OpenAIProvider with the correct defaults and capability overrides.
type GLMProvider struct {
	openai *OpenAIProvider
}

// NewGLMProvider creates a GLMProvider. If baseURL is empty the
// default GLM endpoint (https://open.bigmodel.cn/api/paas/v4) is used.
func NewGLMProvider(apiKey, baseURL string) *GLMProvider {
	if baseURL == "" {
		baseURL = defaultGLMBaseURL
	}
	return &GLMProvider{
		openai: NewOpenAIProvider(apiKey, baseURL),
	}
}

// Name returns the provider identifier.
func (p *GLMProvider) Name() string { return "glm" }

// SupportsToolCalling returns true — GLM models support tool calling.
func (p *GLMProvider) SupportsToolCalling() bool { return true }

// Capabilities returns the feature set supported by the GLM provider.
func (p *GLMProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		SupportsNativeSearch:       true,
		SupportsReasoningContent:   false,
		SupportsStreamingToolCalls: true,
		SupportsToolUseFormat:      false,
		ToolCallingFormat:          "openai_function",
	}
}

// ChatCompletion delegates to the embedded OpenAI-compatible provider.
func (p *GLMProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.openai.ChatCompletion(ctx, req)
}

// StreamChatCompletion delegates to the embedded OpenAI-compatible provider.
func (p *GLMProvider) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	return p.openai.StreamChatCompletion(ctx, req)
}
