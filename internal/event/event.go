// Package event defines the facts that the runner publishes and the Sink
// port that the user interfaces implement.
package event

import (
	"time"

	"dyno/internal/domain"
)

// Event is a fact that the runner publishes.
type Event interface{ isEvent() }

// Lossy marks an event that a sink is permitted to drop under load.
type Lossy interface {
	Event
	lossy()
}

// Actions of a Preparing event.
const (
	ActionWarmup = "warm-up"
	ActionReload = "reload"
	ActionPrimer = "primer"
)

type (
	// SessionStarted comes first. It holds the session row and the schedule.
	SessionStarted struct {
		Session domain.Session
		Steps   []domain.Step
	}
	// ModelLoading comes before the runner loads a model.
	ModelLoading struct{ Model string }
	// ModelReady comes after a load. Load is the time that the server reports.
	ModelReady struct {
		Model string
		Load  time.Duration
	}
	// ModelReleased comes after the runner unloads a model.
	ModelReleased struct{ Model string }
	// StepStarted comes before each step. Index is 0-based.
	StepStarted struct {
		Index, Total int
		Step         domain.Step
	}
	// Preparing comes before each request that is not a run. Index is
	// 1-based and Total is the number of such actions in the step, for
	// example "primer 2 of 2".
	Preparing struct {
		Step         domain.Step
		Action       string // ActionWarmup, ActionReload, or ActionPrimer
		Index, Total int
	}
	// Progress reports the stream of a run. It is lossy.
	Progress struct {
		Step    domain.Step
		Tokens  int
		Elapsed time.Duration
	}
	// StepFinished comes after a successful run.
	StepFinished struct {
		Step     domain.Step
		Exchange domain.Exchange
	}
	// StepFailed comes after a step that did not give a run.
	StepFailed struct {
		Step domain.Step
		Err  error
	}
	// Waiting comes before the pause between two steps.
	Waiting struct {
		Duration time.Duration
		Until    time.Time
	}
	// SessionEnded comes last.
	SessionEnded struct {
		ID     domain.SessionID
		Status domain.SessionStatus
		Failed int // number of failed steps
		Err    error
	}
)

func (SessionStarted) isEvent() {}
func (ModelLoading) isEvent()   {}
func (ModelReady) isEvent()     {}
func (ModelReleased) isEvent()  {}
func (StepStarted) isEvent()    {}
func (Preparing) isEvent()      {}
func (Progress) isEvent()       {}
func (StepFinished) isEvent()   {}
func (StepFailed) isEvent()     {}
func (Waiting) isEvent()        {}
func (SessionEnded) isEvent()   {}

func (Progress) lossy() {}

// Sink receives events. Publish must return quickly.
type Sink interface{ Publish(Event) }

// SinkFunc adapts a function to Sink.
type SinkFunc func(Event)

// Publish calls f(e).
func (f SinkFunc) Publish(e Event) { f(e) }

// Tee returns a sink that publishes each event to every sink in order.
func Tee(sinks ...Sink) Sink {
	return SinkFunc(func(e Event) {
		for _, s := range sinks {
			s.Publish(e)
		}
	})
}
