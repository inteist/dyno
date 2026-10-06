package plain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/golden"
	"dyno/internal/report"
)

// timed is an event and its time after start.
type timed struct {
	at time.Duration
	e  event.Event
}

func step(model string, mode domain.CacheMode, run int) domain.Step {
	return domain.Step{Model: model, Mode: mode, Run: run}
}

// sessionSteps is the schedule of the sample session: two models, two runs,
// and the modes miss and prefix, in model order.
func sessionSteps() []domain.Step {
	var steps []domain.Step
	for _, model := range []string{qwen, lfm} {
		for run := 1; run <= 2; run++ {
			steps = append(steps, step(model, domain.CacheMiss, run), step(model, domain.CachePrefix, run))
		}
	}
	return steps
}

func sampleSession() domain.Session {
	return domain.Session{
		ID:        domain.NewSessionID(start, "K3F9A2QX"),
		StartedAt: start,
		Tag:       "baseline",
		Workload:  domain.Workload{PromptName: "long", NumPredict: 512, NumCtx: 8192, Seed: 42},
		Order:     domain.OrderModel,
		Interval:  30 * time.Second,
		Runs:      2,
	}
}

// waiting returns the pause after the step that ends at t.
func waiting(t time.Duration) timed {
	return timed{t, event.Waiting{Duration: 30 * time.Second, Until: start.Add(t + 30*time.Second)}}
}

func finished(at time.Duration, s domain.Step, m domain.Measurement) timed {
	return timed{at, event.StepFinished{Step: s, Exchange: domain.Exchange{Step: s, Kind: domain.KindRun, Measurement: m}}}
}

func primers(at time.Duration, s domain.Step) []timed {
	return []timed{
		{at, event.Preparing{Step: s, Action: event.ActionPrimer, Index: 1, Total: 2}},
		{at + 3*time.Second, event.Preparing{Step: s, Action: event.ActionPrimer, Index: 2, Total: 2}},
	}
}

// sessionStream returns the events of the sample session in order, with
// one failed step, a reload, and a lossy Progress event.
func sessionStream() []timed {
	steps := sessionSteps()
	sec := time.Second
	stream := []timed{
		{0, event.SessionStarted{Session: sampleSession(), Steps: steps}},
		{0, event.StepStarted{Index: 0, Total: 8, Step: steps[0]}},
		{0, event.ModelLoading{Model: qwen}},
		{4 * sec, event.ModelReady{Model: qwen, Load: 3300 * time.Millisecond}},
		{4 * sec, event.Preparing{Step: steps[0], Action: event.ActionWarmup, Index: 1, Total: 1}},
		{9 * sec, event.Progress{Step: steps[0], Tokens: 214, Elapsed: 3 * sec}},
		finished(14*sec, steps[0], measurement(9242*time.Millisecond, 2207*time.Millisecond, 2236*time.Millisecond, 0)),
		waiting(14 * sec),
		{44 * sec, event.StepStarted{Index: 1, Total: 8, Step: steps[1]}},
	}
	stream = append(stream, primers(44*sec, steps[1])...)
	stream = append(stream,
		finished(57*sec, steps[1], measurement(9134*time.Millisecond, 302*time.Millisecond, 318*time.Millisecond, 1500)),
		waiting(57*sec),
		timed{87 * sec, event.StepStarted{Index: 2, Total: 8, Step: steps[2]}},
		timed{88 * sec, event.StepFailed{Step: steps[2], Err: fmt.Errorf("chat %s: %w", qwen, io.ErrUnexpectedEOF)}},
		waiting(88*sec),
		timed{118 * sec, event.StepStarted{Index: 3, Total: 8, Step: steps[3]}},
	)
	stream = append(stream, primers(118*sec, steps[3])...)
	stream = append(stream,
		finished(131*sec, steps[3], measurement(9135*time.Millisecond, 301*time.Millisecond, 317*time.Millisecond, 1500)),
		timed{131 * sec, event.ModelReleased{Model: qwen}},
		waiting(131*sec),
		timed{161 * sec, event.StepStarted{Index: 4, Total: 8, Step: steps[4]}},
		timed{161 * sec, event.ModelLoading{Model: lfm}},
		timed{168 * sec, event.ModelReady{Model: lfm, Load: 6810 * time.Millisecond}},
		timed{168 * sec, event.Preparing{Step: steps[4], Action: event.ActionWarmup, Index: 1, Total: 1}},
		finished(173*sec, steps[4], measurement(3378*time.Millisecond, 998*time.Millisecond, 1013*time.Millisecond, 0)),
		waiting(173*sec),
		timed{203 * sec, event.StepStarted{Index: 5, Total: 8, Step: steps[5]}},
	)
	stream = append(stream, primers(203*sec, steps[5])...)
	stream = append(stream,
		finished(213*sec, steps[5], measurement(3392*time.Millisecond, 121*time.Millisecond, 134*time.Millisecond, 1498)),
		waiting(213*sec),
		timed{243 * sec, event.StepStarted{Index: 6, Total: 8, Step: steps[6]}},
		timed{243 * sec, event.Preparing{Step: steps[6], Action: event.ActionReload, Index: 1, Total: 1}},
		finished(250*sec, steps[6], measurement(3393*time.Millisecond, 1001*time.Millisecond, 1015*time.Millisecond, 0)),
		waiting(250*sec),
		timed{280 * sec, event.StepStarted{Index: 7, Total: 8, Step: steps[7]}},
	)
	stream = append(stream, primers(280*sec, steps[7])...)
	return append(stream,
		finished(290*sec, steps[7], measurement(3389*time.Millisecond, 122*time.Millisecond, 134*time.Millisecond, 1498)),
		timed{291 * sec, event.ModelReleased{Model: lfm}},
		timed{291 * sec, event.SessionEnded{
			ID: sampleSession().ID, Status: domain.StatusCompleted, Failed: 1, Err: errors.New("1 of 8 steps failed"),
		}},
	)
}

func TestSessionStreamGolden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	var now time.Time
	sink := NewSessionUI(&buf, func() time.Time { return now }).Sink()
	for _, te := range sessionStream() {
		now = start.Add(te.at)
		sink.Publish(te.e)
	}
	golden.Assert(t, "session", buf.String())
}

func TestSessionLines(t *testing.T) {
	t.Parallel()
	s := step(qwen, domain.CacheContinue, 3)
	tests := []struct {
		name string
		e    event.Event
		want string
	}{
		{
			name: "session without tag",
			e:    event.SessionStarted{Session: domain.Session{ID: "S", Workload: domain.Workload{PromptName: "short"}}, Steps: []domain.Step{s}},
			want: "session S · 1 step · prompt short",
		},
		{name: "step started", e: event.StepStarted{Step: s}},
		{name: "progress", e: event.Progress{Step: s, Tokens: 3}},
		{
			name: "action without a count",
			e:    event.Preparing{Step: s, Action: event.ActionReload},
			want: "qwen3.5:4b-mlx  continue  run 3  reload",
		},
		{
			name: "failed without an error",
			e:    event.StepFailed{Step: s},
			want: "qwen3.5:4b-mlx  continue  run 3  failed",
		},
		{
			name: "result without tokens",
			e:    event.StepFinished{Step: s},
			want: "qwen3.5:4b-mlx  continue  run 3  decode 0.0 tok/s · prefill 0 ms · ttft 0 ms · cached 0 %",
		},
		{
			name: "long wait",
			e:    event.Waiting{Duration: 10 * time.Minute},
			want: "next run in 10 min",
		},
		{
			name: "aborted",
			e:    event.SessionEnded{ID: "S", Status: domain.StatusAborted, Err: context.Canceled},
			want: "session S aborted · 0 failed steps",
		},
		{
			name: "completed",
			e:    event.SessionEnded{ID: "S", Status: domain.StatusCompleted},
			want: "session S completed · 0 failed steps",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			sink := NewSessionUI(&buf, fixedNow).Sink()
			sink.Publish(event.SessionStarted{Steps: []domain.Step{s}})
			buf.Reset()
			sink.Publish(tt.e)
			want := ""
			if tt.want != "" {
				want = "14:02:12  " + tt.want + "\n"
			}
			if got := buf.String(); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestSessionLinesBeforeStart(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	NewSessionUI(&buf, fixedNow).Sink().Publish(event.StepFailed{Step: step(lfm, domain.CacheMiss, 1), Err: errDiskFull})
	if got, want := buf.String(), "14:02:12  lfm2.5:8b  miss  run 1  failed: disk full\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSummaryGolden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	NewSessionUI(&buf, fixedNow).ShowSummary(sessionReport(), allTimeReport())
	golden.Assert(t, "summary", buf.String())
}

func TestSummaryOfEmptySession(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	NewSessionUI(&buf, fixedNow).ShowSummary(report.Report{}, report.Report{})
	want := "\nthis session · no runs match\n\nall sessions with this workload · no runs match\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSummaryDropsWriteErrors(t *testing.T) {
	t.Parallel()
	for ok := range 4 {
		NewSessionUI(&failingWriter{ok: ok}, fixedNow).ShowSummary(sessionReport(), allTimeReport())
	}
}

func TestRunCallsSession(t *testing.T) {
	t.Parallel()
	type ctxKey struct{}
	ctx := context.WithValue(t.Context(), ctxKey{}, "value")
	ui := NewSessionUI(io.Discard, fixedNow)
	var got any
	err := ui.Run(ctx, func(ctx context.Context) error {
		got = ctx.Value(ctxKey{})
		return errDiskFull
	})
	if !errors.Is(err, errDiskFull) {
		t.Errorf("Run() = %v, want %v", err, errDiskFull)
	}
	if got != "value" {
		t.Errorf("session got the context value %v, want the context of Run", got)
	}
}

func TestSinkIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	ui := NewSessionUI(&buf, fixedNow)
	sink := ui.Sink()
	s := step(qwen, domain.CacheMiss, 1)
	const goroutines, events = 8, 50
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range events {
				sink.Publish(event.SessionStarted{Steps: []domain.Step{s}})
				sink.Publish(event.StepFinished{Step: s})
				sink.Publish(event.Progress{Step: s})
			}
		})
	}
	wg.Go(func() { ui.ShowSummary(report.Report{}, report.Report{}) })
	wg.Wait()
	var results int
	for _, line := range lines(buf.String()) {
		if strings.Contains(line, "decode") {
			results++
		}
	}
	if want := goroutines * events; results != want {
		t.Errorf("got %d result lines, want %d", results, want)
	}
}
