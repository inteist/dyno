package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"dyno/internal/event"
)

func TestSinkDropsOnlyLossyEventsWhenFull(t *testing.T) {
	t.Parallel()
	s := NewSink()
	for range sinkBuffer {
		s.Publish(event.Progress{})
	}
	s.Publish(event.Progress{Chunks: 1}) // dropped: the buffer is full

	sent := make(chan struct{})
	go func() {
		s.Publish(event.StepFinished{}) // waits for space
		close(sent)
	}()
	select {
	case <-sent:
		t.Fatal("a non-lossy event did not wait for space")
	case <-time.After(20 * time.Millisecond):
	}
	<-s.msgs
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("the non-lossy event did not go into the free space")
	}

	var last tea.Msg
	for len(s.msgs) > 0 {
		last = <-s.msgs
	}
	if _, ok := last.(event.StepFinished); !ok {
		t.Errorf("the last message is %T, want event.StepFinished", last)
	}
}

func TestSinkDoesNotBlockAfterClose(t *testing.T) {
	t.Parallel()
	s := NewSink()
	for range sinkBuffer {
		s.Send("filler")
	}
	s.Close()
	s.Close()
	done := make(chan struct{})
	go func() {
		s.Publish(event.SessionEnded{})
		s.Send("after close")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked after Close")
	}
}
