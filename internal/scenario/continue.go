package scenario

import (
	"context"
	"fmt"

	"token-rate/internal/domain"
)

// continuePrimerTokens limits the answer of the first turn. The answer only
// has to exist, so a short one is enough.
const continuePrimerTokens = 64

// continuation implements the continue mode: the measured request is the
// second turn of a chat. "continue" is a Go keyword.
type continuation struct{ turns }

func (continuation) Mode() domain.CacheMode { return domain.CacheContinue }

func (continuation) Primers() int { return 1 }

// Prepare sends the first turn and returns it with the reply and the
// follow-up question.
func (s continuation) Prepare(ctx context.Context, exec Executor) (domain.Conversation, error) {
	first := s.next(beforePrefix)
	reply, err := exec.Prime(ctx, first, continuePrimerTokens)
	if err != nil {
		return nil, fmt.Errorf("first turn: %w", err)
	}
	return first.With(reply, domain.Message{Role: domain.RoleUser, Content: s.set.FollowUp}), nil
}
