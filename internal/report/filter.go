package report

import (
	"time"

	"token-rate/internal/domain"
)

// Filter selects rows. A zero field selects everything. Since is inclusive
// and Until is exclusive.
type Filter struct {
	Since, Until time.Time
	Models       []string
	CacheModes   []domain.CacheMode
	Prompts      []string // prompt names
	Tags         []string
	Sessions     []domain.SessionID
	Workloads    []string // workload hashes
}
