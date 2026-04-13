// Package filewatcher provides filesystem change monitoring with subscriber-based
// notification delivery and event throttling.
package filewatcher

import (
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Op represents the type of filesystem operation.
type Op int

const (
	Create Op = iota
	Modify
	Delete
)

// String returns a human-readable representation of the operation.
func (o Op) String() string {
	switch o {
	case Create:
		return "CREATE"
	case Modify:
		return "MODIFY"
	case Delete:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

// Event represents a filesystem change event.
type Event struct {
	Path string
	Op   Op
	Time time.Time
}

// Subscriber is the interface for consumers of file change notifications.
type Subscriber interface {
	OnFileChange(event Event)
}

// Watcher monitors directories for filesystem changes and notifies subscribers.
type Watcher struct {
	mu          sync.RWMutex
	subscribers []Subscriber
	throttle    *throttler
	fsWatcher   *fsnotify.Watcher
	dirs        []string
	done        chan struct{}
}

// NewWatcher creates a new Watcher with the given throttle duration.
// Events occurring within the throttle window for the same path are coalesced.
func NewWatcher(throttle time.Duration) *Watcher {
	return &Watcher{
		throttle: newThrottler(throttle),
		done:     make(chan struct{}),
	}
}

// Subscribe registers a subscriber to receive file change notifications.
func (w *Watcher) Subscribe(sub Subscriber) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.subscribers = append(w.subscribers, sub)
}

// Watch starts monitoring the given directories for filesystem changes.
// It is non-blocking; events are processed in a background goroutine.
func (w *Watcher) Watch(dirs ...string) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.fsWatcher = fsw

	for _, dir := range dirs {
		if err := fsw.Add(dir); err != nil {
			fsw.Close()
			return err
		}
	}
	w.dirs = append(w.dirs, dirs...)

	go w.loop()
	return nil
}

// Stop halts the watcher and releases resources.
func (w *Watcher) Stop() {
	select {
	case <-w.done:
		// already stopped
	default:
		close(w.done)
	}
	if w.fsWatcher != nil {
		w.fsWatcher.Close()
	}
	w.throttle.stop()
}

// loop processes raw fsnotify events and feeds them through the throttler.
func (w *Watcher) loop() {
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			op := mapOp(ev.Op)
			if op < 0 {
				continue
			}
			event := Event{
				Path: ev.Name,
				Op:   op,
				Time: time.Now(),
			}
			w.throttle.submit(event, func(e Event) {
				w.notify(e)
			})
		case _, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			// errors are silently dropped; callers can extend if needed
		}
	}
}

// notify delivers an event to all registered subscribers.
func (w *Watcher) notify(event Event) {
	w.mu.RLock()
	subs := make([]Subscriber, len(w.subscribers))
	copy(subs, w.subscribers)
	w.mu.RUnlock()

	for _, sub := range subs {
		sub.OnFileChange(event)
	}
}

// mapOp converts an fsnotify operation to our Op type.
// Returns -1 for operations we don't care about (e.g. Chmod).
func mapOp(op fsnotify.Op) Op {
	switch {
	case op.Has(fsnotify.Create):
		return Create
	case op.Has(fsnotify.Write):
		return Modify
	case op.Has(fsnotify.Remove), op.Has(fsnotify.Rename):
		return Delete
	default:
		return -1
	}
}
