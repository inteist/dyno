package report

import (
	"time"

	"dyno/internal/domain"
	"dyno/internal/stats"
)

// Flag names a reason to leave a run out of the statistics.
type Flag string

const (
	FlagFailed        Flag = "failed"         // the request returned an error
	FlagShort         Flag = "short"          // the model stopped before num_predict
	FlagUnexpectedHit Flag = "unexpected-hit" // a miss run took more than 1 % from the cache
	FlagLoadInRun     Flag = "load-in-run"    // the server loaded the model during the run
)

// Flags returns every flag in display order.
func Flags() []Flag { return []Flag{FlagFailed, FlagShort, FlagUnexpectedHit, FlagLoadInRun} }

// Key holds the values of the grouping dimensions. Unused fields are empty.
// Bucket holds the day ("2026-10-05") or the week ("2026-W41").
type Key struct {
	Model, Digest, Server, Workload, Mode, Tag, Bucket, Session string
}

// Value returns the field of k that d fills.
func (k Key) Value(d Dimension) string {
	switch d {
	case ByModel:
		return k.Model
	case ByDigest:
		return k.Digest
	case ByServer:
		return k.Server
	case ByWorkload:
		return k.Workload
	case ByMode:
		return k.Mode
	case ByTag:
		return k.Tag
	case ByDay, ByWeek:
		return k.Bucket
	case BySession:
		return k.Session
	}
	return ""
}

// Metric selects one value of a run.
type Metric string

const (
	MetricDecode      Metric = "decode"       // tokens per second
	MetricPrefill     Metric = "prefill"      // milliseconds
	MetricPrefillRate Metric = "prefill-rate" // uncached prompt tokens per second
	MetricTTFT        Metric = "ttft"         // milliseconds
)

// Metrics returns every metric in display order.
func Metrics() []Metric {
	return []Metric{MetricDecode, MetricPrefill, MetricPrefillRate, MetricTTFT}
}

// IsRate reports whether m is in tokens per second. The other metrics are
// in milliseconds.
func (m Metric) IsRate() bool { return m == MetricDecode || m == MetricPrefillRate }

// Label returns the name and unit of m, for example "decode tok/s".
func (m Metric) Label() string {
	switch m {
	case MetricDecode:
		return "decode tok/s"
	case MetricPrefill:
		return "prefill ms"
	case MetricPrefillRate:
		return "prefill tok/s"
	case MetricTTFT:
		return "TTFT ms"
	}
	return string(m)
}

// Covers reports whether a run in mode gives a meaningful value of m. The
// prefill rate needs a miss run: after a cache hit, too few uncached tokens
// remain for a rate.
func (m Metric) Covers(mode domain.CacheMode) bool {
	return m != MetricPrefillRate || mode == domain.CacheMiss
}

// Value returns the value of m for one measurement.
func (m Metric) Value(x domain.Measurement) float64 {
	switch m {
	case MetricDecode:
		return x.Server.DecodeRate()
	case MetricPrefill:
		return milliseconds(x.Server.PromptDuration)
	case MetricPrefillRate:
		return x.Server.PrefillRate()
	case MetricTTFT:
		return milliseconds(x.Client.TimeToFirstToken)
	}
	return 0
}

func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// Point is one value at one time.
type Point struct {
	Time  time.Time
	Value float64
}

// RecentRuns is the number of runs in Group.Recent.
const RecentRuns = 50

// Report is the result of Build.
type Report struct {
	Dimensions  []Dimension
	Groups      []Group
	Rows        int // rows before the validity rules
	Valid       int // rows without flags
	Sessions    int
	First, Last time.Time // the first and the last run of all rows
}

// Group is the summary of the rows with one key.
type Group struct {
	Key         Key
	Decode      stats.Summary // tokens per second
	Prefill     stats.Summary // milliseconds
	PrefillRate stats.Summary // uncached prompt tokens per second
	TTFT        stats.Summary // milliseconds
	CachedShare stats.Summary // 0 to 1
	Sessions    int
	First, Last time.Time
	Excluded    map[Flag]int
	Daily       map[Metric][]Point // daily medians of the valid runs, for the charts
	Recent      map[Metric][]Point // the last RecentRuns valid runs, oldest first
}

// Summary returns the summary of m.
func (g Group) Summary(m Metric) stats.Summary {
	switch m {
	case MetricDecode:
		return g.Decode
	case MetricPrefill:
		return g.Prefill
	case MetricPrefillRate:
		return g.PrefillRate
	case MetricTTFT:
		return g.TTFT
	}
	return stats.Summary{}
}
