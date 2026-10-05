package scenario

import (
	"context"

	"token-rate/internal/domain"
)

// miss implements the miss mode: a new chat with the ID in front of the whole
// prompt, so that the server can reuse nothing.
type miss struct{ turns }

func (miss) Mode() domain.CacheMode { return domain.CacheMiss }

func (miss) Primers() int { return 0 }

// Prepare sends no request. It returns a new first turn.
func (s miss) Prepare(context.Context, Executor) (domain.Conversation, error) {
	return s.next(beforePrefix), nil
}
