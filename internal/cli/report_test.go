package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/report"
)

// inUTC returns c with its times in UTC, so that reflect.DeepEqual compares
// instants and not zones.
func inUTC(c ReportCommand) ReportCommand {
	c.Filter.Since, c.Filter.Until = c.Filter.Since.UTC(), c.Filter.Until.UTC()
	return c
}

func TestParseReport(t *testing.T) {
	t.Parallel()
	loc := berlin(t)
	day := func(month time.Month, d int) time.Time { return time.Date(2026, month, d, 0, 0, 0, 0, loc) }
	tests := []struct {
		name   string
		args   []string
		change func(*ReportCommand)
	}{
		{"defaults", nil, func(*ReportCommand) {}},
		{"every list", []string{
			"--model", "a,b", "--cache", "miss, continue", "--prompt", "short", "--tag", "x,y",
			"--session", "20261005T140212Z-K3F9A2", "--group", "model,mode,day", "--plain",
			"--db", "/data/x.db",
		}, func(c *ReportCommand) {
			c.Common = Common{DB: "/data/x.db"}
			c.Plain = true
			c.Filter = report.Filter{
				Models:     []string{"a:latest", "b:latest"},
				CacheModes: []domain.CacheMode{domain.CacheMiss, domain.CacheContinue},
				Prompts:    []string{"short"},
				Tags:       []string{"x", "y"},
				Sessions:   []domain.SessionID{"20261005T140212Z-K3F9A2"},
			}
			c.Dimensions = []report.Dimension{report.ByModel, report.ByMode, report.ByDay}
		}},
		{"since a date", []string{"--since", "2026-10-01"}, func(c *ReportCommand) {
			c.Filter.Since = day(time.October, 1)
		}},
		{"until a date is inclusive", []string{"--until", "2026-10-05"}, func(c *ReportCommand) {
			c.Filter.Until = day(time.October, 6)
		}},
		{"one day", []string{"--since", "2026-10-05", "--until", "2026-10-05"}, func(c *ReportCommand) {
			c.Filter.Since, c.Filter.Until = day(time.October, 5), day(time.October, 6)
		}},
		{"until the day of the clock change", []string{"--until", "2026-10-25"}, func(c *ReportCommand) {
			c.Filter.Until = day(time.October, 25).Add(25 * time.Hour)
		}},
		{"RFC 3339 times are exact", []string{"--since", "2026-10-01T08:00:00Z", "--until", "2026-10-02T09:30:00+02:00"}, func(c *ReportCommand) {
			c.Filter.Since = time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
			c.Filter.Until = time.Date(2026, 10, 2, 7, 30, 0, 0, time.UTC)
		}},
		{"group by week", []string{"--group", "model,week"}, func(c *ReportCommand) {
			c.Dimensions = []report.Dimension{report.ByModel, report.ByWeek}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, stderr, err := parse(testEnv(t, nil), append([]string{"report"}, tt.args...)...)
			if err != nil {
				t.Fatalf("Parse() error = %v, stderr:\n%s", err, stderr)
			}
			want := ReportCommand{Common: Common{DB: homeDB}, Dimensions: report.DefaultDimensions()}
			tt.change(&want)
			if got := inUTC(cmd.(ReportCommand)); !reflect.DeepEqual(got, inUTC(want)) {
				t.Errorf("Parse() =\n%+v\nwant\n%+v", got, inUTC(want))
			}
		})
	}
}

func TestParseReportErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		text string
	}{
		{"unknown dimension", []string{"--group", "model,colour"}, `unknown dimension "colour"`},
		{"dimension twice", []string{"--group", "model,mode,model"}, "the dimension model occurs more than once"},
		{"day and week", []string{"--group", "week,model,day"}, "use day or week, not both"},
		{"unknown cache mode", []string{"--cache", "hit"}, `unknown cache mode "hit"`},
		{"not a date", []string{"--since", "yesterday"}, "use YYYY-MM-DD or RFC 3339"},
		{"no such day", []string{"--until", "2026-02-30"}, "use YYYY-MM-DD or RFC 3339"},
		{"since after until", []string{"--since", "2026-10-06", "--until", "2026-10-05"}, "--since must be before --until"},
		{"run flag", []string{"--host", "http://mac:11434"}, "not defined: -host"},
		{"log flag of run", []string{"--log", "/tmp/report.log"}, "not defined: -log"},
		{"positional argument", []string{"today"}, `unexpected argument "today"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, _, err := parse(testEnv(t, nil), append([]string{"report"}, tt.args...)...)
			if !errors.Is(err, ErrUsage) || cmd != nil {
				t.Fatalf("Parse() = %v, %v, want ErrUsage", cmd, err)
			}
			if !strings.Contains(err.Error(), tt.text) {
				t.Errorf("error = %q, want %q in it", err, tt.text)
			}
		})
	}
}
