package report

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/stats"
)

// Build groups rows by dims and summarizes each group. Empty dims give one
// group for all rows. loc sets the day and week boundaries, and nil means
// UTC. The result does not depend on the order of rows.
func Build(rows []Row, dims []Dimension, loc *time.Location) Report {
	if loc == nil {
		loc = time.UTC
	}
	sorted := inTimeOrder(inLocation(rows, loc))
	groups := buildGroups(sorted, dims, loc)
	first, last := span(sorted)
	return Report{
		Dimensions: slices.Clone(dims),
		Groups:     groups,
		Rows:       len(sorted),
		Valid:      validRuns(groups),
		Sessions:   countSessions(sorted),
		First:      first,
		Last:       last,
	}
}

// buildGroups returns one group for each key of rows, in the order of
// compareKeys. rows must be in time order.
func buildGroups(rows []Row, dims []Dimension, loc *time.Location) []Group {
	parts := groupRows(rows, dims, loc)
	keys := slices.SortedFunc(maps.Keys(parts), compareKeys(dims))
	groups := make([]Group, len(keys))
	for i, k := range keys {
		groups[i] = buildGroup(k, parts[k], loc)
	}
	return groups
}

// buildGroup summarizes the rows of one key. rows must be in time order.
// The statistics and the series use only the valid rows, the counts and the
// times use all rows.
func buildGroup(k Key, rows []Row, loc *time.Location) Group {
	valid, excluded := partition(rows)
	first, last := span(rows)
	return Group{
		Key:         k,
		Decode:      metricSummary(valid, MetricDecode),
		Prefill:     metricSummary(valid, MetricPrefill),
		PrefillRate: metricSummary(valid, MetricPrefillRate),
		TTFT:        metricSummary(valid, MetricTTFT),
		CachedShare: summarize(valid, cachedShare),
		Sessions:    countSessions(rows),
		First:       first,
		Last:        last,
		Excluded:    excluded,
		Daily:       perMetric(func(m Metric) []Point { return dailyMedians(runsOf(valid, m), m, loc) }),
		Recent:      perMetric(func(m Metric) []Point { return recentValues(runsOf(valid, m), m) }),
	}
}

// validRuns returns the number of valid runs of groups. Each summary of a
// group counts the valid runs of the group.
func validRuns(groups []Group) int {
	n := 0
	for _, g := range groups {
		n += g.Decode.N
	}
	return n
}

// metricSummary returns the summary of m over the runs that m covers.
func metricSummary(runs []Row, m Metric) stats.Summary {
	return summarize(runsOf(runs, m), m.Value)
}

// runsOf returns the runs that give a value of m, in their order.
func runsOf(runs []Row, m Metric) []Row {
	return slices.DeleteFunc(slices.Clone(runs), func(r Row) bool { return !m.Covers(r.Mode) })
}

// summarize returns the summary of value over the measurements of runs.
func summarize(runs []Row, value func(domain.Measurement) float64) stats.Summary {
	values := make([]float64, len(runs))
	for i, r := range runs {
		values[i] = value(r.Measurement)
	}
	return stats.Summarize(values)
}

// cachedShare returns the part of the prompt of x that came from the cache.
func cachedShare(x domain.Measurement) float64 { return x.Server.CachedShare() }

// inLocation returns a copy of rows with StartedAt in loc, so that every
// time of the report is local.
func inLocation(rows []Row, loc *time.Location) []Row {
	local := slices.Clone(rows)
	for i := range local {
		local[i].StartedAt = local[i].StartedAt.In(loc)
	}
	return local
}

// inTimeOrder returns a copy of rows, sorted by StartedAt. The session breaks
// ties, and rows with the same time and session keep the order of the
// store.
func inTimeOrder(rows []Row) []Row {
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b Row) int {
		return cmp.Or(a.StartedAt.Compare(b.StartedAt), cmp.Compare(a.Session, b.Session))
	})
	return sorted
}

// span returns the first and the last StartedAt of rows in time order, or
// zero times for no rows.
func span(rows []Row) (first, last time.Time) {
	if len(rows) == 0 {
		return time.Time{}, time.Time{}
	}
	return rows[0].StartedAt, rows[len(rows)-1].StartedAt
}

// countSessions returns the number of distinct sessions of rows.
func countSessions(rows []Row) int {
	sessions := make(map[domain.SessionID]struct{}, len(rows))
	for _, r := range rows {
		sessions[r.Session] = struct{}{}
	}
	return len(sessions)
}
