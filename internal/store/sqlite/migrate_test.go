package sqlite

import (
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"dyno/internal/domain"
	"dyno/internal/report"
)

func TestMigratePopulatedLegacyDatabase(t *testing.T) {
	t.Parallel()
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprintf("from version %d", version), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "legacy.db")
			db := legacyDatabase(t, path, version)
			versionColumn := "server_version"
			if version == 1 {
				versionColumn = "ollama_version"
			}
			sessions := `SELECT id, started_at, finished_at, status, tool_version, %s,
				host, power_source, tag, prompt_name, prompt_hash, workload_hash, num_predict,
				num_ctx, temperature, seed, run_order, interval_ns, runs, cache_modes FROM sessions ORDER BY id`
			models := `SELECT digest, format, family, parameter_size, quantization, size_bytes, first_seen_at FROM models ORDER BY digest`
			exchanges := `SELECT id, session_id, model_name, %s, cache_mode, run_number, kind, started_at,
				prompt_tokens, cached_tokens, prompt_ns, eval_tokens, eval_ns, load_ns, total_ns,
				ttft_ns, wall_ns, done_reason, error FROM exchanges ORDER BY id`
			beforeSessions := snapshotRows(t, db, fmt.Sprintf(sessions, versionColumn))
			beforeModels := snapshotRows(t, db, models)
			beforeExchanges := snapshotRows(t, db, fmt.Sprintf(exchanges, "model_digest"))
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			// The real Open path migrates with foreign keys and WAL already enabled.
			db = openPath(t, path)
			if got := queryValue[int](t, db, "PRAGMA user_version"); got != 4 {
				t.Errorf("user_version = %d, want 4", got)
			}
			if got := snapshotRows(t, db, fmt.Sprintf(sessions, "server_version")); !reflect.DeepEqual(got, beforeSessions) {
				t.Errorf("legacy session facts changed:\n%v\nwant %v", got, beforeSessions)
			}
			if got := snapshotRows(t, db, models); !reflect.DeepEqual(got, beforeModels) {
				t.Errorf("legacy model metadata changed:\n%v\nwant %v", got, beforeModels)
			}
			if got := snapshotRows(t, db, fmt.Sprintf(exchanges, "model_key")); !reflect.DeepEqual(got, beforeExchanges) {
				t.Errorf("legacy relationships/measurements changed:\n%v\nwant %v", got, beforeExchanges)
			}
			if n := queryValue[int](t, db, `SELECT COUNT(*) FROM sessions WHERE backend = 'ollama'
				AND server_kind = 'ollama' AND measurement_protocol = 'ollama-server-v1'
				AND context_policy = 'request' AND version_source = 'server'
				AND endpoint IS NULL AND config_json IS NULL`); n != 4 {
				t.Errorf("%d sessions with truthful native provenance, want 4", n)
			}
			if n := queryValue[int](t, db, `SELECT COUNT(*) FROM models
				WHERE model_key = digest AND identity_source = 'digest' AND revision IS NULL`); n != 2 {
				t.Errorf("%d preserved legacy model identities, want 2", n)
			}
			checkMigratedReferencesAndIndexes(t, db)
			rows, err := NewSource(db).Rows(t.Context(), report.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 5 {
				t.Fatalf("%d report rows, want 5 (no warm-ups/primers)", len(rows))
			}
			for _, r := range rows {
				if r.Model.Identity != r.Model.Digest || r.Model.IdentitySource != "digest" {
					t.Errorf("legacy identity changed in report: %+v", r.Model)
				}
				if r.Model.Name == "alias:latest" && r.Measurement.Server.Has(domain.MetricCachedTokens) {
					t.Error("legacy NULL cache count became a known zero")
				}
				if r.Model.Name == "native:latest" && r.Err == "" && !r.Measurement.Server.Has(domain.MetricCachedTokens) {
					t.Error("legacy cached zero lost availability")
				}
				if r.Err != "" && *r.Measurement.Server.Available != (domain.MetricAvailability{}) {
					t.Error("failed legacy exchange gained server measurements")
				}
			}
			// Appending uses the same legacy model identity; neither old exchanges
			// nor independently persisted session completion are rewritten.
			rec := NewRecorder(db)
			m := domain.Model{Name: "native:latest", Digest: "sha256:legacy-a"}
			s := testSession("running", day(5), "", "short", 64)
			x := exchange(s, m, domain.KindRun, domain.CacheMiss, 2, day(5), measurement(1))
			if err := rec.SaveExchange(t.Context(), x); err != nil {
				t.Fatal(err)
			}
			if id := queryValue[int](t, db, "SELECT MAX(id) FROM exchanges"); id != 62 {
				t.Errorf("new exchange id = %d, want 62", id)
			}
			if err := rec.FinishSession(t.Context(), s.ID, domain.StatusAborted, day(6)); err != nil {
				t.Fatal(err)
			}
			if n := queryValue[int](t, db, "SELECT COUNT(*) FROM exchanges"); n != 8 {
				t.Errorf("%d exchanges after finalization, want 8", n)
			}
		})
	}
}

// legacyDatabase populates the old schema, including every exchange kind and
// completed, failed, aborted, and still-running sessions.
func legacyDatabase(t *testing.T, path string, version int) *sql.DB {
	t.Helper()
	db, err := sql.Open(driverName, dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	files := fstest.MapFS{}
	for _, name := range []string{"0001_init.sql", "0002_server_version.sql"}[:version] {
		data, err := fs.ReadFile(migrations, "migrations/"+name)
		if err != nil {
			t.Fatal(err)
		}
		files["migrations/"+name] = &fstest.MapFile{Data: data}
	}
	if err := migrate(t.Context(), db, files); err != nil {
		t.Fatal(err)
	}
	// Both old versions have the same column order, so their insert is identical.
	const fixture = `INSERT INTO sessions VALUES
		('completed', '2026-10-01T14:02:12.123456789Z', '2026-10-01T15:02:12.123456789Z', 'completed', 'v1', '0.34.0', 'studio', 'ac', 'old', 'short', 'p', 'w', 64, 8192, 0, 42, 'model', 0, 2, 'miss,continue,prefix'),
		('running', '2026-10-02T14:02:12.123456789Z', NULL, 'running', 'v1', '0.34.0', 'studio', 'ac', '', 'short', 'p', 'w', 64, 8192, 0, 42, 'model', 0, 2, 'miss'),
		('aborted', '2026-10-03T14:02:12.123456789Z', '2026-10-03T15:02:12.123456789Z', 'aborted', 'v1', '0.34.0', 'studio', 'battery', '', 'short', 'p', 'w', 64, 8192, 0, 42, 'round-robin', 500, 2, 'miss'),
		('failed', '2026-10-04T14:02:12.123456789Z', '2026-10-04T15:02:12.123456789Z', 'failed', 'v1', '0.34.0', 'studio', 'unknown', '', 'short', 'p', 'w', 64, 8192, 0, 42, 'model', 0, 2, 'miss');
		INSERT INTO models VALUES
		('sha256:legacy-a', 'gguf', 'native', '7B', 'Q4', 7000000000, '2026-10-01T14:00:00.000000000Z'),
		('sha256:legacy-b', 'safetensors', 'other', '9B', 'fp16', 9000000000, '2026-10-02T14:00:00.000000000Z');
		INSERT INTO exchanges VALUES
		(7, 'completed', 'native:latest', 'sha256:legacy-a', 'miss', 1, 'warmup', '2026-10-01T14:03:00.000000001Z', 8, 0, 123, 4, 234, 345, 456, 567, 678, 'length', NULL),
		(11, 'completed', 'native:latest', 'sha256:legacy-a', 'continue', 1, 'primer', '2026-10-01T14:04:00.000000002Z', 128, 64, 1000, 8, 2000, 0, 3000, 1500, 3500, 'stop', NULL),
		(21, 'completed', 'native:latest', 'sha256:legacy-a', 'miss', 1, 'run', '2026-10-01T14:05:00.000000003Z', 1024, 0, 1000000, 64, 2000000, 0, 3000000, 1500000, 3500000, 'length', NULL),
		(31, 'completed', 'alias:latest', 'sha256:legacy-a', 'prefix', 1, 'run', '2026-10-01T14:06:00.000000004Z', 1024, NULL, 1000001, 64, 2000001, 0, 3000001, NULL, 3500001, 'stop', NULL),
		(41, 'failed', 'other:latest', 'sha256:legacy-b', 'miss', 1, 'run', '2026-10-04T14:05:00.000000007Z', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 'timeout'),
		(51, 'running', 'native:latest', 'sha256:legacy-a', 'miss', 1, 'run', '2026-10-02T14:05:00.000000005Z', 1024, 0, 1000002, 64, 2000002, 0, 3000002, 1500002, 3500002, 'length', NULL),
		(61, 'aborted', 'other:latest', 'sha256:legacy-b', 'miss', 1, 'run', '2026-10-03T14:05:00.000000006Z', 1024, 0, NULL, 64, 2000003, 0, 3000003, 1500003, 3500003, 'length', NULL);`
	if _, err := db.ExecContext(t.Context(), fixture); err != nil {
		t.Fatal(err)
	}
	return db
}

// legacyEmptyKeyDatabase includes a valid historical empty digest and its run.
func legacyEmptyKeyDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db := legacyDatabase(t, path, 2)
	const fixture = `INSERT INTO models
		SELECT '', format, family, parameter_size, quantization, size_bytes, first_seen_at
		FROM models WHERE digest = 'sha256:legacy-a';
		INSERT INTO exchanges
		SELECT 71, session_id, 'empty:latest', '', cache_mode, run_number, kind, started_at,
		prompt_tokens, cached_tokens, prompt_ns, eval_tokens, eval_ns, load_ns, total_ns,
		ttft_ns, wall_ns, done_reason, error FROM exchanges WHERE id = 21;`
	if _, err := db.ExecContext(t.Context(), fixture); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigratePreservesLegacyEmptyKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := legacyEmptyKeyDatabase(t, path)
	before := snapshotRows(t, db, "SELECT * FROM exchanges WHERE id = 71")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openPath(t, path)
	if got := snapshotRows(t, db, "SELECT * FROM exchanges WHERE id = 71"); !reflect.DeepEqual(got, before) {
		t.Errorf("legacy empty-key exchange changed: %v, want %v", got, before)
	}
	if digest := queryValue[sql.Null[string]](t, db, "SELECT digest FROM models WHERE model_key = ''"); digest.Valid {
		t.Errorf("legacy empty checksum = %+v, want NULL without changing its key", digest)
	}
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM pragma_foreign_key_check"); n != 0 {
		t.Errorf("%d foreign key violations", n)
	}
	rows, err := NewSource(db).Rows(t.Context(), report.Filter{Models: []string{"empty:latest"}})
	if err != nil || len(rows) != 1 {
		t.Fatalf("legacy empty-key history: %d rows, %v", len(rows), err)
	}
	if rows[0].Model.Identity != "" || rows[0].Model.Digest != "" || rows[0].Measurement.Server.EvalTokens != 64 {
		t.Errorf("legacy empty-key report facts changed: %+v", rows[0])
	}
}

func checkMigratedReferencesAndIndexes(t *testing.T, db *sql.DB) {
	t.Helper()
	if got := queryValue[int](t, db, "PRAGMA foreign_keys"); got != 1 {
		t.Errorf("foreign_keys = %d, want 1", got)
	}
	if got := queryValue[string](t, db, "PRAGMA journal_mode"); got != "wal" {
		t.Errorf("journal_mode = %q, want wal", got)
	}
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM pragma_foreign_key_check"); n != 0 {
		t.Errorf("%d foreign key violations", n)
	}
	for name, columns := range map[string][]string{
		"exchanges_report":  {"kind", "model_name", "cache_mode"},
		"exchanges_session": {"session_id"},
		"sessions_started":  {"started_at"},
	} {
		got := snapshotRows(t, db, "SELECT name FROM pragma_index_info(?) ORDER BY seqno", name)
		var want [][]any
		for _, column := range columns {
			want = append(want, []any{column})
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("index %s columns = %v, want %v", name, got, want)
		}
	}
	got := snapshotRows(t, db, `SELECT "table", "from", "to" FROM pragma_foreign_key_list('exchanges') ORDER BY "table"`)
	want := [][]any{{"models", "model_key", "model_key"}, {"sessions", "session_id", "id"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("foreign key relationships = %v, want %v", got, want)
	}
	for _, statement := range []string{
		"DELETE FROM models WHERE model_key = 'sha256:legacy-a'",
		"DELETE FROM sessions WHERE id = 'completed'",
		"UPDATE exchanges SET model_key = 'missing' WHERE id = 21",
		"UPDATE exchanges SET session_id = 'missing' WHERE id = 21",
		"UPDATE exchanges SET cache_mode = 'hit' WHERE id = 21",
		"UPDATE exchanges SET kind = 'unknown' WHERE id = 21",
	} {
		if _, err := db.ExecContext(t.Context(), statement); err == nil {
			t.Errorf("migration lost a constraint: %s", statement)
		}
	}
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name IN ('models_new', 'exchanges_new')"); n != 0 {
		t.Errorf("%d replacement tables left behind", n)
	}
}

// snapshotRows preserves NULL and every raw SQLite value for migration comparisons.
func snapshotRows(t *testing.T, db *sql.DB, query string, args ...any) [][]any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		values, destinations := make([]any, len(columns)), make([]any, len(columns))
		for i := range destinations {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatal(err)
		}
		out = append(out, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMigrationChecksForeignKeysBeforeCommit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, suffix, row string
	}{
		{"rowid", "", "7"},
		{"without rowid", " WITHOUT ROWID", "unknown (WITHOUT ROWID)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, err := sql.Open(driverName, dsn(filepath.Join(t.TempDir(), "bench.db")))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			files := fstest.MapFS{
				"migrations/0001_parent.sql": {Data: []byte("CREATE TABLE parent (id INTEGER PRIMARY KEY);")},
				"migrations/0002_child.sql": {Data: []byte(`CREATE TABLE child (
					id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED
				)` + tc.suffix + `; INSERT INTO child VALUES (7, 404);`)},
			}
			err = migrate(t.Context(), db, files)
			want := "foreign key violation in child row " + tc.row + " referencing parent (key 0)"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("migrate = %v, want %q before commit", err, want)
			}
			if version := queryValue[int](t, db, "PRAGMA user_version"); version != 1 {
				t.Errorf("version = %d, want 1 after rollback", version)
			}
			if n := queryValue[int](t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name = 'child'"); n != 0 {
				t.Error("failed foreign key check left its replacement table")
			}
		})
	}
}

func TestMigrationCommitsStepsAndNextOpenResumes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := legacyDatabase(t, path, 2)
	beforeExchanges := snapshotRows(t, db, "SELECT * FROM exchanges ORDER BY id")
	const modelFacts = "SELECT digest, format, family, parameter_size, quantization, size_bytes, first_seen_at FROM models ORDER BY digest"
	beforeModels := snapshotRows(t, db, modelFacts)
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, err := fs.ReadFile(migrations, name)
		if err != nil {
			t.Fatal(err)
		}
		if name == "migrations/0004_model_identity.sql" {
			data = append(data, []byte("\nINSERT INTO missing VALUES (1);")...)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	if err := migrate(t.Context(), db, files); err == nil {
		t.Fatal("broken final migration succeeded")
	}
	// The metadata step intentionally commits independently of the rebuild.
	if version := queryValue[int](t, db, "PRAGMA user_version"); version != 3 {
		t.Fatalf("version = %d, want the committed metadata step 3", version)
	}
	beforeSessions := snapshotRows(t, db, "SELECT * FROM sessions ORDER BY id")
	if got := snapshotRows(t, db, "SELECT * FROM exchanges ORDER BY id"); !reflect.DeepEqual(got, beforeExchanges) {
		t.Error("failed rebuild changed exchange facts")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// A new connection through the real Open path resumes at version 3.
	db = openPath(t, path)
	if version := queryValue[int](t, db, "PRAGMA user_version"); version != 4 {
		t.Errorf("version = %d, want 4 after reopening", version)
	}
	for _, tc := range []struct {
		query string
		want  [][]any
	}{
		{"SELECT * FROM sessions ORDER BY id", beforeSessions},
		{"SELECT * FROM exchanges ORDER BY id", beforeExchanges},
		{modelFacts, beforeModels},
	} {
		if got := snapshotRows(t, db, tc.query); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("reopening changed persisted facts for %s: %v, want %v", tc.query, got, tc.want)
		}
	}
	checkMigratedReferencesAndIndexes(t, db)
}

func TestLegacyOrphansBlockUpgradeWithoutDataLoss(t *testing.T) {
	t.Parallel()
	for _, version := range []int{2, 3} {
		for _, column := range []string{"model_digest", "session_id"} {
			t.Run(fmt.Sprintf("version %d orphan %s", version, column), func(t *testing.T) {
				t.Parallel()
				db := legacyDatabase(t, filepath.Join(t.TempDir(), "legacy.db"), 2)
				if version == 3 {
					if err := applyMigration(t.Context(), db, migrations, "migrations/0003_backend_metadata.sql", 3); err != nil {
						t.Fatal(err)
					}
				}
				// Simulate an old writer that did not enforce foreign keys.
				for _, statement := range []string{
					"PRAGMA foreign_keys = OFF",
					"UPDATE exchanges SET " + column + " = 'missing' WHERE id = 21",
					"PRAGMA foreign_keys = ON",
				} {
					if _, err := db.ExecContext(t.Context(), statement); err != nil {
						t.Fatal(err)
					}
				}
				beforeSchema := schema(t, db)
				queries := []string{"SELECT * FROM sessions ORDER BY id", "SELECT * FROM models ORDER BY digest", "SELECT * FROM exchanges ORDER BY id"}
				before := make([][][]any, len(queries))
				for i, query := range queries {
					before[i] = snapshotRows(t, db, query)
				}
				if err := migrate(t.Context(), db, migrations); err == nil || !strings.Contains(err.Error(), "foreign key") && !strings.Contains(err.Error(), "FOREIGN KEY") {
					t.Fatalf("orphaned history migration = %v, want a foreign key error", err)
				}
				if got := queryValue[int](t, db, "PRAGMA user_version"); got != version {
					t.Errorf("version = %d, want %d after rollback", got, version)
				}
				if got := schema(t, db); !reflect.DeepEqual(got, beforeSchema) {
					t.Error("orphaned history changed schema instead of rolling back")
				}
				for i, query := range queries {
					if got := snapshotRows(t, db, query); !reflect.DeepEqual(got, before[i]) {
						t.Errorf("upgrade deleted or repaired historical data for %s", query)
					}
				}
				if n := queryValue[int](t, db, "SELECT COUNT(*) FROM pragma_foreign_key_check"); n != 1 {
					t.Errorf("%d remaining violations, want the original orphan only", n)
				}
				if fk := queryValue[int](t, db, "PRAGMA foreign_keys"); fk != 1 {
					t.Error("rollback disabled foreign key enforcement")
				}
			})
		}
	}
}

func TestIdentityMigrationRollsBackRebuild(t *testing.T) {
	t.Parallel()
	db := legacyDatabase(t, filepath.Join(t.TempDir(), "legacy.db"), 2)
	if err := applyMigration(t.Context(), db, migrations, "migrations/0003_backend_metadata.sql", 3); err != nil {
		t.Fatal(err)
	}
	before := schema(t, db)
	data, err := fs.ReadFile(migrations, "migrations/0004_model_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"migrations/0004_model_identity.sql": {Data: append(data, []byte("\nINSERT INTO missing VALUES (1);")...)}}
	if err := applyMigration(t.Context(), db, files, "migrations/0004_model_identity.sql", 4); err == nil {
		t.Fatal("broken rebuild succeeded")
	}
	if version := queryValue[int](t, db, "PRAGMA user_version"); version != 3 {
		t.Errorf("version = %d, want 3 after rollback", version)
	}
	if got := schema(t, db); !reflect.DeepEqual(got, before) {
		t.Error("failed rebuild changed tables or indexes")
	}
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM exchanges WHERE model_digest = 'sha256:legacy-a'"); n != 5 {
		t.Errorf("%d legacy exchanges after rollback, want 5", n)
	}
	if err := migrate(t.Context(), db, migrations); err != nil {
		t.Fatalf("retry the intact migration: %v", err)
	}
	checkMigratedReferencesAndIndexes(t, db)
}
