package sqlite_test

import (
	"token-rate/internal/bench"
	"token-rate/internal/report"
	"token-rate/internal/store/sqlite"
)

// The adapter implements the ports without an import of bench.
var (
	_ bench.Recorder = (*sqlite.Recorder)(nil)
	_ report.Source  = (*sqlite.Source)(nil)
)
