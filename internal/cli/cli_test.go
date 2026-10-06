package cli

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // Europe/Berlin on every system

	"dyno/internal/bench"
	"dyno/internal/golden"
)

const (
	testHome = "/home/max"
	homeDB   = testHome + "/.local/share/dyno/bench.db"
)

// berlin returns a zone with a daylight saving time change on 2026-10-25.
func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// testEnv returns an Env with vars as the environment.
func testEnv(t *testing.T, vars map[string]string) Env {
	t.Helper()
	return Env{
		Getenv:   func(key string) string { return vars[key] },
		HomeDir:  testHome,
		Location: berlin(t),
	}
}

// parse runs Parse with env and returns what it printed on stderr.
func parse(env Env, args ...string) (Command, string, error) {
	var stderr bytes.Buffer
	cmd, err := Parse(args, env, &stderr)
	return cmd, stderr.String(), err
}

func TestParseHelp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want string
	}{
		{nil, "Usage: dyno <command>"},
		{[]string{"help"}, "Usage: dyno <command>"},
		{[]string{"-h"}, "Usage: dyno <command>"},
		{[]string{"--help"}, "Usage: dyno <command>"},
		{[]string{"run", "-h"}, "Usage: dyno run --models LIST"},
		{[]string{"report", "--help"}, "Usage: dyno report [options]"},
		{[]string{"models", "-help"}, "Usage: dyno models [--host URL]"},
		{[]string{"version", "-h"}, "Usage: dyno version"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			t.Parallel()
			cmd, stderr, err := parse(testEnv(t, nil), tt.args...)
			if !errors.Is(err, flag.ErrHelp) || errors.Is(err, ErrUsage) || cmd != nil {
				t.Fatalf("Parse() = %v, %v, want only flag.ErrHelp", cmd, err)
			}
			if !strings.Contains(stderr, tt.want) || strings.Count(stderr, "Usage:") != 1 {
				t.Errorf("stderr = %q, want %q once", stderr, tt.want)
			}
		})
	}
}

func TestParseUnknownCommand(t *testing.T) {
	t.Parallel()
	cmd, stderr, err := parse(testEnv(t, nil), "bench", "--runs", "3")
	if !errors.Is(err, ErrUsage) || cmd != nil {
		t.Fatalf("Parse() = %v, %v, want ErrUsage", cmd, err)
	}
	if err.Error() != `unknown command "bench"` {
		t.Errorf("error = %q", err)
	}
	if !strings.Contains(stderr, "Commands:") || strings.Contains(stderr, "bench") {
		t.Errorf("stderr = %q, want the overview without the error", stderr)
	}
}

// TestUsageErrorPrintedOnce checks that the user sees the error only from
// the caller and the usage text only from Parse.
func TestUsageErrorPrintedOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args    []string
		message string
	}{
		{[]string{"run", "--models", "a", "--bogus"}, "run: flag provided but not defined: -bogus"},
		{[]string{"run", "--models", "a", "--runs", "x"}, `run: invalid value "x" for flag -runs`},
		{[]string{"report", "extra"}, `report: unexpected argument "extra"`},
		{[]string{"report", "--group", "day,week"}, "report: --group: use day or week, not both"},
		{[]string{"version", "now"}, `version: unexpected argument "now"`},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			t.Parallel()
			_, stderr, err := parse(testEnv(t, nil), tt.args...)
			if !errors.Is(err, ErrUsage) || !strings.HasPrefix(err.Error(), tt.message) {
				t.Fatalf("error = %v, want ErrUsage with %q", err, tt.message)
			}
			if strings.Count(stderr, "Usage: dyno "+tt.args[0]) != 1 {
				t.Errorf("stderr = %q, want the usage text once", stderr)
			}
			if strings.Contains(stderr, tt.message) {
				t.Errorf("stderr = %q, want no error message", stderr)
			}
		})
	}
}

func TestValidationErrorPrintsNoUsage(t *testing.T) {
	t.Parallel()
	_, stderr, err := parse(testEnv(t, nil), "run")
	if !errors.Is(err, bench.ErrInvalidOptions) || errors.Is(err, ErrUsage) {
		t.Fatalf("error = %v, want only bench.ErrInvalidOptions", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
}

func TestDBPath(t *testing.T) {
	t.Parallel()
	all := map[string]string{"DYNO_DB": "/env/bench.db", "XDG_DATA_HOME": "/xdg"}
	tests := []struct {
		name string
		vars map[string]string
		args []string
		want string
	}{
		{"flag first", all, []string{"--db", "/flag.db"}, "/flag.db"},
		{"then DYNO_DB", all, nil, "/env/bench.db"},
		{"then XDG_DATA_HOME", map[string]string{"XDG_DATA_HOME": "/xdg"}, nil, "/xdg/dyno/bench.db"},
		{"then the home folder", nil, nil, homeDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range [][]string{{"run", "--models", "a"}, {"report"}} {
				cmd, _, err := parse(testEnv(t, tt.vars), slices.Concat(command, tt.args)...)
				if err != nil {
					t.Fatalf("%s: Parse() error = %v", command[0], err)
				}
				if got := commonOf(t, cmd).DB; got != tt.want {
					t.Errorf("%s: DB = %q, want %q", command[0], got, tt.want)
				}
			}
		})
	}
}

// commonOf returns the Common of a run or report command.
func commonOf(t *testing.T, cmd Command) Common {
	t.Helper()
	switch c := cmd.(type) {
	case RunCommand:
		return c.Common
	case ReportCommand:
		return c.Common
	}
	t.Fatalf("command %T has no database", cmd)
	return Common{}
}

func TestZeroEnv(t *testing.T) {
	t.Parallel()
	cmd, _, err := parse(Env{}, "report", "--since", "2026-10-01", "--db", "/data/bench.db")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := cmd.(ReportCommand)
	if want := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC); !got.Filter.Since.Equal(want) {
		t.Errorf("Since = %s, want %s", got.Filter.Since, want)
	}
}

// TestUnknownHome checks that only the commands with a database need the
// home folder.
func TestUnknownHome(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"run", "--models", "a"}, {"report"}} {
		_, stderr, err := parse(Env{}, args...)
		if !errors.Is(err, ErrUsage) || !strings.Contains(err.Error(), "set --db or DYNO_DB") {
			t.Errorf("%s without a home folder: error = %v", args[0], err)
		}
		if !strings.Contains(stderr, "Usage: dyno "+args[0]) {
			t.Errorf("%s without a home folder printed no usage:\n%s", args[0], stderr)
		}
	}
	for _, args := range [][]string{{"version"}, {"models"}} {
		if _, _, err := parse(Env{}, args...); err != nil {
			t.Errorf("%s without a home folder: error = %v", args[0], err)
		}
	}
}

func TestModelNamesGetTheDefaultTag(t *testing.T) {
	t.Parallel()
	cmd, _, err := parse(testEnv(t, nil), "run", "--models", "lfm2.5,qwen3.5:4b-mlx")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cmd.(RunCommand).Options.Models, []string{"lfm2.5:latest", "qwen3.5:4b-mlx"}; !slices.Equal(got, want) {
		t.Errorf("run --models = %v, want %v", got, want)
	}
	cmd, _, err = parse(testEnv(t, nil), "report", "--model", "lfm2.5")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cmd.(ReportCommand).Filter.Models, []string{"lfm2.5:latest"}; !slices.Equal(got, want) {
		t.Errorf("report --model = %v, want %v", got, want)
	}
	if _, _, err := parse(testEnv(t, nil), "run", "--models", "lfm2.5,lfm2.5:latest"); !errors.Is(err, bench.ErrInvalidOptions) {
		t.Errorf("two spellings of one model: error = %v, want a duplicate", err)
	}
}

func TestOSEnv(t *testing.T) {
	env := OSEnv()
	home, _ := os.UserHomeDir()
	if env.HomeDir != home || env.Location != time.Local || env.Getenv("HOME") != os.Getenv("HOME") {
		t.Errorf("OSEnv() = %+v, want the process values", env)
	}

	t.Setenv("HOME", "")
	if env := OSEnv(); env.HomeDir != "" {
		t.Errorf("OSEnv() without HOME: HomeDir = %q, want empty", env.HomeDir)
	}
}

func TestUsageGolden(t *testing.T) {
	t.Parallel()
	tests := map[string][]string{
		"overview": {"help"},
		"run":      {"run", "-h"},
		"report":   {"report", "-h"},
		"models":   {"models", "-h"},
		"version":  {"version", "-h"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, stderr, _ := parse(testEnv(t, nil), args...)
			golden.Assert(t, name, stderr)
		})
	}
}
