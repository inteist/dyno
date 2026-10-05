package scenario

import (
	"context"
	"errors"
	"fmt"

	"token-rate/internal/domain"
	"token-rate/internal/prompt"
)

// Executor gives a scenario the actions that it needs. The runner binds an
// executor to one step and one model.
type Executor interface {
	// Reload unloads the model, loads it again, and warms it up.
	Reload(ctx context.Context) error
	// Prime sends a request that is not measured and returns the reply.
	Prime(ctx context.Context, conv domain.Conversation, maxTokens int) (domain.Message, error)
}

// Scenario prepares the server cache for one cache mode. It returns the
// conversation of the measured request.
type Scenario interface {
	Mode() domain.CacheMode
	// Primers returns the number of Prime calls that Prepare makes.
	Primers() int
	Prepare(ctx context.Context, exec Executor) (domain.Conversation, error)
}

// New returns the scenario for mode. It fails if the mode is unknown, if set
// cannot serve the mode, or if ids is nil.
func New(mode domain.CacheMode, set prompt.Set, ids prompt.IDSource) (Scenario, error) {
	if ids == nil {
		return nil, errors.New("scenario: the ID source is nil")
	}
	s := strategy(mode, turns{set: set, ids: ids})
	if s == nil {
		return nil, fmt.Errorf("unknown cache mode %q", mode)
	}
	if !set.Supports(mode) {
		return nil, fmt.Errorf("the prompt %q cannot serve the cache mode %s", set.Name, mode)
	}
	return s, nil
}

// strategy returns the scenario of mode, or nil for an unknown mode.
func strategy(mode domain.CacheMode, t turns) Scenario {
	switch mode {
	case domain.CacheMiss:
		return miss{t}
	case domain.CacheContinue:
		return continuation{t}
	case domain.CachePrefix:
		return prefix{t}
	}
	return nil
}
