package report

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"dyno/internal/domain"
	"dyno/internal/stats"
)

// Build groups rows by dims and summarizes each group. Incompatible
// measurement protocols or policies always form separate groups, even with
// empty dims. Legacy-only empty dims give one group. loc sets the day and week
// boundaries; nil means UTC. The result does not depend on the order of rows.
func Build(rows []Row, dims []Dimension, loc *time.Location) Report {
	if loc == nil {
		loc = time.UTC
	}
	sorted := inTimeOrder(inLocation(rows, loc))
	policies := make(policyKeys)
	dims = effectiveDimensions(sorted, dims, policies)
	groups := buildGroups(sorted, dims, loc, policies)
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
func buildGroups(rows []Row, dims []Dimension, loc *time.Location, policies policyKeys) []Group {
	parts := groupRows(rows, dims, loc, policies)
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
	if k.Digest != "" {
		for _, r := range rows {
			if k.IdentitySource == "" {
				k.IdentitySource = r.Model.IdentitySource
			}
			if k.Revision == "" {
				k.Revision = r.Model.Revision
			}
		}
	}
	valid, excluded := partition(rows)
	first, last := span(rows)
	endpoints := make(map[string]bool)
	for _, r := range rows {
		if r.Endpoint != "" {
			endpoints[r.Endpoint] = true
		}
	}
	return Group{
		Compatible:  slices.ContainsFunc(rows, func(r Row) bool { return !r.Native() }),
		Key:         k,
		Endpoints:   slices.Sorted(maps.Keys(endpoints)),
		Decode:      metricSummary(valid, MetricDecode),
		Prefill:     metricSummary(valid, MetricPrefill),
		PrefillRate: metricSummary(valid, MetricPrefillRate),
		TTFT:        metricSummary(valid, MetricTTFT),
		Latency:     metricSummary(valid, MetricLatency),
		ClientRate:  metricSummary(valid, MetricClientRate),
		Valid:       len(valid),
		CachedShare: summarize(slices.DeleteFunc(slices.Clone(valid), func(r Row) bool { return !CachedShareAvailable(r.Measurement) }), cachedShare),
		Sessions:    countSessions(rows),
		First:       first,
		Last:        last,
		Excluded:    excluded,
		Warnings:    countWarnings(valid),
		Daily:       perMetric(func(m Metric) []Point { return dailyMedians(runsOf(valid, m), m, loc) }),
		Recent:      perMetric(func(m Metric) []Point { return recentValues(runsOf(valid, m), m) }),
	}
}

// effectiveDimensions preserves historical custom grouping for legacy-only
// reports, but never merges incompatible measurement protocols or policies.
func effectiveDimensions(rows []Row, dims []Dimension, policies policyKeys) []Dimension {
	if slices.Contains(dims, ByMeasurement) || len(rows) == 0 {
		return dims
	}
	policy := policies.key(rows[0])
	if slices.ContainsFunc(rows, func(r Row) bool { return r.Protocol() != domain.ProtocolOllama || policies.key(r) != policy }) {
		return append(slices.Clone(dims), ByMeasurement)
	}
	return dims
}

// validRuns returns the number of valid rows, independent of metric availability.
func validRuns(groups []Group) int {
	n := 0
	for _, g := range groups {
		n += g.Valid
	}
	return n
}

// metricSummary returns the summary of m over the runs that m covers.
func metricSummary(runs []Row, m Metric) stats.Summary {
	return summarize(runsOf(runs, m), m.Value)
}

// runsOf returns the runs that give a value of m, in their order.
func runsOf(runs []Row, m Metric) []Row {
	return slices.DeleteFunc(slices.Clone(runs), func(r Row) bool { return !m.Covers(r.Mode) || !m.Available(r.Measurement) })
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
