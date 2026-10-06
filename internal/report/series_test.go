package report

import (
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/stats"
)

// day is one expected point of a daily series, as a local date and the
// median decode rate.
type day struct {
	month  time.Month
	date   int
	decode float64
}

func TestBuildDaily(t *testing.T) {
	t.Parallel()
	rows := []Row{
		newRow(at(5, 6, 0), 10),
		newRow(at(5, 12, 0), 30),
		changed(newRow(at(5, 15, 0), 1000), short),
		newRow(at(5, 22, 30), 50), // 00:30 on October 6 in CEST
		newRow(at(7, 9, 0), 40),
		newRow(at(7, 10, 0), 60),
		newRow(at(7, 11, 0), 80),
		changed(newRow(at(7, 12, 0), 10), failed),
	}
	tests := []struct {
		name string
		loc  *time.Location
		want []day
	}{
		{name: "CEST", loc: cest(), want: []day{{time.October, 5, 20}, {time.October, 6, 50}, {time.October, 7, 60}}},
		{name: "UTC", loc: time.UTC, want: []day{{time.October, 5, 30}, {time.October, 7, 60}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := Build(shuffled(rows), nil, tt.loc).Groups[0]
			for _, m := range linearMetrics() {
				got := g.Daily[m]
				if len(got) != len(tt.want) {
					t.Fatalf("Daily[%s] = %v, want %d points", m, got, len(tt.want))
				}
				for i, w := range tt.want {
					want := Point{Time: time.Date(2026, w.month, w.date, 0, 0, 0, 0, tt.loc), Value: valueOf(m, w.decode)}
					if !got[i].Time.Equal(want.Time) || got[i].Value != want.Value {
						t.Errorf("Daily[%s][%d] = %v, want %v", m, i, got[i], want)
					}
				}
			}
		})
	}
}

func TestBuildRecent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		valid int
		first int // the decode rate of the oldest point
	}{
		{name: "fewer runs than RecentRuns", valid: 3, first: 1},
		{name: "RecentRuns runs", valid: RecentRuns, first: 1},
		{name: "more runs than RecentRuns", valid: RecentRuns + 10, first: 11},
	}
	start := at(5, 8, 0)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var rows []Row
			for i := 1; i <= tt.valid; i++ {
				rows = append(rows, newRow(start.Add(time.Duration(i)*time.Minute), i))
			}
			end := start.Add(time.Duration(tt.valid) * time.Minute)
			rows = append(rows,
				changed(newRow(end.Add(time.Minute), 999), short),
				changed(newRow(end.Add(2*time.Minute), 999), failed))

			g := Build(shuffled(rows), nil, nil).Groups[0]
			for _, m := range linearMetrics() {
				got := g.Recent[m]
				if want := min(tt.valid, RecentRuns); len(got) != want {
					t.Fatalf("len(Recent[%s]) = %d, want %d", m, len(got), want)
				}
				for i, p := range got {
					decode := tt.first + i
					want := Point{Time: start.Add(time.Duration(decode) * time.Minute), Value: valueOf(m, float64(decode))}
					if !p.Time.Equal(want.Time) || p.Value != want.Value {
						t.Errorf("Recent[%s][%d] = %v, want %v", m, i, p, want)
					}
				}
			}
		})
	}
}

func TestBuildPrefillRate(t *testing.T) {
	t.Parallel()
	// 1,000 uncached prompt tokens in 300 ms and in 900 ms. The prefix run
	// does not count for the prefill rate.
	first := newRow(at(5, 8, 0), 30)
	second := newRow(at(6, 8, 0), 90)
	prefix := changed(newRow(at(6, 9, 0), 60), inMode(domain.CachePrefix), cached(400))
	g := Build([]Row{second, prefix, first}, nil, nil).Groups[0]
	rate := func(prefill time.Duration) float64 { return 1000 / prefill.Seconds() }
	want := stats.Summarize([]float64{rate(300 * time.Millisecond), rate(900 * time.Millisecond)})
	if g.PrefillRate != want || g.Summary(MetricPrefillRate) != want {
		t.Errorf("PrefillRate = %+v, want %+v", g.PrefillRate, want)
	}
	if g.Decode.N != 3 {
		t.Errorf("Decode.N = %d, want all 3 runs", g.Decode.N)
	}
	for name, points := range map[string][]Point{"Daily": g.Daily[MetricPrefillRate], "Recent": g.Recent[MetricPrefillRate]} {
		if len(points) != 2 || points[0].Value != rate(300*time.Millisecond) || points[1].Value != rate(900*time.Millisecond) {
			t.Errorf("%s[prefill-rate] = %v, want the two miss runs", name, points)
		}
	}
	if only := Build([]Row{prefix}, nil, nil).Groups[0]; only.PrefillRate.N != 0 || len(only.Recent[MetricPrefillRate]) != 0 {
		t.Errorf("a group of prefix runs has a prefill rate: %+v", only.PrefillRate)
	}
}
