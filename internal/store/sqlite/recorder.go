package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"dyno/internal/domain"
)

// Recorder writes sessions, models, and exchanges. Each call commits at
// once. It implements bench.Recorder.
type Recorder struct {
	db  *sql.DB
	now func() time.Time // the first_seen_at of a new model
}

// NewRecorder returns a Recorder that writes to db.
func NewRecorder(db *sql.DB) *Recorder {
	return &Recorder{db: db, now: time.Now}
}

const insertSession = `INSERT INTO sessions (
	id, started_at, status, tool_version, ollama_version, host, power_source, tag,
	prompt_name, prompt_hash, workload_hash, num_predict, num_ctx, temperature, seed,
	run_order, interval_ns, runs, cache_modes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// StartSession inserts s with the status running.
func (r *Recorder) StartSession(ctx context.Context, s domain.Session) error {
	_, err := r.exec(ctx, "start session "+string(s.ID), insertSession, sessionArgs(s)...)
	return err
}

// sessionArgs returns the values of insertSession.
func sessionArgs(s domain.Session) []any {
	w := s.Workload
	return []any{
		string(s.ID), formatTime(s.StartedAt), string(domain.StatusRunning),
		s.ToolVersion, s.OllamaVersion, s.Host.Name, s.Host.PowerSource, s.Tag,
		w.PromptName, w.PromptHash, w.Hash(), w.NumPredict, w.NumCtx, w.Temperature, w.Seed,
		string(s.Order), int64(s.Interval), s.Runs, joinModes(s.CacheModes),
	}
}

const insertModel = `INSERT INTO models (
	digest, format, family, parameter_size, quantization, size_bytes, first_seen_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (digest) DO NOTHING`

// SaveModel inserts m once for each digest. A known digest keeps its row.
func (r *Recorder) SaveModel(ctx context.Context, m domain.Model) error {
	_, err := r.exec(ctx, "save model "+m.Name, insertModel,
		m.Digest, m.Format, m.Family, m.Parameters, m.Quantization, m.SizeBytes, formatTime(r.now()))
	return err
}

const insertExchange = `INSERT INTO exchanges (
	session_id, model_name, model_digest, cache_mode, run_number, kind, started_at,
	prompt_tokens, cached_tokens, prompt_ns, eval_tokens, eval_ns, load_ns, total_ns,
	ttft_ns, wall_ns, done_reason, error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// SaveExchange inserts x. A failed exchange has NULL metrics and its error
// text.
func (r *Recorder) SaveExchange(ctx context.Context, x domain.Exchange) error {
	_, err := r.exec(ctx, fmt.Sprintf("save %s of %s", x.Kind, x.Step), insertExchange, exchangeArgs(x)...)
	return err
}

// exchangeArgs returns the values of insertExchange.
func exchangeArgs(x domain.Exchange) []any {
	args := []any{
		string(x.Session), x.Model.Name, x.Model.Digest, string(x.Step.Mode), x.Step.Run,
		string(x.Kind), formatTime(x.StartedAt),
	}
	args = append(args, measurementArgs(x)...)
	return append(args, sql.Null[string]{V: x.Err, Valid: x.Failed()})
}

// measurementArgs returns the metric values of x, or NULLs when x failed.
func measurementArgs(x domain.Exchange) []any {
	s, c := x.Measurement.Server, x.Measurement.Client
	args := []any{
		s.PromptTokens, s.CachedTokens, int64(s.PromptDuration), s.EvalTokens, int64(s.EvalDuration),
		int64(s.LoadDuration), int64(s.TotalDuration), int64(c.TimeToFirstToken), int64(c.Wall),
		x.Measurement.DoneReason,
	}
	if x.Failed() {
		clear(args)
	}
	return args
}

const updateSession = `UPDATE sessions SET status = ?, finished_at = ? WHERE id = ?`

// FinishSession sets the final status and the end time of the session id.
// It fails if the session does not exist.
func (r *Recorder) FinishSession(ctx context.Context, id domain.SessionID, status domain.SessionStatus, at time.Time) error {
	what := "finish session " + string(id)
	n, err := r.exec(ctx, what, updateSession, string(status), formatTime(at), string(id))
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s: the session does not exist", what)
	}
	return nil
}

// exec runs one statement and returns the number of changed rows. what
// describes the statement in the error.
func (r *Recorder) exec(ctx context.Context, what, query string, args ...any) (int64, error) {
	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	return n, nil
}
