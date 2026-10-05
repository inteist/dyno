package runview

import (
	"slices"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/event"
)

// Phase is what the current step does.
type Phase int

// Phases of the current step.
const (
	PhaseNone     Phase = iota // no step has started
	PhaseStarting              // the step started, and nothing else happened yet
	PhaseLoading               // the server loads the model
	PhaseWarmup                // the warm-up request after a load
	PhaseReload                // the model reloads for an empty cache
	PhasePrimer                // a primer request fills the cache
	PhaseRunning               // the measured run streams tokens
	PhaseFinished              // the run gave a result
	PhaseFailed                // the step failed
	PhaseWaiting               // the pause before the next step
)

// State is what the run view knows about the session. Apply returns a new
// State and never changes the State that it starts from.
type State struct {
	Started bool // SessionStarted came
	Session domain.Session
	Steps   []domain.Step      // the schedule; nil before SessionStarted
	Modes   []domain.CacheMode // the cache modes of the schedule, in order
	Models  []ModelState       // one for each model, in schedule order
	Current Current
	Failed  int   // the number of failed steps
	LastErr error // the error of the last failed step
	Ended   bool  // SessionEnded came
	End     event.SessionEnded
}

// ModelState is the progress of one model.
type ModelState struct {
	Name     string
	Counts   []Count // one for each State.Modes, in the same order
	Last     LastRun // the last finished run, if HasLast
	HasLast  bool
	Loaded   bool // the runner loaded the model and has not released it
	Released bool // the runner released the model
}

// Count is the progress of one model in one cache mode.
type Count struct {
	Done   int // finished runs
	Failed int // failed steps
	Total  int // scheduled steps
}

// LastRun is a finished run.
type LastRun struct {
	Mode        domain.CacheMode
	Measurement domain.Measurement
}

// Current is the step that runs now, or the wait after it.
type Current struct {
	Index, Total int // Index is 0-based
	Step         domain.Step
	Phase        Phase
	Action       int // the 1-based index of a primer
	Actions      int // the number of primers of the step
	Tokens       int // the tokens of the run so far
	Elapsed      time.Duration
	FirstTokens  int                // the tokens at the first progress of the run
	FirstElapsed time.Duration      // the elapsed time at the first progress
	Result       domain.Measurement // the run, in PhaseFinished
	Err          error              // the failure, in PhaseFailed
	Wait         time.Duration      // the pause, in PhaseWaiting
	Until        time.Time          // the end of the pause
}

// Complete reports whether every scheduled step of m has finished or failed.
func (m ModelState) Complete() bool {
	for _, c := range m.Counts {
		if c.Done+c.Failed < c.Total {
			return false
		}
	}
	return true
}

// FailedSteps returns the number of failed steps of m.
func (m ModelState) FailedSteps() int {
	n := 0
	for _, c := range m.Counts {
		n += c.Failed
	}
	return n
}

// Rate returns the tokens per second since the first progress of the run.
// It leaves out the prefill, so it comes close to the decode rate. It
// reports false until the run has two progress events.
func (c Current) Rate() (float64, bool) {
	d := c.Elapsed - c.FirstElapsed
	if c.Phase != PhaseRunning || d <= 0 {
		return 0, false
	}
	return float64(c.Tokens-c.FirstTokens) / d.Seconds(), true
}

// Remaining returns the rest of the wait at now, rounded up to whole
// seconds. It is never more than Wait, so that a late tick does not make the
// wait look longer.
func (c Current) Remaining(now time.Time) time.Duration {
	left := max(0, min(c.Wait, c.Until.Sub(now)))
	return (left + time.Second - 1).Truncate(time.Second)
}

// Next returns the step after the current one, if the schedule has one.
func (s State) Next() (domain.Step, bool) {
	i := s.Current.Index + 1
	if s.Current.Phase == PhaseNone || i >= len(s.Steps) {
		return domain.Step{}, false
	}
	return s.Steps[i], true
}

// Apply returns the state after e.
func (s State) Apply(e event.Event) State {
	switch e := e.(type) {
	case event.SessionStarted:
		return s.started(e)
	case event.ModelLoading:
		return s.withPhase(PhaseLoading).withModel(e.Model, loading)
	case event.ModelReady:
		return s.withPhase(PhaseStarting).withModel(e.Model, ready)
	case event.ModelReleased:
		return s.withModel(e.Model, released)
	case event.StepStarted:
		s.Current = Current{Index: e.Index, Total: e.Total, Step: e.Step, Phase: PhaseStarting}
		return s
	case event.Preparing:
		return s.preparing(e)
	case event.Progress:
		return s.progress(e)
	case event.StepFinished:
		return s.finished(e)
	case event.StepFailed:
		return s.failed(e)
	case event.Waiting:
		s.Current.Phase, s.Current.Wait, s.Current.Until = PhaseWaiting, e.Duration, e.Until
		return s
	case event.SessionEnded:
		s.Ended, s.End = true, e
		return s
	}
	return s
}

func (s State) started(e event.SessionStarted) State {
	s.Started = true
	s.Session = e.Session
	s.Steps = slices.Clone(e.Steps)
	s.Modes = modesOf(e.Steps)
	s.Models = modelsOf(e.Steps, s.Modes)
	return s
}

func (s State) preparing(e event.Preparing) State {
	s.Current.Step = e.Step
	s.Current.Phase = actionPhase(e.Action)
	s.Current.Action, s.Current.Actions = e.Index, e.Total
	return s
}

func (s State) progress(e event.Progress) State {
	if s.Current.Phase != PhaseRunning || s.Current.Step != e.Step {
		s.Current.FirstTokens, s.Current.FirstElapsed = e.Tokens, e.Elapsed
	}
	s.Current.Step = e.Step
	s.Current.Phase = PhaseRunning
	s.Current.Tokens, s.Current.Elapsed = e.Tokens, e.Elapsed
	return s
}

func (s State) finished(e event.StepFinished) State {
	s.Current.Step = e.Step
	s.Current.Phase = PhaseFinished
	s.Current.Result = e.Exchange.Measurement
	last := LastRun{Mode: e.Step.Mode, Measurement: e.Exchange.Measurement}
	return s.withModel(e.Step.Model, func(m *ModelState) {
		m.Last, m.HasLast = last, true
		m.count(s.Modes, e.Step.Mode, func(c *Count) { c.Done++ })
	})
}

func (s State) failed(e event.StepFailed) State {
	s.Current.Step = e.Step
	s.Current.Phase = PhaseFailed
	s.Current.Err = e.Err
	s.Failed++
	s.LastErr = e.Err
	return s.withModel(e.Step.Model, func(m *ModelState) {
		m.count(s.Modes, e.Step.Mode, func(c *Count) { c.Failed++ })
	})
}

func (s State) withPhase(p Phase) State {
	s.Current.Phase = p
	return s
}

// withModel returns s with a copy of the models in which change edited the
// row of name. It returns s unchanged for a model outside the schedule.
func (s State) withModel(name string, change func(*ModelState)) State {
	i := slices.IndexFunc(s.Models, func(m ModelState) bool { return m.Name == name })
	if i < 0 {
		return s
	}
	s.Models = slices.Clone(s.Models)
	s.Models[i].Counts = slices.Clone(s.Models[i].Counts)
	change(&s.Models[i])
	return s
}

// count applies change to the count of mode. The caller owns m.Counts.
func (m *ModelState) count(modes []domain.CacheMode, mode domain.CacheMode, change func(*Count)) {
	if j := slices.Index(modes, mode); j >= 0 {
		change(&m.Counts[j])
	}
}

func loading(m *ModelState)  { m.Loaded, m.Released = false, false }
func ready(m *ModelState)    { m.Loaded, m.Released = true, false }
func released(m *ModelState) { m.Loaded, m.Released = false, true }

// actionPhase returns the phase of a Preparing action.
func actionPhase(action string) Phase {
	switch action {
	case event.ActionWarmup:
		return PhaseWarmup
	case event.ActionReload:
		return PhaseReload
	}
	return PhasePrimer
}

// modesOf returns the cache modes of steps in the order of their first step.
func modesOf(steps []domain.Step) []domain.CacheMode {
	var modes []domain.CacheMode
	for _, st := range steps {
		if !slices.Contains(modes, st.Mode) {
			modes = append(modes, st.Mode)
		}
	}
	return modes
}

// modelsOf returns one row for each model of steps, in the order of their
// first step, with the number of steps in each mode.
func modelsOf(steps []domain.Step, modes []domain.CacheMode) []ModelState {
	var models []ModelState
	for _, st := range steps {
		i := slices.IndexFunc(models, func(m ModelState) bool { return m.Name == st.Model })
		if i < 0 {
			i = len(models)
			models = append(models, ModelState{Name: st.Model, Counts: make([]Count, len(modes))})
		}
		models[i].count(modes, st.Mode, func(c *Count) { c.Total++ })
	}
	return models
}
