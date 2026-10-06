package scenario

import (
	"context"
	"fmt"

	"dyno/internal/domain"
)

// prefixPrimers is the number of primers before a prefix run. In the test of
// 2026-10-05, the MLX engine reused a shared prefix only from the third
// request on. The GGUF engine needed one earlier request.
const prefixPrimers = 2

// prefix implements the prefix mode: a new question after the same system
// part, on a freshly loaded model.
type prefix struct{ turns }

func (prefix) Mode() domain.CacheMode { return domain.CachePrefix }

func (prefix) Primers() int { return prefixPrimers }

// Prepare reloads the model, sends the primers, and returns a new first turn
// with the same system part.
func (s prefix) Prepare(ctx context.Context, exec Executor) (domain.Conversation, error) {
	if err := exec.Reload(ctx); err != nil {
		return nil, fmt.Errorf("reload: %w", err)
	}
	for i := range prefixPrimers {
		if _, err := exec.Prime(ctx, s.next(beforeQuestion), 1); err != nil {
			return nil, fmt.Errorf("primer %d: %w", i+1, err)
		}
	}
	return s.next(beforeQuestion), nil
}
