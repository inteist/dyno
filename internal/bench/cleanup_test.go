package bench

import (
	"context"
	"errors"
	"testing"
	"time"

	"dyno/internal/bench/benchtest"
	"dyno/internal/domain"
	"dyno/internal/llm"
	"dyno/internal/llm/llmtest"
)

// cancelingLoad cancels the session during each load, as a user who stops
// a session while a model loads.
type cancelingLoad struct {
	*llmtest.Fake
	cancel context.CancelFunc
}

func (c cancelingLoad) Load(ctx context.Context, model string, keepAlive time.Duration, contextSize int) (time.Duration, error) {
	c.cancel()
	_, _ = c.Fake.Load(ctx, model, keepAlive, contextSize) // records the call
	return 0, ctx.Err()
}

func TestRunCancelDuringLoadUnloadsTheModel(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := h.deps()
	d.Client = cancelingLoad{Fake: h.client, cancel: cancel}
	id, err := h.runner(t, d).Run(ctx)
	h.assertAborted(t, id, err)
}

// hangingUnload never answers an unload until the context ends, as a server
// that accepts the connection and then hangs.
type hangingUnload struct{ *llmtest.Fake }

func (hangingUnload) Unload(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestRunCleanupHasALimit(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.client.WithChat(forRuns(func(ctx context.Context, _ llm.ChatRequest, _ llm.ProgressFunc) (llm.ChatResult, error) {
		cancel()
		return llm.ChatResult{}, ctx.Err()
	}))
	d := h.deps()
	d.Client = hangingUnload{h.client}
	r := h.runner(t, d)
	r.cleanup = 20 * time.Millisecond

	done := make(chan struct{})
	var id domain.SessionID
	var err error
	go func() {
		defer close(done)
		id, err = r.Run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the cleanup limit")
	}
	if !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() error = %v, want the cancel and the unload timeout", err)
	}
	if got := h.status(t, id); got != domain.StatusAborted {
		t.Errorf("status = %s, want aborted after the unload timeout", got)
	}
}

// cancelingRecorder cancels the session while it saves a model.
type cancelingRecorder struct {
	*benchtest.Recorder
	cancel context.CancelFunc
}

func (r cancelingRecorder) SaveModel(ctx context.Context, _ domain.Model) error {
	r.cancel()
	return ctx.Err()
}

func TestRunCancelDuringSaveModelIsAborted(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := h.deps()
	d.Recorder = cancelingRecorder{Recorder: h.store, cancel: cancel}
	id, err := h.runner(t, d).Run(ctx)
	if id != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %q, %v; want no ID and context.Canceled", id, err)
	}
	if got := h.status(t, domain.NewSessionID(testStart(), "ID0001")); got != domain.StatusAborted {
		t.Errorf("status = %s, want aborted", got)
	}
}
