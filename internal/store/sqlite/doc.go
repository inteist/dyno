// Package sqlite stores sessions, models, and exchanges in one SQLite file.
// Recorder writes them for the runner, and Source reads the runs for the
// report. Both share the *sql.DB that Open returns. It is the only package
// that imports the SQLite driver.
package sqlite
