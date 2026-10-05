package format

import (
	"slices"
	"strings"
)

// Table is the text of a table: a header, the rows of cells, and the
// alignment of each column. The plain UI and the run view lay it out with
// Lines, each with its own measure of a cell.
type Table struct {
	Header []string
	Right  []bool // the cells of a column align to the right
	Rows   [][]string
}

// TableOf returns the header and the cells of items in cols.
func TableOf[T any](cols []Column[T], items []T) Table {
	t := Table{Header: make([]string, len(cols)), Right: make([]bool, len(cols))}
	for i, c := range cols {
		t.Header[i], t.Right[i] = c.Header, c.Right
	}
	for _, item := range items {
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = c.Cell(item)
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

// With returns a copy of t with one more left-aligned column at the end.
// cells holds one cell for each row.
func (t Table) With(header string, cells []string) Table {
	out := Table{Header: append(slices.Clone(t.Header), header), Right: append(t.aligns(), false)}
	for i, row := range t.Rows {
		out.Rows = append(out.Rows, append(slices.Clone(row), cells[i]))
	}
	return out
}

// Only returns a copy of t with the columns that keep marks.
func (t Table) Only(keep []bool) Table {
	out := Table{Header: kept(t.Header, keep), Right: kept(t.aligns(), keep)}
	for _, row := range t.Rows {
		out.Rows = append(out.Rows, kept(row, keep))
	}
	return out
}

// Map returns a copy of t with header applied to each header cell and cell
// applied to each cell of the rows. A nil function keeps the text.
func (t Table) Map(header, cell func(string) string) Table {
	out := Table{Header: mapped(t.Header, header), Right: t.aligns()}
	for _, row := range t.Rows {
		out.Rows = append(out.Rows, mapped(row, cell))
	}
	return out
}

// Widths returns the width of each column, as measure gives the width of a
// cell.
func (t Table) Widths(measure func(string) int) []int {
	widths := make([]int, len(t.Header))
	for _, row := range slices.Concat([][]string{t.Header}, t.Rows) {
		for i, c := range row {
			widths[i] = max(widths[i], measure(c))
		}
	}
	return widths
}

// Width returns the width of the widest line of t.
func (t Table) Width(gap string, measure func(string) int) int {
	total := len(gap) * max(0, len(t.Header)-1)
	for _, w := range t.Widths(measure) {
		total += w
	}
	return total
}

// Lines returns the header line and one line for each row. Each cell fills
// the width of its column, and the cells of a Right column align to the
// right. gap separates the cells. A line leaves out its empty cells at the
// end and the spaces after its last cell.
func (t Table) Lines(gap string, measure func(string) int) []string {
	widths, aligns := t.Widths(measure), t.aligns()
	lines := make([]string, 0, len(t.Rows)+1)
	for _, row := range slices.Concat([][]string{t.Header}, t.Rows) {
		lines = append(lines, tableLine(row, widths, aligns, gap, measure))
	}
	return lines
}

// aligns returns a copy of Right with one entry for each column.
func (t Table) aligns() []bool {
	out := make([]bool, len(t.Header))
	copy(out, t.Right)
	return out
}

// tableLine pads each cell to the width of its column and joins the cells.
func tableLine(cells []string, widths []int, right []bool, gap string, measure func(string) int) string {
	n := len(cells)
	for n > 0 && measure(cells[n-1]) == 0 {
		n--
	}
	padded := make([]string, n)
	for i, c := range cells[:n] {
		fill := strings.Repeat(" ", max(0, widths[i]-measure(c)))
		if right[i] {
			padded[i] = fill + c
		} else {
			padded[i] = c + fill
		}
	}
	return strings.TrimRight(strings.Join(padded, gap), " ")
}

// Justify returns left and right on one line of width cells, with at least
// minGap spaces between them. measure gives the width of a text.
func Justify(left, right string, width, minGap int, measure func(string) int) string {
	gap := max(minGap, width-measure(left)-measure(right))
	return left + strings.Repeat(" ", gap) + right
}

// kept returns the values whose keep mark is set.
func kept[T any](values []T, keep []bool) []T {
	var out []T
	for i, v := range values {
		if keep[i] {
			out = append(out, v)
		}
	}
	return out
}

// mapped returns a copy of texts with f applied to each, or a plain copy
// for a nil f.
func mapped(texts []string, f func(string) string) []string {
	out := slices.Clone(texts)
	if f == nil {
		return out
	}
	for i, s := range out {
		out[i] = f(s)
	}
	return out
}
