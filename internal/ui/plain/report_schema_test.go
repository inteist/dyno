package plain

import (
	"bytes"
	"dyno/internal/report"
	"strings"
	"testing"
)

func TestPlainTableColumnsDoNotDependOnMeasuredData(t *testing.T) {
	base := allTimeReport()
	measured := allTimeReport()
	for i := range measured.Groups {
		measured.Groups[i].Latency = median(10, 8000)
		measured.Groups[i].ClientRate = median(10, 60)
		measured.Groups[i].Warnings = map[report.Warning]int{report.WarningCacheUnverified: 1}
	}
	header := func(r report.Report) string {
		t.Helper()
		var out bytes.Buffer
		if err := NewReportUI(&out).Show(t.Context(), r); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(out.String(), "\n")
		if len(lines) < 2 || !strings.HasPrefix(lines[1], "MODEL") {
			t.Fatalf("facts displaced table header: %s", out.String())
		}
		return strings.Join(strings.Fields(lines[1]), " ")
	}
	if a, b := header(base), header(measured); a != b {
		t.Fatalf("data-dependent plain columns:\n%s\n%s", a, b)
	}
}
