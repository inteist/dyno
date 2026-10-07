package runview

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"dyno/internal/report"
	"dyno/internal/ui/format"
)

// reportLines returns the heading and a fitted table. External reports stack
// only when fitting would hide distinguishing provenance. Native reports keep
// the historical table layout even when optional statistics must be omitted.
func (m Model) reportLines(title string, r report.Report, width int) []string {
	lines := strings.Split(ansi.Wrap(m.heading(title, r), max(1, width), ""), "\n")
	if len(r.Groups) == 0 {
		return lines
	}
	shared, dims := format.ReportDimensions(r)
	if len(shared) > 0 {
		lines = append(lines, strings.Split(m.look.theme.Text.Render(ansi.Wrap(format.DimensionFacts(r.Groups[0].Key, shared), max(1, width), "")), "\n")...)
	}
	cols := format.GroupColumns(dims, r.Groups...)
	t := m.reportTable(r)
	external := format.HasCompatibleGroups(r.Groups)
	order := dropOrder(len(dims), len(cols))
	if !external {
		order = nativeDropOrder(cols, len(dims))
	}
	t = fitColumns(t, order, width)
	if external && (tableWidth(t) > width || !preservesDimensions(t, dims) || !slices.Contains(t.Header, "WARNINGS") && slices.ContainsFunc(r.Groups, func(g report.Group) bool { return format.Warnings(g.Warnings) != "" })) {
		for _, g := range r.Groups {
			lines = append(lines, m.stackedGroup(g, dims, width)...)
		}
		return lines
	}
	return append(lines, tableLines(t, m.look.theme.Header)...)
}

func preservesDimensions(t format.Table, dims []report.Dimension) bool {
	for _, d := range dims {
		if !slices.Contains(t.Header, d.Header()) {
			return false
		}
	}
	return true
}

// stackedGroup retains every statistic and caveat of the table.
func (m Model) stackedGroup(g report.Group, dims []report.Dimension, width int) []string {
	wrap := func(s string) string { return ansi.Wrap(s, max(1, width), "") }
	text := m.look.theme.Text
	lines := []string{
		m.look.theme.Header.Render(wrap(format.DimensionFacts(g.Key, dims))),
		text.Render(wrap("N " + format.Int(g.Valid) + sep + "DECODE " + format.Spread(g.Decode, format.Rate) + " tok/s" + sep + "PREFILL " + format.Median(g.Prefill, format.Millis) + " ms")),
		text.Render(wrap("TTFT " + format.Median(g.TTFT, format.Millis) + " ms" + sep + "LATENCY " + format.Median(g.Latency, format.Millis) + " ms" + sep + "CLIENT " + format.Median(g.ClientRate, format.Rate) + " tok/s")),
		text.Render(wrap("CACHED " + format.Median(g.CachedShare, format.Percent) + sep + "EXCLUDED " + format.Text(format.Excluded(g.Excluded)))),
	}
	if warnings := format.Warnings(g.Warnings); warnings != "" {
		lines = append(lines, m.look.theme.Warn.Render(wrap("WARNINGS "+warnings)))
	}
	// The inline renderer clips individual entries. Preserve wrapped lines as
	// separate entries or clipping would discard their continuation text.
	return strings.Split(strings.Join(lines, "\n"), "\n")
}

// heading returns title and the overview of r.
func (m Model) heading(title string, r report.Report) string {
	t := m.look.theme
	return t.Title.Render(title) + t.Muted.Render(sep+format.Overview(r))
}

// reportTable uses only varying dimensions; constants appear in the heading.
func (m Model) reportTable(r report.Report) format.Table {
	_, dims := format.ReportDimensions(r)
	return format.TableOf(format.GroupColumns(dims, r.Groups...), r.Groups).Map(nil, render(m.look.theme.Text))
}

// dropOrder tries intermediate dimensions and the last optional column first.
// External callers check that fitting has not hidden provenance.
func dropOrder(dims, cols int) []int {
	var order []int
	for i := 1; i < dims-1; i++ {
		order = append(order, i)
	}
	return append(order, cols-1)
}

// nativeDropOrder preserves all varying dimensions, including full model-ID
// distinctions, while removing optional statistics to fit a narrow table.
func nativeDropOrder(cols []format.Column[report.Group], dims int) []int {
	var order []int
	for _, header := range []string{"WARNINGS", "CLIENT tok/s", "LATENCY ms", "EXCLUDED", "TTFT ms", "CACHED", "DECODE tok/s"} {
		for i := dims; i < len(cols); i++ {
			if cols[i].Header == header {
				order = append(order, i)
			}
		}
	}
	return order
}
