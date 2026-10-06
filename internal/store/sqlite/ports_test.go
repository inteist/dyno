package sqlite_test

import (
	"dyno/internal/bench"
	"dyno/internal/report"
	"dyno/internal/store/sqlite"
)

// The adapter implements the ports without an import of bench.
var (
	_ bench.Recorder = (*sqlite.Recorder)(nil)
	_ report.Source  = (*sqlite.Source)(nil)
)
