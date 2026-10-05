// Package golden compares test output with golden files in testdata. Only
// tests import it. To rewrite the files, set UPDATE_GOLDEN=1:
//
//	UPDATE_GOLDEN=1 go test ./...
package golden

import (
	"os"
	"path/filepath"
	"testing"
)

// UpdateVar is the environment variable that rewrites the golden files.
const UpdateVar = "UPDATE_GOLDEN"

// Assert compares got with the file testdata/<name>.golden. With
// UPDATE_GOLDEN=1, it writes got to the file instead.
func Assert(t testing.TB, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if os.Getenv(UpdateVar) == "1" {
		write(t, path, got)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the golden file (set %s=1 to write it): %v", UpdateVar, err)
	}
	if got != string(want) {
		t.Errorf("the output differs from %s:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

// write writes got to path and makes the folder if needed.
func write(t testing.TB, path, got string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
		t.Fatal(err)
	}
}
