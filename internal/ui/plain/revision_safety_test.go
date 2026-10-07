package plain

import (
	"bytes"
	"strings"
	"testing"
)

func TestHistoricalRevisionFactsAreSafeAndMapped(t *testing.T) {
	r := allTimeReport()
	r.Groups = r.Groups[:1]
	r.Groups[0].Key.Revision = "rev\x1b[31m\nnext\u202e"
	r.Groups[0].Key.Endpoint = "https://host/v1"
	var out bytes.Buffer
	if err := NewReportUI(&out).Show(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.ContainsAny(text, "\x1b\u202e") || strings.Contains(text, "rev [31m\nnext") {
		t.Fatal("historical label escaped sanitization", text)
	}
	if !strings.Contains(text, "\n\nmodel revision: "+r.Groups[0].Key.Model+" · https://host/v1 · rev") {
		t.Fatal("revision fact lacks mapping/separation", text)
	}
}
