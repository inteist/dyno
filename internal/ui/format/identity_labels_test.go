package format

import (
	"dyno/internal/domain"
	"dyno/internal/report"
	"strings"
	"testing"
)

func TestRevisionCellsAreCompactAndFactsAreFull(t *testing.T) {
	revision := strings.Repeat("abcdef01", 5)
	key := report.Key{Digest: "openai:revision:aaa", IdentitySource: "operator-revision", Revision: revision}
	if got := DimensionValue(key, report.ByModelID); got != revision[:12]+"…" {
		t.Fatal(got)
	}
	if got := DimensionFact(key, report.ByModelID); got != revision {
		t.Fatal(got)
	}
}

func TestNumericRevisionIsNotShortenedAsHash(t *testing.T) {
	revision := strings.Repeat("1234567890", 4)
	key := report.Key{Digest: "identity", IdentitySource: "operator-revision", Revision: revision}
	if got := DimensionValue(key, report.ByModelID); got != revision {
		t.Fatalf("numeric label shortened: %s", got)
	}
}

func TestHistoricalOperatorLabelsCannotInjectTerminalControls(t *testing.T) {
	key := report.Key{Revision: "rev\x1b[31m\nnext", IdentitySource: "operator-revision", Digest: "identity", Server: "ver\ttext", Tag: "tag\rnext"}
	for _, text := range []string{DimensionFact(key, report.ByModelID), DimensionValue(key, report.ByModelID), DimensionValue(key, report.ByServer), DimensionValue(key, report.ByTag)} {
		if strings.ContainsAny(text, "\x1b\n\r\t") {
			t.Fatal("unsafe historical label", text)
		}
	}
}

func TestCrossEndpointRevisionLabelsRemainDistinguishable(t *testing.T) {
	groups := []report.Group{{Key: report.Key{Model: "alias", Digest: "openai:revision:" + strings.Repeat("a", 64), IdentitySource: "operator-revision", Revision: "shared-rev", Measurement: domain.ProtocolOpenAIChat}}, {Key: report.Key{Model: "alias", Digest: "openai:revision:" + strings.Repeat("b", 64), IdentitySource: "operator-revision", Revision: "shared-rev", Measurement: domain.ProtocolOpenAIChat}}}
	groups[0].Endpoints = []string{"https://first.example/v1"}
	groups[1].Endpoints = []string{"https://second.example/v1"}
	groups[0].Key.Endpoint = groups[0].Endpoints[0]
	groups[1].Key.Endpoint = groups[1].Endpoints[0]
	dims := []report.Dimension{report.ByModel, report.ByModelID, report.ByEndpoint}
	plainColumns := StableGroupColumns(dims, groups...)
	plainStatistics := StatsColumns(dims, report.MetricLatency, groups...)
	compactColumns := GroupColumns(dims, groups...)
	for _, g := range groups {
		for _, cols := range [][]Column[report.Group]{plainColumns, plainStatistics} {
			if got := cols[1].Cell(g); got != "shared-rev" {
				t.Fatal("plain identity repeats full endpoint column", got)
			}
		}
		if got := compactColumns[1].Cell(g); !strings.Contains(got, g.Key.Endpoint) {
			t.Fatal("compact identity relies on potentially clipped endpoint", got)
		}
	}
	cols := GroupColumns([]report.Dimension{report.ByModel, report.ByModelID}, groups...)
	for i, g := range groups {
		if !strings.Contains(cols[1].Cell(g), g.Endpoints[0]) {
			t.Fatal("collision label lost readable endpoint", i, cols[1].Cell(g))
		}
	}
	if cols[1].Cell(groups[0]) == cols[1].Cell(groups[1]) {
		t.Fatal("distinct full identities displayed identically")
	}
}
