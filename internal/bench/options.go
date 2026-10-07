package bench

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"dyno/internal/domain"
	"dyno/internal/llm"
	"dyno/internal/prompt"
)

// ErrInvalidOptions wraps every problem that Options.Validate finds.
var ErrInvalidOptions = errors.New("invalid options")

// Defaults of Options.
const (
	DefaultProgressEvery = 100 * time.Millisecond
	minKeepAlive         = 10 * time.Minute
	keepAliveMargin      = 5 * time.Minute
)

// DefaultKeepAlive returns the larger of 10 minutes and interval plus 5
// minutes, so that the server keeps a model loaded between two runs. At the
// duration ceiling, saturate rather than wrapping back to the minimum.
func DefaultKeepAlive(interval time.Duration) time.Duration {
	if interval > time.Duration(math.MaxInt64)-keepAliveMargin {
		return time.Duration(math.MaxInt64)
	}
	return max(minKeepAlive, interval+keepAliveMargin)
}

// Options configure one session. The cli package fills them.
type Options struct {
	External       bool // server-managed lifecycle; NewRunner detects it from the client
	Models         []string
	Runs           int
	Interval       time.Duration
	Order          domain.Order
	CacheModes     []domain.CacheMode
	Prompt         prompt.Set
	Generation     llm.GenerationOptions
	KeepAlive      time.Duration
	RequestTimeout time.Duration
	Tag            string
	ProgressEvery  time.Duration // zero means DefaultProgressEvery
}

// Workload returns the workload that o describes.
func (o Options) Workload() domain.Workload {
	return domain.Workload{
		PromptName:  o.Prompt.Name,
		PromptHash:  o.Prompt.Hash(),
		NumPredict:  o.Generation.MaxTokens,
		NumCtx:      o.Generation.ContextSize,
		Temperature: o.Generation.Temperature,
		Seed:        o.Generation.Seed,
	}
}

// Validate returns all problems of o at once, wrapped in ErrInvalidOptions.
// It returns nil when o is valid.
func (o Options) Validate() error {
	problems := slices.Concat(
		o.modelProblems(),
		o.scheduleProblems(),
		o.cacheModeProblems(),
		o.requestProblems(),
	)
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w:\n%w", ErrInvalidOptions, errors.Join(problems...))
}

func (o Options) modelProblems() []error {
	if len(o.Models) == 0 {
		return []error{errors.New("at least one model is required")}
	}
	var problems []error
	if slices.Contains(o.Models, "") {
		problems = append(problems, errors.New("a model name is empty"))
	}
	problems = append(problems, duplicates("model", o.Models)...)
	return problems
}

func (o Options) scheduleProblems() []error {
	var problems []error
	if !domain.IsPrintableText(o.Tag) {
		problems = append(problems, errors.New("tag must be printable text without controls"))
	}
	if o.Runs < 1 {
		problems = append(problems, fmt.Errorf("runs must be 1 or more, not %d", o.Runs))
	}
	if o.Interval < 0 {
		problems = append(problems, fmt.Errorf("the interval must not be negative, not %s", o.Interval))
	}
	if !o.Order.Valid() {
		problems = append(problems, fmt.Errorf("unknown order %q", o.Order))
	}
	if o.External {
		if o.KeepAlive != 0 {
			problems = append(problems, errors.New("external endpoints do not support keep-alive"))
		}
	} else if o.KeepAlive <= o.Interval {
		problems = append(problems, fmt.Errorf(
			"the keep-alive %s must be longer than the interval %s, or the server unloads the model between runs",
			o.KeepAlive, o.Interval))
	}
	if o.ProgressEvery < 0 {
		problems = append(problems, fmt.Errorf("the progress interval must not be negative, not %s", o.ProgressEvery))
	}
	return problems
}

func (o Options) cacheModeProblems() []error {
	if len(o.CacheModes) == 0 {
		return []error{errors.New("at least one cache mode is required")}
	}
	problems := duplicates("cache mode", o.CacheModes)
	for _, m := range o.CacheModes {
		switch {
		case !m.Valid():
			problems = append(problems, fmt.Errorf("unknown cache mode %q", m))
		case !o.Prompt.Supports(m):
			problems = append(problems, fmt.Errorf("the prompt %q cannot serve the cache mode %s", o.Prompt.Name, m))
		}
	}
	return problems
}

func (o Options) requestProblems() []error {
	var problems []error
	g := o.Generation
	if g.MaxTokens < 1 {
		problems = append(problems, fmt.Errorf("num_predict must be 1 or more, not %d", g.MaxTokens))
	}
	if o.External {
		if g.ContextSize < 0 {
			problems = append(problems, fmt.Errorf("num_ctx must not be negative, not %d", g.ContextSize))
		}
	} else if g.ContextSize < 1 {
		problems = append(problems, fmt.Errorf("num_ctx must be 1 or more, not %d", g.ContextSize))
	}
	if g.Temperature < 0 {
		problems = append(problems, fmt.Errorf("the temperature must not be negative, not %g", g.Temperature))
	}
	if o.RequestTimeout <= 0 {
		problems = append(problems, fmt.Errorf("the request timeout must be longer than 0, not %s", o.RequestTimeout))
	}
	return problems
}

// duplicates returns one problem for each value that occurs more than once.
func duplicates[T comparable](what string, values []T) []error {
	seen := make(map[T]int, len(values))
	var problems []error
	for _, v := range values {
		seen[v]++
		if seen[v] == 2 {
			problems = append(problems, fmt.Errorf("the %s %v occurs more than once", what, v))
		}
	}
	return problems
}

// progressEvery returns ProgressEvery or its default.
func (o Options) progressEvery() time.Duration {
	if o.ProgressEvery == 0 {
		return DefaultProgressEvery
	}
	return o.ProgressEvery
}
