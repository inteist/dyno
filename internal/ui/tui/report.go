package tui

import (
	"context"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"

	"dyno/internal/report"
	"dyno/internal/ui/tui/reportview"
)

// ReportUI shows a report in the report view on the alternate screen.
type ReportUI struct {
	in      io.Reader
	out     io.Writer
	options []tea.ProgramOption // more program options, for tests
}

// NewReportUI returns a ReportUI that reads the keys from in and draws on
// out.
func NewReportUI(in io.Reader, out io.Writer) *ReportUI {
	return &ReportUI{in: in, out: out}
}

// Show runs the report view of r until the user quits, and then returns
// nil. When ctx ends first, the view stops and Show returns ctx.Err().
func (u *ReportUI) Show(ctx context.Context, r report.Report) error {
	_, err := tea.NewProgram(reportview.New(r), u.programOptions(ctx)...).Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("show the report: %w", err)
	}
	return nil
}

// programOptions returns the options of the program. The program has no
// signal handler of its own: the caller cancels ctx on a signal, which
// stops the program.
func (u *ReportUI) programOptions(ctx context.Context) []tea.ProgramOption {
	return append([]tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithInput(u.in),
		tea.WithOutput(u.out),
		tea.WithoutSignalHandler(),
	}, u.options...)
}
