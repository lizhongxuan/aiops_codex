package coroot

import (
	"net/http"
	"sync"
)

// TokenManager manages the Coroot authentication token and injects it into
// outgoing HTTP requests.
type TokenManager struct {
	mu    sync.RWMutex
	token string
}

// NewTokenManager creates a TokenManager with the given initial token.
func NewTokenManager(token string) *TokenManager {
	return &TokenManager{token: token}
}

// Token returns the current token value.
func (tm *TokenManager) Token() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.token
}

// SetToken replaces the current token.
func (tm *TokenManager) SetToken(token string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.token = token
}

// InjectAuth adds the Coroot authentication header to the given request.
// If no token is configured the request is left unchanged.
func (tm *TokenManager) InjectAuth(req *http.Request) {
	tok := tm.Token()
	if tok == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+tok)
}
