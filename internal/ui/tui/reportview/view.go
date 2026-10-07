package reportview

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"dyno/internal/report"
	"dyno/internal/ui/format"
)

// Texts of the view.
const (
	marker     = "▸"
	indentText = " "
	minHintGap = 2 // the least space between the title and the key hint
)

// View returns the view on the alternate screen, cut to the window width.
func (m Model) View() tea.View {
	v := tea.NewView(clip(m.content(), m.window.width))
	v.AltScreen = true
	return v
}

// content returns the sections of the view with an empty line between two
// sections. A report without groups has only the header.
func (m Model) content() string {
	if len(m.report.Groups) == 0 {
		return indent(m.headerView())
	}
	sections := []string{indent(m.headerView()), m.tableView(), indent(m.chartsView())}
	if h := m.helpView(); h != "" {
		sections = append(sections, indent(h))
	}
	return strings.Join(sections, strings.Repeat("\n", sectionGap+1))
}

// headerView returns the title line and the dimension values that every
// group shares.
func (m Model) headerView() string {
	if facts := m.factsView(); facts != "" {
		return m.titleView() + "\n" + facts
	}
	return m.titleView()
}

// titleView returns the title, the totals of the report, and the key hint
// on one line. When they do not fit, the totals go to a second line.
func (m Model) titleView() string {
	name := m.theme.Title.Render(format.ReportTitle)
	summary := m.theme.Muted.Render(format.Overview(m.report))
	hint := m.help.ShortHelpView(m.keys.ShortHelp())
	full := name + m.theme.Muted.Render(format.Separator) + summary
	if lipgloss.Width(full)+minHintGap+lipgloss.Width(hint) <= m.contentWidth() {
		return format.Justify(full, hint, m.contentWidth(), minHintGap, lipgloss.Width)
	}
	return format.Justify(name, hint, m.contentWidth(), minHintGap, lipgloss.Width) + "\n" + summary
}

// factsView returns the dimension values that every group shares, or "".
func (m Model) factsView() string {
	if len(m.columns.shared) == 0 {
		return ""
	}
	facts := make([]string, len(m.columns.shared))
	for i, c := range m.columns.shared {
		value := c.Cell(m.report.Groups[0])
		if (c.Header == report.ByModelID.Header() || c.Header == report.ByDigest.Header()) && m.report.Groups[0].Key.Revision != "" {
			value = format.Text(m.report.Groups[0].Key.Revision)
		}
		facts[i] = m.theme.Header.Render(c.Header) + " " + m.theme.Text.Render(value)
	}
	return ansi.Wrap(strings.Join(facts, m.theme.Muted.Render(format.Separator)), max(1, m.contentWidth()), "")
}

// tableView returns the table of the groups.
func (m Model) tableView() string { return m.table.View() }

// chartsView returns the heading and the charts of the selected group and
// metric, or the heading and a hint when the group has no valid run.
func (m Model) chartsView() string {
	g, metric := m.selectedGroup(), m.selectedMetric()
	heading := m.chartHeading(g, metric)
	if warnings := format.Warnings(g.Warnings); warnings != "" {
		heading += "\n" + m.theme.Warn.Render("warnings: "+warnings)
	}
	if g.Summary(metric).N == 0 {
		hint := noValidRuns
		if g.Valid > 0 {
			hint = metricUnavailable
		}
		return heading + "\n" + m.theme.Muted.Render(hint)
	}
	return strings.Join([]string{
		heading,
		m.theme.Muted.Render(format.Stats(g.Summary(metric), format.MetricValue(metric))),
		dailyView(m.theme, g.Daily[metric], metric, m.chart),
		recentView(m.theme, g.Recent[metric], metric, m.chart.width),
	}, "\n")
}

// chartHeading names the metric and the selected group by every value
// that is not in the header.
func (m Model) chartHeading(g report.Group, metric report.Metric) string {
	parts := []string{m.theme.Header.Render(metric.Label() + format.Separator + dailyLabel)}
	for _, c := range m.columns.named {
		parts = append(parts, m.theme.Text.Render(c.Cell(g)))
	}
	return strings.Join(parts, m.theme.Muted.Render(format.Separator))
}

// helpView returns the full help, or "" while it is hidden.
func (m Model) helpView() string {
	if !m.help.ShowAll {
		return ""
	}
	return m.help.FullHelpView(m.keys.FullHelp())
}

// indent moves each line of s right by indentText.
func indent(s string) string {
	return indentText + strings.ReplaceAll(s, "\n", "\n"+indentText)
}

// clip cuts each line of s to width cells.
func clip(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n")
}

// columnSet splits the columns of format.GroupColumns for the view.
type columnSet struct {
	shared []format.Column[report.Group] // dimensions with one value in every group, for the header
	named  []format.Column[report.Group] // the other dimensions, for the chart heading
	table  []format.Column[report.Group] // the dimensions that tell groups apart, then the statistics
}

// splitColumns returns the columns of r. The table leaves out each
// dimension whose values follow from earlier dimensions, except model identity
// and measurement policy: their variation must stay visible. The header shows the
// dimensions with one value, and the chart heading shows all others for the
// selected group. GroupColumns puts the dimensions first.
func splitColumns(r report.Report) columnSet {
	cols := format.GroupColumns(r.Dimensions, r.Groups...)
	var set columnSet
	var distinct, displayDistinct []format.Column[report.Group]
	for i, c := range cols[:len(r.Dimensions)] {
		// Determine dependencies from full values, not abbreviated identity cells.
		raw := c
		d := r.Dimensions[i]
		raw.Cell = func(g report.Group) string { return g.Key.Value(d) }
		switch {
		case determined(raw, nil, r.Groups):
			if !format.RedundantNativeFact(r.Groups[0].Key, d) {
				set.shared = append(set.shared, c)
			}
		case (d == report.ByDigest || d == report.ByModelID) && format.IdentityDeterminedByModel(r):
			set.named = append(set.named, c)
		case d != report.ByDigest && d != report.ByModelID && d != report.ByMeasurement && d != report.ByEndpoint && determined(raw, distinct, r.Groups):
			set.named = append(set.named, c)
		default:
			set.named = append(set.named, c)
			distinct = append(distinct, raw)
			displayDistinct = append(displayDistinct, c)
		}
	}
	// Shared protocol labels go first, so clipping never hides provenance
	// behind an endpoint or a full model identity.
	slices.SortStableFunc(set.shared, protocolFirst)
	slices.SortStableFunc(set.named, protocolFirst)
	slices.SortStableFunc(displayDistinct, protocolFirst)
	set.table = slices.Concat(displayDistinct, cols[len(r.Dimensions):])
	return set
}

func protocolFirst(a, b format.Column[report.Group]) int {
	if a.Header == report.ByMeasurement.Header() && b.Header != a.Header {
		return -1
	}
	if b.Header == report.ByMeasurement.Header() && a.Header != b.Header {
		return 1
	}
	return 0
}

// determined reports whether the cells of c follow from the cells of by: no
// two groups have the same cells in by and different cells in c. It is
// false without groups.
func determined(c format.Column[report.Group], by []format.Column[report.Group], groups []report.Group) bool {
	if len(groups) == 0 {
		return false
	}
	cells := make(map[string]string, len(groups))
	for _, g := range groups {
		k, v := rowKey(by, g), c.Cell(g)
		if prev, ok := cells[k]; ok && prev != v {
			return false
		}
		cells[k] = v
	}
	return true
}

// rowKey joins the cells of cols for g into a map key.
func rowKey(cols []format.Column[report.Group], g report.Group) string {
	cells := make([]string, len(cols))
	for i, c := range cols {
		cells[i] = c.Cell(g)
	}
	return strings.Join(cells, "\x00")
}

// tableColumns returns the marker column and then cols with the widths of
// fit.
func tableColumns(cols []format.Column[report.Group], fit columnFit) []table.Column {
	out := make([]table.Column, 0, len(cols)+1)
	out = append(out, table.Column{Width: markerWidth})
	for i, c := range cols {
		out = append(out, table.Column{Title: fitCell(c.Header, fit.widths[i], c.Right), Width: fit.columnWidth(i)})
	}
	return out
}

// groupRows returns one row for each group. The marker points to the
// selected group.
func groupRows(cols []format.Column[report.Group], groups []report.Group, fit columnFit, selected int) []table.Row {
	rows := make([]table.Row, len(groups))
	for r, g := range groups {
		row := make(table.Row, 0, len(cols)+1)
		row = append(row, markerCell(r == selected))
		for i, c := range cols {
			row = append(row, fitCell(c.Cell(g), fit.widths[i], c.Right))
		}
		rows[r] = row
	}
	return rows
}

// markerCell returns the marker for the selected row and a space for the
// other rows.
func markerCell(selected bool) string {
	if selected {
		return marker
	}
	return " "
}

// fitCell truncates text to width cells. A cell that aligns to the right
// gets spaces on the left.
func fitCell(text string, width int, right bool) string {
	text = ansi.Truncate(text, width, "…")
	if !right {
		return text
	}
	return strings.Repeat(" ", width-ansi.StringWidth(text)) + text
}
