package hooks

import "sync"

// Registry manages hook registration and discovery by event type.
type Registry struct {
	mu    sync.RWMutex
	hooks map[HookEvent][]Hook
}

// NewRegistry creates an empty hook registry.
func NewRegistry() *Registry {
	return &Registry{
		hooks: make(map[HookEvent][]Hook),
	}
}

// Register adds a hook to the registry for its specified event type.
func (r *Registry) Register(hook Hook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks[hook.Event] = append(r.hooks[hook.Event], hook)
}

// Get returns all hooks registered for the given event type.
// The returned slice is a copy and safe to iterate without holding the lock.
func (r *Registry) Get(event HookEvent) []Hook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src := r.hooks[event]
	if len(src) == 0 {
		return nil
	}
	out := make([]Hook, len(src))
	copy(out, src)
	return out
}

// Count returns the number of hooks registered for the given event type.
func (r *Registry) Count(event HookEvent) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.hooks[event])
}
