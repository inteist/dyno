package event_test

import (
	"slices"
	"testing"

	"dyno/internal/event"
	"dyno/internal/event/eventtest"
)

func TestTeeAndRecorder(t *testing.T) {
	t.Parallel()
	var a, b eventtest.Recorder
	sink := event.Tee(&a, &b, event.SinkFunc(func(event.Event) {}))
	sink.Publish(event.ModelLoading{Model: "m"})
	sink.Publish(event.Progress{Chunks: 3})
	sink.Publish(event.SessionEnded{})

	for _, r := range []*eventtest.Recorder{&a, &b} {
		if got, want := r.Names(), []string{"ModelLoading", "SessionEnded"}; !slices.Equal(got, want) {
			t.Errorf("Names() = %v, want %v", got, want)
		}
		if got := eventtest.Of[event.Progress](r); len(got) != 1 || got[0].Chunks != 3 {
			t.Errorf("Of[Progress]() = %v", got)
		}
		if len(r.Events()) != 3 {
			t.Errorf("Events() has %d events, want 3", len(r.Events()))
		}
	}
}

func TestOnlyProgressIsLossy(t *testing.T) {
	t.Parallel()
	all := []event.Event{
		event.SessionStarted{}, event.ModelLoading{}, event.ModelReady{}, event.ModelReleased{},
		event.StepStarted{}, event.Preparing{}, event.Progress{}, event.StepFinished{},
		event.StepFailed{}, event.Waiting{}, event.SessionEnded{},
	}
	for _, e := range all {
		_, lossy := e.(event.Lossy)
		if want := eventtest.Name(e) == "Progress"; lossy != want {
			t.Errorf("%s: lossy = %v, want %v", eventtest.Name(e), lossy, want)
		}
	}
}
