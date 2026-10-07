package format

import (
	"slices"
	"strings"

	"dyno/internal/domain"
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
func GroupColumns(dims []report.Dimension, groups ...report.Group) []Column[report.Group] {
	return append(dimensionColumns(dims, groups, false), visibleStatColumns(groups)...)
}

// visibleStatColumns keeps unavailable external usage visible, without adding
// optional empty columns to historical native reports.
func visibleStatColumns(groups []report.Group) []Column[report.Group] {
	cols := statColumns()
	if len(groups) > 0 {
		// Compatible timing-only groups still show unavailable usage as '-'.
		compatible := HasCompatibleGroups(groups)
		// Do not add empty optional columns to historical native reports.
		cols = slices.DeleteFunc(cols, func(c Column[report.Group]) bool {
			switch c.Header {
			case "LATENCY ms":
				return !compatible
			case "CLIENT tok/s":
				return !compatible
			case "WARNINGS":
				return !slices.ContainsFunc(groups, func(g report.Group) bool { return Warnings(g.Warnings) != "" })
			}
			return false
		})
	}
	return cols
}

// HasCompatibleGroups centralizes the shared presentation decision. Report Build
// retains protocol even when it is not an explicit grouping dimension.
func HasCompatibleGroups(groups []report.Group) bool {
	return slices.ContainsFunc(groups, func(g report.Group) bool {
		return g.Compatible || g.Key.Measurement != "" && g.Key.Measurement != domain.ProtocolOllama
	})
}

// StatsColumns returns the untruncated plain statistics columns of metric m:
// one for each dimension, then N, MEAN, MEDIAN, SD, MIN, and MAX.
func StatsColumns(dims []report.Dimension, m report.Metric, groups ...report.Group) []Column[report.Group] {
	value := MetricValue(m)
	stat := func(header string, pick func(stats.Summary) float64) Column[report.Group] {
		return Column[report.Group]{Header: header, Right: true, Cell: func(g report.Group) string {
			if s := g.Summary(m); s.N > 0 {
				return value(pick(s))
			}
			return Missing
		}}
	}
	return append(dimensionColumns(dims, groups, true),
		Column[report.Group]{Header: "N", Right: true, Cell: func(g report.Group) string { return Int(g.Summary(m).N) }},
		stat("MEAN", func(s stats.Summary) float64 { return s.Mean }),
		stat("MEDIAN", func(s stats.Summary) float64 { return s.Median }),
		stat("SD", func(s stats.Summary) float64 { return s.StdDev }),
		stat("MIN", func(s stats.Summary) float64 { return s.Min }),
		stat("MAX", func(s stats.Summary) float64 { return s.Max }),
	)
}

// dimensionColumns returns one column per dimension. Only untruncated plain
// cells can safely rely on the endpoint column to distinguish revision labels.
func dimensionColumns(dims []report.Dimension, groups []report.Group, untruncated bool) []Column[report.Group] {
	cols := make([]Column[report.Group], len(dims))
	for i, d := range dims {
		cols[i] = Column[report.Group]{
			Header: d.Header(),
			Cell: func(g report.Group) string {
				if d == report.ByServer && g.Key.Server == "" && !HasCompatibleGroups(groups) {
					return "" // historical native empty SERVER, not unavailable usage
				}
				return DimensionValue(g.Key, d)
			},
		}
	}
	// Distinct storage keys can share a revision label across endpoints. Keep
	// them distinguishable even when the user groups without endpoint.
	for i, d := range dims {
		if d != report.ByModelID && d != report.ByDigest {
			continue
		}
		labels := make(map[string]string)
		collisions := make(map[string]bool)
		for _, g := range groups {
			label := DimensionValue(g.Key, d)
			if key, ok := labels[label]; ok && key != g.Key.Digest {
				collisions[label] = true
			}
			labels[label] = g.Key.Digest
		}
		cols[i].Cell = func(g report.Group) string {
			label := DimensionValue(g.Key, d)
			if collisions[label] {
				if untruncated && slices.Contains(dims, report.ByEndpoint) && !slices.ContainsFunc(groups, func(other report.Group) bool {
					return other.Key.Digest != g.Key.Digest && DimensionValue(other.Key, d) == label && other.Key.Endpoint == g.Key.Endpoint
				}) {
					return Text(label) // the endpoint column already distinguishes it
				}
				endpoint := EndpointFact(g)
				if endpoint != "" && !slices.ContainsFunc(groups, func(other report.Group) bool {
					return other.Key.Digest != g.Key.Digest && DimensionValue(other.Key, d) == label && EndpointFact(other) == endpoint
				}) {
					return Text(label + " [" + endpoint + "]")
				}
				key := g.Key.Digest
				if at := strings.LastIndex(key, ":"); at >= 0 {
					key = key[at+1:]
				}
				// Extend a colliding prefix rather than inventing identical labels.
				n := min(12, len(key))
				for n < len(key) && slices.ContainsFunc(groups, func(other report.Group) bool {
					tail := other.Key.Digest[strings.LastIndex(other.Key.Digest, ":")+1:]
					return other.Key.Digest != g.Key.Digest && strings.HasPrefix(tail, key[:n])
				}) {
					n++
				}
				if slices.ContainsFunc(groups, func(other report.Group) bool {
					return other.Key.Digest != g.Key.Digest && other.Key.Digest[strings.LastIndex(other.Key.Digest, ":")+1:] == key
				}) {
					key = g.Key.Digest
					n = len(key)
				}
				label += " [" + key[:n] + "]"
			}
			return Text(label)
		}
	}
	return cols
}

// StableGroupColumns keeps requested dimensions and a fixed protocol-specific
// plain schema: historical native statistics, or compatible statistics/caveats.
// Metric availability never changes either schema.
func StableGroupColumns(dims []report.Dimension, groups ...report.Group) []Column[report.Group] {
	cols := statColumns()
	if !HasCompatibleGroups(groups) {
		cols = slices.DeleteFunc(cols, func(c Column[report.Group]) bool {
			return c.Header == "LATENCY ms" || c.Header == "CLIENT tok/s" || c.Header == "WARNINGS"
		})
	}
	return append(dimensionColumns(dims, groups, true), cols...)
}

func statColumns() []Column[report.Group] {
	return []Column[report.Group]{
		{Header: "N", Right: true, Cell: func(g report.Group) string { return Int(g.Valid) }},
		{Header: "DECODE tok/s", Right: true, Cell: func(g report.Group) string { return Spread(g.Decode, MetricValue(report.MetricDecode)) }},
		{Header: "PREFILL ms", Right: true, Cell: func(g report.Group) string { return Median(g.Prefill, MetricValue(report.MetricPrefill)) }},
		{Header: "TTFT ms", Right: true, Cell: func(g report.Group) string { return Median(g.TTFT, MetricValue(report.MetricTTFT)) }},
		{Header: "LATENCY ms", Right: true, Cell: func(g report.Group) string { return Median(g.Latency, MetricValue(report.MetricLatency)) }},
		{Header: "CLIENT tok/s", Right: true, Cell: func(g report.Group) string { return Median(g.ClientRate, MetricValue(report.MetricClientRate)) }},
		{Header: "CACHED", Right: true, Cell: func(g report.Group) string { return Median(g.CachedShare, Percent) }},
		{Header: "EXCLUDED", Cell: func(g report.Group) string { return Excluded(g.Excluded) }},
		{Header: "WARNINGS", Cell: func(g report.Group) string { return Warnings(g.Warnings) }},
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
	if s.N == 0 {
		return "metric unavailable"
	}
	return strings.Join([]string{
		"n " + Int(s.N),
		"mean " + value(s.Mean),
		"median " + value(s.Median),
		"sd " + value(s.StdDev),
		"min " + value(s.Min),
		"max " + value(s.Max),
	}, Separator)
}
