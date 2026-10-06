package plain

import (
	"context"
	"errors"
	"strings"
	"time"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/report"
	"dyno/internal/stats"
)

// sessionUI and reportUI copy the ports of plan section 8.7, which the app
// package declares.
type (
	sessionUI interface {
		Sink() event.Sink
		Run(ctx context.Context, session func(context.Context) error) error
		ShowSummary(session, allTime report.Report)
	}
	reportUI interface {
		Show(ctx context.Context, r report.Report) error
	}
)

var (
	_ sessionUI = (*SessionUI)(nil)
	_ reportUI  = (*ReportUI)(nil)
)

// zone keeps the golden files independent of the time zone of the machine.
var zone = time.FixedZone("CEST", 2*60*60)

// start is the start of the sample session.
var start = time.Date(2026, 10, 5, 14, 2, 12, 0, zone)

const (
	qwen = "qwen3.5:4b-mlx"
	lfm  = "lfm2.5:8b"
)

var errDiskFull = errors.New("disk full")

// failingWriter accepts ok writes and fails every later write.
type failingWriter struct{ ok int }

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.ok == 0 {
		return 0, errDiskFull
	}
	w.ok--
	return len(p), nil
}

// measurement returns a run of 512 tokens with a prompt of 2,000 tokens.
func measurement(eval, prefill, ttft time.Duration, cached int) domain.Measurement {
	return domain.Measurement{
		Server: domain.ServerMetrics{
			PromptTokens:   2000,
			CachedTokens:   cached,
			PromptDuration: prefill,
			EvalTokens:     512,
			EvalDuration:   eval,
		},
		Client:     domain.ClientMetrics{TimeToFirstToken: ttft, Wall: ttft + eval},
		DoneReason: domain.DoneLength,
	}
}

// spread returns a summary of n runs around median with the standard
// deviation stdDev.
func spread(n int, median, stdDev float64) stats.Summary {
	return stats.Summary{
		N: n, Mean: median - stdDev/10, Median: median, StdDev: stdDev,
		Min: median - 2*stdDev, Max: median + 2*stdDev,
	}
}

// median returns a summary of n runs around m with a spread of 2 %.
func median(n int, m float64) stats.Summary { return spread(n, m, m/50) }

// prefillRate returns the summary of the prefill rate of n miss runs that
// read a 2,000-token prompt in prefillMs. Only miss runs have a prefill rate.
func prefillRate(n int, prefillMs float64) stats.Summary {
	return median(n, 2000/prefillMs*1000)
}

// digestOf returns a full digest, which the tables show short.
func digestOf(model string) string {
	if model == qwen {
		return "a80c4f17acd55265feec403c7aef86be0c25983ab279d83f3bcd3abbcb5b8b72"
	}
	return "4a6e7ed5d0e8e2c4bb2f9de3f53fb7d0e61c1d2b8f4a0c9d7e6b5a4f3e2d1c0b"
}

// key returns the key of a group with the default dimensions.
func key(model string, mode domain.CacheMode) report.Key {
	return report.Key{Model: model, Digest: domain.Model{Digest: digestOf(model)}.ShortDigest(), Ollama: "0.34.0", Workload: "21e02d1b0274", Mode: string(mode)}
}

// allTimeReport is a report over many sessions with the default dimensions.
func allTimeReport() report.Report {
	return report.Report{
		Dimensions: report.DefaultDimensions(),
		Groups: []report.Group{
			{
				Key: key(qwen, domain.CacheMiss), Decode: spread(60, 56.21, 0.84),
				Prefill: median(60, 1941.3), PrefillRate: prefillRate(60, 1941.3), TTFT: median(60, 1958.4), CachedShare: median(60, 0),
				Excluded: map[report.Flag]int{report.FlagShort: 2},
			},
			{
				Key: key(qwen, domain.CachePrefix), Decode: spread(60, 56.04, 0.91),
				Prefill: median(60, 56.2), TTFT: median(60, 71.4), CachedShare: median(60, 0.99),
			},
			{
				Key: key(lfm, domain.CacheMiss), Excluded: map[report.Flag]int{report.FlagFailed: 3},
			},
			{
				Key: key(lfm, domain.CachePrefix), Decode: spread(58, 151.33, 2.07),
				Prefill: median(58, 302.4), TTFT: median(58, 318.1), CachedShare: median(58, 0.75),
				Excluded: map[report.Flag]int{report.FlagLoadInRun: 1, report.FlagFailed: 1},
			},
		},
		Rows:     186,
		Valid:    178,
		Sessions: 23,
		First:    start,
		Last:     time.Date(2026, 11, 2, 9, 15, 0, 0, zone),
	}
}

// sessionReport is the report of the sample session.
func sessionReport() report.Report {
	return report.Report{
		Dimensions: report.DefaultDimensions(),
		Groups: []report.Group{
			{
				Key: key(qwen, domain.CacheMiss), Decode: spread(1, 55.4, 0),
				Prefill: median(1, 2207.1), PrefillRate: prefillRate(1, 2207.1), TTFT: median(1, 2236), CachedShare: median(1, 0),
				Excluded: map[report.Flag]int{report.FlagFailed: 1},
			},
			{
				Key: key(qwen, domain.CachePrefix), Decode: spread(2, 56.05, 0.21),
				Prefill: median(2, 302.4), TTFT: median(2, 318.2), CachedShare: median(2, 0.75),
			},
			{
				Key: key(lfm, domain.CacheMiss), Decode: spread(2, 151.2, 1.13),
				Prefill: median(2, 998.7), PrefillRate: prefillRate(2, 998.7), TTFT: median(2, 1012.6), CachedShare: median(2, 0),
			},
			{
				Key: key(lfm, domain.CachePrefix), Decode: spread(2, 150.95, 0.35),
				Prefill: median(2, 121.5), TTFT: median(2, 133.9), CachedShare: median(2, 0.749),
			},
		},
		Rows:     8,
		Valid:    7,
		Sessions: 1,
		First:    start.Add(14 * time.Second),
		Last:     start.Add(5 * time.Minute),
	}
}

// fixedNow returns start.
func fixedNow() time.Time { return start }

// lines splits output into lines without the last line break.
func lines(output string) []string {
	return strings.Split(strings.TrimSuffix(output, "\n"), "\n")
}
