package runview

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/report"
	"dyno/internal/ui/format"
)

func TestLiveProgressIsChunksNotTokens(t *testing.T) {
	t.Parallel()
	m := New(nil)
	m.state.Session.Workload.NumPredict = 512
	c := Current{Phase: PhaseRunning, Chunks: 50, Elapsed: time.Second}
	got := m.progressText(c)
	if !strings.Contains(got, "50 chunks") || strings.Contains(got, "tok/s") || strings.Contains(got, "/512") || strings.Contains(got, "%") {
		t.Errorf("misleading progress: %s", got)
	}
}

func TestTimingOnlyRunAndNarrowProvenance(t *testing.T) {
	t.Parallel()
	x := domain.Measurement{Server: domain.ServerMetrics{Available: &domain.MetricAvailability{}}, Client: domain.ClientMetrics{Wall: 2 * time.Second}, DoneReason: domain.DoneLength}
	for _, want := range []string{"client completion - tok/s", "ttft -", "latency 2 s", "cache-unverified"} {
		if text := runResult(x); !strings.Contains(text, want) {
			t.Errorf("result lacks %q: %s", want, text)
		}
	}
	r := report.Build([]report.Row{{Session: "s", StartedAt: start, Backend: domain.BackendOpenAI, ServerKind: "vllm", Endpoint: "https://example.test/v1", MeasurementProtocol: domain.ProtocolOpenAIChat, Model: domain.Model{Name: "served", Identity: "session:s:served", IdentitySource: "session"}, Mode: domain.CacheMiss, Measurement: x}}, report.DefaultDimensions(), zone)
	m := New(nil)
	lines := m.reportLines(format.SessionTitle, r, 40)
	if text := strings.Join(lines, "\n"); !strings.Contains(text, domain.ProtocolOpenAIChat) || !strings.Contains(text, "N 1") || !strings.Contains(text, "cache-unverified") {
		t.Errorf("narrow summary lost valid timings/provenance: %s", text)
	}
	s := sampleSession()
	s.Backend, s.MeasurementProtocol = domain.BackendOpenAI, domain.ProtocolOpenAIChat
	s.Endpoint = "https://long-proxy.example/very/long/deployment/api/v1"
	m.state = m.state.Apply(event.SessionStarted{Session: s})
	if title := ansi.Truncate(ansi.Strip(m.title()), 80, "…"); !strings.Contains(title, string(s.ID)) {
		t.Errorf("live session ID cut by provenance: %s", title)
	}
	if !strings.Contains(m.title(), domain.ProtocolOpenAIChat) {
		t.Errorf("live title lacks protocol: %s", m.title())
	}
}
