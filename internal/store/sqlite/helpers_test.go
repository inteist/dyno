package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/report"
)

// openTest opens a new database file in a temporary folder.
func openTest(t *testing.T) *sql.DB {
	t.Helper()
	return openPath(t, filepath.Join(t.TempDir(), "bench.db"))
}

// openPath opens the database file at path and closes it after the test.
func openPath(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// cest returns a zone other than UTC, so that tests see the conversion.
func cest() *time.Location { return time.FixedZone("CEST", 2*3600) }

// day returns a start time of a session with nanoseconds.
func day(d int) time.Time { return time.Date(2026, 10, d, 16, 2, 12, 123456789, cest()) }

func testModels() (qwen, lfm domain.Model) {
	qwen = domain.Model{
		Name: "qwen3.5:9b-mlx", Digest: "sha256:aaaa0000aaaa0000", Format: "safetensors",
		Family: "qwen3_5", Parameters: "9.4B", Quantization: "nvfp4", SizeBytes: 6_123_456_789,
	}
	lfm = domain.Model{
		Name: "lfm2:8b", Digest: "sha256:bbbb1111bbbb1111", Format: "gguf",
		Family: "lfm2moe", Parameters: "8.3B", Quantization: "Q4_K_M", SizeBytes: 5_000_000_001,
	}
	return qwen, lfm
}

func testSession(id domain.SessionID, start time.Time, tag, promptName string, numPredict int) domain.Session {
	return domain.Session{
		ID:            id,
		StartedAt:     start,
		ToolVersion:   "v0.1.0",
		OllamaVersion: "0.34.0",
		Host:          domain.Host{Name: "studio", PowerSource: domain.PowerAC},
		Tag:           tag,
		Workload: domain.Workload{
			PromptName: promptName, PromptHash: "hash-" + promptName,
			NumPredict: numPredict, NumCtx: 8192, Temperature: 0.25, Seed: 42,
		},
		Order:      domain.OrderRoundRobin,
		Interval:   90 * time.Second,
		Runs:       3,
		CacheModes: []domain.CacheMode{domain.CacheMiss, domain.CacheContinue, domain.CachePrefix},
	}
}

// measurement returns a measurement whose every field depends on n.
func measurement(n int) domain.Measurement {
	d := time.Duration(n)
	return domain.Measurement{
		Server: domain.ServerMetrics{
			PromptTokens: 1000 + n, CachedTokens: 900 + n, PromptDuration: 50*time.Millisecond + d,
			EvalTokens: 512 + n, EvalDuration: 8*time.Second + d, LoadDuration: 3*time.Millisecond + d,
			TotalDuration: 9*time.Second + d,
		},
		Client:     domain.ClientMetrics{TimeToFirstToken: 60*time.Millisecond + d, Wall: 9*time.Second + 2*d},
		DoneReason: domain.DoneLength,
	}
}

func exchange(s domain.Session, m domain.Model, kind domain.ExchangeKind, mode domain.CacheMode,
	run int, at time.Time, meas domain.Measurement) domain.Exchange {
	return domain.Exchange{
		Session: s.ID, Step: domain.Step{Model: m.Name, Mode: mode, Run: run},
		Model: m, Kind: kind, StartedAt: at, Measurement: meas,
	}
}

// failed returns x as a failed request.
func failed(x domain.Exchange, err string) domain.Exchange {
	x.Measurement, x.Err = domain.Measurement{}, err
	return x
}

// wantRow returns the row that the run x of s gives. model is the stored
// model row.
func wantRow(s domain.Session, model domain.Model, x domain.Exchange) report.Row {
	model.Name = x.Model.Name
	return report.Row{
		Session: s.ID, StartedAt: x.StartedAt.UTC(), Tag: s.Tag, OllamaVersion: s.OllamaVersion,
		Workload: s.Workload, Model: model, Mode: x.Step.Mode, Measurement: x.Measurement, Err: x.Err,
	}
}

// record writes the sessions, the models, and the exchanges.
func record(t *testing.T, rec *Recorder, sessions []domain.Session, models []domain.Model, xs []domain.Exchange) {
	t.Helper()
	ctx := t.Context()
	for _, s := range sessions {
		if err := rec.StartSession(ctx, s); err != nil {
			t.Fatalf("StartSession: %v", err)
		}
	}
	for _, m := range models {
		if err := rec.SaveModel(ctx, m); err != nil {
			t.Fatalf("SaveModel: %v", err)
		}
	}
	for _, x := range xs {
		if err := rec.SaveExchange(ctx, x); err != nil {
			t.Fatalf("SaveExchange(%s %s): %v", x.Kind, x.Step, err)
		}
	}
}

// queryValue returns the single value of query.
func queryValue[T any](t *testing.T, db *sql.DB, query string, args ...any) T {
	t.Helper()
	var v T
	if err := db.QueryRowContext(t.Context(), query, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}
