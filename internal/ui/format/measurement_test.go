package format

import (
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/report"
)

func TestUnavailableMeasurements(t *testing.T) {
	t.Parallel()
	x := domain.Measurement{Server: domain.ServerMetrics{Available: &domain.MetricAvailability{}}}
	for _, m := range report.Metrics() {
		if got := Metric(x, m); got != Missing {
			t.Errorf("%s = %q, want -", m, got)
		}
	}
	if Cached(x) != Missing || Timing(0) != Missing {
		t.Error("absent cache and client TTFT must not display zero")
	}
	x.Server.Available.EvalTokens = true
	x.Client.Wall = time.Second
	if got := Metric(x, report.MetricClientRate); got != "0.0" {
		t.Errorf("known zero completion usage = %q", got)
	}
	x.Server.Available.CachedTokens, x.Server.Available.PromptTokens = true, true
	if got := Cached(x); got != "0 %" {
		t.Errorf("known zero cache share = %q", got)
	}
}

func TestValidRowsIndependentOfDecode(t *testing.T) {
	t.Parallel()
	g := report.Group{Valid: 3}
	for _, c := range GroupColumns(nil) {
		switch c.Header {
		case "N":
			if c.Cell(g) != "3" {
				t.Error("N must count valid rows, not decode samples")
			}
		case "DECODE tok/s", "PREFILL ms", "TTFT ms", "LATENCY ms", "CLIENT tok/s", "CACHED":
			if c.Cell(g) != Missing {
				t.Errorf("%s = %q, want -", c.Header, c.Cell(g))
			}
		}
	}
}
