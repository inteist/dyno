// Package eventtest gives test helpers for the event package.
package eventtest

import (
	"fmt"
	"sync"

	"token-rate/internal/event"
)

// Recorder is a Sink that keeps every event. It is safe for concurrent use.
type Recorder struct {
	mu     sync.Mutex
	events []event.Event
}

// Publish keeps e.
func (r *Recorder) Publish(e event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

// Events returns a copy of the events in the order of arrival.
func (r *Recorder) Events() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.Event(nil), r.events...)
}

// Names returns the type names of the events, for example "StepStarted".
// Names leaves out the lossy events, because their number depends on time.
func (r *Recorder) Names() []string {
	var names []string
	for _, e := range r.Events() {
		if _, lossy := e.(event.Lossy); lossy {
			continue
		}
		names = append(names, Name(e))
	}
	return names
}

// Name returns the type name of e without the package, for example
// "StepStarted".
func Name(e event.Event) string {
	return fmt.Sprintf("%T", e)[len("event."):]
}

// Of returns the events of type T in the order of arrival.
func Of[T event.Event](r *Recorder) []T {
	var out []T
	for _, e := range r.Events() {
		if t, ok := e.(T); ok {
			out = append(out, t)
		}
	}
	return out
}
