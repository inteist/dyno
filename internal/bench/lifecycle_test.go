package bench

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/event"
	"token-rate/internal/event/eventtest"
	"token-rate/internal/llm/llmtest"
)

func TestPrefixReloadWaitsUntilModelIsGone(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.client.WithUnloadLag(3)
	h.opts.CacheModes = []domain.CacheMode{domain.CachePrefix}
	if _, err := h.run(t); err != nil {
		t.Fatal(err)
	}
	poll := 100 * time.Millisecond
	if got, want := h.clock.Sleeps(), []time.Duration{poll, poll, poll}; !slices.Equal(got, want) {
		t.Errorf("Sleeps() = %v, want %v", got, want)
	}
	const (
		version = llmtest.MethodVersion
		models  = llmtest.MethodModels
		running = llmtest.MethodRunning
		load    = llmtest.MethodLoad
		unload  = llmtest.MethodUnload
		chat    = llmtest.MethodChat
	)
	want := []string{
		version, models,
		running, load, chat, // ensureReady: load and warm-up
		unload, running, running, running, running, // reload: wait until a leaves the list
		running, load, chat, // ensureReady after the reload
		chat, chat, chat, // two primers and the run
		unload, // release
	}
	if got := methods(h.client); !slices.Equal(got, want) {
		t.Errorf("calls =\n%v\nwant\n%v", got, want)
	}
}

func TestPrefixReloadTimesOut(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.client.WithUnloadLag(1 << 20)
	h.opts.CacheModes = []domain.CacheMode{domain.CachePrefix}
	id, err := h.run(t)
	if err == nil || err.Error() != "1 of 1 steps failed" {
		t.Errorf("Run() error = %v", err)
	}
	if got := h.status(t, id); got != domain.StatusCompleted {
		t.Errorf("status = %s, want completed", got)
	}
	sleeps := h.clock.Sleeps()
	if len(sleeps) != 300 || h.clock.Now().Sub(testStart()) != 30*time.Second {
		t.Errorf("%d sleeps over %s, want 300 over 30s", len(sleeps), h.clock.Now().Sub(testStart()))
	}
	failed := eventtest.Of[event.StepFailed](h.events)
	if len(failed) != 1 || !strings.Contains(failed[0].Err.Error(), "reload: a still runs 30s after the unload") {
		t.Errorf("StepFailed = %+v", failed)
	}
	if loads := h.client.CallsOf(llmtest.MethodLoad); len(loads) != 1 {
		t.Errorf("%d loads, want no load after the failed wait", len(loads))
	}
	// The model can still be loaded, so the session unloads it again.
	if released := eventtest.Of[event.ModelReleased](h.events); len(released) != 1 {
		t.Errorf("ModelReleased = %+v", released)
	}
}

func TestCancelDuringReloadWait(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.client.WithUnloadLag(3)
	h.opts.CacheModes = []domain.CacheMode{domain.CachePrefix}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.clock.OnSleep(func(time.Duration) { cancel() })
	id, err := h.runner(t, h.deps()).Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
	if got := h.status(t, id); got != domain.StatusAborted {
		t.Errorf("status = %s, want aborted", got)
	}
	// The reload unloads a once, and the cleanup unloads it again, because a
	// can still be in the running list.
	if unloads := h.client.CallsOf(llmtest.MethodUnload); len(unloads) != 2 {
		t.Errorf("%d unloads, want 2", len(unloads))
	}
}

func TestLifecycleErrorsFailTheStep(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
		mode   domain.CacheMode
		want   string
	}{
		{"running", llmtest.MethodRunning, domain.CacheMiss, "make a ready: list running models: boom"},
		{"load", llmtest.MethodLoad, domain.CacheMiss, "make a ready: load: boom"},
		{"reload", llmtest.MethodUnload, domain.CachePrefix, "prepare a prefix: reload: unload a: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			boom := errors.New("boom")
			h := newHarness("a")
			h.client.WithErr(tt.method, boom)
			h.opts.CacheModes = []domain.CacheMode{tt.mode}
			if _, err := h.run(t); err == nil {
				t.Error("Run() gave no error")
			}
			failed := eventtest.Of[event.StepFailed](h.events)
			if len(failed) != 1 || !errors.Is(failed[0].Err, boom) || !strings.Contains(failed[0].Err.Error(), tt.want) {
				t.Errorf("StepFailed = %+v, want %q", failed, tt.want)
			}
		})
	}
}

func TestReleaseErrorIsRetriedAndReported(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	h := newHarness("a")
	h.client.WithErr(llmtest.MethodUnload, boom)
	id, err := h.run(t)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "unload a: boom") {
		t.Errorf("Run() error = %v, want the unload error", err)
	}
	if got := h.status(t, id); got != domain.StatusCompleted {
		t.Errorf("status = %s, want completed", got)
	}
	if unloads := h.client.CallsOf(llmtest.MethodUnload); len(unloads) != 2 {
		t.Errorf("%d unloads, want the release and one retry", len(unloads))
	}
	if released := eventtest.Of[event.ModelReleased](h.events); len(released) != 0 {
		t.Errorf("ModelReleased = %+v, want none", released)
	}
}

func TestReleaseSkipsModelsThatTheSessionDoesNotHold(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	h := newHarness("a")
	h.client.WithRunning("a").WithErr(llmtest.MethodChat, boom)
	if _, err := h.run(t); err == nil {
		t.Error("Run() gave no error")
	}
	// The warm-up of a model that ran before the session failed, so the
	// session leaves the model as it was.
	if unloads := h.client.CallsOf(llmtest.MethodUnload); len(unloads) != 0 {
		t.Errorf("unloads = %+v, want none", unloads)
	}
}

// TestFailedLoadIsNotReleased fails the load while the session runs, for
// example for lack of memory. The server loaded nothing, so the session
// sends no unload and announces no release.
func TestFailedLoadIsNotReleased(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.client.WithErr(llmtest.MethodLoad, errors.New("out of memory"))
	if _, err := h.run(t); err == nil {
		t.Fatal("Run() gave no error")
	}
	if unloads := h.client.CallsOf(llmtest.MethodUnload); len(unloads) != 0 {
		t.Errorf("unloads = %+v, want none", unloads)
	}
	if released := eventtest.Of[event.ModelReleased](h.events); len(released) != 0 {
		t.Errorf("ModelReleased = %+v, want none", released)
	}
}
