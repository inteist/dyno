package report_test

import (
	"slices"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/ollama"
	"dyno/internal/report"
)

// Info is pure: this deliberately unreachable endpoint must not be contacted.
func TestMigratedLegacyAndRealOllamaInfoMerge(t *testing.T) {
	t.Parallel()
	client, err := ollama.New("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Exact defaults from 0003_backend_metadata.sql, including NULL config_json.
	legacy := report.Row{
		Session: "old", Backend: "ollama", ServerKind: "ollama", MeasurementProtocol: "ollama-server-v1", ContextPolicy: "request",
		StartedAt: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC), ServerVersion: "0.34.0",
		Model: domain.Model{Name: "native:latest", Digest: "sha256:legacy-a", Identity: "sha256:legacy-a", IdentitySource: "digest"}, Mode: domain.CacheMiss,
		Measurement: domain.Measurement{DoneReason: domain.DoneLength, Server: domain.ServerMetrics{EvalTokens: 64, EvalDuration: time.Second}},
	}
	current := legacy
	current.Session = "new"
	current.Backend, current.ServerKind, current.MeasurementProtocol = info.Backend, info.ServerKind, info.MeasurementProtocol
	current.ContextPolicy, current.ConfigJSON = info.ContextPolicy, info.ConfigJSON
	equivalent := current
	equivalent.Session = "equivalent"
	equivalent.ConfigJSON = ` { "cache_policy": "ollama-calibrated", "reasoning_policy": "disabled-when-capable" } `
	for _, row := range []report.Row{legacy, current, equivalent} {
		if key := row.PolicyKey(); key != "" {
			t.Fatalf("native policy split from migrated legacy: %q (%+v)", key, row)
		}
	}
	for _, dims := range [][]report.Dimension{report.DefaultDimensions(), {report.ByModel, report.ByMode}, nil} {
		got := report.Build([]report.Row{legacy, current, equivalent}, dims, nil)
		if len(got.Groups) != 1 || got.Groups[0].Valid != 3 || got.Groups[0].Sessions != 3 || !slices.Equal(got.Dimensions, dims) {
			t.Errorf("equivalent native policies split for %v: %+v", dims, got)
		}
		changed := current
		changed.ConfigJSON = `{"reasoning_policy":"enabled","cache_policy":"ollama-calibrated"}`
		if split := report.Build([]report.Row{legacy, changed}, dims, nil); len(split.Groups) != 2 {
			t.Errorf("genuinely different native policy merged for %v: %+v", dims, split)
		}
	}
}
