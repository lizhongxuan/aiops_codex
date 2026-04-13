package bifrost

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RetryConfig controls the exponential backoff retry behaviour.
type RetryConfig struct {
	MaxRetries     int           // default 3
	InitialBackoff time.Duration // default 5s
	MaxBackoff     time.Duration // default 120s
	BackoffFactor  float64       // default 2.0
}

// DefaultRetryConfig returns sensible defaults for LLM API retries.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 5 * time.Second,
		MaxBackoff:     120 * time.Second,
		BackoffFactor:  2.0,
	}
}

// APIError represents an HTTP API error with a status code.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bifrost: API error %d: %s", e.StatusCode, e.Message)
}

// isRetryableError determines whether an error should trigger a retry.
// Retryable: 5xx server errors, 429 rate limit, connection/transport errors.
// Non-retryable: 4xx client errors (except 429).
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for APIError with status code.
	var apiErr *APIError
	if ok := errorAs(err, &apiErr); ok {
		// 429 Too Many Requests is retryable.
		if apiErr.StatusCode == 429 {
			return true
		}
		// 5xx server errors are retryable.
		if apiErr.StatusCode >= 500 {
			return true
		}
		// Other 4xx client errors are not retryable.
		if apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			return false
		}
	}

	// Transport/connection errors are retryable.
	if IsTransportError(err) {
		return true
	}

	// Check for common retryable error patterns in the message.
	msg := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"api error 429",
		"api error 5",
		"rate limit",
		"server error",
		"internal server error",
		"service unavailable",
		"bad gateway",
		"gateway timeout",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}

	return false
}

// isEmptyResponse checks if a ChatResponse is empty or malformed,
// which is a common symptom of rate limiting.
func isEmptyResponse(resp *ChatResponse) bool {
	if resp == nil {
		return true
	}
	if resp.Message.Role == "" && resp.Message.Content == nil && len(resp.Message.ToolCalls) == 0 {
		return true
	}
	return false
}

// WithRetry executes fn with exponential backoff retry logic.
// It retries on retryable errors and empty/malformed responses.
// The wait is interruptible: ctx.Done() is checked every 200ms during backoff.
func WithRetry(ctx context.Context, cfg RetryConfig, fn func() (*ChatResponse, error)) (*ChatResponse, error) {
	backoff := cfg.InitialBackoff
	var lastErr error

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		// Check context before each attempt.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := fn()

		// Success with a valid response.
		if err == nil && !isEmptyResponse(resp) {
			return resp, nil
		}

		// Determine if we should retry.
		if err != nil {
			if !isRetryableError(err) {
				return nil, err // Non-retryable error, fail immediately.
			}
			lastErr = err
		} else {
			// Empty/malformed response — treat as retryable.
			lastErr = fmt.Errorf("bifrost: empty or malformed response (attempt %d/%d)", attempt+1, cfg.MaxRetries+1)
		}

		// Don't wait after the last attempt.
		if attempt == cfg.MaxRetries {
			break
		}

		// Interruptible wait: check ctx.Done() every 200ms.
		if err := interruptibleSleep(ctx, backoff); err != nil {
			return nil, err
		}

		// Increase backoff for next attempt, capped at MaxBackoff.
		backoff = time.Duration(float64(backoff) * cfg.BackoffFactor)
		if backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
	}

	return nil, fmt.Errorf("bifrost: max retries (%d) exceeded: %w", cfg.MaxRetries, lastErr)
}

// interruptibleSleep waits for the given duration but checks ctx.Done()
// every 200ms so the caller can be interrupted promptly.
func interruptibleSleep(ctx context.Context, d time.Duration) error {
	const checkInterval = 200 * time.Millisecond
	remaining := d

	for remaining > 0 {
		wait := checkInterval
		if remaining < wait {
			wait = remaining
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}

		remaining -= wait
	}
	return nil
}

// errorAs is a thin wrapper around errors.As to keep the import tidy.
func errorAs(err error, target interface{}) bool {
	// Use type assertion approach for *APIError.
	switch t := target.(type) {
	case **APIError:
		// Walk the error chain.
		for err != nil {
			if apiErr, ok := err.(*APIError); ok {
				*t = apiErr
				return true
			}
			// Try unwrapping.
			if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
				err = unwrapper.Unwrap()
			} else {
				return false
			}
		}
	}
	return false
}

// IsTransportError checks whether an error is a TCP/TLS transport-level error
// that suggests the HTTP client should be rebuilt.
func IsTransportError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	transportPatterns := []string{
		"connection reset",
		"connection refused",
		"broken pipe",
		"eof",
		"tls handshake timeout",
		"tls handshake failure",
		"no such host",
		"i/o timeout",
		"connection timed out",
		"use of closed network connection",
	}

	for _, pattern := range transportPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}
