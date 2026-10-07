package runview

import (
	"dyno/internal/domain"
	"dyno/internal/report"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestProductionNativeSummaryPrioritizesPrefillAndExclusions(t *testing.T) {
	r := allTimeReport()
	for _, width := range []int{72, 80, 100, 120} {
		text := ansi.Strip(strings.Join(New(nil).reportLines("history", r, width), "\n"))
		if !strings.Contains(text, "PREFILL") || strings.Contains(text, "LATENCY") || strings.Contains(text, "CLIENT") {
			t.Errorf("native columns regressed at %d:\n%s", width, text)
		}
		if width >= 100 && (!strings.Contains(text, "EXCLUDED") || !strings.Contains(text, "failed")) {
			t.Errorf("native exclusions lost at %d:\n%s", width, text)
		}
		assertWidth(t, text, width)
	}
}

func TestExternalSummaryKeepsCaveatsWhenWarningsColumnCannotFit(t *testing.T) {
	r := report.Report{Dimensions: report.DefaultDimensions(), Groups: []report.Group{{Key: report.Key{Model: "served", Digest: "session-one", Backend: domain.BackendOpenAI, Engine: "vllm", Endpoint: "https://host/v1", Measurement: domain.ProtocolOpenAIChat, Mode: "miss"}, Warnings: map[report.Warning]int{report.WarningIdentityUnverified: 1, report.WarningCacheUnverified: 1}}}}
	for _, width := range []int{120, 160} {
		text := ansi.Strip(strings.Join(New(nil).reportLines("history", r, width), "\n"))
		for _, warning := range []string{"identity-unverified", "cache-unverified"} {
			if !strings.Contains(text, warning) {
				t.Fatalf("%s lost at %d:\n%s", warning, width, text)
			}
		}
	}
}
