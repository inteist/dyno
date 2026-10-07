package format

import (
	"slices"
	"strings"
	"testing"

	"dyno/internal/domain"
	"dyno/internal/report"
)

func TestReportDimensionsUseFullIdentityAndPolicy(t *testing.T) {
	t.Parallel()
	r := report.Report{Dimensions: []report.Dimension{report.ByModel, report.ByModelID, report.ByMeasurement, report.ByMode}}
	for _, suffix := range []string{"1", "2"} {
		r.Groups = append(r.Groups, report.Group{Key: report.Key{Model: "same", Digest: "0123456789ab" + suffix, Measurement: domain.ProtocolOpenAIChat, Policy: strings.Repeat("a", 63) + suffix, Mode: "miss"}})
	}
	shared, varying := ReportDimensions(r)
	if !slices.Equal(shared, []report.Dimension{report.ByModel, report.ByMode}) || !slices.Equal(varying, []report.Dimension{report.ByModelID, report.ByMeasurement}) {
		t.Fatalf("shared %v / varying %v", shared, varying)
	}
	if got := DimensionFacts(r.Groups[0].Key, shared); got != "MODEL same · MODE miss" {
		t.Errorf("facts = %q", got)
	}
	if shared, varying := ReportDimensions(report.Report{Dimensions: r.Dimensions}); len(shared) != 0 || !slices.Equal(varying, r.Dimensions) {
		t.Errorf("empty report dimensions = %v / %v", shared, varying)
	}
}

func TestDimensionValueRetainsRevisionAndSessionFacts(t *testing.T) {
	t.Parallel()
	for _, d := range []report.Dimension{report.ByDigest, report.ByModelID} {
		revision := report.Key{Digest: "openai:revision:release:2026-10", IdentitySource: "operator-revision", Revision: "release:2026-10"}
		if got := DimensionValue(revision, d); !strings.Contains(got, revision.Revision) {
			t.Errorf("revision display %q loses %q", got, revision.Revision)
		}
		a := report.Key{Digest: "openai:session:20261005T120212Z-AAAAAA:served", IdentitySource: "session"}
		b := a
		b.Digest = "openai:session:20261005T120212Z-BBBBBB:served"
		if DimensionValue(a, d) == DimensionValue(b, d) {
			t.Errorf("session-scoped rows with the same alias look identical: %q", DimensionValue(a, d))
		}
		if got := DimensionValue(report.Key{}, d); got != Missing {
			t.Errorf("missing identity = %q", got)
		}
	}
}
