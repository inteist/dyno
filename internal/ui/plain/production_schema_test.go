package plain

import (
	"bytes"
	"dyno/internal/report"
	"dyno/internal/ui/format"
	"strings"
	"testing"
)

func TestNativeProductionSummaryRetainsHistoricalColumns(t *testing.T) {
	r := allTimeReport()
	r.Dimensions = report.AllTimeDimensions()
	dims := plainDimensions(r)
	want := []report.Dimension{report.ByModel, report.ByDigest, report.ByServer, report.ByMode}
	if len(dims) != len(want) {
		t.Fatal(dims)
	}
	for i := range dims {
		if dims[i] != want[i] {
			t.Fatal(dims)
		}
	}
	r.Groups[0].Key.Server = ""
	var out bytes.Buffer
	if err := writeReport(&out, "Summary", r); err != nil {
		t.Fatal(err)
	}
	header := strings.Join(strings.Fields(strings.Split(out.String(), "\n")[1]), " ")
	if header != "MODEL DIGEST SERVER MODE N DECODE tok/s PREFILL ms TTFT ms CACHED EXCLUDED" {
		t.Fatal(header)
	}
	// Blank SERVER is deliberate legacy text, unlike unknown external metrics.
	for _, c := range format.StableGroupColumns(plainDimensions(r), r.Groups...) {
		if c.Header == "SERVER" && c.Cell(r.Groups[0]) != "" {
			t.Fatal("empty server changed", c.Cell(r.Groups[0]))
		}
	}
}

func TestNativeWarningsIdentifyModeAndRow(t *testing.T) {
	r := allTimeReport()
	r.Groups = r.Groups[:2]
	for i := range r.Groups {
		r.Groups[i].Warnings = map[report.Warning]int{report.WarningVersionUnknown: 3}
		r.Groups[i].Key.Server = ""
	}
	var out bytes.Buffer
	if err := writeReport(&out, "Summary", r); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "SERVER -") {
		t.Fatal("warning invents missing SERVER cell", text)
	}
	for _, want := range []string{"warnings [row 1]:", "warnings [row 2]:", "MODE miss", "MODE prefix"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
}

func TestRevisionFactsAreOneBlockWithRetainedEndpoints(t *testing.T) {
	r := allTimeReport()
	r.Groups = r.Groups[:2]
	for i := range r.Groups {
		r.Groups[i].Key.Revision = "release"
		r.Groups[i].Endpoints = []string{"https://host/v1"}
		r.Groups[i].Key.Model += string(rune('a' + i))
	}
	var out bytes.Buffer
	if err := writeRevisionFacts(&out, r); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Count(text, "\n\n") != 0 || !strings.HasPrefix(text, "\nmodel revision:") || strings.Count(text, "https://host/v1") != 2 {
		t.Fatal(text)
	}
	if !strings.Contains(text, "release\nmodel revision:") {
		t.Fatal("extra revision separators", text)
	}
}
