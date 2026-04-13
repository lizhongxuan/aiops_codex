package filewatcher

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// testSubscriber collects events for assertions.
type testSubscriber struct {
	mu     sync.Mutex
	events []Event
}

func (s *testSubscriber) OnFileChange(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *testSubscriber) getEvents() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]Event, len(s.events))
	copy(cp, s.events)
	return cp
}

func TestNewWatcher(t *testing.T) {
	w := NewWatcher(100 * time.Millisecond)
	if w == nil {
		t.Fatal("NewWatcher returned nil")
	}
	if w.throttle == nil {
		t.Fatal("throttle not initialized")
	}
	w.Stop()
}

func TestSubscribe(t *testing.T) {
	w := NewWatcher(50 * time.Millisecond)
	defer w.Stop()

	sub := &testSubscriber{}
	w.Subscribe(sub)

	w.mu.RLock()
	count := len(w.subscribers)
	w.mu.RUnlock()

	if count != 1 {
		t.Fatalf("expected 1 subscriber, got %d", count)
	}
}

func TestWatchAndNotify(t *testing.T) {
	dir := t.TempDir()

	w := NewWatcher(50 * time.Millisecond)
	defer w.Stop()

	sub := &testSubscriber{}
	w.Subscribe(sub)

	if err := w.Watch(dir); err != nil {
		t.Fatalf("Watch failed: %v", err)
	}

	// Create a file — should trigger a Create event.
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Wait for throttle window + processing time.
	time.Sleep(200 * time.Millisecond)

	events := sub.getEvents()
	if len(events) == 0 {
		t.Fatal("expected at least one event, got none")
	}

	// The first event should be for our file.
	found := false
	for _, ev := range events {
		if ev.Path == filePath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected event for %s, got events: %+v", filePath, events)
	}
}

func TestWatchInvalidDir(t *testing.T) {
	w := NewWatcher(50 * time.Millisecond)
	defer w.Stop()

	err := w.Watch("/nonexistent-dir-xyz-12345")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestOpString(t *testing.T) {
	tests := []struct {
		op   Op
		want string
	}{
		{Create, "CREATE"},
		{Modify, "MODIFY"},
		{Delete, "DELETE"},
		{Op(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.op.String(); got != tt.want {
			t.Errorf("Op(%d).String() = %q, want %q", tt.op, got, tt.want)
		}
	}
}
