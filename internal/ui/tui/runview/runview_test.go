package runview

import (
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"token-rate/internal/domain"
	"token-rate/internal/event"
	"token-rate/internal/report"
	"token-rate/internal/stats"
)

// zone keeps the golden files independent of the time zone of the machine.
var zone = time.FixedZone("CEST", 2*60*60)

// start is the start of the sample session.
var start = time.Date(2026, 10, 5, 14, 2, 12, 0, zone)

const (
	qwen = "qwen3.5:4b-mlx"
	lfm  = "lfm2.5:8b"
)

var errEOF = fmt.Errorf("chat %s: %w", qwen, io.ErrUnexpectedEOF)

// errSteps is the error of the sample session.
var errSteps = errors.New("1 of 8 steps failed")

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
		ID:         domain.NewSessionID(start, "K3F9A2QX"),
		StartedAt:  start,
		Tag:        "baseline",
		Workload:   domain.Workload{PromptName: "long", NumPredict: 512, NumCtx: 8192, Seed: 42},
		Order:      domain.OrderModel,
		Interval:   30 * time.Second,
		Runs:       2,
		CacheModes: []domain.CacheMode{domain.CacheMiss, domain.CachePrefix},
	}
}

// measurement returns a run of 512 tokens with a prompt of 2,000 tokens.
func measurement(eval, prefill, ttft time.Duration, cached int) domain.Measurement {
	return domain.Measurement{
		Server: domain.ServerMetrics{
			PromptTokens:   2000,
			CachedTokens:   cached,
			PromptDuration: prefill,
			EvalTokens:     512,
			EvalDuration:   eval,
		},
		Client:     domain.ClientMetrics{TimeToFirstToken: ttft, Wall: ttft + eval},
		DoneReason: domain.DoneLength,
	}
}

func finished(s domain.Step, m domain.Measurement) event.StepFinished {
	return event.StepFinished{Step: s, Exchange: domain.Exchange{Step: s, Kind: domain.KindRun, Measurement: m}}
}

func primers(s domain.Step) []event.Event {
	return []event.Event{
		event.Preparing{Step: s, Action: event.ActionPrimer, Index: 1, Total: 2},
		event.Preparing{Step: s, Action: event.ActionPrimer, Index: 2, Total: 2},
	}
}

// waitUntil is the end of the wait after step i of the sample session.
func waitUntil(i int) time.Time { return start.Add(time.Duration(i+1) * time.Minute) }

func waiting(i int) event.Waiting {
	return event.Waiting{Duration: 30 * time.Second, Until: waitUntil(i)}
}

func started(i int) event.StepStarted {
	return event.StepStarted{Index: i, Total: 8, Step: sessionSteps()[i]}
}

// loaded returns the events of a model load and its warm-up.
func loaded(model string, s domain.Step, load time.Duration) []event.Event {
	return []event.Event{
		event.ModelLoading{Model: model},
		event.ModelReady{Model: model, Load: load},
		event.Preparing{Step: s, Action: event.ActionWarmup},
	}
}

// runningEvents returns the sample session up to the run of step 4 (qwen,
// prefix, run 2), which streams tokens. Step 3 failed.
func runningEvents() []event.Event {
	steps := sessionSteps()
	ms := time.Millisecond
	ev := []event.Event{event.SessionStarted{Session: sampleSession(), Steps: steps}, started(0)}
	ev = append(ev, loaded(qwen, steps[0], 3300*ms)...)
	ev = append(ev,
		event.Progress{Step: steps[0], Tokens: 1, Elapsed: 2240 * ms},
		finished(steps[0], measurement(9242*ms, 2207*ms, 2236*ms, 0)),
		waiting(0),
		started(1),
	)
	ev = append(ev, primers(steps[1])...)
	ev = append(ev,
		finished(steps[1], measurement(9134*ms, 302*ms, 318*ms, 1500)),
		waiting(1),
		started(2),
		event.StepFailed{Step: steps[2], Err: errEOF},
		waiting(2),
		started(3),
	)
	ev = append(ev, primers(steps[3])...)
	return append(ev,
		event.Progress{Step: steps[3], Tokens: 1, Elapsed: 320 * ms},
		event.Progress{Step: steps[3], Tokens: 214, Elapsed: 4120 * ms},
	)
}

// waitingEvents returns the sample session up to the wait after step 5
// (lfm, miss, run 1).
func waitingEvents() []event.Event {
	steps := sessionSteps()
	ms := time.Millisecond
	ev := append(runningEvents(),
		finished(steps[3], measurement(9135*ms, 301*ms, 317*ms, 1500)),
		event.ModelReleased{Model: qwen},
		waiting(3),
		started(4),
	)
	ev = append(ev, loaded(lfm, steps[4], 6810*ms)...)
	return append(ev,
		finished(steps[4], measurement(3378*ms, 998*ms, 1013*ms, 0)),
		waiting(4),
	)
}

// sessionEvents returns every event of the sample session, with one failed
// step and a reload.
func sessionEvents() []event.Event {
	steps := sessionSteps()
	ms := time.Millisecond
	ev := append(waitingEvents(), started(5))
	ev = append(ev, primers(steps[5])...)
	ev = append(ev,
		finished(steps[5], measurement(3392*ms, 121*ms, 134*ms, 1498)),
		waiting(5),
		started(6),
		event.Preparing{Step: steps[6], Action: event.ActionReload},
	)
	ev = append(ev, loaded(lfm, steps[6], 6790*ms)...)
	ev = append(ev,
		finished(steps[6], measurement(3393*ms, 1001*ms, 1015*ms, 0)),
		waiting(6),
		started(7),
	)
	ev = append(ev, primers(steps[7])...)
	return append(ev,
		finished(steps[7], measurement(3389*ms, 122*ms, 134*ms, 1498)),
		event.ModelReleased{Model: lfm},
		event.SessionEnded{ID: sampleSession().ID, Status: domain.StatusCompleted, Failed: 1, Err: errSteps},
	)
}

func spreadOf(n int, median, stdDev float64) stats.Summary {
	return stats.Summary{N: n, Median: median, StdDev: stdDev}
}

func median(n int, m float64) stats.Summary { return stats.Summary{N: n, Median: m} }

// digestOf returns a full digest, which the tables show short.
func digestOf(model string) string {
	if model == qwen {
		return "a80c4f17acd55265feec403c7aef86be0c25983ab279d83f3bcd3abbcb5b8b72"
	}
	return "4a6e7ed5d0e8e2c4bb2f9de3f53fb7d0e61c1d2b8f4a0c9d7e6b5a4f3e2d1c0b"
}

func groupKey(model string, mode domain.CacheMode) report.Key {
	return report.Key{Model: model, Digest: domain.Model{Digest: digestOf(model)}.ShortDigest(), Ollama: "0.34.0", Mode: string(mode)}
}

// sessionReport is the report of the sample session, grouped as the app
// groups one session.
func sessionReport() report.Report {
	return report.Report{
		Dimensions: []report.Dimension{report.ByModel, report.ByMode},
		Groups: []report.Group{
			{
				Key: groupKey(qwen, domain.CacheMiss), Decode: spreadOf(1, 55.4, 0),
				Prefill: median(1, 2207.1), TTFT: median(1, 2236), CachedShare: median(1, 0),
				Excluded: map[report.Flag]int{report.FlagFailed: 1},
			},
			{
				Key: groupKey(qwen, domain.CachePrefix), Decode: spreadOf(2, 56.05, 0.21),
				Prefill: median(2, 302.4), TTFT: median(2, 318.2), CachedShare: median(2, 0.75),
			},
			{
				Key: groupKey(lfm, domain.CacheMiss), Decode: spreadOf(2, 151.2, 1.13),
				Prefill: median(2, 998.7), TTFT: median(2, 1012.6), CachedShare: median(2, 0),
			},
			{
				Key: groupKey(lfm, domain.CachePrefix), Decode: spreadOf(2, 150.95, 0.35),
				Prefill: median(2, 121.5), TTFT: median(2, 133.9), CachedShare: median(2, 0.749),
			},
		},
		Rows:     8,
		Valid:    7,
		Sessions: 1,
		First:    start.Add(14 * time.Second),
		Last:     start.Add(5 * time.Minute),
	}
}

// allTimeReport is a report over many sessions, grouped as the app groups
// all sessions.
func allTimeReport() report.Report {
	return report.Report{
		Dimensions: []report.Dimension{report.ByModel, report.ByDigest, report.ByOllama, report.ByMode},
		Groups: []report.Group{
			{
				Key: groupKey(qwen, domain.CacheMiss), Decode: spreadOf(60, 56.21, 0.84),
				Prefill: median(60, 1941.3), TTFT: median(60, 1958.4), CachedShare: median(60, 0),
				Excluded: map[report.Flag]int{report.FlagShort: 2},
			},
			{
				Key: groupKey(qwen, domain.CachePrefix), Decode: spreadOf(60, 56.04, 0.91),
				Prefill: median(60, 56.2), TTFT: median(60, 71.4), CachedShare: median(60, 0.99),
			},
			{
				Key: groupKey(lfm, domain.CacheMiss), Excluded: map[report.Flag]int{report.FlagFailed: 3},
			},
			{
				Key: groupKey(lfm, domain.CachePrefix), Decode: spreadOf(58, 151.33, 2.07),
				Prefill: median(58, 302.4), TTFT: median(58, 318.1), CachedShare: median(58, 0.75),
				Excluded: map[report.Flag]int{report.FlagLoadInRun: 1, report.FlagFailed: 1},
			},
		},
		Rows:     186,
		Valid:    178,
		Sessions: 23,
		First:    start,
		Last:     time.Date(2026, 11, 2, 9, 15, 0, 0, zone),
	}
}

// apply returns the state after events, starting from the zero State.
func apply(events []event.Event) State {
	var s State
	for _, e := range events {
		s = s.Apply(e)
	}
	return s
}

// feed sends msgs to m in order and returns the last model and command.
func feed(t *testing.T, m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, msg := range msgs {
		var next tea.Model
		next, cmd = m.Update(msg)
		var ok bool
		if m, ok = next.(Model); !ok {
			t.Fatalf("Update returned %T, want Model", next)
		}
	}
	return m, cmd
}

// events turns events into messages.
func events(ev []event.Event) []tea.Msg {
	msgs := make([]tea.Msg, len(ev))
	for i, e := range ev {
		msgs[i] = e
	}
	return msgs
}

// sized returns a new model with the window size.
func sized(t *testing.T, width, height int) Model {
	t.Helper()
	m, _ := feed(t, New(nil), tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

// feedAll is feed without the last command.
func feedAll(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	m, _ = feed(t, m, msgs...)
	return m
}

// settle sends printedMsg when m waits for the print of its final frame, as
// the program does after the print. A final frame that fits gets no print.
func settle(t *testing.T, m Model) Model {
	t.Helper()
	if m.done && !m.printed {
		return feedAll(t, m, printedMsg{})
	}
	return m
}

// plain returns the frame of m without ANSI codes.
func plain(m Model) string { return ansi.Strip(m.View().Content) }

// seen returns what the terminal shows: the final frame that the program
// printed into the history, if any, then the live frame.
func seen(m Model) string {
	if !m.inHistory {
		return plain(m)
	}
	return ansi.Strip(m.finalFrame()) + "\n" + plain(m)
}
