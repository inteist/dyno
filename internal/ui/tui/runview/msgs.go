package runview

import (
	"time"

	"dyno/internal/report"
)

// SummaryMsg carries the reports after the session: the runs of this
// session, and all runs with the same workload.
type SummaryMsg struct {
	Session, AllTime report.Report
}

// DoneMsg says that the session function returned Err.
type DoneMsg struct{ Err error }

// StopMsg says that the parent context ended, for example by SIGTERM. The
// model handles it like ctrl+c.
type StopMsg struct{}

// tickMsg moves the countdown of a wait. It holds the time of the tick.
type tickMsg time.Time
