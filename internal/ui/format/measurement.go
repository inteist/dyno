package format

import (
	"strings"
	"time"

	"dyno/internal/domain"
	"dyno/internal/report"
)

// Metric returns a measured value, or Missing when its inputs are absent.
func Metric(x domain.Measurement, m report.Metric) string {
	if !m.Available(x) {
		return Missing
	}
	return MetricValue(m)(m.Value(x))
}

// Timing returns a client interval, or Missing for an unavailable interval.
func Timing(d time.Duration) string {
	if d <= 0 {
		return Missing
	}
	return Duration(d)
}

// Cached returns the measured cache share, preserving unknown cache details.
func Cached(x domain.Measurement) string {
	if !report.CachedShareAvailable(x) {
		return Missing
	}
	return Percent(x.Server.CachedShare())
}

// DimensionValue abbreviates identity and policy only for display, never grouping.
func DimensionValue(k report.Key, d report.Dimension) string {
	value := k.Value(d)
	if d == report.ByMeasurement && k.Policy != "" {
		value = k.Measurement + " policy-" + k.Policy[:min(12, len(k.Policy))]
	}
	if value == "" {
		return Missing
	}
	if d == report.ByDigest || d == report.ByModelID {
		return Text((domain.Model{Identity: value, IdentitySource: k.IdentitySource, Revision: k.Revision}).ShortDigest())
	}
	return Text(value)
}

// Warnings formats non-excluding caveats in the report's display order.
func Warnings(counts map[report.Warning]int) string {
	var parts []string
	for _, w := range report.Warnings() {
		if n := counts[w]; n > 0 {
			parts = append(parts, Int(n)+" "+string(w))
		}
	}
	return strings.Join(parts, ", ")
}

// Provenance keeps the backend and measurement definition visible in compact views.
// Native session headers retain their existing layout.
func Provenance(s domain.Session) string {
	if s.Native() {
		return ""
	}
	parts := []string{s.BackendName(), s.Protocol()}
	if s.ServerKind != "" {
		parts = append(parts, s.ServerKind)
	}
	if s.Endpoint != "" {
		parts = append(parts, s.Endpoint)
	}
	return strings.Join(parts, Separator)
}
