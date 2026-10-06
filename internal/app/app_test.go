package app

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dyno/internal/bench"
	"dyno/internal/cli"
	"dyno/internal/domain"
	"dyno/internal/llm"
	"dyno/internal/llm/llmtest"
	"dyno/internal/prompt"
	"dyno/internal/report"
	"dyno/internal/store/sqlite"
)

var testModels = []domain.Model{
	{Name: "small", Digest: "aaaaaaaaaaaaaaaa", Format: "safetensors", Thinking: true},
	{Name: "large", Digest: "bbbbbbbbbbbbbbbb", Format: "gguf"},
}

func runCommand(t *testing.T, db string) cli.RunCommand {
	t.Helper()
	long, _ := prompt.Builtin(prompt.Long)
	return cli.RunCommand{
		Common: cli.Common{DB: db},
		Options: bench.Options{
			Models:         []string{"small", "large"},
			Runs:           2,
			Order:          domain.OrderModel,
			CacheModes:     domain.CacheModes(),
			Prompt:         long,
			Generation:     llm.GenerationOptions{MaxTokens: 16, ContextSize: 8192, Seed: 42},
			KeepAlive:      bench.DefaultKeepAlive(0),
			RequestTimeout: time.Minute,
		},
		Plain: true,
	}
}

// TestSessionWithRealStore runs two sessions against a fake server and a
// real SQLite file, then reads them back through the report command.
func TestSessionWithRealStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "data", "bench.db")
	cmd := runCommand(t, db)

	for i := range 2 {
		var out bytes.Buffer
		if err := runSession(ctx, cmd, IO{Out: &out}, llmtest.New(testModels...)); err != nil {
			t.Fatalf("session %d: %v\n%s", i+1, err, out.String())
		}
		text := out.String()
		for _, want := range []string{"12 steps", "this session", "all sessions with this workload", "completed"} {
			if !strings.Contains(text, want) {
				t.Errorf("session %d output has no %q:\n%s", i+1, want, text)
			}
		}
	}

	rows := storedRuns(t, db)
	if len(rows) != 24 {
		t.Fatalf("the store has %d runs, want 24", len(rows))
	}
	r := report.Build(rows, report.DefaultDimensions(), time.UTC)
	if len(r.Groups) != 6 || r.Sessions != 2 || r.Valid != 24 {
		t.Errorf("report: %d groups, %d sessions, %d valid runs; want 6, 2, 24", len(r.Groups), r.Sessions, r.Valid)
	}

	var out bytes.Buffer
	reportCmd := cli.ReportCommand{Common: cli.Common{DB: db}, Dimensions: report.DefaultDimensions(), Plain: true}
	if err := Report(ctx, reportCmd, IO{Out: &out}); err != nil {
		t.Fatal(err)
	}
	// Each of the 3 groups of the model is in the table and in the
	// statistics table of each metric.
	if !strings.Contains(out.String(), "24 runs") || strings.Count(out.String(), "small") != 3*(1+len(report.Metrics())) {
		t.Errorf("report output:\n%s", out.String())
	}
}

func storedRuns(t *testing.T, path string) []report.Row {
	t.Helper()
	db, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := sqlite.NewSource(db).Rows(context.Background(), report.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestSessionWithLogFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cmd := runCommand(t, filepath.Join(dir, "bench.db"))
	cmd.Options.Models, cmd.Options.Runs = []string{"small"}, 1
	cmd.LogFile = filepath.Join(dir, "session.log")
	if err := runSession(context.Background(), cmd, IO{Out: &bytes.Buffer{}}, llmtest.New(testModels...)); err != nil {
		t.Fatal(err)
	}
	log := readFile(t, cmd.LogFile)
	if !strings.Contains(log, "small  prefix") || !strings.Contains(log, "completed") {
		t.Errorf("log file:\n%s", log)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSessionErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	missing := runCommand(t, filepath.Join(dir, "missing.db"))
	missing.Options.Models = []string{"small", "absent"}
	err := runSession(context.Background(), missing, IO{Out: &bytes.Buffer{}}, llmtest.New(testModels...))
	if !errors.Is(err, llm.ErrModelNotFound) || ExitCode(err) != ExitFailure {
		t.Errorf("missing model: err = %v, exit code %d", err, ExitCode(err))
	}

	failing := runCommand(t, filepath.Join(dir, "failing.db"))
	client := llmtest.New(testModels...).WithChat(func(ctx context.Context, req llm.ChatRequest, p llm.ProgressFunc) (llm.ChatResult, error) {
		if req.Model.Name == "large" && req.Options.MaxTokens == 16 {
			return llm.ChatResult{}, errors.New("boom")
		}
		return llmtest.DefaultChat(ctx, req, p)
	})
	err = runSession(context.Background(), failing, IO{Out: &bytes.Buffer{}}, client)
	if err == nil || !strings.Contains(err.Error(), "6 of 12 steps failed") || ExitCode(err) != ExitFailure {
		t.Errorf("failed steps: err = %v, exit code %d", err, ExitCode(err))
	}

	bad := runCommand(t, filepath.Join(dir, "bad.db"))
	bad.Options.Runs = 0
	err = runSession(context.Background(), bad, IO{Out: &bytes.Buffer{}}, llmtest.New(testModels...))
	if !errors.Is(err, bench.ErrInvalidOptions) || ExitCode(err) != ExitUsage {
		t.Errorf("invalid options: err = %v, exit code %d", err, ExitCode(err))
	}
}

func TestCanceledSession(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	client := llmtest.New(testModels...).WithChat(func(c context.Context, req llm.ChatRequest, p llm.ProgressFunc) (llm.ChatResult, error) {
		if req.Options.MaxTokens == 16 {
			cancel() // the user stops the session during the first run
			<-c.Done()
			return llm.ChatResult{}, c.Err()
		}
		return llmtest.DefaultChat(c, req, p)
	})
	db := filepath.Join(t.TempDir(), "bench.db")
	err := runSession(ctx, runCommand(t, db), IO{Out: &bytes.Buffer{}}, client)
	if !errors.Is(err, context.Canceled) || ExitCode(err) != ExitStopped {
		t.Fatalf("err = %v, exit code %d", err, ExitCode(err))
	}
	if rows := storedRuns(t, db); len(rows) != 0 {
		t.Errorf("the store has %d runs after a stop in the first run, want 0", len(rows))
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := Execute(context.Background(), cli.VersionCommand{}, IO{Out: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "dyno ") {
		t.Errorf("version output = %q", out.String())
	}
}

func TestExitCodeAndFail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		code int
		text string
	}{
		{nil, ExitOK, ""},
		{flag.ErrHelp, ExitOK, ""},
		{fmt.Errorf("run: %w", cli.ErrUsage), ExitUsage, "dyno: run: wrong arguments\n"},
		{fmt.Errorf("%w: runs", bench.ErrInvalidOptions), ExitUsage, "dyno: invalid options: runs\n"},
		{fmt.Errorf("run 1: %w", context.Canceled), ExitStopped, "dyno: stopped\n"},
		{errors.New("boom"), ExitFailure, "dyno: boom\n"},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		if code := Fail(&out, tt.err); code != tt.code || out.String() != tt.text {
			t.Errorf("Fail(%v) = %d, %q; want %d, %q", tt.err, code, out.String(), tt.code, tt.text)
		}
	}
}

func TestChooseUI(t *testing.T) {
	t.Parallel()
	if !(IO{Terminal: true}).useTUI(false) {
		t.Error("a terminal without --plain must use the TUI")
	}
	if (IO{Terminal: true}).useTUI(true) || (IO{}).useTUI(false) {
		t.Error("--plain or a pipe must use plain text")
	}
}
