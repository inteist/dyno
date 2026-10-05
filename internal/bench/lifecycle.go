package bench

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/event"
	"token-rate/internal/llm"
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

// lifecycle loads, warms up, reloads, and unloads the models of one session.
type lifecycle struct {
	client    llm.Client
	clock     Clock
	sink      event.Sink
	exchange  *exchanger
	keepAlive time.Duration
	numCtx    int                  // the context size of the chats, also for loads
	held      map[string]readiness // the models that the session must release
}

// newLifecycle returns a lifecycle that holds no model.
func newLifecycle(client llm.Client, clock Clock, sink event.Sink, x *exchanger, keepAlive time.Duration, numCtx int) *lifecycle {
	return &lifecycle{
		client: client, clock: clock, sink: sink, exchange: x,
		keepAlive: keepAlive, numCtx: numCtx, held: map[string]readiness{},
	}
}

// ensureReady makes model ready for step. It loads the model if the server
// does not run it. It sends one warm-up after each load, and one to a model
// that ran before this session warmed it.
func (l *lifecycle) ensureReady(ctx context.Context, step domain.Step, model domain.Model) error {
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
	running, err := l.client.Running(ctx)
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
	took, err := l.client.Load(ctx, model, l.keepAlive, l.numCtx)
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
	l.held[model.Name] = warm
	return nil
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
	if err := l.client.Unload(ctx, model); err != nil {
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
