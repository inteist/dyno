package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/ollama"
	"dyno/internal/report"
)

// Use an actual v2 database and the real adapter policy, not copied recorder
// defaults: an upgrade must not split the nightly all-time history.
func TestMigratedHistoryMergesWithRealNativeInfo(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "native-history.db")
	oldDB := legacyDatabase(t, path, 2)
	if err := oldDB.Close(); err != nil {
		t.Fatal(err)
	}
	db := openPath(t, path)
	filter := report.Filter{Sessions: []domain.SessionID{"completed"}, Models: []string{"native:latest"}, CacheModes: []domain.CacheMode{domain.CacheMiss}}
	rows, err := NewSource(db).Rows(t.Context(), filter)
	if err != nil || len(rows) != 1 {
		t.Fatalf("legacy baseline: rows=%d err=%v", len(rows), err)
	}
	legacy := rows[0]
	client, err := ollama.New("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	session := domain.Session{ID: "new-native", StartedAt: legacy.StartedAt.Add(24 * time.Hour), ServerVersion: legacy.ServerVersion,
		Backend: info.Backend, ServerKind: info.ServerKind, Endpoint: info.Endpoint,
		MeasurementProtocol: info.MeasurementProtocol, ContextPolicy: info.ContextPolicy,
		ConfigJSON: info.ConfigJSON, VersionSource: info.VersionSource,
		Workload: legacy.Workload, Order: domain.OrderModel, Runs: 1, CacheModes: []domain.CacheMode{domain.CacheMiss}}
	rec := NewRecorder(db)
	if err := rec.StartSession(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	x := domain.Exchange{Session: session.ID, Step: domain.Step{Model: legacy.Model.Name, Mode: legacy.Mode, Run: 1}, Model: legacy.Model, Kind: domain.KindRun, StartedAt: session.StartedAt, Measurement: legacy.Measurement}
	if err := rec.SaveExchange(t.Context(), x); err != nil {
		t.Fatal(err)
	}
	filter.Sessions = append(filter.Sessions, session.ID)
	rows, err = NewSource(db).Rows(t.Context(), filter)
	if err != nil {
		t.Fatal(err)
	}
	built := report.Build(rows, report.DefaultDimensions(), time.UTC)
	if len(rows) != 2 || built.Valid != 2 || len(built.Groups) != 1 {
		t.Fatalf("native history split: rows=%d valid=%d groups=%d", len(rows), built.Valid, len(built.Groups))
	}
	if rows[0].ContextPolicy != "request" || rows[0].ConfigJSON != "" || rows[1].ContextPolicy != domain.NativeContextPolicy || rows[1].ConfigJSON != domain.NativeConfigJSON {
		t.Fatalf("test did not exercise legacy and actual native facts: %+v", rows)
	}
}
