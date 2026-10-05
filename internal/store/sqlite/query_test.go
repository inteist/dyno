package sqlite

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/report"
)

func TestBuildRowsQuery(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 10, 1, 2, 0, 0, 0, cest())
	until := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	const (
		sinceText = "2026-10-01T00:00:00.000000000Z"
		untilText = "2026-10-08T00:00:00.000000000Z"
		runs      = "x.kind = 'run'"
	)
	tests := []struct {
		name  string
		f     report.Filter
		where string
		args  []any
	}{
		{name: "zero", where: runs},
		{name: "since", f: report.Filter{Since: since}, where: runs + " AND x.started_at >= ?", args: []any{sinceText}},
		{name: "until", f: report.Filter{Until: until}, where: runs + " AND x.started_at < ?", args: []any{untilText}},
		{
			name:  "models",
			f:     report.Filter{Models: []string{"a", "b"}},
			where: runs + " AND x.model_name IN (?, ?)",
			args:  []any{"a", "b"},
		},
		{
			name:  "cache modes",
			f:     report.Filter{CacheModes: []domain.CacheMode{domain.CacheMiss, domain.CachePrefix}},
			where: runs + " AND x.cache_mode IN (?, ?)",
			args:  []any{"miss", "prefix"},
		},
		{
			name:  "prompts",
			f:     report.Filter{Prompts: []string{"long"}},
			where: runs + " AND s.prompt_name IN (?)",
			args:  []any{"long"},
		},
		{
			name:  "tags",
			f:     report.Filter{Tags: []string{"nightly", "", "ac"}},
			where: runs + " AND s.tag IN (?, ?, ?)",
			args:  []any{"nightly", "", "ac"},
		},
		{
			name:  "sessions",
			f:     report.Filter{Sessions: []domain.SessionID{"S1"}},
			where: runs + " AND x.session_id IN (?)",
			args:  []any{"S1"},
		},
		{
			name:  "workloads",
			f:     report.Filter{Workloads: []string{"21e02d1b0274", "000000000000"}},
			where: runs + " AND s.workload_hash IN (?, ?)",
			args:  []any{"21e02d1b0274", "000000000000"},
		},
		{
			name: "combined",
			f: report.Filter{
				Since: since, Until: until, Models: []string{"m"}, CacheModes: []domain.CacheMode{domain.CacheContinue},
				Prompts: []string{"p"}, Tags: []string{"t"}, Sessions: []domain.SessionID{"S"}, Workloads: []string{"w"},
			},
			where: runs + " AND x.started_at >= ? AND x.started_at < ? AND x.model_name IN (?)" +
				" AND x.cache_mode IN (?) AND s.prompt_name IN (?) AND s.tag IN (?)" +
				" AND x.session_id IN (?) AND s.workload_hash IN (?)",
			args: []any{sinceText, untilText, "m", "continue", "p", "t", "S", "w"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			query, args := buildRowsQuery(tt.f)
			rest, ok := strings.CutPrefix(query, rowsFrom+"\nWHERE ")
			if !ok {
				t.Fatalf("the query does not start with the select:\n%s", query)
			}
			if want := tt.where + rowsOrder; rest != want {
				t.Errorf("WHERE %s\nwant  %s", rest, want)
			}
			if !reflect.DeepEqual(args, tt.args) {
				t.Errorf("args = %#v, want %#v", args, tt.args)
			}
		})
	}
}
