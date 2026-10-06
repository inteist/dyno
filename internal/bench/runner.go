package bench

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/llm"
	"dyno/internal/scenario"
)

// cleanupTimeout limits the cleanup after a session: the unloads and the
// final status. The cleanup ignores a cancel, so without a limit a server
// that never answers would hold the process.
const cleanupTimeout = 30 * time.Second

// Runner runs benchmark sessions. NewRunner builds it.
type Runner struct {
	deps      Deps
	opts      Options
	scenarios map[domain.CacheMode]scenario.Scenario
	cleanup   time.Duration // the limit of the cleanup; tests make it short
}

// NewRunner validates the options and the dependencies and builds one
// scenario for each cache mode. It returns the error of Options.Validate
// unchanged.
func NewRunner(d Deps, o Options) (*Runner, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if err := d.check(); err != nil {
		return nil, err
	}
	scenarios, err := newScenarios(d, o)
	if err != nil {
		return nil, err
	}
	return &Runner{deps: d, opts: o, scenarios: scenarios, cleanup: cleanupTimeout}, nil
}

// check returns an error that names every nil dependency of d.
func (d Deps) check() error {
	deps := []struct {
		name  string
		isNil bool
	}{
		{"Client", d.Client == nil},
		{"Recorder", d.Recorder == nil},
		{"Sink", d.Sink == nil},
		{"Clock", d.Clock == nil},
		{"IDs", d.IDs == nil},
	}
	var missing []string
	for _, dep := range deps {
		if dep.isNil {
			missing = append(missing, dep.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("bench: the dependencies %s are nil", strings.Join(missing, ", "))
	}
	return nil
}

// newScenarios returns one scenario for each cache mode of o.
func newScenarios(d Deps, o Options) (map[domain.CacheMode]scenario.Scenario, error) {
	out := make(map[domain.CacheMode]scenario.Scenario, len(o.CacheModes))
	for _, mode := range o.CacheModes {
		s, err := scenario.New(mode, o.Prompt, d.IDs)
		if err != nil {
			return nil, fmt.Errorf("cache mode %s: %w", mode, err)
		}
		out[mode] = s
	}
	return out, nil
}

// Run executes the session and returns its ID. Finished exchanges stay in
// the store when ctx ends early. Run returns an empty ID when the session
// ends before its first step. It returns an error when a step failed, even
// though the status is completed.
func (r *Runner) Run(ctx context.Context) (domain.SessionID, error) {
	version, models, err := r.preflight(ctx)
	if err != nil {
		return "", err
	}
	session := r.newSession(version)
	if err := r.deps.Recorder.StartSession(ctx, session); err != nil {
		return "", fmt.Errorf("start session: %w", err)
	}
	if err := r.saveModels(ctx, models); err != nil {
		return "", errors.Join(err, r.abandon(ctx, session.ID))
	}
	steps := Schedule(r.opts.Models, r.opts.Runs, r.opts.CacheModes, r.opts.Order)
	r.deps.Sink.Publish(event.SessionStarted{Session: session, Steps: steps})

	s := r.newSessionRun(session.ID, models)
	status, failed, runErr := s.execute(ctx, steps)
	runErr = errors.Join(runErr, r.end(ctx, s, session.ID, status))
	r.deps.Sink.Publish(event.SessionEnded{ID: session.ID, Status: status, Failed: failed, Err: runErr})
	return session.ID, runErr
}

// preflight returns the server version and the selected models in the order
// of the options. It writes nothing.
func (r *Runner) preflight(ctx context.Context) (string, []domain.Model, error) {
	version, err := r.deps.Client.Version(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("server version: %w", err)
	}
	all, err := r.deps.Client.Models(ctx, r.opts.Models...)
	if err != nil {
		return "", nil, fmt.Errorf("list models: %w", err)
	}
	models, err := selectModels(all, r.opts.Models)
	if err != nil {
		return "", nil, err
	}
	return version, models, nil
}

// selectModels returns the models with names in the order of names. Its
// error wraps llm.ErrModelNotFound and names every missing model.
func selectModels(all []domain.Model, names []string) ([]domain.Model, error) {
	known := byName(all)
	var models []domain.Model
	var missing []string
	for _, name := range names {
		if m, ok := known[name]; ok {
			models = append(models, m)
		} else {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", llm.ErrModelNotFound, strings.Join(missing, ", "))
	}
	return models, nil
}

// newSession returns the session row. It starts now.
func (r *Runner) newSession(version string) domain.Session {
	start := r.deps.Clock.Now()
	return domain.Session{
		ID:            domain.NewSessionID(start, r.deps.IDs.Next()),
		StartedAt:     start,
		ToolVersion:   r.deps.Meta.Tool,
		OllamaVersion: version,
		Host:          r.deps.Meta.Host,
		Tag:           r.opts.Tag,
		Workload:      r.opts.Workload(),
		Order:         r.opts.Order,
		Interval:      r.opts.Interval,
		Runs:          r.opts.Runs,
		CacheModes:    slices.Clone(r.opts.CacheModes),
	}
}

// saveModels stores the details of the selected models.
func (r *Runner) saveModels(ctx context.Context, models []domain.Model) error {
	for _, m := range models {
		if err := r.deps.Recorder.SaveModel(ctx, m); err != nil {
			return fmt.Errorf("save model %s: %w", m.Name, err)
		}
	}
	return nil
}

// end releases the models of s and stores the final status, also after a
// cancel. Each part has its own cleanup limit, so that an unload that never
// returns does not keep the status from the store.
func (r *Runner) end(ctx context.Context, s *sessionRun, id domain.SessionID, status domain.SessionStatus) error {
	return errors.Join(r.release(ctx, s), r.finish(ctx, id, status))
}

// abandon stores the final status of a session that ended before its first
// step: aborted after a cancel, else failed.
func (r *Runner) abandon(ctx context.Context, id domain.SessionID) error {
	status := domain.StatusFailed
	if ctx.Err() != nil {
		status = domain.StatusAborted
	}
	return r.finish(ctx, id, status)
}

// release unloads the models that s still holds, also after a cancel.
func (r *Runner) release(ctx context.Context, s *sessionRun) error {
	cleanup, cancel := r.cleanupContext(ctx)
	defer cancel()
	return s.life.releaseAll(cleanup)
}

// finish stores the final status of the session, also after a cancel.
func (r *Runner) finish(ctx context.Context, id domain.SessionID, status domain.SessionStatus) error {
	cleanup, cancel := r.cleanupContext(ctx)
	defer cancel()
	if err := r.deps.Recorder.FinishSession(cleanup, id, status, r.deps.Clock.Now()); err != nil {
		return fmt.Errorf("finish session: %w", err)
	}
	return nil
}

// cleanupContext returns a context that ignores the end of ctx and ends
// after the cleanup limit.
func (r *Runner) cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), r.cleanup)
}
