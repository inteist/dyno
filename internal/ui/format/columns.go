package format

import (
	"strings"

	"dyno/internal/report"
	"dyno/internal/stats"
)

// Column describes one column of a table of T values.
type Column[T any] struct {
	Header string
	Right  bool // the cells align to the right, as numbers do
	Cell   func(T) string
}

// GroupColumns returns the columns of a report table: one for each
// dimension, then the number of valid runs, the medians, and the excluded
// runs.
func GroupColumns(dims []report.Dimension) []Column[report.Group] {
	return append(dimensionColumns(dims), statColumns()...)
}

// StatsColumns returns the columns of a statistics table of the metric m:
// one for each dimension, then N, MEAN, MEDIAN, SD, MIN, and MAX.
func StatsColumns(dims []report.Dimension, m report.Metric) []Column[report.Group] {
	value := MetricValue(m)
	stat := func(header string, pick func(stats.Summary) float64) Column[report.Group] {
		return Column[report.Group]{Header: header, Right: true, Cell: func(g report.Group) string {
			if s := g.Summary(m); s.N > 0 {
				return value(pick(s))
			}
			return Missing
		}}
	}
	return append(dimensionColumns(dims),
		Column[report.Group]{Header: "N", Right: true, Cell: func(g report.Group) string { return Int(g.Summary(m).N) }},
		stat("MEAN", func(s stats.Summary) float64 { return s.Mean }),
		stat("MEDIAN", func(s stats.Summary) float64 { return s.Median }),
		stat("SD", func(s stats.Summary) float64 { return s.StdDev }),
		stat("MIN", func(s stats.Summary) float64 { return s.Min }),
		stat("MAX", func(s stats.Summary) float64 { return s.Max }),
	)
}

// dimensionColumns returns one column for each dimension of dims.
func dimensionColumns(dims []report.Dimension) []Column[report.Group] {
	cols := make([]Column[report.Group], len(dims))
	for i, d := range dims {
		cols[i] = Column[report.Group]{
			Header: d.Header(),
			Cell:   func(g report.Group) string { return g.Key.Value(d) },
		}
	}
	return cols
}

func statColumns() []Column[report.Group] {
	return []Column[report.Group]{
		{Header: "N", Right: true, Cell: func(g report.Group) string { return Int(g.Decode.N) }},
		{Header: "DECODE tok/s", Right: true, Cell: func(g report.Group) string { return Spread(g.Decode, MetricValue(report.MetricDecode)) }},
		{Header: "PREFILL ms", Right: true, Cell: func(g report.Group) string { return Median(g.Prefill, MetricValue(report.MetricPrefill)) }},
		{Header: "TTFT ms", Right: true, Cell: func(g report.Group) string { return Median(g.TTFT, MetricValue(report.MetricTTFT)) }},
		{Header: "CACHED", Right: true, Cell: func(g report.Group) string { return Median(g.CachedShare, Percent) }},
		{Header: "EXCLUDED", Cell: func(g report.Group) string { return Excluded(g.Excluded) }},
	}
}

// MetricValue returns the formatter of the values of m: a rate for the
// rates, milliseconds for the others.
func MetricValue(m report.Metric) func(float64) string {
	if m.IsRate() {
		return Rate
	}
	return Millis
}

// Stats returns every statistic of s on one line, for example
// "n 60 · mean 56.1 · median 56.2 · sd 0.8 · min 54.9 · max 57.3".
func Stats(s stats.Summary, value func(float64) string) string {
	return strings.Join([]string{
		"n " + Int(s.N),
		"mean " + value(s.Mean),
		"median " + value(s.Median),
		"sd " + value(s.StdDev),
		"min " + value(s.Min),
		"max " + value(s.Max),
	}, Separator)
}
