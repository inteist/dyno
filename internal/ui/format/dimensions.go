package format

import (
	"slices"
	"strings"

	"dyno/internal/domain"

	"dyno/internal/report"
)

// ReportDimensions separates constant facts from the dimensions a table must
// display. Full values determine visibility; display abbreviations never do.
func ReportDimensions(r report.Report) (shared, varying []report.Dimension) {
	for _, d := range r.Dimensions {
		constant := len(r.Groups) > 0
		if constant {
			for _, g := range r.Groups[1:] {
				if g.Key.Value(d) != r.Groups[0].Key.Value(d) {
					constant = false
					break
				}
			}
		}
		if constant {
			if !RedundantNativeFact(r.Groups[0].Key, d) {
				shared = append(shared, d)
			}
		} else if !((d == report.ByModelID || d == report.ByDigest) && IdentityDeterminedByModel(r)) {
			varying = append(varying, d)
		}
	}
	return shared, varying
}

// IdentityDeterminedByModel uses full values, never display abbreviations.
func IdentityDeterminedByModel(r report.Report) bool {
	if !slices.Contains(r.Dimensions, report.ByModel) || len(r.Groups) == 0 || HasCompatibleGroups(r.Groups) {
		return false
	}
	identities := make(map[string]string)
	for _, g := range r.Groups {
		if previous, ok := identities[g.Key.Model]; ok && previous != g.Key.Digest {
			return false
		}
		identities[g.Key.Model] = g.Key.Digest
	}
	return true
}

// RedundantNativeFact omits only known legacy defaults, not distinct policies.
func RedundantNativeFact(k report.Key, d report.Dimension) bool {
	if k.Measurement != "" && k.Measurement != domain.ProtocolOllama || k.Backend != "" && k.Backend != domain.BackendOllama {
		return false
	}
	switch d {
	case report.ByBackend:
		return k.Backend == "" || k.Backend == domain.BackendOllama
	case report.ByEngine:
		return k.Engine == "" || k.Engine == domain.BackendOllama
	case report.ByEndpoint:
		return k.Endpoint == ""
	case report.ByMeasurement:
		return k.Policy == ""
	}
	return false
}

// EndpointFact retains display provenance even without endpoint grouping.
func EndpointFact(g report.Group) string {
	if g.Key.Endpoint != "" {
		return g.Key.Endpoint
	}
	return strings.Join(g.Endpoints, ", ")
}

// DimensionFact retains the full operator revision once, outside compact cells.
func DimensionFact(k report.Key, d report.Dimension) string {
	if (d == report.ByModelID || d == report.ByDigest) && k.Revision != "" {
		return Text(k.Revision)
	}
	return DimensionValue(k, d)
}

// DimensionFacts names constant dimensions once, outside the table.
func DimensionFacts(k report.Key, dims []report.Dimension) string {
	facts := make([]string, len(dims))
	for i, d := range dims {
		facts[i] = d.Header() + " " + DimensionFact(k, d)
	}
	return strings.Join(facts, Separator)
}
