package reportview

import (
	"slices"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"dyno/internal/report"
	"dyno/internal/ui/format"
	"dyno/internal/ui/tui/theme"
)

// Layout limits, in cells.
const (
	defaultWidth   = 80 // the window before the first tea.WindowSizeMsg
	defaultHeight  = 24
	minTableRows   = 3  // the table shows at least this many groups
	minChartHeight = 6  // the daily chart takes at least this many rows
	indentWidth    = 1  // the lines outside the table leave room for its marker
	markerWidth    = 1  // the first table column points to the selected group
	wideGap        = 2  // the space between two table columns
	narrowGap      = 1  // the space between two table columns in a narrow window
	minTextWidth   = 10 // a text column keeps at least this width, which a date needs
)

// Rows of the view outside the table rows and the daily chart.
const (
	sectionGap      = 1 // the empty line between two sections
	tableHeaderRows = 1
	chartTextRows   = 3 // the chart heading, the statistics, and the line of the last runs
)

// size is a width and a height in cells.
type size struct{ width, height int }

// Model is the report view. It implements tea.Model.
type Model struct {
	report  report.Report
	columns columnSet
	fit     columnFit
	keys    keyMap
	help    help.Model
	table   table.Model
	theme   theme.Theme
	metric  int // the index of the selected metric in report.Metrics
	window  size
	chart   size // the daily chart
}

// New returns the view of r. It has the theme of a dark background until
// the terminal reports its color, and the size of 80×24 cells until the
// first tea.WindowSizeMsg.
func New(r report.Report) Model {
	m := Model{
		report:  r,
		columns: splitColumns(r),
		keys:    newKeyMap(len(r.Groups) > 0),
		help:    newHelp(),
		table:   table.New(),
		window:  size{width: defaultWidth, height: defaultHeight},
	}
	return m.themed(theme.New(true)).laidOut()
}

// newHelp returns the help model with the separator of the other lines.
func newHelp() help.Model {
	h := help.New()
	h.ShortSeparator = format.Separator
	return h
}

// Init asks the terminal for its background color, which picks the theme.
func (m Model) Init() tea.Cmd { return tea.RequestBackgroundColor }

// Update handles the background color, the window size, and the keys.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		return m.themed(theme.New(msg.IsDark())), nil
	case tea.WindowSizeMsg:
		m.window = size{width: msg.Width, height: msg.Height}
		return m.laidOut(), nil
	case tea.KeyPressMsg:
		return m.pressed(msg)
	}
	return m, nil
}

// pressed handles one key.
func (m Model) pressed(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Up):
		return m.moved(-1), nil
	case key.Matches(msg, m.keys.Down):
		return m.moved(1), nil
	case key.Matches(msg, m.keys.Next):
		return m.cycled(1), nil
	case key.Matches(msg, m.keys.Prev):
		return m.cycled(-1), nil
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m.laidOut(), nil
	}
	return m, nil
}

// moved selects the group delta rows away. It stops at the first and the
// last group.
func (m Model) moved(delta int) Model {
	if delta < 0 {
		m.table.MoveUp(-delta)
	} else {
		m.table.MoveDown(delta)
	}
	m.table.SetRows(groupRows(m.columns.table, m.report.Groups, m.fit, m.table.Cursor()))
	return m
}

// cycled selects the metric delta places away in report.Metrics. It wraps
// around at both ends.
func (m Model) cycled(delta int) Model {
	n := len(report.Metrics())
	m.metric = ((m.metric+delta)%n + n) % n
	return m
}

// themed gives the table and the help the styles of th.
func (m Model) themed(th theme.Theme) Model {
	m.theme = th
	// The cells keep the zero style: a colored cell would end the
	// background of the selected row after the first cell.
	m.table.SetStyles(table.Styles{Header: th.Header, Selected: th.Selected})
	m.help.Styles = help.Styles{
		Ellipsis:       th.Muted,
		ShortKey:       th.Text,
		ShortDesc:      th.Muted,
		ShortSeparator: th.Muted,
		FullKey:        th.Text,
		FullDesc:       th.Muted,
		FullSeparator:  th.Muted,
	}
	return m
}

// laidOut sizes the table and the chart for the window.
func (m Model) laidOut() Model {
	m.help.SetWidth(m.contentWidth())
	cols := m.columns.table
	m.fit = fitColumns(cols, m.report.Groups, m.window.width-markerWidth)
	m.table.SetColumns(tableColumns(cols, m.fit))
	m.table.SetRows(groupRows(cols, m.report.Groups, m.fit, m.table.Cursor()))
	m.table.SetWidth(m.window.width)
	rows := m.tableRows()
	m.table.SetHeight(tableHeaderRows + rows)
	m.chart = size{width: m.contentWidth(), height: m.chartHeight(rows)}
	return m
}

// contentWidth returns the width of the lines outside the table.
func (m Model) contentWidth() int { return max(0, m.window.width-indentWidth) }

// tableRows returns the number of groups that the table shows: all of them,
// unless the daily chart would get fewer than minChartHeight rows.
func (m Model) tableRows() int {
	free := m.window.height - m.fixedRows() - minChartHeight
	return min(len(m.report.Groups), max(minTableRows, free))
}

// chartHeight returns the rows that are left for the daily chart.
func (m Model) chartHeight(tableRows int) int {
	return max(minChartHeight, m.window.height-m.fixedRows()-tableRows)
}

// fixedRows returns the rows of the sections of content without the table
// rows and the daily chart.
func (m Model) fixedRows() int {
	rows := lipgloss.Height(m.headerView()) + sectionGap + tableHeaderRows + sectionGap + chartTextRows
	if slices.ContainsFunc(m.report.Groups, func(g report.Group) bool { return format.Warnings(g.Warnings) != "" }) {
		rows++
	}
	if h := m.helpView(); h != "" {
		rows += sectionGap + lipgloss.Height(h)
	}
	return rows
}

// selectedGroup returns the group of the table row with the marker. The
// report must have groups.
func (m Model) selectedGroup() report.Group { return m.report.Groups[m.table.Cursor()] }

// selectedMetric returns the metric of the charts.
func (m Model) selectedMetric() report.Metric { return report.Metrics()[m.metric] }

// columnFit is the width of each table column and the gap between them.
type columnFit struct {
	widths []int
	gap    int
}

// fitColumns returns the widths that fit cols into width cells. It narrows
// the gap first. Then it truncates non-identity text columns from the last one,
// down to minTextWidth. Identity and policy labels retain their display widths
// so fitting cannot erase their distinguishing suffixes. The view clips
// what still does not fit.
func fitColumns[T any](cols []format.Column[T], items []T, width int) columnFit {
	fit := columnFit{widths: naturalWidths(cols, items), gap: wideGap}
	if fit.total() <= width {
		return fit
	}
	fit.gap = narrowGap
	for i := len(cols) - 1; i >= 0 && fit.total() > width; i-- {
		if cols[i].Right || cols[i].Header == report.ByModelID.Header() || cols[i].Header == report.ByDigest.Header() || cols[i].Header == report.ByMeasurement.Header() {
			continue
		}
		w, over := fit.widths[i], fit.total()-width
		fit.widths[i] = max(min(w, minTextWidth), w-over)
	}
	return fit
}

// total returns the width of all columns and gaps.
func (f columnFit) total() int {
	sum := f.gap * max(0, len(f.widths)-1)
	for _, w := range f.widths {
		sum += w
	}
	return sum
}

// columnWidth returns the width of column i with the gap after it. The last
// column has no gap.
func (f columnFit) columnWidth(i int) int {
	if i == len(f.widths)-1 {
		return f.widths[i]
	}
	return f.widths[i] + f.gap
}

// naturalWidths returns the width of the widest header or cell of each
// column.
func naturalWidths[T any](cols []format.Column[T], items []T) []int {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = ansi.StringWidth(c.Header)
		for _, item := range items {
			widths[i] = max(widths[i], ansi.StringWidth(c.Cell(item)))
		}
	}
	return widths
}
