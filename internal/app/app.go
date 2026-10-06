package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"dyno/internal/bench"
	"dyno/internal/cli"
	"dyno/internal/event"
	"dyno/internal/report"
)

// SessionUI presents one session. plain.SessionUI and tui.SessionUI
// implement it.
type SessionUI interface {
	// Sink receives the events of the runner.
	Sink() event.Sink
	// Run calls session and presents its events until session returns.
	Run(ctx context.Context, session func(context.Context) error) error
	// ShowSummary presents the results after the session.
	ShowSummary(session, allTime report.Report)
}

// ReportUI presents a report. plain.ReportUI and tui.ReportUI implement it.
type ReportUI interface {
	Show(ctx context.Context, r report.Report) error
}

// Execute runs cmd.
func Execute(ctx context.Context, cmd cli.Command, stdio IO) error {
	switch c := cmd.(type) {
	case cli.RunCommand:
		return Run(ctx, c, stdio)
	case cli.ReportCommand:
		return Report(ctx, c, stdio)
	case cli.ModelsCommand:
		return Models(ctx, c, stdio)
	case cli.VersionCommand:
		return Version(stdio)
	}
	return fmt.Errorf("unknown command %T", cmd)
}

// Exit codes of the tool.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
	ExitStopped = 130
)

// ExitCode maps the error of Parse or Execute to the exit code.
func ExitCode(err error) int {
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return ExitOK
	case errors.Is(err, cli.ErrUsage), errors.Is(err, bench.ErrInvalidOptions):
		return ExitUsage
	case errors.Is(err, context.Canceled):
		return ExitStopped
	}
	return ExitFailure
}

// Fail writes err to w, except for a help request, and returns the exit
// code of err.
func Fail(w io.Writer, err error) int {
	code := ExitCode(err)
	switch {
	case err == nil || errors.Is(err, flag.ErrHelp):
	case code == ExitStopped:
		fmt.Fprintln(w, "token-rate: stopped")
	default:
		fmt.Fprintf(w, "token-rate: %v\n", err)
	}
	return code
}
