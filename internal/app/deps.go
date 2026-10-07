package app

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"

	"dyno/internal/cli"
	"dyno/internal/endpoint"
	"dyno/internal/llm"
	"dyno/internal/ollama"
	"dyno/internal/openaicompat"
	"dyno/internal/store/sqlite"
	"dyno/internal/ui/plain"
	"dyno/internal/ui/tui"
)

// IO holds the standard streams of the process.
type IO struct {
	In       io.Reader
	Out      io.Writer
	Err      io.Writer
	Terminal bool // stdin and stdout are a terminal
}

// StdIO returns the streams of the process.
func StdIO() IO {
	return IO{
		In:       os.Stdin,
		Out:      os.Stdout,
		Err:      os.Stderr,
		Terminal: term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())),
	}
}

// useTUI reports whether a command shows the TUI.
func (s IO) useTUI(plainFlag bool) bool { return s.Terminal && !plainFlag }

// newClient returns the server client of the common flags.
func newClient(c cli.Common) (llm.Client, error) {
	switch c.BackendName() {
	case "ollama":
		client, err := ollama.New(c.Host)
		if err != nil {
			return nil, err
		}
		return client, nil
	case "openai":
		return openaicompat.New(openaicompat.Config{
			BaseURL: c.BaseURL, APIKey: c.APIKey(), Usage: c.Usage,
			TokenLimitField: c.TokenLimitField, ServerKind: c.ServerKind,
			ServerVersion: c.ServerVersion, ModelRevision: c.ModelRevision,
			SkipModelDiscovery: c.SkipModelDiscovery,
			OmitSeed:           c.OmitSeed, OmitTemperature: c.OmitTemperature,
		})
	default:
		return nil, fmt.Errorf("unsupported backend %q", c.BackendName())
	}
}

// warnEndpoint never includes secrets or URLs in diagnostics.
func warnEndpoint(c cli.Common, stdio IO) {
	if c.BackendName() != "openai" || c.APIKey() == "" || stdio.Err == nil {
		return
	}
	u, err := endpoint.Parse(c.BaseURL)
	if err != nil || endpoint.IsLoopback(u) {
		return
	}
	fmt.Fprintf(stdio.Err, "notice: authentication from %s is sent to the configured non-loopback endpoint\n", c.APIKeyEnv)
	if u.Scheme == "http" {
		fmt.Fprintln(stdio.Err, "warning: bearer authentication over non-local plain HTTP; use HTTPS to protect credentials")
	}
}

// openStore opens the database of the common flags.
func openStore(ctx context.Context, c cli.Common) (*sql.DB, error) {
	db, err := sqlite.Open(ctx, c.DB)
	if err != nil {
		return nil, fmt.Errorf("open the database %s: %w", c.DB, err)
	}
	return db, nil
}

// openLog opens the log file of the common flags for appending. It returns
// a nil file when the flag is empty.
func openLog(c cli.Common) (*os.File, error) {
	if c.LogFile == "" {
		return nil, nil
	}
	f, err := os.OpenFile(c.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open the log file: %w", err)
	}
	return f, nil
}

// chooseSessionUI returns the TUI for a terminal and plain text otherwise.
func chooseSessionUI(plainFlag bool, stdio IO) SessionUI {
	if stdio.useTUI(plainFlag) {
		return tui.NewSessionUI(stdio.In, stdio.Out)
	}
	return plain.NewSessionUI(stdio.Out, time.Now)
}

// chooseReportUI returns the TUI for a terminal and plain text otherwise.
func chooseReportUI(plainFlag bool, stdio IO) ReportUI {
	if stdio.useTUI(plainFlag) {
		return tui.NewReportUI(stdio.In, stdio.Out)
	}
	return plain.NewReportUI(stdio.Out)
}
