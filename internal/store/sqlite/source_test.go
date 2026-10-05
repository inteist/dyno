package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/report"
)

// filterData is a store with two sessions that differ in every filter
// field, and the runs in the order of their start.
type filterData struct {
	source    *Source
	a, b      domain.Session
	qwen, lfm domain.Model
	runs      []domain.Exchange
}

func newFilterData(t *testing.T) filterData {
	t.Helper()
	db := openTest(t)
	qwen, lfm := testModels()
	a := testSession("A", day(1), "nightly", "short", 64)
	b := testSession("B", day(2), "", "long", 512)
	at := func(s domain.Session, min int) time.Time { return s.StartedAt.Add(time.Duration(min) * time.Minute) }
	d := filterData{
		source: NewSource(db), a: a, b: b, qwen: qwen, lfm: lfm,
		runs: []domain.Exchange{
			exchange(a, qwen, domain.KindRun, domain.CacheMiss, 1, at(a, 1), measurement(1)),
			exchange(a, qwen, domain.KindRun, domain.CachePrefix, 1, at(a, 2), measurement(2)),
			exchange(a, lfm, domain.KindRun, domain.CacheMiss, 1, at(a, 3), measurement(3)),
			exchange(b, qwen, domain.KindRun, domain.CacheContinue, 1, at(b, 1), measurement(4)),
			failed(exchange(b, lfm, domain.KindRun, domain.CacheMiss, 1, at(b, 2), measurement(5)), "timeout"),
			exchange(b, lfm, domain.KindRun, domain.CachePrefix, 1, at(b, 3), measurement(6)),
		},
	}
	// Warm-ups and primers are never rows. The runs go in reverse, so that
	// the order comes from the query.
	xs := []domain.Exchange{
		exchange(a, qwen, domain.KindWarmup, domain.CacheMiss, 1, at(a, 0), measurement(7)),
		exchange(b, qwen, domain.KindPrimer, domain.CacheContinue, 1, at(b, 0), measurement(8)),
	}
	for i := len(d.runs) - 1; i >= 0; i-- {
		xs = append(xs, d.runs[i])
	}
	record(t, NewRecorder(db), []domain.Session{a, b}, []domain.Model{qwen, lfm}, xs)
	return d
}

// starts returns the start of each row.
func starts(rows []report.Row) []time.Time {
	var out []time.Time
	for _, r := range rows {
		out = append(out, r.StartedAt)
	}
	return out
}

// startsOf returns the start of the runs with the indexes in UTC.
func (d filterData) startsOf(indexes ...int) []time.Time {
	var out []time.Time
	for _, i := range indexes {
		out = append(out, d.runs[i].StartedAt.UTC())
	}
	return out
}

func TestRowsAppliesFilters(t *testing.T) {
	t.Parallel()
	d := newFilterData(t)
	tests := []struct {
		name string
		f    report.Filter
		want []int // indexes of d.runs
	}{
		{name: "all runs", want: []int{0, 1, 2, 3, 4, 5}},
		{name: "one model", f: report.Filter{Models: []string{d.lfm.Name}}, want: []int{2, 4, 5}},
		{name: "two models", f: report.Filter{Models: []string{d.lfm.Name, d.qwen.Name}}, want: []int{0, 1, 2, 3, 4, 5}},
		{name: "unknown model", f: report.Filter{Models: []string{"nope"}}},
		{name: "one mode", f: report.Filter{CacheModes: []domain.CacheMode{domain.CacheMiss}}, want: []int{0, 2, 4}},
		{
			name: "two modes",
			f:    report.Filter{CacheModes: []domain.CacheMode{domain.CacheContinue, domain.CachePrefix}},
			want: []int{1, 3, 5},
		},
		{name: "since is inclusive", f: report.Filter{Since: d.runs[1].StartedAt}, want: []int{1, 2, 3, 4, 5}},
		{name: "since in UTC", f: report.Filter{Since: d.runs[3].StartedAt.UTC()}, want: []int{3, 4, 5}},
		{name: "until is exclusive", f: report.Filter{Until: d.runs[2].StartedAt}, want: []int{0, 1}},
		{
			name: "since and until",
			f:    report.Filter{Since: d.runs[2].StartedAt, Until: d.runs[4].StartedAt.Add(time.Nanosecond)},
			want: []int{2, 3, 4},
		},
		{name: "prompt", f: report.Filter{Prompts: []string{"long"}}, want: []int{3, 4, 5}},
		{name: "tag", f: report.Filter{Tags: []string{"nightly"}}, want: []int{0, 1, 2}},
		{name: "empty tag", f: report.Filter{Tags: []string{""}}, want: []int{3, 4, 5}},
		{name: "session", f: report.Filter{Sessions: []domain.SessionID{d.b.ID}}, want: []int{3, 4, 5}},
		{name: "workload", f: report.Filter{Workloads: []string{d.a.Workload.Hash()}}, want: []int{0, 1, 2}},
		{
			name: "combined",
			f: report.Filter{
				Models: []string{d.lfm.Name}, CacheModes: []domain.CacheMode{domain.CacheMiss, domain.CachePrefix},
				Since: d.runs[3].StartedAt,
			},
			want: []int{4, 5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rows, err := d.source.Rows(t.Context(), tt.f)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := starts(rows), d.startsOf(tt.want...); !reflect.DeepEqual(got, want) {
				t.Errorf("starts = %v\nwant     %v", got, want)
			}
		})
	}
}

func TestReaderWorksWhileWriterWrites(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "bench.db")
	writer, reader := openPath(t, path), openPath(t, path)
	rec := NewRecorder(writer)
	qwen, _ := testModels()
	s := testSession("S1", day(5), "", "short", 64)
	run := func(n int) domain.Exchange {
		return exchange(s, qwen, domain.KindRun, domain.CacheMiss, n, day(5).Add(time.Duration(n)*time.Second), measurement(n))
	}
	record(t, rec, []domain.Session{s}, []domain.Model{qwen}, []domain.Exchange{run(1)})
	source := NewSource(reader)

	// A write transaction is open: the reader sees the committed runs.
	tx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, insertExchange, exchangeArgs(run(2))...); err != nil {
		t.Fatal(err)
	}
	assertRuns(t, source, 1)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertRuns(t, source, 2)

	// A read transaction is open: the writer still commits at once. The
	// transaction declares that it only reads; other transactions take the
	// write lock at their start.
	readTx, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	var before int
	if err := readTx.QueryRowContext(ctx, "SELECT COUNT(*) FROM exchanges").Scan(&before); err != nil {
		t.Fatal(err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := rec.SaveExchange(writeCtx, run(3)); err != nil {
		t.Fatalf("the writer waited for the reader: %v", err)
	}
	if err := readTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertRuns(t, source, 3)
}

// assertRuns checks the number of runs that source returns.
func assertRuns(t *testing.T, source *Source, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	rows, err := source.Rows(ctx, report.Filter{})
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != want {
		t.Errorf("Rows returned %d runs, want %d", len(rows), want)
	}
}
