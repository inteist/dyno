package reportview

import (
	"dyno/internal/domain"
	"dyno/internal/report"
	"strings"
	"testing"
)

func TestHistoricalRevisionFactSanitizedAtProductionPath(t *testing.T) {
	r := sampleReport(3)
	r.Groups = r.Groups[:1]
	r.Groups[0].Key.Revision = "rev\x1b[31m\nnext\u202e"
	r.Groups[0].Key.IdentitySource = "operator-revision"
	m := New(r)
	text := m.factsView()
	if strings.Contains(text, "\x1b[31m") || strings.Contains(text, "\u202e") || strings.Contains(text, "[31m\nnext") {
		t.Fatal("unsafe revision fact", text)
	}
}

func TestVaryingEndpointIsNotHiddenByIdentityDependency(t *testing.T) {
	r := report.Report{Dimensions: report.DefaultDimensions(), Groups: []report.Group{
		{Key: report.Key{Model: "alias", Digest: "id1", Revision: "r1", IdentitySource: "operator-revision", Backend: domain.BackendOpenAI, Measurement: domain.ProtocolOpenAIChat, Endpoint: "https://a/v1"}},
		{Key: report.Key{Model: "alias", Digest: "id2", Revision: "r1", IdentitySource: "operator-revision", Backend: domain.BackendOpenAI, Measurement: domain.ProtocolOpenAIChat, Endpoint: "https://b/v1"}},
	}}
	found := false
	for _, c := range splitColumns(r).table {
		found = found || c.Header == report.ByEndpoint.Header()
	}
	if !found {
		t.Fatal("endpoint hidden behind opaque identity suffix")
	}
}
