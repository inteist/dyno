package bench

import (
	"context"
	"errors"
	"fmt"

	"token-rate/internal/domain"
	"token-rate/internal/event"
	"token-rate/internal/llm"
	"token-rate/internal/scenario"
)

// sessionRun holds the state of one session: the models, the lifecycle, and
// the exchanger.
type sessionRun struct {
	opts      Options
	clock     Clock
	sink      event.Sink
	models    map[string]domain.Model
	scenarios map[domain.CacheMode]scenario.Scenario
	life      *lifecycle
	exchange  *exchanger
}

// newSessionRun returns the state of the session id with the selected models.
func (r *Runner) newSessionRun(id domain.SessionID, models []domain.Model) *sessionRun {
	x := &exchanger{
		client: r.deps.Client, recorder: r.deps.Recorder, clock: r.deps.Clock, session: id,
		gen: r.opts.Generation, keepAlive: r.opts.KeepAlive, timeout: r.opts.RequestTimeout,
	}
	return &sessionRun{
		opts:      r.opts,
		clock:     r.deps.Clock,
		sink:      r.deps.Sink,
		models:    byName(models),
		scenarios: r.scenarios,
		life:      newLifecycle(r.deps.Client, r.deps.Clock, r.deps.Sink, x, r.opts.KeepAlive, r.opts.Generation.ContextSize),
		exchange:  x,
	}
}

// execute runs the steps in order. It returns the final status, the number
// of failed steps, and the error of the session. A failed step does not stop
// the session, but the error counts the failed steps.
func (s *sessionRun) execute(ctx context.Context, steps []domain.Step) (domain.SessionStatus, int, error) {
	last := lastStepOfModel(steps)
	failed := 0
	for i, step := range steps {
		s.sink.Publish(event.StepStarted{Index: i, Total: len(steps), Step: step})
		err := s.runStep(ctx, step)
		switch {
		case ctx.Err() != nil:
			return domain.StatusAborted, failed, ctx.Err()
		case errors.Is(err, llm.ErrUnavailable):
			return domain.StatusFailed, failed, err
		case err != nil:
			failed++
			s.sink.Publish(event.StepFailed{Step: step, Err: err})
		}
		if i == last[step.Model] {
			// A model that fails to unload stays held, and releaseAll
			// reports the error at the end.
			_ = s.life.release(ctx, step.Model)
		}
		if i < len(steps)-1 {
			if err := s.wait(ctx); err != nil {
				return domain.StatusAborted, failed, err
			}
		}
	}
	return domain.StatusCompleted, failed, failedSteps(failed, len(steps))
}

// runStep makes the model ready, prepares the cache, and sends the measured
// run of step.
func (s *sessionRun) runStep(ctx context.Context, step domain.Step) error {
	model := s.models[step.Model]
	if err := s.life.ensureReady(ctx, step, model); err != nil {
		return fmt.Errorf("make %s ready: %w", model.Name, err)
	}
	sc := s.scenarios[step.Mode]
	conv, err := sc.Prepare(ctx, s.executor(step, model, sc.Primers()))
	if err != nil {
		return fmt.Errorf("prepare %s %s: %w", model.Name, step.Mode, err)
	}
	x, _, err := s.exchange.send(ctx, step, model, domain.KindRun, conv, s.opts.Generation.MaxTokens, s.progress(step))
	if err != nil {
		return fmt.Errorf("run %d of %s %s: %w", step.Run, model.Name, step.Mode, err)
	}
	s.sink.Publish(event.StepFinished{Step: step, Exchange: x})
	return nil
}

// executor binds a scenario.Executor to step and model.
func (s *sessionRun) executor(step domain.Step, model domain.Model, primers int) *stepExecutor {
	return &stepExecutor{life: s.life, exchange: s.exchange, sink: s.sink, step: step, model: model, primers: primers}
}

// progress returns a callback that publishes at most one Progress event for
// each Options.ProgressEvery.
func (s *sessionRun) progress(step domain.Step) llm.ProgressFunc {
	start := s.clock.Now()
	limit := newThrottle(s.clock, s.opts.progressEvery())
	return func(tokens int) {
		if limit.allow() {
			s.sink.Publish(event.Progress{Step: step, Tokens: tokens, Elapsed: s.clock.Now().Sub(start)})
		}
	}
}

// wait pauses for Options.Interval between two steps. It returns ctx.Err()
// if ctx ends first.
func (s *sessionRun) wait(ctx context.Context) error {
	d := s.opts.Interval
	if d <= 0 {
		return nil
	}
	s.sink.Publish(event.Waiting{Duration: d, Until: s.clock.Now().Add(d)})
	return s.clock.Sleep(ctx, d)
}

// lastStepOfModel returns the index of the last step of each model.
func lastStepOfModel(steps []domain.Step) map[string]int {
	last := make(map[string]int)
	for i, step := range steps {
		last[step.Model] = i
	}
	return last
}

// failedSteps returns an error that counts the failed steps, or nil if no
// step failed.
func failedSteps(failed, total int) error {
	if failed == 0 {
		return nil
	}
	return fmt.Errorf("%d of %d steps failed", failed, total)
}

// byName returns models by name.
func byName(models []domain.Model) map[string]domain.Model {
	out := make(map[string]domain.Model, len(models))
	for _, m := range models {
		out[m.Name] = m
	}
	return out
}
