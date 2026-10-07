// Package sqlite stores sessions, models, and exchanges in one SQLite file.
// Recorder writes them for the runner, and Source reads the runs for the
// report. Both share the *sql.DB that Open returns. It is the only package
// that imports the SQLite driver.
//
// Open applies forward-only migrations, each in its own transaction, with foreign
// keys enabled.
// Back up the database (including committed WAL data) before upgrading; older
// binaries cannot open the upgraded schema. Legacy model keys remain their full
// content digests. Unavailable raw measurements and optional model metadata are SQL NULL;
// unknown server_version and num_ctx use an empty string and 0 respectively.
// Optional endpoint/config_json and other nullable provenance use SQL NULL;
// codec defaults preserve historical required strings. The availability mask preserves
// known-zero measurements;
// historical cached-token zeros cannot be distinguished from formerly absent
// values and are retained unchanged.
package sqlite
