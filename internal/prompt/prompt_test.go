package prompt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"token-rate/internal/domain"
)

func TestBuiltinHashes(t *testing.T) {
	t.Parallel()
	// A changed hash starts a new workload in the report. Change these values
	// only on purpose.
	for name, want := range map[string]string{Short: "231cfd192f02", Long: "7c5e856814a0"} {
		set, ok := Builtin(name)
		if !ok {
			t.Fatalf("Builtin(%q) not found", name)
		}
		if got := set.Hash(); got != want {
			t.Errorf("%s: Hash() = %q, want %q", name, got, want)
		}
		if set.Name != name {
			t.Errorf("%s: Name = %q", name, set.Name)
		}
	}
	if _, ok := Builtin("medium"); ok {
		t.Error("Builtin(medium) found a set")
	}
}

func TestHashIgnoresName(t *testing.T) {
	t.Parallel()
	a := Set{Name: "a", System: "s", Question: "q", FollowUp: "f"}
	b := a
	b.Name = "b"
	if a.Hash() != b.Hash() {
		t.Error("the name changed the hash")
	}
	// The separator keeps the fields apart.
	if (Set{System: "ab", Question: "c"}).Hash() == (Set{System: "a", Question: "bc"}).Hash() {
		t.Error("two different sets have the same hash")
	}
}

func TestSupports(t *testing.T) {
	t.Parallel()
	full := Set{System: "s", Question: "q", FollowUp: "f"}
	tests := []struct {
		name string
		set  Set
		mode domain.CacheMode
		want bool
	}{
		{"miss with question", Set{Question: "q"}, domain.CacheMiss, true},
		{"miss without question", Set{System: "s"}, domain.CacheMiss, false},
		{"prefix with system", Set{System: "s", Question: "q"}, domain.CachePrefix, true},
		{"prefix without system", Set{Question: "q"}, domain.CachePrefix, false},
		{"continue full", full, domain.CacheContinue, true},
		{"continue without follow-up", Set{System: "s", Question: "q"}, domain.CacheContinue, false},
		{"continue without system", Set{Question: "q", FollowUp: "f"}, domain.CacheContinue, false},
		{"unknown mode", full, domain.CacheMode("hit"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.set.Supports(tt.mode); got != tt.want {
				t.Errorf("Supports(%s) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestBuiltinSupport(t *testing.T) {
	t.Parallel()
	long, _ := Builtin(Long)
	if !long.SupportsAll(domain.CacheModes()) {
		t.Error("long does not support every mode")
	}
	short, _ := Builtin(Short)
	if !short.Supports(domain.CacheMiss) || short.Supports(domain.CachePrefix) {
		t.Error("short must support only miss")
	}
}

func TestDefaultFor(t *testing.T) {
	t.Parallel()
	if got := DefaultFor([]domain.CacheMode{domain.CacheMiss}); got != Short {
		t.Errorf("DefaultFor(miss) = %q, want short", got)
	}
	if got := DefaultFor([]domain.CacheMode{domain.CacheMiss, domain.CachePrefix}); got != Long {
		t.Errorf("DefaultFor(miss,prefix) = %q, want long", got)
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	if set, err := Resolve(Long); err != nil || set.Name != Long {
		t.Errorf("Resolve(long) = %v, %v", set.Name, err)
	}

	named := write("named.json", `{"name": "doc", "system": "s", "question": "q", "follow_up": "f"}`)
	if set, err := Resolve(named); err != nil || set != (Set{Name: "doc", System: "s", Question: "q", FollowUp: "f"}) {
		t.Errorf("Resolve(named) = %+v, %v", set, err)
	}

	unnamed := write("code-review.json", `{"question": "q"}`)
	if set, err := Resolve(unnamed); err != nil || set.Name != "code-review" {
		t.Errorf("Resolve(unnamed) = %+v, %v", set, err)
	}

	for name, content := range map[string]string{
		"bad.json":     `{"question": `,
		"unknown.json": `{"question": "q", "folow_up": "f"}`,
		"empty.json":   `{"system": "s", "question": "  "}`,
		"two.json":     `{"question": "q"} {"question": "r"}`,
		"tail.json":    `{"question": "q"} trailing`,
		"builtin.json": `{"name": "long", "question": "q"}`,
		"short.json":   `{"question": "q"}`,
	} {
		path := write(name, content)
		if _, err := Resolve(path); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("Resolve(%s) error = %v, want an error that names the file", name, err)
		}
	}

	_, err := Resolve(filepath.Join(dir, "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "short, long") {
		t.Errorf("Resolve(missing) error = %v, want the built-in names", err)
	}
	if _, err := LoadFile(filepath.Join(dir, "missing.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("LoadFile(missing) error = %v, want ErrNotExist", err)
	}
}

func TestRandomIDs(t *testing.T) {
	t.Parallel()
	var ids RandomIDs
	seen := map[string]bool{}
	for range 100 {
		id := ids.Next()
		if len(id) != idLen || strings.ToUpper(id) != id {
			t.Fatalf("Next() = %q, want %d upper-case characters", id, idLen)
		}
		if seen[id] {
			t.Fatalf("Next() repeated %q", id)
		}
		seen[id] = true
	}
}
