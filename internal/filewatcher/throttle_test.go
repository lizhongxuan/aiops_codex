package filewatcher

import (
	"sync"
	"testing"
	"time"
)

func TestThrottleCoalescing(t *testing.T) {
	thr := newThrottler(100 * time.Millisecond)
	defer thr.stop()

	var mu sync.Mutex
	var delivered []Event

	cb := func(e Event) {
		mu.Lock()
		delivered = append(delivered, e)
		mu.Unlock()
	}

	// Submit multiple events for the same path rapidly.
	for i := 0; i < 5; i++ {
		thr.submit(Event{
			Path: "/tmp/file.txt",
			Op:   Modify,
			Time: time.Now(),
		}, cb)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for throttle window to expire.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := len(delivered)
	mu.Unlock()

	// Should have coalesced into a single delivery.
	if count != 1 {
		t.Fatalf("expected 1 coalesced event, got %d", count)
	}
}

func TestThrottleDifferentPaths(t *testing.T) {
	thr := newThrottler(50 * time.Millisecond)
	defer thr.stop()

	var mu sync.Mutex
	var delivered []Event

	cb := func(e Event) {
		mu.Lock()
		delivered = append(delivered, e)
		mu.Unlock()
	}

	// Submit events for different paths.
	thr.submit(Event{Path: "/a.txt", Op: Create, Time: time.Now()}, cb)
	thr.submit(Event{Path: "/b.txt", Op: Modify, Time: time.Now()}, cb)
	thr.submit(Event{Path: "/c.txt", Op: Delete, Time: time.Now()}, cb)

	// Wait for all to flush.
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	count := len(delivered)
	mu.Unlock()

	if count != 3 {
		t.Fatalf("expected 3 events (one per path), got %d", count)
	}
}

func TestThrottleStop(t *testing.T) {
	thr := newThrottler(200 * time.Millisecond)

	var mu sync.Mutex
	var delivered []Event

	cb := func(e Event) {
		mu.Lock()
		delivered = append(delivered, e)
		mu.Unlock()
	}

	thr.submit(Event{Path: "/x.txt", Op: Create, Time: time.Now()}, cb)

	// Stop before the throttle window expires.
	thr.stop()

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	count := len(delivered)
	mu.Unlock()

	if count != 0 {
		t.Fatalf("expected 0 events after stop, got %d", count)
	}
}

func TestThrottleSubmitAfterStop(t *testing.T) {
	thr := newThrottler(50 * time.Millisecond)
	thr.stop()

	var mu sync.Mutex
	var delivered []Event

	cb := func(e Event) {
		mu.Lock()
		delivered = append(delivered, e)
		mu.Unlock()
	}

	// Submit after stop — should be ignored.
	thr.submit(Event{Path: "/y.txt", Op: Modify, Time: time.Now()}, cb)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := len(delivered)
	mu.Unlock()

	if count != 0 {
		t.Fatalf("expected 0 events after stop, got %d", count)
	}
}
