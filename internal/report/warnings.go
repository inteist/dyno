package report

import (
	"encoding/json"

	"dyno/internal/domain"
)

// Warning describes a measurement caveat, never a reason to exclude client timings.
type Warning string

const (
	WarningCacheUnverified    Warning = "cache-unverified"
	WarningVersionUnknown     Warning = "version-unknown"
	WarningIdentityUnverified Warning = "identity-unverified"
	WarningPolicyUnknown      Warning = "policy-unknown"
	WarningReasoningUnknown   Warning = "reasoning-unknown"
)

// Warnings returns caveats in display order.
func Warnings() []Warning {
	return []Warning{WarningCacheUnverified, WarningVersionUnknown, WarningIdentityUnverified, WarningPolicyUnknown, WarningReasoningUnknown}
}

// MeasurementWarnings returns non-excluding caveats for a row.
func MeasurementWarnings(r Row) []Warning {
	var warnings []Warning
	if !CachedShareAvailable(r.Measurement) {
		warnings = append(warnings, WarningCacheUnverified)
	}
	if r.ServerVersion == "" {
		warnings = append(warnings, WarningVersionUnknown)
	}
	if r.Model.Digest == "" && (r.Model.Identity == "" || r.Model.Revision == "") {
		warnings = append(warnings, WarningIdentityUnverified)
	}
	if r.BackendName() != domain.BackendOllama {
		if r.ConfigJSON == "" {
			warnings = append(warnings, WarningPolicyUnknown)
		}
		var policy map[string]json.RawMessage
		var reasoning string
		if json.Unmarshal([]byte(r.ConfigJSON), &policy) != nil || json.Unmarshal(policy[domain.ReasoningPolicyKey], &reasoning) != nil || reasoning == "" || reasoning == "unknown" {
			warnings = append(warnings, WarningReasoningUnknown)
		}
	}
	return warnings
}

func countWarnings(rows []Row) map[Warning]int {
	counts := make(map[Warning]int)
	for _, r := range rows {
		for _, w := range MeasurementWarnings(r) {
			counts[w]++
		}
	}
	return counts
}
