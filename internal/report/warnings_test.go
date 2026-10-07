package report

import (
	"encoding/json"
	"slices"
	"testing"

	"dyno/internal/domain"
)

func TestReasoningWarningUsesEffectivePolicyVocabulary(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{"disabled", "disabled-when-capable", "enabled", "unknown", ""} {
		t.Run(policy, func(t *testing.T) {
			t.Parallel()
			config, err := json.Marshal(map[string]string{domain.ReasoningPolicyKey: policy})
			if err != nil {
				t.Fatal(err)
			}
			r := Row{Backend: domain.BackendOpenAI, MeasurementProtocol: domain.ProtocolOpenAIChat, ConfigJSON: string(config)}
			want := policy == "unknown" || policy == ""
			if got := slices.Contains(MeasurementWarnings(r), WarningReasoningUnknown); got != want {
				t.Errorf("reasoning policy %q: unknown warning = %v, want %v", policy, got, want)
			}
		})
	}
	for _, config := range []string{"", "{}", "null", "{", `{"reasoning":"disabled"}`, `{"reasoning_policy":false}`} {
		if !slices.Contains(MeasurementWarnings(Row{Backend: domain.BackendOpenAI, ConfigJSON: config}), WarningReasoningUnknown) {
			t.Errorf("missing or malformed reasoning policy %q must warn", config)
		}
	}
}
