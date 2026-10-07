package runview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"dyno/internal/domain"
	"dyno/internal/report"
	"dyno/internal/stats"
)

func TestNarrowSummaryRetainsIdentityVersionAndEndpoint(t *testing.T) {
	t.Parallel()
	r := report.Report{Dimensions: []report.Dimension{report.ByMeasurement, report.ByModel, report.ByModelID, report.ByServer, report.ByEndpoint, report.ByMode}, Rows: 2, Valid: 2}
	for _, suffix := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"} {
		r.Groups = append(r.Groups, report.Group{Key: report.Key{Measurement: domain.ProtocolOpenAIChat, Model: "same-served-id", Digest: "openai:revision:" + suffix, IdentitySource: "operator-revision", Revision: suffix, Server: "vllm-" + suffix, Endpoint: "http://gpu/v1", Mode: "miss"}, Valid: 1})
	}
	for _, width := range []int{60, 80} {
		m := sized(t, width, 24)
		text := ansi.Strip(strings.Join(m.reportLines("history", r, width), "\n"))
		assertWidth(t, text, width)
		unwrapped := strings.Join(strings.Fields(text), "")
		for _, want := range []string{"MODEL-ID", "aaaaaaaaaaaa", "bbbbbbbbbbbb", "vllm-aaaaaaaaaaaa", "vllm-bbbbbbbbbbbb", "http://gpu/v1"} {
			if !strings.Contains(unwrapped, want) {
				t.Errorf("width %d hides %q:\n%s", width, want, text)
			}
		}
	}
}

func TestNativeDefaultSummaryStaysAFittedTable(t *testing.T) {
	t.Parallel()
	r := report.Report{Dimensions: report.DefaultDimensions(), Rows: 2, Valid: 2}
	for _, digest := range []string{"aaaaaaaaaaaa1111", "bbbbbbbbbbbb2222"} {
		r.Groups = append(r.Groups, report.Group{Key: report.Key{Model: "native:latest", Digest: digest, Backend: domain.BackendOllama, Engine: domain.BackendOllama, Measurement: domain.ProtocolOllama, Server: "0.34.0", Mode: "miss"}, Valid: 1, Decode: stats.Summary{N: 1, Median: 10}})
	}
	m := sized(t, 80, 24)
	text := ansi.Strip(strings.Join(m.reportLines("history", r, 80), "\n"))
	assertWidth(t, text, 80)
	for _, want := range []string{"MODEL-ID", "aaaaaaaaaaaa", "bbbbbbbbbbbb", "DECODE tok/s"} {
		if !strings.Contains(text, want) {
			t.Errorf("native table hides %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "DECODE") != 1 || strings.Contains(text, "LATENCY ") {
		t.Errorf("native-only summary unexpectedly stacked:\n%s", text)
	}
}

func TestExternalSummaryStacksOnlyWhenFitLosesProvenance(t *testing.T) {
	t.Parallel()
	r := report.Report{Dimensions: []report.Dimension{report.ByModelID, report.ByEndpoint, report.ByMeasurement}, Rows: 2, Valid: 2}
	for _, suffix := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"} {
		r.Groups = append(r.Groups, report.Group{Key: report.Key{Digest: "openai:revision:" + suffix, IdentitySource: "operator-revision", Revision: suffix, Endpoint: "http://gpu/" + suffix + "/v1", Measurement: domain.ProtocolOpenAIChat}, Valid: 1,
			Excluded: map[report.Flag]int{report.FlagFailed: 1}, Warnings: map[report.Warning]int{report.WarningCacheUnverified: 1}})
	}
	m := New(nil)
	wide := ansi.Strip(strings.Join(m.reportLines("history", r, 500), "\n"))
	if strings.Count(wide, "PREFILL") != 1 || !strings.Contains(wide, "DECODE tok/s") {
		t.Errorf("external report stacked despite a provenance-preserving fit:\n%s", wide)
	}
	// Exercise the final inline renderer too: wrapped continuations must not
	// disappear when indent clips individual frame lines.
	narrow := ansi.Strip(indent(m.reportLines("history", r, 59), 60))
	assertWidth(t, narrow, 60)
	for _, want := range []string{"PREFILL", "CACHED", "EXCLUDED", "1 failed", "WARNINGS", "cache-unverified", "aaaaaaaaaaaa", "bbbbbbbbbbbb", "/v1"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("stacked summary hides %q:\n%s", want, narrow)
		}
	}
}
