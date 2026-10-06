package report

import (
	"maps"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/stats"
)

// at returns a UTC time in October 2026. October 5 is a Monday in 2026-W41.
func at(day, hour, minute int) time.Time {
	return time.Date(2026, time.October, day, hour, minute, 0, 0, time.UTC)
}

// cest is a fixed zone two hours east of UTC.
func cest() *time.Location { return time.FixedZone("CEST", 2*3600) }

// newRow returns a valid miss run at t. Its decode rate is decode tok/s, its
// prefill time is 10×decode ms, and its TTFT is 10×decode+50 ms, so that each
// metric has its own value. valueOf returns these values.
func newRow(t time.Time, decode int) Row {
	return Row{
		Session:       "s1",
		StartedAt:     t,
		Tag:           "nightly",
		ServerVersion: "0.34.0",
		Workload:      workload("h1"),
		Model:         domain.Model{Name: "qwen3.5:4b-mlx", Digest: "0123456789abcdef"},
		Mode:          domain.CacheMiss,
		Measurement: domain.Measurement{
			Server: domain.ServerMetrics{
				PromptTokens:   1000,
				PromptDuration: time.Duration(10*decode) * time.Millisecond,
				EvalTokens:     decode,
				EvalDuration:   time.Second,
			},
			Client:     domain.ClientMetrics{TimeToFirstToken: time.Duration(10*decode+50) * time.Millisecond},
			DoneReason: domain.DoneLength,
		},
	}
}

// linearMetrics are the metrics whose value in a newRow grows linearly
// with the decode rate, so that valueOf can give it. The prefill rate falls
// with the prefill time and has its own test.
func linearMetrics() []Metric { return []Metric{MetricDecode, MetricPrefill, MetricTTFT} }

// valueOf returns the value of m for a newRow with the decode rate decode.
// m is one of linearMetrics.
func valueOf(m Metric, decode float64) float64 {
	switch m {
	case MetricPrefill:
		return 10 * decode
	case MetricTTFT:
		return 10*decode + 50
	}
	return decode
}

// workload returns a workload with the prompt hash h.
func workload(h string) domain.Workload {
	return domain.Workload{PromptName: "short", PromptHash: h, NumPredict: 512, NumCtx: 8192, Seed: 42}
}

// changed returns r with every change applied.
func changed(r Row, changes ...func(*Row)) Row {
	for _, change := range changes {
		change(&r)
	}
	return r
}

func failed(r *Row) {
	r.Err = "boom"
	r.Measurement = domain.Measurement{}
}

func short(r *Row) { r.Measurement.DoneReason = "stop" }

func cached(tokens int) func(*Row) {
	return func(r *Row) { r.Measurement.Server.CachedTokens = tokens }
}

func loaded(d time.Duration) func(*Row) {
	return func(r *Row) { r.Measurement.Server.LoadDuration = d }
}

func inMode(m domain.CacheMode) func(*Row) { return func(r *Row) { r.Mode = m } }

func inSession(s domain.SessionID) func(*Row) { return func(r *Row) { r.Session = s } }

// shuffled returns a copy of rows in a fixed random order.
func shuffled(rows []Row) []Row {
	out := slices.Clone(rows)
	rand.New(rand.NewPCG(1, 2)).Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func keysOf(groups []Group) []Key {
	keys := make([]Key, len(groups))
	for i, g := range groups {
		keys[i] = g.Key
	}
	return keys
}

func TestBuildEmpty(t *testing.T) {
	t.Parallel()
	for name, rows := range map[string][]Row{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := Build(rows, DefaultDimensions(), nil)
			if len(r.Groups) != 0 || r.Rows != 0 || r.Valid != 0 || r.Sessions != 0 {
				t.Errorf("Build() = %+v, want no groups and zero counts", r)
			}
			if !r.First.IsZero() || !r.Last.IsZero() {
				t.Errorf("First, Last = %v, %v, want zero times", r.First, r.Last)
			}
			if !slices.Equal(r.Dimensions, DefaultDimensions()) {
				t.Errorf("Dimensions = %v, want %v", r.Dimensions, DefaultDimensions())
			}
		})
	}
}

func TestBuildSummaries(t *testing.T) {
	t.Parallel()
	cont := inMode(domain.CacheContinue)
	rows := []Row{
		changed(newRow(at(5, 8, 0), 999), failed),
		changed(newRow(at(5, 9, 0), 10), cont, cached(900)),
		changed(newRow(at(5, 10, 0), 20), cont, cached(950), inSession("s2")),
		changed(newRow(at(5, 11, 0), 60), cont, cached(1000), inSession("s2")),
		changed(newRow(at(5, 12, 0), 999), short, cached(500), inSession("s3")),
		changed(newRow(at(5, 13, 0), 999), short, loaded(2*time.Second), inSession("s3")),
		changed(newRow(at(5, 14, 0), 999), loaded(2*time.Second), inSession("s4")),
	}
	r := Build(shuffled(rows), []Dimension{ByModel}, nil)

	if r.Rows != 7 || r.Valid != 3 || r.Sessions != 4 {
		t.Errorf("Rows, Valid, Sessions = %d, %d, %d, want 7, 3, 4", r.Rows, r.Valid, r.Sessions)
	}
	if !r.First.Equal(at(5, 8, 0)) || !r.Last.Equal(at(5, 14, 0)) {
		t.Errorf("report First, Last = %v, %v, want the first and the last row", r.First, r.Last)
	}
	if len(r.Groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(r.Groups))
	}
	g := r.Groups[0]
	decodes := []float64{10, 20, 60}
	for _, m := range linearMetrics() {
		values := make([]float64, len(decodes))
		for i, d := range decodes {
			values[i] = valueOf(m, d)
		}
		if got, want := g.Summary(m), stats.Summarize(values); got != want {
			t.Errorf("Summary(%s) = %+v, want %+v", m, got, want)
		}
	}
	if got, want := g.CachedShare, stats.Summarize([]float64{0.9, 0.95, 1}); got != want {
		t.Errorf("CachedShare = %+v, want %+v", got, want)
	}
	wantExcluded := map[Flag]int{FlagFailed: 1, FlagShort: 2, FlagUnexpectedHit: 1, FlagLoadInRun: 2}
	if !maps.Equal(g.Excluded, wantExcluded) {
		t.Errorf("Excluded = %v, want %v", g.Excluded, wantExcluded)
	}
	if g.Sessions != 4 {
		t.Errorf("Sessions = %d, want 4", g.Sessions)
	}
	if !g.First.Equal(at(5, 8, 0)) || !g.Last.Equal(at(5, 14, 0)) {
		t.Errorf("group First, Last = %v, %v, want the first and the last row", g.First, g.Last)
	}
}

func TestBuildGroupWithoutValidRows(t *testing.T) {
	t.Parallel()
	broken := changed(newRow(at(5, 9, 0), 10), failed)
	broken.Model.Name = "broken"
	r := Build([]Row{newRow(at(5, 8, 0), 10), broken}, []Dimension{ByModel}, nil)

	if got, want := keysOf(r.Groups), []Key{{Model: "broken"}, {Model: "qwen3.5:4b-mlx"}}; !slices.Equal(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	g := r.Groups[0]
	zero := stats.Summary{}
	if g.Decode != zero || g.Prefill != zero || g.TTFT != zero || g.CachedShare != zero {
		t.Errorf("summaries = %+v, %+v, %+v, %+v, want zero", g.Decode, g.Prefill, g.TTFT, g.CachedShare)
	}
	if want := map[Flag]int{FlagFailed: 1}; !maps.Equal(g.Excluded, want) {
		t.Errorf("Excluded = %v, want %v", g.Excluded, want)
	}
	if g.Sessions != 1 || !g.First.Equal(broken.StartedAt) || !g.Last.Equal(broken.StartedAt) {
		t.Errorf("Sessions, First, Last = %d, %v, %v, want 1 and the time of the row", g.Sessions, g.First, g.Last)
	}
	for _, m := range linearMetrics() {
		if len(g.Daily[m]) != 0 || len(g.Recent[m]) != 0 {
			t.Errorf("series of %s = %v, %v, want empty", m, g.Daily[m], g.Recent[m])
		}
	}
	if r.Rows != 2 || r.Valid != 1 {
		t.Errorf("Rows, Valid = %d, %d, want 2, 1", r.Rows, r.Valid)
	}
}

// mixedRows returns runs of two models, every mode, and four sessions over
// two weeks. Every seventh run is short.
func mixedRows() []Row {
	var rows []Row
	for i := range 84 {
		r := newRow(at(5+i/6, 8+i%6, i%60), 40+i%17)
		r.Model.Name = []string{"a", "b"}[i%2]
		r.Mode = domain.CacheModes()[i%3]
		r.Session = []domain.SessionID{"s1", "s2", "s3", "s4"}[i/21]
		if i%7 == 0 {
			short(&r)
		}
		rows = append(rows, r)
	}
	return rows
}

func TestBuildIgnoresInputOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		dims []Dimension
	}{
		{name: "default", dims: DefaultDimensions()},
		{name: "day and mode", dims: []Dimension{ByDay, ByMode}},
		{name: "week and session", dims: []Dimension{ByWeek, BySession}},
		{name: "none", dims: nil},
	}
	rows := mixedRows()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loc := cest()
			want := Build(rows, tt.dims, loc)
			got := Build(shuffled(rows), tt.dims, loc)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Build() of shuffled rows differs:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestBuildReturnsLocalTimes(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone("CEST", 2*3600)
	at := time.Date(2026, 10, 5, 23, 30, 0, 0, time.UTC) // 01:30 on 10-06 in CEST
	r := Build([]Row{{Session: "s", StartedAt: at, Mode: domain.CacheMiss}}, []Dimension{ByModel}, zone)
	if r.First.Location() != zone || r.Groups[0].Last.Location() != zone {
		t.Fatalf("times are in %v and %v, want %v", r.First.Location(), r.Groups[0].Last.Location(), zone)
	}
	if !r.First.Equal(at) || r.First.Day() != 6 {
		t.Errorf("First = %v, want the same instant on day 6", r.First)
	}
}
