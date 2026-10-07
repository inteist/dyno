package bench

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/llm"
)

// The wait until an unloaded model leaves the running list.
const (
	unloadPoll    = 100 * time.Millisecond
	unloadTimeout = 30 * time.Second
)

// The warm-up request. It runs the model once after a load, so that the
// first measured request does not pay for the start of the runner.
const (
	warmUpPrompt = "Say hi."
	warmUpTokens = 4
)

// readiness is the state of a model that the session holds.
type readiness int

const (
	cold readiness = iota + 1 // the model needs a warm-up
	warm                      // the session warmed the model up
)

// lifecycle tracks readiness separately from ownership. Managed models are
// loaded/reloaded/unloaded; external models are only warmed up once per session.
type lifecycle struct {
	managed   llm.ManagedLifecycle
	resetter  llm.CacheResetter
	clock     Clock
	sink      event.Sink
	exchange  *exchanger
	keepAlive time.Duration
	numCtx    int                  // the context size of the chats, also for loads
	held      map[string]readiness // managed models that the session must release
	ready     map[string]bool      // external readiness, never ownership
}

// newLifecycle returns a lifecycle that holds no model.
func newLifecycle(client llm.Client, clock Clock, sink event.Sink, x *exchanger, keepAlive time.Duration, numCtx int) *lifecycle {
	managed, _ := client.(llm.ManagedLifecycle)
	resetter, _ := client.(llm.CacheResetter)
	return &lifecycle{
		managed: managed, resetter: resetter, clock: clock, sink: sink, exchange: x,
		keepAlive: keepAlive, numCtx: numCtx, held: map[string]readiness{}, ready: map[string]bool{},
	}
}

// ensureReady makes model ready for step. It loads the model if the server
// does not run it. It sends one warm-up after each load, and one to a model
// that ran before this session warmed it. An external model needs only one
// warm-up, without discovery polling or ownership of its runner.
func (l *lifecycle) ensureReady(ctx context.Context, step domain.Step, model domain.Model) error {
	if l.managed == nil {
		if l.ready[model.Name] {
			return nil
		}
		return l.warmUp(ctx, step, model)
	}
	running, err := l.isRunning(ctx, model.Name)
	if err != nil {
		return err
	}
	if running && l.held[model.Name] == warm {
		return nil
	}
	if !running {
		if err := l.load(ctx, model.Name); err != nil {
			return err
		}
	}
	return l.warmUp(ctx, step, model)
}

// isRunning reports whether the server lists model as loaded.
func (l *lifecycle) isRunning(ctx context.Context, model string) (bool, error) {
	running, err := l.managed.Running(ctx)
	if err != nil {
		return false, fmt.Errorf("list running models: %w", err)
	}
	return slices.Contains(running, model), nil
}

// load loads model and announces the load. The session holds the model
// before the load ends: a cancel can end the request while the server still
// loads the model, and releaseAll must unload it then. A load that fails
// while the session runs loaded nothing, so the session does not hold it.
func (l *lifecycle) load(ctx context.Context, model string) error {
	l.sink.Publish(event.ModelLoading{Model: model})
	l.held[model] = cold
	took, err := l.managed.Load(ctx, model, l.keepAlive, l.numCtx)
	if err != nil {
		if ctx.Err() == nil {
			delete(l.held, model)
		}
		return fmt.Errorf("load: %w", err)
	}
	l.sink.Publish(event.ModelReady{Model: model, Load: took})
	return nil
}

// warmUp announces and sends the warm-up request.
func (l *lifecycle) warmUp(ctx context.Context, step domain.Step, model domain.Model) error {
	l.sink.Publish(event.Preparing{Step: step, Action: event.ActionWarmup})
	conv := domain.Conversation{{Role: domain.RoleUser, Content: warmUpPrompt}}
	if _, _, err := l.exchange.send(ctx, step, model, domain.KindWarmup, conv, warmUpTokens, nil); err != nil {
		return fmt.Errorf("warm-up: %w", err)
	}
	if l.managed == nil {
		l.ready[model.Name] = true
	} else {
		l.held[model.Name] = warm
	}
	return nil
}

// resetCache preserves the native reload mechanism, or invokes a real external
// cache reset. A reset failure cannot fall back to an assumed empty cache.
func (l *lifecycle) resetCache(ctx context.Context, step domain.Step, model domain.Model) error {
	if l.managed != nil {
		return l.reload(ctx, step, model)
	}
	if l.resetter == nil {
		return errors.New("external cache mode prefix requires a real cache reset")
	}
	l.sink.Publish(event.Preparing{Step: step, Action: event.ActionResetCache})
	return l.resetter.ResetCache(ctx, model.Name)
}

// reload unloads model, waits until the server no longer lists it, and makes
// it ready again. Without the wait, the next load can reuse the old runner
// and its cache.
func (l *lifecycle) reload(ctx context.Context, step domain.Step, model domain.Model) error {
	l.sink.Publish(event.Preparing{Step: step, Action: event.ActionReload})
	if err := l.unload(ctx, model.Name); err != nil {
		return err
	}
	// The session still holds the model, so that release unloads it again
	// if it stays in the running list.
	l.held[model.Name] = cold
	if err := l.waitGone(ctx, model.Name); err != nil {
		return err
	}
	return l.ensureReady(ctx, step, model)
}

// unload asks the server to unload model.
func (l *lifecycle) unload(ctx context.Context, model string) error {
	if err := l.managed.Unload(ctx, model); err != nil {
		return fmt.Errorf("unload %s: %w", model, err)
	}
	return nil
}

// waitGone polls the running list every unloadPoll until model leaves it. It
// fails after unloadTimeout.
func (l *lifecycle) waitGone(ctx context.Context, model string) error {
	deadline := l.clock.Now().Add(unloadTimeout)
	for {
		running, err := l.isRunning(ctx, model)
		if err != nil || !running {
			return err
		}
		if !l.clock.Now().Before(deadline) {
			return fmt.Errorf("%s still runs %s after the unload", model, unloadTimeout)
		}
		if err := l.clock.Sleep(ctx, unloadPoll); err != nil {
			return err
		}
	}
}

// release unloads model after its last step and announces it. It does
// nothing for a model that the session does not hold. A model that fails to
// unload stays held, so that releaseAll tries again.
func (l *lifecycle) release(ctx context.Context, model string) error {
	if l.managed == nil {
		return nil
	}
	if _, ok := l.held[model]; !ok {
		return nil
	}
	if err := l.unload(ctx, model); err != nil {
		return err
	}
	delete(l.held, model)
	l.sink.Publish(event.ModelReleased{Model: model})
	return nil
}

// releaseAll releases every model that the session still holds, in name
// order.
func (l *lifecycle) releaseAll(ctx context.Context) error {
	var errs []error
	for _, model := range slices.Sorted(maps.Keys(l.held)) {
		errs = append(errs, l.release(ctx, model))
	}
	return errors.Join(errs...)
}
