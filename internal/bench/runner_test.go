package bench

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"token-rate/internal/bench/benchtest"
	"token-rate/internal/domain"
	"token-rate/internal/event"
	"token-rate/internal/event/eventtest"
	"token-rate/internal/llm"
	"token-rate/internal/llm/llmtest"
)

// runTokens is the num_predict of the test sessions. It differs from the
// warm-up and primer limits, so that a fake can tell the runs apart.
const runTokens = 8

func testStart() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }

func testModel(name string) domain.Model {
	return domain.Model{Name: name, Digest: "sha256:" + name, Format: "gguf"}
}

// seqIDs gives the tokens "ID0001", "ID0002", and so on.
type seqIDs struct{ n int }

func (s *seqIDs) Next() string { s.n++; return fmt.Sprintf("ID%04d", s.n) }

// harness holds the fakes of one runner test.
type harness struct {
	client *llmtest.Fake
	store  *benchtest.Recorder
	events *eventtest.Recorder
	clock  *benchtest.Clock
	ids    *seqIDs
	opts   Options
}

// newHarness returns fakes for a session of one miss run of each model, with
// no interval.
func newHarness(models ...string) *harness {
	known := make([]domain.Model, len(models))
	for i, name := range models {
		known[i] = testModel(name)
	}
	opts := validOptions()
	opts.Models = models
	opts.Runs = 1
	opts.Interval = 0
	opts.CacheModes = []domain.CacheMode{domain.CacheMiss}
	opts.Generation.MaxTokens = runTokens
	return &harness{
		client: llmtest.New(known...),
		store:  benchtest.NewRecorder(),
		events: &eventtest.Recorder{},
		clock:  benchtest.NewClock(testStart()),
		ids:    &seqIDs{},
		opts:   opts,
	}
}

func (h *harness) deps() Deps {
	return Deps{
		Client: h.client, Recorder: h.store, Sink: h.events, Clock: h.clock, IDs: h.ids,
		Meta: Meta{Tool: "1.2.3", Host: domain.Host{Name: "mac", PowerSource: domain.PowerAC}},
	}
}

func (h *harness) runner(t *testing.T, d Deps) *Runner {
	t.Helper()
	r, err := NewRunner(d, h.opts)
	if err != nil {
		t.Fatalf("NewRunner() = %v", err)
	}
	return r
}

// run runs a session with the default dependencies.
func (h *harness) run(t *testing.T) (domain.SessionID, error) {
	t.Helper()
	return h.runner(t, h.deps()).Run(context.Background())
}

// status returns the stored status of the session id.
func (h *harness) status(t *testing.T, id domain.SessionID) domain.SessionStatus {
	t.Helper()
	rec, ok := h.store.Session(id)
	if !ok {
		t.Fatalf("session %q is not in the store", id)
	}
	return rec.Status
}

// ended returns the SessionEnded event.
func (h *harness) ended(t *testing.T) event.SessionEnded {
	t.Helper()
	ended := eventtest.Of[event.SessionEnded](h.events)
	if len(ended) != 1 {
		t.Fatalf("%d SessionEnded events, want 1", len(ended))
	}
	return ended[0]
}

// assertNothingWritten checks that the session left no trace.
func (h *harness) assertNothingWritten(t *testing.T) {
	t.Helper()
	if _, ok := h.store.Session(domain.NewSessionID(testStart(), "ID0001")); ok {
		t.Error("the store has a session")
	}
	if len(h.store.Models()) != 0 || len(h.store.Exchanges()) != 0 {
		t.Errorf("the store has models %v and exchanges %v", h.store.Models(), h.store.Exchanges())
	}
	if len(h.events.Events()) != 0 {
		t.Errorf("events = %v, want none", h.events.Names())
	}
}

// methods returns the method names of the calls of f.
func methods(f *llmtest.Fake) []string {
	var out []string
	for _, c := range f.Calls() {
		out = append(out, c.Method)
	}
	return out
}

// forRuns answers the runs with fn and every other request with
// llmtest.DefaultChat.
func forRuns(fn llmtest.ChatFunc) llmtest.ChatFunc {
	return func(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
		if req.Options.MaxTokens == runTokens {
			return fn(ctx, req, progress)
		}
		return llmtest.DefaultChat(ctx, req, progress)
	}
}

// faultyRecorder fails the methods that have an error. FinishSession stores
// the status before it fails.
type faultyRecorder struct {
	*benchtest.Recorder
	start, saveModel, finish error
}

func (r faultyRecorder) StartSession(ctx context.Context, s domain.Session) error {
	if r.start != nil {
		return r.start
	}
	return r.Recorder.StartSession(ctx, s)
}

func (r faultyRecorder) SaveModel(ctx context.Context, m domain.Model) error {
	if r.saveModel != nil {
		return r.saveModel
	}
	return r.Recorder.SaveModel(ctx, m)
}

func (r faultyRecorder) FinishSession(ctx context.Context, id domain.SessionID, status domain.SessionStatus, at time.Time) error {
	return errors.Join(r.finish, r.Recorder.FinishSession(ctx, id, status, at))
}

func TestNewRunnerReturnsValidateErrors(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.opts.Runs = 0
	r, err := NewRunner(h.deps(), h.opts)
	if r != nil || !errors.Is(err, ErrInvalidOptions) || err.Error() != h.opts.Validate().Error() {
		t.Errorf("NewRunner() = %v, %v, want the error of Validate", r, err)
	}
}

func TestNewRunnerRejectsNilDependencies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*Deps)
		want   string
	}{
		{"client", func(d *Deps) { d.Client = nil }, "dependencies Client are nil"},
		{"recorder", func(d *Deps) { d.Recorder = nil }, "dependencies Recorder are nil"},
		{"sink", func(d *Deps) { d.Sink = nil }, "dependencies Sink are nil"},
		{"clock", func(d *Deps) { d.Clock = nil }, "dependencies Clock are nil"},
		{"ids", func(d *Deps) { d.IDs = nil }, "dependencies IDs are nil"},
		{"all", func(d *Deps) { *d = Deps{} }, "dependencies Client, Recorder, Sink, Clock, IDs are nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness("a")
			d := h.deps()
			tt.change(&d)
			r, err := NewRunner(d, h.opts)
			if r != nil || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("NewRunner() = %v, %v, want an error with %q", r, err, tt.want)
			}
		})
	}
}

func TestRunStoresSessionRow(t *testing.T) {
	t.Parallel()
	h := newHarness("a", "b")
	h.opts.Models = []string{"b", "a"}
	h.opts.Tag = "nightly"
	h.client.WithVersion("0.34.1")
	id, err := h.run(t)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Session{
		ID:            "20261005T120000Z-ID0001",
		StartedAt:     testStart(),
		ToolVersion:   "1.2.3",
		OllamaVersion: "0.34.1",
		Host:          domain.Host{Name: "mac", PowerSource: domain.PowerAC},
		Tag:           "nightly",
		Workload:      h.opts.Workload(),
		Order:         domain.OrderModel,
		Interval:      0,
		Runs:          1,
		CacheModes:    []domain.CacheMode{domain.CacheMiss},
	}
	if id != want.ID {
		t.Errorf("Run() = %q, want %q", id, want.ID)
	}
	rec, ok := h.store.Session(id)
	if !ok || !reflect.DeepEqual(rec.Session, want) {
		t.Errorf("stored session =\n%+v\nwant\n%+v", rec.Session, want)
	}
	if rec.Status != domain.StatusCompleted || !rec.FinishedAt.Equal(h.clock.Now()) {
		t.Errorf("status = %s at %s", rec.Status, rec.FinishedAt)
	}
	if got := h.store.Models(); !slices.Equal(got, []domain.Model{testModel("b"), testModel("a")}) {
		t.Errorf("stored models = %v, want b and a in option order", got)
	}
	started := eventtest.Of[event.SessionStarted](h.events)
	wantSteps := Schedule(h.opts.Models, 1, h.opts.CacheModes, domain.OrderModel)
	if len(started) != 1 || !reflect.DeepEqual(started[0].Session, want) || !slices.Equal(started[0].Steps, wantSteps) {
		t.Errorf("SessionStarted = %+v", started)
	}
	if got := h.ended(t); got.ID != id || got.Status != domain.StatusCompleted || got.Failed != 0 || got.Err != nil {
		t.Errorf("SessionEnded = %+v", got)
	}
}

func TestRunMissingModelWritesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.opts.Models = []string{"x", "a", "y"}
	id, err := h.run(t)
	if !errors.Is(err, llm.ErrModelNotFound) || !strings.Contains(err.Error(), "x, y") {
		t.Errorf("Run() error = %v, want ErrModelNotFound for x and y", err)
	}
	if id != "" {
		t.Errorf("Run() = %q, want no ID", id)
	}
	h.assertNothingWritten(t)
	if got, want := methods(h.client), []string{llmtest.MethodVersion, llmtest.MethodModels}; !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestRunPreflightErrors(t *testing.T) {
	t.Parallel()
	for _, method := range []string{llmtest.MethodVersion, llmtest.MethodModels} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			boom := errors.New("boom")
			h := newHarness("a")
			h.client.WithErr(method, boom)
			id, err := h.run(t)
			if id != "" || !errors.Is(err, boom) {
				t.Errorf("Run() = %q, %v, want the error of %s", id, err, method)
			}
			h.assertNothingWritten(t)
		})
	}
}

func TestRunStartErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name       string
		rec        func(*benchtest.Recorder) faultyRecorder
		want       string
		wantStatus domain.SessionStatus // empty: no session row
	}{
		{
			name: "start session",
			rec:  func(r *benchtest.Recorder) faultyRecorder { return faultyRecorder{Recorder: r, start: boom} },
			want: "start session: boom",
		},
		{
			name:       "save model",
			rec:        func(r *benchtest.Recorder) faultyRecorder { return faultyRecorder{Recorder: r, saveModel: boom} },
			want:       "save model a: boom",
			wantStatus: domain.StatusFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness("a")
			d := h.deps()
			d.Recorder = tt.rec(h.store)
			id, err := h.runner(t, d).Run(context.Background())
			if id != "" || !errors.Is(err, boom) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Run() = %q, %v, want %q", id, err, tt.want)
			}
			rec, ok := h.store.Session(domain.NewSessionID(testStart(), "ID0001"))
			if ok != (tt.wantStatus != "") || rec.Status != tt.wantStatus {
				t.Errorf("stored session = %+v, %v, want status %q", rec, ok, tt.wantStatus)
			}
			if len(h.events.Events()) != 0 || len(h.client.CallsOf(llmtest.MethodChat)) != 0 {
				t.Errorf("events = %v, chats = %d, want none", h.events.Names(), len(h.client.CallsOf(llmtest.MethodChat)))
			}
		})
	}
}

func TestRunReportsFinishError(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	h := newHarness("a")
	d := h.deps()
	d.Recorder = faultyRecorder{Recorder: h.store, finish: boom}
	id, err := h.runner(t, d).Run(context.Background())
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "finish session: boom") {
		t.Errorf("Run() error = %v, want the finish error", err)
	}
	if got := h.ended(t); got.ID != id || !errors.Is(got.Err, boom) {
		t.Errorf("SessionEnded = %+v", got)
	}
}
