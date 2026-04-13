package filewatcher

import (
	"sync"
	"time"
)

// throttler coalesces rapid events for the same file path within a configurable
// time window, preventing excessive notification delivery.
type throttler struct {
	mu       sync.Mutex
	window   time.Duration
	pending  map[string]*pendingEvent
	timers   map[string]*time.Timer
	stopped  bool
}

// pendingEvent holds the latest event for a given path during the throttle window.
type pendingEvent struct {
	event    Event
	callback func(Event)
}

// newThrottler creates a throttler with the given coalescing window.
func newThrottler(window time.Duration) *throttler {
	return &throttler{
		window:  window,
		pending: make(map[string]*pendingEvent),
		timers:  make(map[string]*time.Timer),
	}
}

// submit enqueues an event. If an event for the same path is already pending,
// it is replaced (coalesced). The callback fires after the throttle window
// elapses with no new events for that path.
func (t *throttler) submit(event Event, cb func(Event)) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return
	}

	path := event.Path

	// Update or create the pending event for this path.
	t.pending[path] = &pendingEvent{event: event, callback: cb}

	// Reset existing timer or create a new one.
	if timer, exists := t.timers[path]; exists {
		timer.Reset(t.window)
	} else {
		t.timers[path] = time.AfterFunc(t.window, func() {
			t.flush(path)
		})
	}
}

// flush delivers the pending event for the given path and cleans up state.
func (t *throttler) flush(path string) {
	t.mu.Lock()
	pe, ok := t.pending[path]
	if ok {
		delete(t.pending, path)
		delete(t.timers, path)
	}
	stopped := t.stopped
	t.mu.Unlock()

	if ok && !stopped {
		pe.callback(pe.event)
	}
}

// stop cancels all pending timers and prevents future submissions.
func (t *throttler) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stopped = true
	for path, timer := range t.timers {
		timer.Stop()
		delete(t.timers, path)
		delete(t.pending, path)
	}
}
