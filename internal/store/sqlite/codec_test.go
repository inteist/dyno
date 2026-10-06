package sqlite

import (
	"testing"
	"time"

	"dyno/internal/domain"
)

func TestFormatTime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 10, 5, 16, 2, 12, 123456789, cest()), "2026-10-05T14:02:12.123456789Z"},
		{time.Date(2026, 10, 5, 14, 2, 12, 0, time.UTC), "2026-10-05T14:02:12.000000000Z"},
		{time.Date(2026, 10, 5, 14, 2, 12, 500_000_000, time.UTC), "2026-10-05T14:02:12.500000000Z"},
	}
	for _, tt := range tests {
		got := formatTime(tt.in)
		if got != tt.want {
			t.Errorf("formatTime(%v) = %q, want %q", tt.in, got, tt.want)
		}
		back, err := parseTime(got)
		if err != nil || !back.Equal(tt.in) || back.Location() != time.UTC {
			t.Errorf("parseTime(%q) = %v, %v, want %v in UTC", got, back, err, tt.in)
		}
	}
	// Text order is time order, also when a time has no fraction.
	if a, b := formatTime(tests[1].in), formatTime(tests[2].in); a >= b {
		t.Errorf("%q sorts after %q", a, b)
	}
}

func TestParseTimeRejectsOtherText(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "2026-10-05", "2026-10-05T14:02:12Z", "2026-10-05T16:02:12.000000000+02:00"} {
		if _, err := parseTime(s); err == nil {
			t.Errorf("parseTime(%q) gave no error", s)
		}
	}
	var dst time.Time
	if err := (timeColumn{&dst}).Scan(nil); err == nil {
		t.Error("a NULL time gave no error")
	}
	if err := (timeColumn{&dst}).Scan([]byte("2026-10-05T14:02:12.000000000Z")); err != nil || dst.IsZero() {
		t.Errorf("Scan of bytes = %v, %v", dst, err)
	}
}

func TestJoinModes(t *testing.T) {
	t.Parallel()
	modes := []domain.CacheMode{domain.CachePrefix, domain.CacheMiss}
	if got := joinModes(modes); got != "prefix,miss" {
		t.Errorf("joinModes() = %q", got)
	}
	if got := joinModes(nil); got != "" {
		t.Errorf("joinModes(nil) = %q", got)
	}
}
