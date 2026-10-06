package plain

import (
	"context"
	"fmt"
	"io"

	"dyno/internal/report"
	"dyno/internal/ui/format"
)

// reportTitle starts the heading of the report command.
const reportTitle = "token-rate report"

// ReportUI prints a report as a table.
type ReportUI struct{ out io.Writer }

// NewReportUI returns a ReportUI that writes to out.
func NewReportUI(out io.Writer) *ReportUI { return &ReportUI{out: out} }

// Show prints a heading with the totals of r, the table of its groups, and
// the statistics of each metric, or a line that no runs match.
func (u *ReportUI) Show(_ context.Context, r report.Report) error {
	if err := writeReport(u.out, reportTitle, r); err != nil {
		return fmt.Errorf("show report: %w", err)
	}
	if err := writeStatistics(u.out, r); err != nil {
		return fmt.Errorf("show report: %w", err)
	}
	return nil
}

// writeStatistics writes one table with every statistic of each metric. A
// report without groups gets none.
func writeStatistics(w io.Writer, r report.Report) error {
	if len(r.Groups) == 0 {
		return nil
	}
	for _, m := range report.Metrics() {
		if _, err := fmt.Fprintf(w, "\n%s%sstatistics\n", m.Label(), format.Separator); err != nil {
			return err
		}
		if err := writeTable(w, format.StatsColumns(r.Dimensions, m), r.Groups); err != nil {
			return err
		}
	}
	return nil
}

// writeReport writes the heading of r and the table of its groups. A report
// without groups gets only the heading.
func writeReport(w io.Writer, title string, r report.Report) error {
	if _, err := fmt.Fprintln(w, heading(title, r)); err != nil {
		return err
	}
	if len(r.Groups) == 0 {
		return nil
	}
	return writeTable(w, format.GroupColumns(r.Dimensions), r.Groups)
}

// heading returns title and the overview of r.
func heading(title string, r report.Report) string {
	return title + format.Separator + format.Overview(r)
}
