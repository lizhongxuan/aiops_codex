package bifrost

import (
	"context"
)

const defaultOllamaBaseURL = "http://localhost:11434/v1"

// OllamaProvider implements Provider by delegating to an embedded OpenAIProvider.
// Ollama exposes an OpenAI-compatible /v1/chat/completions endpoint, so we
// simply wrap OpenAIProvider with the correct defaults (local URL, no API key).
type OllamaProvider struct {
	openai *OpenAIProvider
}

// NewOllamaProvider creates an OllamaProvider. If baseURL is empty the
// default local Ollama endpoint (http://localhost:11434/v1) is used.
func NewOllamaProvider(baseURL string) *OllamaProvider {
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	return &OllamaProvider{
		openai: NewOpenAIProvider("", baseURL),
	}
}

// Name returns the provider identifier.
func (p *OllamaProvider) Name() string { return "ollama" }

// SupportsToolCalling returns true — most modern Ollama models support tool calling.
func (p *OllamaProvider) SupportsToolCalling() bool { return true }

// ChatCompletion delegates to the embedded OpenAI-compatible provider.
func (p *OllamaProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.openai.ChatCompletion(ctx, req)
}

// StreamChatCompletion delegates to the embedded OpenAI-compatible provider.
func (p *OllamaProvider) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	return p.openai.StreamChatCompletion(ctx, req)
}
