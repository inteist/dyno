package plain

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/golden"
	"dyno/internal/report"
)

func optionalRow(usage bool) report.Row {
	return report.Row{
		Session: "20261005T120212Z-A1B2C3", StartedAt: start, Model: domain.Model{Name: "served", Identity: "session:20261005T120212Z-A1B2C3:served", IdentitySource: "session"},
		Backend: domain.BackendOpenAI, ServerKind: "vllm", Endpoint: "https://example.test/v1", MeasurementProtocol: domain.ProtocolOpenAIChat, ContextPolicy: "server-managed", VersionSource: "unknown",
		Workload: domain.Workload{PromptName: "short", PromptHash: "h", NumPredict: 512}, Mode: domain.CacheMiss,
		Measurement: domain.Measurement{Server: domain.ServerMetrics{Available: &domain.MetricAvailability{EvalTokens: usage}, EvalTokens: 100}, Client: domain.ClientMetrics{TimeToFirstToken: time.Second, Wall: 2 * time.Second}, DoneReason: domain.DoneLength},
	}
}

func TestOptionalReportGolden(t *testing.T) {
	t.Parallel()
	a, b := optionalRow(true), optionalRow(false)
	b.StartedAt = b.StartedAt.Add(24 * time.Hour)
	b.Measurement.Client.TimeToFirstToken = 0
	r := report.Build([]report.Row{a, b}, report.DefaultDimensions(), zone)
	var buf bytes.Buffer
	if err := NewReportUI(&buf).Show(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	golden.Assert(t, "report-compatible", buf.String())
}

func TestOptionalRawResult(t *testing.T) {
	t.Parallel()
	for _, usage := range []bool{true, false} {
		x := optionalRow(usage).Measurement
		x.Client.TimeToFirstToken = 0
		text := result(x)
		for _, want := range []string{"decode - tok/s", "prefill - ms", "ttft -", "cached -", "latency 2 s", "cache-unverified"} {
			if !strings.Contains(text, want) {
				t.Errorf("raw result lacks %q: %s", want, text)
			}
		}
		want := "client completion - tok/s"
		if usage {
			want = "client completion 50.0 tok/s"
		}
		if !strings.Contains(text, want) {
			t.Errorf("raw result lacks %q: %s", want, text)
		}
	}
}
