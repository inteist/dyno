package format

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"dyno/internal/report"
	"dyno/internal/stats"
)

// floatCase is one input of a function that formats a float64.
type floatCase struct {
	in   float64
	want string
}

func checkFloats(t *testing.T, name string, f func(float64) string, cases []floatCase) {
	t.Helper()
	for _, c := range cases {
		if got := f(c.in); got != c.want {
			t.Errorf("%s(%v) = %q, want %q", name, c.in, got, c.want)
		}
	}
}

func TestInt(t *testing.T) {
	t.Parallel()
	for in, want := range map[int]string{
		0:             "0",
		7:             "7",
		999:           "999",
		1000:          "1,000",
		2207:          "2,207",
		123456:        "123,456",
		1_000_000:     "1,000,000",
		-1234:         "-1,234",
		-999:          "-999",
		math.MinInt64: "-9,223,372,036,854,775,808",
		math.MaxInt64: "9,223,372,036,854,775,807",
	} {
		if got := Int(in); got != want {
			t.Errorf("Int(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n    int
		noun string
		want string
	}{
		{0, "step", "0 steps"},
		{1, "step", "1 step"},
		{27, "step", "27 steps"},
		{1000, "run", "1,000 runs"},
		{1, "failed step", "1 failed step"},
	}
	for _, tt := range tests {
		if got := Count(tt.n, tt.noun); got != tt.want {
			t.Errorf("Count(%d, %q) = %q, want %q", tt.n, tt.noun, got, tt.want)
		}
	}
}

func TestRate(t *testing.T) {
	t.Parallel()
	checkFloats(t, "Rate", Rate, []floatCase{
		{55.44, "55.4"},
		{55.45, "55.5"},
		{0, "0.0"},
		{-0.04, "0.0"},
		{-1.25, "-1.3"},
		{999.96, "1,000.0"},
		{1234.56, "1,234.6"},
		{1_000_000, "1,000,000.0"},
		{math.NaN(), Missing},
		{math.Inf(1), Missing},
	})
}

func TestMillis(t *testing.T) {
	t.Parallel()
	checkFloats(t, "Millis", Millis, []floatCase{
		{2207.4, "2,207"},
		{999.4, "999"},
		{999.5, "1,000"},
		{999.96, "1,000"},
		{0, "0"},
		{-0.4, "0"},
		{-12.5, "-13"},
		{1_000_000, "1,000,000"},
		{math.Inf(-1), Missing},
	})
}

func TestPercent(t *testing.T) {
	t.Parallel()
	checkFloats(t, "Percent", Percent, []floatCase{
		{0, "0 %"},
		{0.004, "0 %"},
		{0.005, "1 %"},
		{0.75, "75 %"},
		{0.99, "99 %"},
		{0.994, "99 %"},
		{0.995, "100 %"},
		{1, "100 %"},
		{1.2, "120 %"},
		{-0.001, "0 %"},
		{-0.5, "-50 %"},
	})
}

func TestDuration(t *testing.T) {
	t.Parallel()
	for in, want := range map[time.Duration]string{
		0:                                 "0 ms",
		400 * time.Microsecond:            "0 ms",
		71 * time.Millisecond:             "71 ms",
		999_400 * time.Microsecond:        "999 ms",
		999_960 * time.Microsecond:        "1 s",
		time.Second:                       "1 s",
		2236 * time.Millisecond:           "2.24 s",
		3300 * time.Millisecond:           "3.3 s",
		9996 * time.Millisecond:           "10 s",
		30 * time.Second:                  "30 s",
		59400 * time.Millisecond:          "59.4 s",
		59500 * time.Millisecond:          "1 min",
		90 * time.Second:                  "1 min 30 s",
		10 * time.Minute:                  "10 min",
		time.Hour + 5*time.Second:         "1 h 5 s",
		2*time.Hour + 30*time.Minute:      "2 h 30 min",
		-71 * time.Millisecond:            "-71 ms",
		-2500 * time.Millisecond:          "-2.5 s",
		-time.Duration(math.MaxInt64) - 1: "-2,562,047 h 47 min 17 s",
	} {
		if got := Duration(in); got != want {
			t.Errorf("Duration(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestMedianAndSpread(t *testing.T) {
	t.Parallel()
	full := stats.Summary{N: 60, Median: 56.21, StdDev: 0.84}
	one := stats.Summary{N: 1, Median: 1941.2}
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"spread", Spread(full, Rate), "56.2 ± 0.8"},
		{"spread of one value", Spread(one, Millis), "1,941 ± 0"},
		{"spread of no values", Spread(stats.Summary{}, Rate), Missing},
		{"median", Median(one, Millis), "1,941"},
		{"median share", Median(stats.Summary{N: 3, Median: 0.995}, Percent), "100 %"},
		{"median of no values", Median(stats.Summary{}, Millis), Missing},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestBytes(t *testing.T) {
	t.Parallel()
	for in, want := range map[int64]string{
		0:                     "0 B",
		999:                   "999 B",
		-999:                  "-999 B",
		1000:                  "1.0 KB",
		999_949:               "999.9 KB",
		999_950:               "1.0 MB",
		5_600_000_000:         "5.6 GB",
		18_349_000_000:        "18.3 GB",
		-5_600_000_000:        "-5.6 GB",
		1_000_000_000_000_000: "1.0 PB",
		math.MaxInt64:         "9.2 EB",
		math.MinInt64:         "-9.2 EB",
	} {
		if got := Bytes(in); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestExcluded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   map[report.Flag]int
		want string
	}{
		{"nil", nil, ""},
		{"empty", map[report.Flag]int{}, ""},
		{"zero counts", map[report.Flag]int{report.FlagShort: 0}, ""},
		{"display order", map[report.Flag]int{report.FlagShort: 2, report.FlagFailed: 1}, "1 failed, 2 short"},
		{"every flag", map[report.Flag]int{
			report.FlagLoadInRun: 4, report.FlagUnexpectedHit: 3, report.FlagShort: 2, report.FlagFailed: 1,
		}, "1 failed, 2 short, 3 unexpected-hit, 4 load-in-run"},
		{"unknown flag", map[report.Flag]int{"other": 5}, ""},
		{"large count", map[report.Flag]int{report.FlagShort: 1200}, "1,200 short"},
	}
	for _, tt := range tests {
		if got := Excluded(tt.in); got != tt.want {
			t.Errorf("%s: Excluded() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestDates(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone("CEST", 2*60*60)
	first := time.Date(2026, 10, 5, 0, 30, 0, 0, zone)
	if got, want := Date(first), "2026-10-05"; got != want {
		t.Errorf("Date() = %q, want %q", got, want)
	}
	if got, want := Date(first.UTC()), "2026-10-04"; got != want {
		t.Errorf("Date(UTC) = %q, want %q", got, want)
	}
	if got, want := DateRange(first, first.Add(20*time.Hour)), "2026-10-05"; got != want {
		t.Errorf("DateRange(same day) = %q, want %q", got, want)
	}
	if got, want := DateRange(first, first.AddDate(0, 0, 28)), "2026-10-05 → 2026-11-02"; got != want {
		t.Errorf("DateRange() = %q, want %q", got, want)
	}
}

func TestTotals(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone("CEST", 2*60*60)
	first := time.Date(2026, 10, 5, 14, 2, 12, 0, zone)
	r := report.Report{Rows: 412, Valid: 400, Sessions: 23, First: first, Last: first.AddDate(0, 0, 28)}
	if got, want := Totals(r), "412 runs · 400 valid · 23 sessions · 2026-10-05 → 2026-11-02"; got != want {
		t.Errorf("Totals() = %q, want %q", got, want)
	}
	if got, want := Totals(report.Report{}), "0 runs · 0 valid · 0 sessions"; got != want {
		t.Errorf("Totals(empty) = %q, want %q", got, want)
	}
	if got, want := Totals(report.Report{Rows: 1, Valid: 1, Sessions: 1, First: first, Last: first}), "1 run · 1 valid · 1 session · 2026-10-05"; got != want {
		t.Errorf("Totals(one) = %q, want %q", got, want)
	}
}

func TestGroupColumns(t *testing.T) {
	t.Parallel()
	cols := GroupColumns([]report.Dimension{report.ByModel, report.ByMode, report.ByDay})
	g := report.Group{
		Key:         report.Key{Model: "qwen3.5:4b-mlx", Mode: "continue", Bucket: "2026-10-05"},
		Valid:       60,
		Decode:      stats.Summary{N: 60, Median: 56.04, StdDev: 0.91},
		Prefill:     stats.Summary{N: 60, Median: 56.2},
		TTFT:        stats.Summary{N: 60, Median: 71.4},
		CachedShare: stats.Summary{N: 60, Median: 0.99},
		Excluded:    map[report.Flag]int{report.FlagShort: 1},
	}
	var headers, cells []string
	for _, c := range cols {
		headers = append(headers, c.Header)
		cells = append(cells, c.Cell(g))
	}
	wantHeaders := []string{"MODEL", "MODE", "DAY", "N", "DECODE tok/s", "PREFILL ms", "TTFT ms", "LATENCY ms", "CLIENT tok/s", "CACHED", "EXCLUDED", "WARNINGS"}
	if !slices.Equal(headers, wantHeaders) {
		t.Errorf("headers = %q, want %q", headers, wantHeaders)
	}
	wantCells := []string{"qwen3.5:4b-mlx", "continue", "2026-10-05", "60", "56.0 ± 0.9", "56", "71", "-", "-", "99 %", "1 short", ""}
	if !slices.Equal(cells, wantCells) {
		t.Errorf("cells = %q, want %q", cells, wantCells)
	}
	var right []string
	for _, c := range cols {
		if c.Right {
			right = append(right, c.Header)
		}
	}
	if want := []string{"N", "DECODE tok/s", "PREFILL ms", "TTFT ms", "LATENCY ms", "CLIENT tok/s", "CACHED"}; !slices.Equal(right, want) {
		t.Errorf("right-aligned columns = %q, want %q", right, want)
	}
}

func TestOverview(t *testing.T) {
	t.Parallel()
	if got := Overview(report.Report{}); got != NoRuns {
		t.Errorf("Overview(empty) = %q, want %q", got, NoRuns)
	}
	r := report.Report{Groups: []report.Group{{}}, Rows: 2, Valid: 1, Sessions: 1}
	if got, want := Overview(r), Totals(r); got != want {
		t.Errorf("Overview() = %q, want %q", got, want)
	}
}

func TestMetricValue(t *testing.T) {
	t.Parallel()
	if got := MetricValue(report.MetricDecode)(55.44); got != "55.4" {
		t.Errorf("decode value = %q", got)
	}
	for _, m := range []report.Metric{report.MetricPrefill, report.MetricTTFT} {
		if got := MetricValue(m)(2207.4); got != "2,207" {
			t.Errorf("%s value = %q", m, got)
		}
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()
	stopped := fmt.Errorf("run 2: %w", context.Canceled)
	tests := []struct {
		err             error
		text, inSession string
	}{
		{nil, "", ""},
		{errors.New("a\nb"), "a; b", "a; b"},
		{stopped, "run 2: context canceled", ""},
		{context.DeadlineExceeded, "context deadline exceeded", "context deadline exceeded"},
	}
	for _, tt := range tests {
		if got := Error(tt.err); got != tt.text {
			t.Errorf("Error(%v) = %q, want %q", tt.err, got, tt.text)
		}
		if got := SessionError(tt.err); got != tt.inSession {
			t.Errorf("SessionError(%v) = %q, want %q", tt.err, got, tt.inSession)
		}
	}
}

func TestFailed(t *testing.T) {
	t.Parallel()
	if got := Failed(nil); got != "failed" {
		t.Errorf("Failed(nil) = %q", got)
	}
	if got := Failed(errors.New("chat: EOF\nretry")); got != "failed: chat: EOF; retry" {
		t.Errorf("Failed() = %q", got)
	}
}
