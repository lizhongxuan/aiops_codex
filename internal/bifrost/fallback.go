package bifrost

import "sync"

// RuntimeSnapshot captures the primary provider+model state so it can be
// restored after a fallback activation.
type RuntimeSnapshot struct {
	Provider string
	Model    string
}

// FallbackChain manages a list of fallback provider+model pairs.
// When the primary provider fails persistently, TryActivate switches the
// gateway to the next fallback entry. RestorePrimary reverts to the
// original provider+model and should be called at the start of each new turn.
type FallbackChain struct {
	entries         []FallbackEntry
	currentIndex    int
	activated       bool
	primarySnapshot *RuntimeSnapshot
	mu              sync.Mutex
}

// NewFallbackChain creates a FallbackChain from the given entries.
func NewFallbackChain(entries []FallbackEntry) *FallbackChain {
	return &FallbackChain{
		entries:      entries,
		currentIndex: 0,
	}
}

// TryActivate switches the gateway to the next fallback provider+model.
// On the first activation it saves the primary provider+model as a snapshot.
// Returns true if a fallback was activated, false if the chain is exhausted.
func (fc *FallbackChain) TryActivate(gateway *Gateway) bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if len(fc.entries) == 0 {
		return false
	}

	// Save primary state on first activation.
	if !fc.activated {
		gateway.mu.RLock()
		fc.primarySnapshot = &RuntimeSnapshot{
			Provider: gateway.defaultProvider,
			Model:    gateway.defaultModel,
		}
		gateway.mu.RUnlock()
	}

	// Check if we've exhausted all fallback entries.
	if fc.currentIndex >= len(fc.entries) {
		return false
	}

	entry := fc.entries[fc.currentIndex]
	fc.currentIndex++
	fc.activated = true

	// Switch the gateway to the fallback provider+model.
	gateway.mu.Lock()
	gateway.defaultProvider = entry.Provider
	gateway.defaultModel = entry.Model
	gateway.mu.Unlock()

	return true
}

// RestorePrimary reverts the gateway to the original primary provider+model.
// This should be called at the start of each new turn (turn-scoped fallback).
// Returns true if the primary was restored, false if no snapshot exists.
func (fc *FallbackChain) RestorePrimary(gateway *Gateway) bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if fc.primarySnapshot == nil {
		return false
	}

	gateway.mu.Lock()
	gateway.defaultProvider = fc.primarySnapshot.Provider
	gateway.defaultModel = fc.primarySnapshot.Model
	gateway.mu.Unlock()

	// Reset chain state so it can be re-activated if needed.
	fc.currentIndex = 0
	fc.activated = false

	return true
}

// IsActivated returns whether the fallback chain is currently active
// (i.e., the gateway is using a fallback provider instead of the primary).
func (fc *FallbackChain) IsActivated() bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.activated
}

// Reset resets the chain index without restoring the primary.
// Useful for testing or manual chain management.
func (fc *FallbackChain) Reset() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.currentIndex = 0
	fc.activated = false
	fc.primarySnapshot = nil
}
