package bench

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/event/eventtest"
	"dyno/internal/llm"
	"dyno/internal/llm/llmtest"
)

func TestRunEventOrder(t *testing.T) {
	t.Parallel()
	h := newHarness("a", "b")
	h.client.WithRunning("b")
	h.opts.CacheModes = []domain.CacheMode{domain.CacheMiss, domain.CachePrefix}
	h.opts.Interval = time.Minute
	if _, err := h.run(t); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"SessionStarted",
		// a miss run 1: a is not loaded, so load and warm up.
		"StepStarted", "ModelLoading", "ModelReady", "Preparing", "StepFinished", "Waiting",
		// a prefix run 1: reload, warm-up, and two primers.
		"StepStarted", "Preparing", "ModelLoading", "ModelReady", "Preparing", "Preparing", "Preparing", "StepFinished",
		"ModelReleased", "Waiting",
		// b miss run 1: b ran before the session, so only a warm-up.
		"StepStarted", "Preparing", "StepFinished", "Waiting",
		// b prefix run 1.
		"StepStarted", "Preparing", "ModelLoading", "ModelReady", "Preparing", "Preparing", "Preparing", "StepFinished",
		"ModelReleased",
		"SessionEnded",
	}
	if got := h.events.Names(); !slices.Equal(got, want) {
		t.Errorf("Names() =\n%v\nwant\n%v", got, want)
	}

	var preparing []string
	for _, p := range eventtest.Of[event.Preparing](h.events) {
		preparing = append(preparing, fmt.Sprintf("%s: %s %d/%d", p.Step, p.Action, p.Index, p.Total))
	}
	wantPreparing := []string{
		"a miss run 1: warm-up 0/0",
		"a prefix run 1: reload 0/0", "a prefix run 1: warm-up 0/0",
		"a prefix run 1: primer 1/2", "a prefix run 1: primer 2/2",
		"b miss run 1: warm-up 0/0",
		"b prefix run 1: reload 0/0", "b prefix run 1: warm-up 0/0",
		"b prefix run 1: primer 1/2", "b prefix run 1: primer 2/2",
	}
	if !slices.Equal(preparing, wantPreparing) {
		t.Errorf("Preparing =\n%v\nwant\n%v", preparing, wantPreparing)
	}

	var loading, released []string
	for _, e := range eventtest.Of[event.ModelLoading](h.events) {
		loading = append(loading, e.Model)
	}
	for _, e := range eventtest.Of[event.ModelReleased](h.events) {
		released = append(released, e.Model)
	}
	if !slices.Equal(loading, []string{"a", "a", "b"}) || !slices.Equal(released, []string{"a", "b"}) {
		t.Errorf("loading = %v, released = %v", loading, released)
	}
	for _, e := range eventtest.Of[event.ModelReady](h.events) {
		if e.Load != time.Second {
			t.Errorf("ModelReady = %+v, want the load time of the fake", e)
		}
	}
	for i, e := range eventtest.Of[event.StepStarted](h.events) {
		if e.Index != i || e.Total != 4 {
			t.Errorf("StepStarted %d = %+v", i, e)
		}
	}
}

func TestRunStoresExchanges(t *testing.T) {
	t.Parallel()
	h := newHarness("a", "b")
	h.opts.CacheModes = domain.CacheModes()
	h.opts.Runs = 2
	id, err := h.run(t)
	if err != nil {
		t.Fatal(err)
	}
	chats := h.client.CallsOf(llmtest.MethodChat)
	xs := h.store.Exchanges()
	if len(chats) != len(xs) {
		t.Fatalf("%d chats and %d exchanges, want one exchange for each chat", len(chats), len(xs))
	}
	got := map[string]int{}
	runs := map[domain.Step]int{}
	for i, x := range xs {
		req := chats[i].Request
		got[fmt.Sprintf("%s %s %d", x.Kind, x.Step.Mode, req.Options.MaxTokens)]++
		if x.Kind == domain.KindRun {
			runs[x.Step]++
		}
		if x.Session != id || x.Model != testModel(x.Step.Model) || req.Model != x.Model || x.Failed() || !x.StartedAt.Equal(testStart()) {
			t.Errorf("exchange %d = %+v", i, x)
		}
		if req.KeepAlive != h.opts.KeepAlive || req.Options.ContextSize != h.opts.Generation.ContextSize {
			t.Errorf("request %d = %+v, want the keep-alive and num_ctx of the options", i, req)
		}
	}
	want := map[string]int{
		"warmup miss 4":      2, // the first load of each model
		"warmup prefix 4":    4, // each prefix reload
		"primer continue 64": 4, // one for each continue step
		"primer prefix 1":    8, // two for each prefix step
		"run miss 8":         4,
		"run continue 8":     4,
		"run prefix 8":       4,
	}
	if !maps.Equal(got, want) {
		t.Errorf("exchanges = %v, want %v", got, want)
	}
	loads := h.client.CallsOf(llmtest.MethodLoad)
	if warmups := h.store.ExchangesOf(domain.KindWarmup); len(loads) != 6 || len(warmups) != len(loads) {
		t.Errorf("%d loads and %d warm-ups, want 6 of each", len(loads), len(warmups))
	}
	for _, l := range loads {
		if l.KeepAlive != h.opts.KeepAlive || l.ContextSize != h.opts.Generation.ContextSize {
			t.Errorf("Load(%s) keep-alive = %s, context size = %d", l.Model, l.KeepAlive, l.ContextSize)
		}
	}
	for _, step := range Schedule(h.opts.Models, h.opts.Runs, h.opts.CacheModes, h.opts.Order) {
		if runs[step] != 1 {
			t.Errorf("%s has %d runs, want 1", step, runs[step])
		}
	}
}

func TestRunConversations(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.opts.CacheModes = domain.CacheModes()
	if _, err := h.run(t); err != nil {
		t.Fatal(err)
	}
	var lengths []int
	for _, c := range h.client.CallsOf(llmtest.MethodChat) {
		if c.Request.Options.MaxTokens == runTokens {
			lengths = append(lengths, len(c.Request.Conversation))
		}
	}
	// miss: system and question. continue: the first turn, the reply, and
	// the follow-up. prefix: system and question.
	if want := []int{2, 4, 2}; !slices.Equal(lengths, want) {
		t.Errorf("run conversation lengths = %v, want %v", lengths, want)
	}
	warmUp := h.client.CallsOf(llmtest.MethodChat)[0].Request.Conversation
	if want := (domain.Conversation{{Role: domain.RoleUser, Content: "Say hi."}}); !slices.Equal(warmUp, want) {
		t.Errorf("warm-up = %v, want %v", warmUp, want)
	}
}

func TestRunWaitsBetweenSteps(t *testing.T) {
	t.Parallel()
	for _, interval := range []time.Duration{0, time.Minute} {
		t.Run(interval.String(), func(t *testing.T) {
			t.Parallel()
			h := newHarness("a")
			h.opts.Runs = 3
			h.opts.Interval = interval
			if _, err := h.run(t); err != nil {
				t.Fatal(err)
			}
			var wantSleeps []time.Duration
			var wantUntil []time.Time
			if interval > 0 {
				wantSleeps = []time.Duration{interval, interval}
				wantUntil = []time.Time{testStart().Add(interval), testStart().Add(2 * interval)}
			}
			if got := h.clock.Sleeps(); !slices.Equal(got, wantSleeps) {
				t.Errorf("Sleeps() = %v, want %v", got, wantSleeps)
			}
			waits := eventtest.Of[event.Waiting](h.events)
			if len(waits) != len(wantUntil) {
				t.Fatalf("%d Waiting events, want %d", len(waits), len(wantUntil))
			}
			for i, w := range waits {
				if w.Duration != interval || !w.Until.Equal(wantUntil[i]) {
					t.Errorf("Waiting %d = %+v, want until %s", i, w, wantUntil[i])
				}
			}
		})
	}
}

// lifeTrace returns the step starts and the model releases in order.
func lifeTrace(r *eventtest.Recorder) []string {
	var out []string
	for _, e := range r.Events() {
		switch e := e.(type) {
		case event.StepStarted:
			out = append(out, "start "+e.Step.String())
		case event.ModelReleased:
			out = append(out, "release "+e.Model)
		}
	}
	return out
}

func TestRunReleasesAfterLastStep(t *testing.T) {
	t.Parallel()
	tests := []struct {
		order domain.Order
		want  []string
	}{
		{domain.OrderModel, []string{
			"start a miss run 1", "start a miss run 2", "release a",
			"start b miss run 1", "start b miss run 2", "release b",
		}},
		{domain.OrderRoundRobin, []string{
			"start a miss run 1", "start b miss run 1", "start a miss run 2", "release a",
			"start b miss run 2", "release b",
		}},
	}
	for _, tt := range tests {
		t.Run(string(tt.order), func(t *testing.T) {
			t.Parallel()
			h := newHarness("a", "b")
			h.opts.Runs = 2
			h.opts.Order = tt.order
			if _, err := h.run(t); err != nil {
				t.Fatal(err)
			}
			if got := lifeTrace(h.events); !slices.Equal(got, tt.want) {
				t.Errorf("trace =\n%v\nwant\n%v", got, tt.want)
			}
			if unloads := h.client.CallsOf(llmtest.MethodUnload); len(unloads) != 2 {
				t.Errorf("%d unloads, want one for each model", len(unloads))
			}
		})
	}
}

// assertAborted checks the end of a session that ctx stopped.
func (h *harness) assertAborted(t *testing.T, id domain.SessionID, err error) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
	if got := h.status(t, id); got != domain.StatusAborted {
		t.Errorf("status = %s, want aborted", got)
	}
	if got := h.ended(t); got.Status != domain.StatusAborted || !errors.Is(got.Err, context.Canceled) {
		t.Errorf("SessionEnded = %+v", got)
	}
	unloads := h.client.CallsOf(llmtest.MethodUnload)
	if len(unloads) != 1 || unloads[0].Model != "a" {
		t.Errorf("unloads = %+v, want a after the cancel", unloads)
	}
}

func TestRunCancelDuringRun(t *testing.T) {
	t.Parallel()
	h := newHarness("a", "b")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.client.WithChat(forRuns(func(ctx context.Context, _ llm.ChatRequest, _ llm.ProgressFunc) (llm.ChatResult, error) {
		cancel()
		return llm.ChatResult{}, ctx.Err()
	}))
	id, err := h.runner(t, h.deps()).Run(ctx)
	h.assertAborted(t, id, err)
	if xs := h.store.Exchanges(); len(xs) != 1 || xs[0].Kind != domain.KindWarmup {
		t.Errorf("exchanges = %+v, want only the warm-up", xs)
	}
	want := []string{"SessionStarted", "StepStarted", "ModelLoading", "ModelReady", "Preparing", "ModelReleased", "SessionEnded"}
	if got := h.events.Names(); !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

func TestRunCancelDuringWait(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.opts.Runs = 2
	h.opts.Interval = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.clock.OnSleep(func(time.Duration) { cancel() })
	id, err := h.runner(t, h.deps()).Run(ctx)
	h.assertAborted(t, id, err)
	if runs := h.store.ExchangesOf(domain.KindRun); len(runs) != 1 {
		t.Errorf("%d runs, want 1", len(runs))
	}
}

func TestRunFailedRequestContinues(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	h := newHarness("a")
	h.opts.Runs = 2
	calls := 0
	h.client.WithChat(forRuns(func(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
		calls++
		if calls == 1 {
			return llm.ChatResult{}, boom
		}
		return llmtest.DefaultChat(ctx, req, progress)
	}))
	id, err := h.run(t)
	if err == nil || err.Error() != "1 of 2 steps failed" {
		t.Errorf("Run() error = %v, want 1 of 2 steps failed", err)
	}
	if got := h.status(t, id); got != domain.StatusCompleted {
		t.Errorf("status = %s, want completed", got)
	}
	failed := eventtest.Of[event.StepFailed](h.events)
	if len(failed) != 1 || failed[0].Step.Run != 1 || !errors.Is(failed[0].Err, boom) {
		t.Errorf("StepFailed = %+v", failed)
	}
	if finished := eventtest.Of[event.StepFinished](h.events); len(finished) != 1 || finished[0].Step.Run != 2 {
		t.Errorf("StepFinished = %+v, want run 2", finished)
	}
	runs := h.store.ExchangesOf(domain.KindRun)
	if len(runs) != 2 || runs[0].Err != "boom" || runs[1].Failed() {
		t.Errorf("runs = %+v, want a failed run 1 and a good run 2", runs)
	}
	if got := h.ended(t); got.Status != domain.StatusCompleted || got.Failed != 1 || got.Err == nil {
		t.Errorf("SessionEnded = %+v", got)
	}
}

func TestRunRequestTimeout(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.opts.RequestTimeout = 20 * time.Millisecond
	h.client.WithChat(forRuns(func(ctx context.Context, _ llm.ChatRequest, _ llm.ProgressFunc) (llm.ChatResult, error) {
		<-ctx.Done()
		return llm.ChatResult{}, ctx.Err()
	}))
	id, err := h.run(t)
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() error = %v, want a count of failed steps", err)
	}
	if got := h.status(t, id); got != domain.StatusCompleted {
		t.Errorf("status = %s, want completed", got)
	}
	runs := h.store.ExchangesOf(domain.KindRun)
	if len(runs) != 1 || !strings.Contains(runs[0].Err, "no answer within the request timeout 20ms") {
		t.Errorf("runs = %+v, want one timed-out run", runs)
	}
	if failed := eventtest.Of[event.StepFailed](h.events); len(failed) != 1 || !errors.Is(failed[0].Err, context.DeadlineExceeded) {
		t.Errorf("StepFailed = %+v", failed)
	}
}

func TestRunServerUnavailable(t *testing.T) {
	t.Parallel()
	h := newHarness("a", "b")
	h.client.WithErr(llmtest.MethodChat, fmt.Errorf("dial: %w", llm.ErrUnavailable))
	id, err := h.run(t)
	if !errors.Is(err, llm.ErrUnavailable) {
		t.Errorf("Run() error = %v, want ErrUnavailable", err)
	}
	if got := h.status(t, id); got != domain.StatusFailed {
		t.Errorf("status = %s, want failed", got)
	}
	if got := h.ended(t); got.Status != domain.StatusFailed || !errors.Is(got.Err, llm.ErrUnavailable) {
		t.Errorf("SessionEnded = %+v", got)
	}
	if started := eventtest.Of[event.StepStarted](h.events); len(started) != 1 {
		t.Errorf("%d steps started, want 1", len(started))
	}
	if xs := h.store.Exchanges(); len(xs) != 1 || !xs[0].Failed() {
		t.Errorf("exchanges = %+v, want the failed warm-up", xs)
	}
	// The model is loaded, so the cleanup unloads it.
	if unloads := h.client.CallsOf(llmtest.MethodUnload); len(unloads) != 1 || unloads[0].Model != "a" {
		t.Errorf("unloads = %+v", unloads)
	}
}

func TestRunSaveExchangeError(t *testing.T) {
	t.Parallel()
	boom := errors.New("disk full")
	h := newHarness("a")
	h.store.FailSaveExchange(boom)
	_, err := h.run(t)
	if err == nil || err.Error() != "1 of 1 steps failed" {
		t.Errorf("Run() error = %v", err)
	}
	failed := eventtest.Of[event.StepFailed](h.events)
	if len(failed) != 1 || !errors.Is(failed[0].Err, boom) || !strings.Contains(failed[0].Err.Error(), "save exchange") {
		t.Errorf("StepFailed = %+v", failed)
	}
}

func TestRunThrottlesProgress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		every   time.Duration
		tokens  []int
		elapsed []time.Duration
	}{
		{0, []int{1, 11, 21, 31, 41}, []time.Duration{10, 110, 210, 310, 410}},
		{200 * time.Millisecond, []int{1, 21, 41}, []time.Duration{10, 210, 410}},
	}
	for _, tt := range tests {
		t.Run(tt.every.String(), func(t *testing.T) {
			t.Parallel()
			h := newHarness("a")
			h.opts.ProgressEvery = tt.every
			// The run streams 50 tokens, one every 10 ms.
			h.client.WithChat(forRuns(func(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
				for i := 1; i <= 50; i++ {
					h.clock.Advance(10 * time.Millisecond)
					progress(i)
				}
				return llmtest.DefaultChat(ctx, req, nil)
			}))
			if _, err := h.run(t); err != nil {
				t.Fatal(err)
			}
			var tokens []int
			var elapsed []time.Duration
			for _, p := range eventtest.Of[event.Progress](h.events) {
				tokens = append(tokens, p.Chunks)
				elapsed = append(elapsed, p.Elapsed/time.Millisecond)
				if p.Step != (domain.Step{Model: "a", Mode: domain.CacheMiss, Run: 1}) {
					t.Errorf("Progress step = %s", p.Step)
				}
			}
			if !slices.Equal(tokens, tt.tokens) || !slices.Equal(elapsed, tt.elapsed) {
				t.Errorf("Progress tokens = %v at %v ms, want %v at %v ms", tokens, elapsed, tt.tokens, tt.elapsed)
			}
		})
	}
}
