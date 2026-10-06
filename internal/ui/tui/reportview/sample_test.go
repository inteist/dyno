package reportview

import (
	"math"
	"time"

	"dyno/internal/domain"
	"dyno/internal/report"
)

// zone keeps the golden files independent of the time zone of the machine.
var zone = time.FixedZone("CEST", 2*60*60)

// firstDay is the first evening of the sample sessions.
var firstDay = time.Date(2026, 9, 28, 21, 0, 0, 0, zone)

const (
	runsPerDay   = 3
	promptTokens = 2048
	evalTokens   = 512
)

var (
	qwen = domain.Model{Name: "qwen3.5:4b-mlx", Digest: "a80c4f17acd5c3e9b1f0d2a4"}
	lfm  = domain.Model{Name: "lfm2.5:8b", Digest: "4a6e7ed5d0e8f19c2b7a3d65"}

	workload = domain.Workload{
		PromptName: "long", PromptHash: "9b3f6c0e", NumPredict: evalTokens, NumCtx: 8192, Seed: 42,
	}
)

// profile holds the typical values of one model in one cache mode.
type profile struct {
	model   domain.Model
	mode    domain.CacheMode
	decode  float64       // tokens per second
	prefill time.Duration // server prompt time
	ttft    time.Duration // client time to the first token
	cached  float64       // the part of the prompt from the cache
}

// profiles are the three groups of the sample report.
func profiles() []profile {
	return []profile{
		{qwen, domain.CacheMiss, 56.2, 1941 * time.Millisecond, 1958 * time.Millisecond, 0},
		{qwen, domain.CacheContinue, 56.0, 56 * time.Millisecond, 71 * time.Millisecond, 0.99},
		{lfm, domain.CachePrefix, 151.3, 302 * time.Millisecond, 318 * time.Millisecond, 0.75},
	}
}

// sampleReport builds the report of days evenings with runsPerDay runs of
// each profile, plus one failed and one short run.
func sampleReport(days int) report.Report {
	return report.Build(sampleRows(days), report.DefaultDimensions(), zone)
}

// sampleRows returns the rows of sampleReport.
func sampleRows(days int) []report.Row {
	var rows []report.Row
	for day := range days {
		start := firstDay.AddDate(0, 0, day)
		session := domain.NewSessionID(start, "K3F9A2")
		for p, prof := range profiles() {
			for run := range runsPerDay {
				at := start.Add(time.Duration(p*runsPerDay+run) * time.Minute)
				rows = append(rows, sampleRow(session, at, prof, wobble(day, p, run)))
			}
		}
	}
	return append(rows, excludedRows()...)
}

// excludedRows returns one failed and one short run on the first day.
func excludedRows() []report.Row {
	session := domain.NewSessionID(firstDay, "K3F9A2")
	ps := profiles()
	failed := sampleRow(session, firstDay.Add(30*time.Minute), ps[2], 1)
	failed.Err = "connection refused"
	short := sampleRow(session, firstDay.Add(31*time.Minute), ps[0], 1)
	short.Measurement.DoneReason = "stop"
	return []report.Row{failed, short}
}

// wobble returns a factor near 1 that changes from day to day and from run
// to run, like real measurements.
func wobble(day, profile, run int) float64 {
	return 1 + 0.012*math.Sin(float64(day)*1.3+float64(profile)*2.1) + 0.004*math.Cos(float64(run)*2.7+float64(day))
}

// sampleRow returns one valid run of p, with the decode rate times f and the
// times divided by f.
func sampleRow(session domain.SessionID, at time.Time, p profile, f float64) report.Row {
	return report.Row{
		Session:       session,
		StartedAt:     at,
		OllamaVersion: "0.34.0",
		Workload:      workload,
		Model:         p.model,
		Mode:          p.mode,
		Measurement: domain.Measurement{
			Server: domain.ServerMetrics{
				PromptTokens:   promptTokens,
				CachedTokens:   int(p.cached * promptTokens),
				PromptDuration: scaled(p.prefill, 1/f),
				EvalTokens:     evalTokens,
				EvalDuration:   scaled(time.Second, evalTokens/(p.decode*f)),
			},
			Client:     domain.ClientMetrics{TimeToFirstToken: scaled(p.ttft, 1/f)},
			DoneReason: domain.DoneLength,
		},
	}
}

// scaled returns d times f.
func scaled(d time.Duration, f float64) time.Duration { return time.Duration(float64(d) * f) }

// failedReport has one group whose runs all failed.
func failedReport() report.Report {
	session := domain.NewSessionID(firstDay, "K3F9A2")
	row := sampleRow(session, firstDay, profiles()[0], 1)
	row.Err = "connection refused"
	return report.Build([]report.Row{row, row}, report.DefaultDimensions(), zone)
}
