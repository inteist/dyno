package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"dyno/internal/report"
)

// Source reads the stored runs. It implements report.Source.
type Source struct {
	db *sql.DB
}

// NewSource returns a Source that reads from db.
func NewSource(db *sql.DB) *Source { return &Source{db: db} }

// Rows returns the runs that f selects, in the order of their start.
func (s *Source) Rows(ctx context.Context, f report.Filter) ([]report.Row, error) {
	query, args := buildRowsQuery(f)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query runs: %w", err)
	}
	defer rows.Close()
	var out []report.Row
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("read run: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read runs: %w", err)
	}
	return out, nil
}

// rowColumns are the columns of a report.Row. scanRow reads them in this
// order.
const rowColumns = `x.session_id, x.started_at, s.tag, s.ollama_version,
	s.prompt_name, s.prompt_hash, s.num_predict, s.num_ctx, s.temperature, s.seed,
	x.model_name, m.digest, m.format, m.family, m.parameter_size, m.quantization, m.size_bytes,
	x.cache_mode, x.prompt_tokens, x.cached_tokens, x.prompt_ns, x.eval_tokens, x.eval_ns,
	x.load_ns, x.total_ns, x.ttft_ns, x.wall_ns, x.done_reason, x.error`

// scanRow reads the rowColumns of the current row.
func scanRow(rows *sql.Rows) (report.Row, error) {
	var r report.Row
	w, m := &r.Workload, &r.Model
	srv, cli := &r.Measurement.Server, &r.Measurement.Client
	err := rows.Scan(
		&r.Session, timeColumn{&r.StartedAt}, &r.Tag, &r.ServerVersion,
		&w.PromptName, &w.PromptHash, &w.NumPredict, &w.NumCtx, &w.Temperature, &w.Seed,
		&m.Name, &m.Digest, &m.Format, &m.Family, &m.Parameters, &m.Quantization, &m.SizeBytes,
		&r.Mode, orZero(&srv.PromptTokens), orZero(&srv.CachedTokens), orZero(&srv.PromptDuration),
		orZero(&srv.EvalTokens), orZero(&srv.EvalDuration), orZero(&srv.LoadDuration),
		orZero(&srv.TotalDuration), orZero(&cli.TimeToFirstToken), orZero(&cli.Wall),
		orZero(&r.Measurement.DoneReason), orZero(&r.Err),
	)
	return r, err
}
