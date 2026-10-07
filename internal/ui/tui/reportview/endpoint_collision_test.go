package reportview

import (
	"fmt"
	"strings"
	"testing"

	"dyno/internal/report"
	"github.com/charmbracelet/x/ansi"
)

func TestCrossEndpointRevisionsStayDistinctInVisibleTable(t *testing.T) {
	r := timingOnlyReport()
	r.Dimensions = report.DefaultDimensions()
	r.Groups = append(r.Groups, r.Groups[0])
	for i := range r.Groups {
		k := &r.Groups[i].Key
		k.Model = "shared-model"
		k.Revision, k.IdentitySource = "r1", "operator-revision"
		k.Digest = "openai:revision:" + strings.Repeat(string(rune('a'+i)), 64)
		k.Endpoint = fmt.Sprintf("https://gpu-%c.example.internal/llm/v1", 'a'+i)
		r.Groups[i].Endpoints = []string{k.Endpoint}
	}
	for _, width := range []int{60, 72, 80, 100, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := resize(New(r), size{width, 24})
			assertFits(t, screen(m), size{width, 24})
			// Compare fitted table-row text with the selection marker removed,
			// never the selected group's full-detail line under the table.
			rows := groupRows(m.columns.table, r.Groups, m.fit, -1)
			visible := func(row []string) string {
				return ansi.Truncate(strings.Join(row[1:], strings.Repeat(" ", m.fit.gap)), width-markerWidth, "")
			}
			if a, b := visible(rows[0]), visible(rows[1]); a == b {
				t.Fatalf("width %d renders identical data rows %q:\n%s", width, a, ansi.Strip(m.table.View()))
			}
		})
	}
}
