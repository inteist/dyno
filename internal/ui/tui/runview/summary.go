package runview

import (
	"dyno/internal/report"
	"dyno/internal/ui/format"
)

// reportLines returns the heading of r and the table of its groups, with
// the columns of the plain UI. A report without groups gets only the
// heading.
func (m Model) reportLines(title string, r report.Report, width int) []string {
	lines := []string{m.heading(title, r)}
	if len(r.Groups) == 0 {
		return lines
	}
	t := fitColumns(m.reportTable(r), dropOrder(len(r.Dimensions), len(format.GroupColumns(r.Dimensions))), width)
	return append(lines, tableLines(t, m.look.theme.Header)...)
}

// heading returns title and the overview of r.
func (m Model) heading(title string, r report.Report) string {
	t := m.look.theme
	return t.Title.Render(title) + t.Muted.Render(sep+format.Overview(r))
}

// reportTable returns the cells of the groups of r in the text style.
func (m Model) reportTable(r report.Report) format.Table {
	return format.TableOf(format.GroupColumns(r.Dimensions), r.Groups).Map(nil, render(m.look.theme.Text))
}

// dropOrder returns the columns that a narrow report leaves out first: the
// dimensions between the first and the last one (such as the digest and the
// server version), then the last column (the excluded runs).
func dropOrder(dims, cols int) []int {
	var order []int
	for i := 1; i < dims-1; i++ {
		order = append(order, i)
	}
	return append(order, cols-1)
}
