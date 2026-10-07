package reportview

import (
	"testing"

	"dyno/internal/domain"
	"dyno/internal/report"
	"dyno/internal/ui/format"
)

func TestIdentityDependencyKeysStayFullButDisplayedCellsAreAbbreviated(t *testing.T) {
	t.Parallel()
	r := report.Report{Dimensions: []report.Dimension{report.ByModel, report.ByModelID, report.ByMeasurement}}
	for _, identity := range []string{"openai:revision:aaaaaaaaaaaa12345678", "openai:revision:bbbbbbbbbbbb12345678"} {
		r.Groups = append(r.Groups, report.Group{Key: report.Key{Model: "same-alias", Digest: identity, Measurement: domain.ProtocolOpenAIChat}})
	}
	columns := splitColumns(r)
	found := false
	for _, column := range columns.table {
		if column.Header != report.ByModelID.Header() {
			continue
		}
		found = true
		for _, group := range r.Groups {
			got := column.Cell(group)
			if want := format.DimensionValue(group.Key, report.ByModelID); got != want || got == group.Key.Digest {
				t.Fatalf("full storage namespace reached display cell: %q want %q", got, want)
			}
		}
	}
	if !found {
		t.Fatal("identity distinction dropped from table")
	}
}

func TestIdentityAndPolicyVariationCannotBeHiddenByModelDependency(t *testing.T) {
	t.Parallel()
	r := report.Report{Dimensions: []report.Dimension{report.ByModel, report.ByModelID, report.ByMeasurement}}
	for _, name := range []string{"a", "b"} {
		r.Groups = append(r.Groups, report.Group{Key: report.Key{Model: name, Digest: "openai:revision:" + name, IdentitySource: "operator-revision", Revision: "release-" + name, Measurement: domain.ProtocolOpenAIChat, Policy: name}})
	}
	set := splitColumns(r)
	for _, d := range r.Dimensions {
		found := false
		for _, c := range set.table {
			found = found || c.Header == d.Header()
		}
		if !found {
			t.Errorf("varying %s hidden because it follows the model name", d)
		}
	}
}
