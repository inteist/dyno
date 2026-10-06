package sqlite

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func TestOpenNewFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "new folder", "odd?#%name", "bench.db")
	db := openPath(t, path)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file is not at %s: %v", path, err)
	}
	if got, want := queryValue[int](t, db, "PRAGMA user_version"), schemaVersion(t); got != want {
		t.Errorf("user_version = %d, want %d", got, want)
	}
	tables := queryValue[int](t, db,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('sessions', 'models', 'exchanges')")
	if tables != 3 {
		t.Errorf("%d of the 3 tables exist", tables)
	}
}

func TestOpenSetsPragmas(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	tests := []struct {
		pragma string
		want   string
	}{
		{"foreign_keys", "1"},
		{"journal_mode", "wal"},
		{"busy_timeout", "5000"},
		{"synchronous", "1"}, // NORMAL
	}
	for _, tt := range tests {
		if got := queryValue[string](t, db, "PRAGMA "+tt.pragma); got != tt.want {
			t.Errorf("PRAGMA %s = %q, want %q", tt.pragma, got, tt.want)
		}
	}
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", got)
	}
}

func TestOpenTwiceChangesNothing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bench.db")
	first, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	s := testSession("S1", day(5), "", "short", 64)
	if err := NewRecorder(first).StartSession(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	before := schema(t, first)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := openPath(t, path)
	if got, want := queryValue[int](t, second, "PRAGMA user_version"), schemaVersion(t); got != want {
		t.Errorf("user_version = %d, want %d", got, want)
	}
	if after := schema(t, second); !reflect.DeepEqual(after, before) {
		t.Errorf("schema changed:\nbefore %q\nafter  %q", before, after)
	}
	if n := queryValue[int](t, second, "SELECT COUNT(*) FROM sessions"); n != 1 {
		t.Errorf("%d sessions, want 1", n)
	}
}

// schema returns the definitions of every table and index.
func schema(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT sql FROM sqlite_master ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s.String)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpenFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	writeFile(t, file, "a plain file")
	garbage := filepath.Join(dir, "garbage.db")
	writeFile(t, garbage, "this is not a SQLite database, but it is long enough to have a header of one")
	newer := filepath.Join(dir, "newer.db")
	db := openPath(t, newer)
	if _, err := db.ExecContext(t.Context(), "PRAGMA user_version = 7"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	for name, path := range map[string]string{
		"parent is a file": filepath.Join(file, "bench.db"),
		"not a database":   garbage,
		"newer schema":     newer,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, err := Open(t.Context(), path)
			if err == nil {
				db.Close()
				t.Fatal("Open gave no error")
			}
			t.Log(err)
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateAppliesEachFileInOneTransaction(t *testing.T) {
	t.Parallel()
	db, err := sql.Open(driverName, dsn(filepath.Join(t.TempDir(), "bench.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	files := fstest.MapFS{
		"migrations/0001_a.sql": {Data: []byte("CREATE TABLE a (x INTEGER);")},
		"migrations/0002_b.sql": {Data: []byte("CREATE TABLE b (x INTEGER); INSERT INTO missing VALUES (1);")},
	}
	if err := migrate(t.Context(), db, files); err == nil {
		t.Fatal("migrate gave no error for a broken file")
	}
	if got := queryValue[int](t, db, "PRAGMA user_version"); got != 1 {
		t.Errorf("user_version after the failure = %d, want 1", got)
	}
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name = 'b'"); n != 0 {
		t.Error("the failed migration left the table b")
	}

	files["migrations/0002_b.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE b (x INTEGER);")}
	if err := migrate(t.Context(), db, files); err != nil {
		t.Fatalf("migrate after the fix: %v", err)
	}
	if got := queryValue[int](t, db, "PRAGMA user_version"); got != 2 {
		t.Errorf("user_version = %d, want 2", got)
	}
}

func TestMigrateRenamesTheServerVersion(t *testing.T) {
	t.Parallel()
	db, err := sql.Open(driverName, dsn(filepath.Join(t.TempDir(), "bench.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	first, err := fs.ReadFile(migrations, "migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(t.Context(), db, fstest.MapFS{"migrations/0001_init.sql": {Data: first}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO sessions (
		id, started_at, status, tool_version, ollama_version, host, power_source,
		prompt_name, prompt_hash, workload_hash, num_predict, num_ctx, temperature, seed,
		run_order, interval_ns, runs, cache_modes
	) VALUES ('S1', '2026-10-05T14:02:12Z', 'completed', 'v1', '0.34.0', 'studio', 'ac',
		'short', 'p', 'w', 512, 8192, 0, 42, 'model', 0, 1, 'miss')`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(t.Context(), db, migrations); err != nil {
		t.Fatal(err)
	}
	if got := queryValue[string](t, db, "SELECT server_version FROM sessions WHERE id = 'S1'"); got != "0.34.0" {
		t.Errorf("server_version = %q, want 0.34.0", got)
	}
}

// TestOpenRelativePath must not run in parallel, because it changes the
// working folder.
func TestOpenRelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, rel := range []string{"bench.db", "./dot.db", filepath.Join("sub", "bench.db")} {
		db, err := Open(t.Context(), rel)
		if err != nil {
			t.Fatalf("Open(%q) = %v", rel, err)
		}
		db.Close()
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("Open(%q) made no file in the working folder: %v", rel, err)
		}
	}
}

func TestDSNPath(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]string{
		"/Users/max/bench.db": "file:///Users/max/bench.db?",
		"C:/data/bench.db":    "file:///C:/data/bench.db?",
	} {
		if got := dsn(path); !strings.HasPrefix(got, want) {
			t.Errorf("dsn(%q) = %q, want the prefix %q", path, got, want)
		}
	}
}

// TestOpenNewFileConcurrently opens one new file from several connection
// pools at the same time, as two processes that start together.
func TestOpenNewFileConcurrently(t *testing.T) {
	t.Parallel()
	for round := range 10 {
		path := filepath.Join(t.TempDir(), "bench.db")
		errs := make([]error, 4)
		var wg sync.WaitGroup
		for i := range errs {
			wg.Go(func() {
				db, err := Open(t.Context(), path)
				if err == nil {
					err = db.Close()
				}
				errs[i] = err
			})
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
	}
}
