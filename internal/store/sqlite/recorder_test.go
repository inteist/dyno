package sqlite

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/report"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	rec := NewRecorder(db)
	seen := time.Date(2026, 10, 5, 14, 0, 0, 5, time.UTC)
	rec.now = func() time.Time { return seen }

	qwen, lfm := testModels()
	alias := qwen // the same digest under a second name
	alias.Name, alias.Family = "qwen3.5:latest", "ignored"
	s := testSession("20261005T140212Z-K3F9A2", day(5), "nightly", "long", 512)
	at := func(sec int) time.Time { return s.StartedAt.Add(time.Duration(sec) * time.Second) }
	warmup := exchange(s, qwen, domain.KindWarmup, domain.CacheMiss, 1, at(1), measurement(1))
	runMiss := exchange(s, qwen, domain.KindRun, domain.CacheMiss, 1, at(3), measurement(2))
	primer := exchange(s, qwen, domain.KindPrimer, domain.CacheContinue, 1, at(4), measurement(3))
	runContinue := exchange(s, qwen, domain.KindRun, domain.CacheContinue, 1, at(5), measurement(4))
	runFailed := failed(exchange(s, lfm, domain.KindRun, domain.CacheMiss, 1, at(7), measurement(5)), "connection refused")
	runSameStart := exchange(s, lfm, domain.KindRun, domain.CacheMiss, 2, at(7), measurement(6))
	runSameStart.Measurement.DoneReason = "stop"
	runAlias := exchange(s, alias, domain.KindRun, domain.CachePrefix, 1, at(6), measurement(7))

	record(t, rec, []domain.Session{s}, []domain.Model{qwen, lfm, alias},
		[]domain.Exchange{warmup, runMiss, primer, runContinue, runFailed, runSameStart, runAlias})

	got, err := NewSource(db).Rows(t.Context(), report.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []report.Row{
		wantRow(s, qwen, runMiss),
		wantRow(s, qwen, runContinue),
		wantRow(s, qwen, runAlias),
		wantRow(s, lfm, runFailed),
		wantRow(s, lfm, runSameStart),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Rows() =\n%+v\nwant\n%+v", got, want)
	}

	checkSessionRow(t, db, s)
	checkModelRows(t, db, qwen, seen)
	checkStoredMetrics(t, db)
}

// checkSessionRow compares the columns that Row does not return.
func checkSessionRow(t *testing.T, db *sql.DB, s domain.Session) {
	t.Helper()
	var got sessionColumns
	err := db.QueryRowContext(t.Context(), `SELECT started_at, finished_at IS NULL, status, tool_version,
		host, power_source, workload_hash, run_order, interval_ns, runs, cache_modes
		FROM sessions WHERE id = ?`, string(s.ID)).Scan(&got.startedAt, &got.unfinished, &got.status,
		&got.tool, &got.host, &got.power, &got.workload, &got.order, &got.interval, &got.runs, &got.modes)
	if err != nil {
		t.Fatal(err)
	}
	want := sessionColumns{
		startedAt: "2026-10-05T14:02:12.123456789Z", unfinished: true, status: "running", tool: s.ToolVersion,
		host: s.Host.Name, power: s.Host.PowerSource, workload: s.Workload.Hash(), order: "round-robin",
		interval: int64(90 * time.Second), runs: 3, modes: "miss,continue,prefix",
	}
	if got != want {
		t.Errorf("session row = %+v, want %+v", got, want)
	}
}

type sessionColumns struct {
	startedAt                                         string
	unfinished                                        bool
	status, tool, host, power, workload, order, modes string
	interval                                          int64
	runs                                              int
}

// checkModelRows checks that the first row of a digest stays.
func checkModelRows(t *testing.T, db *sql.DB, first domain.Model, seen time.Time) {
	t.Helper()
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM models"); n != 2 {
		t.Errorf("%d model rows, want 2", n)
	}
	var family, seenAt string
	err := db.QueryRowContext(t.Context(), "SELECT family, first_seen_at FROM models WHERE digest = ?",
		first.Digest).Scan(&family, &seenAt)
	if err != nil {
		t.Fatal(err)
	}
	if family != first.Family || seenAt != "2026-10-05T14:00:00.000000005Z" {
		t.Errorf("model row = %q, %q, want %q, %q", family, seenAt, first.Family, formatTime(seen))
	}
}

// checkStoredMetrics checks the NULL rules of the metric columns.
func checkStoredMetrics(t *testing.T, db *sql.DB) {
	t.Helper()
	const allNull = `COALESCE(prompt_tokens, cached_tokens, prompt_ns, eval_tokens, eval_ns,
		load_ns, total_ns, ttft_ns, wall_ns, done_reason) IS NULL`
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM exchanges WHERE error = 'connection refused' AND "+allNull); n != 1 {
		t.Errorf("%d failed rows with NULL metrics, want 1", n)
	}
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM exchanges WHERE error IS NULL"); n != 6 {
		t.Errorf("%d rows with a NULL error, want 6", n)
	}
}

func TestFinishSession(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	rec := NewRecorder(db)
	s := testSession("S1", day(5), "", "short", 64)
	record(t, rec, []domain.Session{s}, nil, nil)

	end := time.Date(2026, 10, 5, 18, 30, 0, 0, cest())
	if err := rec.FinishSession(t.Context(), s.ID, domain.StatusAborted, end); err != nil {
		t.Fatal(err)
	}
	var status, finished string
	err := db.QueryRowContext(t.Context(), "SELECT status, finished_at FROM sessions WHERE id = ?", "S1").
		Scan(&status, &finished)
	if err != nil {
		t.Fatal(err)
	}
	if status != "aborted" || finished != "2026-10-05T16:30:00.000000000Z" {
		t.Errorf("session = %q, %q", status, finished)
	}

	if err := rec.FinishSession(t.Context(), "missing", domain.StatusCompleted, end); err == nil {
		t.Error("FinishSession of a missing session gave no error")
	}
}

func TestSaveExchangeChecksReferences(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	rec := NewRecorder(db)
	qwen, lfm := testModels()
	s := testSession("S1", day(5), "", "short", 64)
	record(t, rec, []domain.Session{s}, []domain.Model{qwen}, nil)

	tests := map[string]domain.Exchange{
		"unknown model":   exchange(s, lfm, domain.KindRun, domain.CacheMiss, 1, day(5), measurement(1)),
		"unknown session": exchange(testSession("S2", day(5), "", "short", 64), qwen, domain.KindRun, domain.CacheMiss, 1, day(5), measurement(1)),
		"unknown mode":    exchange(s, qwen, domain.KindRun, "hit", 1, day(5), measurement(1)),
	}
	for name, x := range tests {
		if err := rec.SaveExchange(t.Context(), x); err == nil {
			t.Errorf("%s: SaveExchange gave no error", name)
		}
	}
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM exchanges"); n != 0 {
		t.Errorf("%d exchanges were stored, want 0", n)
	}
}

func TestClosedDatabaseFails(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	db.Close()
	rec := NewRecorder(db)
	qwen, _ := testModels()
	s := testSession("S1", day(5), "", "short", 64)
	ctx := t.Context()
	errs := map[string]error{
		"StartSession":  rec.StartSession(ctx, s),
		"SaveModel":     rec.SaveModel(ctx, qwen),
		"SaveExchange":  rec.SaveExchange(ctx, exchange(s, qwen, domain.KindRun, domain.CacheMiss, 1, day(5), measurement(1))),
		"FinishSession": rec.FinishSession(ctx, s.ID, domain.StatusCompleted, day(5)),
	}
	_, errs["Rows"] = NewSource(db).Rows(ctx, report.Filter{})
	for name, err := range errs {
		if err == nil {
			t.Errorf("%s on a closed database gave no error", name)
		}
	}
}
