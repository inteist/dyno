package golden

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeT records failures instead of stopping the test.
type fakeT struct {
	testing.TB
	failed bool
}

func (f *fakeT) Helper()               {}
func (f *fakeT) Errorf(string, ...any) { f.failed = true }
func (f *fakeT) Fatalf(string, ...any) { f.failed = true }
func (f *fakeT) Fatal(...any)          { f.failed = true }

// The tests change the working folder and the environment, so they do not
// run in parallel.
func TestAssert(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Setenv(UpdateVar, "1")
	Assert(t, "view", "frame\n")
	if data, err := os.ReadFile(filepath.Join("testdata", "view.golden")); err != nil || string(data) != "frame\n" {
		t.Fatalf("the update wrote %q, %v", data, err)
	}

	t.Setenv(UpdateVar, "")
	same := &fakeT{TB: t}
	Assert(same, "view", "frame\n")
	if same.failed {
		t.Error("Assert failed for equal output")
	}
	other := &fakeT{TB: t}
	Assert(other, "view", "changed\n")
	if !other.failed {
		t.Error("Assert passed for different output")
	}
	missing := &fakeT{TB: t}
	Assert(missing, "missing", "x")
	if !missing.failed {
		t.Error("Assert passed without a golden file")
	}
}
