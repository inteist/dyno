package sqlite

import (
	"strings"

	"dyno/internal/report"
)

// rowsFrom selects the rowColumns of every exchange with its session and
// model.
const rowsFrom = `SELECT ` + rowColumns + `
FROM exchanges x
JOIN sessions s ON s.id = x.session_id
JOIN models m ON m.digest = x.model_digest`

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
	in(&c, "x.model_name", f.Models)
	in(&c, "x.cache_mode", f.CacheModes)
	in(&c, "s.prompt_name", f.Prompts)
	in(&c, "s.tag", f.Tags)
	in(&c, "x.session_id", f.Sessions)
	in(&c, "s.workload_hash", f.Workloads)
	return rowsFrom + "\nWHERE " + strings.Join(c.sql, " AND ") + rowsOrder, c.args
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
