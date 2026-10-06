package report

import (
	"slices"
	"time"

	"dyno/internal/stats"
)

// perMetric returns the series that f makes for each metric.
func perMetric(f func(Metric) []Point) map[Metric][]Point {
	series := make(map[Metric][]Point, len(Metrics()))
	for _, m := range Metrics() {
		series[m] = f(m)
	}
	return series
}

// dailyMedians returns the median of m for each local day of runs, oldest
// first. Each point is at local midnight.
func dailyMedians(runs []Row, m Metric, loc *time.Location) []Point {
	// The days are safe map keys: they share loc and have no monotonic
	// clock reading.
	days := make(map[time.Time][]float64)
	for _, r := range runs {
		day := midnight(r.StartedAt, loc)
		days[day] = append(days[day], m.Value(r.Measurement))
	}
	points := make([]Point, 0, len(days))
	for day, values := range days {
		points = append(points, Point{Time: day, Value: stats.Median(values)})
	}
	slices.SortFunc(points, func(a, b Point) int { return a.Time.Compare(b.Time) })
	return points
}

// recentValues returns m of the last RecentRuns runs, oldest first. runs
// must be in time order.
func recentValues(runs []Row, m Metric) []Point {
	last := runs[max(0, len(runs)-RecentRuns):]
	points := make([]Point, len(last))
	for i, r := range last {
		points[i] = Point{Time: r.StartedAt, Value: m.Value(r.Measurement)}
	}
	return points
}
