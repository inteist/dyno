package sqlite

import (
	"strings"

	"dyno/internal/domain"
	"dyno/internal/report"
)

// rowsFrom selects the rowColumns of every exchange with its session and
// model.
const rowsFrom = `SELECT ` + rowColumns + `
FROM exchanges x
JOIN sessions s ON s.id = x.session_id
JOIN models m ON m.model_key = x.model_key`

// rowsOrder sorts the runs by start. The ID keeps the insert order of runs
// with the same start.
const rowsOrder = ` ORDER BY x.started_at, x.id`

// buildRowsQuery returns the query for the runs that f selects and its
// arguments. Each non-zero field of f adds one condition.
func buildRowsQuery(f report.Filter) (string, []any) {
	c := conditions{sql: []string{"x.kind = 'run'"}}
	if !f.Since.IsZero() {
		c.add("x.started_at >= ?", formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		c.add("x.started_at < ?", formatTime(f.Until))
	}
	modelNames(&c, f.Models)
	in(&c, "s.backend", f.Backends)
	// An empty requested endpoint explicitly includes legacy unknown endpoints.
	// Callers decide whether their history should include those native rows.
	in(&c, "COALESCE(s.endpoint, '')", f.Endpoints)
	in(&c, "x.cache_mode", f.CacheModes)
	in(&c, "s.prompt_name", f.Prompts)
	in(&c, "s.tag", f.Tags)
	in(&c, "x.session_id", f.Sessions)
	in(&c, "s.workload_hash", f.Workloads)
	return rowsFrom + "\nWHERE " + strings.Join(c.sql, " AND ") + rowsOrder, c.args
}

// modelNames matches opaque names literally on every backend. The Ollama
// shorthand alternative applies only to native rows, never external aliases.
func modelNames(c *conditions, names []string) {
	if len(names) == 0 {
		return
	}
	exact := conditions{}
	in(&exact, "x.model_name", names)
	var normalized []string
	for _, name := range names {
		if n := domain.NormalizeModelName(name); n != name {
			normalized = append(normalized, n)
		}
	}
	if len(normalized) == 0 {
		c.add(exact.sql[0], exact.args...)
		return
	}
	// Constrain the composite report index before the backend guard; an OR
	// spanning exchanges and sessions alone makes SQLite visit every run.
	candidates := append(append([]string(nil), names...), normalized...)
	in(c, "x.model_name", candidates)
	c.add("(s.backend = 'ollama' OR "+exact.sql[0]+")", exact.args...)
}

// conditions collects the parts of a WHERE clause and their arguments.
type conditions struct {
	sql  []string
	args []any
}

// add appends one condition with its arguments.
func (c *conditions) add(sql string, args ...any) {
	c.sql = append(c.sql, sql)
	c.args = append(c.args, args...)
}

// in adds "column IN (?, ...)" with one argument for each value. It adds
// nothing when values is empty.
func in[T ~string](c *conditions, column string, values []T) {
	if len(values) == 0 {
		return
	}
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = string(v)
	}
	marks := strings.Repeat(", ?", len(values))[2:]
	c.add(column+" IN ("+marks+")", args...)
}
