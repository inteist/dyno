package reportview

import (
	"strings"
	"testing"
)

func TestNativeReportWithProductionWallTimeKeepsCacheAndExclusions(t *testing.T) {
	r := sampleReport(3)
	if r.Groups[0].Latency.N == 0 || r.Groups[0].ClientRate.N == 0 {
		t.Fatal("fixture is missing production wall time")
	}
	cols := splitColumns(r)
	var headers []string
	for _, c := range cols.table {
		headers = append(headers, c.Header)
	}
	joined := strings.Join(headers, "|")
	if strings.Contains(joined, "LATENCY") || strings.Contains(joined, "CLIENT") || strings.Contains(joined, "MODEL-ID") {
		t.Fatal("redundant native columns crowd real metrics", joined)
	}
	if !strings.Contains(joined, "CACHED") || !strings.Contains(joined, "EXCLUDED") {
		t.Fatal("essential native columns missing", joined)
	}
}
