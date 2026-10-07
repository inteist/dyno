package sqlite

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/report"
)

func TestModelShorthandUsesReportIndex(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	s := testSession("indexed", day(5), "", "short", 64)
	m, _ := testModels()
	m.Name = "bench:latest"
	models := []domain.Model{m}
	runs := []domain.Exchange{exchange(s, m, domain.KindRun, domain.CacheMiss, 1, day(5), measurement(1))}
	for i := range 1000 {
		other := m
		other.Name = fmt.Sprintf("other-%d:latest", i)
		models = append(models, other)
		runs = append(runs, exchange(s, other, domain.KindRun, domain.CacheMiss, 1, day(5), measurement(1)))
	}
	record(t, NewRecorder(db), []domain.Session{s}, models, runs)
	if _, err := db.Exec("ANALYZE"); err != nil {
		t.Fatal(err)
	}
	// Selective populated data and statistics exercise the planner, rather than
	// requiring one optimizer decision for a one-row table.
	for _, f := range []report.Filter{
		{Models: []string{"bench"}},
		{Models: []string{"bench"}, Backends: []string{"ollama"}, CacheModes: []domain.CacheMode{domain.CacheMiss}},
		{Models: []string{"bench"}, Backends: []string{"openai"}},
		{Models: []string{"bench:latest"}},
	} {
		query, args := buildRowsQuery(f)
		plan := snapshotRows(t, db, "EXPLAIN QUERY PLAN "+query, args...)
		var details []string
		for _, row := range plan {
			details = append(details, row[3].(string))
		}
		detail := strings.Join(details, "\n")
		t.Logf("filter %+v:\n%s", f, detail)
		if !strings.Contains(detail, "model_name=?") {
			t.Errorf("model filter does not constrain the report index by name:\n%s", detail)
		}
		if len(f.CacheModes) != 0 && !strings.Contains(detail, "cache_mode=?") {
			t.Errorf("cache filter does not constrain the third index column:\n%s", detail)
		}
	}
}

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
			where: runs + " AND x.model_name IN (?, ?, ?, ?) AND (s.backend = 'ollama' OR x.model_name IN (?, ?))",
			args:  []any{"a", "b", "a:latest", "b:latest", "a", "b"},
		},
		{
			name:  "overlapping shorthand and tagged names",
			f:     report.Filter{Models: []string{"a", "a:latest", "a"}},
			where: runs + " AND x.model_name IN (?, ?, ?, ?, ?) AND (s.backend = 'ollama' OR x.model_name IN (?, ?, ?))",
			args:  []any{"a", "a:latest", "a", "a:latest", "a:latest", "a", "a:latest", "a"},
		},
		{
			name:  "tagged model",
			f:     report.Filter{Models: []string{"a:latest"}},
			where: runs + " AND x.model_name IN (?)",
			args:  []any{"a:latest"},
		},
		{
			name:  "backends",
			f:     report.Filter{Backends: []string{"ollama", "openai"}},
			where: runs + " AND s.backend IN (?, ?)",
			args:  []any{"ollama", "openai"},
		},
		{
			name:  "endpoints including legacy unknown",
			f:     report.Filter{Endpoints: []string{"https://example.test/api/v1", ""}},
			where: runs + " AND COALESCE(s.endpoint, '') IN (?, ?)",
			args:  []any{"https://example.test/api/v1", ""},
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
				Backends: []string{"openai"}, Endpoints: []string{"https://example.test/api/v1"},
			},
			where: runs + " AND x.started_at >= ? AND x.started_at < ?" +
				" AND x.model_name IN (?, ?) AND (s.backend = 'ollama' OR x.model_name IN (?))" +
				" AND s.backend IN (?) AND COALESCE(s.endpoint, '') IN (?)" +
				" AND x.cache_mode IN (?) AND s.prompt_name IN (?) AND s.tag IN (?)" +
				" AND x.session_id IN (?) AND s.workload_hash IN (?)",
			args: []any{sinceText, untilText, "m", "m:latest", "m", "openai", "https://example.test/api/v1", "continue", "p", "t", "S", "w"},
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
