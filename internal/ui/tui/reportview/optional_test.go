package reportview

import (
	"strings"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/golden"
	"dyno/internal/report"
)

func timingOnlyReport() report.Report {
	return report.Build([]report.Row{{Session: "s", StartedAt: firstDay, Backend: domain.BackendOpenAI, ServerKind: "vllm", Endpoint: "https://example.test/v1", MeasurementProtocol: domain.ProtocolOpenAIChat, Model: domain.Model{Name: "served", Identity: "session:s:served", IdentitySource: "session"}, Mode: domain.CacheMiss,
		Measurement: domain.Measurement{Server: domain.ServerMetrics{Available: &domain.MetricAvailability{}}, Client: domain.ClientMetrics{TimeToFirstToken: time.Second, Wall: 2 * time.Second}, DoneReason: domain.DoneLength}}}, report.DefaultDimensions(), zone)
}

func TestTimingOnlyReportView(t *testing.T) {
	t.Parallel()
	r := timingOnlyReport()
	m := resize(New(r), size{80, 24})
	got := screen(m)
	for _, want := range []string{"1 valid", domain.ProtocolOpenAIChat, metricUnavailable, "-"} {
		if !strings.Contains(got, want) {
			t.Errorf("view lacks %q: %s", want, got)
		}
	}
	golden.Assert(t, "timing-only-80x24", got)
	for range 4 {
		m = press(t, m, "tab")
	}
	if got := screen(m); !strings.Contains(got, report.MetricLatency.Label()) || strings.Contains(got, metricUnavailable) {
		t.Errorf("latency was lost with missing decode: %s", got)
	}
}

func TestFullIdentityDependencyUsesFullKeys(t *testing.T) {
	t.Parallel()
	a, b := sampleRows(1)[0], sampleRows(1)[0]
	a.Model.Digest, b.Model.Digest = "0123456789abaaaa", "0123456789abbbbb"
	r := report.Build([]report.Row{a, b}, []report.Dimension{report.ByModel, report.ByDigest}, zone)
	set := splitColumns(r)
	for _, c := range set.table {
		if c.Header == report.ByDigest.Header() {
			return
		}
	}
	t.Error("two full identities sharing a display prefix must not be treated as one shared identity")
}
