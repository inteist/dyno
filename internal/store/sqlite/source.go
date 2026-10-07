package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"dyno/internal/domain"
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
const rowColumns = `x.session_id, x.started_at, s.tag, s.server_version,
	s.backend, s.server_kind, s.version_source, s.endpoint, s.measurement_protocol, s.context_policy, s.config_json,
	s.prompt_name, s.prompt_hash, s.num_predict, s.num_ctx, s.temperature, s.seed,
	x.model_name, m.model_key, m.digest, m.identity_source, m.revision,
	m.format, m.family, m.parameter_size, m.quantization, m.size_bytes,
	x.cache_mode, x.prompt_tokens, x.cached_tokens, x.prompt_ns, x.eval_tokens, x.eval_ns,
	x.load_ns, x.total_ns, x.ttft_ns, x.wall_ns, x.done_reason, x.error`

// scanRow reads the rowColumns of the current row.
func scanRow(rows *sql.Rows) (report.Row, error) {
	var r report.Row
	w, m := &r.Workload, &r.Model
	srv, cli := &r.Measurement.Server, &r.Measurement.Client
	srv.Available = &domain.MetricAvailability{}
	// Thinking support was never stored; size retains its SQL NULL availability.
	m.MetadataAvailable = &domain.ModelMetadataAvailability{}
	a := srv.Available
	var ttft, wall sql.Null[int64]
	err := rows.Scan(
		&r.Session, timeColumn{&r.StartedAt}, &r.Tag, &r.ServerVersion,
		&r.Backend, &r.ServerKind, orZero(&r.VersionSource), orZero(&r.Endpoint),
		&r.MeasurementProtocol, &r.ContextPolicy, orZero(&r.ConfigJSON),
		&w.PromptName, &w.PromptHash, &w.NumPredict, &w.NumCtx, &w.Temperature, &w.Seed,
		&m.Name, &m.Identity, orZero(&m.Digest), orZero(&m.IdentitySource), orZero(&m.Revision),
		orZero(&m.Format), orZero(&m.Family), orZero(&m.Parameters), orZero(&m.Quantization), metricColumn(&m.SizeBytes, &m.MetadataAvailable.SizeBytes),
		&r.Mode, metricColumn(&srv.PromptTokens, &a.PromptTokens), metricColumn(&srv.CachedTokens, &a.CachedTokens),
		metricColumn(&srv.PromptDuration, &a.PromptDuration), metricColumn(&srv.EvalTokens, &a.EvalTokens),
		metricColumn(&srv.EvalDuration, &a.EvalDuration), metricColumn(&srv.LoadDuration, &a.LoadDuration),
		metricColumn(&srv.TotalDuration, &a.TotalDuration), &ttft, &wall,
		orZero(&r.Measurement.DoneReason), orZero(&r.Err),
	)
	// ClientMetrics represents absent TTFT as zero; completed wall time is measured.
	cli.TimeToFirstToken, cli.Wall = time.Duration(ttft.V), time.Duration(wall.V)
	return r, err
}
