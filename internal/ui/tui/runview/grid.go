package runview

import (
	"charm.land/lipgloss/v2"

	"token-rate/internal/ui/format"
)

// gap is the space between two columns.
const gap = "  "

// tableWidth returns the width of the widest line of t. lipgloss.Width
// ignores the styles of the cells.
func tableWidth(t format.Table) int { return t.Width(gap, lipgloss.Width) }

// tableLines returns the lines of t with the header in style h.
func tableLines(t format.Table, h lipgloss.Style) []string {
	return t.Map(render(h), nil).Lines(gap, lipgloss.Width)
}

// render returns a function that renders one text in style s.
func render(s lipgloss.Style) func(string) string {
	return func(text string) string { return s.Render(text) }
}

// fitColumns drops the columns of t in the given order until t fits width.
// It stops when the order ends, even if t is still too wide.
func fitColumns(t format.Table, order []int, width int) format.Table {
	keep := make([]bool, len(t.Header))
	for i := range keep {
		keep[i] = true
	}
	out := t
	for _, i := range order {
		if tableWidth(out) <= width {
			break
		}
		keep[i] = false
		out = t.Only(keep)
	}
	return out
}
