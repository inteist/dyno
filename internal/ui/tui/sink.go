package tui

import (
	"sync"

	tea "charm.land/bubbletea/v2"

	"dyno/internal/event"
)

// sinkBuffer is the number of messages that the sink holds before it drops
// lossy events. It covers a burst of events between two reads.
const sinkBuffer = 256

// Sink forwards events to a Bubble Tea program and never blocks the runner
// for long. When the buffer is full, it drops lossy events. It keeps all
// other events and messages, in order.
type Sink struct {
	msgs chan tea.Msg
	done chan struct{}
	stop sync.Once
}

// NewSink returns a sink with an empty buffer.
func NewSink() *Sink {
	return &Sink{msgs: make(chan tea.Msg, sinkBuffer), done: make(chan struct{})}
}

// Publish implements event.Sink. An event value is a valid tea.Msg.
func (s *Sink) Publish(e event.Event) {
	if _, lossy := e.(event.Lossy); lossy {
		select {
		case s.msgs <- e:
		default: // a newer progress event comes soon
		}
		return
	}
	s.Send(e)
}

// Send queues msg after the events that came before it. It waits for space
// in the buffer, and it returns at once after Close.
func (s *Sink) Send(msg tea.Msg) {
	select {
	case s.msgs <- msg:
	case <-s.done:
	}
}

// Forward sends the queued messages to p in order until Close.
func (s *Sink) Forward(p *tea.Program) {
	for {
		select {
		case msg := <-s.msgs:
			p.Send(msg)
		case <-s.done:
			return
		}
	}
}

// Close stops Forward and makes later Publish and Send calls return at once.
// It is safe to call Close more than once.
func (s *Sink) Close() { s.stop.Do(func() { close(s.done) }) }
