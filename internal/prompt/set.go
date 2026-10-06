package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"dyno/internal/domain"
)

// Set is the text of a workload.
type Set struct {
	Name     string
	System   string // shared prefix; empty for the short prompt
	Question string
	FollowUp string // second question for the continue mode
}

// Supports reports whether the set can serve mode.
func (s Set) Supports(mode domain.CacheMode) bool {
	switch mode {
	case domain.CacheMiss:
		return s.Question != ""
	case domain.CachePrefix:
		return s.System != "" && s.Question != ""
	case domain.CacheContinue:
		return s.System != "" && s.Question != "" && s.FollowUp != ""
	}
	return false
}

// SupportsAll reports whether the set can serve every mode of modes.
func (s Set) SupportsAll(modes []domain.CacheMode) bool {
	for _, m := range modes {
		if !s.Supports(m) {
			return false
		}
	}
	return true
}

// Hash returns a stable ID of the text. The name is not part of it.
func (s Set) Hash() string {
	sum := sha256.Sum256(fmt.Appendf(nil, "v1\x00%s\x00%s\x00%s", s.System, s.Question, s.FollowUp))
	return hex.EncodeToString(sum[:6])
}
