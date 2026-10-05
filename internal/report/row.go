package report

import (
	"context"
	"time"

	"token-rate/internal/domain"
)

// Row is one run with the session facts that the report groups by.
type Row struct {
	Session       domain.SessionID
	StartedAt     time.Time // the start of the run, not of the session
	Tag           string
	OllamaVersion string
	Workload      domain.Workload
	Model         domain.Model
	Mode          domain.CacheMode
	Measurement   domain.Measurement
	Err           string
}

// Source loads rows. The SQLite adapter implements it.
type Source interface {
	// Rows returns the runs that f selects, in the order of StartedAt.
	// Warm-ups and primers are never rows.
	Rows(ctx context.Context, f Filter) ([]Row, error)
}
