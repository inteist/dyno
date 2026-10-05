package runview

import (
	"errors"
	"slices"
	"testing"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/event"
)

func TestApplyFullSession(t *testing.T) {
	t.Parallel()
	s := apply(sessionEvents())
	if !s.Started || s.Session.ID != sampleSession().ID {
		t.Errorf("session = %q, started %v", s.Session.ID, s.Started)
	}
	if want := []domain.CacheMode{domain.CacheMiss, domain.CachePrefix}; !slices.Equal(s.Modes, want) {
		t.Errorf("modes = %v, want %v", s.Modes, want)
	}
	if !slices.Equal(s.Steps, sessionSteps()) {
		t.Errorf("steps = %v, want the schedule", s.Steps)
	}
	wantCounts := map[string][]Count{
		qwen: {{Done: 1, Failed: 1, Total: 2}, {Done: 2, Total: 2}},
		lfm:  {{Done: 2, Total: 2}, {Done: 2, Total: 2}},
	}
	if len(s.Models) != 2 || s.Models[0].Name != qwen || s.Models[1].Name != lfm {
		t.Fatalf("models = %v, want qwen and lfm in schedule order", s.Models)
	}
	for _, m := range s.Models {
		if !slices.Equal(m.Counts, wantCounts[m.Name]) {
			t.Errorf("%s counts = %v, want %v", m.Name, m.Counts, wantCounts[m.Name])
		}
		if !m.Complete() || !m.Released || m.Loaded || !m.HasLast {
			t.Errorf("%s: complete %v, released %v, loaded %v, has last %v", m.Name, m.Complete(), m.Released, m.Loaded, m.HasLast)
		}
	}
	if got := s.Models[0].FailedSteps(); got != 1 {
		t.Errorf("qwen failed steps = %d, want 1", got)
	}
	if last := s.Models[1].Last; last.Mode != domain.CachePrefix || last.Measurement.Server.PromptDuration != 122*time.Millisecond {
		t.Errorf("lfm last run = %+v, want the prefix run 2", last)
	}
	if s.Failed != 1 || !errors.Is(s.LastErr, errEOF) {
		t.Errorf("failed = %d, last error = %v", s.Failed, s.LastErr)
	}
	if !s.Ended || s.End.Status != domain.StatusCompleted || s.End.Failed != 1 || !errors.Is(s.End.Err, errSteps) {
		t.Errorf("end = %v %+v", s.Ended, s.End)
	}
	if c := s.Current; c.Index != 7 || c.Total != 8 || c.Phase != PhaseFinished {
		t.Errorf("current = %d/%d in phase %d, want 7/8 finished", c.Index, c.Total, c.Phase)
	}
}

func TestApplyPhases(t *testing.T) {
	t.Parallel()
	steps := sessionSteps()
	s := steps[1]
	until := start.Add(time.Minute)
	tests := []struct {
		name  string
		e     event.Event
		check func(Current) bool
	}{
		{"step started", started(1), func(c Current) bool { return c.Phase == PhaseStarting && c.Index == 1 && c.Total == 8 && c.Step == s }},
		{"loading", event.ModelLoading{Model: qwen}, func(c Current) bool { return c.Phase == PhaseLoading }},
		{"ready", event.ModelReady{Model: qwen}, func(c Current) bool { return c.Phase == PhaseStarting }},
		{"warm-up", event.Preparing{Step: s, Action: event.ActionWarmup}, func(c Current) bool { return c.Phase == PhaseWarmup }},
		{"reload", event.Preparing{Step: s, Action: event.ActionReload}, func(c Current) bool { return c.Phase == PhaseReload }},
		{
			"primer", event.Preparing{Step: s, Action: event.ActionPrimer, Index: 2, Total: 3},
			func(c Current) bool { return c.Phase == PhasePrimer && c.Action == 2 && c.Actions == 3 },
		},
		{
			"progress", event.Progress{Step: s, Tokens: 9, Elapsed: time.Second},
			func(c Current) bool { return c.Phase == PhaseRunning && c.Tokens == 9 && c.Elapsed == time.Second },
		},
		{"finished", finished(s, measurement(time.Second, 0, 0, 0)), func(c Current) bool { return c.Phase == PhaseFinished && c.Result.Server.EvalTokens == 512 }},
		{"failed", event.StepFailed{Step: s, Err: errEOF}, func(c Current) bool { return c.Phase == PhaseFailed && errors.Is(c.Err, errEOF) }},
		{
			"waiting", event.Waiting{Duration: 30 * time.Second, Until: until},
			func(c Current) bool {
				return c.Phase == PhaseWaiting && c.Wait == 30*time.Second && c.Until.Equal(until)
			},
		},
	}
	base := apply([]event.Event{event.SessionStarted{Session: sampleSession(), Steps: steps}, started(1)})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if c := base.Apply(tt.e).Current; !tt.check(c) {
				t.Errorf("current after %T = %+v", tt.e, c)
			}
		})
	}
}

func TestApplyModelLifecycle(t *testing.T) {
	t.Parallel()
	s := apply([]event.Event{event.SessionStarted{Steps: sessionSteps()}, event.ModelLoading{Model: lfm}})
	if m := s.Models[1]; m.Loaded || m.Released {
		t.Errorf("loading: loaded %v, released %v", m.Loaded, m.Released)
	}
	s = s.Apply(event.ModelReady{Model: lfm})
	if m := s.Models[1]; !m.Loaded || m.Released {
		t.Errorf("ready: loaded %v, released %v", m.Loaded, m.Released)
	}
	s = s.Apply(event.ModelReleased{Model: lfm})
	if m := s.Models[1]; m.Loaded || !m.Released {
		t.Errorf("released: loaded %v, released %v", m.Loaded, m.Released)
	}
	if m := s.Models[0]; m.Loaded || m.Released {
		t.Errorf("the other model changed: loaded %v, released %v", m.Loaded, m.Released)
	}
}

func TestApplyDoesNotChangeEarlierStates(t *testing.T) {
	t.Parallel()
	steps := sessionSteps()
	before := apply([]event.Event{event.SessionStarted{Session: sampleSession(), Steps: steps}, started(0)})
	done := before.Apply(finished(steps[0], measurement(time.Second, 0, 0, 0)))
	failed := before.Apply(event.StepFailed{Step: steps[0], Err: errEOF})
	gone := done.Apply(event.ModelReleased{Model: qwen})

	if c := before.Models[0].Counts[0]; c != (Count{Total: 2}) {
		t.Errorf("the earlier state changed its count to %+v", c)
	}
	if before.Models[0].HasLast || before.Models[0].Released || before.Failed != 0 {
		t.Errorf("the earlier state changed: %+v", before.Models[0])
	}
	if c := done.Models[0].Counts[0]; c != (Count{Done: 1, Total: 2}) {
		t.Errorf("finished count = %+v, want one done", c)
	}
	if c := failed.Models[0].Counts[0]; c != (Count{Failed: 1, Total: 2}) {
		t.Errorf("failed count = %+v, want one failed; a sibling state leaked into it", c)
	}
	if done.Models[0].Released || !gone.Models[0].Released {
		t.Error("ModelReleased changed the state before it")
	}

	steps[0].Model = "changed"
	if before.Steps[0].Model != qwen {
		t.Error("the state shares the schedule of the event")
	}
}

func TestApplyIgnoresUnknownModelsAndEvents(t *testing.T) {
	t.Parallel()
	base := apply([]event.Event{event.SessionStarted{Steps: sessionSteps()}})
	other := step("other", domain.CacheMiss, 1)
	s := base.Apply(finished(other, domain.Measurement{})).
		Apply(event.ModelLoading{Model: "other"}).
		Apply(finished(step(qwen, domain.CacheContinue, 1), domain.Measurement{})).
		Apply(nil)
	for i, m := range s.Models {
		if !slices.Equal(m.Counts, base.Models[i].Counts) {
			t.Errorf("%s counts = %v, want %v", m.Name, m.Counts, base.Models[i].Counts)
		}
	}
	var empty State
	if got := empty.Apply(finished(other, domain.Measurement{})); got.Models != nil {
		t.Errorf("a step before the session made models: %v", got.Models)
	}
}

func TestRate(t *testing.T) {
	t.Parallel()
	s := sessionSteps()[0]
	first := event.Progress{Step: s, Tokens: 1, Elapsed: 2 * time.Second}
	base := apply([]event.Event{started(0)})
	if _, ok := base.Current.Rate(); ok {
		t.Error("a rate before any progress")
	}
	one := base.Apply(first)
	if _, ok := one.Current.Rate(); ok {
		t.Error("a rate after one progress")
	}
	two := one.Apply(event.Progress{Step: s, Tokens: 101, Elapsed: 4 * time.Second})
	if rate, ok := two.Current.Rate(); !ok || rate != 50 {
		t.Errorf("rate = %v, %v, want 50 tokens per second without the prefill", rate, ok)
	}
	next := two.Apply(started(1)).Apply(event.Progress{Step: sessionSteps()[1], Tokens: 5, Elapsed: time.Second})
	if c := next.Current; c.FirstTokens != 5 || c.FirstElapsed != time.Second {
		t.Errorf("the next step kept the first progress %d, %v", c.FirstTokens, c.FirstElapsed)
	}
}

func TestRemaining(t *testing.T) {
	t.Parallel()
	until := start.Add(time.Minute)
	c := Current{Wait: 30 * time.Second, Until: until}
	tests := []struct {
		now  time.Time
		want time.Duration
	}{
		{time.Time{}, 30 * time.Second},
		{until.Add(-40 * time.Second), 30 * time.Second},
		{until.Add(-17400 * time.Millisecond), 18 * time.Second},
		{until.Add(-time.Millisecond), time.Second},
		{until, 0},
		{until.Add(time.Second), 0},
	}
	for _, tt := range tests {
		if got := c.Remaining(tt.now); got != tt.want {
			t.Errorf("Remaining(%v before the end) = %v, want %v", until.Sub(tt.now), got, tt.want)
		}
	}
}

func TestNext(t *testing.T) {
	t.Parallel()
	steps := sessionSteps()
	var none State
	if _, ok := none.Next(); ok {
		t.Error("a next step before the session")
	}
	s := apply([]event.Event{event.SessionStarted{Steps: steps}, started(2)})
	if next, ok := s.Next(); !ok || next != steps[3] {
		t.Errorf("Next() = %v, %v, want %v", next, ok, steps[3])
	}
	if _, ok := s.Apply(event.StepStarted{Index: 7, Total: 8, Step: steps[7]}).Next(); ok {
		t.Error("a next step after the last step")
	}
}
