package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"slices"
)

// migrations holds the schema changes. The file names sort in the order of
// application.
//
//go:embed migrations/*.sql
var migrations embed.FS

// migrate applies the migrations in files that the database does not have
// yet. PRAGMA user_version holds the number of applied migrations.
func migrate(ctx context.Context, db *sql.DB, files fs.FS) error {
	names, err := fs.Glob(files, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	slices.Sort(names)
	var applied int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&applied); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if applied > len(names) {
		return fmt.Errorf("the schema version %d is newer than this tool, which knows %d", applied, len(names))
	}
	for i := applied; i < len(names); i++ {
		if err := applyMigration(ctx, db, files, names[i], i+1); err != nil {
			return fmt.Errorf("apply %s: %w", names[i], err)
		}
	}
	return nil
}

// applyMigration runs the file name and sets the schema version to version,
// both in one transaction. The transaction holds the write lock from its
// start and reads the version again, so that it skips a file that another
// process applied in the meantime.
func applyMigration(ctx context.Context, db *sql.DB, files fs.FS, name string, version int) error {
	script, err := fs.ReadFile(files, name)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() // a no-op after Commit
	var applied int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&applied); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if applied >= version {
		return nil
	}
	for _, statement := range []string{string(script), fmt.Sprintf("PRAGMA user_version = %d", version)} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := checkForeignKeys(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// checkForeignKeys verifies rebuilt relationships before committing the schema.
func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("check foreign keys: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowID sql.Null[int64]
		var foreignKey int
		if err := rows.Scan(&table, &rowID, &parent, &foreignKey); err != nil {
			return fmt.Errorf("check foreign keys: %w", err)
		}
		row := "unknown (WITHOUT ROWID)"
		if rowID.Valid {
			row = fmt.Sprint(rowID.V)
		}
		return fmt.Errorf("foreign key violation in %s row %s referencing %s (key %d); back up the database, inspect PRAGMA foreign_key_check, and repair orphan rows before retrying", table, row, parent, foreignKey)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("check foreign keys: %w", err)
	}
	return nil
}
