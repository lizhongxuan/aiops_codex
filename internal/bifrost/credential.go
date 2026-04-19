package bifrost

import (
	"fmt"
	"sync"
	"time"
)

// Default cooldown durations for different error scenarios.
const (
	DefaultCooldown   = 60 * time.Second  // 429 rate limit
	AuthErrorCooldown = 24 * time.Hour    // 401/403 auth errors
)

// Credential represents a single API key with its status and cooldown metadata.
type Credential struct {
	ID             string
	Provider       string // openai, anthropic, ollama
	APIKey         string
	BaseURL        string
	Status         string // active, exhausted, disabled
	ExhaustedUntil time.Time
}

// CredentialPool implements the Pool interface with round-robin selection
// and cooldown-based exhaustion tracking.
type CredentialPool struct {
	credentials []*Credential
	index       map[string]int // round-robin index per provider
	mu          sync.Mutex
}

// NewCredentialPool creates a CredentialPool from the given credentials.
func NewCredentialPool(creds []*Credential) *CredentialPool {
	return &CredentialPool{
		credentials: creds,
		index:       make(map[string]int),
	}
}

// Select picks the next available credential for the given provider using
// round-robin. It skips exhausted credentials and auto-recovers those whose
// cooldown has expired. Returns the API key or an error if all credentials
// for the provider are exhausted.
func (p *CredentialPool) Select(provider string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()

	// Collect credentials for this provider.
	var providerCreds []*Credential
	for _, c := range p.credentials {
		if c.Provider == provider {
			providerCreds = append(providerCreds, c)
		}
	}

	if len(providerCreds) == 0 {
		return "", fmt.Errorf("bifrost: no credentials configured for provider %q", provider)
	}

	startIdx := p.index[provider] % len(providerCreds)

	// Try each credential starting from the current round-robin position.
	for i := 0; i < len(providerCreds); i++ {
		idx := (startIdx + i) % len(providerCreds)
		cred := providerCreds[idx]

		// Auto-recover: if cooldown has expired, reactivate.
		if cred.Status == "exhausted" && !now.Before(cred.ExhaustedUntil) {
			cred.Status = "active"
		}

		if cred.Status == "active" {
			// Advance round-robin to the next position.
			p.index[provider] = (idx + 1) % len(providerCreds)
			return cred.APIKey, nil
		}
	}

	return "", fmt.Errorf("bifrost: all credentials exhausted for provider %q", provider)
}

// MarkExhausted marks the credential matching provider+apiKey as exhausted
// with the default cooldown (60 seconds, suitable for 429 rate-limit errors).
func (p *CredentialPool) MarkExhausted(provider, apiKey string) {
	p.MarkExhaustedWithCooldown(provider, apiKey, DefaultCooldown)
}

// MarkExhaustedWithCooldown marks the credential matching provider+apiKey
// as exhausted with an explicit cooldown duration.
func (p *CredentialPool) MarkExhaustedWithCooldown(provider, apiKey string, cooldown time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, c := range p.credentials {
		if c.Provider == provider && c.APIKey == apiKey {
			c.Status = "exhausted"
			c.ExhaustedUntil = time.Now().Add(cooldown)
			return
		}
	}
}
