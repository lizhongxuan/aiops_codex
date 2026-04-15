package bifrost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Pool is a forward-declared interface for credential pool (implemented in credential.go).
// It selects an available API key and marks exhausted ones.
type Pool interface {
	Select(provider string) (string, error)
	MarkExhausted(provider, apiKey string)
}

// Tracker is a forward-declared interface for usage tracking (implemented in usage.go).
// It records token usage and cost per LLM call.
type Tracker interface {
	Record(rec UsageRecord)
}

// UsageRecord holds token consumption data for a single LLM call.
type UsageRecord struct {
	SessionID    string
	Provider     string
	Model        string
	PromptTokens int
	OutputTokens int
	CostUSD      float64
}

// FallbackEntry pairs a provider name with a model for fallback chains.
type FallbackEntry struct {
	Provider string
	Model    string
}

// GatewayConfig holds initialization parameters for a Gateway.
type GatewayConfig struct {
	DefaultProvider string
	DefaultModel    string
	Fallbacks       []FallbackEntry
	Pool            Pool
	Tracker         Tracker
}

// Gateway is the main entry point for LLM calls. The agent loop calls
// ChatCompletion / StreamChatCompletion on Gateway without caring which
// provider or credential is used underneath.
type Gateway struct {
	providers       map[string]Provider
	pool            Pool
	tracker         Tracker
	fallbacks       []FallbackEntry
	fallbackChain   *FallbackChain
	defaultProvider string
	defaultModel    string
	mu              sync.RWMutex
}

// NewGateway creates a Gateway from the given config.
func NewGateway(cfg GatewayConfig) *Gateway {
	return &Gateway{
		providers:       make(map[string]Provider),
		pool:            cfg.Pool,
		tracker:         cfg.Tracker,
		fallbacks:       cfg.Fallbacks,
		defaultProvider: cfg.DefaultProvider,
		defaultModel:    cfg.DefaultModel,
	}
}

// RegisterProvider adds (or replaces) a named provider.
func (g *Gateway) RegisterProvider(name string, p Provider) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.providers[name] = p
}

// getProvider returns the provider registered under name, or an error.
func (g *Gateway) getProvider(name string) (Provider, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	p, ok := g.providers[name]
	if !ok {
		return nil, fmt.Errorf("bifrost: unknown provider %q", name)
	}
	return p, nil
}

// resolveProvider determines the provider name from the request model string.
// If the model contains a "/" prefix (e.g. "anthropic/claude-sonnet-4-20250514"), the
// prefix is used as the provider name and stripped from the model. Otherwise
// the gateway's default provider is used.
func (g *Gateway) resolveProvider(req *ChatRequest) (Provider, error) {
	providerName := g.defaultProvider

	if idx := strings.Index(req.Model, "/"); idx > 0 {
		providerName = req.Model[:idx]
		req.Model = req.Model[idx+1:]
	}

	if providerName == "" {
		return nil, errors.New("bifrost: no provider specified and no default configured")
	}

	return g.getProvider(providerName)
}

// SetFallbackChain attaches a FallbackChain to the gateway for provider-level
// failover. When the primary provider fails persistently, the gateway will
// try fallback providers from the chain.
func (g *Gateway) SetFallbackChain(fc *FallbackChain) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fallbackChain = fc
}

// ChatCompletion performs a non-streaming LLM call with integrated credential
// rotation (R3) and provider fallback (R4).
func (g *Gateway) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Save original model in case resolveProvider modifies it (prefix stripping).
	originalModel := req.Model

	provider, err := g.resolveProvider(&req)
	if err != nil {
		return nil, err
	}

	resp, err := provider.ChatCompletion(ctx, req)

	// R3: Credential rotation on 429 errors.
	if err != nil && is429Error(err) && g.pool != nil {
		providerName := provider.Name()
		// Try to get the current key and mark it exhausted.
		if currentKey, selErr := g.pool.Select(providerName); selErr == nil {
			g.pool.MarkExhausted(providerName, currentKey)
		}
		// Try with a new credential.
		if _, selErr := g.pool.Select(providerName); selErr == nil {
			resp, err = provider.ChatCompletion(ctx, req)
		}
	}

	// R4: Provider fallback on persistent failures.
	if err != nil {
		g.mu.RLock()
		fc := g.fallbackChain
		g.mu.RUnlock()

		if fc != nil {
			for fc.TryActivate(g) {
				// Re-resolve provider after fallback activation.
				req.Model = originalModel
				provider, resolveErr := g.resolveProvider(&req)
				if resolveErr != nil {
					continue
				}
				resp, err = provider.ChatCompletion(ctx, req)
				if err == nil {
					break
				}
			}
		}
	}

	if err != nil {
		return nil, err
	}

	// Record usage when a tracker is configured.
	if g.tracker != nil {
		g.tracker.Record(UsageRecord{
			Provider:     provider.Name(),
			Model:        req.Model,
			PromptTokens: resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		})
	}

	return resp, nil
}

// is429Error checks if an error indicates a 429 Too Many Requests response.
func is429Error(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if ok := errorAs(err, &apiErr); ok {
		return apiErr.StatusCode == 429
	}
	return strings.Contains(strings.ToLower(err.Error()), "429")
}

// ProviderCapabilities resolves the provider for the given model string and
// returns its capabilities. If the provider cannot be resolved, a zero-value
// ProviderCapabilities is returned.
func (g *Gateway) ProviderCapabilities(model string) ProviderCapabilities {
	req := ChatRequest{Model: model}
	provider, err := g.resolveProvider(&req)
	if err != nil {
		return ProviderCapabilities{}
	}
	return provider.Capabilities()
}

// StreamChatCompletion performs a streaming LLM call and returns a channel of events.
func (g *Gateway) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	provider, err := g.resolveProvider(&req)
	if err != nil {
		return nil, err
	}

	return provider.StreamChatCompletion(ctx, req)
}
