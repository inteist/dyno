package plain

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/golden"
	"dyno/internal/report"
)

// dailyReport groups one model by mode and day.
func dailyReport() report.Report {
	day := func(mode domain.CacheMode, bucket string) report.Key {
		return report.Key{Model: lfm, Mode: string(mode), Bucket: bucket}
	}
	return report.Report{
		Dimensions: []report.Dimension{report.ByModel, report.ByMode, report.ByDay},
		Groups: []report.Group{
			{
				Key: day(domain.CacheMiss, "2026-10-01"), Decode: spread(12, 151.84, 1.92),
				Prefill: median(12, 1001.6), TTFT: median(12, 1014.2), CachedShare: median(12, 0),
			},
			{
				Key: day(domain.CacheMiss, "2026-10-02"), Decode: spread(9, 149.06, 2.4),
				Prefill: median(9, 999.96), TTFT: median(9, 1012.5), CachedShare: median(9, 0.004),
				Excluded: map[report.Flag]int{report.FlagUnexpectedHit: 1, report.FlagShort: 2},
			},
			{
				Key: day(domain.CacheContinue, "2026-10-01"), Decode: spread(12, 150.2, 0.71),
				Prefill: median(12, 18.4), TTFT: median(12, 31.7), CachedShare: median(12, 0.995),
			},
		},
		Rows:     36,
		Valid:    33,
		Sessions: 4,
		First:    time.Date(2026, 10, 1, 21, 0, 0, 0, zone),
		Last:     time.Date(2026, 10, 2, 21, 40, 0, 0, zone),
	}
}

func TestReportGolden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		r    report.Report
	}{
		{"report-default", allTimeReport()},
		{"report-model-mode-day", dailyReport()},
		{"report-empty", report.Report{Dimensions: report.DefaultDimensions()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := NewReportUI(&buf).Show(t.Context(), tt.r); err != nil {
				t.Fatalf("Show() = %v", err)
			}
			golden.Assert(t, tt.name, buf.String())
		})
	}
}

func TestReportWriteErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ok   int
		r    report.Report
	}{
		{"heading of an empty report", 0, report.Report{}},
		{"heading", 0, allTimeReport()},
		{"table", 1, allTimeReport()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := NewReportUI(&failingWriter{ok: tt.ok}).Show(t.Context(), tt.r)
			if !errors.Is(err, errDiskFull) {
				t.Errorf("Show() = %v, want %v", err, errDiskFull)
			}
		})
	}
}
