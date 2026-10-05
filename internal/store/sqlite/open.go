package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	driver "modernc.org/sqlite" // registers the driver "sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// driverName is the name that modernc.org/sqlite registers.
const driverName = "sqlite"

// Open opens the database file at path, makes its folder if needed, and
// brings the schema up to date. A relative path starts at the working
// folder. The caller closes the returned DB.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve the database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("make the folder of %s: %w", path, err)
	}
	db, err := sql.Open(driverName, dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // one writer in the process
	if err := prepare(ctx, db); err != nil {
		return nil, errors.Join(fmt.Errorf("open %s: %w", path, err), db.Close())
	}
	return db, nil
}

// The retries of a new file that another process creates at the same time.
// Two connections that switch one new file to WAL can deadlock, and SQLite
// then returns SQLITE_BUSY at once instead of after the busy timeout. The
// limit is on the total time, because a try can also wait for the busy
// timeout when another process holds the lock.
const (
	busyRetryLimit = 5 * time.Second
	busyRetryWait  = 100 * time.Millisecond
)

// prepare checks the connection and applies the migrations. While another
// connection holds the lock, it tries again until busyRetryLimit.
func prepare(ctx context.Context, db *sql.DB) error {
	deadline := time.Now().Add(busyRetryLimit)
	for {
		err := prepareOnce(ctx, db)
		if err == nil || !isBusy(err) || !time.Now().Before(deadline) {
			return err
		}
		select {
		case <-time.After(busyRetryWait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// prepareOnce checks the connection and applies the migrations.
func prepareOnce(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	return migrate(ctx, db, migrations)
}

// isBusy reports whether err tells that another connection holds the lock.
func isBusy(err error) bool {
	var e *driver.Error
	return errors.As(err, &e) && e.Code()&0xff == sqlite3.SQLITE_BUSY
}

// dsn returns a SQLite URI for the absolute path with the pragmas of every
// connection. The URI form escapes characters such as '?' in the path. The
// driver applies busy_timeout first and the other pragmas in name order, so
// the order of the list has no effect. Transactions that are not read-only
// take the write lock at their start, so that two processes that migrate
// one new file do not both apply a migration.
func dsn(path string) string {
	q := url.Values{
		"_pragma": {
			"busy_timeout(5000)",
			"foreign_keys(1)",
			"journal_mode(WAL)",
			"synchronous(NORMAL)",
		},
		"_txlock": {"immediate"},
	}
	u := url.URL{Scheme: "file", Path: uriPath(path), RawQuery: q.Encode()}
	return u.String()
}

// uriPath returns path as the path of a file URI, which starts with "/".
// Without the slash, a Windows path such as C:/x would become the host.
func uriPath(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}
