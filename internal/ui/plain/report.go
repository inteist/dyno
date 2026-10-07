package plain

import (
	"context"
	"dyno/internal/report"
	"dyno/internal/ui/format"
	"fmt"
	"io"
	"slices"
)

// ReportUI prints a report as a table.
type ReportUI struct{ out io.Writer }

// NewReportUI returns a ReportUI that writes to out.
func NewReportUI(out io.Writer) *ReportUI { return &ReportUI{out: out} }

// Show prints a heading with the totals of r, the table of its groups, and
// the statistics of each metric, or a line that no runs match.
func (u *ReportUI) Show(_ context.Context, r report.Report) error {
	if err := writeReport(u.out, format.ReportTitle, r); err != nil {
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
	dims := plainDimensions(r)
	for _, m := range report.Metrics() {
		if !format.HasCompatibleGroups(r.Groups) && (m == report.MetricLatency || m == report.MetricClientRate) {
			continue
		}
		if _, err := fmt.Fprintf(w, "\n%s%sstatistics\n", m.Label(), format.Separator); err != nil {
			return err
		}
		if err := writeTable(w, format.StatsColumns(dims, m, r.Groups...), r.Groups); err != nil {
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
	if err := writeTable(w, format.StableGroupColumns(plainDimensions(r), r.Groups...), r.Groups); err != nil {
		return err
	}
	if err := writeNativeWarnings(w, r); err != nil {
		return err
	}
	return writeRevisionFacts(w, r)
}

func writeNativeWarnings(w io.Writer, r report.Report) error {
	if format.HasCompatibleGroups(r.Groups) {
		return nil
	}
	for i, g := range r.Groups {
		if warning := format.Warnings(g.Warnings); warning != "" {
			dims := plainDimensions(r)
			if g.Key.Server == "" {
				dims = slices.DeleteFunc(dims, func(d report.Dimension) bool { return d == report.ByServer })
			}
			label := format.DimensionFacts(g.Key, dims)
			if _, err := fmt.Fprintf(w, "\nwarnings [row %d]: %s · %s\n", i+1, label, warning); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeRevisionFacts prints one separated block, retaining full display
// provenance independently of the user's selected grouping dimensions.
func writeRevisionFacts(w io.Writer, r report.Report) error {
	seen := make(map[string]bool)
	separated := false
	for _, g := range r.Groups {
		revision, endpoint := g.Key.Revision, format.EndpointFact(g)
		identity := g.Key.Model + "\x00" + endpoint + "\x00" + revision
		if revision == "" || seen[identity] {
			continue
		}
		seen[identity] = true
		if !separated {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
			separated = true
		}
		label := format.Text(g.Key.Model)
		if endpoint != "" {
			label += " · " + format.Text(endpoint)
		}
		if _, err := fmt.Fprintln(w, "model revision: "+label+" · "+format.Text(revision)); err != nil {
			return err
		}
	}
	return nil
}

// plainDimensions restores the native default schema, retaining non-default facts.
func plainDimensions(r report.Report) []report.Dimension {
	dims := slices.Clone(r.Dimensions)
	if !format.HasCompatibleGroups(r.Groups) && (slices.Equal(dims, report.DefaultDimensions()) || slices.Equal(dims, report.AllTimeDimensions())) {
		dims = slices.DeleteFunc(dims, func(d report.Dimension) bool {
			if d != report.ByBackend && d != report.ByEngine && d != report.ByEndpoint && d != report.ByMeasurement {
				return false
			}
			return !slices.ContainsFunc(r.Groups, func(g report.Group) bool { return !format.RedundantNativeFact(g.Key, d) })
		})
		for i, d := range dims {
			if d == report.ByModelID {
				dims[i] = report.ByDigest // retain the native default DIGEST header
			}
		}
	}
	return dims
}

// heading returns title and the overview of r.
func heading(title string, r report.Report) string {
	return title + format.Separator + format.Overview(r)
}
